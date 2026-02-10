package lcsc

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClientDefaults(t *testing.T) {
	client := NewClient()
	defer func() { _ = client.Close() }()

	if client.baseURL != defaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", defaultBaseURL, client.baseURL)
	}
	if client.currency != defaultCurrency {
		t.Fatalf("expected currency %q, got %q", defaultCurrency, client.currency)
	}
	if client.httpClient == nil {
		t.Fatal("expected non-nil HTTP client")
	}
	if client.rateLimiter == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	if client.Search == nil {
		t.Fatal("expected Search service to be initialized")
	}
	if client.Product == nil {
		t.Fatal("expected Product service to be initialized")
	}
	if client.cache == nil {
		t.Fatal("expected default cache to be initialized")
	}
}

func TestNewClientOptions(t *testing.T) {
	customHTTP := &http.Client{Timeout: 60 * time.Second}
	customCache := NewMemoryCache(time.Minute)
	defer customCache.Close()

	client := NewClient(
		WithHTTPClient(customHTTP),
		WithBaseURL("https://example.com/api/"),
		WithCurrency("eur"),
		WithRateLimit(2.5),
		WithCache(customCache),
		WithCacheConfig(CacheConfig{
			Enabled:    true,
			SearchTTL:  time.Minute,
			DetailsTTL: 2 * time.Minute,
		}),
		WithRetryConfig(RetryConfig{
			MaxRetries:     7,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     time.Second,
			Multiplier:     1.5,
			Jitter:         0.0,
		}),
	)
	defer func() { _ = client.Close() }()

	if client.httpClient != customHTTP {
		t.Fatal("expected custom HTTP client")
	}
	if client.baseURL != "https://example.com/api" {
		t.Fatalf("expected trimmed base URL, got %q", client.baseURL)
	}
	if client.currency != "EUR" {
		t.Fatalf("expected uppercase currency, got %q", client.currency)
	}
	if client.cache != customCache {
		t.Fatal("expected custom cache")
	}
	if client.retryConfig.MaxRetries != 7 {
		t.Fatalf("expected retry max retries 7, got %d", client.retryConfig.MaxRetries)
	}
}

func TestWithoutCache(t *testing.T) {
	client := NewClient(WithoutCache())
	defer func() { _ = client.Close() }()

	if client.cacheConfig.Enabled {
		t.Fatal("expected cache to be disabled")
	}
	if client.cache != nil {
		t.Fatal("expected nil cache when disabled")
	}
}

func TestWithoutRetry(t *testing.T) {
	client := NewClient(WithoutRetry())
	defer func() { _ = client.Close() }()

	if client.retryConfig.MaxRetries != 0 {
		t.Fatalf("expected retries disabled, got %d", client.retryConfig.MaxRetries)
	}
}

func TestClearCache(t *testing.T) {
	cache := NewMemoryCache(time.Minute)
	client := NewClient(WithCache(cache))
	defer func() { _ = client.Close() }()

	cache.Set("k1", []byte("v1"), time.Minute)
	cache.Set("k2", []byte("v2"), time.Minute)
	if cache.Size() != 2 {
		t.Fatalf("expected 2 cache entries, got %d", cache.Size())
	}

	client.ClearCache()
	if cache.Size() != 0 {
		t.Fatalf("expected cache cleared, got %d entries", cache.Size())
	}
}

func TestWithCurrencyIgnoresBlankValue(t *testing.T) {
	client := NewClient(WithCurrency("   "))
	defer func() { _ = client.Close() }()
	if client.currency != defaultCurrency {
		t.Fatalf("expected default currency %q, got %q", defaultCurrency, client.currency)
	}
}
