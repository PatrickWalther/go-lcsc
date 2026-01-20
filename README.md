# go-lcsc

[![Go Reference](https://pkg.go.dev/badge/github.com/PatrickWalther/go-lcsc.svg)](https://pkg.go.dev/github.com/PatrickWalther/go-lcsc)
[![Go Report Card](https://goreportcard.com/badge/github.com/PatrickWalther/go-lcsc)](https://goreportcard.com/report/github.com/PatrickWalther/go-lcsc)
[![Tests](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml/badge.svg)](https://github.com/PatrickWalther/go-lcsc/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Coverage](https://img.shields.io/badge/coverage-88.8%25-brightgreen)](https://github.com/PatrickWalther/go-lcsc)

A Go client library for [LCSC](https://lcsc.com) electronics components.

> **Note**: LCSC does not have an official public API. This library uses undocumented
> endpoints that work without authentication. These endpoints may change at any time
> without notice. The approach is based on the [Part-DB](https://github.com/Part-DB/Part-DB-server)
> implementation.

## Requirements

- **Go 1.22+** (tested on Go 1.22 and 1.23)
- No external dependencies

## Installation

```bash
go get github.com/PatrickWalther/go-lcsc
```

## Quick Start

```bash
# Initialize a new Go module (if needed)
go mod init example.com/myapp

# Get the library
go get github.com/PatrickWalther/go-lcsc

# Run tests to verify installation
go test github.com/PatrickWalther/go-lcsc/...
```

## Usage

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
    // Create a new client (no authentication required)
    client := lcsc.NewClient()

    // Or with options
    client = lcsc.NewClient(
        lcsc.WithCurrency("USD"),
        lcsc.WithCache(lcsc.NewMemoryCache(10 * time.Minute)),
        lcsc.WithRateLimit(5.0), // requests per second
    )

    ctx := context.Background()

    // Search for products
    results, err := client.KeywordSearch(ctx, lcsc.SearchRequest{
        Keyword:  "STM32F103",
        PageSize: 10,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, product := range results.Products {
        fmt.Printf("%s - %s: %s\n", 
            product.ProductCode, 
            product.ProductModel,
            product.ProductIntroEn)
    }

    // Get product details
    product, err := client.GetProductDetails(ctx, "C8734")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Product: %s by %s\n", product.ProductModel, product.BrandNameEn)
    fmt.Printf("Stock: %d, Min Order: %d\n", product.StockNumber, product.MinPacketNumber)
    fmt.Printf("URL: %s\n", product.GetProductURL())

    // Access specifications
    for _, param := range product.ParamVOList {
        fmt.Printf("  %s: %s\n", param.ParamNameEn, param.ParamValueEn)
    }
}
```

## Features

- **No authentication required** - Uses unofficial LCSC endpoints
- **Currency support** - Set currency via `WithCurrency()` (default: USD)
- **Rate limiting** - Built-in token bucket rate limiter
- **Caching** - Optional in-memory cache with TTL
- **Retry logic** - Exponential backoff with jitter for transient errors
- **Product search** - Search by keyword with pagination
- **Product details** - Get full product info including specs and pricing

## Client Options

```go
// Custom HTTP client
client := lcsc.NewClient(
    lcsc.WithHTTPClient(&http.Client{Timeout: 60*time.Second}))

// Custom currency (affects pricing)
client := lcsc.NewClient(lcsc.WithCurrency("EUR"))

// Custom rate limit (requests per second)
client := lcsc.NewClient(lcsc.WithRateLimit(10.0))

// Enable caching
cache := lcsc.NewMemoryCache(10 * time.Minute)
client := lcsc.NewClient(lcsc.WithCache(cache))

// Custom retry configuration
client := lcsc.NewClient(lcsc.WithRetryConfig(lcsc.RetryConfig{
    MaxRetries:     5,
    InitialBackoff: 1 * time.Second,
    MaxBackoff:     60 * time.Second,
    Multiplier:     2.0,
    Jitter:         0.1,
}))

// Disable retries
client := lcsc.NewClient(lcsc.WithRetryConfig(lcsc.NoRetry()))
```

## API Methods

### Product Search

```go
results, err := client.KeywordSearch(ctx, lcsc.SearchRequest{
    Keyword:  "capacitor 100nF",
    PageSize: 30, // max 100
})

// Access results
for _, p := range results.Products {
    fmt.Println(p.ProductCode, p.ProductModel, p.BrandNameEn)
}

// Check for direct match (when searching by LCSC part number like "C12345")
if results.DirectMatchCode != "" {
    // LCSC found an exact match
}
```

### Product Details

```go
product, err := client.GetProductDetails(ctx, "C8734")

// Product info
fmt.Println(product.ProductCode)     // "C8734"
fmt.Println(product.ProductModel)    // MPN
fmt.Println(product.BrandNameEn)     // Manufacturer
fmt.Println(product.ProductIntroEn)  // Description
fmt.Println(product.StockNumber)     // Available stock
fmt.Println(product.PdfUrl)          // Datasheet URL
fmt.Println(product.EncapStandard)   // Package/footprint
fmt.Println(product.GetProductURL()) // LCSC product page

// Pricing
for _, pb := range product.ProductPriceList {
    fmt.Printf("Qty %d+: %s%.4f\n", pb.Ladder, pb.CurrencySymbol, pb.ProductPrice)
}

// Specifications
for _, param := range product.ParamVOList {
    fmt.Printf("%s: %s\n", param.ParamNameEn, param.ParamValueEn)
}
```

## Data Types

### Product

| Field | Type | Description |
|-------|------|-------------|
| ProductCode | string | LCSC part number (e.g., "C12345") |
| ProductModel | string | Manufacturer part number |
| BrandNameEn | string | Manufacturer name |
| ProductIntroEn | string | Product description |
| PdfUrl | string | Datasheet URL |
| ProductImages | []string | Product image URLs |
| ProductImageUrl | string | Primary image URL |
| StockNumber | int | Available stock |
| MinPacketNumber | int | Minimum order quantity |
| ProductPriceList | []PriceBreak | Quantity price breaks |
| ParamVOList | []Parameter | Product specifications |
| EncapStandard | string | Package/footprint |
| ParentCatalogName | string | Parent category |
| CatalogName | string | Subcategory |
| Weight | float64 | Weight in grams |

### PriceBreak

| Field | Type | Description |
|-------|------|-------------|
| Ladder | int | Minimum quantity for this price |
| ProductPrice | float64 | Unit price |
| CurrencySymbol | string | Currency symbol (e.g., "US$") |

### Parameter

| Field | Type | Description |
|-------|------|-------------|
| ParamNameEn | string | Parameter name |
| ParamValueEn | string | Parameter value |

## Error Handling

```go
if errors.Is(err, lcsc.ErrProductNotFound) {
    // Product not found (404)
}
if errors.Is(err, lcsc.ErrRateLimited) {
    // Rate limited (429)
}
if errors.Is(err, lcsc.ErrServiceUnavailable) {
    // Service unavailable (503)
}
```

## Supported Currencies

Currency is set via the `WithCurrency()` option. Common values:
- `USD` - US Dollar (default)
- `EUR` - Euro
- `GBP` - British Pound
- `CNY` - Chinese Yuan
- `JPY` - Japanese Yen
- `AUD` - Australian Dollar
- `CAD` - Canadian Dollar

## Testing

This library includes comprehensive unit and integration tests:

```bash
# Run all tests (unit tests only, ~1.8s)
go test ./...

# Run with coverage report
go test ./... -cover

# Generate coverage HTML report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run integration tests (makes real API calls, ~1.8s)
go test -run Integration ./...

# Run specific test
go test -run TestKeywordSearchBasic ./...
```

**Test Coverage**: 88.8%
- Unit tests: Fast, no external dependencies
- Integration tests: Real API calls to LCSC, can be run separately

## Development

### Code Quality

```bash
# Run linter
golangci-lint run ./...

# Run type checker
go vet ./...

# Format code
go fmt ./...
```

### Project Structure

```
.
├── *.go              # Main library code
├── *_test.go         # Unit tests
├── *_integration_test.go  # Integration tests (real API calls)
├── examples/         # Example usage
├── .github/workflows/
│   ├── test.yml      # CI/CD: Unit tests on each push
│   └── release.yml   # Release workflow
├── go.mod            # Module definition
└── README.md         # Documentation
```

### Adding to Your Project

1. **Import the package**:
   ```go
   import "github.com/PatrickWalther/go-lcsc"
   ```

2. **Create a client**:
   ```go
   client := lcsc.NewClient()
   ```

3. **Use the API methods**:
   ```go
   results, err := client.KeywordSearch(ctx, lcsc.SearchRequest{
       Keyword: "STM32F103",
   })
   ```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure `go test ./...` and `golangci-lint run ./...` pass
5. Submit a pull request

## Acknowledgments

- [LCSC](https://lcsc.com) for providing the electronics components API
- [Part-DB](https://github.com/Part-DB/Part-DB-server) for the API endpoint discovery
