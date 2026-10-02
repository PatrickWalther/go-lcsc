//go:build integration
// +build integration

package lcsc

import (
	"context"
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
	client := NewClient()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Search.Keyword(ctx, &SearchRequest{Keyword: "100nF 0402"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	t.Logf("query types %v, parametric %v, %d products", resp.QueryTypes, resp.ParametricQuery, len(resp.Products))
	if resp.ParametricQuery && len(resp.Products) != 0 {
		t.Fatalf("expected no products for a parametric query, got %d", len(resp.Products))
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
		if pb.USDPrice != pb.ProductPrice {
			t.Errorf("ladder %d: expected usdPrice %v to equal productPrice %v", pb.Ladder, pb.USDPrice, pb.ProductPrice)
		}
	}
}
