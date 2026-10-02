package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	// defaultListPageSize is the page size that the client sends when the
	// request has no page size.
	defaultListPageSize = 25

	// maxListPageSize is the largest page size that LCSC accepts. LCSC
	// answers a larger page size with envelope code 405 "Product search
	// error."
	maxListPageSize = 100

	// maxListRows is the number of rows that LCSC returns at most for one
	// filter. LCSC answers a page that ends after row 5000 with envelope
	// code 405 "Product search error.", also when the page starts before
	// row 5000 and totalPage includes the page. For example, page 167 at
	// page size 30 (rows 4981-5010) gives 405. Thus a page size that does
	// not divide 5000 cannot read the last rows. Use a page size that
	// divides 5000, for example 25, 50 or 100, to read them.
	maxListRows = 5000

	// sceneFullMatch is the scene that the client sends with a global
	// keyword. Without it, LCSC uses a loose match.
	sceneFullMatch = "FULL_MATCH"
)

// SortField is a sort field of [SearchService.List]. The constants hold
// the fields that the LCSC site uses. The type is a string, so other
// values pass to LCSC unchanged.
type SortField string

const (
	// SortStock sorts by stock. The LCSC stock sort is approximate.
	SortStock SortField = "stock"

	// SortPrice sorts by price. LCSC uses the price of the largest
	// quantity break.
	SortPrice SortField = "price"
)

// Filter selects products for [SearchService.List] and
// [SearchService.Facets].
type Filter struct {
	// GlobalKeyword is a parameter or package query, for example
	// "10k 0603". It needs CatalogIDs. The client then sends the scene
	// "FULL_MATCH", which gives the exact match that the LCSC site uses.
	GlobalKeyword string

	// Keyword is a text search. Without CatalogIDs, LCSC matches it as a
	// substring of the product model, so "AO3400A" also finds
	// "AO3400A-MS". With GlobalKeyword, it searches inside the results.
	Keyword string

	// CatalogIDs holds leaf category ids (see [CatalogService.Leaves]).
	// LCSC finds no products for the id of a parent category, and it sends
	// no error for it.
	CatalogIDs []int

	// BrandIDs holds manufacturer ids (see [Product.BrandID] and
	// [Facets.Manufacturers]).
	BrandIDs []int

	// Packages holds package names, for example "0603" (see
	// [Facets.Packages]).
	Packages []string

	// Params maps a parameter name to the accepted values, for example
	// {"Resistance": {"10kΩ"}}. A product must match one value of each
	// name. Use the exact names and values of [Facets]. LCSC finds no
	// products for an unknown name or value, and it sends no error for
	// it. One value can have more than one name, for example "100nF" and
	// "100000pF". Use [Facets.ExpandParams] to add the other names.
	Params map[string][]string

	// InStock limits the result to products with LCSC stock.
	InStock bool

	// RoHS limits the result to products with a RoHS certificate.
	RoHS bool
}

// ListRequest contains the parameters for [SearchService.List].
type ListRequest struct {
	Filter

	// Sort is the sort field. The server order applies when Sort is empty.
	Sort SortField

	// Desc sorts in descending order. It has no effect when Sort is empty.
	Desc bool

	// Page is the page number. The first page is 1. The client sends 1
	// when Page is 0.
	Page int

	// PageSize is the number of products on one page, from 1 to 100. The
	// client sends 25 when PageSize is 0.
	PageSize int
}

// ListResponse contains one page of products.
type ListResponse struct {
	// Products holds the products on the page.
	Products []Product

	// TotalCount is the number of matching products that LCSC can return
	// (totalRow). LCSC caps it at 5000.
	TotalCount int

	// ActualTotalCount is the real number of matching products
	// (actualTotalRow). It is TotalCount when the response does not send
	// actualTotalRow.
	ActualTotalCount int

	// Page is the page number of Products.
	Page int

	// PageSize is the requested page size.
	PageSize int

	// CatalogIDs holds the leaf category ids of the request. For
	// [SearchService.Parametric], they come from the route.
	CatalogIDs []int

	// Route is the route that [SearchService.Parametric] used. It is nil
	// for [SearchService.List].
	Route *Route
}

// queryListBody is the request body of /product/query/list and
// /product/query/param/group. The field set follows the LCSC web client.
type queryListBody struct {
	Keyword           string              `json:"keyword"`
	GlobalKeyword     string              `json:"globalKeyword,omitempty"`
	Scene             string              `json:"scene,omitempty"`
	CatalogIDList     []int               `json:"catalogIdList"`
	BrandIDList       []int               `json:"brandIdList"`
	EncapValueList    []string            `json:"encapValueList"`
	IsStock           bool                `json:"isStock"`
	IsOtherSuppliers  bool                `json:"isOtherSuppliers"`
	IsAsianBrand      bool                `json:"isAsianBrand"`
	IsDeals           bool                `json:"isDeals"`
	IsRohsCert        bool                `json:"isRohsCert"`
	ParamNameValueMap map[string][]string `json:"paramNameValueMap"`
	SortField         string              `json:"sortField,omitempty"`
	SortType          string              `json:"sortType,omitempty"`
	CurrentPage       int                 `json:"currentPage,omitempty"`
	PageSize          int                 `json:"pageSize,omitempty"`
}

type productListWrapper struct {
	CurrPage int `json:"currPage"`

	// TotalRow is the number of matching products. LCSC caps it at 5000.
	TotalRow int `json:"totalRow"`

	// ActualTotalRow is the real number of matching products.
	ActualTotalRow int `json:"actualTotalRow"`

	DataList []Product `json:"dataList"`
}

// actualTotal returns actualTotalRow, or totalRow when the response does
// not send actualTotalRow.
func (w *productListWrapper) actualTotal() int {
	if w.ActualTotalRow < w.TotalRow {
		// Older responses do not send actualTotalRow.
		return w.TotalRow
	}
	return w.ActualTotalRow
}

// List returns one page of products that match the filter. It uses
// /product/query/list, which the LCSC category pages use.
//
// List returns [ErrInvalidRequest] and does not send the request in these
// cases:
//
//   - PageSize is above 100 or negative, or Page is negative.
//   - Page × PageSize is above 5000. LCSC returns at most 5000 rows. It
//     answers a page that ends after row 5000 with code 405, also when
//     the page starts before row 5000. To read the last rows, use a page
//     size that divides 5000.
//   - GlobalKeyword is set and CatalogIDs is empty.
//   - A category id or a brand id is not positive.
//
// LCSC answers an invalid field with envelope code 405, which also gives
// [ErrInvalidRequest]. The client caches the response for
// CacheConfig.SearchTTL.
func (s *SearchService) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidRequest)
	}
	page, pageSize, err := normalizeListPage(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	body, err := newQueryListBody(&req.Filter)
	if err != nil {
		return nil, err
	}
	if req.Sort != "" {
		body.SortField = string(req.Sort)
		body.SortType = "asc"
		if req.Desc {
			body.SortType = "desc"
		}
	}
	body.CurrentPage = page
	body.PageSize = pageSize

	client := s.client
	cacheKey, keyErr := cacheKeyForBody("list", client.currency, body)
	useCache := keyErr == nil && client.cacheConfig.Enabled && client.cache != nil
	if useCache {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var resp ListResponse
			if err := json.Unmarshal(cached, &resp); err == nil {
				return &resp, nil
			}
		}
	}

	var list productListWrapper
	if err := client.do(ctx, http.MethodPost, "/product/query/list", nil, body, &list); err != nil {
		return nil, err
	}

	resp := &ListResponse{
		Products:         list.DataList,
		TotalCount:       list.TotalRow,
		ActualTotalCount: list.actualTotal(),
		Page:             page,
		PageSize:         pageSize,
		CatalogIDs:       body.CatalogIDList,
	}

	if useCache {
		if data, err := json.Marshal(resp); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return resp, nil
}

// normalizeListPage checks the page and the page size and sets the
// defaults.
func normalizeListPage(page, pageSize int) (int, int, error) {
	switch {
	case page < 0:
		return 0, 0, fmt.Errorf("%w: page must not be negative, got %d", ErrInvalidRequest, page)
	case page == 0:
		page = 1
	}
	switch {
	case pageSize < 0 || pageSize > maxListPageSize:
		return 0, 0, fmt.Errorf("%w: pageSize must be from 1 to %d, got %d", ErrInvalidRequest, maxListPageSize, pageSize)
	case pageSize == 0:
		pageSize = defaultListPageSize
	}
	if page > maxListRows/pageSize {
		return 0, 0, fmt.Errorf("%w: page %d with pageSize %d is after row %d", ErrInvalidRequest, page, pageSize, maxListRows)
	}
	return page, pageSize, nil
}

// newQueryListBody checks the filter and makes the request body without
// sort and paging fields.
func newQueryListBody(f *Filter) (*queryListBody, error) {
	if f == nil {
		return nil, fmt.Errorf("%w: filter is nil", ErrInvalidRequest)
	}

	body := &queryListBody{
		Keyword:           strings.TrimSpace(f.Keyword),
		GlobalKeyword:     strings.TrimSpace(f.GlobalKeyword),
		CatalogIDList:     []int{},
		BrandIDList:       []int{},
		EncapValueList:    []string{},
		IsStock:           f.InStock,
		IsRohsCert:        f.RoHS,
		ParamNameValueMap: map[string][]string{},
	}

	for _, id := range f.CatalogIDs {
		if id <= 0 {
			return nil, fmt.Errorf("%w: category id must be positive, got %d", ErrInvalidRequest, id)
		}
		body.CatalogIDList = append(body.CatalogIDList, id)
	}
	for _, id := range f.BrandIDs {
		if id <= 0 {
			return nil, fmt.Errorf("%w: brand id must be positive, got %d", ErrInvalidRequest, id)
		}
		body.BrandIDList = append(body.BrandIDList, id)
	}
	for _, pkg := range f.Packages {
		if pkg = strings.TrimSpace(pkg); pkg != "" {
			body.EncapValueList = append(body.EncapValueList, pkg)
		}
	}
	for name, values := range f.Params {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, v := range values {
			if v = strings.TrimSpace(v); v != "" {
				body.ParamNameValueMap[name] = append(body.ParamNameValueMap[name], v)
			}
		}
	}

	if body.GlobalKeyword != "" {
		// LCSC answers a global keyword without a category with code 405
		// "Invalid field. Please check again."
		if len(body.CatalogIDList) == 0 {
			return nil, fmt.Errorf("%w: globalKeyword needs at least one leaf category id", ErrInvalidRequest)
		}
		body.Scene = sceneFullMatch
	}

	return body, nil
}

// cacheKeyForBody makes a cache key from the JSON form of a request body.
func cacheKeyForBody(prefix, currency string, body interface{}) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%s:%s:%s", prefix, strings.ToUpper(currency), hex.EncodeToString(hash[:8])), nil
}
