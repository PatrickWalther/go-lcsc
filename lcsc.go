// Package lcsc provides a Go client for the LCSC electronics component API.
//
// IMPORTANT: LCSC does not provide an official public API. This package uses
// undocumented endpoints scraped from the LCSC website. These endpoints may
// change at any time without notice, which could break this library.
//
// No authentication is required to use this API.
//
// # Usage
//
//	client := lcsc.NewClient()  // No auth needed
//
//	// Or with options:
//	client := lcsc.NewClient(
//	    lcsc.WithCurrency("USD"),
//	    lcsc.WithCache(lcsc.NewMemoryCache(10 * time.Minute)),
//	)
//
//	// Search for products
//	results, err := client.KeywordSearch(ctx, lcsc.SearchRequest{
//	    Keyword:  "STM32F103",
//	    PageSize: 10,
//	})
//
//	// Get product details
//	product, err := client.GetProductDetails(ctx, "C8734")
//
// # Configuration Options
//
//	client := lcsc.NewClient(
//	    lcsc.WithCurrency("EUR"),           // Set currency (default: USD)
//	    lcsc.WithCache(lcsc.NewMemoryCache(5*time.Minute)),
//	    lcsc.WithRateLimit(2.0),            // 2 requests per second
//	    lcsc.WithRetryConfig(lcsc.DefaultRetryConfig()),
//	)
//
// # Rate Limiting
//
// The client includes built-in rate limiting (default: 5 requests/second).
// You can customize this with WithRateLimit option.
//
// # Caching
//
// Optional caching support via the Cache interface. Use NewMemoryCache for
// a simple in-memory cache with TTL support.
//
// # Retries
//
// Built-in retry logic with exponential backoff for transient failures.
//
// # Thread Safety
//
// The Client is safe for concurrent use by multiple goroutines.
//
// # Disclaimer
//
// This is an unofficial client. Use at your own risk. The maintainers are
// not affiliated with LCSC. Be respectful of LCSC's servers and implement
// appropriate rate limiting.
package lcsc

import "encoding/json"

// Version is the current version of the go-lcsc package.
const Version = "1.0.0"

// Ensure json is imported for models.go
var _ = json.RawMessage{}
