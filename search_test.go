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

func TestSearchKeywordParametricQueryUsesRouteCategories(t *testing.T) {
	// Live response for "100nF 0402". LCSC classifies the keyword as a
	// package and parameter query, returns no product list and names the
	// leaf category 1142 (Ceramic Capacitors).
	v3 := mustReadFixture(t, "search_v3_parametric_100nF_0402.json")
	list := mustReadFixture(t, "query_list_1142_100nF_0402.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case testSearchV3Path:
			return jsonResponse(http.StatusOK, v3), nil
		case testQueryListPath:
			body := decodeJSONBody(t, req)
			want := map[string]interface{}{
				"keyword":           "",
				"globalKeyword":     "100nF 0402",
				"scene":             "FULL_MATCH",
				"catalogIdList":     []interface{}{float64(1142)},
				"brandIdList":       []interface{}{},
				"encapValueList":    []interface{}{},
				"isStock":           false,
				"isOtherSuppliers":  false,
				"isAsianBrand":      false,
				"isDeals":           false,
				"isRohsCert":        false,
				"paramNameValueMap": map[string]interface{}{},
				"currentPage":       float64(1),
				"pageSize":          float64(25),
			}
			if !reflect.DeepEqual(body, want) {
				t.Errorf("unexpected list body:\n got %v\nwant %v", body, want)
			}
			return jsonResponse(http.StatusOK, list), nil
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusNotFound, ""), nil
		}
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if want := []string{testSearchV3Path, testQueryListPath}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
	if !resp.ParametricQuery {
		t.Fatal("expected ParametricQuery to be true")
	}
	if got, want := productCodes(resp.Products), []string{"C60474", "C77020"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected products %v, got %v", want, got)
	}
	if resp.TotalCount != 211 || resp.ActualTotalCount != 211 {
		t.Fatalf("expected counts 211 and 211, got %d and %d", resp.TotalCount, resp.ActualTotalCount)
	}
	if want := []string{"STANDARD", "PRODUCT_PARAM"}; !reflect.DeepEqual(resp.QueryTypes, want) {
		t.Fatalf("expected query types %v, got %v", want, resp.QueryTypes)
	}
}

func TestSearchKeywordParametricQueryWithoutCategory(t *testing.T) {
	// Live v3 response with scene NO_RESULT for a parameter query.
	v3 := mustReadFixture(t, "search_v3_no_result.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "4.7uF 0805 25V"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected only the v3 request, got %v", paths)
	}
	if !resp.ParametricQuery || len(resp.Products) != 0 || resp.TotalCount != 0 {
		t.Fatalf("expected an empty parametric result, got %+v", resp)
	}
}

func TestSearchKeywordParametricResultIsCached(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_100nF_0402.json")
	list := mustReadFixture(t, "query_list_1142_100nF_0402.json")

	var calls int32
	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			if req.URL.Path == testQueryListPath {
				return jsonResponse(http.StatusOK, list), nil
			}
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

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected the v3 and the list request only one time, got %d requests", got)
	}
	if !resp.ParametricQuery {
		t.Fatal("expected cached response to keep ParametricQuery")
	}
	if len(resp.Products) != 2 {
		t.Fatalf("expected cached response to keep 2 products, got %v", productCodes(resp.Products))
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

func TestCacheKeyForSearchKeepsCase(t *testing.T) {
	if cacheKeyForSearch("USD", "1m 0603", false) != cacheKeyForSearch("usd", " 1m 0603 ", false) {
		t.Fatal("expected the same key for the same keyword with spaces at the ends")
	}
	if cacheKeyForSearch("USD", "1m 0603", false) == cacheKeyForSearch("USD", "1M 0603", false) {
		t.Fatal("expected different keys for keywords with a different case")
	}
	if cacheKeyForSearch("USD", "1m 0603", false) == cacheKeyForSearch("EUR", "1m 0603", false) {
		t.Fatal("expected different keys for different currencies")
	}
	if cacheKeyForSearch("USD", "1m 0603", true) == cacheKeyForSearch("USD", "1m 0603", false) {
		t.Fatal("expected different keys with and without SkipParametricList")
	}
}

// countingParametricClient returns a client with a cache that answers the v3
// request and the list request for "10k 0603". It records the path and the
// body of each request.
func countingParametricClient(t *testing.T, cache Cache, paths *[]string, bodies *[]map[string]interface{}) *Client {
	t.Helper()
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")
	return newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		*paths = append(*paths, req.URL.Path)
		*bodies = append(*bodies, decodeJSONBody(t, req))
		switch req.URL.Path {
		case testSearchV3Path:
			return jsonResponse(http.StatusOK, v3), nil
		case testQueryListPath:
			return jsonResponse(http.StatusOK, list), nil
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusNotFound, ""), nil
		}
	})
}

func TestSearchKeywordThenParametricRequestCount(t *testing.T) {
	const keyword = "10k 0603"
	ownOptions := &ParametricOptions{MaxCatalogs: 1, InStock: true, Sort: SortStock, Desc: true, PageSize: 50}

	tests := []struct {
		name      string
		skipList  bool
		opts      *ParametricOptions
		wantPaths []string
	}{
		// Parametric gets the route and the list from the cache.
		{"default options", false, nil, []string{testSearchV3Path, testQueryListPath}},
		// Parametric gets the route from the cache and sends its own list.
		{"skip list and own options", true, ownOptions, []string{testSearchV3Path, testQueryListPath}},
		// Keyword sends the default list. Parametric sends its own list.
		{"own options", false, ownOptions, []string{testSearchV3Path, testQueryListPath, testQueryListPath}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			var bodies []map[string]interface{}
			cache := newRecordingCache()
			client := countingParametricClient(t, cache, &paths, &bodies)
			defer func() { _ = client.Close() }()

			ctx := context.Background()
			resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: keyword, SkipParametricList: tt.skipList})
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if !resp.ParametricQuery {
				t.Fatal("expected ParametricQuery to be true")
			}
			if resp.Route == nil || resp.Route.Scene != SceneFullMatch || resp.Route.Keyword != keyword {
				t.Fatalf("expected the route in the response, got %+v", resp.Route)
			}
			if got, want := resp.Route.LeafCatalogIDs(0), []int{1199, 1272, 1200}; !reflect.DeepEqual(got, want) {
				t.Fatalf("expected leaf ids %v, got %v", want, got)
			}
			if _, ok := cache.Get(cacheKeyForRoute("USD", keyword)); !ok {
				t.Fatal("expected Keyword to store the route under the route cache key")
			}
			if tt.skipList {
				if len(resp.Products) != 0 || resp.TotalCount != 0 || resp.CatalogIDs != nil {
					t.Fatalf("expected no products and no list ids, got %v and %v", productCodes(resp.Products), resp.CatalogIDs)
				}
				if want := []string{testSearchV3Path}; !reflect.DeepEqual(paths, want) {
					t.Fatalf("expected only the v3 request, got %v", paths)
				}
			} else {
				if len(resp.Products) != 3 || resp.ActualTotalCount != 181 {
					t.Fatalf("expected 3 products and 181 in total, got %d and %d", len(resp.Products), resp.ActualTotalCount)
				}
				if want := []int{1199, 1272, 1200}; !reflect.DeepEqual(resp.CatalogIDs, want) {
					t.Fatalf("expected list ids %v, got %v", want, resp.CatalogIDs)
				}
			}

			list, err := client.Search.Parametric(ctx, keyword, tt.opts)
			if err != nil {
				t.Fatalf("parametric failed: %v", err)
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Fatalf("expected requests %v, got %v", tt.wantPaths, paths)
			}
			if list.Route == nil || list.Route.Scene != SceneFullMatch || len(list.Products) != 3 {
				t.Fatalf("unexpected parametric response: route %v, %d products", list.Route, len(list.Products))
			}
			if tt.opts != nil {
				last := bodies[len(bodies)-1]
				if want := []interface{}{float64(1199)}; !reflect.DeepEqual(last["catalogIdList"], want) {
					t.Fatalf("expected the list request with the best category %v, got %v", want, last["catalogIdList"])
				}
				if last["isStock"] != true || last["sortField"] != "stock" || last["pageSize"] != float64(50) {
					t.Fatalf("expected the list request with the own options, got %v", last)
				}
			}
		})
	}
}

func TestSearchParametricThenKeywordUsesCachedRoute(t *testing.T) {
	var paths []string
	var bodies []map[string]interface{}
	client := countingParametricClient(t, newRecordingCache(), &paths, &bodies)
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if _, err := client.Search.Parametric(ctx, "10k 0603", nil); err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "10k 0603"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if want := []string{testSearchV3Path, testQueryListPath}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
	if resp.Route == nil || !resp.ParametricQuery || len(resp.Products) != 3 {
		t.Fatalf("unexpected response: route %v, parametric %v, %d products", resp.Route, resp.ParametricQuery, len(resp.Products))
	}
}

func TestSearchKeywordSkipParametricListHasOwnCacheEntry(t *testing.T) {
	var paths []string
	var bodies []map[string]interface{}
	client := countingParametricClient(t, newRecordingCache(), &paths, &bodies)
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	skip := &SearchRequest{Keyword: "10k 0603", SkipParametricList: true}
	if resp, err := client.Search.Keyword(ctx, skip); err != nil || len(resp.Products) != 0 {
		t.Fatalf("expected no products, got %v (error %v)", resp, err)
	}

	// The response without the list must not answer a request that wants
	// the list.
	full, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "10k 0603"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(full.Products) != 3 {
		t.Fatalf("expected 3 products, got %v", productCodes(full.Products))
	}

	again, err := client.Search.Keyword(ctx, skip)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(again.Products) != 0 || again.Route == nil {
		t.Fatalf("expected the cached response without products, got %v", productCodes(again.Products))
	}
	if want := []string{testSearchV3Path, testQueryListPath}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
}

func TestSearchKeywordSkipParametricListWithoutCache(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_100nF_0402.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "100nF 0402", SkipParametricList: true})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if want := []string{testSearchV3Path}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected only the v3 request, got %v", paths)
	}
	if !resp.ParametricQuery || len(resp.Products) != 0 || resp.Route == nil {
		t.Fatalf("unexpected response: parametric %v, %d products, route %v", resp.ParametricQuery, len(resp.Products), resp.Route)
	}
	if got := resp.Route.LeafCatalogIDs(0); !reflect.DeepEqual(got, []int{1142}) {
		t.Fatalf("expected the leaf id 1142, got %v", got)
	}
}

func TestSearchKeywordSkipParametricListKeepsOtherResults(t *testing.T) {
	// The field changes only parametric queries. A model keyword still
	// gives the exact matches.
	v3 := mustReadFixture(t, "search_v3_model_RP2040.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "RP2040", SkipParametricList: true})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.ParametricQuery || len(resp.Products) == 0 || resp.Route == nil || resp.CatalogIDs != nil {
		t.Fatalf("unexpected response: parametric %v, products %v, route %v, ids %v", resp.ParametricQuery, productCodes(resp.Products), resp.Route, resp.CatalogIDs)
	}
}

func TestSearchKeywordCacheEntryWithoutRouteIsIgnored(t *testing.T) {
	// A cache entry of v1.2.0 has no route. Keyword sends the requests
	// again, so that Route is not nil.
	var paths []string
	var bodies []map[string]interface{}
	cache := newRecordingCache()
	cache.Set(cacheKeyForSearch("USD", "10k 0603", false), []byte(`{"Products":null,"ParametricQuery":true}`), time.Minute)
	client := countingParametricClient(t, cache, &paths, &bodies)
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "10k 0603"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Route == nil || len(resp.Products) != 3 {
		t.Fatalf("expected a new response with the route, got route %v and %d products", resp.Route, len(resp.Products))
	}
	if want := []string{testSearchV3Path, testQueryListPath}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
}
