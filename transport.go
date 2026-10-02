package lcsc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	maxAttempts := c.retryConfig.MaxRetries + 1
	envelopeServerRetries := 0
	var delay time.Duration

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := retrySleep(ctx, delay); err != nil {
				return err
			}
		}

		if err := c.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("lcsc: rate limiter wait failed: %w", err)
		}

		statusCode, err := c.doOnce(ctx, method, path, params, reqBody, result)
		if err == nil {
			return nil
		}

		if attempt >= maxAttempts-1 || !shouldRetry(err, statusCode) {
			return err
		}
		if isEnvelopeServerError(err, statusCode) {
			if envelopeServerRetries >= maxEnvelopeServerRetries {
				return err
			}
			envelopeServerRetries++
		}

		var ok bool
		delay, ok = c.retryConfig.retryDelay(attempt, err)
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
