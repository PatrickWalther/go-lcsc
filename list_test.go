package lcsc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSearchListDecodesFixture(t *testing.T) {
	// Trimmed live response for "10k 0603" in leaf 1199 with stock only.
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")

	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.Method != http.MethodPost || req.URL.Path != testQueryListPath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type %q", got)
		}
		if got := req.Header.Get("Cookie"); got != "currencyCode=USD" {
			t.Errorf("unexpected cookie %q", got)
		}
		body := decodeJSONBody(t, req)
		// The body that the LCSC site sends for this search (report L2).
		want := map[string]interface{}{
			"keyword":           "",
			"globalKeyword":     "10k 0603",
			"scene":             "FULL_MATCH",
			"catalogIdList":     []interface{}{float64(1199)},
			"brandIdList":       []interface{}{},
			"encapValueList":    []interface{}{},
			"isStock":           true,
			"isOtherSuppliers":  false,
			"isAsianBrand":      false,
			"isDeals":           false,
			"isRohsCert":        false,
			"paramNameValueMap": map[string]interface{}{},
			"currentPage":       float64(1),
			"pageSize":          float64(50),
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("unexpected body:\n got %v\nwant %v", body, want)
		}
		return jsonResponse(http.StatusOK, list), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.List(context.Background(), &ListRequest{
		Filter: Filter{
			GlobalKeyword: " 10k 0603 ",
			CatalogIDs:    []int{1199},
			InStock:       true,
		},
		PageSize: 50,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if got, want := productCodes(resp.Products), []string{"C98220", "C2906982", "C2930027"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected products %v, got %v", want, got)
	}
	if resp.TotalCount != 181 || resp.ActualTotal != 181 {
		t.Fatalf("expected counts 181 and 181, got %d and %d", resp.TotalCount, resp.ActualTotal)
	}
	if resp.Page != 1 || resp.PageSize != 50 {
		t.Fatalf("expected page 1 with size 50, got %d with size %d", resp.Page, resp.PageSize)
	}
	if !reflect.DeepEqual(resp.CatalogIDs, []int{1199}) || resp.Route != nil {
		t.Fatalf("unexpected catalog ids %v or route %v", resp.CatalogIDs, resp.Route)
	}

	p := resp.Products[0]
	if p.ProductID != 99429 || p.ProductModel != "RC0603FR-0710KL" || p.EncapStandard != "0603" {
		t.Fatalf("unexpected product: %d %s %s", p.ProductID, p.ProductModel, p.EncapStandard)
	}
	if p.WmCatalogID != 1199 || p.WmCatalogNameEn != "Chip Resistor - Surface Mount" {
		t.Fatalf("unexpected category: %d %q", p.WmCatalogID, p.WmCatalogNameEn)
	}
	wantPath := []CategoryRef{{30, "Passives"}, {501, "Resistors"}, {1199, "Chip Resistor - Surface Mount"}}
	if got := p.CatalogPath(); !reflect.DeepEqual(got, wantPath) {
		t.Fatalf("CatalogPath() = %v, want %v", got, wantPath)
	}
	if p.MoistureSensitivityLevel != "1级(无限)" {
		t.Fatalf("unexpected moisture sensitivity level %q", p.MoistureSensitivityLevel)
	}
	if p.StockNumber != 8529000 || len(p.ParamVOList) != 3 || len(p.ProductPriceList) != 2 {
		t.Fatalf("unexpected row: stock %d, %d parameters, %d prices", p.StockNumber, len(p.ParamVOList), len(p.ProductPriceList))
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
}

func TestSearchListRequestBodyWithAllFields(t *testing.T) {
	var body map[string]interface{}
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		body = decodeJSONBody(t, req)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalRow":0,"dataList":[]},"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Search.List(context.Background(), &ListRequest{
		Filter: Filter{
			GlobalKeyword: "100nF 0402",
			Keyword:       "X7R",
			CatalogIDs:    []int{1142, 1141},
			BrandIDs:      []int{254},
			Packages:      []string{" 0402 ", ""},
			Params: map[string][]string{
				"Capacitance":    {"100nF", "100000pF"},
				"Voltage Rating": {" ", "16V"},
				" ":              {"ignored"},
				"Tolerance":      nil,
			},
			InStock: true,
			RoHS:    true,
		},
		Sort:     SortPrice,
		Page:     2,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	want := map[string]interface{}{
		"keyword":          "X7R",
		"globalKeyword":    "100nF 0402",
		"scene":            "FULL_MATCH",
		"catalogIdList":    []interface{}{float64(1142), float64(1141)},
		"brandIdList":      []interface{}{float64(254)},
		"encapValueList":   []interface{}{"0402"},
		"isStock":          true,
		"isOtherSuppliers": false,
		"isAsianBrand":     false,
		"isDeals":          false,
		"isRohsCert":       true,
		"paramNameValueMap": map[string]interface{}{
			"Capacitance":    []interface{}{"100nF", "100000pF"},
			"Voltage Rating": []interface{}{"16V"},
		},
		"sortField":   "price",
		"sortType":    "asc",
		"currentPage": float64(2),
		"pageSize":    float64(10),
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("unexpected body:\n got %v\nwant %v", body, want)
	}
}

func TestSearchListRequestBodyWithoutGlobalKeyword(t *testing.T) {
	var body map[string]interface{}
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		body = decodeJSONBody(t, req)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalRow":0,"dataList":[]},"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.Search.List(context.Background(), &ListRequest{
		Filter: Filter{CatalogIDs: []int{1199}, Packages: []string{"0603"}},
		Sort:   SortStock,
		Desc:   true,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	for _, key := range []string{"globalKeyword", "scene"} {
		if _, ok := body[key]; ok {
			t.Fatalf("expected no %s without a global keyword, got %v", key, body[key])
		}
	}
	if body["sortField"] != "stock" || body["sortType"] != "desc" {
		t.Fatalf("expected stock sort in descending order, got %v %v", body["sortField"], body["sortType"])
	}
	if body["currentPage"] != float64(1) || body["pageSize"] != float64(25) {
		t.Fatalf("expected default page 1 with size 25, got %v and %v", body["currentPage"], body["pageSize"])
	}
	if params, ok := body["paramNameValueMap"].(map[string]interface{}); !ok || len(params) != 0 {
		t.Fatalf("expected an empty parameter map, got %v", body["paramNameValueMap"])
	}
	if brands, ok := body["brandIdList"].([]interface{}); !ok || len(brands) != 0 {
		t.Fatalf("expected an empty brand list, got %v", body["brandIdList"])
	}
}

func TestSearchListSortFieldsWithoutSort(t *testing.T) {
	var body map[string]interface{}
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		body = decodeJSONBody(t, req)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalRow":0,"dataList":[]},"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Search.List(context.Background(), &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, Desc: true}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	for _, key := range []string{"sortField", "sortType"} {
		if _, ok := body[key]; ok {
			t.Fatalf("expected no %s without a sort field, got %v", key, body[key])
		}
	}
}

func TestSearchListValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *ListRequest
	}{
		{"nil request", nil},
		{"page size above 100", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, PageSize: 101}},
		{"negative page size", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, PageSize: -1}},
		{"negative page", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, Page: -1}},
		{"page after row 5000", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, Page: 51, PageSize: 100}},
		{"default page size after row 5000", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}, Page: 201}},
		{"global keyword without category", &ListRequest{Filter: Filter{GlobalKeyword: "4.7uF 0805 25V"}}},
		{"category id 0", &ListRequest{Filter: Filter{CatalogIDs: []int{1199, 0}}}},
		{"negative brand id", &ListRequest{Filter: Filter{CatalogIDs: []int{1199}, BrandIDs: []int{-254}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalRow":0,"dataList":[]},"ok":true}`), nil
			})
			defer func() { _ = client.Close() }()

			_, err := client.Search.List(context.Background(), tt.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
			if got := atomic.LoadInt32(&calls); got != 0 {
				t.Fatalf("expected no request, got %d", got)
			}
		})
	}
}

func TestSearchListAcceptsLimits(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
	}{
		{"page size 100", 1, 100},
		{"row 5000 with page size 100", 50, 100},
		{"row 5000 with the default page size", 200, 0},
		{"page size 1", 5000, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalRow":0,"dataList":[]},"ok":true}`), nil
			})
			defer func() { _ = client.Close() }()

			_, err := client.Search.List(context.Background(), &ListRequest{
				Filter:   Filter{CatalogIDs: []int{1199}},
				Page:     tt.page,
				PageSize: tt.pageSize,
			})
			if err != nil {
				t.Fatalf("expected page %d with size %d to pass, got %v", tt.page, tt.pageSize, err)
			}
		})
	}
}

func TestSearchListActualTotal(t *testing.T) {
	tests := []struct {
		name       string
		result     string
		wantTotal  int
		wantActual int
	}{
		{"capped total", `{"totalRow":5000,"actualTotalRow":939792,"dataList":[]}`, 5000, 939792},
		{"equal counts", `{"totalRow":181,"actualTotalRow":181,"dataList":[]}`, 181, 181},
		{"no actualTotalRow", `{"totalRow":42,"dataList":[]}`, 42, 42},
		{"null actualTotalRow", `{"totalRow":42,"actualTotalRow":null,"dataList":[]}`, 42, 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":`+tt.result+`,"ok":true}`), nil
			})
			defer func() { _ = client.Close() }()

			resp, err := client.Search.List(context.Background(), &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}})
			if err != nil {
				t.Fatalf("list failed: %v", err)
			}
			if resp.TotalCount != tt.wantTotal || resp.ActualTotal != tt.wantActual {
				t.Fatalf("expected counts %d and %d, got %d and %d", tt.wantTotal, tt.wantActual, resp.TotalCount, resp.ActualTotal)
			}
		})
	}
}

func TestSearchListEnvelope405IsNotRetried(t *testing.T) {
	// Live envelope for a page size above 100 and for a page after row
	// 5000.
	var calls int32
	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return jsonResponse(http.StatusOK, `{"code":405,"msg":"Product search error.","result":null,"ok":false}`), nil
		})),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	_, err := client.Search.List(context.Background(), &ListRequest{Filter: Filter{CatalogIDs: []int{1199}}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
	if !strings.Contains(err.Error(), "Product search error.") {
		t.Fatalf("expected the LCSC message in the error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
}

func TestSearchListIsCached(t *testing.T) {
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")
	cache := newRecordingCache()

	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, list), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	req := &ListRequest{Filter: Filter{GlobalKeyword: "10k 0603", CatalogIDs: []int{1199}}}
	for i := 0; i < 2; i++ {
		resp, err := client.Search.List(ctx, req)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(resp.Products) != 3 || resp.ActualTotal != 181 {
			t.Fatalf("unexpected response: %d products, actual total %d", len(resp.Products), resp.ActualTotal)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request for the same filter, got %d", got)
	}

	if _, err := client.Search.List(ctx, &ListRequest{Filter: req.Filter, Page: 2}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if _, err := client.Search.List(ctx, &ListRequest{Filter: Filter{GlobalKeyword: "10k 0603", CatalogIDs: []int{1199}, InStock: true}}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected a new request for another page and another filter, got %d requests", got)
	}
}

func TestProductCatalogPath(t *testing.T) {
	tests := []struct {
		name    string
		product *Product
		want    []CategoryRef
	}{
		{"nil product", nil, nil},
		{"no category", &Product{}, nil},
		{
			"detail response",
			&Product{
				WmCatalogID:       1199,
				WmCatalogNameEn:   "Chip Resistor - Surface Mount",
				ParentCatalogList: []CategoryRef{{30, "Passives"}, {501, "Resistors"}},
			},
			[]CategoryRef{{30, "Passives"}, {501, "Resistors"}, {1199, "Chip Resistor - Surface Mount"}},
		},
		{
			"parent list ends with the leaf",
			&Product{WmCatalogID: 501, ParentCatalogList: []CategoryRef{{30, "Passives"}, {501, "Resistors"}}},
			[]CategoryRef{{30, "Passives"}, {501, "Resistors"}},
		},
		{
			"list row",
			&Product{
				WmCatalogID:           1436,
				FirstWmCatalogID:      12,
				FirstWmCatalogNameEn:  "Discrete Semiconductors",
				SecondWmCatalogID:     1420,
				SecondWmCatalogNameEn: "Transistors",
				ThirdWmCatalogID:      1433,
				ThirdWmCatalogNameEn:  "FETs, MOSFETs",
				FourthWmCatalogID:     1436,
				FourthWmCatalogNameEn: "Single FETs, MOSFETs",
				SixthWmCatalogID:      99,
			},
			[]CategoryRef{{12, "Discrete Semiconductors"}, {1420, "Transistors"}, {1433, "FETs, MOSFETs"}, {1436, "Single FETs, MOSFETs"}},
		},
		{
			"leaf only",
			&Product{WmCatalogID: 1142, WmCatalogNameEn: "Ceramic Capacitors"},
			[]CategoryRef{{1142, "Ceramic Capacitors"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.product.CatalogPath(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("CatalogPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
