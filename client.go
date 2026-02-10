package lcsc

import (
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://wmsc.lcsc.com/ftps/wm"
	defaultTimeout   = 30 * time.Second
	defaultRateLimit = 5.0
	defaultCurrency  = "USD"
	userAgent        = "go-lcsc/1.0"
)

type service struct {
	client *Client
}

// Client is an LCSC API client.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	currency    string
	rateLimiter *RateLimiter
	cache       Cache
	cacheConfig CacheConfig
	retryConfig RetryConfig

	common  service
	Search  *SearchService
	Product *ProductService
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		baseURL = strings.TrimSpace(baseURL)
		if baseURL != "" {
			c.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithCurrency sets the currency for price responses.
func WithCurrency(currency string) ClientOption {
	return func(c *Client) {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency != "" {
			c.currency = currency
		}
	}
}

// WithRateLimit sets a custom rate limit (requests per second).
func WithRateLimit(rps float64) ClientOption {
	return func(c *Client) {
		c.rateLimiter = NewRateLimiter(rps)
	}
}

// WithCache sets a custom cache implementation.
func WithCache(cache Cache) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

// WithCacheConfig sets the cache configuration.
func WithCacheConfig(config CacheConfig) ClientOption {
	return func(c *Client) {
		c.cacheConfig = config
	}
}

// WithoutCache disables response caching.
func WithoutCache() ClientOption {
	return func(c *Client) {
		c.cacheConfig.Enabled = false
		c.cache = nil
	}
}

// WithRetryConfig sets the retry configuration.
func WithRetryConfig(config RetryConfig) ClientOption {
	return func(c *Client) {
		c.retryConfig = config
	}
}

// WithoutRetry disables retries.
func WithoutRetry() ClientOption {
	return func(c *Client) {
		c.retryConfig = NoRetry()
	}
}

// NewClient creates a new LCSC API client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient:  &http.Client{Timeout: defaultTimeout},
		baseURL:     defaultBaseURL,
		currency:    defaultCurrency,
		rateLimiter: NewRateLimiter(defaultRateLimit),
		cacheConfig: DefaultCacheConfig(),
		retryConfig: DefaultRetryConfig(),
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.cacheConfig.Enabled && c.cache == nil {
		c.cache = NewMemoryCache(c.cacheConfig.DetailsTTL)
	}

	c.common.client = c
	c.Search = (*SearchService)(&c.common)
	c.Product = (*ProductService)(&c.common)
	return c
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	if mc, ok := c.cache.(*MemoryCache); ok {
		mc.Close()
	}
	return nil
}

// ClearCache clears all cached responses in the default memory cache.
func (c *Client) ClearCache() {
	if mc, ok := c.cache.(*MemoryCache); ok {
		mc.Clear()
	}
}
