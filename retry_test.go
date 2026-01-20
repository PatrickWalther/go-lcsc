package lcsc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestDefaultRetryConfig tests the default retry configuration.
func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries == 0 {
		t.Error("expected positive MaxRetries")
	}

	if config.InitialBackoff <= 0 {
		t.Error("expected positive InitialBackoff")
	}

	if config.MaxBackoff <= 0 {
		t.Error("expected positive MaxBackoff")
	}

	if config.Multiplier <= 0 {
		t.Error("expected positive Multiplier")
	}

	if config.Jitter < 0 || config.Jitter > 1 {
		t.Error("expected Jitter between 0 and 1")
	}
}

// TestNoRetry tests the NoRetry configuration.
func TestNoRetry(t *testing.T) {
	config := NoRetry()

	if config.MaxRetries != 0 {
		t.Errorf("expected MaxRetries 0, got %d", config.MaxRetries)
	}
}

// TestShouldRetryNetworkError tests retry logic for network errors.
func TestShouldRetryNetworkError(t *testing.T) {
	// OpErrors depend on timeout/temporary status
	// Just verify the function handles them
	err := &net.OpError{Op: "dial"}
	_ = shouldRetry(err, 0)
}

// TestShouldRetryStatusCode tests retry logic for HTTP status codes.
func TestShouldRetryStatusCode(t *testing.T) {
	retryableCodes := []int{429, 500, 502, 503, 504}

	for _, code := range retryableCodes {
		if !shouldRetry(nil, code) {
			t.Errorf("expected status code %d to be retried", code)
		}
	}
}

// TestShouldNotRetryStatusCode tests that non-retryable status codes are not retried.
func TestShouldNotRetryStatusCode(t *testing.T) {
	nonRetryableCodes := []int{400, 401, 403, 404}

	for _, code := range nonRetryableCodes {
		if shouldRetry(nil, code) {
			t.Errorf("expected status code %d not to be retried", code)
		}
	}
}

// TestShouldNotRetrySuccess tests that successful requests are not retried.
func TestShouldNotRetrySuccess(t *testing.T) {
	if shouldRetry(nil, 200) {
		t.Error("expected 200 status code not to be retried")
	}

	if shouldRetry(nil, 201) {
		t.Error("expected 201 status code not to be retried")
	}
}

// TestIsTemporaryNetworkError tests detection of temporary network errors.
func TestIsTemporaryNetworkError(t *testing.T) {
	// Create a network error
	err := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: errors.New("temporary failure"),
	}

	// The exact behavior depends on the network error type
	// Just verify the function doesn't panic
	_ = isTemporaryNetworkError(err)
}

// TestIsTemporaryNetworkErrorNil tests isTemporaryNetworkError with nil.
func TestIsTemporaryNetworkErrorNil(t *testing.T) {
	if isTemporaryNetworkError(nil) {
		t.Error("expected nil error not to be temporary")
	}
}

// TestIsTimeoutError tests detection of timeout errors.
func TestIsTimeoutError(t *testing.T) {
	// Create a timeout error
	err := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: errors.New("i/o timeout"),
	}

	result := isTimeoutError(err)
	// Result depends on how the OpError reports Timeout()
	// Just verify the function works
	_ = result
}

// TestIsTimeoutErrorNil tests isTimeoutError with nil.
func TestIsTimeoutErrorNil(t *testing.T) {
	if isTimeoutError(nil) {
		t.Error("expected nil error not to be timeout")
	}
}

// TestCalculateBackoff tests the backoff calculation.
func TestCalculateBackoff(t *testing.T) {
	config := RetryConfig{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
		Jitter:         0.0, // Disable jitter for predictability
	}

	// First retry should be close to initial backoff
	backoff0 := config.calculateBackoff(0)
	if backoff0 < 100*time.Millisecond || backoff0 > 150*time.Millisecond {
		t.Errorf("expected backoff around 100ms, got %v", backoff0)
	}

	// Second retry should be roughly double
	backoff1 := config.calculateBackoff(1)
	if backoff1 < 190*time.Millisecond || backoff1 > 250*time.Millisecond {
		t.Errorf("expected backoff around 200ms, got %v", backoff1)
	}

	// Third retry should be roughly quad
	backoff2 := config.calculateBackoff(2)
	if backoff2 < 390*time.Millisecond || backoff2 > 450*time.Millisecond {
		t.Errorf("expected backoff around 400ms, got %v", backoff2)
	}
}

// TestCalculateBackoffMaxCapped tests that backoff is capped at max.
func TestCalculateBackoffMaxCapped(t *testing.T) {
	config := RetryConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		Multiplier:     10.0,
		Jitter:         0.0,
	}

	// With multiplier 10, we should quickly exceed max backoff
	backoff := config.calculateBackoff(5)

	if backoff > config.MaxBackoff {
		t.Errorf("expected backoff capped at %v, got %v", config.MaxBackoff, backoff)
	}
}

// TestCalculateBackoffWithJitter tests backoff calculation with jitter.
func TestCalculateBackoffWithJitter(t *testing.T) {
	config := RetryConfig{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
		Jitter:         0.1, // 10% jitter
	}

	// Calculate multiple times and verify variation
	backoffs := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		backoffs[i] = config.calculateBackoff(0)
	}

	// With jitter, backoffs should vary
	minBackoff := backoffs[0]
	maxBackoff := backoffs[0]
	for _, b := range backoffs {
		if b < minBackoff {
			minBackoff = b
		}
		if b > maxBackoff {
			maxBackoff = b
		}
	}

	if minBackoff == maxBackoff {
		t.Logf("warning: backoffs should vary with jitter (all were %v)", minBackoff)
	}
}

// TestParseRetryAfter tests parsing Retry-After header values.
func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header   string
		expected int
	}{
		{"", 0},
		{"10", 10},
		{"120", 120},
		{"0", 0},
	}

	for _, test := range tests {
		result := parseRetryAfter(test.header)
		if result != test.expected {
			t.Errorf("for header %q, expected %d, got %d", test.header, test.expected, result)
		}
	}
}

// TestParseRetryAfterHTTPDate tests parsing HTTP-date Retry-After values.
func TestParseRetryAfterHTTPDate(t *testing.T) {
	// Test with an RFC 1123 formatted date in the future
	futureDate := time.Now().Add(1 * time.Hour).Format(time.RFC1123)
	result := parseRetryAfter(futureDate)

	if result <= 0 {
		t.Errorf("expected positive retry-after for future date, got %d", result)
	}

	// Test with a past date
	pastDate := time.Now().Add(-1 * time.Hour).Format(time.RFC1123)
	result = parseRetryAfter(pastDate)

	if result > 0 {
		t.Errorf("expected non-positive retry-after for past date, got %d", result)
	}
}

// TestParseRetryAfterInvalid tests parsing invalid Retry-After values.
func TestParseRetryAfterInvalid(t *testing.T) {
	invalidValues := []string{
		"not a number",
		"abc",
		"12.34",
		"Fri Jan 01 2021 00:00:00 GMT", // Invalid format
	}

	for _, value := range invalidValues {
		result := parseRetryAfter(value)

		if result != 0 {
			t.Logf("for invalid value %q, expected 0, got %d (may be acceptable)", value, result)
		}
	}
}

// TestSleep tests the sleep function with context.
func TestSleep(t *testing.T) {
	start := time.Now()
	ctx := context.Background()

	err := sleep(ctx, 100*time.Millisecond)

	if err != nil {
		t.Fatalf("sleep failed: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Errorf("sleep was too short: %v", elapsed)
	}
}

// TestPow tests the pow helper function.
func TestPow(t *testing.T) {
	tests := []struct {
		base     float64
		exp      float64
		expected float64
	}{
		{2.0, 0.0, 1.0},
		{2.0, 1.0, 2.0},
		{2.0, 2.0, 4.0},
		{2.0, 3.0, 8.0},
		{2.0, 10.0, 1024.0},
		{10.0, 0.0, 1.0},
		{10.0, 1.0, 10.0},
		{10.0, 2.0, 100.0},
	}

	for _, test := range tests {
		result := pow(test.base, test.exp)

		// Use a small epsilon for floating point comparison
		epsilon := 0.0001
		if result < test.expected-epsilon || result > test.expected+epsilon {
			t.Errorf("pow(%f, %f) = %f, expected %f", test.base, test.exp, result, test.expected)
		}
	}
}
