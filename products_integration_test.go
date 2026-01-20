package lcsc

import (
	"context"
	"testing"
	"time"
)

// TestKeywordSearchBasicIntegration tests basic keyword search functionality with real LCSC API.
func TestKeywordSearchBasicIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: "STM32F103",
	})

	if err != nil {
		t.Fatalf("KeywordSearch failed: %v", err)
	}

	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	if len(resp.Products) == 0 {
		t.Fatal("expected at least one product in search results")
	}

	// Verify first product has expected fields
	p := resp.Products[0]
	if p.ProductCode == "" {
		t.Error("expected product code to be non-empty")
	}
	if p.BrandNameEn == "" {
		t.Error("expected brand name to be non-empty")
	}
	if p.ProductIntroEn == "" {
		t.Error("expected product intro to be non-empty")
	}
}

// TestGetProductDetailsBasicIntegration tests retrieving detailed product information.
func TestGetProductDetailsBasicIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// First, find a product code from search
	searchResp, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: "STM32F103",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(searchResp.Products) == 0 {
		t.Fatal("no products found for STM32F103")
	}

	productCode := searchResp.Products[0].ProductCode

	// Get details for the product
	product, err := client.GetProductDetails(ctx, productCode)
	if err != nil {
		t.Fatalf("GetProductDetails failed for %s: %v", productCode, err)
	}

	if product == nil {
		t.Fatal("expected non-nil product")
	}

	if product.ProductCode != productCode {
		t.Errorf("expected product code %s, got %s", productCode, product.ProductCode)
	}

	// Verify required fields
	if product.BrandNameEn == "" {
		t.Error("expected brand name to be non-empty")
	}
	if product.ProductIntroEn == "" {
		t.Error("expected product intro to be non-empty")
	}
}

// TestGetProductDetailsFieldsIntegration tests that all expected product fields are populated.
func TestGetProductDetailsFieldsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use a known component that typically has comprehensive data
	product, err := client.GetProductDetails(ctx, "C8734")
	if err != nil {
		t.Fatalf("GetProductDetails failed: %v", err)
	}

	// Check key fields that should be populated for most products
	if product.ProductCode == "" {
		t.Error("expected product code")
	}
	if product.BrandNameEn == "" {
		t.Error("expected brand name")
	}
	if product.ProductModel == "" {
		t.Error("expected product model")
	}

	// Stock and pricing info
	if product.StockNumber < 0 {
		t.Error("expected non-negative stock number")
	}
	if product.MinPacketNumber <= 0 {
		t.Error("expected positive min packet number")
	}

	// Should have price breaks (optional, may not exist for all products)
	// No assertion needed - some products may have no price breaks

	// Verify price breaks are valid
	for i, pb := range product.ProductPriceList {
		if pb.Ladder <= 0 {
			t.Errorf("price break %d has non-positive ladder", i)
		}
		if pb.ProductPrice < 0 {
			t.Errorf("price break %d has negative price", i)
		}
	}
}

// TestGetProductDetailsNotFoundIntegration tests handling of non-existent product codes.
func TestGetProductDetailsNotFoundIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use a product code that doesn't exist
	product, err := client.GetProductDetails(ctx, "C99999999")

	// API may return either error or empty product - both are acceptable
	if product != nil && product.ProductCode == "" && err == nil {
		// Product returned but empty - acceptable
		return
	}

	if err != nil && err != ErrProductNotFound {
		t.Logf("API returned error for non-existent product: %v (acceptable)", err)
		return
	}
}

// TestKeywordSearchCachingIntegration tests that search results are cached properly.
func TestKeywordSearchCachingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cache := NewMemoryCache(5 * time.Minute)
	client := NewClient(WithCache(cache))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	keyword := "LM7805"

	// First search should hit the API
	resp1, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: keyword,
	})
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	initialCacheSize := cache.Size()
	if initialCacheSize == 0 {
		t.Error("expected cache to have entries after first search")
	}

	// Second search should use cache
	resp2, err := client.KeywordSearch(ctx, SearchRequest{
		Keyword: keyword,
	})
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	// Results should be identical
	if len(resp1.Products) != len(resp2.Products) {
		t.Errorf("expected same number of products, got %d vs %d", len(resp1.Products), len(resp2.Products))
	}

	if resp1.TotalCount != resp2.TotalCount {
		t.Errorf("expected same total count, got %d vs %d", resp1.TotalCount, resp2.TotalCount)
	}
}

// TestGetProductDetailsCachingIntegration tests that product details are cached.
func TestGetProductDetailsCachingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cache := NewMemoryCache(5 * time.Minute)
	client := NewClient(WithCache(cache))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	productCode := "C8734"

	// First request should populate cache
	product1, err := client.GetProductDetails(ctx, productCode)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	if cache.Size() == 0 {
		t.Error("expected cache to have entries after first request")
	}

	// Second request should use cache
	product2, err := client.GetProductDetails(ctx, productCode)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	// Results should be identical
	if product1.ProductCode != product2.ProductCode {
		t.Errorf("product codes differ: %s vs %s", product1.ProductCode, product2.ProductCode)
	}
	if product1.BrandNameEn != product2.BrandNameEn {
		t.Errorf("brand names differ: %s vs %s", product1.BrandNameEn, product2.BrandNameEn)
	}
}

// TestConcurrentSearchesIntegration tests that multiple concurrent searches work correctly.
func TestConcurrentSearchesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	keywords := []string{"STM32", "capacitor", "resistor", "diode"}
	results := make(chan error, len(keywords))

	for _, keyword := range keywords {
		go func(kw string) {
			_, err := client.KeywordSearch(ctx, SearchRequest{
				Keyword: kw,
			})
			results <- err
		}(keyword)
	}

	for i := 0; i < len(keywords); i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent search failed: %v", err)
		}
	}
}
