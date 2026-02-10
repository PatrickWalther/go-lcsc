package lcsc

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Parameter represents a product specification.
type Parameter struct {
	ParamNameEn  string `json:"paramNameEn"`
	ParamValueEn string `json:"paramValueEn"`
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
}

// GetProductURL returns the LCSC product page URL.
func (p *Product) GetProductURL() string {
	return fmt.Sprintf("https://www.lcsc.com/product-detail/%s.html", p.ProductCode)
}
