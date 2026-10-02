package lcsc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
)

const testDetailPath = "/ftps/wm/product/detail"

func TestSearchRouteFullMatch(t *testing.T) {
	// Trimmed live v3 response for "10k 0603".
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")

	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		if body := mustReadBody(t, req); body != `{"keyword":"10k 0603"}` {
			t.Errorf("unexpected body %s", body)
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	route, err := client.Search.Route(context.Background(), " 10k 0603 ")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}

	if route.Keyword != "10k 0603" || route.Scene != SceneFullMatch || route.TotalCount != 979 {
		t.Fatalf("unexpected route: keyword %q, scene %q, total %d", route.Keyword, route.Scene, route.TotalCount)
	}
	if !route.IsParametric() || route.ProductModel {
		t.Fatalf("expected a parametric route, got query types %v", route.QueryTypes)
	}
	if len(route.TopResults) != 3 {
		t.Fatalf("expected 3 top results, got %d", len(route.TopResults))
	}
	top := route.TopResults[0]
	if top.ID != 1199 || top.Name != "Chip Resistor - Surface Mount" || top.ProductCount != 697 || top.ParentID != 501 || top.RootID != 30 || top.Level != 3 {
		t.Fatalf("unexpected top result: %+v", top)
	}
	if got := route.LeafCatalogIDs(0); !reflect.DeepEqual(got, []int{1199, 1272, 1200}) {
		t.Fatalf("expected all leaf ids, got %v", got)
	}
	if got := route.LeafCatalogIDs(2); !reflect.DeepEqual(got, []int{1199, 1272}) {
		t.Fatalf("expected 2 leaf ids, got %v", got)
	}
	if route.RedirectCode != "" || len(route.ExactMatches) != 0 || len(route.Products) != 0 {
		t.Fatalf("unexpected route content: %+v", route)
	}
}

func TestSearchRouteModel(t *testing.T) {
	// Trimmed live v3 response for "AO3400A".
	v3 := mustReadFixture(t, "search_v3_model_AO3400A.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	route, err := client.Search.Route(context.Background(), "AO3400A")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if !route.ProductModel || route.IsParametric() || route.Scene != SceneFullMatch {
		t.Fatalf("expected a product model route, got %+v", route)
	}
	if got, want := productCodes(route.ExactMatches), []string{"C20917", "C347475", "C49195711"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected exact matches %v, got %v", want, got)
	}
	if route.ExactMatches[0].ProductID != 21629 {
		t.Fatalf("expected product id 21629, got %d", route.ExactMatches[0].ProductID)
	}
	if got := route.LeafCatalogIDs(3); !reflect.DeepEqual(got, []int{1436}) {
		t.Fatalf("expected leaf 1436, got %v", got)
	}
}

func TestSearchRouteRedirect(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_redirect_C25744.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	route, err := client.Search.Route(context.Background(), "C25744")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if route.Scene != SceneRedirectProductDetail || route.RedirectCode != "C25744" {
		t.Fatalf("expected a redirect to C25744, got scene %q, code %q", route.Scene, route.RedirectCode)
	}
	if len(route.QueryTypes) != 0 || route.TotalCount != 0 {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestSearchRoutePartialMatch(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_partial_USB-C_16P.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	route, err := client.Search.Route(context.Background(), "USB Type-C 16P")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if route.Scene != ScenePartialMatch || route.TotalCount != 57986 {
		t.Fatalf("unexpected scene %q or total %d", route.Scene, route.TotalCount)
	}
	if len(route.Products) != 2 || route.Page != 1 || route.PageSize != 25 {
		t.Fatalf("unexpected product page: %d products, page %d, size %d", len(route.Products), route.Page, route.PageSize)
	}
	if route.Products[0].ProductCode != "C2765186" || route.Products[0].ProductID == 0 {
		t.Fatalf("unexpected first product: %s (%d)", route.Products[0].ProductCode, route.Products[0].ProductID)
	}
}

func TestSearchRouteValidationAndCache(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")

	var calls int32
	var bodies []string
	client := newCachedTestClient(newRecordingCache(), func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		bodies = append(bodies, mustReadBody(t, req))
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if _, err := client.Search.Route(ctx, "  "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an empty keyword, got %v", err)
	}
	first, err := client.Search.Route(ctx, "1m 0603")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	second, err := client.Search.Route(ctx, " 1m 0603 ")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
	if !reflect.DeepEqual(first.TopResults, second.TopResults) || second.Scene != SceneFullMatch || second.Keyword != "1m 0603" {
		t.Fatalf("expected the cached route, got %+v", second)
	}

	// The case of an SI prefix changes the query: "1M" is mega and "1m" is
	// milli. The cache must not give the route of the other keyword.
	third, err := client.Search.Route(ctx, "1M 0603")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected a second request for a keyword with a different case, got %d requests", got)
	}
	if third.Keyword != "1M 0603" {
		t.Fatalf("expected the keyword of the request, got %q", third.Keyword)
	}
	want := []string{`{"keyword":"1m 0603"}`, `{"keyword":"1M 0603"}`}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("expected bodies %v, got %v", want, bodies)
	}
}

func TestRouteLeafCatalogIDsWithChildren(t *testing.T) {
	route := &Route{TopResults: []RouteCategory{
		{ID: 495, Children: []RouteCategory{{ID: 1142}, {ID: 1141}}},
		{ID: 1142},
		{ID: 0},
		{ID: 1199},
	}}
	if got := route.LeafCatalogIDs(0); !reflect.DeepEqual(got, []int{1142, 1141, 1199}) {
		t.Fatalf("expected child leaves without repeats, got %v", got)
	}
	if got := route.LeafCatalogIDs(1); !reflect.DeepEqual(got, []int{1142}) {
		t.Fatalf("expected one leaf, got %v", got)
	}
	var nilRoute *Route
	if nilRoute.LeafCatalogIDs(3) != nil || nilRoute.IsParametric() {
		t.Fatal("expected no ids for a nil route")
	}
}

func TestSearchParametricFullMatchUsesList(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")

	tests := []struct {
		name        string
		opts        *ParametricOptions
		wantIDs     []interface{}
		wantStock   bool
		wantSort    interface{}
		wantPage    float64
		wantPageLen float64
	}{
		{"default options", nil, []interface{}{float64(1199), float64(1272), float64(1200)}, false, nil, 1, 25},
		{
			"best category only",
			&ParametricOptions{MaxCatalogs: 1, InStock: true, Sort: SortStock, Desc: true, Page: 2, PageSize: 50},
			[]interface{}{float64(1199)}, true, "stock", 2, 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				if req.URL.Path == testSearchV3Path {
					return jsonResponse(http.StatusOK, v3), nil
				}
				body := decodeJSONBody(t, req)
				if body["globalKeyword"] != "10k 0603" || body["scene"] != "FULL_MATCH" || body["keyword"] != "" {
					t.Errorf("unexpected keyword fields in %v", body)
				}
				if !reflect.DeepEqual(body["catalogIdList"], tt.wantIDs) {
					t.Errorf("expected catalog ids %v, got %v", tt.wantIDs, body["catalogIdList"])
				}
				if body["isStock"] != tt.wantStock || body["sortField"] != tt.wantSort {
					t.Errorf("unexpected stock or sort fields in %v", body)
				}
				if body["currentPage"] != tt.wantPage || body["pageSize"] != tt.wantPageLen {
					t.Errorf("unexpected page fields in %v", body)
				}
				return jsonResponse(http.StatusOK, list), nil
			})
			defer func() { _ = client.Close() }()

			resp, err := client.Search.Parametric(context.Background(), "10k 0603", tt.opts)
			if err != nil {
				t.Fatalf("parametric failed: %v", err)
			}
			if want := []string{testSearchV3Path, testQueryListPath}; !reflect.DeepEqual(paths, want) {
				t.Fatalf("expected requests %v, got %v", want, paths)
			}
			if len(resp.Products) != 3 || resp.TotalCount != 181 || resp.ActualTotalCount != 181 {
				t.Fatalf("unexpected response: %d products, counts %d and %d", len(resp.Products), resp.TotalCount, resp.ActualTotalCount)
			}
			if resp.Route == nil || resp.Route.Scene != SceneFullMatch || len(resp.CatalogIDs) != len(tt.wantIDs) {
				t.Fatalf("expected the route and the catalog ids in the response, got route %v and ids %v", resp.Route, resp.CatalogIDs)
			}
		})
	}
}

func TestSearchParametricRedirectUsesDetails(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_redirect_C25744.json")
	detail := mustReadFixture(t, "product_detail_C25744.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case testSearchV3Path:
			return jsonResponse(http.StatusOK, v3), nil
		case testDetailPath:
			if got := req.URL.Query().Get("productCode"); got != "C25744" {
				t.Errorf("unexpected product code %q", got)
			}
			return jsonResponse(http.StatusOK, detail), nil
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusNotFound, ""), nil
		}
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	resp, err := client.Search.Parametric(ctx, "C25744", nil)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if want := []string{testSearchV3Path, testDetailPath}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected requests %v, got %v", want, paths)
	}
	if got := productCodes(resp.Products); !reflect.DeepEqual(got, []string{"C25744"}) {
		t.Fatalf("expected [C25744], got %v", got)
	}
	if resp.TotalCount != 1 || resp.ActualTotalCount != 1 || resp.Route.RedirectCode != "C25744" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	resp, err = client.Search.Parametric(ctx, "C25744", &ParametricOptions{Page: 2})
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(resp.Products) != 0 || resp.TotalCount != 1 {
		t.Fatalf("expected an empty second page, got %v", productCodes(resp.Products))
	}
}

func TestSearchParametricRedirectToUnknownProduct(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_redirect_C25744.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == testDetailPath {
			return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{},"ok":true}`), nil
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Parametric(context.Background(), "C25744", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Products) != 0 || resp.TotalCount != 0 {
		t.Fatalf("expected an empty response, got %+v", resp)
	}
}

func TestSearchParametricModelUsesExactMatches(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_model_AO3400A.json")

	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	resp, err := client.Search.Parametric(ctx, "AO3400A", nil)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(resp.Products) != 3 || resp.TotalCount != 3 || resp.ActualTotalCount != 3 {
		t.Fatalf("expected the 3 exact matches, got %v (total %d)", productCodes(resp.Products), resp.TotalCount)
	}

	resp, err = client.Search.Parametric(ctx, "AO3400A", &ParametricOptions{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if got := productCodes(resp.Products); !reflect.DeepEqual(got, []string{"C49195711"}) {
		t.Fatalf("expected the third exact match on page 2, got %v", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected only the v3 requests, got %d", got)
	}
}

func TestSearchParametricModelWithoutExactMatchUsesList(t *testing.T) {
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")

	var paths []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == testSearchV3Path {
			return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{
				"scene":"FULL_MATCH","totalCount":181,"exactMatchResult":null,
				"searchEngineProcess":{"judgeSuccessType":["PRODUCT_MODEL"]},
				"topResults":[{"catalogId":1199,"productNum":181,"childCatalogs":[]}]
			},"ok":true}`), nil
		}
		body := decodeJSONBody(t, req)
		if body["globalKeyword"] != "RC0603FR-0710KL" || !reflect.DeepEqual(body["catalogIdList"], []interface{}{float64(1199)}) {
			t.Errorf("unexpected list body %v", body)
		}
		return jsonResponse(http.StatusOK, list), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Parametric(context.Background(), "RC0603FR-0710KL", nil)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(paths) != 2 || len(resp.Products) != 3 {
		t.Fatalf("expected the list request, got requests %v and %d products", paths, len(resp.Products))
	}
}

func TestSearchParametricPartialMatch(t *testing.T) {
	page1 := mustReadFixture(t, "search_v3_partial_USB-C_16P.json")
	page2 := mustReadFixture(t, "search_v3_partial_USB-C_16P_page2.json")

	var bodies []string
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != testSearchV3Path {
			t.Errorf("unexpected request to %s", req.URL.Path)
		}
		body := mustReadBody(t, req)
		bodies = append(bodies, body)
		if body == `{"keyword":"USB Type-C 16P"}` {
			return jsonResponse(http.StatusOK, page1), nil
		}
		return jsonResponse(http.StatusOK, page2), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	resp, err := client.Search.Parametric(ctx, "USB Type-C 16P", nil)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected only the route request for page 1, got %v", bodies)
	}
	if got, want := productCodes(resp.Products), []string{"C2765186", "C3151749"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if resp.TotalCount != 57986 || resp.ActualTotalCount != 57986 || resp.Page != 1 || resp.PageSize != 25 {
		t.Fatalf("unexpected counts or page: %+v", resp)
	}

	bodies = nil
	resp, err = client.Search.Parametric(ctx, "USB Type-C 16P", &ParametricOptions{Page: 2})
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	want := []string{`{"keyword":"USB Type-C 16P"}`, `{"keyword":"USB Type-C 16P","currentPage":2,"pageSize":25}`}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("expected requests %v, got %v", want, bodies)
	}
	if got, want := productCodes(resp.Products), []string{"C709358", "C41417453"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected page 2 products %v, got %v", want, got)
	}
	if resp.TotalCount != 57986 || resp.Page != 2 {
		t.Fatalf("unexpected counts or page: %+v", resp)
	}
}

func TestSearchParametricNoResult(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_no_result.json")

	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, v3), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Parametric(context.Background(), "4.7uF 0805 25V", nil)
	if err != nil {
		t.Fatalf("parametric failed: %v", err)
	}
	if len(resp.Products) != 0 || resp.TotalCount != 0 || resp.Route.Scene != SceneNoResult {
		t.Fatalf("expected an empty response with scene NO_RESULT, got %+v", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
}

func TestSearchParametricValidation(t *testing.T) {
	tests := []struct {
		name  string
		query string
		opts  *ParametricOptions
	}{
		{"empty query", " ", nil},
		{"page size above 100", "100nF 0402", &ParametricOptions{PageSize: 101}},
		{"negative page", "100nF 0402", &ParametricOptions{Page: -1}},
		{"page after row 5000", "100nF 0402", &ParametricOptions{Page: 201}},
		{"negative category count", "100nF 0402", &ParametricOptions{MaxCatalogs: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{},"ok":true}`), nil
			})
			defer func() { _ = client.Close() }()

			if _, err := client.Search.Parametric(context.Background(), tt.query, tt.opts); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
			if got := atomic.LoadInt32(&calls); got != 0 {
				t.Fatalf("expected no request, got %d", got)
			}
		})
	}
}

func TestSearchKeywordParametricQueryUsesTopCategories(t *testing.T) {
	// "10k 0603" has three top categories. Keyword uses all three, as
	// Parametric does with the default options.
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")
	list := mustReadFixture(t, "query_list_1199_10k_0603.json")

	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == testSearchV3Path {
			return jsonResponse(http.StatusOK, v3), nil
		}
		body := decodeJSONBody(t, req)
		want := []interface{}{float64(1199), float64(1272), float64(1200)}
		if !reflect.DeepEqual(body["catalogIdList"], want) {
			t.Errorf("expected catalog ids %v, got %v", want, body["catalogIdList"])
		}
		return jsonResponse(http.StatusOK, list), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "10k 0603"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !resp.ParametricQuery || len(resp.Products) != 3 || resp.ActualTotalCount != 181 {
		t.Fatalf("unexpected response: parametric %v, %d products, actual total %d", resp.ParametricQuery, len(resp.Products), resp.ActualTotalCount)
	}
}

func TestSearchKeywordParametricListError(t *testing.T) {
	v3 := mustReadFixture(t, "search_v3_parametric_10k_0603.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == testSearchV3Path {
			return jsonResponse(http.StatusOK, v3), nil
		}
		return jsonResponse(http.StatusOK, `{"code":405,"msg":"Invalid field. Please check again.","result":null,"ok":false}`), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Search.Keyword(context.Background(), &SearchRequest{Keyword: "10k 0603"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected the list error, got %v", err)
	}
}

func TestProductSimilarFilter(t *testing.T) {
	product := &Product{
		WmCatalogID:   1199,
		EncapStandard: "0402",
		ParamVOList: []Parameter{
			{ParamNameEn: "Resistance", ParamValueEn: "10kΩ", IsMain: true},
			{ParamNameEn: "Power(Watts)", ParamValueEn: "62.5mW", IsMain: true},
			{ParamNameEn: "Operating Temperature", ParamValueEn: "-55℃~+155℃"},
			{ParamNameEn: "Tolerance", ParamValueEn: "±1%", IsMain: true},
			{ParamNameEn: "Type", ParamValueEn: "-", IsMain: true},
			{ParamNameEn: " ", ParamValueEn: "x", IsMain: true},
		},
	}

	got := product.SimilarFilter()
	want := Filter{
		CatalogIDs: []int{1199},
		Packages:   []string{"0402"},
		Params: map[string][]string{
			"Resistance":   {"10kΩ"},
			"Power(Watts)": {"62.5mW"},
			"Tolerance":    {"±1%"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SimilarFilter() = %+v, want %+v", got, want)
	}

	got = product.SimilarFilter("tolerance", "power (watts)")
	if !reflect.DeepEqual(got.Params, map[string][]string{"Resistance": {"10kΩ"}}) {
		t.Fatalf("expected relaxed parameters to be removed, got %v", got.Params)
	}

	noPackage := &Product{WmCatalogID: 1142, EncapStandard: "-"}
	if got := noPackage.SimilarFilter(); got.Packages != nil || got.Params != nil {
		t.Fatalf("expected no package and no parameters, got %+v", got)
	}
	var nilProduct *Product
	if got := nilProduct.SimilarFilter(); !reflect.DeepEqual(got, Filter{}) {
		t.Fatalf("expected an empty filter for a nil product, got %+v", got)
	}
}

func TestSearchSimilar(t *testing.T) {
	// Live detail of C25744 and the live list for its key parameters.
	detail := mustReadFixture(t, "product_detail_C25744.json")
	list := mustReadFixture(t, "query_list_similar_C25744.json")

	var bodies []map[string]interface{}
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case testDetailPath:
			// LCSC finds no product for a lower-case code.
			if got := req.URL.Query().Get("productCode"); got != "C25744" {
				t.Errorf("expected productCode C25744, got %q", got)
			}
			return jsonResponse(http.StatusOK, detail), nil
		case testQueryListPath:
			bodies = append(bodies, decodeJSONBody(t, req))
			return jsonResponse(http.StatusOK, list), nil
		default:
			t.Errorf("unexpected request to %s", req.URL.Path)
			return jsonResponse(http.StatusNotFound, ""), nil
		}
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	resp, err := client.Search.Similar(ctx, " c25744 ", &SimilarOptions{Sort: SortStock, Desc: true, PageSize: 50})
	if err != nil {
		t.Fatalf("similar failed: %v", err)
	}
	if got, want := productCodes(resp.Products), []string{"C2906861", "C25744"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if resp.ActualTotalCount != 32 {
		t.Fatalf("expected actual total 32, got %d", resp.ActualTotalCount)
	}

	// The body that gave the live response (report L2, similar search).
	want := map[string]interface{}{
		"keyword":          "",
		"catalogIdList":    []interface{}{float64(1199)},
		"brandIdList":      []interface{}{},
		"encapValueList":   []interface{}{"0402"},
		"isStock":          false,
		"isOtherSuppliers": false,
		"isAsianBrand":     false,
		"isDeals":          false,
		"isRohsCert":       false,
		"paramNameValueMap": map[string]interface{}{
			"Resistance":              []interface{}{"10kΩ"},
			"Power(Watts)":            []interface{}{"62.5mW"},
			"Voltage Rating":          []interface{}{"50V"},
			"Type":                    []interface{}{"Thick Film Resistor"},
			"Temperature Coefficient": []interface{}{"±100ppm/℃"},
			"Tolerance":               []interface{}{"±1%"},
		},
		"sortField":   "stock",
		"sortType":    "desc",
		"currentPage": float64(1),
		"pageSize":    float64(50),
	}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Fatalf("unexpected list body:\n got %v\nwant %v", bodies[0], want)
	}

	if _, err := client.Search.Similar(ctx, "C25744", &SimilarOptions{Relax: []string{"Tolerance"}, AnyPackage: true, InStock: true}); err != nil {
		t.Fatalf("similar failed: %v", err)
	}
	relaxed := bodies[1]
	params := relaxed["paramNameValueMap"].(map[string]interface{})
	if _, ok := params["Tolerance"]; ok || len(params) != 5 {
		t.Fatalf("expected 5 parameters without Tolerance, got %v", params)
	}
	if pkgs := relaxed["encapValueList"].([]interface{}); len(pkgs) != 0 || relaxed["isStock"] != true {
		t.Fatalf("expected no package and stock only, got %v", relaxed)
	}
}

func TestSearchSimilarErrors(t *testing.T) {
	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"productCode":"C1","wmCatalogId":0},"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if _, err := client.Search.Similar(ctx, "", nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an empty code, got %v", err)
	}
	if _, err := client.Search.Similar(ctx, "C1", &SimilarOptions{PageSize: 101}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for page size 101, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no request for invalid input, got %d", got)
	}
	if _, err := client.Search.Similar(ctx, "C1", nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a product without category, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected only the detail request, got %d", got)
	}
}
