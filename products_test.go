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

	product, err := client.Product.Details(context.Background(), "C8734")
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
	// Trimmed live response for C1525 with five alternates.
	fixture := mustReadFixture(t, "product_detail_C1525.json")
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
	fixture := mustReadFixture(t, "product_detail_C1525.json")
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
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached product differs from the decoded product:\n%+v\n%+v", first, second)
	}
}
