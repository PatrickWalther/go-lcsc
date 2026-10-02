//go:build integration
// +build integration

package lcsc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIntegrationSearchKeyword(t *testing.T) {
	client := NewClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "STM32F103"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if len(resp.Products) == 0 {
		t.Fatal("expected at least one product")
	}
}

func TestIntegrationSearchModelUsesExactMatch(t *testing.T) {
	client := NewClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "RP2040"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	found := false
	for _, p := range resp.Products {
		if p.ProductCode == "C2040" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected C2040 in results, got %d products (query types %v)", len(resp.Products), resp.QueryTypes)
	}
}

func TestIntegrationSearchParametricQuery(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	t.Logf("query types %v, parametric %v, %d products (actual total %d)", resp.QueryTypes, resp.ParametricQuery, len(resp.Products), resp.ActualTotalCount)
	if resp.ParametricQuery && len(resp.Products) == 0 {
		t.Fatal("expected products from the route categories for a parametric query")
	}
}

func TestIntegrationSearchRegressionPartNumberSuggestion(t *testing.T) {
	client := NewClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{
		Keyword: "CGJ2B2C0G1H390J050BA",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp == nil || len(resp.Products) == 0 {
		t.Fatal("expected at least one result for CGJ2B2C0G1H390J050BA")
	}
}

func TestIntegrationProductDetails(t *testing.T) {
	client := NewClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "STM32F103"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp == nil || len(resp.Products) == 0 {
		t.Fatal("expected at least one product from search")
	}

	product, err := client.Product.Details(ctx, resp.Products[0].ProductCode)
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if product.ProductCode == "" {
		t.Fatal("expected product code")
	}
}

// TestIntegrationProductDetailsCurrencyEUR checks that LCSC sends
// productPrice in USD and currencyPrice in EUR under the EUR cookie.
func TestIntegrationProductDetailsCurrencyEUR(t *testing.T) {
	client := NewClient(WithCurrency("EUR"), WithoutCache())
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	product, err := client.Product.Details(ctx, "C2040")
	t.Logf("detail C2040 (EUR): %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}

	// The id of C2040 has not changed in the observed data.
	if product.ProductID != 2392 {
		t.Errorf("expected product id 2392, got %d", product.ProductID)
	}
	if got := product.Currency(); got != "EUR" {
		t.Fatalf("expected currency EUR, got %q (currencyType %q)", got, product.CurrencyType)
	}
	if len(product.ProductPriceList) == 0 {
		t.Fatal("expected price breaks")
	}
	for _, pb := range product.ProductPriceList {
		if pb.CurrencyPrice <= 0 || pb.CurrencyPrice == pb.ProductPrice {
			t.Fatalf("ladder %d: expected a EUR price different from the USD price, got currencyPrice %v and productPrice %v", pb.Ladder, pb.CurrencyPrice, pb.ProductPrice)
		}
		if pb.Price() != float64(pb.CurrencyPrice) {
			t.Fatalf("ladder %d: expected Price to return currencyPrice %v, got %v", pb.Ladder, pb.CurrencyPrice, pb.Price())
		}
		if pb.USDPrice != pb.ProductPrice {
			t.Errorf("ladder %d: expected usdPrice %v to equal productPrice %v", pb.Ladder, pb.USDPrice, pb.ProductPrice)
		}
	}
}

// TestIntegrationAlternatesC1525 checks the cross-reference alternates
// endpoint. C1525 had 99 alternates with the match types 4, 5 and 6.
func TestIntegrationAlternatesC1525(t *testing.T) {
	client := NewClient(WithoutCache())
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.Alternates.List(ctx, &AlternatesRequest{ProductCode: "C1525"})
	t.Logf("alternates C1525: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("alternates failed: %v", err)
	}

	if resp.Original.ProductCode != "C1525" || resp.Original.ProductID != 1877 {
		t.Fatalf("unexpected original: %s (%d)", resp.Original.ProductCode, resp.Original.ProductID)
	}
	if len(resp.Alternates) == 0 {
		t.Fatal("expected alternates for C1525")
	}
	if resp.TotalCount < len(resp.Alternates) || resp.InStockCount > resp.TotalCount {
		t.Fatalf("unexpected counts: %d alternates, total %d, in stock %d", len(resp.Alternates), resp.TotalCount, resp.InStockCount)
	}

	labels := map[string]int{}
	var direct *Product
	for i := range resp.Alternates {
		alt := &resp.Alternates[i]
		if alt.ProductCode == "" || alt.ProductID == 0 || alt.Match() == "" {
			t.Fatalf("alternate %d: expected a code, an id and a match type, got %q %d %q", i, alt.ProductCode, alt.ProductID, alt.MatchType)
		}
		labels[alt.Match().Label()]++
		if direct == nil && alt.Match() == MatchTypeDirect {
			direct = alt
		}
	}
	t.Logf("%d alternates (total %d, in stock %d), labels %v", len(resp.Alternates), resp.TotalCount, resp.InStockCount, labels)
	if labels["Direct"]+labels["Upgrade"] == 0 {
		t.Fatalf("expected at least one alternate with match type 5 or 6, got labels %v", labels)
	}
	if direct != nil {
		t.Logf("parameter differences for %s: %+v", direct.ProductCode, DiffParameters(&resp.Original, direct))
	}
}

// TestIntegrationAlternatesPageSizeLimit checks that LCSC answers a page
// size above 100 with envelope code 405. AlternateService.List refuses such
// a page size before the request, so this test calls the transport
// directly.
func TestIntegrationAlternatesPageSizeLimit(t *testing.T) {
	client := NewClient(WithoutCache())
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := client.Alternates.List(ctx, &AlternatesRequest{ProductCode: "C1525", PageSize: 101}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected List to refuse page size 101, got %v", err)
	}

	params := url.Values{}
	params.Set("productCode", "C1525")
	params.Set("inStockOnly", "false")
	params.Set("currentPage", "1")
	params.Set("pageSize", "101")

	start := time.Now()
	var wrapper alternatesWrapper
	err := client.do(ctx, http.MethodGet, "/product/alternate/part/list", params, nil, &wrapper)
	t.Logf("alternates C1525 page size 101: %v, error %v", time.Since(start), err)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest (code 405) for page size 101, got %v", err)
	}
}

// newPoliteIntegrationClient returns a client without cache that sends at
// most one request per second. It waits one second first, so that the
// previous test does not send a request in the same second.
func newPoliteIntegrationClient() *Client {
	time.Sleep(time.Second)
	return NewClient(WithoutCache(), WithRateLimit(1))
}

// TestIntegrationSearchList1199 checks the parametric list body of the
// LCSC site: "10k 0603" as a global keyword in leaf category 1199.
func TestIntegrationSearchList1199(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.Search.List(ctx, &ListRequest{
		Filter:   Filter{GlobalKeyword: "10k 0603", CatalogIDs: []int{1199}, InStock: true},
		Sort:     SortStock,
		Desc:     true,
		PageSize: 10,
	})
	t.Logf("list 1199 \"10k 0603\": %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	t.Logf("%d rows, total %d, actual total %d", len(resp.Products), resp.TotalCount, resp.ActualTotal)
	if len(resp.Products) == 0 {
		t.Fatal("expected rows for \"10k 0603\" in category 1199")
	}
	if resp.ActualTotal < resp.TotalCount || resp.TotalCount < len(resp.Products) {
		t.Fatalf("unexpected counts: %d rows, total %d, actual total %d", len(resp.Products), resp.TotalCount, resp.ActualTotal)
	}
	for _, p := range resp.Products {
		if p.WmCatalogID != 1199 || p.ProductID == 0 {
			t.Fatalf("expected rows in category 1199 with an id, got %s in %d (id %d)", p.ProductCode, p.WmCatalogID, p.ProductID)
		}
	}
}

// TestIntegrationSearchListPageSizeLimit checks that LCSC answers a page
// size above 100 with envelope code 405. SearchService.List refuses such a
// page size before the request, so this test calls the transport directly.
func TestIntegrationSearchListPageSizeLimit(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req := &ListRequest{Filter: Filter{GlobalKeyword: "10k 0603", CatalogIDs: []int{1199}}, PageSize: 101}
	if _, err := client.Search.List(ctx, req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected List to refuse page size 101, got %v", err)
	}

	body, err := newQueryListBody(&req.Filter)
	if err != nil {
		t.Fatalf("body failed: %v", err)
	}
	body.CurrentPage = 1
	body.PageSize = 101

	start := time.Now()
	var list productListWrapper
	err = client.do(ctx, http.MethodPost, "/product/query/list", nil, body, &list)
	t.Logf("list 1199 page size 101: %v, error %v", time.Since(start), err)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest (code 405) for page size 101, got %v", err)
	}
}

// TestIntegrationSearchParametric100nF0402 checks that a parameter query
// gives products through the route categories.
func TestIntegrationSearchParametric100nF0402(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.Search.Parametric(ctx, "100nF 0402", nil)
	t.Logf("parametric \"100nF 0402\": %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if resp.Route == nil {
		t.Fatal("expected the route in the response")
	}
	t.Logf("scene %q, query types %v, categories %v, %d products, actual total %d",
		resp.Route.Scene, resp.Route.QueryTypes, resp.CatalogIDs, len(resp.Products), resp.ActualTotal)
	if len(resp.Products) == 0 {
		t.Fatal("expected at least one product for \"100nF 0402\"")
	}
	if !resp.Route.IsParametric() {
		t.Errorf("expected a parametric classification, got %v", resp.Route.QueryTypes)
	}
}

// TestIntegrationSearchFacets1199 checks the facets of "10k 0603" in leaf
// category 1199.
func TestIntegrationSearchFacets1199(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	facets, err := client.Search.Facets(ctx, &Filter{GlobalKeyword: "10k 0603", CatalogIDs: []int{1199}, InStock: true})
	t.Logf("facets 1199 \"10k 0603\": %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}
	t.Logf("total %d, packages %v, %d manufacturers, %d parameters", facets.TotalCount, facets.Packages, len(facets.Manufacturers), len(facets.Params))
	if facets.TotalCount == 0 || len(facets.Manufacturers) == 0 || facets.Manufacturers[0].ID == 0 {
		t.Fatalf("expected products and manufacturers with ids, got %+v", facets)
	}
	resistance := facets.Param("Resistance")
	if resistance == nil || len(resistance.Equivalents("10kΩ")) == 0 {
		t.Fatalf("expected the value 10kΩ in the Resistance facet, got %+v", resistance)
	}
}

// TestIntegrationCatalogTree checks the category tree and the child
// counts of the Capacitors category.
func TestIntegrationCatalogTree(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	tree, err := client.Catalog.Tree(ctx)
	t.Logf("category tree: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("tree failed: %v", err)
	}
	for _, root := range tree {
		if root.ID == mroRootCategoryID {
			t.Fatal("expected Tree to remove root 1729")
		}
	}
	path := findCategoryPath(tree, 1199, nil)
	if len(path) != 3 || path[0].ID != 30 || path[1].ID != 501 || path[2].ID != 1199 {
		t.Fatalf("expected path 30 > 501 > 1199, got %v", path)
	}
	capacitors := findCategory(tree, 495)
	if capacitors == nil || len(capacitors.LeafIDs()) == 0 {
		t.Fatal("expected leaf categories under 495")
	}
	t.Logf("%d roots, %d leaves under 495", len(tree), len(capacitors.LeafIDs()))

	start = time.Now()
	counts, err := client.Catalog.ChildCounts(ctx, 495)
	t.Logf("child counts 495: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("child counts failed: %v", err)
	}
	found := false
	for _, c := range counts {
		if c.ID == 1142 && c.ProductCount > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected category 1142 with products in %+v", counts)
	}
}
