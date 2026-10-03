//go:build integration
// +build integration

package lcsc

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
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
	t.Logf("query types %v, parametric %v, %d products (actual total %d), list categories %v", resp.QueryTypes, resp.ParametricQuery, len(resp.Products), resp.ActualTotalCount, resp.CatalogIDs)
	if resp.Route == nil {
		t.Fatal("expected the route in the response")
	}
	if resp.ParametricQuery && len(resp.Products) == 0 {
		t.Fatal("expected products from the route categories for a parametric query")
	}
	if resp.ParametricQuery && len(resp.CatalogIDs) == 0 {
		t.Fatal("expected the leaf category ids of the list request")
	}
}

// TestIntegrationSearchKeywordThenParametricRequestCount checks that
// Keyword with SkipParametricList and then Parametric with other options
// send 2 requests for one keyword: the v3 request and one list request.
// The client does not retry, so a retry cannot change the count. A
// rate-limited request fails with its own error.
func TestIntegrationSearchKeywordThenParametricRequestCount(t *testing.T) {
	time.Sleep(time.Second)
	var requests int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&requests, 1)
		return http.DefaultTransport.RoundTrip(req)
	})
	client := NewClient(
		WithHTTPClient(&http.Client{Transport: transport, Timeout: 30 * time.Second}),
		WithRateLimit(1),
		WithoutRetry(),
	)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "100nF 0402", SkipParametricList: true})
	t.Logf("keyword \"100nF 0402\": %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !resp.ParametricQuery || resp.Route == nil {
		t.Fatalf("expected a parametric query with a route, got parametric %v and route %v", resp.ParametricQuery, resp.Route)
	}
	if len(resp.Products) != 0 {
		t.Fatalf("expected no products with SkipParametricList, got %d", len(resp.Products))
	}

	start = time.Now()
	list, err := client.Search.Parametric(ctx, "100nF 0402", &ParametricOptions{MaxCatalogs: 1, InStock: true, Sort: SortStock, Desc: true, PageSize: 10})
	t.Logf("parametric \"100nF 0402\": %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(list.Products) == 0 {
		t.Fatal("expected at least one product for \"100nF 0402\"")
	}
	if want := resp.Route.LeafCatalogIDs(1); !reflect.DeepEqual(list.CatalogIDs, want) {
		t.Errorf("expected the best category %v of the route, got %v", want, list.CatalogIDs)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
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
		if amount, currency := pb.PriceIn(product.Currency()); amount != float64(pb.CurrencyPrice) || currency != "EUR" {
			t.Fatalf("ladder %d: expected PriceIn to return %v EUR, got %v %s", pb.Ladder, pb.CurrencyPrice, amount, currency)
		}
		if pb.USDPrice != pb.ProductPrice {
			t.Errorf("ladder %d: expected usdPrice %v to equal productPrice %v", pb.Ladder, pb.USDPrice, pb.ProductPrice)
		}
	}
}

// TestIntegrationProductDetailsLowerCaseCode checks that Details finds a
// product for a lower-case code. LCSC itself finds no product for
// "c2040", so the client must send the code in upper case.
func TestIntegrationProductDetailsLowerCaseCode(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	product, err := client.Product.Details(ctx, "c2040")
	t.Logf("detail c2040: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("details failed: %v", err)
	}
	if product.ProductCode != "C2040" {
		t.Fatalf("expected C2040, got %q", product.ProductCode)
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
	if resp.TotalCount < len(resp.Alternates) || resp.InStockCount > resp.ActualTotalCount || resp.ActualTotalCount < resp.TotalCount {
		t.Fatalf("unexpected counts: %d alternates, total %d, actual total %d, in stock %d",
			len(resp.Alternates), resp.TotalCount, resp.ActualTotalCount, resp.InStockCount)
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
	t.Logf("%d alternates (total %d, actual total %d, in stock %d), labels %v",
		len(resp.Alternates), resp.TotalCount, resp.ActualTotalCount, resp.InStockCount, labels)
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
	t.Logf("%d rows, total %d, actual total %d", len(resp.Products), resp.TotalCount, resp.ActualTotalCount)
	if len(resp.Products) == 0 {
		t.Fatal("expected rows for \"10k 0603\" in category 1199")
	}
	if resp.ActualTotalCount < resp.TotalCount || resp.TotalCount < len(resp.Products) {
		t.Fatalf("unexpected counts: %d rows, total %d, actual total %d", len(resp.Products), resp.TotalCount, resp.ActualTotalCount)
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

// TestIntegrationSearchListRowCap checks the 5000-row cap of
// /product/query/list. LCSC refuses a page that ends after row 5000, also
// when the page starts before row 5000. At page size 30, page 166 (rows
// 4951-4980) is the last page that LCSC accepts, and page 167 (rows
// 4981-5010) gives code 405. SearchService.List refuses page 167 before
// the request, so this test calls the transport directly.
func TestIntegrationSearchListRowCap(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	filter := Filter{CatalogIDs: []int{1199}}
	if _, err := client.Search.List(ctx, &ListRequest{Filter: filter, Page: 167, PageSize: 30}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected List to refuse page 167 at page size 30, got %v", err)
	}

	send := func(page int) (*productListWrapper, error) {
		body, err := newQueryListBody(&filter)
		if err != nil {
			t.Fatalf("body failed: %v", err)
		}
		body.CurrentPage = page
		body.PageSize = 30
		start := time.Now()
		var list productListWrapper
		err = client.do(ctx, http.MethodPost, "/product/query/list", nil, body, &list)
		t.Logf("list 1199 page %d at page size 30: %v, error %v", page, time.Since(start), err)
		return &list, err
	}

	list, err := send(166)
	if err != nil {
		t.Fatalf("expected page 166 to work, got %v", err)
	}
	t.Logf("page 166: %d rows, totalRow %d, actualTotalRow %d", len(list.DataList), list.TotalRow, list.ActualTotalRow)
	if len(list.DataList) == 0 || list.TotalRow != maxListRows {
		t.Fatalf("expected rows and totalRow %d, got %d rows and totalRow %d", maxListRows, len(list.DataList), list.TotalRow)
	}

	if _, err := send(167); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest (code 405) for page 167, got %v", err)
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
		resp.Route.Scene, resp.Route.QueryTypes, resp.CatalogIDs, len(resp.Products), resp.ActualTotalCount)
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

// TestIntegrationThirdPartyOffersC8734 checks the marketplace offer
// endpoints. C8734 had 7 offers with 3,000 to 120,000 pieces and a
// delivery time of 3 to 15 days.
func TestIntegrationThirdPartyOffersC8734(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.ThirdParty.Offers(ctx, &OffersRequest{ProductCode: "C8734"})
	t.Logf("offers C8734: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("offers failed: %v", err)
	}
	if len(resp.Offers) == 0 {
		t.Fatal("expected offers for C8734")
	}
	if resp.TotalCount < len(resp.Offers) {
		t.Fatalf("unexpected total %d for %d offers", resp.TotalCount, len(resp.Offers))
	}

	sources := map[string]int{}
	for i := range resp.Offers {
		offer := &resp.Offers[i]
		sources[offer.Source]++
		minDays, maxDays, ok := offer.DeliveryDays()
		if offer.ProductCode != "C8734" || offer.StockNumber <= 0 || offer.MinBuyNumber <= 0 || !ok || minDays > maxDays {
			t.Fatalf("offer %d: unexpected values: code %q, stock %d, moq %d, days %d-%d (%v)",
				i, offer.ProductCode, offer.StockNumber, offer.MinBuyNumber, minDays, maxDays, ok)
		}
		if len(offer.ProductPriceList) == 0 || offer.ProductPriceList[0].Price() <= 0 || offer.ProductPriceList[0].USDPrice <= 0 {
			t.Fatalf("offer %d: expected a price ladder with currencyPrice and usdPrice, got %+v", i, offer.ProductPriceList)
		}
		if offer.Currency() != "USD" {
			t.Fatalf("offer %d: expected USD, got %q", i, offer.Currency())
		}
		t.Logf("offer %d: %s, stock %d, moq %d, split %d, %d-%d days, %d breaks, first %.4f USD",
			i, offer.Source, offer.StockNumber, offer.MinBuyNumber, offer.Split, minDays, maxDays,
			len(offer.ProductPriceList), offer.ProductPriceList[0].Price())
	}
	t.Logf("%d offers (total %d), sources %v", len(resp.Offers), resp.TotalCount, sources)
	if n := countOfferBadges(resp.Offers); n == 0 {
		t.Fatal("expected badges for a ProductCode request")
	}

	start = time.Now()
	hasStock, err := client.ThirdParty.HasStock(ctx, "C8734")
	t.Logf("has third-party stock C8734: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("has stock failed: %v", err)
	}
	if !hasStock {
		t.Fatal("expected third-party stock for C8734")
	}
}

// TestIntegrationResolveDatasheetURLC1525 checks that the datasheet viewer
// page of C1525 gives a URL that sends a PDF file.
func TestIntegrationResolveDatasheetURLC1525(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const viewerURL = "https://www.lcsc.com/datasheet/C1525.pdf"
	start := time.Now()
	pdfURL, err := client.Product.ResolveDatasheetURL(ctx, viewerURL)
	t.Logf("resolve %s: %v, error %v", viewerURL, time.Since(start), err)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	t.Logf("datasheet URL %s", pdfURL)
	if !strings.HasPrefix(pdfURL, "https://datasheet.lcsc.com/datasheet/pdf/") || !strings.Contains(pdfURL, "C1525") {
		t.Fatalf("unexpected datasheet URL %q", pdfURL)
	}

	// Read only the first bytes of the file.
	time.Sleep(time.Second)
	checkPDFRange(ctx, t, pdfURL)
}

// countOfferBadges returns the number of badges in offers.
func countOfferBadges(offers []Offer) int {
	n := 0
	for i := range offers {
		for _, badge := range []bool{offers[i].IsPriceFirst, offers[i].IsStockFirst, offers[i].IsDeliveryTimeFirst} {
			if badge {
				n++
			}
		}
	}
	return n
}

// TestIntegrationThirdPartyOffersKeywordHasNoBadges checks that LCSC sets
// no offer badges for a Keyword request. The keyword gives the offers of
// C8734, which have badges for a ProductCode request.
func TestIntegrationThirdPartyOffersKeywordHasNoBadges(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.ThirdParty.Offers(ctx, &OffersRequest{Keyword: "STM32F103C8T6"})
	t.Logf("offers STM32F103C8T6: %v, error %v", time.Since(start), err)
	if err != nil {
		t.Fatalf("offers failed: %v", err)
	}
	t.Logf("%d offers (total %d)", len(resp.Offers), resp.TotalCount)
	if len(resp.Offers) == 0 {
		t.Fatal("expected offers for STM32F103C8T6")
	}
	if n := countOfferBadges(resp.Offers); n != 0 {
		t.Fatalf("expected no badges for a Keyword request, got %d", n)
	}
}

// TestIntegrationResolveDatasheetURLLegacySzlcsc checks that the
// /szlcsc/ legacy datasheet URL of C327414 maps to a URL that sends a PDF
// file. JLCPCB sends this form for some parts.
func TestIntegrationResolveDatasheetURLLegacySzlcsc(t *testing.T) {
	client := newPoliteIntegrationClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const legacyURL = "https://datasheet.lcsc.com/szlcsc/1811141225_YAGEO-CC0402ZRY5V7BB104P_C327414.pdf"
	pdfURL, err := client.Product.ResolveDatasheetURL(ctx, legacyURL)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	t.Logf("datasheet URL %s", pdfURL)
	if !strings.HasPrefix(pdfURL, legacyDatasheetBaseURL) {
		t.Fatalf("unexpected datasheet URL %q", pdfURL)
	}
	checkPDFRange(ctx, t, pdfURL)
}

// checkPDFRange reads the first bytes of the file at fileURL. It checks
// that the server sends a PDF file.
func checkPDFRange(ctx context.Context, t *testing.T, fileURL string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", "bytes=0-1023")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	head, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	t.Logf("datasheet range request: %v, status %d, content type %q, error %v", time.Since(start), resp.StatusCode, resp.Header.Get("Content-Type"), err)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mediaType != "application/pdf" {
		t.Fatalf("expected application/pdf, got %q", resp.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(string(head), "%PDF-") {
		t.Fatalf("expected a PDF file, got %q", head[:min(len(head), 16)])
	}
}
