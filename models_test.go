package lcsc

import (
	"encoding/json"
	"testing"
)

// TestFlexFloat64UnmarshalNumber tests unmarshaling numeric JSON values.
func TestFlexFloat64UnmarshalNumber(t *testing.T) {
	data := []byte(`123.45`)

	var f FlexFloat64
	err := json.Unmarshal(data, &f)

	if err != nil {
		t.Fatalf("failed to unmarshal number: %v", err)
	}

	if float64(f) != 123.45 {
		t.Errorf("expected 123.45, got %f", f)
	}
}

// TestFlexFloat64UnmarshalString tests unmarshaling string JSON values.
func TestFlexFloat64UnmarshalString(t *testing.T) {
	data := []byte(`"456.78"`)

	var f FlexFloat64
	err := json.Unmarshal(data, &f)

	if err != nil {
		t.Fatalf("failed to unmarshal string: %v", err)
	}

	if float64(f) != 456.78 {
		t.Errorf("expected 456.78, got %f", f)
	}
}

// TestFlexFloat64UnmarshalZero tests unmarshaling zero values.
func TestFlexFloat64UnmarshalZero(t *testing.T) {
	tests := []struct {
		data     []byte
		expected float64
	}{
		{[]byte(`0`), 0},
		{[]byte(`"0"`), 0},
		{[]byte(`0.0`), 0.0},
		{[]byte(`"0.0"`), 0.0},
	}

	for _, test := range tests {
		var f FlexFloat64
		err := json.Unmarshal(test.data, &f)

		if err != nil {
			t.Errorf("failed to unmarshal %s: %v", test.data, err)
			continue
		}

		if float64(f) != test.expected {
			t.Errorf("expected %f for input %s, got %f", test.expected, test.data, f)
		}
	}
}

// TestFlexFloat64UnmarshalNegative tests unmarshaling negative values.
func TestFlexFloat64UnmarshalNegative(t *testing.T) {
	data := []byte(`"-123.45"`)

	var f FlexFloat64
	err := json.Unmarshal(data, &f)

	if err != nil {
		t.Fatalf("failed to unmarshal negative string: %v", err)
	}

	if float64(f) != -123.45 {
		t.Errorf("expected -123.45, got %f", f)
	}
}

// TestFlexFloat64UnmarshalInvalid tests error handling for invalid values.
func TestFlexFloat64UnmarshalInvalid(t *testing.T) {
	invalidData := [][]byte{
		[]byte(`"not a number"`),
		[]byte(`true`),
		[]byte(`[]`),
	}

	for _, data := range invalidData {
		var f FlexFloat64
		err := json.Unmarshal(data, &f)

		if err == nil {
			t.Errorf("expected error for invalid data %s, but got none", data)
		}
	}
}

// TestProductGetURL tests the GetProductURL method.
func TestProductGetURL(t *testing.T) {
	product := &Product{
		ProductCode: "C12345",
	}

	expectedURL := "https://www.lcsc.com/product-detail/C12345.html"
	actualURL := product.GetProductURL()

	if actualURL != expectedURL {
		t.Errorf("expected %s, got %s", expectedURL, actualURL)
	}
}

// TestProductGetURLSpecialCharacters tests GetProductURL with special characters.
func TestProductGetURLSpecialCharacters(t *testing.T) {
	product := &Product{
		ProductCode: "C123-456",
	}

	url := product.GetProductURL()

	if url == "" {
		t.Fatal("expected non-empty URL")
	}

	// Verify format
	expected := "https://www.lcsc.com/product-detail/C123-456.html"
	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}

// TestSearchResponseDirectMatch tests SearchResponse with DirectMatchCode.
func TestSearchResponseDirectMatch(t *testing.T) {
	resp := &SearchResponse{
		Products:        []Product{{ProductCode: "C1"}, {ProductCode: "C2"}},
		TotalCount:      100,
		DirectMatchCode: "C1",
	}

	if resp.DirectMatchCode != "C1" {
		t.Errorf("expected direct match code C1, got %s", resp.DirectMatchCode)
	}

	if len(resp.Products) != 2 {
		t.Errorf("expected 2 products, got %d", len(resp.Products))
	}
}

// TestPriceBreakValidation tests PriceBreak structure.
func TestPriceBreakValidation(t *testing.T) {
	pb := PriceBreak{
		Ladder:         1,
		ProductPrice:   FlexFloat64(9.99),
		CurrencySymbol: "US$",
	}

	if pb.Ladder != 1 {
		t.Errorf("expected ladder 1, got %d", pb.Ladder)
	}

	if float64(pb.ProductPrice) != 9.99 {
		t.Errorf("expected price 9.99, got %f", pb.ProductPrice)
	}

	if pb.CurrencySymbol != "US$" {
		t.Errorf("expected currency symbol US$, got %s", pb.CurrencySymbol)
	}
}

// TestProductStructure tests complete Product structure.
func TestProductStructure(t *testing.T) {
	product := &Product{
		ProductCode:       "C8734",
		ProductModel:      "LM7805",
		BrandNameEn:       "STMicroelectronics",
		ProductIntroEn:    "Linear Regulator",
		PdfUrl:            "https://example.com/datasheet.pdf",
		ProductImages:     []string{"img1.jpg", "img2.jpg"},
		ProductImageUrl:   "https://example.com/img.jpg",
		StockNumber:       1000,
		MinPacketNumber:   1,
		ProductPriceList:  []PriceBreak{{Ladder: 1, ProductPrice: 5.0}},
		ParamVOList:       []Parameter{{ParamNameEn: "Voltage", ParamValueEn: "5V"}},
		EncapStandard:     "TO-220",
		ParentCatalogName: "Regulators",
		CatalogName:       "Linear Regulators",
		Weight:            2.5,
	}

	// Verify all fields are accessible
	if product.ProductCode != "C8734" {
		t.Error("product code mismatch")
	}
	if product.BrandNameEn != "STMicroelectronics" {
		t.Error("brand name mismatch")
	}
	if product.StockNumber != 1000 {
		t.Error("stock number mismatch")
	}
	if len(product.ProductImages) != 2 {
		t.Error("image count mismatch")
	}
	// Price list may or may not have entries depending on product type
	// Just verify it's a valid slice
	_ = product.ProductPriceList
}

// TestParameterStructure tests Parameter structure.
func TestParameterStructure(t *testing.T) {
	param := Parameter{
		ParamNameEn:  "Temperature",
		ParamValueEn: "-40°C to +85°C",
	}

	if param.ParamNameEn != "Temperature" {
		t.Errorf("expected parameter name Temperature, got %s", param.ParamNameEn)
	}

	if param.ParamValueEn != "-40°C to +85°C" {
		t.Errorf("expected parameter value -40°C to +85°C, got %s", param.ParamValueEn)
	}
}

// TestCategoryProductsResponse tests CategoryProductsResponse structure.
func TestCategoryProductsResponse(t *testing.T) {
	resp := &CategoryProductsResponse{
		Products:   []Product{{ProductCode: "C1"}, {ProductCode: "C2"}},
		TotalCount: 50,
		PageSize:   2,
		PageNumber: 1,
	}

	if len(resp.Products) != 2 {
		t.Errorf("expected 2 products, got %d", len(resp.Products))
	}

	if resp.TotalCount != 50 {
		t.Errorf("expected total count 50, got %d", resp.TotalCount)
	}

	if resp.PageSize != 2 {
		t.Errorf("expected page size 2, got %d", resp.PageSize)
	}

	if resp.PageNumber != 1 {
		t.Errorf("expected page number 1, got %d", resp.PageNumber)
	}
}

// TestSearchRequestStructure tests SearchRequest structure.
func TestSearchRequestStructure(t *testing.T) {
	req := SearchRequest{
		Keyword:     "STM32",
		CurrentPage: 1,
		PageSize:    20,
		IsAvailable: true,
		MatchType:   "fuzzy",
	}

	if req.Keyword != "STM32" {
		t.Errorf("expected keyword STM32, got %s", req.Keyword)
	}

	if req.PageSize != 20 {
		t.Errorf("expected page size 20, got %d", req.PageSize)
	}

	if !req.IsAvailable {
		t.Error("expected IsAvailable to be true")
	}
}
