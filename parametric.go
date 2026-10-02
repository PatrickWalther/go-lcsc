package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// RouteScene is the scene that /search/v3/global gives to a keyword. It
// tells which result type the LCSC site shows.
type RouteScene string

const (
	// SceneFullMatch means that LCSC found the keyword in one or more
	// categories ([Route.TopResults]). For a product model, LCSC also
	// sends the exact matches ([Route.ExactMatches]).
	SceneFullMatch RouteScene = "FULL_MATCH"

	// ScenePartialMatch means that LCSC sends a page of loosely matching
	// products ([Route.Products]).
	ScenePartialMatch RouteScene = "PARTIAL_MATCH"

	// SceneRedirectProductDetail means that the keyword names one product
	// ([Route.RedirectCode]), for example an LCSC product code.
	SceneRedirectProductDetail RouteScene = "REDIRECT_PRODUCT_DETAIL"

	// SceneNoResult means that LCSC found no product for the keyword.
	SceneNoResult RouteScene = "NO_RESULT"
)

const (
	// defaultParametricCatalogs is the number of leaf categories of the
	// route that Parametric uses when the options do not set a number.
	defaultParametricCatalogs = 3

	// defaultRoutePageSize is the page size of the product page in a v3
	// response when the request has no page size.
	defaultRoutePageSize = 25
)

// RouteCategory is a category in [Route.TopResults].
type RouteCategory struct {
	// ID is the category id. It is the same id as [Category.ID].
	ID int `json:"catalogId"`

	// Name is the English category name.
	Name string `json:"catalogNameEn"`

	// ParentID and ParentName identify the parent category.
	ParentID   int    `json:"parentId"`
	ParentName string `json:"parentName"`

	// RootID and RootName identify the root category.
	RootID   int    `json:"rootCatalogId"`
	RootName string `json:"rootCatalogName"`

	// Level is the depth of the category in the tree. A root category has
	// level 1.
	Level int `json:"level"`

	// ProductCount is the number of products in the category that match
	// the keyword.
	ProductCount int `json:"productNum"`

	// Children holds child categories. LCSC sent no child categories in
	// the observed responses, and each top result was a leaf category
	// (level 3 or 4). The rule that a top result without children is a
	// leaf is inferred.
	Children []RouteCategory `json:"childCatalogs"`
}

// Route is the result of [SearchService.Route]. It tells how LCSC answers
// a keyword.
type Route struct {
	// Keyword is the keyword that the client sent.
	Keyword string

	// Scene is the result type, for example [SceneFullMatch]. LCSC can
	// send a value that this package does not define.
	Scene RouteScene

	// TotalCount is the number of matching products that LCSC reports for
	// the scene. It is 0 when LCSC sends no count, for example for a
	// redirect.
	TotalCount int

	// TopResults holds the categories with matching products, with the
	// best category first. Only [SceneFullMatch] sends them.
	TopResults []RouteCategory

	// ExactMatches holds the products whose model is equal to the keyword.
	// LCSC fills it for a product model keyword. Each product has its
	// ProductID.
	ExactMatches []Product

	// RedirectCode is the LCSC product code that the keyword names. It is
	// set for [SceneRedirectProductDetail].
	RedirectCode string

	// QueryTypes holds the classification that LCSC gives to the keyword,
	// for example "PRODUCT_MODEL", "PRODUCT_PARAM" or "STANDARD".
	QueryTypes []string

	// ProductModel is true when LCSC classifies the keyword as a product
	// model (PRODUCT_MODEL).
	ProductModel bool

	// Products holds the page of loosely matching products that LCSC sends
	// for [ScenePartialMatch].
	Products []Product

	// Page and PageSize describe the page in Products.
	Page     int
	PageSize int
}

// IsParametric reports whether LCSC classifies the keyword as a parameter
// or package query and not as a product model.
func (r *Route) IsParametric() bool {
	return r != nil && isParametricQuery(r.QueryTypes)
}

// LeafCatalogIDs returns up to limit leaf category ids from TopResults, in
// the route order. A top result without child categories counts as a leaf
// (inferred, see [RouteCategory.Children]). For a top result with child
// categories, it uses the leaves of the child categories. A limit of 0 or
// less gives all ids.
func (r *Route) LeafCatalogIDs(limit int) []int {
	if r == nil {
		return nil
	}
	var ids []int
	seen := map[int]bool{}
	var walk func(nodes []RouteCategory) bool
	walk = func(nodes []RouteCategory) bool {
		for i := range nodes {
			n := &nodes[i]
			if len(n.Children) > 0 {
				if walk(n.Children) {
					return true
				}
				continue
			}
			if n.ID <= 0 || seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			ids = append(ids, n.ID)
			if limit > 0 && len(ids) >= limit {
				return true
			}
		}
		return false
	}
	walk(r.TopResults)
	return ids
}

type routeRequestBody struct {
	Keyword     string `json:"keyword"`
	CurrentPage int    `json:"currentPage,omitempty"`
	PageSize    int    `json:"pageSize,omitempty"`
}

// newRoute makes a Route from a decoded v3 response.
func newRoute(keyword string, w *searchResponseWrapper) *Route {
	r := &Route{
		Keyword:      keyword,
		Scene:        RouteScene(strings.TrimSpace(w.Scene)),
		TotalCount:   w.TotalCount,
		TopResults:   w.TopResults,
		ExactMatches: w.ExactMatchResult,
	}
	if w.TipProductDetailURLVO != nil {
		r.RedirectCode = strings.TrimSpace(w.TipProductDetailURLVO.ProductCode)
	}
	if w.SearchEngineProcess != nil && len(w.SearchEngineProcess.JudgeSuccessType) > 0 {
		r.QueryTypes = w.SearchEngineProcess.JudgeSuccessType
	}
	r.ProductModel = hasQueryType(r.QueryTypes, queryTypeProductModel)
	if p := w.ProductSearchResultVO; p != nil {
		r.Products = p.ProductList
		r.Page = p.CurrentPage
		r.PageSize = p.PageSize
		if p.TotalCount > 0 {
			r.TotalCount = p.TotalCount
		}
	}
	return r
}

// Route sends the keyword to /search/v3/global and returns how LCSC
// answers it: the scene, the best categories, the exact matches, a
// redirect to one product, or a page of products. [SearchService.Parametric]
// uses the route to select the request for the products.
//
// The client caches the route for CacheConfig.SearchTTL.
func (s *SearchService) Route(ctx context.Context, keyword string) (*Route, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%w: keyword is required", ErrInvalidRequest)
	}

	client := s.client
	cacheKey := cacheKeyForRoute(client.currency, keyword)
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var route Route
			if err := json.Unmarshal(cached, &route); err == nil {
				return &route, nil
			}
		}
	}

	var wrapper searchResponseWrapper
	if err := client.do(ctx, http.MethodPost, "/search/v3/global", nil, routeRequestBody{Keyword: keyword}, &wrapper); err != nil {
		return nil, err
	}
	route := newRoute(keyword, &wrapper)

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(route); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return route, nil
}

// ParametricOptions contains the options for [SearchService.Parametric].
type ParametricOptions struct {
	// MaxCatalogs is the largest number of leaf categories from the route
	// that the list request uses. The client uses 3 when MaxCatalogs is
	// 0. Set 1 to use only the best category.
	MaxCatalogs int

	// InStock limits the list request to products with LCSC stock.
	InStock bool

	// Sort and Desc set the order of the list request.
	Sort SortField
	Desc bool

	// Page is the page number. The first page is 1. The client uses 1 when
	// Page is 0.
	Page int

	// PageSize is the number of products on one page, from 1 to 100. The
	// client uses 25 when PageSize is 0.
	PageSize int
}

type parametricOptions struct {
	maxCatalogs int
	inStock     bool
	sort        SortField
	desc        bool
	page        int
	pageSize    int
}

func normalizeParametricOptions(opts *ParametricOptions) (parametricOptions, error) {
	var o ParametricOptions
	if opts != nil {
		o = *opts
	}
	page, pageSize, err := normalizeListPage(o.Page, o.PageSize)
	if err != nil {
		return parametricOptions{}, err
	}
	maxCatalogs := o.MaxCatalogs
	switch {
	case maxCatalogs < 0:
		return parametricOptions{}, fmt.Errorf("%w: maxCatalogs must not be negative, got %d", ErrInvalidRequest, maxCatalogs)
	case maxCatalogs == 0:
		maxCatalogs = defaultParametricCatalogs
	}
	return parametricOptions{
		maxCatalogs: maxCatalogs,
		inStock:     o.InStock,
		sort:        o.Sort,
		desc:        o.Desc,
		page:        page,
		pageSize:    pageSize,
	}, nil
}

// Parametric returns products for a query that the keyword search cannot
// answer with a product list, for example "100nF 0402" or "10k 0603". It
// gets the route of the query ([SearchService.Route]) and then uses the
// first rule that applies:
//
//  1. The route names one product (RedirectCode): Parametric returns the
//     details of that product.
//  2. LCSC classifies the query as a product model and sends exact
//     matches: Parametric returns the exact matches.
//  3. The route has top categories: Parametric calls [SearchService.List]
//     with the query as GlobalKeyword and the first leaf categories of the
//     route (see [ParametricOptions.MaxCatalogs]). This is the exact
//     match that the LCSC site uses. This rule also applies to a product
//     model without exact matches.
//  4. The route has a product page ([ScenePartialMatch]): Parametric
//     returns that page. For a page other than the first page, or for a
//     page size other than 25, it sends the v3 request again with the page
//     fields.
//  5. Otherwise, for example for [SceneNoResult], Parametric returns an
//     empty response and no error.
//
// InStock, Sort and Desc apply only to rule 3. The response holds the
// route in [ListResponse.Route].
func (s *SearchService) Parametric(ctx context.Context, query string, opts *ParametricOptions) (*ListResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	o, err := normalizeParametricOptions(opts)
	if err != nil {
		return nil, err
	}
	route, err := s.Route(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.listFromRoute(ctx, route, o)
}

// listFromRoute gets the products for a route. See
// [SearchService.Parametric] for the rules.
func (s *SearchService) listFromRoute(ctx context.Context, route *Route, o parametricOptions) (*ListResponse, error) {
	resp := &ListResponse{Page: o.page, PageSize: o.pageSize, Route: route}

	if route.RedirectCode != "" {
		product, err := s.client.Product.Details(ctx, route.RedirectCode)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return resp, nil
			}
			return nil, err
		}
		resp.Products = pageOfProducts([]Product{*product}, o.page, o.pageSize)
		resp.TotalCount, resp.ActualTotalCount = 1, 1
		return resp, nil
	}

	if route.ProductModel && len(route.ExactMatches) > 0 {
		resp.Products = pageOfProducts(route.ExactMatches, o.page, o.pageSize)
		resp.TotalCount = len(route.ExactMatches)
		resp.ActualTotalCount = resp.TotalCount
		return resp, nil
	}

	// A product model without exact matches also gets here. The rule that
	// LCSC accepts a model as a global keyword is inferred. It was not
	// checked live.
	if ids := route.LeafCatalogIDs(o.maxCatalogs); len(ids) > 0 {
		list, err := s.List(ctx, &ListRequest{
			Filter: Filter{
				GlobalKeyword: route.Keyword,
				CatalogIDs:    ids,
				InStock:       o.inStock,
			},
			Sort:     o.sort,
			Desc:     o.desc,
			Page:     o.page,
			PageSize: o.pageSize,
		})
		if err != nil {
			return nil, err
		}
		list.Route = route
		return list, nil
	}

	if route.Scene == ScenePartialMatch || len(route.Products) > 0 {
		products, total := route.Products, route.TotalCount
		routePageSize := route.PageSize
		if routePageSize == 0 {
			routePageSize = defaultRoutePageSize
		}
		if o.page != 1 || o.pageSize != routePageSize {
			var err error
			products, total, err = s.routePage(ctx, route.Keyword, o.page, o.pageSize)
			if err != nil {
				return nil, err
			}
		}
		resp.Products = products
		resp.TotalCount, resp.ActualTotalCount = total, total
		return resp, nil
	}

	return resp, nil
}

// routePage gets one page of the v3 product list. A live check showed that
// LCSC accepts page size 100 for this list, the largest page size of
// [ParametricOptions].
func (s *SearchService) routePage(ctx context.Context, keyword string, page, pageSize int) ([]Product, int, error) {
	body := routeRequestBody{Keyword: keyword, CurrentPage: page, PageSize: pageSize}
	var wrapper searchResponseWrapper
	if err := s.client.do(ctx, http.MethodPost, "/search/v3/global", nil, body, &wrapper); err != nil {
		return nil, 0, err
	}
	p := wrapper.ProductSearchResultVO
	if p == nil {
		return nil, wrapper.TotalCount, nil
	}
	total := p.TotalCount
	if total == 0 {
		total = wrapper.TotalCount
	}
	return p.ProductList, total, nil
}

// pageOfProducts returns the products on one page of a local list.
func pageOfProducts(products []Product, page, pageSize int) []Product {
	start := (page - 1) * pageSize
	if start >= len(products) {
		return nil
	}
	end := start + pageSize
	if end > len(products) {
		end = len(products)
	}
	return products[start:end]
}

// cacheKeyForRoute makes the cache key of [SearchService.Route]. The key
// keeps the case of the keyword (see [cacheKeyForSearch]). Route.Keyword
// is then always the keyword of the request.
func cacheKeyForRoute(currency, keyword string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(keyword)))
	return fmt.Sprintf("route:%s:%s", strings.ToUpper(currency), hex.EncodeToString(hash[:8]))
}

// SimilarOptions contains the options for [SearchService.Similar].
type SimilarOptions struct {
	// Relax holds the names of key parameters that the filter does not
	// use, for example "Tolerance". The comparison ignores case, spaces
	// and punctuation.
	Relax []string

	// AnyPackage removes the package from the filter.
	AnyPackage bool

	// InStock limits the result to products with LCSC stock.
	InStock bool

	// Sort and Desc set the order of the result.
	Sort SortField
	Desc bool

	// Page and PageSize select the page. See [ListRequest].
	Page     int
	PageSize int
}

// SimilarFilter returns a filter for products like p. The filter uses the
// category of p (WmCatalogID), the package (EncapStandard) and the value of
// each key parameter (IsMain). It does not use the parameters in relax.
// The comparison of the names in relax ignores case, spaces and
// punctuation.
//
// A filter with all key parameters can be too narrow. Relax one parameter
// at a time to find more products.
func (p *Product) SimilarFilter(relax ...string) Filter {
	var f Filter
	if p == nil {
		return f
	}
	if p.WmCatalogID > 0 {
		f.CatalogIDs = []int{p.WmCatalogID}
	}
	if pkg := strings.TrimSpace(p.EncapStandard); pkg != "" && pkg != "-" {
		f.Packages = []string{pkg}
	}

	skip := map[string]bool{}
	for _, name := range relax {
		skip[normalizeParameterName(name)] = true
	}
	for i := range p.ParamVOList {
		param := &p.ParamVOList[i]
		name := strings.TrimSpace(param.ParamNameEn)
		if !param.IsMain || name == "" || !hasParameterValue(param) || skip[normalizeParameterName(name)] {
			continue
		}
		if f.Params == nil {
			f.Params = map[string][]string{}
		}
		f.Params[name] = append(f.Params[name], strings.TrimSpace(param.ParamValueEn))
	}
	return f
}

// Similar returns products like the product with the code. It gets the
// product details, makes a filter with [Product.SimilarFilter] and calls
// [SearchService.List]. The result can include the product itself.
//
// Similar returns [ErrInvalidRequest] when the product has no category
// id. LCSC sends no error for a parameter name or value that it does not
// know. It returns no products then. Similar changes the code to upper
// case, as [ProductService.Details] does.
func (s *SearchService) Similar(ctx context.Context, code string, opts *SimilarOptions) (*ListResponse, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, fmt.Errorf("%w: productCode is required", ErrInvalidRequest)
	}
	var o SimilarOptions
	if opts != nil {
		o = *opts
	}
	if _, _, err := normalizeListPage(o.Page, o.PageSize); err != nil {
		return nil, err
	}

	product, err := s.client.Product.Details(ctx, code)
	if err != nil {
		return nil, err
	}

	filter := product.SimilarFilter(o.Relax...)
	if len(filter.CatalogIDs) == 0 {
		return nil, fmt.Errorf("%w: product %s has no category id", ErrInvalidRequest, product.ProductCode)
	}
	if o.AnyPackage {
		filter.Packages = nil
	}
	filter.InStock = o.InStock

	return s.List(ctx, &ListRequest{
		Filter:   filter,
		Sort:     o.Sort,
		Desc:     o.Desc,
		Page:     o.Page,
		PageSize: o.PageSize,
	})
}
