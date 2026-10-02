package lcsc

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testSearchV3Path  = "/ftps/wm/search/v3/global"
	testQueryListPath = "/ftps/wm/product/query/list"
)

func newSearchTestClient(fn roundTripFunc) *Client {
	return NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(fn)),
		WithoutRetry(),
		WithoutCache(),
	)
}

func productCodes(products []Product) []string {
	codes := make([]string, 0, len(products))
	for _, p := range products {
		codes = append(codes, p.ProductCode)
	}
	return codes
}

func TestSearchKeywordParametricQuerySkipsFallback(t *testing.T) {
	// Live response for "100nF 0402". LCSC classifies the keyword as a
	// package and parameter query and returns no product list.
	v3 := mustReadFixture(t, "search_v3_parametric_100nF_0402.json")
	unrelated := mustReadFixture(t, "query_list_100nF_0402.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusOK, unrelated), nil
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("expected only the v3 request, got %v", paths)
	}
	if !resp.ParametricQuery {
		t.Fatal("expected ParametricQuery to be true")
	}
	if len(resp.Products) != 0 {
		t.Fatalf("expected no products, got %v", productCodes(resp.Products))
	}
	if resp.TotalCount != 0 {
		t.Fatalf("expected total count 0, got %d", resp.TotalCount)
	}
	if want := []string{"STANDARD", "PRODUCT_PARAM"}; !reflect.DeepEqual(resp.QueryTypes, want) {
		t.Fatalf("expected query types %v, got %v", want, resp.QueryTypes)
	}
}

func TestSearchKeywordParametricResultIsCached(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_100nF_0402.json")

	var calls int32
	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return jsonResponse(http.StatusOK, v3), nil
		})),
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
	if _, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "100nF 0402"}); err != nil {
		t.Fatalf("first search failed: %v", err)
	}
	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one HTTP request with cache hit, got %d", got)
	}
	if !resp.ParametricQuery {
		t.Fatal("expected cached response to keep ParametricQuery")
	}
	if want := []string{"STANDARD", "PRODUCT_PARAM"}; !reflect.DeepEqual(resp.QueryTypes, want) {
		t.Fatalf("expected cached query types %v, got %v", want, resp.QueryTypes)
	}
}

func TestSearchKeywordUsesExactMatchResult(t *testing.T) {
	// Live response for "RP2040". LCSC classifies the keyword as a product
	// model and puts C2040 in exactMatchResult.
	v3 := mustReadFixture(t, "search_v3_model_RP2040.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "RP2040"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("expected only the v3 request, got %v", paths)
	}
	if got := productCodes(resp.Products); !reflect.DeepEqual(got, []string{"C2040"}) {
		t.Fatalf("expected [C2040], got %v", got)
	}
	if resp.TotalCount != 1 {
		t.Fatalf("expected total count 1, got %d", resp.TotalCount)
	}
	if resp.ParametricQuery {
		t.Fatal("expected ParametricQuery to be false")
	}
	if want := []string{"PRODUCT_MODEL"}; !reflect.DeepEqual(resp.QueryTypes, want) {
		t.Fatalf("expected query types %v, got %v", want, resp.QueryTypes)
	}

	p := resp.Products[0]
	if p.ProductModel != "RP2040" || p.BrandNameEn != "Raspberry Pi" {
		t.Fatalf("unexpected product: %s %s", p.ProductModel, p.BrandNameEn)
	}
	if p.MinBuyNumber != 1 || p.Split != 1 {
		t.Fatalf("unexpected order limits: min %d, split %d", p.MinBuyNumber, p.Split)
	}
	if p.ProductCycle != "normal" || p.IsPreSale {
		t.Fatalf("unexpected lifecycle: %q, pre-sale %v", p.ProductCycle, p.IsPreSale)
	}
	if p.MatchType != "" {
		t.Fatalf("expected empty match type for null, got %q", p.MatchType)
	}
	if len(p.ProductPriceList) == 0 || float64(p.ProductPriceList[0].ProductPrice) != 0.9975 {
		t.Fatalf("unexpected price list: %+v", p.ProductPriceList)
	}
}

func TestSearchKeywordFallbackDropsUnrelatedRows(t *testing.T) {
	// The list fixture mixes two RP2040 rows with three unrelated rows.
	// All rows come from live /product/query/list responses.
	list := mustReadFixture(t, "query_list_mixed_RP2040.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case testSearchV3Path:
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"productSearchResultVO": null,
					"tipProductDetailUrlVO": null,
					"exactMatchResult": null,
					"searchEngineProcess": {"judgeSuccessType": ["PRODUCT_MODEL"]}
				}
			}`), nil
		case testQueryListPath:
			body := mustReadBody(t, req)
			if !strings.Contains(body, `"keyword":"RP2040"`) {
				t.Errorf("unexpected fallback body: %s", body)
			}
			return jsonResponse(http.StatusOK, list), nil
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusNotFound, ""), nil
		}
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "RP2040"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %v", paths)
	}
	if got, want := productCodes(resp.Products), []string{"C2040", "C5350143"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if resp.TotalCount != 2 {
		t.Fatalf("expected total count 2 after filtering, got %d", resp.TotalCount)
	}
	if resp.ActualTotalCount != 2 {
		t.Fatalf("expected actual total count 2 after filtering, got %d", resp.ActualTotalCount)
	}
	if resp.ParametricQuery {
		t.Fatal("expected ParametricQuery to be false")
	}
}

// modelQueryV3 is a v3 response that classifies the keyword as a product
// model and has no product list. The client then sends the fallback
// request.
const modelQueryV3 = `{
	"code": 200,
	"msg": null,
	"result": {
		"productSearchResultVO": null,
		"tipProductDetailUrlVO": null,
		"exactMatchResult": null,
		"searchEngineProcess": {"judgeSuccessType": ["PRODUCT_MODEL"]}
	}
}`

func TestSearchKeywordFallbackActualTotalCount(t *testing.T) {
	// LCSC caps totalRow at 5000 and sends the real count in
	// actualTotalRow.
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == testSearchV3Path {
			return jsonResponse(http.StatusOK, modelQueryV3), nil
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"totalRow": 5000,
				"actualTotalRow": 939792,
				"dataList": [
					{"productCode": "C8734", "productModel": "STM32F103C8T6"},
					{"productCode": "C8304", "productModel": "STM32F103RCT6"}
				]
			}
		}`), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "STM32F103"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.TotalCount != 5000 {
		t.Fatalf("expected total count 5000, got %d", resp.TotalCount)
	}
	if resp.ActualTotalCount != 939792 {
		t.Fatalf("expected actual total count 939792, got %d", resp.ActualTotalCount)
	}
}

func TestSearchKeywordFallbackDecodesListRowFields(t *testing.T) {
	// Trimmed live /product/query/list row for C1525 under the EUR cookie.
	// List rows send fields that detail responses do not send.
	list := mustReadFixture(t, "query_list_C1525_EUR.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == testSearchV3Path {
			return jsonResponse(http.StatusOK, modelQueryV3), nil
		}
		return jsonResponse(http.StatusOK, list), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "CL05B104KO5NNNC"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Products) != 1 {
		t.Fatalf("expected 1 product, got %v", productCodes(resp.Products))
	}
	if resp.TotalCount != 1 || resp.ActualTotalCount != 1 {
		t.Fatalf("expected counts 1 and 1, got %d and %d", resp.TotalCount, resp.ActualTotalCount)
	}

	p := resp.Products[0]
	if p.ProductID != 1877 || p.BrandID != 254 || p.WmCatalogID != 1142 {
		t.Fatalf("unexpected ids: product %d, brand %d, catalog %d", p.ProductID, p.BrandID, p.WmCatalogID)
	}
	if p.CurrencyType != "" {
		t.Fatalf("expected no currency type on a list row, got %q", p.CurrencyType)
	}
	if got := p.Currency(); got != "EUR" {
		t.Fatalf("expected currency EUR from the price symbol, got %q", got)
	}
	if got := p.ProductPriceList[0].Price(); got != 0.0041 {
		t.Fatalf("expected EUR price 0.0041, got %v", got)
	}
	if !p.HasThirdPartyStock {
		t.Fatal("expected HasThirdPartyStock to be true on the list row")
	}
	if p.HasAlternatePart == nil || !*p.HasAlternatePart {
		t.Fatalf("expected HasAlternatePart to be true, got %v", p.HasAlternatePart)
	}
	if !strings.Contains(p.ProductImageURLBig, "/900x900/") {
		t.Fatalf("unexpected big image URL: %q", p.ProductImageURLBig)
	}
	if p.StockSz+p.StockJs+p.WmStockHk != p.StockNumber {
		t.Fatalf("expected warehouse stock to add up to %d, got %d+%d+%d", p.StockNumber, p.StockSz, p.StockJs, p.WmStockHk)
	}
	if p.FlashSale == nil || p.FlashSale.ValidNumber != 120000 {
		t.Fatalf("unexpected flash sale: %+v", p.FlashSale)
	}
	if p.Lifecycle() != LifecycleActive || !p.AllowsBackorder() {
		t.Fatalf("unexpected lifecycle %q, backorder %v", p.Lifecycle(), p.AllowsBackorder())
	}
}

func TestSearchKeywordFallbackWithoutClassificationDropsUnrelatedRows(t *testing.T) {
	// Older v3 responses have no searchEngineProcess. The client still
	// sends the fallback request, but it must not return the popular parts
	// that /product/query/list sends for "100nF 0402".
	list := mustReadFixture(t, "query_list_100nF_0402.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == testQueryListPath {
			return jsonResponse(http.StatusOK, list), nil
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {"productSearchResultVO": null, "tipProductDetailUrlVO": null}
		}`), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %v", paths)
	}
	if len(resp.Products) != 0 {
		t.Fatalf("expected no products, got %v", productCodes(resp.Products))
	}
	if resp.TotalCount != 0 {
		t.Fatalf("expected total count 0, got %d", resp.TotalCount)
	}
	if resp.ParametricQuery {
		t.Fatal("expected ParametricQuery to be false without a classification")
	}
	if len(resp.QueryTypes) != 0 {
		t.Fatalf("expected no query types, got %v", resp.QueryTypes)
	}
}

func TestSearchKeywordOtherClassificationSkipsFallback(t *testing.T) {
	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": null,
				"tipProductDetailUrlVO": null,
				"searchEngineProcess": {"judgeSuccessType": ["BRAND"]}
			}
		}`), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "muRata"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("expected only the v3 request, got %v", paths)
	}
	if len(resp.Products) != 0 || resp.ParametricQuery {
		t.Fatalf("expected empty non-parametric result, got %+v", resp)
	}
	if want := []string{"BRAND"}; !reflect.DeepEqual(resp.QueryTypes, want) {
		t.Fatalf("expected query types %v, got %v", want, resp.QueryTypes)
	}
}

func TestSearchKeywordDirectMatchUsesFallback(t *testing.T) {
	list := mustReadFixture(t, "query_list_mixed_RP2040.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == testQueryListPath {
			return jsonResponse(http.StatusOK, list), nil
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": null,
				"isToDetail": true,
				"tipProductDetailUrlVO": {"productCode": "C2040"},
				"searchEngineProcess": {"judgeSuccessType": ["PRODUCT_CODE"]}
			}
		}`), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "C2040"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %v", paths)
	}
	if resp.DirectMatchCode != "C2040" {
		t.Fatalf("unexpected direct match: %s", resp.DirectMatchCode)
	}
	if got := productCodes(resp.Products); !reflect.DeepEqual(got, []string{"C2040"}) {
		t.Fatalf("expected [C2040], got %v", got)
	}
}

func TestSearchKeywordDirectCodeDropsModelsThatContainTheCode(t *testing.T) {
	// For the keyword "C2040", LCSC gives a direct match and no
	// classification. The live fallback list also has two unrelated parts
	// whose model contains "C2040".
	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == testQueryListPath {
			return jsonResponse(http.StatusOK, `{
				"code": 200,
				"msg": null,
				"result": {
					"totalRow": 3,
					"dataList": [
						{"productCode": "C2040", "productModel": "RP2040"},
						{"productCode": "C575598", "productModel": "PI6C20400BLEX"},
						{"productCode": "C51908978", "productModel": "G823003271C2040CY"}
					]
				}
			}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"code": 200,
			"msg": null,
			"result": {
				"productSearchResultVO": null,
				"tipProductDetailUrlVO": {"productCode": "C2040"},
				"searchEngineProcess": {"judgeSuccessType": null}
			}
		}`), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "c2040"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 requests, got %v", paths)
	}
	if got := productCodes(resp.Products); !reflect.DeepEqual(got, []string{"C2040"}) {
		t.Fatalf("expected [C2040], got %v", got)
	}
	if resp.TotalCount != 1 {
		t.Fatalf("expected total count 1 after filtering, got %d", resp.TotalCount)
	}
}

func TestIsParametricQuery(t *testing.T) {
	tests := []struct {
		name  string
		types []string
		want  bool
	}{
		{"no classification", nil, false},
		{"model", []string{"PRODUCT_MODEL"}, false},
		{"package and parameter", []string{"STANDARD", "PRODUCT_PARAM"}, true},
		{"package only", []string{"STANDARD"}, true},
		{"lower case parameter", []string{"product_param"}, true},
		{"model wins over parameter", []string{"PRODUCT_PARAM", "PRODUCT_MODEL"}, false},
		{"brand", []string{"BRAND"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isParametricQuery(tt.types); got != tt.want {
				t.Fatalf("isParametricQuery(%v) = %v, want %v", tt.types, got, tt.want)
			}
		})
	}
}

func TestFilterRelatedProducts(t *testing.T) {
	products := []Product{
		{ProductCode: "C2040", ProductModel: "RP2040"},
		{ProductCode: "C5350143", ProductModel: "RP2040-Zero"},
		{ProductCode: "C20401", ProductModel: "OTHER"},
		{ProductCode: "C1591", ProductModel: "CL10B104KB8NNNC"},
		{ProductCode: "C404027", ProductModel: "TLV75533PDBVR"},
	}

	tests := []struct {
		name    string
		keyword string
		direct  string
		want    []string
	}{
		{"model substring", "RP2040", "", []string{"C2040", "C5350143"}},
		{"ignores case and spaces", "rp2040 zero", "", []string{"C5350143"}},
		{"ignores dots", "RP2040.ZERO", "", []string{"C5350143"}},
		{"ignores dashes", "rp-2040-zero", "", []string{"C5350143"}},
		{"code must be equal", "c2040", "", []string{"C2040"}},
		{"code prefix does not match", "C204", "", nil},
		{"direct match code", "TLV-X", "C404027", []string{"C404027"}},
		{"model keyword with direct match", "RP2040", "C2040", []string{"C2040", "C5350143"}},
		{"parametric keyword", "100nF 0402", "", nil},
		{"empty after normalization", "- . -", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterRelatedProducts(products, tt.keyword, tt.direct)
			if codes := productCodes(got); len(codes) != len(tt.want) || (len(codes) > 0 && !reflect.DeepEqual(codes, tt.want)) {
				t.Fatalf("filterRelatedProducts(%q, %q) = %v, want %v", tt.keyword, tt.direct, codes, tt.want)
			}
		})
	}
}

func TestFilterRelatedProductsKeywordIsDirectCode(t *testing.T) {
	// Rows from a live /product/query/list response for "C2040".
	products := []Product{
		{ProductCode: "C2040", ProductModel: "RP2040"},
		{ProductCode: "C575598", ProductModel: "PI6C20400BLEX"},
		{ProductCode: "C51908978", ProductModel: "G823003271C2040CY"},
	}

	tests := []struct {
		name    string
		keyword string
		direct  string
		want    []string
	}{
		{"keyword is the direct match code", "C2040", "C2040", []string{"C2040"}},
		{"keyword case differs from the code", "c2040", "C2040", []string{"C2040"}},
		{"no direct match keeps model matches", "C2040", "", []string{"C2040", "C575598", "C51908978"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := productCodes(filterRelatedProducts(products, tt.keyword, tt.direct))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("filterRelatedProducts(%q, %q) = %v, want %v", tt.keyword, tt.direct, got, tt.want)
			}
		})
	}
}
