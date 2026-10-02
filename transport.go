package lcsc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message json.RawMessage `json:"msg"`
	Result  json.RawMessage `json:"result"`
}

// maxEnvelopeServerRetries is the maximum number of retries for a server
// error that LCSC sends in the response envelope with a 2xx HTTP status.
// LCSC also sends such an error for some bad inputs. More retries only send
// the same failing request again.
const maxEnvelopeServerRetries = 1

// retrySleep waits between attempts. Tests replace it to record the delays.
var retrySleep = sleep

func (c *Client) do(ctx context.Context, method, path string, params url.Values, reqBody interface{}, result interface{}) error {
	return c.withRetry(ctx, func() (int, error) {
		return c.doOnce(ctx, method, path, params, reqBody, result)
	})
}

// withRetry calls attempt until it succeeds or the retry rules stop it. It
// waits for the rate limiter before each attempt. attempt returns the HTTP
// status code of its response, or 0 when it got no response.
func (c *Client) withRetry(ctx context.Context, attempt func() (int, error)) error {
	maxAttempts := c.retryConfig.MaxRetries + 1
	envelopeServerRetries := 0
	var delay time.Duration

	for n := 0; ; n++ {
		if n > 0 {
			if err := retrySleep(ctx, delay); err != nil {
				return err
			}
		}

		if err := c.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("lcsc: rate limiter wait failed: %w", err)
		}

		statusCode, err := attempt()
		if err == nil {
			return nil
		}

		if n >= maxAttempts-1 || !shouldRetry(err, statusCode) {
			return err
		}
		if isEnvelopeServerError(err, statusCode) {
			if envelopeServerRetries >= maxEnvelopeServerRetries {
				return err
			}
			envelopeServerRetries++
		}

		var ok bool
		delay, ok = c.retryConfig.retryDelay(n, err)
		if !ok {
			return err
		}
		// Do not wait when the context ends before the next attempt. The
		// caller then gets the API error and not a context error.
		if deadline, hasDeadline := ctx.Deadline(); hasDeadline && time.Until(deadline) < delay {
			return err
		}
	}
}

func (c *Client) doOnce(ctx context.Context, method, path string, params url.Values, reqBody interface{}, result interface{}) (int, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		payload, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("%w: failed to marshal request body: %v", ErrInvalidRequest, err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("lcsc: failed to create request: %w", err)
	}

	c.setHeaders(req, reqBody != nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("lcsc: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("lcsc: failed to read response body: %w", err)
	}

	retryAfter := time.Duration(parseRetryAfter(resp.Header.Get("Retry-After"))) * time.Second

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Code:       resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			Details:    string(respBody),
			RetryAfter: retryAfter,
		}
	}

	var envelope apiEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return resp.StatusCode, fmt.Errorf("lcsc: failed to parse response envelope: %w", err)
	}

	if envelope.Code != 200 {
		return resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Code:       envelope.Code,
			Message:    envelopeMessage(envelope.Message),
			Details:    string(respBody),
			RetryAfter: retryAfter,
		}
	}

	if result != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return resp.StatusCode, fmt.Errorf("lcsc: failed to parse result payload: %w", err)
		}
	}

	return resp.StatusCode, nil
}

func (c *Client) setHeaders(req *http.Request, hasBody bool) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", fmt.Sprintf("currencyCode=%s", c.currency))
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

func envelopeMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return strings.TrimSpace(string(raw))
}

const (
	// maxPageBytes is the largest web page body that the client reads. A
	// datasheet viewer page is about 10 KB.
	maxPageBytes = 2 << 20

	// maxPageRedirects is the largest number of redirects that the client
	// follows for one web page.
	maxPageRedirects = 5

	// pdfMediaType is the media type of a PDF file.
	pdfMediaType = "application/pdf"
)

// pageResponse is the result of [Client.getPage].
type pageResponse struct {
	// URL is the URL of the last response, after the redirects that the
	// client followed.
	URL string

	// StatusCode is the HTTP status of the last response. It is a 3xx
	// status when the client did not follow a redirect.
	StatusCode int

	// Location is the redirect target of a 3xx response.
	Location string

	// ContentType is the Content-Type header of the last response.
	ContentType string

	// Body holds the response body, up to maxPageBytes. It is empty for a
	// PDF file, because the client does not read the file.
	Body []byte
}

// isPDF reports whether the response is a PDF file.
func (p *pageResponse) isPDF() bool {
	if p == nil {
		return false
	}
	if mediaType, _, err := mime.ParseMediaType(p.ContentType); err == nil && mediaType == pdfMediaType {
		return true
	}
	return bytes.HasPrefix(p.Body, []byte("%PDF-"))
}

// getPage sends a GET request for a web page outside the API base URL, for
// example an LCSC datasheet viewer page. It uses the rate limiter and the
// retry rules of the client. It follows a redirect only when follow returns
// true for the target URL. For another redirect, it returns the 3xx
// response with no error. It returns an [APIError] for another status
// outside 2xx.
func (c *Client) getPage(ctx context.Context, pageURL string, follow func(*url.URL) bool) (*pageResponse, error) {
	// Copy the HTTP client, so that the redirect rule does not change the
	// client of the caller.
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxPageRedirects || follow == nil || !follow(req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}

	var page *pageResponse
	err := c.withRetry(ctx, func() (int, error) {
		var statusCode int
		var err error
		page, statusCode, err = getPageOnce(ctx, &httpClient, pageURL)
		return statusCode, err
	})
	if err != nil {
		return nil, err
	}
	return page, nil
}

func getPageOnce(ctx context.Context, httpClient *http.Client, pageURL string) (*pageResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("lcsc: failed to create request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("lcsc: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	page := &pageResponse{
		URL:         pageURL,
		StatusCode:  resp.StatusCode,
		Location:    resp.Header.Get("Location"),
		ContentType: resp.Header.Get("Content-Type"),
	}
	if resp.Request != nil && resp.Request.URL != nil {
		page.URL = resp.Request.URL.String()
	}

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return page, resp.StatusCode, nil
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		retryAfter := time.Duration(parseRetryAfter(resp.Header.Get("Retry-After"))) * time.Second
		return nil, resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Code:       resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			RetryAfter: retryAfter,
		}
	}

	// Do not read a PDF file. The caller needs only its URL.
	if page.isPDF() {
		return page, resp.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("lcsc: failed to read response body: %w", err)
	}
	page.Body = body
	return page, resp.StatusCode, nil
}
