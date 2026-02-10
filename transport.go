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
)

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message json.RawMessage `json:"msg"`
	Result  json.RawMessage `json:"result"`
}

func (c *Client) do(ctx context.Context, method, path string, params url.Values, reqBody interface{}, result interface{}) error {
	var lastErr error
	maxAttempts := c.retryConfig.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := c.retryConfig.calculateBackoff(attempt - 1)
			if err := sleep(ctx, backoff); err != nil {
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

		lastErr = err
		if !shouldRetry(err, statusCode) || attempt >= maxAttempts-1 {
			return err
		}
	}

	return lastErr
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Code:       resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			Details:    string(respBody),
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
