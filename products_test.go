package lcsc

import (
	"context"
	"errors"
	"net/http"
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
		if req.URL.Path != "/ftps/wm/search/v2/global" {
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
