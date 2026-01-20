package lcsc

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestNewClient tests client creation with default options.
func TestNewClient(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.baseURL != defaultBaseURL {
		t.Errorf("expected base URL %s, got %s", defaultBaseURL, client.baseURL)
	}

	if client.currency != defaultCurrency {
		t.Errorf("expected currency %s, got %s", defaultCurrency, client.currency)
	}

	if client.httpClient == nil {
		t.Fatal("expected non-nil HTTP client")
	}

	if client.rateLimiter == nil {
		t.Fatal("expected non-nil rate limiter")
	}
}

// TestNewClientWithCurrency tests client creation with custom currency.
func TestNewClientWithCurrency(t *testing.T) {
	client := NewClient(WithCurrency("EUR"))

	if client.currency != "EUR" {
		t.Errorf("expected currency EUR, got %s", client.currency)
	}
}

// TestNewClientWithCache tests client creation with custom cache.
func TestNewClientWithCache(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	client := NewClient(WithCache(cache))

	if client.cache == nil {
		t.Fatal("expected non-nil cache")
	}

	if client.cache != cache {
		t.Error("expected same cache instance")
	}
}

// TestNewClientWithRateLimit tests client creation with custom rate limit.
func TestNewClientWithRateLimit(t *testing.T) {
	client := NewClient(WithRateLimit(2.0))

	if client.rateLimiter == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	// Rate limiter is initialized but we can't directly check the RPS value
	// It will be tested through actual rate limiting behavior
}

// TestNewClientWithHTTPClient tests client creation with custom HTTP client.
func TestNewClientWithHTTPClient(t *testing.T) {
	customHTTPClient := &http.Client{
		Timeout: 60 * time.Second,
	}

	client := NewClient(WithHTTPClient(customHTTPClient))

	if client.httpClient != customHTTPClient {
		t.Error("expected same HTTP client instance")
	}
}

// TestNewClientWithRetryConfig tests client creation with custom retry config.
func TestNewClientWithRetryConfig(t *testing.T) {
	config := RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 100 * time.Millisecond,
	}

	client := NewClient(WithRetryConfig(config))

	if client.retryConfig.MaxRetries != 5 {
		t.Errorf("expected max retries 5, got %d", client.retryConfig.MaxRetries)
	}
}

// TestNewClientWithMultipleOptions tests client creation with multiple options.
func TestNewClientWithMultipleOptions(t *testing.T) {
	cache := NewMemoryCache(5 * time.Minute)
	client := NewClient(
		WithCurrency("GBP"),
		WithCache(cache),
		WithRateLimit(3.0),
		WithRetryConfig(RetryConfig{MaxRetries: 2}),
	)

	if client.currency != "GBP" {
		t.Errorf("expected currency GBP, got %s", client.currency)
	}

	if client.cache != cache {
		t.Error("expected same cache instance")
	}

	if client.retryConfig.MaxRetries != 2 {
		t.Errorf("expected max retries 2, got %d", client.retryConfig.MaxRetries)
	}
}

// TestBuildCacheKey tests cache key generation.
func TestBuildCacheKey(t *testing.T) {
	client := NewClient(WithCurrency("USD"))

	params := make(map[string][]string)
	params["productCode"] = []string{"C8734"}

	key := client.buildCacheKey("GET", "/product/detail", params)

	if key == "" {
		t.Fatal("expected non-empty cache key")
	}

	if !contains(key, "GET") {
		t.Error("expected cache key to contain method")
	}

	if !contains(key, "USD") {
		t.Error("expected cache key to contain currency")
	}

	if !contains(key, "/product/detail") {
		t.Error("expected cache key to contain path")
	}
}

// TestBuildCacheKeyDifferentMethods produces different keys for different methods.
func TestBuildCacheKeyDifferentMethods(t *testing.T) {
	client := NewClient()

	key1 := client.buildCacheKey("GET", "/path", nil)
	key2 := client.buildCacheKey("POST", "/path", nil)

	if key1 == key2 {
		t.Error("expected different cache keys for different methods")
	}
}

// TestBuildCacheKeyWithParams produces different keys for different parameters.
func TestBuildCacheKeyWithParams(t *testing.T) {
	client := NewClient()

	params1 := make(map[string][]string)
	params1["productCode"] = []string{"C8734"}

	params2 := make(map[string][]string)
	params2["productCode"] = []string{"C5555"}

	key1 := client.buildCacheKey("GET", "/product/detail", params1)
	key2 := client.buildCacheKey("GET", "/product/detail", params2)

	if key1 == key2 {
		t.Error("expected different cache keys for different parameters")
	}
}

// TestBuildCacheKeyNilParams produces a valid key with nil params.
func TestBuildCacheKeyNilParams(t *testing.T) {
	client := NewClient()

	key := client.buildCacheKey("GET", "/path", nil)

	if key == "" {
		t.Fatal("expected non-empty cache key")
	}

	if !contains(key, "GET") || !contains(key, "/path") {
		t.Error("cache key missing expected components")
	}
}

// TestParseResponseValidSuccess tests parsing a valid success response.
func TestParseResponseValidSuccess(t *testing.T) {
	client := NewClient()

	// Sample valid API response for a simple product
	body := []byte(`{
		"code": 200,
		"msg": "success",
		"result": {
			"productCode": "C8734",
			"productModel": "LM7805",
			"brandNameEn": "STMicroelectronics"
		}
	}`)

	var product Product
	err := client.parseResponse(body, &product)

	if err != nil {
		t.Fatalf("parseResponse failed: %v", err)
	}

	if product.ProductCode != "C8734" {
		t.Errorf("expected product code C8734, got %s", product.ProductCode)
	}

	if product.ProductModel != "LM7805" {
		t.Errorf("expected product model LM7805, got %s", product.ProductModel)
	}
}

// TestParseResponseInvalidJSON tests handling of invalid JSON.
func TestParseResponseInvalidJSON(t *testing.T) {
	client := NewClient()

	body := []byte(`{invalid json`)

	var product Product
	err := client.parseResponse(body, &product)

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestParseResponseErrorCode tests handling of error response codes.
func TestParseResponseErrorCode(t *testing.T) {
	client := NewClient()

	body := []byte(`{
		"code": 404,
		"msg": "not found",
		"result": null
	}`)

	var product Product
	err := client.parseResponse(body, &product)

	if err == nil {
		t.Fatal("expected error for 404 code")
	}

	if err != ErrProductNotFound {
		t.Errorf("expected ErrProductNotFound, got %v", err)
	}
}

// TestParseResponseRateLimit tests handling of rate limit response.
func TestParseResponseRateLimit(t *testing.T) {
	client := NewClient()

	body := []byte(`{
		"code": 429,
		"msg": "rate limited",
		"result": null
	}`)

	var product Product
	err := client.parseResponse(body, &product)

	if err != ErrRateLimited {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

// TestContextCancellation tests that context cancellation is respected.
func TestContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: "test",
	})

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	// Should be a context error
	if !contains(err.Error(), "context") {
		t.Logf("unexpected error for cancelled context: %v", err)
	}
}

// TestContextTimeout tests that context timeout is respected.
func TestContextTimeout(t *testing.T) {
	client := NewClient()

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: "test",
	})

	if err == nil {
		t.Fatal("expected error for timeout context")
	}

	// Should be a timeout error
	if !contains(err.Error(), "context deadline") && !contains(err.Error(), "timeout") {
		t.Logf("expected timeout-related error, got: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substring string) bool {
	for i := 0; i <= len(s)-len(substring); i++ {
		if s[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
