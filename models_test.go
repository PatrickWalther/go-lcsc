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

func TestFlexStringUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want FlexString
	}{
		{"string", `"5"`, "5"},
		{"integer", `6`, "6"},
		{"float", `1.5`, "1.5"},
		{"empty string", `""`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s FlexString
			if err := json.Unmarshal([]byte(tt.raw), &s); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if s != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, s)
			}
		})
	}
}

func TestFlexStringUnmarshalNullKeepsValue(t *testing.T) {
	s := FlexString("4")
	if err := json.Unmarshal([]byte(`null`), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if s != "4" {
		t.Fatalf("expected null to keep %q, got %q", "4", s)
	}
}

func TestFlexStringUnmarshalInvalid(t *testing.T) {
	var s FlexString
	if err := json.Unmarshal([]byte(`true`), &s); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestProductMatchTypeAcceptsStringNumberAndNull(t *testing.T) {
	raw := []byte(`[
		{"productCode":"C1","matchType":"5"},
		{"productCode":"C2","matchType":6},
		{"productCode":"C3","matchType":null}
	]`)

	var products []Product
	if err := json.Unmarshal(raw, &products); err != nil {
		t.Fatalf("failed to unmarshal products: %v", err)
	}

	want := []FlexString{"5", "6", ""}
	for i, p := range products {
		if p.MatchType != want[i] {
			t.Fatalf("%s: expected match type %q, got %q", p.ProductCode, want[i], p.MatchType)
		}
	}
}

func TestParameterJSONTags(t *testing.T) {
	// Values from live LCSC product detail responses (C1525 and C7593).
	raw := []byte(`[
		{"paramCode":"param_10951_n","paramNameEn":"Capacitance","paramValueEn":"100nF","paramValueEnForSearch":100000.0,"isMain":true},
		{"paramCode":"param_10923_s","paramNameEn":"Tolerance","paramValueEn":"±10%","paramValueEnForSearch":null,"isMain":true},
		{"paramCode":"param_33225","paramNameEn":"Features","paramValueEn":"Reset function","paramValueEnForSearch":null,"isMain":null},
		{"paramCode":"param_32640_n","paramNameEn":"Supply Current","paramValueEn":"2mA","paramValueEnForSearch":0.002,"isMain":false}
	]`)

	var params []Parameter
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("failed to unmarshal parameters: %v", err)
	}
	if len(params) != 4 {
		t.Fatalf("expected 4 parameters, got %d", len(params))
	}

	if params[0].ParamCode != "param_10951_n" || !params[0].IsMain {
		t.Fatalf("unexpected first parameter: %+v", params[0])
	}
	if params[0].ParamValueEnForSearch == nil || *params[0].ParamValueEnForSearch != 100000 {
		t.Fatalf("expected search value 100000, got %v", params[0].ParamValueEnForSearch)
	}
	if params[1].ParamValueEnForSearch != nil {
		t.Fatalf("expected nil search value for null, got %v", *params[1].ParamValueEnForSearch)
	}
	if params[2].IsMain {
		t.Fatal("expected null isMain to decode as false")
	}
	if params[3].IsMain || params[3].ParamValueEnForSearch == nil || *params[3].ParamValueEnForSearch != 0.002 {
		t.Fatalf("unexpected fourth parameter: %+v", params[3])
	}
}
