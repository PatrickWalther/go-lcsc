package lcsc

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Parameter represents a product specification/parameter.
type Parameter struct {
	ParamNameEn  string `json:"paramNameEn"`
	ParamValueEn string `json:"paramValueEn"`
}

// FlexFloat64 handles JSON values that may be either a number or a string.
type FlexFloat64 float64

// UnmarshalJSON implements json.Unmarshaler for FlexFloat64.
func (f *FlexFloat64) UnmarshalJSON(data []byte) error {
	// Try as number first
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexFloat64(num)
		return nil
	}
	// Try as string
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
	Ladder         int         `json:"ladder"`         // Quantity threshold
	ProductPrice   FlexFloat64 `json:"productPrice"`   // Price in selected currency
	CurrencySymbol string      `json:"currencySymbol"` // Currency symbol like "US$"
}

// Product represents an LCSC electronic component.
type Product struct {
	ProductCode       string       `json:"productCode"`       // LCSC part number like "C12345"
	ProductModel      string       `json:"productModel"`      // MPN
	BrandNameEn       string       `json:"brandNameEn"`       // Manufacturer
	ProductIntroEn    string       `json:"productIntroEn"`    // Description
	PdfUrl            string       `json:"pdfUrl"`            // Datasheet URL
	ProductImages     []string     `json:"productImages"`     // Multiple images
	ProductImageUrl   string       `json:"productImageUrl"`   // Primary image
	StockNumber       int          `json:"stockNumber"`       // Stock quantity
	MinPacketNumber   int          `json:"minPacketNumber"`   // Min order qty
	ProductPriceList  []PriceBreak `json:"productPriceList"`  // Price breaks
	ParamVOList       []Parameter  `json:"paramVOList"`       // Specs/parameters
	EncapStandard     string       `json:"encapStandard"`     // Footprint/package
	ParentCatalogName string       `json:"parentCatalogName"` // Parent category
	CatalogName       string       `json:"catalogName"`       // Subcategory
	Weight            float64      `json:"weight"`            // Weight in grams
}

// GetProductURL returns the LCSC product page URL.
func (p *Product) GetProductURL() string {
	return fmt.Sprintf("https://www.lcsc.com/product-detail/%s.html", p.ProductCode)
}

// SearchRequest contains parameters for a product search.
type SearchRequest struct {
	Keyword     string
	CurrentPage int
	PageSize    int
	IsAvailable bool
	MatchType   string // "exact" or "fuzzy"
}

// SearchResponse contains the results of a product search.
type SearchResponse struct {
	Products        []Product
	TotalCount      int
	DirectMatchCode string // Set when tipProductDetailUrlVO indicates exact match
}

// searchResponseWrapper matches the actual LCSC API response structure.
type searchResponseWrapper struct {
	ProductSearchResultVO struct {
		ProductList []Product `json:"productList"`
		TotalCount  int       `json:"totalCount"`
	} `json:"productSearchResultVO"`
	TipProductDetailUrlVO *struct {
		ProductCode string `json:"productCode"`
	} `json:"tipProductDetailUrlVO"`
}

// ProductResponse contains a single product response.
type ProductResponse struct {
	Product Product `json:"result"`
}

// CategoryProductsResponse contains products for a category.
type CategoryProductsResponse struct {
	Products   []Product `json:"productList"`
	TotalCount int       `json:"total"`
	PageSize   int       `json:"pageSize"`
	PageNumber int       `json:"currentPage"`
}

// apiResponse is the common response wrapper from LCSC API.
type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"msg"`
	Result  json.RawMessage `json:"result"`
}
