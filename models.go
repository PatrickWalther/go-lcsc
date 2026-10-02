package lcsc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SupportedCurrencies maps the currency codes that LCSC supports to the
// currency symbols that LCSC sends in price rows. CNY uses the full-width
// yen sign U+FFE5. LCSC answers in USD for any other currency code (see
// [WithCurrency]).
var SupportedCurrencies = map[string]string{
	"USD": "$",
	"CNY": "\uFFE5",
	"EUR": "\u20AC",
	"HKD": "HK$",
}

// currencyForSymbol returns the code in [SupportedCurrencies] for symbol.
// It returns an empty string when no code has that symbol.
func currencyForSymbol(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return ""
	}
	for code, s := range SupportedCurrencies {
		if s == symbol {
			return code
		}
	}
	return ""
}

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
// Null and an empty string give 0.
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
		str = strings.TrimSpace(str)
		if str == "" {
			*f = 0
			return nil
		}
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
	Ladder int `json:"ladder"`

	// ProductPrice is the unit price in USD. LCSC sends USD in this field
	// for every currency cookie, also when CurrencySymbol is not "$". Use
	// [PriceBreak.Price] for the price in the response currency.
	ProductPrice FlexFloat64 `json:"productPrice"`

	// USDPrice is the unit price in USD. LCSC sends it as a number.
	USDPrice FlexFloat64 `json:"usdPrice"`

	// CurrencyPrice is the unit price in the response currency. LCSC
	// selects this currency from the currency cookie (see [WithCurrency]
	// and [Product.Currency]). LCSC rounds the converted price. Do not
	// calculate it again from USDPrice.
	CurrencyPrice FlexFloat64 `json:"currencyPrice"`

	// CurrencySymbol is the symbol of the response currency, for example
	// "$" or "€". It applies to CurrencyPrice. It does not apply to
	// ProductPrice.
	CurrencySymbol string `json:"currencySymbol"`
}

// Price returns the unit price in the response currency. It returns
// CurrencyPrice when CurrencyPrice is more than zero. Else it returns
// ProductPrice, which is in USD.
func (pb PriceBreak) Price() float64 {
	if pb.CurrencyPrice > 0 {
		return float64(pb.CurrencyPrice)
	}
	return float64(pb.ProductPrice)
}

// FlashSale is a time-limited offer from third-party stock. LCSC sends it
// in flashSaleProductPO. The offer has its own quantity, minimum order,
// price and delivery time.
type FlashSale struct {
	// ValidNumber is the quantity on offer. Show this value. Do not show
	// recommendNumber, because it can be larger than the quantity on
	// offer.
	ValidNumber int `json:"validNumber"`

	// MinOrderNumber is the minimum order quantity of the offer.
	MinOrderNumber int `json:"minOrderNum"`

	// Split is the order multiple of the offer. The meaning comes from the
	// product field with the same name (inferred).
	Split int `json:"split"`

	// SellPrice is the unit price in SellCurrencyType.
	SellPrice FlexFloat64 `json:"sellPrice"`

	// USDPrice is the unit price in USD.
	USDPrice FlexFloat64 `json:"usdPrice"`

	// SellCurrencyType is the currency code of SellPrice, for example
	// "EUR".
	SellCurrencyType string `json:"sellCurrencyType"`

	// CurrencySymbol is the symbol of SellCurrencyType.
	CurrencySymbol string `json:"currencySymbol"`

	// DeliveryTimeWayDays holds the minimum and the maximum delivery time
	// in days, for example [7, 9].
	DeliveryTimeWayDays []int `json:"deliveryTimeWayDays"`

	// ExpiredTime is the end of the offer as LCSC sends it, for example
	// "2026-10-13 23:59:59". LCSC sends no time zone.
	ExpiredTime string `json:"expiredTime"`

	// IsOnsale is true when the offer is open.
	IsOnsale bool `json:"isOnsale"`

	// BatchCode is the date code of the offered lot, for example "25/26+".
	BatchCode string `json:"productBatchCodeEn"`
}

// Price returns the unit price of the offer. It returns SellPrice when
// SellPrice is more than zero. Else it returns USDPrice.
func (f *FlashSale) Price() float64 {
	if f == nil {
		return 0
	}
	if f.SellPrice > 0 {
		return float64(f.SellPrice)
	}
	return float64(f.USDPrice)
}

// DeliveryDays returns the minimum and the maximum delivery time in days.
// ok is false when the offer has no delivery time.
func (f *FlashSale) DeliveryDays() (minDays, maxDays int, ok bool) {
	if f == nil || len(f.DeliveryTimeWayDays) == 0 {
		return 0, 0, false
	}
	minDays = f.DeliveryTimeWayDays[0]
	maxDays = f.DeliveryTimeWayDays[len(f.DeliveryTimeWayDays)-1]
	return minDays, maxDays, true
}

// Lifecycle is the lifecycle state of a product. [Product.Lifecycle] gets
// it from the LCSC fields.
type Lifecycle string

const (
	// LifecycleUnknown means that the record has no lifecycle data.
	LifecycleUnknown Lifecycle = "unknown"

	// LifecycleActive means that LCSC sells the product as a normal
	// product (productCycle "normal").
	LifecycleActive Lifecycle = "active"

	// LifecycleNotRecommended means that LCSC sells only the remaining
	// stock, for example for productCycle "sold_out". The LCSC site shows
	// "Not recommended for new".
	LifecycleNotRecommended Lifecycle = "not_recommended"

	// LifecycleDiscontinued means that the product is discontinued
	// (productCycle "stop_product").
	LifecycleDiscontinued Lifecycle = "discontinued"
)

const (
	productCycleNormal  = "normal"
	productCycleStopped = "stop_product"
)

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
	// for example "normal", "sold_out" or "stop_product". Use
	// [Product.Lifecycle] for a typed value.
	ProductCycle string `json:"productCycle"`

	// IsPreSale is true when LCSC sells the product as a pre-sale item.
	IsPreSale bool `json:"isPreSale"`

	// ProductID is the numeric LCSC id of the product, for example 1877
	// for C1525. It is equal to the JLCPCB lcscComponentId and to the
	// componentId of the JLCPCB search.
	ProductID int64 `json:"productId"`

	// CurrencyType is the code of the response currency, for example
	// "EUR". Only [ProductService.Details] fills it. Use
	// [Product.Currency] for all response types.
	CurrencyType string `json:"currencyType"`

	// BrandID is the LCSC id of the manufacturer.
	BrandID int `json:"brandId"`

	// WmCatalogID is the id of the leaf category in the LCSC category
	// tree (see [CatalogService]).
	WmCatalogID int `json:"wmCatalogId"`

	// WmCatalogNameEn is the English name of the WmCatalogID category.
	WmCatalogNameEn string `json:"wmCatalogNameEn"`

	// ParentCatalogList holds the parent categories of WmCatalogID, from
	// the root category down. Only detail responses send it. Use
	// [Product.CatalogPath].
	ParentCatalogList []CategoryRef `json:"parentCatalogList"`

	// FirstWmCatalogID to SixthWmCatalogID and their names are the
	// category path of a list row, from the root category down. The levels
	// below the leaf category are 0 and empty. Only list rows send them.
	// Use [Product.CatalogPath].
	FirstWmCatalogID      int    `json:"firstWmCatalogId"`
	FirstWmCatalogNameEn  string `json:"firstWmCatalogNameEn"`
	SecondWmCatalogID     int    `json:"secondWmCatalogId"`
	SecondWmCatalogNameEn string `json:"secondWmCatalogNameEn"`
	ThirdWmCatalogID      int    `json:"thirdWmCatalogId"`
	ThirdWmCatalogNameEn  string `json:"thirdWmCatalogNameEn"`
	FourthWmCatalogID     int    `json:"fourthWmCatalogId"`
	FourthWmCatalogNameEn string `json:"fourthWmCatalogNameEn"`
	FifthWmCatalogID      int    `json:"fifthWmCatalogId"`
	FifthWmCatalogNameEn  string `json:"fifthWmCatalogNameEn"`
	SixthWmCatalogID      int    `json:"sixthWmCatalogId"`
	SixthWmCatalogNameEn  string `json:"sixthWmCatalogNameEn"`

	// ProductImageURLBig is the 900x900 version of ProductImageURL. Detail
	// responses do not send it. For a detail response, use ProductImages.
	ProductImageURLBig string `json:"productImageUrlBig"`

	// IsNotOverstock is true when LCSC refuses an order quantity above
	// StockNumber. LCSC sets it exactly when ProductCycle is not "normal".
	IsNotOverstock bool `json:"isNotOverstock"`

	// IsForeignOnsale is false when LCSC does not sell the product to
	// overseas customers. It is nil when the response does not send it.
	IsForeignOnsale *bool `json:"isForeignOnsale"`

	// HasThirdPartyStock is true when marketplace offers exist for the
	// product. Only list rows (/product/query/list) send a correct value.
	// Detail responses send false also for products with offers.
	HasThirdPartyStock bool `json:"hasThirdPartyStock"`

	// HasAlternatePart is true when LCSC has cross-reference alternates
	// for the product. Only list rows send it. It is nil for detail
	// responses.
	HasAlternatePart *bool `json:"hasAlternatePart"`

	// MaxBuyNumber is the maximum order quantity. LCSC sends -1 when no
	// maximum applies.
	MaxBuyNumber int `json:"maxBuyNumber"`

	// IsReel is true when LCSC offers a reel for the product. ReelPrice is
	// the fee for the reel. The meaning comes from the field names and the
	// data (inferred).
	IsReel bool `json:"isReel"`

	// ReelPrice is the reel fee in the response currency (see
	// [Product.Currency]), for example 3.0 USD or EUR, or 21.0 CNY or
	// HKD. It is 0 for products that LCSC does not supply on a reel, for
	// example tray parts.
	ReelPrice FlexFloat64 `json:"reelPrice"`

	// ProductArrange is the packaging, for example "Tape & Reel (TR)".
	ProductArrange string `json:"productArrange"`

	// StockSz, StockJs and WmStockHk are the stock quantities of the LCSC
	// warehouses. Their sum is StockNumber. Detail responses send only
	// StockSz.
	StockSz   int `json:"stockSz"`
	StockJs   int `json:"stockJs"`
	WmStockHk int `json:"wmStockHk"`

	// Eccn is the export control classification number, for example
	// "EAR99".
	Eccn string `json:"eccn"`

	// MoistureSensitivityLevel is the moisture sensitivity level as LCSC
	// sends it. The text is in Chinese, for example "1级(无限)" (level 1,
	// unlimited floor life). Only list rows send it.
	MoistureSensitivityLevel string `json:"moistureSensitivityLevel"`

	// FlashSale is the time-limited third-party offer for the product. It
	// is nil when no offer exists.
	FlashSale *FlashSale `json:"flashSaleProductPO"`

	// MatchType is the code that LCSC gives to an alternate part, for
	// example "1", "4", "5" or "6". LCSC does not document the codes. Only
	// the entries of AlternatePartList and of
	// [AlternatesResponse.Alternates] have a value. Use [Product.Match]
	// to get a [MatchType] with a label.
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

// CatalogPath returns the category path of the product, from the root
// category down to the leaf category (WmCatalogID). It uses the first
// source that it finds:
//
//  1. ParentCatalogList and WmCatalogID. Detail responses send them.
//  2. FirstWmCatalogID to SixthWmCatalogID. List rows send them.
//  3. WmCatalogID alone.
//
// It returns nil when the product has no category id.
func (p *Product) CatalogPath() []CategoryRef {
	if p == nil {
		return nil
	}

	leaf := CategoryRef{ID: p.WmCatalogID, Name: strings.TrimSpace(p.WmCatalogNameEn)}
	if len(p.ParentCatalogList) > 0 {
		path := append([]CategoryRef(nil), p.ParentCatalogList...)
		if leaf.ID > 0 && path[len(path)-1].ID != leaf.ID {
			path = append(path, leaf)
		}
		return path
	}

	levels := []CategoryRef{
		{ID: p.FirstWmCatalogID, Name: p.FirstWmCatalogNameEn},
		{ID: p.SecondWmCatalogID, Name: p.SecondWmCatalogNameEn},
		{ID: p.ThirdWmCatalogID, Name: p.ThirdWmCatalogNameEn},
		{ID: p.FourthWmCatalogID, Name: p.FourthWmCatalogNameEn},
		{ID: p.FifthWmCatalogID, Name: p.FifthWmCatalogNameEn},
		{ID: p.SixthWmCatalogID, Name: p.SixthWmCatalogNameEn},
	}
	var path []CategoryRef
	for _, level := range levels {
		if level.ID <= 0 {
			break
		}
		path = append(path, CategoryRef{ID: level.ID, Name: strings.TrimSpace(level.Name)})
	}
	if len(path) > 0 {
		return path
	}

	if leaf.ID > 0 {
		return []CategoryRef{leaf}
	}
	return nil
}

// Currency returns the code of the response currency. [PriceBreak.Price]
// and ReelPrice use this currency. Currency uses the first value that it
// finds:
//
//  1. CurrencyType. Only detail responses send it.
//  2. The code in [SupportedCurrencies] for the CurrencySymbol of a price
//     break. List rows send only this symbol.
//  3. "USD".
func (p *Product) Currency() string {
	if p == nil {
		return defaultCurrency
	}
	if code := strings.ToUpper(strings.TrimSpace(p.CurrencyType)); code != "" {
		return code
	}
	for _, pb := range p.ProductPriceList {
		if code := currencyForSymbol(pb.CurrencySymbol); code != "" {
			return code
		}
	}
	return defaultCurrency
}

// Lifecycle returns the lifecycle state of the product:
//
//   - [LifecycleDiscontinued] when ProductCycle is "stop_product".
//   - [LifecycleNotRecommended] when ProductCycle has a value other than
//     "normal", or when ProductCycle is empty and IsNotOverstock is true.
//   - [LifecycleActive] when ProductCycle is "normal". Also when
//     ProductCycle is empty but the record has other lifecycle data
//     (IsForeignOnsale is set).
//   - [LifecycleUnknown] when the record has no lifecycle data.
func (p *Product) Lifecycle() Lifecycle {
	if p == nil {
		return LifecycleUnknown
	}
	cycle := strings.ToLower(strings.TrimSpace(p.ProductCycle))
	switch {
	case cycle == productCycleStopped:
		return LifecycleDiscontinued
	case cycle != "" && cycle != productCycleNormal:
		return LifecycleNotRecommended
	case cycle == "" && p.IsNotOverstock:
		// LCSC sets isNotOverstock exactly when productCycle is not
		// "normal".
		return LifecycleNotRecommended
	case cycle == productCycleNormal:
		return LifecycleActive
	case p.IsForeignOnsale != nil:
		// The record has lifecycle fields, but no cycle.
		return LifecycleActive
	default:
		return LifecycleUnknown
	}
}

// AllowsBackorder reports whether LCSC accepts an order quantity above
// StockNumber. It returns false when IsNotOverstock is true, because the
// LCSC cart then refuses a quantity above StockNumber. It also returns
// false when IsForeignOnsale is false, because LCSC then does not sell the
// product to overseas customers. LCSC does not document this rule. It comes
// from the behavior of the LCSC site (inferred).
func (p *Product) AllowsBackorder() bool {
	if p == nil || p.IsNotOverstock {
		return false
	}
	if p.IsForeignOnsale != nil && !*p.IsForeignOnsale {
		return false
	}
	return true
}
