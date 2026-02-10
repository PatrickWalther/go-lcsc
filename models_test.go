package lcsc

import (
	"encoding/json"
	"testing"
)

func TestFlexFloat64UnmarshalNumber(t *testing.T) {
	var f FlexFloat64
	if err := json.Unmarshal([]byte(`123.45`), &f); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if float64(f) != 123.45 {
		t.Fatalf("expected 123.45, got %f", f)
	}
}

func TestFlexFloat64UnmarshalString(t *testing.T) {
	var f FlexFloat64
	if err := json.Unmarshal([]byte(`"456.78"`), &f); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if float64(f) != 456.78 {
		t.Fatalf("expected 456.78, got %f", f)
	}
}

func TestFlexFloat64UnmarshalInvalid(t *testing.T) {
	var f FlexFloat64
	if err := json.Unmarshal([]byte(`"abc"`), &f); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestProductGetProductURL(t *testing.T) {
	p := &Product{ProductCode: "C12345"}
	if got := p.GetProductURL(); got != "https://www.lcsc.com/product-detail/C12345.html" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestProductJSONTags(t *testing.T) {
	raw := []byte(`{
		"productCode":"C8734",
		"productModel":"LM7805",
		"brandNameEn":"ST",
		"productIntroEn":"Regulator",
		"pdfUrl":"https://example.com/d.pdf",
		"productImageUrl":"https://example.com/i.png"
	}`)

	var p Product
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("failed to unmarshal product: %v", err)
	}

	if p.PdfURL == "" {
		t.Fatal("expected PdfURL to be populated from pdfUrl")
	}
	if p.ProductImageURL == "" {
		t.Fatal("expected ProductImageURL to be populated from productImageUrl")
	}
}
