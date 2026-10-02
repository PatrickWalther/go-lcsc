package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// SearchService handles product search operations.
type SearchService service

// SearchRequest contains parameters for product search.
type SearchRequest struct {
	Keyword string
}

// SearchResponse contains product search results.
type SearchResponse struct {
	// Products holds the matching products. It is empty when LCSC has no
	// product list for the keyword.
	Products []Product

	// TotalCount is the number of matching products that LCSC reports for
	// the product list in Products. For the exact match list, TotalCount is
	// the length of that list. For the fallback list and for a parametric
	// query, TotalCount is the totalRow value of /product/query/list, which
	// LCSC caps at 5000. When the client drops unrelated rows from the
	// fallback list, TotalCount is the number of rows it keeps.
	TotalCount int

	// ActualTotalCount is the real number of matching products. For the
	// fallback list and for a parametric query, it is the actualTotalRow
	// value of /product/query/list, which LCSC does not cap. For all other
	// cases, it is equal to TotalCount: the v3 product list does not cap
	// its count, and when the client drops unrelated rows, it counts the
	// rows it keeps.
	ActualTotalCount int

	// DirectMatchCode is the LCSC product code that LCSC links directly to
	// the keyword. It is empty when LCSC gives no direct match.
	DirectMatchCode string

	// QueryTypes holds the classification that LCSC gives to the keyword,
	// for example "PRODUCT_MODEL", "PRODUCT_PARAM" or "STANDARD". It is
	// empty when the response has no classification.
	QueryTypes []string

	// ParametricQuery is true when LCSC classifies the keyword as a
	// parameter or package query (for example "100nF 0402") and returns no
	// product list. The client then gets Products from the categories of
	// the route, as [SearchService.Parametric] does with the default
	// options: the first page of 25 products in the first 3 leaf
	// categories. Products is empty when LCSC gives no category.
	ParametricQuery bool
}

type productListRequestBody struct {
	Keyword     string `json:"keyword"`
	CurrentPage int    `json:"currentPage"`
	PageSize    int    `json:"pageSize"`
}

type searchResponseWrapper struct {
	Scene                 string          `json:"scene"`
	TotalCount            int             `json:"totalCount"`
	TopResults            []RouteCategory `json:"topResults"`
	ProductSearchResultVO *struct {
		ProductList []Product `json:"productList"`
		TotalCount  int       `json:"totalCount"`
		CurrentPage int       `json:"currentPage"`
		PageSize    int       `json:"pageSize"`
	} `json:"productSearchResultVO"`
	ExactMatchResult    []Product `json:"exactMatchResult"`
	SearchEngineProcess *struct {
		JudgeSuccessType []string `json:"judgeSuccessType"`
	} `json:"searchEngineProcess"`
	TipProductDetailURLVO *struct {
		ProductCode string `json:"productCode"`
	} `json:"tipProductDetailUrlVO"`
}

const (
	queryTypeProductModel = "PRODUCT_MODEL"
	queryTypeStandard     = "STANDARD"
)

// Keyword searches for products by keyword.
//
// The client sends the keyword to /search/v3/global and uses the first
// source that has products:
//
//  1. The product list of the v3 response.
//  2. The exact match list of the v3 response.
//  3. The /product/query/list endpoint. The client uses this fallback only
//     when LCSC classifies the keyword as a product model, when LCSC gives a
//     direct match, or when the v3 response has no classification. The
//     client keeps only the rows that match the keyword. See
//     [SearchResponse.TotalCount].
//
// When LCSC classifies the keyword as a parameter or package query, the
// client sets [SearchResponse.ParametricQuery]. It then sends the keyword
// as a global keyword to /product/query/list, with the leaf categories of
// the v3 response (see [SearchService.Parametric]). When the v3 response
// has no category, the result is empty. For other classifications without
// a product list, for example a brand, the client returns an empty result.
// It does not return an error for these cases.
func (s *SearchService) Keyword(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidRequest)
	}

	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%w: keyword is required", ErrInvalidRequest)
	}

	client := s.client
	cacheKey := cacheKeyForSearch(client.currency, keyword)
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var resp SearchResponse
			if err := json.Unmarshal(cached, &resp); err == nil {
				return &resp, nil
			}
		}
	}

	var wrapper searchResponseWrapper
	if err := client.do(ctx, http.MethodPost, "/search/v3/global", nil, routeRequestBody{Keyword: keyword}, &wrapper); err != nil {
		return nil, err
	}

	route := newRoute(keyword, &wrapper)
	resp := &SearchResponse{
		DirectMatchCode: route.RedirectCode,
		QueryTypes:      route.QueryTypes,
	}

	// search/v3/global mostly routes the query (direct match, categories).
	// For model keywords it can give an exact match list. Other product
	// lists moved to /product/query/list. With only a keyword, that
	// endpoint returns popular unrelated parts for parameter queries. For
	// those queries, the client uses the categories of the route instead.
	switch {
	case len(route.Products) > 0:
		resp.Products = route.Products
		resp.TotalCount = route.TotalCount
		resp.ActualTotalCount = resp.TotalCount
	case len(route.ExactMatches) > 0:
		resp.Products = route.ExactMatches
		resp.TotalCount = len(route.ExactMatches)
		resp.ActualTotalCount = resp.TotalCount
	case route.IsParametric():
		resp.ParametricQuery = true
		opts, err := normalizeParametricOptions(nil)
		if err != nil {
			return nil, err
		}
		list, err := s.listFromRoute(ctx, route, opts)
		if err != nil {
			return nil, err
		}
		resp.Products = list.Products
		resp.TotalCount = list.TotalCount
		resp.ActualTotalCount = list.ActualTotalCount
	case allowsFallback(resp.QueryTypes, resp.DirectMatchCode):
		products, total, actualTotal, err := s.fallbackProducts(ctx, keyword, resp.DirectMatchCode)
		if err != nil {
			return nil, err
		}
		resp.Products = products
		resp.TotalCount = total
		resp.ActualTotalCount = actualTotal
	}

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(resp); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return resp, nil
}

// fallbackProducts gets the product list from /product/query/list. It keeps
// only the rows that match the keyword or the direct match code. It returns
// the kept rows, the total count and the actual total count. When it drops
// rows, both counts are the number of kept rows.
func (s *SearchService) fallbackProducts(ctx context.Context, keyword, directMatchCode string) ([]Product, int, int, error) {
	if normalizeSearchText(keyword) == "" && normalizeSearchText(directMatchCode) == "" {
		return nil, 0, 0, nil
	}

	var list productListWrapper
	body := productListRequestBody{Keyword: keyword, CurrentPage: 1, PageSize: 25}
	if err := s.client.do(ctx, http.MethodPost, "/product/query/list", nil, body, &list); err != nil {
		return nil, 0, 0, err
	}

	kept := filterRelatedProducts(list.DataList, keyword, directMatchCode)
	if len(kept) != len(list.DataList) {
		return kept, len(kept), len(kept), nil
	}
	return kept, list.TotalRow, list.actualTotal(), nil
}

// hasQueryType reports whether types contains want. The comparison ignores
// case.
func hasQueryType(types []string, want string) bool {
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// isParametricQuery reports whether LCSC classifies the keyword as a
// parameter or package query and not as a product model.
func isParametricQuery(types []string) bool {
	if hasQueryType(types, queryTypeProductModel) {
		return false
	}
	for _, t := range types {
		t = strings.ToUpper(strings.TrimSpace(t))
		if strings.Contains(t, "PARAM") || t == queryTypeStandard {
			return true
		}
	}
	return false
}

// allowsFallback reports whether the client can send the fallback request.
// Older v3 responses have no classification. The client keeps the fallback
// for them.
func allowsFallback(types []string, directMatchCode string) bool {
	return len(types) == 0 || hasQueryType(types, queryTypeProductModel) || directMatchCode != ""
}

// filterRelatedProducts keeps a product when one of these conditions is
// true:
//   - The normalized product model contains the normalized keyword.
//   - The normalized product code is equal to the normalized keyword.
//   - The product code is equal to the direct match code.
//
// When the keyword is the direct match code (for example "C2040"), it keeps
// only the product with that code. For this keyword, the fallback list also
// has parts whose model contains the code, for example "PI6C20400BLEX".
// These parts are not related to the keyword.
func filterRelatedProducts(products []Product, keyword, directMatchCode string) []Product {
	needle := normalizeSearchText(keyword)
	direct := normalizeSearchText(directMatchCode)
	keywordIsDirectCode := direct != "" && needle == direct

	var kept []Product
	for _, p := range products {
		code := normalizeSearchText(p.ProductCode)
		switch {
		case direct != "" && code == direct:
		case keywordIsDirectCode:
			continue
		case needle != "" && strings.Contains(normalizeSearchText(p.ProductModel), needle):
		case needle != "" && code == needle:
		default:
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// normalizeSearchText changes s to lower case and removes white space,
// dashes and dots.
func normalizeSearchText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || r == '-' || r == '.' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// cacheKeyForSearch makes the cache key of [SearchService.Keyword]. The key
// keeps the case of the keyword, because a parameter query can use the
// case of an SI prefix: "1m 0603" (milli) and "1M 0603" (mega) are
// different queries.
func cacheKeyForSearch(currency, keyword string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(keyword)))
	return fmt.Sprintf("search:%s:%s", strings.ToUpper(currency), hex.EncodeToString(hash[:8]))
}
