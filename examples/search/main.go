// Example CLI application demonstrating LCSC API search functionality.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PatrickWalther/go-lcsc"
)

func main() {
	keyword := flag.String("keyword", "", "Search keyword")
	partNumber := flag.String("part", "", "LCSC part number to lookup")
	pageSize := flag.Int("limit", 10, "Number of results to return")
	inStock := flag.Bool("instock", false, "Only show in-stock items")
	currency := flag.String("currency", "USD", "Currency code (USD, EUR, CNY, etc.)")

	flag.Parse()

	// Create client - no authentication required for unofficial API
	client := lcsc.NewClient(
		lcsc.WithCurrency(*currency),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if *partNumber != "" {
		lookupPart(ctx, client, *partNumber)
		return
	}

	if *keyword != "" {
		searchProducts(ctx, client, *keyword, *pageSize, *inStock)
		return
	}

	flag.Usage()
}

func searchProducts(ctx context.Context, client *lcsc.Client, keyword string, pageSize int, inStock bool) {
	fmt.Printf("Searching for: %s\n\n", keyword)

	results, err := client.KeywordSearch(ctx, lcsc.SearchRequest{
		Keyword:     keyword,
		PageSize:    pageSize,
		IsAvailable: inStock,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d results (showing %d)\n\n", results.TotalCount, len(results.Products))

	for _, p := range results.Products {
		fmt.Printf("%-12s | %-25s | %-15s | Stock: %d\n",
			p.ProductCode,
			truncate(p.ProductModel, 25),
			truncate(p.BrandNameEn, 15),
			p.StockNumber)
		fmt.Printf("             | %s\n", truncate(p.ProductIntroEn, 70))
		if len(p.ProductPriceList) > 0 {
			fmt.Printf("             | Price: %s%.4f (%d+)\n", p.ProductPriceList[0].CurrencySymbol, p.ProductPriceList[0].ProductPrice, p.ProductPriceList[0].Ladder)
		}
		fmt.Println()
	}
}

func lookupPart(ctx context.Context, client *lcsc.Client, partNumber string) {
	fmt.Printf("Looking up: %s\n\n", partNumber)

	product, err := client.GetProductDetails(ctx, partNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("LCSC Part:    %s\n", product.ProductCode)
	fmt.Printf("MFR Part:     %s\n", product.ProductModel)
	fmt.Printf("Manufacturer: %s\n", product.BrandNameEn)
	fmt.Printf("Description:  %s\n", product.ProductIntroEn)
	fmt.Printf("Category:     %s > %s\n", product.ParentCatalogName, product.CatalogName)
	fmt.Printf("Stock:        %d\n", product.StockNumber)
	fmt.Printf("Min Order:    %d\n", product.MinPacketNumber)
	fmt.Printf("Package:      %s\n", product.EncapStandard)
	fmt.Printf("URL:          %s\n", product.GetProductURL())

	if product.PdfUrl != "" {
		fmt.Printf("Datasheet:    %s\n", product.PdfUrl)
	}

	if len(product.ParamVOList) > 0 {
		fmt.Println("\nParameters:")
		for _, param := range product.ParamVOList {
			fmt.Printf("  %s: %s\n", param.ParamNameEn, param.ParamValueEn)
		}
	}

	if len(product.ProductPriceList) > 0 {
		fmt.Println("\nPricing:")
		for _, pb := range product.ProductPriceList {
			fmt.Printf("  %d+: %s%.4f\n", pb.Ladder, pb.CurrencySymbol, pb.ProductPrice)
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
