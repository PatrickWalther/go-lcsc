package lcsc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchKeywordSuccess(t *testing.T) {
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/ftps/wm/search/v3/global" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body := mustReadBody(t, req)
		if !strings.Contains(body, `"keyword":"STM32F103"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": {
					"totalCount": 1,
					"productList": [
						{
							"productCode": "C123",
							"productModel": "STM32F103"
						}
					]
				},
				"tipProductDetailUrlVO": {
					"productCode": "C123"
				}
			}
		}`), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "STM32F103"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if resp.TotalCount != 1 {
		t.Fatalf("expected total count 1, got %d", resp.TotalCount)
	}
	if resp.ActualTotalCount != 1 {
		t.Fatalf("expected actual total count 1, got %d", resp.ActualTotalCount)
	}
	if len(resp.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(resp.Products))
	}
	if resp.Products[0].ProductCode != "C123" {
		t.Fatalf("unexpected product code: %s", resp.Products[0].ProductCode)
	}
	if resp.DirectMatchCode != "C123" {
		t.Fatalf("unexpected direct match: %s", resp.DirectMatchCode)
	}
}

func TestSearchKeywordFallbackToProductQueryList(t *testing.T) {
	// v3/global no longer returns product lists for keyword searches; the
	// client must fall back to /product/query/list.
	var paths []string
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/ftps/wm/search/v3/global":
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"productSearchResultVO": null,
					"tipProductDetailUrlVO": null,
					"scene": "FULL_MATCH",
					"totalCount": 129
				}
			}`), nil
		case "/ftps/wm/product/query/list":
			body := mustReadBody(t, req)
			if !strings.Contains(body, `"keyword":"STM32F103"`) {
				t.Fatalf("unexpected fallback body: %s", body)
			}
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"totalRow": 129,
					"dataList": [
						{"productCode": "C8734", "productModel": "STM32F103C8T6"}
					]
				}
			}`), nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "STM32F103"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %v", paths)
	}
	if len(resp.Products) != 1 || resp.Products[0].ProductCode != "C8734" {
		t.Fatalf("unexpected products: %+v", resp.Products)
	}
	if resp.TotalCount != 129 {
		t.Fatalf("expected total count 129, got %d", resp.TotalCount)
	}
	// The response has no actualTotalRow. The client uses totalRow.
	if resp.ActualTotalCount != 129 {
		t.Fatalf("expected actual total count 129, got %d", resp.ActualTotalCount)
	}
}

func TestSearchKeywordDirectMatchStillReturnsProducts(t *testing.T) {
	// Callers that only read Products (not DirectMatchCode) must still get
	// results for exact-code/MPN keywords.
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/ftps/wm/search/v3/global":
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"productSearchResultVO": null,
					"isToDetail": true,
					"tipProductDetailUrlVO": {"productCode": "C8734"}
				}
			}`), nil
		case "/ftps/wm/product/query/list":
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"totalRow": 1,
					"dataList": [{"productCode": "C8734", "productModel": "STM32F103C8T6"}]
				}
			}`), nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "C8734"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if resp.DirectMatchCode != "C8734" {
		t.Fatalf("unexpected direct match: %s", resp.DirectMatchCode)
	}
	if len(resp.Products) != 1 || resp.Products[0].ProductCode != "C8734" {
		t.Fatalf("unexpected products: %+v", resp.Products)
	}
}

func TestSearchKeywordRegressionPartNumberSuggestion(t *testing.T) {
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": {
					"totalCount": 1,
					"productList": [
						{
							"productCode": "C2182038",
							"productModel": "CGA2B2C0G1H390J050BA",
							"productIntroEn": "39pF C0G ±5% 50V 0402 Ceramic Capacitors RoHS",
							"brandNameEn": "TDK"
						}
					]
				},
				"tipProductDetailUrlVO": null
			}
		}`), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{
		Keyword: "CGJ2B2C0G1H390J050BA",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Products) == 0 {
		t.Fatal("expected at least one product for CGJ2B2C0G1H390J050BA")
	}
	if resp.Products[0].ProductCode != "C2182038" {
		t.Fatalf("unexpected product code: %s", resp.Products[0].ProductCode)
	}
}

func TestSearchKeywordCaching(t *testing.T) {
	var calls int32
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": {
					"totalCount": 1,
					"productList": [{"productCode":"C1"}]
				}
			}
		}`), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithCache(NewMemoryCache(time.Minute)),
		WithCacheConfig(CacheConfig{
			Enabled:    true,
			SearchTTL:  time.Minute,
			DetailsTTL: time.Minute,
		}),
		WithoutRetry(),
	)
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	_, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "LM7805"})
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}
	_, err = client.Search.Keyword(ctx, &SearchRequest{Keyword: "LM7805"})
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one HTTP request with cache hit, got %d", got)
	}
}

func TestSearchKeywordValidation(t *testing.T) {
	client := NewClient(WithoutRetry(), WithoutCache())
	defer func() { _ = client.Close() }()

	_, err := client.Search.Keyword(context.Background(), nil)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for nil request, got %v", err)
	}

	_, err = client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "   "})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for blank keyword, got %v", err)
	}
}

func TestProductDetailsSuccess(t *testing.T) {
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		if req.URL.Path != "/ftps/wm/product/detail" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("productCode") != "C8734" {
			t.Fatalf("unexpected productCode query: %s", req.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productCode": "C8734",
				"productModel": "LM7805",
				"brandNameEn": "ST"
			}
		}`), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	// LCSC finds no product for a lower-case code. The client sends the
	// code in upper case.
	product, err := client.Product.Details(context.Background(), " c8734 ")
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if product.ProductCode != "C8734" {
		t.Fatalf("unexpected product code: %s", product.ProductCode)
	}
}

func TestProductDetailsValidationAndNotFound(t *testing.T) {
	client := NewClient(WithoutRetry(), WithoutCache())
	defer func() { _ = client.Close() }()

	_, err := client.Product.Details(context.Background(), "  ")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}

	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {}
		}`), nil
	})
	client = NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	_, err = client.Product.Details(context.Background(), "C99999999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProductDetailsDecodesOrderFieldsAndAlternates(t *testing.T) {
	// Trimmed live response for C1525 under the EUR cookie, with five
	// alternates.
	fixture := mustReadFixture(t, "product_detail_C1525_EUR.json")
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fixture), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	p, err := client.Product.Details(context.Background(), "C1525")
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}

	if p.MinBuyNumber != 100 || p.Split != 100 {
		t.Fatalf("unexpected order limits: min %d, split %d", p.MinBuyNumber, p.Split)
	}
	if p.ProductCycle != "normal" || p.IsPreSale {
		t.Fatalf("unexpected lifecycle: %q, pre-sale %v", p.ProductCycle, p.IsPreSale)
	}
	if p.MatchType != "" {
		t.Fatalf("expected no match type on the main product, got %q", p.MatchType)
	}

	wantAlternates := []struct {
		code      string
		model     string
		matchType FlexString
		minBuy    int
		split     int
	}{
		{"C106994", "GRM155R71C104KA88J", "5", 50, 50},
		{"C71629", "GRM155R71C104KA88D", "5", 100, 100},
		{"C92753", "EMK105B7104KV-F", "5", 50, 50},
		{"C2167897", "C0402C104J4RACTU", "6", 50, 50},
		{"C913742", "GRM155R71C104JA88D", "6", 50, 50},
	}
	if len(p.AlternatePartList) != len(wantAlternates) {
		t.Fatalf("expected %d alternates, got %d", len(wantAlternates), len(p.AlternatePartList))
	}
	for i, want := range wantAlternates {
		alt := p.AlternatePartList[i]
		if alt.ProductCode != want.code || alt.ProductModel != want.model {
			t.Fatalf("alternate %d: got %s %s, want %s %s", i, alt.ProductCode, alt.ProductModel, want.code, want.model)
		}
		if alt.MatchType != want.matchType {
			t.Fatalf("alternate %d: got match type %q, want %q", i, alt.MatchType, want.matchType)
		}
		if alt.MinBuyNumber != want.minBuy || alt.Split != want.split {
			t.Fatalf("alternate %d: got min %d split %d, want min %d split %d", i, alt.MinBuyNumber, alt.Split, want.minBuy, want.split)
		}
		if alt.ProductCycle != "normal" {
			t.Fatalf("alternate %d: unexpected lifecycle %q", i, alt.ProductCycle)
		}
		if len(alt.AlternatePartList) != 0 {
			t.Fatalf("alternate %d: expected no nested alternates", i)
		}
	}

	if len(p.ParamVOList) != 4 {
		t.Fatalf("expected 4 parameters, got %d", len(p.ParamVOList))
	}
	capacitance := p.ParamVOList[0]
	if capacitance.ParamCode != "param_10951_n" || capacitance.ParamNameEn != "Capacitance" || !capacitance.IsMain {
		t.Fatalf("unexpected capacitance parameter: %+v", capacitance)
	}
	if capacitance.ParamValueEnForSearch == nil || *capacitance.ParamValueEnForSearch != 100000 {
		t.Fatalf("expected capacitance search value 100000, got %v", capacitance.ParamValueEnForSearch)
	}
	dielectric := p.ParamVOList[1]
	if dielectric.ParamValueEnForSearch == nil || *dielectric.ParamValueEnForSearch != -1 {
		t.Fatalf("expected dielectric search value -1, got %v", dielectric.ParamValueEnForSearch)
	}
	tolerance := p.ParamVOList[2]
	if tolerance.ParamNameEn != "Tolerance" || tolerance.ParamValueEnForSearch != nil {
		t.Fatalf("expected nil search value for tolerance, got %+v", tolerance)
	}
}

func TestProductDetailsCacheKeepsNewFields(t *testing.T) {
	fixture := mustReadFixture(t, "product_detail_C1525_EUR.json")
	var calls int32
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, fixture), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithCache(NewMemoryCache(time.Minute)),
		WithCacheConfig(CacheConfig{
			Enabled:    true,
			SearchTTL:  time.Minute,
			DetailsTTL: time.Minute,
		}),
		WithoutRetry(),
	)
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	first, err := client.Product.Details(ctx, "C1525")
	if err != nil {
		t.Fatalf("first details call failed: %v", err)
	}
	second, err := client.Product.Details(ctx, "C1525")
	if err != nil {
		t.Fatalf("second details call failed: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one HTTP request with cache hit, got %d", got)
	}
	if first.FlashSale == nil || first.IsForeignOnsale == nil || first.ProductPriceList[0].CurrencyPrice == 0 {
		t.Fatalf("expected the fixture to fill the pointer and currency fields: %+v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached product differs from the decoded product:\n%+v\n%+v", first, second)
	}
}

func TestProductDetailsDecodesCurrencyIDsAndLifecycle(t *testing.T) {
	// Trimmed live response for C1525 under the EUR cookie. LCSC sends
	// productPrice in USD and currencyPrice in EUR.
	fixture := mustReadFixture(t, "product_detail_C1525_EUR.json")
	httpClient := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Cookie"); got != "currencyCode=EUR" {
			t.Errorf("expected the EUR currency cookie, got %q", got)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})

	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(httpClient),
		WithCurrency("eur"),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	p, err := client.Product.Details(context.Background(), "C1525")
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}

	if p.ProductID != 1877 || p.BrandID != 254 || p.WmCatalogID != 1142 {
		t.Fatalf("unexpected ids: product %d, brand %d, catalog %d", p.ProductID, p.BrandID, p.WmCatalogID)
	}
	if p.CurrencyType != "EUR" || p.Currency() != "EUR" {
		t.Fatalf("expected currency EUR, got type %q and currency %q", p.CurrencyType, p.Currency())
	}

	wantPrices := []struct {
		ladder       int
		productPrice float64
		usdPrice     float64
		eurPrice     float64
	}{
		{100, 0.0045, 0.0045, 0.0041},
		{1000, 0.0034, 0.0034, 0.0031},
		{3000, 0.0029, 0.0029, 0.0026},
		{10000, 0.0025, 0.0025, 0.0023},
		{50000, 0.0024, 0.0024, 0.0022},
		{100000, 0.0023, 0.0023, 0.0021},
	}
	if len(p.ProductPriceList) != len(wantPrices) {
		t.Fatalf("expected %d price breaks, got %d", len(wantPrices), len(p.ProductPriceList))
	}
	for i, want := range wantPrices {
		pb := p.ProductPriceList[i]
		if pb.Ladder != want.ladder || float64(pb.ProductPrice) != want.productPrice || float64(pb.USDPrice) != want.usdPrice || float64(pb.CurrencyPrice) != want.eurPrice {
			t.Fatalf("price break %d: got %+v, want %+v", i, pb, want)
		}
		if pb.CurrencySymbol != "\u20ac" {
			t.Fatalf("price break %d: unexpected symbol %q", i, pb.CurrencySymbol)
		}
		if pb.Price() != want.eurPrice || pb.Price() == float64(pb.ProductPrice) {
			t.Fatalf("price break %d: expected Price %v (EUR), got %v", i, want.eurPrice, pb.Price())
		}
	}

	if !p.IsReel || float64(p.ReelPrice) != 3 || p.ProductArrange != "Tape & Reel (TR)" {
		t.Fatalf("unexpected reel data: reel %v, price %v, arrange %q", p.IsReel, p.ReelPrice, p.ProductArrange)
	}
	if p.MaxBuyNumber != -1 || p.Eccn != "EAR99" {
		t.Fatalf("unexpected max buy %d or ECCN %q", p.MaxBuyNumber, p.Eccn)
	}
	if p.IsNotOverstock || p.IsForeignOnsale == nil || !*p.IsForeignOnsale {
		t.Fatalf("unexpected order flags: not overstock %v, foreign on sale %v", p.IsNotOverstock, p.IsForeignOnsale)
	}
	if p.HasAlternatePart != nil {
		t.Fatalf("expected no hasAlternatePart on a detail response, got %v", *p.HasAlternatePart)
	}
	if p.HasThirdPartyStock {
		t.Fatal("expected the detail response to send hasThirdPartyStock false")
	}
	if p.Lifecycle() != LifecycleActive || !p.AllowsBackorder() {
		t.Fatalf("unexpected lifecycle %q, backorder %v", p.Lifecycle(), p.AllowsBackorder())
	}

	fs := p.FlashSale
	if fs == nil {
		t.Fatal("expected a flash sale")
	}
	if fs.ValidNumber != 120000 || fs.MinOrderNumber != 100000 || fs.Split != 10000 {
		t.Fatalf("unexpected flash sale quantities: %+v", fs)
	}
	if float64(fs.SellPrice) != 0.0016 || float64(fs.USDPrice) != 0.0017 || fs.SellCurrencyType != "EUR" || fs.Price() != 0.0016 {
		t.Fatalf("unexpected flash sale price: %+v", fs)
	}
	if minDays, maxDays, ok := fs.DeliveryDays(); !ok || minDays != 7 || maxDays != 9 {
		t.Fatalf("expected delivery 7-9 days, got %d-%d (%v)", minDays, maxDays, ok)
	}
	if !fs.IsOnsale || fs.BatchCode != "25/26+" || fs.ExpiredTime != "2026-10-13 23:59:59" {
		t.Fatalf("unexpected flash sale state: %+v", fs)
	}

	alt := p.AlternatePartList[0]
	if alt.ProductCode != "C106994" || alt.ProductID != 108210 {
		t.Fatalf("unexpected first alternate: %s (%d)", alt.ProductCode, alt.ProductID)
	}
	if alt.StockSz != 19350 || alt.StockSz+alt.StockJs+alt.WmStockHk != alt.StockNumber {
		t.Fatalf("unexpected alternate stock: sz %d, js %d, hk %d, total %d", alt.StockSz, alt.StockJs, alt.WmStockHk, alt.StockNumber)
	}
	if alt.CurrencyType != "" || alt.Currency() != "EUR" {
		t.Fatalf("expected alternate currency EUR from the symbol, got type %q and currency %q", alt.CurrencyType, alt.Currency())
	}
	if alt.HasAlternatePart == nil || *alt.HasAlternatePart {
		t.Fatalf("expected hasAlternatePart false on the alternate, got %v", alt.HasAlternatePart)
	}
	if alt.FlashSale != nil {
		t.Fatalf("expected no flash sale on the alternate, got %+v", alt.FlashSale)
	}
	if !strings.Contains(alt.ProductImageURLBig, "/900x900/") {
		t.Fatalf("unexpected alternate big image URL: %q", alt.ProductImageURLBig)
	}
}
