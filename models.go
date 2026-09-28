package lcsc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Parameter represents a product specification.
type Parameter struct {
	ParamNameEn  string `json:"paramNameEn"`
	ParamValueEn string `json:"paramValueEn"`

	// ParamCode is the LCSC identifier of the parameter, for example
	// "param_10951_n".
	ParamCode string `json:"paramCode"`

	// ParamValueEnForSearch is the numeric value that LCSC uses for
	// parametric search. LCSC uses base units (Ω, V, W, A, Hz), but
	// capacitance is in pF: 100nF gives 100000. It is nil or -1 when the
	// value is not a single number, for example "X7R" or "±10%".
	ParamValueEnForSearch *float64 `json:"paramValueEnForSearch"`

	// IsMain is true for the key parameters of the product. LCSC sends
	// null for some parameters. The client decodes null as false.
	IsMain bool `json:"isMain"`
}

// FlexFloat64 handles JSON values that may be either a number or a string.
type FlexFloat64 float64

// UnmarshalJSON implements json.Unmarshaler for FlexFloat64.
func (f *FlexFloat64) UnmarshalJSON(data []byte) error {
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexFloat64(num)
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		num, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as float64: %w", str, err)
		}
		*f = FlexFloat64(num)
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into FlexFloat64", string(data))
}

// FlexString handles JSON values that may be a string, a number, or null.
// A number keeps its JSON text, so 5 gives "5". Null does not change the
// value.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler for FlexString.
func (s *FlexString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	var str string
	if err := json.Unmarshal(trimmed, &str); err == nil {
		*s = FlexString(str)
		return nil
	}

	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err == nil {
		*s = FlexString(num.String())
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into FlexString", string(data))
}

// PriceBreak represents a quantity-based price tier.
type PriceBreak struct {
	Ladder         int         `json:"ladder"`
	ProductPrice   FlexFloat64 `json:"productPrice"`
	CurrencySymbol string      `json:"currencySymbol"`
}

// Product represents an LCSC component.
type Product struct {
	ProductCode       string       `json:"productCode"`
	ProductModel      string       `json:"productModel"`
	BrandNameEn       string       `json:"brandNameEn"`
	ProductIntroEn    string       `json:"productIntroEn"`
	PdfURL            string       `json:"pdfUrl"`
	ProductImages     []string     `json:"productImages"`
	ProductImageURL   string       `json:"productImageUrl"`
	StockNumber       int          `json:"stockNumber"`
	MinPacketNumber   int          `json:"minPacketNumber"`
	ProductPriceList  []PriceBreak `json:"productPriceList"`
	ParamVOList       []Parameter  `json:"paramVOList"`
	EncapStandard     string       `json:"encapStandard"`
	ParentCatalogName string       `json:"parentCatalogName"`
	CatalogName       string       `json:"catalogName"`
	Weight            float64      `json:"weight"`

	// MinBuyNumber is the minimum order quantity.
	MinBuyNumber int `json:"minBuyNumber"`

	// Split is the order multiple. An order quantity must be a multiple of
	// Split.
	Split int `json:"split"`

	// ProductCycle is the lifecycle status that LCSC gives to the product,
	// for example "normal".
	ProductCycle string `json:"productCycle"`

	// IsPreSale is true when LCSC sells the product as a pre-sale item.
	IsPreSale bool `json:"isPreSale"`

	// MatchType is the code that LCSC gives to an alternate part, for
	// example "1", "4", "5" or "6". LCSC does not document the codes. Only
	// the entries of AlternatePartList have a value.
	MatchType FlexString `json:"matchType"`

	// AlternatePartList holds the alternate parts that LCSC selects for
	// the product (up to five). Only [ProductService.Details] fills it.
	// LCSC sends no alternates for the alternates.
	AlternatePartList []Product `json:"alternatePartList"`
}

// GetProductURL returns the LCSC product page URL.
func (p *Product) GetProductURL() string {
	return fmt.Sprintf("https://www.lcsc.com/product-detail/%s.html", p.ProductCode)
}
