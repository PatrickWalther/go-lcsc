package lcsc

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// RetryConfig configures retry behavior.
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
	if err != nil {
		if isTemporaryNetworkError(err) {
			return true
		}
		if isTimeoutError(err) {
			return true
		}
	}

	switch statusCode {
	case http.StatusTooManyRequests: // 429
		return true
	case http.StatusInternalServerError: // 500
		return true
	case http.StatusBadGateway: // 502
		return true
	case http.StatusServiceUnavailable: // 503
		return true
	case http.StatusGatewayTimeout: // 504
		return true
	}

	return false
}

// isTemporaryNetworkError checks if the error is a temporary network error.
func isTemporaryNetworkError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		// Check for temporary errors without using deprecated Temporary() method
		return netErr.Timeout() || isTemporaryNetErr(netErr)
	}
	return false
}

// isTemporaryNetErr checks if a network error is temporary.
func isTemporaryNetErr(err net.Error) bool {
	if _, ok := err.(*net.OpError); ok {
		// OpError is considered temporary if it's not a timeout
		// Most transient errors are timeouts, so we're conservative here
		return false
	}
	if _, ok := err.(*net.DNSError); ok {
		// DNS errors can be temporary
		return false
	}
	return false
}

// isTimeoutError checks if the error is a timeout error.
func isTimeoutError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
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

// parseRetryAfter parses the Retry-After header value.
// Returns the number of seconds to wait, or 0 if not parseable.
// nolint: unused
func parseRetryAfter(header string) int {
	if header == "" {
		return 0
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(header); err == nil {
		return seconds
	}

	// Try parsing as HTTP-date
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		seconds := int(time.Until(t).Seconds())
		if seconds > 0 {
			return seconds
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
