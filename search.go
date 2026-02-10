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

// SearchService handles product search operations.
type SearchService service

// SearchRequest contains parameters for product search.
type SearchRequest struct {
	Keyword string
}

// SearchResponse contains product search results.
type SearchResponse struct {
	Products        []Product
	TotalCount      int
	DirectMatchCode string
}

type searchRequestBody struct {
	Keyword string `json:"keyword"`
}

type searchResponseWrapper struct {
	ProductSearchResultVO struct {
		ProductList []Product `json:"productList"`
		TotalCount  int       `json:"totalCount"`
	} `json:"productSearchResultVO"`
	TipProductDetailURLVO *struct {
		ProductCode string `json:"productCode"`
	} `json:"tipProductDetailUrlVO"`
}

// Keyword searches for products by keyword.
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
	if err := client.do(ctx, http.MethodPost, "/search/v2/global", nil, searchRequestBody{Keyword: keyword}, &wrapper); err != nil {
		return nil, err
	}

	resp := &SearchResponse{
		Products:   wrapper.ProductSearchResultVO.ProductList,
		TotalCount: wrapper.ProductSearchResultVO.TotalCount,
	}
	if wrapper.TipProductDetailURLVO != nil && wrapper.TipProductDetailURLVO.ProductCode != "" {
		resp.DirectMatchCode = wrapper.TipProductDetailURLVO.ProductCode
	}

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(resp); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return resp, nil
}

func cacheKeyForSearch(currency, keyword string) string {
	hash := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(keyword))))
	return fmt.Sprintf("search:%s:%s", strings.ToUpper(currency), hex.EncodeToString(hash[:8]))
}
