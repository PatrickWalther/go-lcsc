package lcsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// searchRequestBody is the JSON body for the search endpoint.
type searchRequestBody struct {
	Keyword string `json:"keyword"`
}

// KeywordSearch searches for products by keyword.
// Uses POST /search/v2/global with JSON body.
func (c *Client) KeywordSearch(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}

	cacheKey := c.getCacheKeySearch(keyword)
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			var resp SearchResponse
			if err := json.Unmarshal(cached, &resp); err == nil {
				return &resp, nil
			}
		}
	}

	body, err := c.doRequest(ctx, "POST", "/search/v2/global", nil, searchRequestBody{
		Keyword: keyword,
	})
	if err != nil {
		return nil, err
	}

	var wrapper searchResponseWrapper
	if err := c.parseResponse(body, &wrapper); err != nil {
		return nil, err
	}

	resp := &SearchResponse{
		Products:   wrapper.ProductSearchResultVO.ProductList,
		TotalCount: wrapper.ProductSearchResultVO.TotalCount,
	}

	if wrapper.TipProductDetailUrlVO != nil && wrapper.TipProductDetailUrlVO.ProductCode != "" {
		resp.DirectMatchCode = wrapper.TipProductDetailUrlVO.ProductCode
	}

	if c.cache != nil {
		if cacheData, err := json.Marshal(resp); err == nil {
			c.cache.Set(cacheKey, cacheData, 5*time.Minute)
		}
	}

	return resp, nil
}

// GetProductDetails retrieves detailed information for a specific product.
// Uses GET /product/detail?productCode=...
func (c *Client) GetProductDetails(ctx context.Context, productCode string) (*Product, error) {
	productCode = strings.TrimSpace(productCode)
	if productCode == "" {
		return nil, fmt.Errorf("productCode is required")
	}

	cacheKey := c.getCacheKeyProduct(productCode)
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			var product Product
			if err := json.Unmarshal(cached, &product); err == nil {
				return &product, nil
			}
		}
	}

	params := url.Values{}
	params.Set("productCode", productCode)

	body, err := c.doRequest(ctx, "GET", "/product/detail", params, nil)
	if err != nil {
		return nil, err
	}

	var product Product
	if err := c.parseResponse(body, &product); err != nil {
		return nil, err
	}

	if c.cache != nil {
		if cacheData, err := json.Marshal(product); err == nil {
			c.cache.Set(cacheKey, cacheData, 5*time.Minute)
		}
	}

	return &product, nil
}

// getCacheKeySearch generates a cache key for search requests.
func (c *Client) getCacheKeySearch(keyword string) string {
	return fmt.Sprintf("search:%s:%s", c.currency, keyword)
}

// getCacheKeyProduct generates a cache key for product detail requests.
func (c *Client) getCacheKeyProduct(productCode string) string {
	return fmt.Sprintf("product:%s:%s", c.currency, productCode)
}
