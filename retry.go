package lcsc

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryConfig configures retry behavior.
//
// The client retries transport failures, HTTP 429 and HTTP 5xx responses
// up to MaxRetries times. LCSC can also send an error code in the response
// envelope with HTTP status 200. The client retries an envelope 429 up to
// MaxRetries times, but an envelope 5xx only one time.
//
// When a retryable response has a Retry-After header, the client waits at
// least that time before the next attempt. This applies to every
// retryable status, also to HTTP 5xx. When the Retry-After time is longer
// than MaxBackoff, the client does not retry. It returns the error, and
// [APIError.RetryAfter] holds the time.
//
// When the wait before the next attempt (the backoff or the Retry-After
// time) ends after the context deadline, the client does not wait. It
// returns the last error, for example an error that matches [ErrServer],
// and not context.DeadlineExceeded.
type RetryConfig struct {
	MaxRetries     int           // Maximum number of retry attempts (default 3)
	InitialBackoff time.Duration // Initial backoff duration (default 500ms)
	MaxBackoff     time.Duration // Maximum backoff duration (default 30s)
	Multiplier     float64       // Backoff multiplier (default 2.0)
	Jitter         float64       // Random jitter factor 0-1 (default 0.1)
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		Jitter:         0.1,
	}
}

// NoRetry returns a configuration that disables retries.
func NoRetry() RetryConfig {
	return RetryConfig{
		MaxRetries: 0,
	}
}

// shouldRetry determines if a request should be retried based on the error.
func shouldRetry(err error, statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}

	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if isTemporaryNetworkError(err) {
		return true
	}
	if isTimeoutError(err) {
		return true
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		if code == 0 {
			code = apiErr.StatusCode
		}
		switch code {
		case 429, 500, 502, 503, 504:
			return true
		}
	}

	return false
}

// isTemporaryNetworkError checks if the error is a temporary network error.
func isTemporaryNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// isTimeoutError checks if the error is a timeout error.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// calculateBackoff calculates the backoff duration for a retry attempt.
func (c RetryConfig) calculateBackoff(attempt int) time.Duration {
	backoff := float64(c.InitialBackoff) * pow(c.Multiplier, float64(attempt))

	// Apply jitter
	if c.Jitter > 0 {
		jitter := backoff * c.Jitter * (rand.Float64()*2 - 1)
		backoff += jitter
	}

	// Cap at max backoff
	if backoff > float64(c.MaxBackoff) {
		backoff = float64(c.MaxBackoff)
	}

	return time.Duration(backoff)
}

// pow calculates base^exp without importing math package.
func pow(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// retryDelay returns the wait time before the retry that follows the failed
// attempt. It uses the exponential backoff. When the server sends a
// Retry-After value that is longer than the backoff, it uses the Retry-After
// value. It returns false when the Retry-After value is longer than
// MaxBackoff. The client then does not retry and returns the error. The
// caller can read [APIError.RetryAfter].
func (c RetryConfig) retryDelay(attempt int, err error) (time.Duration, bool) {
	delay := c.calculateBackoff(attempt)

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		if apiErr.RetryAfter > c.MaxBackoff {
			return 0, false
		}
		if apiErr.RetryAfter > delay {
			delay = apiErr.RetryAfter
		}
	}
	return delay, true
}

// isEnvelopeServerError reports whether err is a server error that LCSC
// sends in the response envelope with a 2xx HTTP status.
func isEnvelopeServerError(err error, statusCode int) bool {
	if statusCode < 200 || statusCode >= 300 {
		return false
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code >= 500
}

// parseRetryAfter parses the Retry-After header value. The value is a
// number of seconds or an HTTP date. It returns the number of seconds to
// wait, or 0 when the value is not valid or is in the past.
func parseRetryAfter(header string) int {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds < 0 {
			return 0
		}
		return seconds
	}

	// Try parsing as HTTP-date. Also accept RFC 1123 with a zone name other
	// than GMT.
	t, err := http.ParseTime(header)
	if err != nil {
		t, err = time.Parse(time.RFC1123, header)
	}
	if err == nil {
		// Round up, so that the client does not retry too early.
		if wait := time.Until(t); wait > 0 {
			return int((wait + time.Second - 1) / time.Second)
		}
	}

	return 0
}

// sleep waits for the specified duration, respecting context cancellation.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
