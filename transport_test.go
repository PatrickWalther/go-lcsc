package lcsc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordRetrySleeps replaces the retry wait with a function that records
// the delay and does not wait. The cleanup restores the real wait.
func recordRetrySleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var delays []time.Duration
	original := retrySleep
	retrySleep = func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		return ctx.Err()
	}
	t.Cleanup(func() { retrySleep = original })
	return &delays
}

// newRetryTestClient returns a client with three retries, a short fixed
// backoff and no cache.
func newRetryTestClient(fn roundTripFunc) *Client {
	return NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(fn)),
		WithRateLimit(1000),
		WithoutCache(),
		WithRetryConfig(RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     30 * time.Second,
			Multiplier:     1,
			Jitter:         0,
		}),
	)
}

func responseWithHeader(status int, body, name, value string) *http.Response {
	resp := jsonResponse(status, body)
	resp.Header.Set(name, value)
	return resp
}

const okDetailBody = `{"code":200,"msg":null,"result":{"productCode":"C1525"}}`

func TestEnvelope405MapsToInvalidRequest(t *testing.T) {
	// LCSC sends code 405 with HTTP status 200 for an invalid request
	// body. Both messages come from live responses.
	messages := []string{"Invalid field. Please check again.", "Product search error."}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			recordRetrySleeps(t)
			var calls int32
			client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return jsonResponse(http.StatusOK, `{"code":405,"msg":"`+msg+`","result":null,"ok":false}`), nil
			})
			defer func() { _ = client.Close() }()

			_, err := client.Product.Details(context.Background(), "C1525")
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != 405 || apiErr.StatusCode != http.StatusOK || apiErr.Message != msg {
				t.Fatalf("unexpected API error: %#v", apiErr)
			}
			if !strings.Contains(err.Error(), msg) {
				t.Fatalf("expected the message in the error text, got %q", err.Error())
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Fatalf("expected no retry for code 405, got %d requests", got)
			}
		})
	}
}

func TestEnvelopeServerErrorIsRetriedOnce(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":500,"msg":"system error","result":null,"ok":false}`), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Product.Details(context.Background(), "C1525")
	if !errors.Is(err, ErrServer) {
		t.Fatalf("expected ErrServer, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 requests for an envelope 500, got %d", got)
	}
	if len(*delays) != 1 {
		t.Fatalf("expected 1 wait, got %v", *delays)
	}
}

func TestEnvelopeServerErrorRetrySucceeds(t *testing.T) {
	recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return jsonResponse(http.StatusOK, `{"code":503,"msg":"busy","result":null,"ok":false}`), nil
		}
		return jsonResponse(http.StatusOK, okDetailBody), nil
	})
	defer func() { _ = client.Close() }()

	p, err := client.Product.Details(context.Background(), "C1525")
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if p.ProductCode != "C1525" {
		t.Fatalf("unexpected product: %+v", p)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
	}
}

func TestHTTPServerErrorUsesAllRetries(t *testing.T) {
	recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusBadGateway, "bad gateway"), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Product.Details(context.Background(), "C1525")
	if !errors.Is(err, ErrServer) {
		t.Fatalf("expected ErrServer, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("expected 4 requests for HTTP 502, got %d", got)
	}
}

func TestEnvelopeRateLimitUsesAllRetries(t *testing.T) {
	recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":429,"msg":"too many requests","result":null,"ok":false}`), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Product.Details(context.Background(), "C1525")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("expected 4 requests for an envelope 429, got %d", got)
	}
}

func TestRetryAfterSecondsSetsRetryDelay(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return responseWithHeader(http.StatusTooManyRequests, "slow down", "Retry-After", "2"), nil
		}
		return jsonResponse(http.StatusOK, okDetailBody), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Product.Details(context.Background(), "C1525"); err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if len(*delays) != 1 || (*delays)[0] != 2*time.Second {
		t.Fatalf("expected one wait of 2s, got %v", *delays)
	}
}

func TestRetryAfterHTTPDateOnEnvelopeRateLimit(t *testing.T) {
	delays := recordRetrySleeps(t)
	retryAt := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			body := `{"code":429,"msg":"too many requests","result":null,"ok":false}`
			return responseWithHeader(http.StatusOK, body, "Retry-After", retryAt), nil
		}
		return jsonResponse(http.StatusOK, okDetailBody), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Product.Details(context.Background(), "C1525"); err != nil {
		t.Fatalf("details failed: %v", err)
	}
	// An HTTP date has a resolution of one second.
	if len(*delays) != 1 || (*delays)[0] < 4*time.Second || (*delays)[0] > 5*time.Second {
		t.Fatalf("expected one wait of 4s to 5s, got %v", *delays)
	}
}

func TestRetryAfterLongerThanMaxBackoffStops(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return responseWithHeader(http.StatusTooManyRequests, "slow down", "Retry-After", "120"), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Product.Details(context.Background(), "C1525")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != 120*time.Second {
		t.Fatalf("expected RetryAfter 120s, got %#v", apiErr)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if len(*delays) != 0 {
		t.Fatalf("expected no wait, got %v", *delays)
	}
}

func TestRetryStopsWhenContextEndsBeforeRetryAfter(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return responseWithHeader(http.StatusTooManyRequests, "slow down", "Retry-After", "10"), nil
	})
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.Product.Details(ctx, "C1525")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited and not a context error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if len(*delays) != 0 {
		t.Fatalf("expected no wait, got %v", *delays)
	}
}

func TestRetryStopsWhenContextEndsBeforeBackoff(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return jsonResponse(http.StatusServiceUnavailable, "busy"), nil
		})),
		WithRateLimit(1000),
		WithoutCache(),
		WithRetryConfig(RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 2 * time.Second,
			MaxBackoff:     30 * time.Second,
			Multiplier:     1,
		}),
	)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// The backoff without Retry-After also ends after the deadline. The
	// client returns the API error and does not wait.
	_, err := client.Product.Details(ctx, "C1525")
	if !errors.Is(err, ErrServer) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected ErrServer and not a context error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if len(*delays) != 0 {
		t.Fatalf("expected no wait, got %v", *delays)
	}
}

func TestRetryAfterOnHTTPServerError(t *testing.T) {
	delays := recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return responseWithHeader(http.StatusServiceUnavailable, "busy", "Retry-After", "2"), nil
		}
		return jsonResponse(http.StatusOK, okDetailBody), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Product.Details(context.Background(), "C1525"); err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if want := []time.Duration{2 * time.Second}; len(*delays) != 1 || (*delays)[0] != want[0] {
		t.Fatalf("expected delays %v, got %v", want, *delays)
	}
}

func TestRetryDelay(t *testing.T) {
	config := RetryConfig{
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     1,
	}
	tests := []struct {
		name   string
		err    error
		want   time.Duration
		wantOK bool
	}{
		{"no API error", errors.New("network"), 5 * time.Second, true},
		{"no Retry-After", &APIError{Code: 429}, 5 * time.Second, true},
		{"Retry-After shorter than backoff", &APIError{Code: 429, RetryAfter: time.Second}, 5 * time.Second, true},
		{"Retry-After longer than backoff", &APIError{Code: 429, RetryAfter: 12 * time.Second}, 12 * time.Second, true},
		{"Retry-After longer than max backoff", &APIError{Code: 429, RetryAfter: time.Minute}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := config.retryDelay(0, tt.err)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("expected %v (%v), got %v (%v)", tt.want, tt.wantOK, got, ok)
			}
		})
	}
}

func TestIsEnvelopeServerError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		want       bool
	}{
		{"envelope 500", &APIError{StatusCode: 200, Code: 500}, 200, true},
		{"envelope 429", &APIError{StatusCode: 200, Code: 429}, 200, false},
		{"HTTP 500", &APIError{StatusCode: 500, Code: 500}, 500, false},
		{"other error", errors.New("network"), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEnvelopeServerError(tt.err, tt.statusCode); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
