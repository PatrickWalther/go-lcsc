package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ThirdPartyService handles marketplace offer operations. A marketplace
// offer is stock of a third-party supplier, for example Waldom or
// Rochester, that LCSC sells in addition to its own stock. The LCSC site
// shows the offers on the product page.
type ThirdPartyService service

const (
	// defaultOffersPageSize is the page size that the client sends when
	// the request has no page size.
	defaultOffersPageSize = 10

	// maxOffersPageSize is the largest page size that the client sends.
	// LCSC answered page size 100. A larger page size was not checked
	// (inferred limit, the same as the other LCSC list endpoints).
	maxOffersPageSize = 100
)

// OffersRequest contains the parameters for [ThirdPartyService.Offers].
// Set ProductCode or Keyword. Do not set both.
type OffersRequest struct {
	// ProductCode is the LCSC product code, for example "C8734". The
	// client changes it to upper case. The response holds only the offers
	// for this product.
	ProductCode string

	// Keyword is a manufacturer part number or a part of it, for example
	// "STM32F103". The response holds the offers of all products that
	// match the keyword. One product can have many offers.
	Keyword string

	// Page is the page number. The first page is 1. The client sends 1
	// when Page is 0.
	Page int

	// PageSize is the number of offers on one page, from 1 to 100. The
	// client sends 10 when PageSize is 0.
	PageSize int
}

// OffersResponse contains one page of marketplace offers.
type OffersResponse struct {
	// Offers holds the offers on the page in the server order.
	Offers []Offer

	// TotalCount is the number of offers on all pages.
	TotalCount int

	// Page is the page number that the client sent.
	Page int

	// PageSize is the page size that the client sent.
	PageSize int
}

// Offer is one marketplace offer of a third-party supplier. The offer has
// its own stock, minimum order, order multiple, price ladder and delivery
// time. The values are not the values of the LCSC stock of the product.
//
// The offers are information only. JLCPCB pre-orders do not use them
// (inferred).
type Offer struct {
	// ProductCode is the LCSC product code, for example "C8734".
	ProductCode string `json:"productCode"`

	// ManufacturerPartNumber is the manufacturer part number, for example
	// "STM32F103C8T6".
	ManufacturerPartNumber string `json:"productCodeManufacturer"`

	// ProductModel is the title of the offer. For most suppliers, it holds
	// the part number, the brand of the supplier and the date code, for
	// example "STM32F103C8T6 ST 26+". For Rochester, it holds an id of the
	// supplier, for example "01t4w00000PQNH0AAP". Use
	// ManufacturerPartNumber for the part number.
	ProductModel string `json:"productModel"`

	// BrandID is the LCSC id of the manufacturer.
	BrandID int `json:"brandId"`

	// BrandNameEn is the LCSC name of the manufacturer.
	BrandNameEn string `json:"brandNameEn"`

	// SupplierBrandName is the manufacturer name that the supplier uses,
	// for example "STMICRO".
	SupplierBrandName string `json:"lcOrderBrandNameEn"`

	// EncapStandard is the package. Most offers do not send it.
	EncapStandard string `json:"encapStandard"`

	// ProductIntroEn is the description. Most offers do not send it.
	ProductIntroEn string `json:"productIntroEn"`

	// Source is the supplier, for example "waldom" or "rochester".
	Source string `json:"productSource"`

	// SupplyChannelType is the supply channel, for example "lc_order".
	SupplyChannelType string `json:"supplyChannelType"`

	// VendorCode is the LCSC code of the offer, for example "G12277".
	VendorCode string `json:"vendorCode"`

	// StockNumber is the quantity on offer.
	StockNumber int `json:"stockNumber"`

	// MinBuyNumber is the minimum order quantity (MOQ) of the offer.
	MinBuyNumber int `json:"minBuyNumber"`

	// MinPacketNumber is the packet quantity. It is 0 when the response
	// does not send it.
	MinPacketNumber int `json:"minPacketNumber"`

	// Split is the order multiple of the offer.
	Split int `json:"split"`

	// DeliveryTimeWayDays holds the minimum and the maximum delivery time
	// in days, for example [3, 15]. Use [Offer.DeliveryDays].
	DeliveryTimeWayDays []int `json:"deliveryTimeWayDays"`

	// ProductPriceList is the price ladder of the offer. Each break has
	// CurrencyPrice and USDPrice. It has no ProductPrice. Use
	// [PriceBreak.Price] and [Offer.Currency].
	ProductPriceList []PriceBreak `json:"productPriceList"`

	// BatchCode is the date code of the offered lot, for example "26+" or
	// "2551". It is empty when the supplier does not send it.
	BatchCode string `json:"batchNumberEn"`

	// IsOnsale is true when the offer is open.
	IsOnsale bool `json:"isOnsale"`

	// Eccn is the export control classification number, for example
	// "3A991A2".
	Eccn string `json:"eccn"`

	// PdfURL is the datasheet URL of the offer. Most offers do not send
	// it.
	PdfURL string `json:"pdfUrl"`

	// ProductImageURL and ProductImageURLBig are the images of the offer.
	// Most offers do not send them. A Rochester offer can send an image
	// URL that is not on an LCSC host.
	ProductImageURL    string `json:"productImageUrl"`
	ProductImageURLBig string `json:"productImageUrlBig"`

	// IsPriceFirst, IsStockFirst and IsDeliveryTimeFirst mark the offer
	// with the best price, the most stock and the shortest delivery time.
	// The LCSC site shows them as badges. The meaning comes from the field
	// names and the data (inferred). LCSC sends null for false. LCSC sets
	// the badges only for a ProductCode request. For a Keyword request, all
	// offers have false, also the offers that have a badge in the
	// ProductCode response.
	IsPriceFirst        bool `json:"isPriceFirst"`
	IsStockFirst        bool `json:"isStockFirst"`
	IsDeliveryTimeFirst bool `json:"isDeliveryTimeFirst"`
}

// DeliveryDays returns the minimum and the maximum delivery time in days.
// ok is false when the offer has no delivery time.
func (o *Offer) DeliveryDays() (minDays, maxDays int, ok bool) {
	if o == nil {
		return 0, 0, false
	}
	return deliveryDays(o.DeliveryTimeWayDays)
}

// Currency returns the code of the currency of [PriceBreak.Price] for the
// offer. Offer rows send no currencyType, so Currency uses the code in
// [SupportedCurrencies] for the CurrencySymbol of a price break. It returns
// "USD" when no symbol matches.
func (o *Offer) Currency() string {
	if o == nil {
		return defaultCurrency
	}
	for _, pb := range o.ProductPriceList {
		if code := currencyForSymbol(pb.CurrencySymbol); code != "" {
			return code
		}
	}
	return defaultCurrency
}

type offersBody struct {
	CurrentPage int    `json:"currentPage"`
	PageSize    int    `json:"pageSize"`
	ProductCode string `json:"productCode,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
}

type offersWrapper struct {
	TotalCount  int     `json:"totalCount"`
	ProductList []Offer `json:"productList"`
}

// Offers returns the marketplace offers for a product code or a keyword.
// It uses the /search/third endpoint, which the LCSC product page uses.
//
// The client returns [ErrInvalidRequest] and does not send the request in
// these cases:
//
//   - req is nil.
//   - ProductCode and Keyword are both empty, or both are set.
//   - Page is negative.
//   - PageSize is negative or above 100.
//
// A product with no offers gives an empty Offers list and no error. LCSC
// sends the same empty list for an unknown product code. The client
// caches the response for CacheConfig.SearchTTL.
func (s *ThirdPartyService) Offers(ctx context.Context, req *OffersRequest) (*OffersResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidRequest)
	}

	body := offersBody{
		ProductCode: strings.ToUpper(strings.TrimSpace(req.ProductCode)),
		Keyword:     strings.TrimSpace(req.Keyword),
	}
	switch {
	case body.ProductCode == "" && body.Keyword == "":
		return nil, fmt.Errorf("%w: productCode or keyword is required", ErrInvalidRequest)
	case body.ProductCode != "" && body.Keyword != "":
		return nil, fmt.Errorf("%w: set productCode or keyword, not both", ErrInvalidRequest)
	}

	switch {
	case req.Page < 0:
		return nil, fmt.Errorf("%w: page must not be negative, got %d", ErrInvalidRequest, req.Page)
	case req.Page == 0:
		body.CurrentPage = 1
	default:
		body.CurrentPage = req.Page
	}

	switch {
	case req.PageSize < 0 || req.PageSize > maxOffersPageSize:
		return nil, fmt.Errorf("%w: pageSize must be from 1 to %d, got %d", ErrInvalidRequest, maxOffersPageSize, req.PageSize)
	case req.PageSize == 0:
		body.PageSize = defaultOffersPageSize
	default:
		body.PageSize = req.PageSize
	}

	client := s.client
	cacheKey, keyErr := cacheKeyForBody("offers", client.currency, body)
	useCache := keyErr == nil && client.cacheConfig.Enabled && client.cache != nil
	if useCache {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var resp OffersResponse
			if err := json.Unmarshal(cached, &resp); err == nil {
				return &resp, nil
			}
		}
	}

	var wrapper offersWrapper
	if err := client.do(ctx, http.MethodPost, "/search/third", nil, body, &wrapper); err != nil {
		return nil, err
	}

	resp := &OffersResponse{
		Offers:     wrapper.ProductList,
		TotalCount: wrapper.TotalCount,
		Page:       body.CurrentPage,
		PageSize:   body.PageSize,
	}
	if resp.Offers == nil {
		resp.Offers = []Offer{}
	}

	if useCache {
		if data, err := json.Marshal(resp); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return resp, nil
}

// HasStock reports whether marketplace offers exist for a product code. It
// uses the /search/has/third/stock endpoint. Use it instead of
// [Product.HasThirdPartyStock] of a detail response, because detail
// responses send false also for products with offers.
//
// The client changes the code to upper case. LCSC answers false for an
// unknown product code. The client caches the answer for
// CacheConfig.SearchTTL.
func (s *ThirdPartyService) HasStock(ctx context.Context, productCode string) (bool, error) {
	productCode = strings.ToUpper(strings.TrimSpace(productCode))
	if productCode == "" {
		return false, fmt.Errorf("%w: productCode is required", ErrInvalidRequest)
	}

	client := s.client
	cacheKey := cacheKeyForThirdPartyStock(productCode)
	useCache := client.cacheConfig.Enabled && client.cache != nil
	if useCache {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var hasStock bool
			if err := json.Unmarshal(cached, &hasStock); err == nil {
				return hasStock, nil
			}
		}
	}

	params := url.Values{}
	params.Set("productCode", productCode)

	var hasStock bool
	if err := client.do(ctx, http.MethodGet, "/search/has/third/stock", params, nil, &hasStock); err != nil {
		return false, err
	}

	if useCache {
		if data, err := json.Marshal(hasStock); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return hasStock, nil
}

// cacheKeyForThirdPartyStock makes the cache key of [ThirdPartyService.HasStock].
// The answer does not depend on the currency.
func cacheKeyForThirdPartyStock(productCode string) string {
	hash := sha256.Sum256([]byte(productCode))
	return "thirdstock:" + hex.EncodeToString(hash[:8])
}
