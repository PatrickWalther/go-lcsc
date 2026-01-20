package lcsc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL   = "https://wmsc.lcsc.com/ftps/wm"
	defaultTimeout   = 30 * time.Second
	defaultRateLimit = 5.0 // requests per second
	defaultCurrency  = "USD"
	userAgent        = "go-lcsc/1.0"
)

// Client is an LCSC API client.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	currency    string
	rateLimiter *RateLimiter
	cache       Cache
	retryConfig RetryConfig
}

// ClientOption is a function that configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithCurrency sets the currency for price responses.
func WithCurrency(currency string) ClientOption {
	return func(c *Client) {
		c.currency = currency
	}
}

// WithRateLimit sets a custom rate limit (requests per second).
func WithRateLimit(rps float64) ClientOption {
	return func(c *Client) {
		c.rateLimiter = NewRateLimiter(rps)
	}
}

// WithCache sets a cache for API responses.
func WithCache(cache Cache) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

// WithRetryConfig sets the retry configuration.
func WithRetryConfig(config RetryConfig) ClientOption {
	return func(c *Client) {
		c.retryConfig = config
	}
}

// NewClient creates a new LCSC API client.
// The unofficial LCSC API requires no authentication.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		baseURL:     defaultBaseURL,
		currency:    defaultCurrency,
		rateLimiter: NewRateLimiter(defaultRateLimit),
		retryConfig: DefaultRetryConfig(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request to the LCSC API.
func (c *Client) doRequest(ctx context.Context, method, path string, params url.Values, body interface{}) ([]byte, error) {
	cacheKey := ""
	if method == http.MethodGet && c.cache != nil {
		cacheKey = c.buildCacheKey(method, path, params)
		if cached, ok := c.cache.Get(cacheKey); ok {
			return cached, nil
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			waitTime := c.retryConfig.calculateBackoff(attempt - 1)
			if err := sleep(ctx, waitTime); err != nil {
				return nil, err
			}
		}

		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		respBody, statusCode, err := c.executeRequest(ctx, method, path, params, body)
		if err != nil {
			lastErr = err
			if shouldRetry(err, statusCode) {
				continue
			}
			return nil, err
		}

		if cacheKey != "" && c.cache != nil {
			c.cache.Set(cacheKey, respBody, 5*time.Minute)
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// executeRequest performs a single HTTP request.
func (c *Client) executeRequest(ctx context.Context, method, path string, params url.Values, body interface{}) ([]byte, int, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL = fmt.Sprintf("%s?%s", reqURL, params.Encode())
	}

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", fmt.Sprintf("currencyCode=%s", c.currency))

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return respBody, resp.StatusCode, nil
}

// buildCacheKey creates a cache key from request parameters.
func (c *Client) buildCacheKey(method, path string, params url.Values) string {
	key := method + ":" + c.currency + ":" + path
	if params != nil {
		key += "?" + params.Encode()
	}
	return key
}

// parseResponse parses the API response and checks for errors.
func (c *Client) parseResponse(body []byte, result interface{}) error {
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Code != 200 {
		return errorFromCode(resp.Code, resp.Message)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to parse result: %w", err)
		}
	}

	return nil
}
