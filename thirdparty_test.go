package lcsc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testOffersPath        = "/ftps/wm/search/third"
	testHasThirdStockPath = "/ftps/wm/search/has/third/stock"
)

func TestThirdPartyOffersDecodesFixture(t *testing.T) {
	// Trimmed live response for C8734 (page size 10). The server sent 7
	// offers. The fixture keeps 4 of them.
	fixture := mustReadFixture(t, "search_third_C8734.json")
	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.Method != http.MethodPost || req.URL.Path != testOffersPath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		if req.URL.RawQuery != "" {
			t.Errorf("expected no query, got %q", req.URL.RawQuery)
		}
		if got := req.Header.Get("Cookie"); got != "currencyCode=USD" {
			t.Errorf("unexpected cookie %q", got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type %q", got)
		}
		want := map[string]interface{}{
			"currentPage": float64(1),
			"pageSize":    float64(10),
			"productCode": "C8734",
		}
		if body := decodeJSONBody(t, req); !reflect.DeepEqual(body, want) {
			t.Errorf("unexpected body:\n got %v\nwant %v", body, want)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.ThirdParty.Offers(context.Background(), &OffersRequest{ProductCode: " c8734 "})
	if err != nil {
		t.Fatalf("offers failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if resp.TotalCount != 7 || resp.Page != 1 || resp.PageSize != 10 || len(resp.Offers) != 4 {
		t.Fatalf("unexpected response: total %d, page %d, size %d, %d offers", resp.TotalCount, resp.Page, resp.PageSize, len(resp.Offers))
	}

	best := resp.Offers[0]
	if best.ProductCode != "C8734" || best.ManufacturerPartNumber != "STM32F103C8T6" || best.ProductModel != "STM32F103C8T6 ST(意法半导体) 26+" {
		t.Fatalf("unexpected identity: %q %q %q", best.ProductCode, best.ManufacturerPartNumber, best.ProductModel)
	}
	if best.Source != "waldom" || best.SupplyChannelType != "lc_order" || best.VendorCode != "G12277" {
		t.Fatalf("unexpected source: %q %q %q", best.Source, best.SupplyChannelType, best.VendorCode)
	}
	if best.BrandID != 74 || best.BrandNameEn != "ST" || best.SupplierBrandName != "ST" || best.Eccn != "3A991A2" {
		t.Fatalf("unexpected brand fields: %d %q %q %q", best.BrandID, best.BrandNameEn, best.SupplierBrandName, best.Eccn)
	}
	if best.StockNumber != 5000 || best.MinBuyNumber != 1 || best.MinPacketNumber != 1 || best.Split != 1 {
		t.Fatalf("unexpected quantities: stock %d, moq %d, packet %d, split %d", best.StockNumber, best.MinBuyNumber, best.MinPacketNumber, best.Split)
	}
	if minDays, maxDays, ok := best.DeliveryDays(); !ok || minDays != 13 || maxDays != 15 {
		t.Fatalf("unexpected delivery days: %d-%d (%v)", minDays, maxDays, ok)
	}
	if best.BatchCode != "26+" || !best.IsOnsale || best.PdfURL != "" || best.EncapStandard != "" {
		t.Fatalf("unexpected lot fields: %q %v %q %q", best.BatchCode, best.IsOnsale, best.PdfURL, best.EncapStandard)
	}
	if !best.IsPriceFirst || best.IsStockFirst || best.IsDeliveryTimeFirst {
		t.Fatalf("unexpected badges: price %v, stock %v, delivery %v", best.IsPriceFirst, best.IsStockFirst, best.IsDeliveryTimeFirst)
	}
	if len(best.ProductPriceList) != 6 || best.Currency() != "USD" {
		t.Fatalf("unexpected ladder: %d breaks, currency %q", len(best.ProductPriceList), best.Currency())
	}
	first := best.ProductPriceList[0]
	if first.Ladder != 1 || first.ProductPrice != 0 || first.USDPrice != 1.1861 || first.CurrencyPrice != 1.1861 || first.Price() != 1.1861 {
		t.Fatalf("unexpected first break: %+v (Price %v)", first, first.Price())
	}

	stock := resp.Offers[1]
	if !stock.IsStockFirst || stock.StockNumber != 120000 || stock.MinBuyNumber != 10000 || stock.Split != 10000 {
		t.Fatalf("unexpected stock offer: %+v", stock)
	}
	if stock.SupplierBrandName != "STMICRO" || stock.BatchCode != "22+" || len(stock.ProductPriceList) != 1 || stock.ProductPriceList[0].Price() != 5.2253 {
		t.Fatalf("unexpected stock offer details: %q %q %+v", stock.SupplierBrandName, stock.BatchCode, stock.ProductPriceList)
	}

	fast := resp.Offers[2]
	if minDays, maxDays, ok := fast.DeliveryDays(); !fast.IsDeliveryTimeFirst || !ok || minDays != 3 || maxDays != 5 {
		t.Fatalf("unexpected fast offer: first %v, days %d-%d", fast.IsDeliveryTimeFirst, minDays, maxDays)
	}

	lot := resp.Offers[3]
	if lot.BatchCode != "2551" || lot.MinBuyNumber != 1500 || lot.Split != 1500 || lot.SupplierBrandName != "STMicroelectronics" {
		t.Fatalf("unexpected lot offer: %+v", lot)
	}
}

func TestThirdPartyOffersKeyword(t *testing.T) {
	// Trimmed live response for the keyword STM32F103 (page size 100). The
	// server sent 72 offers. The fixture keeps one Waldom offer with no
	// date code and the only Rochester offer.
	fixture := mustReadFixture(t, "search_third_kw_STM32F103.json")
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		want := map[string]interface{}{
			"currentPage": float64(2),
			"pageSize":    float64(100),
			"keyword":     "STM32F103",
		}
		if body := decodeJSONBody(t, req); !reflect.DeepEqual(body, want) {
			t.Errorf("unexpected body:\n got %v\nwant %v", body, want)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	resp, err := client.ThirdParty.Offers(context.Background(), &OffersRequest{Keyword: " STM32F103 ", Page: 2, PageSize: 100})
	if err != nil {
		t.Fatalf("offers failed: %v", err)
	}
	if resp.TotalCount != 72 || resp.Page != 2 || resp.PageSize != 100 || len(resp.Offers) != 2 {
		t.Fatalf("unexpected response: total %d, page %d, size %d, %d offers", resp.TotalCount, resp.Page, resp.PageSize, len(resp.Offers))
	}

	// LCSC sends the date code only in Chinese (batchNumber) for this
	// offer. BatchCode reads batchNumberEn, which is null.
	waldom := resp.Offers[0]
	if waldom.ProductCode != "C882165" || waldom.BatchCode != "" || waldom.IsPriceFirst {
		t.Fatalf("unexpected Waldom offer: %+v", waldom)
	}

	rochester := resp.Offers[1]
	if rochester.Source != "rochester" || rochester.ProductCode != "C2053946" || rochester.ManufacturerPartNumber != "STM32F103C6T6ATR" {
		t.Fatalf("unexpected Rochester offer: %q %q %q", rochester.Source, rochester.ProductCode, rochester.ManufacturerPartNumber)
	}
	// For Rochester, productModel is an id of the supplier.
	if rochester.ProductModel != "01t4w00000PQNH0AAP" {
		t.Fatalf("unexpected product model %q", rochester.ProductModel)
	}
	if rochester.MinPacketNumber != 0 || rochester.MinBuyNumber != 95 || rochester.BatchCode != "" {
		t.Fatalf("expected null fields as zero values, got packet %d, moq %d, batch %q", rochester.MinPacketNumber, rochester.MinBuyNumber, rochester.BatchCode)
	}
	if minDays, maxDays, ok := rochester.DeliveryDays(); !ok || minDays != 12 || maxDays != 20 {
		t.Fatalf("unexpected delivery days: %d-%d", minDays, maxDays)
	}
	// The Rochester image is not on an LCSC host, so the size helper does
	// not change it.
	if rochester.ProductImageURLBig == "" || ImageURLAtSize(rochester.ProductImageURLBig, ImageSizeSmall) != rochester.ProductImageURLBig {
		t.Fatalf("unexpected image URL %q", rochester.ProductImageURLBig)
	}
}

func TestThirdPartyOffersCurrencyEUR(t *testing.T) {
	// Trimmed live response for C1525 under the EUR cookie. The offer
	// price is 0.0016 EUR and 0.0017 USD.
	fixture := mustReadFixture(t, "search_third_C1525_EUR.json")
	client := NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Cookie"); got != "currencyCode=EUR" {
				t.Errorf("unexpected cookie %q", got)
			}
			return jsonResponse(http.StatusOK, fixture), nil
		})),
		WithCurrency("EUR"),
		WithoutRetry(),
		WithoutCache(),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.ThirdParty.Offers(context.Background(), &OffersRequest{ProductCode: "C1525", PageSize: 3})
	if err != nil {
		t.Fatalf("offers failed: %v", err)
	}
	if len(resp.Offers) != 1 {
		t.Fatalf("expected 1 offer, got %d", len(resp.Offers))
	}
	offer := resp.Offers[0]
	if offer.Currency() != "EUR" {
		t.Fatalf("expected EUR, got %q", offer.Currency())
	}
	pb := offer.ProductPriceList[0]
	if pb.Ladder != 100000 || pb.Price() != 0.0016 || pb.USDPrice != 0.0017 || pb.CurrencySymbol != "€" {
		t.Fatalf("unexpected price break: %+v (Price %v)", pb, pb.Price())
	}
	if offer.MinBuyNumber != 100000 || offer.Split != 10000 || offer.StockNumber != 200000 || offer.EncapStandard != "0402-X7R-16V" {
		t.Fatalf("unexpected offer: %+v", offer)
	}
}

func TestThirdPartyOffersEmpty(t *testing.T) {
	// C2040 has no offers. LCSC sends the same shape for the unknown code
	// C99999999.
	bodies := map[string]string{
		"empty list":  `{"code":200,"msg":null,"result":{"totalCount":0,"currentPage":1,"pageSize":10,"currentCount":0,"productList":[]},"ok":true}`,
		"null list":   `{"code":200,"msg":null,"result":{"totalCount":0,"productList":null},"ok":true}`,
		"null result": `{"code":200,"msg":null,"result":null,"ok":true}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			})
			defer func() { _ = client.Close() }()

			resp, err := client.ThirdParty.Offers(context.Background(), &OffersRequest{ProductCode: "C2040"})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if resp.Offers == nil || len(resp.Offers) != 0 || resp.TotalCount != 0 {
				t.Fatalf("expected an empty list, got %+v", resp)
			}
		})
	}
}

func TestThirdPartyOffersValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *OffersRequest
	}{
		{"nil request", nil},
		{"no code and no keyword", &OffersRequest{ProductCode: " ", Keyword: " "}},
		{"code and keyword", &OffersRequest{ProductCode: "C8734", Keyword: "STM32F103"}},
		{"negative page", &OffersRequest{ProductCode: "C8734", Page: -1}},
		{"negative page size", &OffersRequest{ProductCode: "C8734", PageSize: -1}},
		{"page size above 100", &OffersRequest{ProductCode: "C8734", PageSize: 101}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				t.Fatal("expected no request")
				return nil, nil
			})
			defer func() { _ = client.Close() }()

			if _, err := client.ThirdParty.Offers(context.Background(), tt.req); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestThirdPartyOffersEnvelope405IsNotRetried(t *testing.T) {
	recordRetrySleeps(t)
	var calls int32
	client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":405,"msg":"Invalid field. Please check again.","result":null,"ok":false}`), nil
	})
	defer func() { _ = client.Close() }()

	_, err := client.ThirdParty.Offers(context.Background(), &OffersRequest{ProductCode: "C8734"})
	var apiErr *APIError
	if !errors.Is(err, ErrInvalidRequest) || !errors.As(err, &apiErr) || apiErr.Message != "Invalid field. Please check again." {
		t.Fatalf("expected ErrInvalidRequest with the message, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestThirdPartyOffersIsCached(t *testing.T) {
	fixture := mustReadFixture(t, "search_third_C8734.json")
	cache := newRecordingCache()
	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	first, err := client.ThirdParty.Offers(ctx, &OffersRequest{ProductCode: "C8734"})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	second, err := client.ThirdParty.Offers(ctx, &OffersRequest{ProductCode: "c8734", PageSize: 10})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request for the same body, got %d", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("expected the cached response to equal the first response")
	}
	if len(cache.ttls) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.ttls))
	}
	for key, ttl := range cache.ttls {
		if ttl != time.Minute {
			t.Fatalf("expected the search TTL for %s, got %v", key, ttl)
		}
	}

	if _, err := client.ThirdParty.Offers(ctx, &OffersRequest{ProductCode: "C8734", Page: 2}); err != nil {
		t.Fatalf("third call failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected a new request for another page, got %d requests", got)
	}
}

func TestThirdPartyHasStock(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		// Live responses for C1525 (offers) and C2040 (no offers).
		{"offers", `{"code":200,"msg":null,"result":true,"ok":true}`, true},
		{"no offers", `{"code":200,"msg":null,"result":false,"ok":true}`, false},
		{"null result", `{"code":200,"msg":null,"result":null,"ok":true}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != testHasThirdStockPath {
					t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
				}
				if got := req.URL.Query().Get("productCode"); got != "C1525" || len(req.URL.Query()) != 1 {
					t.Errorf("unexpected query %q", req.URL.RawQuery)
				}
				return jsonResponse(http.StatusOK, tt.body), nil
			})
			defer func() { _ = client.Close() }()

			got, err := client.ThirdParty.HasStock(context.Background(), " c1525 ")
			if err != nil {
				t.Fatalf("has stock failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestThirdPartyHasStockValidation(t *testing.T) {
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		t.Fatal("expected no request")
		return nil, nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.ThirdParty.HasStock(context.Background(), " "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestThirdPartyHasStockIsCached(t *testing.T) {
	cache := newRecordingCache()
	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":true,"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	for i := 0; i < 2; i++ {
		got, err := client.ThirdParty.HasStock(context.Background(), "C8734")
		if err != nil || !got {
			t.Fatalf("call %d: expected true, got %v (%v)", i, got, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
	if ttl := cache.ttls[cacheKeyForThirdPartyStock("C8734")]; ttl != time.Minute {
		t.Fatalf("expected the search TTL, got %v", ttl)
	}
}

func TestOfferCurrency(t *testing.T) {
	tests := []struct {
		name   string
		offer  *Offer
		expect string
	}{
		{"nil offer", nil, "USD"},
		{"no prices", &Offer{}, "USD"},
		{"dollar", &Offer{ProductPriceList: []PriceBreak{{CurrencySymbol: "$"}}}, "USD"},
		{"yuan", &Offer{ProductPriceList: []PriceBreak{{CurrencySymbol: "￥"}}}, "CNY"},
		{"hong kong dollar", &Offer{ProductPriceList: []PriceBreak{{CurrencySymbol: "HK$"}}}, "HKD"},
		{"unknown symbol first", &Offer{ProductPriceList: []PriceBreak{{CurrencySymbol: "?"}, {CurrencySymbol: "€"}}}, "EUR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.offer.Currency(); got != tt.expect {
				t.Fatalf("expected %q, got %q", tt.expect, got)
			}
		})
	}
}

func TestOfferDeliveryDays(t *testing.T) {
	var nilOffer *Offer
	if _, _, ok := nilOffer.DeliveryDays(); ok {
		t.Fatal("expected no delivery time for a nil offer")
	}
	if _, _, ok := (&Offer{}).DeliveryDays(); ok {
		t.Fatal("expected no delivery time for an empty list")
	}
	if minDays, maxDays, ok := (&Offer{DeliveryTimeWayDays: []int{7}}).DeliveryDays(); !ok || minDays != 7 || maxDays != 7 {
		t.Fatalf("expected 7-7, got %d-%d (%v)", minDays, maxDays, ok)
	}
}
