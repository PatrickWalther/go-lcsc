# go-lcsc

[![Go Reference](https://pkg.go.dev/badge/github.com/PatrickWalther/go-lcsc.svg)](https://pkg.go.dev/github.com/PatrickWalther/go-lcsc)
[![Go Report Card](https://goreportcard.com/badge/github.com/PatrickWalther/go-lcsc)](https://goreportcard.com/report/github.com/PatrickWalther/go-lcsc)
[![Tests](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml/badge.svg)](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Unofficial Go client for [LCSC](https://www.lcsc.com) component search and product details.

LCSC does not provide a documented public API for this data. This library uses undocumented endpoints that can change without notice.

## Requirements

- Go 1.22+
- No external dependencies (stdlib only)

## Installation

```bash
go get github.com/PatrickWalther/go-lcsc
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/PatrickWalther/go-lcsc"
)

func main() {
	client := lcsc.NewClient(
		lcsc.WithCurrency("USD"),
		lcsc.WithRateLimit(5),
	)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	search, err := client.Search.Keyword(ctx, &lcsc.SearchRequest{
		Keyword: "STM32F103",
	})
	if err != nil {
		log.Fatal(err)
	}

	if len(search.Products) == 0 {
		log.Fatal("no products found")
	}

	product, err := client.Product.Details(ctx, search.Products[0].ProductCode)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s - %s\n", product.ProductCode, product.ProductModel)
	fmt.Printf("Stock: %d\n", product.StockNumber)
}
```

## Features

- Service-based API: `client.Search` and `client.Product`
- Automatic retries with exponential backoff for transient failures
- Token-bucket request rate limiting
- Optional in-memory response caching with configurable TTL
- Typed errors with `errors.Is`/`errors.As` support
- Thread-safe client for concurrent use

## API

### Search Service

```go
resp, err := client.Search.Keyword(ctx, &lcsc.SearchRequest{
	Keyword: "CGJ2B2C0G1H390J050BA",
})

for _, p := range resp.Products {
	fmt.Println(p.ProductCode, p.ProductModel)
}

if resp.DirectMatchCode != "" {
	fmt.Println("direct match:", resp.DirectMatchCode)
}
```

### Product Service

```go
product, err := client.Product.Details(ctx, "C8734")
if err != nil {
	// handle error
}

fmt.Println(product.ProductCode)
fmt.Println(product.ProductModel)
fmt.Println(product.BrandNameEn)
fmt.Println(product.PdfURL)
fmt.Println(product.GetProductURL())
```

## Configuration Options

```go
client := lcsc.NewClient(
	lcsc.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
	lcsc.WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
	lcsc.WithCurrency("EUR"),
	lcsc.WithRateLimit(10),
	lcsc.WithCache(lcsc.NewMemoryCache(10*time.Minute)),
	lcsc.WithCacheConfig(lcsc.CacheConfig{
		Enabled:    true,
		SearchTTL:  2 * time.Minute,
		DetailsTTL: 5 * time.Minute,
	}),
	lcsc.WithRetryConfig(lcsc.RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 300 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
		Jitter:         0.1,
	}),
)
defer client.Close()
```

### Cache Controls

```go
client := lcsc.NewClient()
defer client.Close()

client.ClearCache()

clientNoCache := lcsc.NewClient(lcsc.WithoutCache())
defer clientNoCache.Close()
```

### Retry Controls

```go
client := lcsc.NewClient(lcsc.WithoutRetry())
defer client.Close()
```

## Error Handling

```go
import "errors"

_, err := client.Product.Details(ctx, "C99999999")
if err != nil {
	if errors.Is(err, lcsc.ErrInvalidRequest) {
		// bad input
	}
	if errors.Is(err, lcsc.ErrNotFound) {
		// no component found
	}
	if errors.Is(err, lcsc.ErrRateLimited) {
		// upstream rate limited
	}
	if errors.Is(err, lcsc.ErrServer) {
		// upstream server failure
	}

	var apiErr *lcsc.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message)
	}
}
```

## Breaking Changes In v1.0.0

- Removed flat client methods:
  - `client.KeywordSearch(...)`
  - `client.GetProductDetails(...)`
- Replaced with service methods:
  - `client.Search.Keyword(...)`
  - `client.Product.Details(...)`
- Removed legacy search request fields that were ignored by the upstream endpoint.
- Standardized errors to:
  - `ErrInvalidRequest`
  - `ErrNotFound`
  - `ErrRateLimited`
  - `ErrServer`
- Product struct field names standardized to Go initialisms:
  - `PdfUrl` -> `PdfURL`
  - `ProductImageUrl` -> `ProductImageURL`

## Testing

### Unit tests

```bash
go test ./...
```

### Integration tests (real LCSC API)

```bash
go test -tags=integration -run Integration ./...
```

## License

MIT - see [LICENSE](LICENSE).
