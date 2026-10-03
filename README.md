# go-lcsc

[![Go Reference](https://pkg.go.dev/badge/github.com/PatrickWalther/go-lcsc.svg)](https://pkg.go.dev/github.com/PatrickWalther/go-lcsc)
[![Go Report Card](https://goreportcard.com/badge/github.com/PatrickWalther/go-lcsc)](https://goreportcard.com/report/github.com/PatrickWalther/go-lcsc)
[![Tests](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml/badge.svg)](https://github.com/PatrickWalther/go-lcsc/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Unofficial Go client for [LCSC](https://www.lcsc.com) component search, parametric search, categories, product details, cross-reference alternates and marketplace offers.

LCSC does not provide a documented public API for this data. This library uses undocumented endpoints that can change without notice. Read [Risks](#risks) before you use it.

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

- Service-based API: `client.Search`, `client.Product`, `client.Alternates`, `client.Catalog` and `client.ThirdParty`
- Parametric search with category, package, manufacturer and parameter filters, and filter facets
- Prices in the response currency, LCSC ids, lifecycle state and order limits
- Cross-reference alternates with match types and a parameter comparison
- Marketplace offers of third-party suppliers
- Image size and datasheet URL helpers
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
	fmt.Println("parameter query: the products come from the route categories")
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

LCSC can classify a keyword as a parameter or package query, for example `100nF 0402`. Then the keyword endpoints cannot give a product list. For this case, the client sets `ParametricQuery` to `true` and does not send the fallback request. It gets the products from the categories of the v3 response, as `Parametric` does with the default options (see [Parametric Search](#parametric-search)). `Products` then holds the first 25 products from the first 3 leaf categories. When the v3 response has no category, `Products` is empty. The client returns no error for this case.

For other classifications without a product list, for example a brand name (`BRAND`), the client also does not send the fallback request. It returns an empty `Products` list and no error. `ParametricQuery` is `false`.

`QueryTypes` holds the raw classification from LCSC, for example `["STANDARD", "PRODUCT_PARAM"]` or `["PRODUCT_MODEL"]`.

`TotalCount` and `ActualTotalCount` give the number of matching products:

- For the fallback list and for a parameter query, `TotalCount` is the `totalRow` value of `/product/query/list`. LCSC caps this value at 5000. `ActualTotalCount` is the `actualTotalRow` value, which LCSC does not cap.
- For the v3 product list and the exact match list, both values are equal.
- When the client drops fallback rows, both values are the number of rows that the client keeps.

`Route` holds the route of the keyword (see [Parametric Search](#parametric-search)). It is not `nil` when `Keyword` returns no error. `Route.LeafCatalogIDs(n)` gives the leaf category ids of the route. For a parameter query, `CatalogIDs` holds the leaf category ids of the list request.

`Keyword` gets the route with `Route`. Thus `Keyword`, `Route` and `Parametric` use one cache entry for the route of a keyword, and `Parametric` for the same keyword does not send the v3 request again. With the cache, `Keyword` and then `Parametric` send these requests for a parameter query:

| Calls | Requests |
|---|---|
| `Keyword`, then `Parametric` with the default options | 2. `Parametric` gets the route and the list from the cache. |
| `Keyword` with `SkipParametricList: true`, then `Parametric` with any options | 2. `Keyword` sends only the v3 request. `Parametric` sends the list request with its options. |
| `Keyword`, then `Parametric` with other options | 3 |

```go
resp, err := client.Search.Keyword(ctx, &lcsc.SearchRequest{
	Keyword:            "10k 0603",
	SkipParametricList: true,
})
if err != nil {
	// handle error
}
if resp.ParametricQuery {
	// Parametric uses the cached route and sends one list request.
	list, err := client.Search.Parametric(ctx, "10k 0603", &lcsc.ParametricOptions{
		MaxCatalogs: 1,
		InStock:     true,
		Sort:        lcsc.SortStock,
		Desc:        true,
	})
	if err != nil {
		// handle error
	}
	fmt.Println(list.CatalogIDs, list.ActualTotalCount)
}
```

With `SkipParametricList`, `Keyword` returns no products for a parameter query. For other keywords, the field has no effect.

### Parametric Search

`client.Search.Parametric` returns products for a parameter or package query, for example `100nF 0402` or `10k 0603`. LCSC cannot answer such a query with a keyword list.

```go
resp, err := client.Search.Parametric(ctx, "100nF 0402", &lcsc.ParametricOptions{
	MaxCatalogs: 3,
	InStock:     true,
	Sort:        lcsc.SortStock,
	Desc:        true,
	PageSize:    50,
})
if err != nil {
	// handle error
}

fmt.Println(resp.Route.Scene, resp.CatalogIDs, resp.ActualTotalCount)
for _, p := range resp.Products {
	fmt.Println(p.ProductCode, p.ProductModel, p.StockNumber)
}
```

`Parametric` first calls `Route`, which sends the query to `/search/v3/global`. Then it uses the first rule that applies:

1. The route names one product (`RedirectCode`, scene `REDIRECT_PRODUCT_DETAIL`). `Parametric` returns the details of that product.
2. LCSC classifies the query as a product model and sends exact matches. `Parametric` returns the exact matches.
3. The route has top categories (scene `FULL_MATCH`). `Parametric` calls `List` with the query as `GlobalKeyword` and the first leaf categories of the route. `MaxCatalogs` sets the number of categories. The default is 3.
4. The route has a product page (scene `PARTIAL_MATCH`). `Parametric` returns that page. For another page or another page size, it sends the v3 request again with the page fields.
5. Otherwise, for example for scene `NO_RESULT`, `Parametric` returns an empty response and no error.

`InStock`, `Sort` and `Desc` apply only to rule 3. `Page` and `PageSize` follow the rules of `List`. `ListResponse.Route` holds the route.

`Route` fields:

| Field | Description |
|---|---|
| `Scene` | `SceneFullMatch`, `ScenePartialMatch`, `SceneRedirectProductDetail` or `SceneNoResult`. LCSC can send other values. |
| `TotalCount` | Number of matching products that LCSC reports for the scene. |
| `TopResults` | Categories with matching products, best category first. Each category has an id and a product count. |
| `ExactMatches` | Products whose model is equal to the keyword. Each product has a `ProductID`. |
| `RedirectCode` | Product code that the keyword names. |
| `QueryTypes`, `ProductModel` | Classification of the keyword. `ProductModel` is `true` for `PRODUCT_MODEL`. |
| `Products`, `Page`, `PageSize` | Product page for `PARTIAL_MATCH`. |

`Route.LeafCatalogIDs(n)` returns up to `n` leaf category ids from `TopResults`. `Route.IsParametric()` reports a parameter or package classification.

The cache keys of `Keyword` and `Route` keep the case of the keyword, because the case of an SI prefix changes a parameter query: `1m 0603` is milli and `1M 0603` is mega. `Route.Keyword` is always the keyword of the request.

`client.Search.Similar(ctx, code, opts)` finds products like a given product. It gets the product details. Then it calls `List` with the category, the package and the key parameters (`IsMain`) of the product. `SimilarOptions.Relax` removes parameters from the filter, and `SimilarOptions.AnyPackage` removes the package. A filter with all key parameters can be too narrow, so relax one parameter at a time. The result can include the product itself. `Product.SimilarFilter(relax...)` gives the same filter for a product that you already have.

### List and Facets

`client.Search.List` sends a filter to `/product/query/list`. The LCSC category pages use this endpoint.

```go
resp, err := client.Search.List(ctx, &lcsc.ListRequest{
	Filter: lcsc.Filter{
		GlobalKeyword: "10k 0603",
		CatalogIDs:    []int{1199},
		Params:        map[string][]string{"Tolerance": {"±1%"}},
		InStock:       true,
	},
	Sort:     lcsc.SortPrice,
	Page:     1,
	PageSize: 100,
})
if err != nil {
	// handle error
}

fmt.Println(resp.TotalCount, resp.ActualTotalCount)
```

Filter fields:

| Field | JSON | Description |
|---|---|---|
| `GlobalKeyword` | `globalKeyword` | Parameter or package query. It needs `CatalogIDs`. The client then sends `scene` `FULL_MATCH` for an exact match. Without the scene, LCSC uses a loose match. |
| `Keyword` | `keyword` | Text search. Without `CatalogIDs`, LCSC matches it as a substring of the model. With `GlobalKeyword`, it searches inside the results. |
| `CatalogIDs` | `catalogIdList` | Leaf category ids. LCSC finds no products for a parent id and sends no error. Use `Catalog.Leaves` to get the leaf ids. |
| `BrandIDs` | `brandIdList` | Manufacturer ids. |
| `Packages` | `encapValueList` | Package names, for example `0603`. |
| `Params` | `paramNameValueMap` | Parameter name to accepted values. A product must match one value of each name. Use the exact names and values of the facets. LCSC finds no products for an unknown name and sends no error. |
| `InStock` | `isStock` | Only products with LCSC stock. |
| `RoHS` | `isRohsCert` | Only products with a RoHS certificate. |

Request rules:

- `Page` starts at 1. `PageSize` is from 1 to 100. The client sends 25 when `PageSize` is 0.
- The client returns `ErrInvalidRequest` and does not send the request in these cases:
  - `PageSize` is above 100.
  - `Page × PageSize` is above 5000. LCSC returns at most 5000 rows. It answers a page that ends after row 5000 with code 405, also when the page starts before row 5000. For example, page 167 at page size 30 (rows 4981 to 5010) gives 405. To read the last rows, use a page size that divides 5000, for example 25, 50 or 100.
  - `GlobalKeyword` is set and `CatalogIDs` is empty.
  - A category id or a brand id is not positive.
- LCSC answers the first three cases with code 405.
- `Sort` is `SortStock` or `SortPrice`. `Desc` sets the descending order. The price sort uses the price of the largest quantity break. The stock sort is approximate.

`TotalCount` is the `totalRow` value, which LCSC caps at 5000. `ActualTotalCount` is the `actualTotalRow` value, which is the real count. `SearchResponse` and `AlternatesResponse` use the same two names with the same rule.

`client.Search.Facets` sends the same filter to `/product/query/param/group`. It returns the values that exist for the filter: `TotalCount`, `Packages`, `Manufacturers` (with ids), `Packagings`, and `Params` in the order of the LCSC site. For a dimension that the filter uses, LCSC sends only the selected values.

```go
facets, err := client.Search.Facets(ctx, &lcsc.Filter{CatalogIDs: []int{1142}, Packages: []string{"0402"}})
if err != nil {
	// handle error
}

capacitance := facets.Param("Capacitance")
for _, v := range capacitance.Values {
	n, _ := v.Number() // value in the standard unit (pF for capacitance)
	fmt.Println(v.Name, n)
}

// LCSC gives one value more than one name, for example 100nF and 100000pF.
params := facets.ExpandParams(map[string][]string{"Capacitance": {"100nF"}})
fmt.Println(params["Capacitance"]) // [100nF 100000pF ...]
```

`ParamFacet.Equivalents(name)` returns the names with the same quantity. Two names are equivalent only when both are a number with a unit of the facet, and when the values are not ranges. So `15mΩ@4.5V` and `15mΩ@10V` are not equivalent, although LCSC gives both the value 0.015. `FacetValue.Range()` returns the start and the end of a range value.

The client caches `List`, `Facets` and `Route` for `CacheConfig.SearchTTL`.

### Catalog Service

`client.Catalog` reads the LCSC category tree. A category id is the same id as `Product.WmCatalogID`.

```go
tree, err := client.Catalog.Tree(ctx)               // root categories with their children
path, err := client.Catalog.Path(ctx, 1199)         // Passives > Resistors > Chip Resistor - Surface Mount
leaves, err := client.Catalog.Leaves(ctx, 495)      // leaf ids under Capacitors
counts, err := client.Catalog.ChildCounts(ctx, 495) // child categories with product counts
```

- `Tree` uses `/product/category/tree`. It removes the root category "Maintenance, Repair & Operations" (id 1729), because that subtree repeats categories of other roots under different ids. The client caches the tree for 24 hours. When caching is disabled, each call sends a request, and the response is about 400 KB.
- `Path` and `Leaves` use `Tree`. They return `ErrNotFound` for an id that is not in the tree. Some ids occur two times in the tree. `Path` uses the first one.
- `Leaves` returns the id itself for a leaf category. Use the result in `Filter.CatalogIDs`.
- `ChildCounts` uses `/product/catalog/menu/onelevel`. It returns an empty list for a leaf category and `ErrNotFound` for an unknown id.

`Category` has the fields `ID`, `Name`, `ParentID`, `Level` (1 for a root category) and `Children`, and the methods `IsLeaf()` and `LeafIDs()`.

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
fmt.Println(product.AllowsBackorder()) // false when LCSC sells only up to StockNumber
fmt.Println(product.IsPreSale)

// Ids. ProductID is equal to the JLCPCB lcscComponentId.
fmt.Println(product.ProductID, product.BrandID, product.WmCatalogID)

// Prices with their currency.
for i, pb := range product.ProductPriceList {
	amount, currency := product.PriceBreakAmount(i)
	fmt.Println(pb.Ladder, amount, currency)
}

// Alternates that LCSC selects (up to five).
for _, alt := range product.AlternatePartList {
	fmt.Println(alt.ProductCode, alt.ProductModel, alt.Match().Label())
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
| `WmCatalogNameEn` | `wmCatalogNameEn` | English name of the `WmCatalogID` category. |
| `ParentCatalogList` | `parentCatalogList` | Parent categories, from the root category down. Only detail responses send it. |
| `FirstWmCatalogID` to `SixthWmCatalogID`, with the `...NameEn` fields | `firstWmCatalogId` to `sixthWmCatalogId` | Category path of a list row, from the root category down. Only list rows send it. |
| `ProductImageURLBig` | `productImageUrlBig` | 900x900 image. Detail responses do not send it. |
| `IsNotOverstock` | `isNotOverstock` | `true` when LCSC refuses an order quantity above `StockNumber`. LCSC still sells the product up to `StockNumber`. In the observed data, it is `true` exactly when `ProductCycle` is not `normal`. |
| `IsForeignOnsale` | `isForeignOnsale` | `false` when LCSC does not sell the product to overseas customers. `nil` when the response does not send it. |
| `HasThirdPartyStock` | `hasThirdPartyStock` | `true` when marketplace offers exist. Only list rows send a correct value. |
| `HasAlternatePart` | `hasAlternatePart` | `true` when LCSC has cross-reference alternates. Only list rows send it. |
| `IsReel`, `ReelPrice` | `isReel`, `reelPrice` | Reel option and reel fee in the response currency. |
| `ProductArrange` | `productArrange` | Packaging, for example `Tape & Reel (TR)`. |
| `StockSz`, `StockJs`, `WmStockHk` | `stockSz`, `stockJs`, `wmStockHk` | Stock per warehouse. The sum is `StockNumber`. Detail responses send only `stockSz`. |
| `Eccn` | `eccn` | Export control classification number. |
| `MoistureSensitivityLevel` | `moistureSensitivityLevel` | Moisture sensitivity level as Chinese text, for example `1级(无限)` (level 1, unlimited floor life). Only list rows send it. |
| `FlashSale` | `flashSaleProductPO` | Time-limited third-party offer, or `nil`. |
| `AlternatePartList` | `alternatePartList` | Alternates that LCSC selects (up to five). Only `Details` fills this list. Use `client.Alternates.List` for the full cross-reference list. |
| `MatchType` | `matchType` | Match code of an alternate, for example `"1"`, `"4"`, `"5"` or `"6"`. LCSC does not document the codes. The type is `FlexString`, which accepts a JSON string, number or null. Use `Match()` for a typed `MatchType`. |

Product methods:

| Method | Description |
|---|---|
| `Currency()` | Code of the response currency: `CurrencyType`, else the code for the price symbol, else `USD`. `CurrencyPrice` and `ReelPrice` are in this currency. `PriceBreak.Price()` is not always in this currency. See [Prices and Currency](#prices-and-currency). |
| `PriceBreakAmount(i)` | Unit price of `ProductPriceList[i]` and the currency code of that price. The currency is `USD` when the price break has no `CurrencyPrice`. |
| `Lifecycle()` | `LifecycleActive`, `LifecycleNotRecommended`, `LifecycleDiscontinued` or `LifecycleUnknown`. |
| `AllowsBackorder()` | `true` when LCSC accepts an order quantity above `StockNumber`. `false` when `IsNotOverstock` is `true` or `IsForeignOnsale` is `false`. See the rule below the table. |
| `Match()` | `MatchType` as a typed `MatchType` value, with `Label()` and `IsDropIn()`. |
| `CatalogPath()` | Category path from the root category to the leaf category. It uses `ParentCatalogList` (detail) or the list-row path fields. |
| `SimilarFilter(relax...)` | Filter for products like this product. See [Parametric Search](#parametric-search). |
| `ImageURL(size)` | First product image at the size `ImageSizeSmall`, `ImageSizeMedium` or `ImageSizeLarge`. See [Media Helpers](#media-helpers). |

`Lifecycle` follows the labels of the LCSC site:

- `LifecycleDiscontinued` for `stop_product`.
- `LifecycleNotRecommended` when `IsNotOverstock` is `true` and the cycle is not `stop_product`, for example for `sold_out`.
- `LifecycleActive` for `normal` and `on_sale`, and also for an empty cycle when the record has other lifecycle fields.
- `LifecycleUnknown` when the record has no lifecycle data. Also for another cycle when `IsNotOverstock` is `false`. The LCSC site shows no label for such a product, but the observed data has no such record.

`AllowsBackorder() == false` does not mean that the product cannot be ordered. When `IsNotOverstock` is `true`, LCSC still sells the product up to `StockNumber`. For example, C6119803 has the cycle `stop_product`, but LCSC sells its remaining stock. LCSC sells a quantity up to `StockNumber` when `IsForeignOnsale` is not `false`. It sells a larger quantity only when `AllowsBackorder()` is `true`.

`Details` changes the product code to upper case, because LCSC finds no product for a lower-case code.

Parameter fields:

| Field | JSON | Description |
|---|---|---|
| `ParamCode` | `paramCode` | LCSC identifier of the parameter, for example `param_10951_n`. |
| `ParamValueEnForSearch` | `paramValueEnForSearch` | Numeric value that LCSC uses for parametric search. It uses base units, but capacitance is in pF (100nF gives `100000`). It is `nil` or `-1` for many values that are not a single number. LCSC removes a test condition and other text from it: `21mΩ@2.5V` and `21mΩ@10V` both give `0.021`, and `100,000 cycles` gives `10`. Use it only for a value that is one number with a unit. |
| `IsMain` | `isMain` | `true` for the key parameters. A JSON null gives `false`. |

### Prices and Currency

LCSC supports four currencies (`lcsc.SupportedCurrencies`): USD (`$`), CNY (`￥`, U+FFE5), EUR (`€`) and HKD (`HK$`). The client sends the code of `WithCurrency` in the `currencyCode` cookie. For any other code, LCSC answers in USD.

Each `PriceBreak` has three prices:

| Field | JSON | Description |
|---|---|---|
| `ProductPrice` | `productPrice` | Unit price in USD for every currency. |
| `USDPrice` | `usdPrice` | Unit price in USD. |
| `CurrencyPrice` | `currencyPrice` | Unit price in the response currency. LCSC rounds it. Do not calculate it again. |

`PriceBreak.Price()` returns `CurrencyPrice` when it is more than zero. Else it returns `ProductPrice`, and when that is also zero, `USDPrice`. Thus `Price()` is in USD when `CurrencyPrice` is zero, also when the response currency is not USD. `Product.Currency()` gives the response currency and not the currency of `Price()`. Do not label `Price()` with `Currency()`.

To get a price together with its currency, use one of these methods:

| Method | Result |
|---|---|
| `PriceBreak.PriceIn(responseCurrency)` | The amount of `Price()`, and `responseCurrency` for `CurrencyPrice` or `USD` for `ProductPrice` and `USDPrice`. When `responseCurrency` is empty, it uses the code for `CurrencySymbol`. |
| `Product.PriceBreakAmount(i)` | `ProductPriceList[i].PriceIn(product.Currency())`. |
| `Offer.PriceBreakAmount(i)` | `ProductPriceList[i].PriceIn(offer.Currency())`. |
| `FlashSale.Amount()` | `SellPrice` in `SellCurrencyType`, or `USDPrice` in `USD`. |

For example, a detail response with `currencyType` `EUR` and the price break `{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975}` gives `Price()` 0.9975 and `Currency()` `EUR`, but `PriceBreakAmount(0)` gives 0.9975 `USD`.

```go
client := lcsc.NewClient(lcsc.WithCurrency("EUR"))
defer client.Close()

product, err := client.Product.Details(ctx, "C2040")
if err != nil {
	// handle error
}
for i, pb := range product.ProductPriceList {
	amount, currency := product.PriceBreakAmount(i)
	fmt.Printf("%d: %.4f %s (%.4f USD)\n", pb.Ladder, amount, currency, pb.USDPrice)
}
```

`FlashSale` has its own price and currency: `SellPrice` in `SellCurrencyType`, and `USDPrice`. `FlashSale.Price()` returns `USDPrice` when `SellPrice` is zero, so use `FlashSale.Amount()` to get the currency. Show `ValidNumber` as the quantity on offer. `DeliveryDays()` gives the minimum and the maximum delivery time.

### Alternate Service

`client.Alternates.List` returns the cross-reference alternates of a product. It uses `/product/alternate/part/list`, which the LCSC cross-reference tool uses.

```go
resp, err := client.Alternates.List(ctx, &lcsc.AlternatesRequest{
	ProductCode: "C1525",
	InStockOnly: false,
	Page:        1,
	PageSize:    100,
})
if err != nil {
	// handle error
}

fmt.Println(resp.Original.ProductCode, resp.TotalCount, resp.InStockCount)

for i := range resp.Alternates {
	alt := &resp.Alternates[i]
	fmt.Println(alt.ProductCode, alt.Match().Label(), alt.Match().IsDropIn(), alt.StockNumber)

	for _, d := range lcsc.DiffParameters(&resp.Original, alt) {
		fmt.Println("  ", d.Kind, d.Name, d.OriginalValue, "->", d.AlternateValue)
	}
}
```

Request rules:

- `ProductCode` is required. The client changes it to upper case, because LCSC finds no product for a lower-case code.
- `Page` starts at 1. The client sends 1 when `Page` is 0.
- `PageSize` is from 1 to 100. The client sends 100 when `PageSize` is 0. For a value above 100, the client returns `ErrInvalidRequest` and does not send the request. LCSC answers such a value with code 405.
- `InStockOnly` counts LCSC retail stock only. LCSC does not count JLCPCB stock. An alternate with LCSC stock 0 can have JLCPCB stock.
- The request has no sort options, because LCSC ignores the sort fields of this endpoint.

Response fields:

| Field | Description |
|---|---|
| `Original` | The product that the request names (`rawMaterial`). |
| `Alternates` | The alternates on the page, in the server order. Each alternate is a full `Product` with a `MatchType`. |
| `InStockCount` | Number of alternates with LCSC retail stock. `InStockOnly` does not change it. |
| `TotalCount` | Number of alternates on all pages (`totalRow`). |
| `ActualTotalCount` | Real number of alternates (`actualTotalRow`), else `TotalCount`. For the products that were checked, both counts had the same value. |

The server order is not always grouped by match type. Sort the list when the order is important. `List` returns `ErrNotFound` when LCSC does not know the product code. A known product with no alternates gives an empty `Alternates` list and no error. The client caches the response for `CacheConfig.SearchTTL`.

Match types:

| Code | `Label()` | `IsDropIn()` |
|---|---|---|
| `"2"` (`MatchTypeAltPackaging`) | `Alt. Packaging` | `true` |
| `"5"` (`MatchTypeDirect`) | `Direct` | `true` |
| `"6"` (`MatchTypeUpgrade`) | `Upgrade` | `false` |
| any other code, for example `"1"`, `"3"` or `"4"` | `Similar` | `false` |
| empty | empty | `false` |

The labels come from the LCSC web client. LCSC does not document the codes.

`DiffParameters(original, alt)` compares the parameters of two products and returns only the differences. Each `ParameterDiff` has a `Kind`:

- `ParameterChanged`: both products have a value, and the values are different.
- `ParameterMissing`: only the original product has a value.
- `ParameterAdded`: only the alternate has a value.

`DiffParameters` matches parameters on `ParamCode` first. Then it matches the remaining parameters on the name, and ignores case, spaces and punctuation in the name. LCSC uses different codes for the same parameter in some categories. Two values are equal when the text is equal without spaces. The value comparison does not ignore case, because `1mΩ` and `1MΩ` are different. Two values are also equal when both have the same numeric `ParamValueEnForSearch`, for example `100nF` and `0.1µF`. `DiffParameters` uses this rule only when both texts are one number with an optional unit, and when both units are the same unit with an optional SI prefix. LCSC removes the test condition from the search value, so `21mΩ@2.5V` and `21mΩ@10V` are different values, although both have the search value `0.021`. An empty value and `-` count as no value. `DiffParameters` does not compare the package (`EncapStandard`).

### Third Party Service

`client.ThirdParty` reads the marketplace offers of a product. A marketplace offer is stock of a third-party supplier, for example Waldom or Rochester, that LCSC sells in addition to its own stock. Each offer has its own stock, minimum order, order multiple, price ladder and delivery time.

```go
resp, err := client.ThirdParty.Offers(ctx, &lcsc.OffersRequest{ProductCode: "C8734"})
if err != nil {
	// handle error
}

for _, offer := range resp.Offers {
	minDays, maxDays, _ := offer.DeliveryDays()
	fmt.Printf("%s: %d pcs, MOQ %d, multiple %d, %d-%d days\n",
		offer.Source, offer.StockNumber, offer.MinBuyNumber, offer.Split, minDays, maxDays)
	for i, pb := range offer.ProductPriceList {
		amount, currency := offer.PriceBreakAmount(i)
		fmt.Printf("  %d+: %.4f %s\n", pb.Ladder, amount, currency)
	}
}

hasOffers, err := client.ThirdParty.HasStock(ctx, "C8734")
```

`Offers` sends the request to `/search/third`. Request rules:

- Set `ProductCode` or `Keyword`. Do not set both. The client changes `ProductCode` to upper case. A `Keyword` such as `STM32F103` gives the offers of all products that match it.
- `Page` starts at 1. `PageSize` is from 1 to 100. The client sends 10 when `PageSize` is 0. LCSC accepted 100. A larger page size was not checked.
- The client returns `ErrInvalidRequest` and does not send the request in these cases: a nil request, no selector, two selectors, a negative page, and a page size outside 0 to 100.
- A product with no offers gives an empty `Offers` list and no error. LCSC sends the same answer for an unknown product code.

Offer fields:

| Field | JSON | Description |
|---|---|---|
| `ProductCode` | `productCode` | LCSC product code. |
| `ManufacturerPartNumber` | `productCodeManufacturer` | Manufacturer part number. |
| `ProductModel` | `productModel` | Title of the offer, for example `STM32F103C8T6 ST 26+`. For Rochester, it is an id of the supplier. |
| `BrandID`, `BrandNameEn` | `brandId`, `brandNameEn` | LCSC manufacturer id and name. |
| `SupplierBrandName` | `lcOrderBrandNameEn` | Manufacturer name of the supplier, for example `STMICRO`. |
| `Source` | `productSource` | Supplier, for example `waldom` or `rochester`. |
| `VendorCode` | `vendorCode` | LCSC code of the offer, for example `G12277`. |
| `StockNumber` | `stockNumber` | Quantity on offer. |
| `MinBuyNumber` | `minBuyNumber` | Minimum order quantity of the offer. |
| `Split` | `split` | Order multiple of the offer. |
| `DeliveryTimeWayDays` | `deliveryTimeWayDays` | Minimum and maximum delivery time in days. Use `DeliveryDays()`. |
| `ProductPriceList` | `productPriceList` | Price ladder with `CurrencyPrice` and `USDPrice`, but no `ProductPrice`. Use `Offer.PriceBreakAmount(i)` to get each price with its currency. |
| `BatchCode` | `batchNumberEn` | Date code of the lot, for example `26+` or `2551`. |
| `IsOnsale` | `isOnsale` | `true` when the offer is open. |
| `IsPriceFirst`, `IsStockFirst`, `IsDeliveryTimeFirst` | same names | Badges for the best price, the most stock and the shortest delivery time. The meaning comes from the names and the data (inferred). LCSC sets the badges only for a `ProductCode` request. For a `Keyword` request, all offers have `false`. |

Offer rows send no `currencyType`, so `Offer.Currency()` gets the currency code from the price symbol. A price break without `CurrencyPrice` gives `USDPrice` in `USD` (see [Prices and Currency](#prices-and-currency)). The offers are information only. JLCPCB pre-orders do not use them (inferred).

`HasStock` sends the request to `/search/has/third/stock`. It answers `false` for an unknown product code. A detail response sends `HasThirdPartyStock` as `false` also for products with offers. Use `HasStock`, or the `HasThirdPartyStock` field of a list row.

The client caches `Offers` and `HasStock` for `CacheConfig.SearchTTL`.

### Media Helpers

LCSC stores each product image in three sizes under the same file name. The image URLs are not signed and need no Referer. `lcsc.ImageURLAtSize` changes the size segment of an image URL and sends no request.

```go
small := "https://assets.lcsc.com/images/lcsc/96x96/20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg"

large := lcsc.ImageURLAtSize(small, lcsc.ImageSizeLarge)
// https://assets.lcsc.com/images/lcsc/900x900/20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg

thumb := product.ImageURL(lcsc.ImageSizeMedium) // first product image at 224x224
```

| Constant | Size | Source in the responses |
|---|---|---|
| `ImageSizeSmall` | 96x96 | `ProductImageURL` of list rows |
| `ImageSizeMedium` | 224x224 | none |
| `ImageSizeLarge` | 900x900 | `ProductImages` of detail responses, `ProductImageURLBig` of list rows |

`ImageURLAtSize` returns the URL with no change when the host is not `assets.lcsc.com`, when the path does not start with `/images/`, or when the path has no size segment.

`Product.ImageURL` uses the first image URL in this order: `ProductImageURLBig`, the entries of `ProductImages`, then `ProductImageURL`. It skips a value that is not an http or https URL with a file name. Some records send a folder URL without a file name, for example `https://assets.lcsc.com/images/lcsc/900x900/`.

`client.Product.ResolveDatasheetURL` returns a URL that sends the datasheet PDF file. LCSC and JLCPCB use several URL forms for one datasheet:

| Input form | Example | Result | Requests |
|---|---|---|---|
| Direct file | `https://datasheet.lcsc.com/datasheet/pdf/{hash}.pdf?productCode=C1525` (`Product.PdfURL`) | The same URL | 0 |
| Legacy file | `https://datasheet.lcsc.com/lcsc/{file}.pdf` or `https://datasheet.lcsc.com/szlcsc/{file}.pdf` | `https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/{file}.pdf` | 0 |
| Viewer page | `https://www.lcsc.com/datasheet/C1525.pdf`, or the JLCPCB form `https://www.lcsc.com/datasheet/lcsc_datasheet_{file}.pdf` | The `previewPdfUrl` of the page (a direct file URL) | 1 |
| Other http or https URL | `https://www.ti.com/lit/ds/symlink/lm358.pdf` | The same URL | 0 |

```go
pdfURL, err := client.Product.ResolveDatasheetURL(ctx, "https://www.lcsc.com/datasheet/C1525.pdf")
if err != nil {
	// handle error
}
fmt.Println(pdfURL) // https://datasheet.lcsc.com/datasheet/pdf/....pdf?productCode=C1525
```

- The viewer page is HTML, although its name ends with `.pdf`. The legacy forms send a redirect to the viewer page. The `/szlcsc/` form first sends a redirect to the `/lcsc/` form.
- The client follows a redirect only to another viewer page or to a PDF file on an LCSC host. For an unknown product, LCSC sends a redirect to the home page. `ResolveDatasheetURL` then returns `ErrNotFound`.
- An empty value, or a value that is not an absolute http or https URL, gives `ErrInvalidRequest`. For example, JLCPCB sends `--` for a part with no datasheet.
- The viewer request uses the rate limiter and the retry rules of the client. The client caches the result for 24 hours.

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

When a retryable response has a `Retry-After` header (seconds or an HTTP date), the client waits at least that time before the next attempt. This applies to every retryable status, also to HTTP 5xx. When that time is longer than `MaxBackoff`, the client does not retry. It returns the error, and `APIError.RetryAfter` holds the time.

When the wait before the next attempt (the backoff or the `Retry-After` time) ends after the context deadline, the client does not wait. It returns the last error, for example an error that matches `ErrServer`, and not `context.DeadlineExceeded`.

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

## Risks

- **Undocumented endpoints.** LCSC does not document the endpoints that this library uses. LCSC can change or remove them without notice. The weekly integration workflow (`.github/workflows/integration.yml`) runs contract tests against the live endpoints. Run the integration tests before you upgrade.
- **Errors arrive with HTTP 200.** LCSC sends most errors in the response envelope with HTTP status 200, for example code 405 for an invalid field and code 500 for a server error. The client maps the envelope code to the typed errors. Some failures give no error at all:
  - An unknown parameter name or value in `Filter.Params` gives 0 rows. Treat 0 rows with a parameter filter as a sign to check the facet names.
  - A parent category id in `Filter.CatalogIDs` gives 0 rows.
  - An unknown product code gives an empty offer list, and `HasStock` gives `false`.
- **5000-row cap.** `/product/query/list` returns at most 5000 rows for one filter, and its `totalRow` value stops at 5000. LCSC answers a page that ends after row 5000 with code 405, also when `totalPage` includes that page. `List` refuses such a page and does not send the request. To read the last rows, use a page size that divides 5000. Use `ActualTotalCount` for the real count. Narrow the filter to get other rows.
- **Request protection.** The LCSC web client encrypts the search keyword of `/search/v3/global` with SM2. Each LCSC response also sends a key pair in the `x_web_cipher_pairs` header. These are anti-scraping controls. This library sends the keyword as plain text, which LCSC accepts today. Do not reproduce the SM2 keyword encryption in this library or in your code. If LCSC stops accepting the plain keyword, use `List` with `Filter.Keyword`.
- **Rate limits.** LCSC does not publish rate limits. The default client sends at most 5 requests per second. Keep interactive use at 1 to 2 requests per second, and bulk jobs lower. The client honors `Retry-After` (see [Retry Controls](#retry-controls)).
- **Inferred rules.** Some rules come from live data and from the LCSC web client, not from documentation. Examples are `Product.AllowsBackorder()`, `MatchType.IsDropIn()`, the offer badges and some page size limits. The Go doc comments mark these rules as inferred.
- **Terms of use.** The terms of the LCSC partner API forbid bulk capture of LCSC data. They also forbid hosting of LCSC data, datasheets or images for third parties. Read the LCSC terms before you store or share data from this library.

## Changes In v1.3.0

All API changes are additive. Existing code compiles without changes, except code that writes `SearchRequest` or `SearchResponse` as a literal without field names.

- New method `PriceBreak.PriceIn(responseCurrency)`. It returns the price together with its currency. When a price break has no `CurrencyPrice`, `Price()` falls back to the USD price, but `Product.Currency()` still gives the response currency. `PriceIn` then gives `USD`. New methods `Product.PriceBreakAmount(i)`, `Offer.PriceBreakAmount(i)` and `FlashSale.Amount()` use the same rule.
- The doc comments of `PriceBreak.Price()`, `Product.Currency()`, `Offer.Currency()` and `FlashSale.Price()` tell that the fallback price is in USD.
- `Product.ImageURL` skips a value that is not an http or https URL with a file name, for example the folder URL `https://assets.lcsc.com/images/lcsc/900x900/`. Before, it returned the folder URL.
- `Search.Keyword` gets the route with `Search.Route` and stores it under the route cache key. `Search.Parametric` for the same keyword then does not send the v3 request again. Before, `Keyword` and then `Parametric` sent 3 requests for a parameter query with the default options, and 4 requests with other options.
- New `SearchResponse` fields: `Route` and `CatalogIDs`.
- New `SearchRequest.SkipParametricList` field. With it, `Keyword` sends no list request for a parameter query. `Keyword` and then `Parametric` with any options then send 2 requests.
- `Search.Keyword` ignores a cache entry without a route, for example an entry of v1.2.0 in a shared cache, and sends the requests again.
- The doc comment of `Product.AllowsBackorder()` tells that `false` does not mean "not orderable". LCSC still sells a product with `IsNotOverstock` up to `StockNumber`, for example C6119803.

## Changes In v1.2.0

All API changes are additive. Existing code compiles without changes.

- `PriceBreak` has the new fields `USDPrice` and `CurrencyPrice`, and the new method `Price()`. `ProductPrice` is in USD for every currency. Use `Price()` for the price in the response currency.
- New `SupportedCurrencies` map and `Product.Currency()` method.
- New `Product` fields: `ProductID`, `CurrencyType`, `BrandID`, `WmCatalogID`, `ProductImageURLBig`, `IsNotOverstock`, `IsForeignOnsale`, `HasThirdPartyStock`, `HasAlternatePart`, `MaxBuyNumber`, `IsReel`, `ReelPrice`, `ProductArrange`, `StockSz`, `StockJs`, `WmStockHk`, `Eccn` and `FlashSale`.
- New `Lifecycle` type with `Product.Lifecycle()`, and new method `Product.AllowsBackorder()`.
- New `FlashSale` type with the methods `Price()` and `DeliveryDays()`.
- New `SearchResponse.ActualTotalCount` field. `ListResponse` and `AlternatesResponse` use the same name for the real count.
- New `APIError.RetryAfter` field.
- `FlexFloat64` decodes an empty string as 0. Before, it returned an error.
- Envelope code 405 matches `ErrInvalidRequest`.
- The client retries an envelope 5xx only one time. Before, it retried up to `MaxRetries` times.
- The client honors `Retry-After` for every retryable response, also for HTTP 5xx.
- When the wait before the next attempt ends after the context deadline, the client returns the last API error at once. Before, it waited until the deadline and returned the context error.
- New `client.Alternates` service (`AlternateService`) with `List()`, and the types `AlternatesRequest` and `AlternatesResponse`.
- New `MatchType` type with the methods `Label()` and `IsDropIn()`, the constants `MatchTypeAltPackaging`, `MatchTypeDirect` and `MatchTypeUpgrade`, and the new method `Product.Match()`. The field `Product.MatchType` keeps the type `FlexString`.
- New `DiffParameters()` function with the types `ParameterDiff` and `ParameterDiffKind`.
- New `client.Catalog` service (`CatalogService`) with `Tree()`, `Path()`, `Leaves()` and `ChildCounts()`, and the types `Category`, `CategoryRef` and `CategoryCount`.
- New `SearchService` methods: `List()`, `Facets()`, `Route()`, `Parametric()` and `Similar()`. New types: `Filter`, `ListRequest`, `ListResponse`, `SortField`, `Facets`, `FacetBrand`, `ParamFacet`, `FacetValue`, `Route`, `RouteScene`, `RouteCategory`, `ParametricOptions` and `SimilarOptions`.
- New `Product` fields: `WmCatalogNameEn`, `ParentCatalogList`, `FirstWmCatalogID` to `SixthWmCatalogID` with their names, and `MoistureSensitivityLevel`. New methods: `Product.CatalogPath()` and `Product.SimilarFilter()`.
- `Search.Keyword` returns products for a parameter query. Before, it returned an empty list. It now sends a second request to `/product/query/list` with the categories of the v3 response. `ParametricQuery` stays `true`.
- New `client.ThirdParty` service (`ThirdPartyService`) with `Offers()` and `HasStock()`, and the types `OffersRequest`, `OffersResponse` and `Offer`.
- New `ImageSize` type with the constants `ImageSizeSmall`, `ImageSizeMedium` and `ImageSizeLarge`, the new function `ImageURLAtSize()` and the new method `Product.ImageURL()`.
- New method `ProductService.ResolveDatasheetURL()`. It changes a legacy or viewer datasheet URL to a URL that sends the PDF file.
- `Product.Details` changes the product code to upper case. Before, a lower-case code gave `ErrNotFound`.
- The cache key of `Search.Keyword` keeps the case of the keyword. Before, keywords that differ only in case shared one cache entry.

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

The integration tests send about 27 read-only requests to the live LCSC endpoints.

## License

MIT - see [LICENSE](LICENSE).
