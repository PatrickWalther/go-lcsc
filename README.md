# go-lcsc

[![Go Reference](https://pkg.go.dev/badge/github.com/PatrickWalther/go-lcsc.svg)](https://pkg.go.dev/github.com/PatrickWalther/go-lcsc)
[![Go Report Card](https://goreportcard.com/badge/github.com/PatrickWalther/go-lcsc)](https://goreportcard.com/report/github.com/PatrickWalther/go-lcsc)
[![Tests](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml/badge.svg)](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Unofficial Go client for [LCSC](https://www.lcsc.com) component search and product details.

LCSC does not provide a documented public API for this data. This library uses undocumented endpoints that can change without notice.

## Requirements

- Go 1.23+
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

if resp.ParametricQuery {
	fmt.Println("LCSC cannot list parts for a parameter query")
}
```

`Keyword` sends the keyword to `/search/v3/global`. It uses the first source that has products:

1. The product list of the v3 response.
2. The exact match list of the v3 response (`exactMatchResult`). LCSC fills this list for model keywords such as `RP2040`. `TotalCount` is the length of this list.
3. The `/product/query/list` endpoint. The client uses this fallback only in these cases:
   - LCSC classifies the keyword as a product model (`PRODUCT_MODEL`).
   - LCSC gives a direct match code.
   - The v3 response has no classification.

The fallback endpoint can return popular parts that do not match the keyword. The client keeps a fallback row only when one of these conditions is true:

- The product model contains the keyword.
- The product code is equal to the keyword.
- The product code is equal to the direct match code.

The comparison ignores case, white space, dashes and dots. When the keyword is the direct match code (for example `C2040`), the client keeps only the row with that code. When the client drops rows, `TotalCount` is the number of rows that it keeps.

LCSC can classify a keyword as a parameter or package query, for example `100nF 0402`. Then the keyword endpoints cannot give a product list. For this case, the client does not send the fallback request. It returns an empty `Products` list, sets `ParametricQuery` to `true`, and returns no error.

For other classifications without a product list, for example a brand name (`BRAND`), the client also does not send the fallback request. It returns an empty `Products` list and no error. `ParametricQuery` is `false`.

`QueryTypes` holds the raw classification from LCSC, for example `["STANDARD", "PRODUCT_PARAM"]` or `["PRODUCT_MODEL"]`.

`TotalCount` and `ActualTotalCount` give the number of matching products:

- For the fallback list, `TotalCount` is the `totalRow` value of `/product/query/list`. LCSC caps this value at 5000. `ActualTotalCount` is the `actualTotalRow` value, which LCSC does not cap.
- For the v3 product list and the exact match list, both values are equal.
- When the client drops fallback rows, both values are the number of rows that the client keeps.

### Product Service

```go
product, err := client.Product.Details(ctx, "C1525")
if err != nil {
	// handle error
}

fmt.Println(product.ProductCode)
fmt.Println(product.ProductModel)
fmt.Println(product.BrandNameEn)
fmt.Println(product.PdfURL)
fmt.Println(product.GetProductURL())

// Order limits and lifecycle.
fmt.Println(product.MinBuyNumber)      // minimum order quantity
fmt.Println(product.Split)             // order multiple
fmt.Println(product.ProductCycle)      // for example "normal"
fmt.Println(product.Lifecycle())       // for example lcsc.LifecycleActive
fmt.Println(product.AllowsBackorder()) // false when LCSC sells only the stock
fmt.Println(product.IsPreSale)

// Ids. ProductID is equal to the JLCPCB lcscComponentId.
fmt.Println(product.ProductID, product.BrandID, product.WmCatalogID)

// Prices in the response currency.
for _, pb := range product.ProductPriceList {
	fmt.Println(pb.Ladder, pb.Price(), product.Currency())
}

// Alternates that LCSC selects (up to five).
for _, alt := range product.AlternatePartList {
	fmt.Println(alt.ProductCode, alt.ProductModel, alt.MatchType)
}

// Parameters.
for _, param := range product.ParamVOList {
	fmt.Println(param.ParamCode, param.ParamNameEn, param.ParamValueEn, param.IsMain)
	if v := param.ParamValueEnForSearch; v != nil && *v != -1 {
		fmt.Println("numeric value:", *v)
	}
}
```

Product fields:

| Field | JSON | Description |
|---|---|---|
| `MinBuyNumber` | `minBuyNumber` | Minimum order quantity. |
| `MaxBuyNumber` | `maxBuyNumber` | Maximum order quantity. `-1` means no maximum. |
| `Split` | `split` | Order multiple. |
| `ProductCycle` | `productCycle` | Lifecycle status, for example `normal`, `sold_out` or `stop_product`. |
| `IsPreSale` | `isPreSale` | `true` when LCSC sells the product as a pre-sale item. |
| `ProductID` | `productId` | Numeric LCSC id (1877 for C1525). It is equal to the JLCPCB `lcscComponentId`. |
| `CurrencyType` | `currencyType` | Code of the response currency. Only `Details` fills it. |
| `BrandID` | `brandId` | LCSC id of the manufacturer. |
| `WmCatalogID` | `wmCatalogId` | Id of the leaf category in the LCSC category tree. |
| `ProductImageURLBig` | `productImageUrlBig` | 900x900 image. Detail responses do not send it. |
| `IsNotOverstock` | `isNotOverstock` | `true` when LCSC refuses an order quantity above `StockNumber`. |
| `IsForeignOnsale` | `isForeignOnsale` | `false` when LCSC does not sell the product to overseas customers. `nil` when the response does not send it. |
| `HasThirdPartyStock` | `hasThirdPartyStock` | `true` when marketplace offers exist. Only list rows send a correct value. |
| `HasAlternatePart` | `hasAlternatePart` | `true` when LCSC has cross-reference alternates. Only list rows send it. |
| `IsReel`, `ReelPrice` | `isReel`, `reelPrice` | Reel option and reel fee in the response currency. |
| `ProductArrange` | `productArrange` | Packaging, for example `Tape & Reel (TR)`. |
| `StockSz`, `StockJs`, `WmStockHk` | `stockSz`, `stockJs`, `wmStockHk` | Stock per warehouse. The sum is `StockNumber`. Detail responses send only `stockSz`. |
| `Eccn` | `eccn` | Export control classification number. |
| `FlashSale` | `flashSaleProductPO` | Time-limited third-party offer, or `nil`. |
| `AlternatePartList` | `alternatePartList` | Alternates that LCSC selects (up to five). Only `Details` fills this list. |
| `MatchType` | `matchType` | Match code of an alternate, for example `"1"`, `"4"`, `"5"` or `"6"`. LCSC does not document the codes. The type is `FlexString`, which accepts a JSON string, number or null. |

Product methods:

| Method | Description |
|---|---|
| `Currency()` | Code of the response currency: `CurrencyType`, else the code for the price symbol, else `USD`. |
| `Lifecycle()` | `LifecycleActive`, `LifecycleNotRecommended`, `LifecycleDiscontinued` or `LifecycleUnknown`. |
| `AllowsBackorder()` | `false` when `IsNotOverstock` is `true` or `IsForeignOnsale` is `false`. |

`Lifecycle` gives `LifecycleDiscontinued` for `stop_product`. It gives `LifecycleNotRecommended` for other cycles that are not `normal`. It gives `LifecycleActive` for `normal`, and also for an empty cycle when the record has other lifecycle fields. It gives `LifecycleUnknown` when the record has no lifecycle data.

### Prices and currency

LCSC supports four currencies (`lcsc.SupportedCurrencies`): USD (`$`), CNY (`￥`, U+FFE5), EUR (`€`) and HKD (`HK$`). The client sends the code of `WithCurrency` in the `currencyCode` cookie. For any other code, LCSC answers in USD.

Each `PriceBreak` has three prices:

| Field | JSON | Description |
|---|---|---|
| `ProductPrice` | `productPrice` | Unit price in USD for every currency. |
| `USDPrice` | `usdPrice` | Unit price in USD. |
| `CurrencyPrice` | `currencyPrice` | Unit price in the response currency. LCSC rounds it. Do not calculate it again. |

`PriceBreak.Price()` returns `CurrencyPrice` when it is more than zero, else `ProductPrice`. `Product.Currency()` gives the code of that currency.

```go
client := lcsc.NewClient(lcsc.WithCurrency("EUR"))
defer client.Close()

product, err := client.Product.Details(ctx, "C2040")
if err != nil {
	// handle error
}
for _, pb := range product.ProductPriceList {
	fmt.Printf("%d: %.4f %s (%.4f USD)\n", pb.Ladder, pb.Price(), product.Currency(), pb.USDPrice)
}
```

`FlashSale` has its own price and currency: `SellPrice` in `SellCurrencyType`, and `USDPrice`. Show `ValidNumber` as the quantity on offer. `DeliveryDays()` gives the minimum and the maximum delivery time.

Parameter fields:

| Field | JSON | Description |
|---|---|---|
| `ParamCode` | `paramCode` | LCSC identifier of the parameter, for example `param_10951_n`. |
| `ParamValueEnForSearch` | `paramValueEnForSearch` | Numeric value that LCSC uses for parametric search. It uses base units, but capacitance is in pF (100nF gives `100000`). It is `nil` or `-1` when the value is not a single number. |
| `IsMain` | `isMain` | `true` for the key parameters. A JSON null gives `false`. |

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

The client retries transport failures, HTTP 429 and HTTP 5xx up to `MaxRetries` times. LCSC also sends error codes in the response envelope with HTTP status 200. The client retries an envelope 429 up to `MaxRetries` times, but an envelope 5xx only one time. It does not retry an envelope 405.

When a response has a `Retry-After` header (seconds or an HTTP date), the client waits at least that time before the next attempt. When that time is longer than `MaxBackoff`, or when the context ends before that time, the client does not retry. It returns the error, and `APIError.RetryAfter` holds the time.

## Error Handling

```go
import "errors"

_, err := client.Product.Details(ctx, "C99999999")
if err != nil {
	if errors.Is(err, lcsc.ErrInvalidRequest) {
		// bad input, also envelope code 405
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
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RetryAfter)
	}
}
```

## Changes In v1.2.0

All API changes are additive. Existing code compiles without changes.

- `PriceBreak` has the new fields `USDPrice` and `CurrencyPrice`, and the new method `Price()`. `ProductPrice` is in USD for every currency. Use `Price()` for the price in the response currency.
- New `SupportedCurrencies` map and `Product.Currency()` method.
- New `Product` fields: `ProductID`, `CurrencyType`, `BrandID`, `WmCatalogID`, `ProductImageURLBig`, `IsNotOverstock`, `IsForeignOnsale`, `HasThirdPartyStock`, `HasAlternatePart`, `MaxBuyNumber`, `IsReel`, `ReelPrice`, `ProductArrange`, `StockSz`, `StockJs`, `WmStockHk`, `Eccn` and `FlashSale`.
- New `Lifecycle` type with `Product.Lifecycle()`, and new method `Product.AllowsBackorder()`.
- New `FlashSale` type with the methods `Price()` and `DeliveryDays()`.
- New `SearchResponse.ActualTotalCount` field.
- New `APIError.RetryAfter` field.
- `FlexFloat64` decodes an empty string as 0. Before, it returned an error.
- Envelope code 405 matches `ErrInvalidRequest`.
- The client retries an envelope 5xx only one time. Before, it retried up to `MaxRetries` times.
- The client honors `Retry-After` for HTTP 429 and envelope 429.

## Changes In v1.1.0

All API changes are additive. Existing code compiles without changes. `Search.Keyword` gives different results for some keywords:

- `Search.Keyword` reads `exactMatchResult` from the v3 response. For a model keyword such as `RP2040`, it can now return only the exact match.
- `Search.Keyword` does not send the fallback request for parameter queries. It returns an empty result with `ParametricQuery` set to `true`.
- `Search.Keyword` does not send the fallback request for other classifications, for example a brand name. It returns an empty result.
- `Search.Keyword` drops fallback rows that do not match the keyword.
- New `SearchResponse` fields: `QueryTypes` and `ParametricQuery`.
- New `Product` fields: `MinBuyNumber`, `Split`, `ProductCycle`, `IsPreSale`, `MatchType` and `AlternatePartList`.
- New `Parameter` fields: `ParamCode`, `ParamValueEnForSearch` and `IsMain`.
- New type `FlexString`.

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
