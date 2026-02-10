package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ProductService handles product-detail operations.
type ProductService service

// Details retrieves detailed information for a specific product code.
func (s *ProductService) Details(ctx context.Context, productCode string) (*Product, error) {
	productCode = strings.TrimSpace(productCode)
	if productCode == "" {
		return nil, fmt.Errorf("%w: productCode is required", ErrInvalidRequest)
	}

	client := s.client
	cacheKey := cacheKeyForDetails(client.currency, productCode)
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var product Product
			if err := json.Unmarshal(cached, &product); err == nil {
				return &product, nil
			}
		}
	}

	params := url.Values{}
	params.Set("productCode", productCode)

	var product Product
	if err := client.do(ctx, http.MethodGet, "/product/detail", params, nil, &product); err != nil {
		return nil, err
	}

	if strings.TrimSpace(product.ProductCode) == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, productCode)
	}

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(product); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.DetailsTTL)
		}
	}

	return &product, nil
}

func cacheKeyForDetails(currency, productCode string) string {
	normalized := strings.ToUpper(strings.TrimSpace(productCode))
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("details:%s:%s", strings.ToUpper(currency), hex.EncodeToString(hash[:8]))
}
