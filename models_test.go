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

func TestFlexFloat64UnmarshalEmptyAndNull(t *testing.T) {
	for _, raw := range []string{`""`, `"  "`, `null`} {
		f := FlexFloat64(1)
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			t.Fatalf("%s: unmarshal failed: %v", raw, err)
		}
		if f != 0 {
			t.Fatalf("%s: expected 0, got %v", raw, f)
		}
	}
}

func TestPriceBreakDecodesCurrencyFields(t *testing.T) {
	// Live C2040 price break under the EUR cookie. productPrice is a USD
	// string. usdPrice and currencyPrice are numbers.
	raw := []byte(`{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975,"currencyPrice":0.8878,"currencySymbol":"\u20ac"}`)

	var pb PriceBreak
	if err := json.Unmarshal(raw, &pb); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if pb.ProductPrice != 0.9975 || pb.USDPrice != 0.9975 || pb.CurrencyPrice != 0.8878 {
		t.Fatalf("unexpected prices: %+v", pb)
	}
	if pb.CurrencySymbol != "\u20ac" {
		t.Fatalf("unexpected symbol %q", pb.CurrencySymbol)
	}
	if pb.Price() != 0.8878 {
		t.Fatalf("expected Price 0.8878, got %v", pb.Price())
	}
}

func TestPriceBreakPrice(t *testing.T) {
	tests := []struct {
		name string
		pb   PriceBreak
		want float64
	}{
		{"currency price", PriceBreak{ProductPrice: 0.9975, USDPrice: 0.9975, CurrencyPrice: 6.9227}, 6.9227},
		{"no currency price", PriceBreak{ProductPrice: 0.9975}, 0.9975},
		{"zero currency price", PriceBreak{ProductPrice: 0.5, CurrencyPrice: 0}, 0.5},
		// Offer rows send currencyPrice and usdPrice, but no productPrice.
		{"offer row", PriceBreak{USDPrice: 0.0017, CurrencyPrice: 0.0016}, 0.0016},
		{"usd price only", PriceBreak{USDPrice: 1.1861}, 1.1861},
		{"no price", PriceBreak{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pb.Price(); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestPriceBreakPriceIn(t *testing.T) {
	tests := []struct {
		name         string
		pb           PriceBreak
		currency     string
		wantAmount   float64
		wantCurrency string
	}{
		{"currency price", PriceBreak{ProductPrice: 0.9975, USDPrice: 0.9975, CurrencyPrice: 0.8878, CurrencySymbol: "€"}, "EUR", 0.8878, "EUR"},
		{"lower-case response currency", PriceBreak{CurrencyPrice: 6.9227}, " cny ", 6.9227, "CNY"},
		{"no response currency uses the symbol", PriceBreak{CurrencyPrice: 7.9601, CurrencySymbol: "HK$"}, "", 7.9601, "HKD"},
		{"no response currency and unknown symbol", PriceBreak{CurrencyPrice: 1.5, CurrencySymbol: "£"}, "", 1.5, "USD"},
		// LCSC sent the response currency EUR, but no currencyPrice. The
		// price falls back to productPrice, which is in USD.
		{"product price fallback is USD", PriceBreak{ProductPrice: 0.9975, USDPrice: 0.9975}, "EUR", 0.9975, "USD"},
		// Offer rows send no productPrice.
		{"offer row without currency price is USD", PriceBreak{USDPrice: 0.0017, CurrencySymbol: "€"}, "EUR", 0.0017, "USD"},
		{"offer row", PriceBreak{USDPrice: 0.0017, CurrencyPrice: 0.0016, CurrencySymbol: "€"}, "EUR", 0.0016, "EUR"},
		{"no price", PriceBreak{}, "EUR", 0, "USD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, currency := tt.pb.PriceIn(tt.currency)
			if amount != tt.wantAmount || currency != tt.wantCurrency {
				t.Fatalf("expected %v %s, got %v %s", tt.wantAmount, tt.wantCurrency, amount, currency)
			}
			if amount != tt.pb.Price() {
				t.Fatalf("expected the amount of Price %v, got %v", tt.pb.Price(), amount)
			}
		})
	}
}

func TestProductPriceBreakAmountUSDFallback(t *testing.T) {
	// The response currency is EUR, but the first price break has no
	// currencyPrice. Price gives the USD productPrice, and Currency still
	// gives EUR. PriceBreakAmount must label the amount USD.
	raw := `{"currencyType":"EUR","productPriceList":[{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975},{"ladder":10,"productPrice":"0.8","usdPrice":0.8,"currencyPrice":0.712,"currencySymbol":"€"}]}`

	var p Product
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got := p.Currency(); got != "EUR" {
		t.Fatalf("expected the response currency EUR, got %q", got)
	}
	if got := p.ProductPriceList[0].Price(); got != 0.9975 {
		t.Fatalf("expected Price 0.9975, got %v", got)
	}

	if amount, currency := p.PriceBreakAmount(0); amount != 0.9975 || currency != "USD" {
		t.Fatalf("expected 0.9975 USD for the fallback, got %v %s", amount, currency)
	}
	if amount, currency := p.PriceBreakAmount(1); amount != 0.712 || currency != "EUR" {
		t.Fatalf("expected 0.712 EUR, got %v %s", amount, currency)
	}
	for _, i := range []int{-1, 2} {
		if amount, currency := p.PriceBreakAmount(i); amount != 0 || currency != "" {
			t.Fatalf("index %d: expected 0 and no currency, got %v %q", i, amount, currency)
		}
	}
	var nilProduct *Product
	if amount, currency := nilProduct.PriceBreakAmount(0); amount != 0 || currency != "" {
		t.Fatalf("expected 0 and no currency for a nil product, got %v %q", amount, currency)
	}
}

func TestSupportedCurrencies(t *testing.T) {
	want := map[string]string{"USD": "$", "CNY": "\uffe5", "EUR": "\u20ac", "HKD": "HK$"}
	if len(SupportedCurrencies) != len(want) {
		t.Fatalf("expected %d currencies, got %v", len(want), SupportedCurrencies)
	}
	for code, symbol := range want {
		if SupportedCurrencies[code] != symbol {
			t.Fatalf("%s: expected symbol %q, got %q", code, symbol, SupportedCurrencies[code])
		}
	}
}

func TestProductCurrency(t *testing.T) {
	withSymbol := func(symbol string) []PriceBreak {
		return []PriceBreak{{Ladder: 1, ProductPrice: 1, CurrencySymbol: symbol}}
	}
	tests := []struct {
		name string
		p    *Product
		want string
	}{
		{"currency type", &Product{CurrencyType: "EUR", ProductPriceList: withSymbol("$")}, "EUR"},
		{"lower-case currency type", &Product{CurrencyType: " hkd "}, "HKD"},
		{"USD symbol", &Product{ProductPriceList: withSymbol("$")}, "USD"},
		{"CNY symbol", &Product{ProductPriceList: withSymbol("\uffe5")}, "CNY"},
		{"EUR symbol", &Product{ProductPriceList: withSymbol("\u20ac")}, "EUR"},
		{"HKD symbol", &Product{ProductPriceList: withSymbol("HK$")}, "HKD"},
		{"first known symbol", &Product{ProductPriceList: append(withSymbol(""), withSymbol("HK$")...)}, "HKD"},
		{"unknown symbol", &Product{ProductPriceList: withSymbol("\u00a3")}, "USD"},
		{"no prices", &Product{}, "USD"},
		{"nil product", nil, "USD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Currency(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestProductCurrencyFromLiveRows(t *testing.T) {
	// Live C2040 price rows. A row without currencyType gets the currency
	// from the symbol. A JPY cookie gives USD prices, because LCSC does not
	// support JPY.
	tests := []struct {
		name  string
		raw   string
		want  string
		price float64
	}{
		{"CNY row", `{"productCode":"C2040","productPriceList":[{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975,"currencyPrice":6.9227,"currencySymbol":"\uffe5"}]}`, "CNY", 6.9227},
		{"HKD row", `{"productCode":"C2040","productPriceList":[{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975,"currencyPrice":7.9601,"currencySymbol":"HK$"}]}`, "HKD", 7.9601},
		{"JPY cookie detail", `{"productCode":"C2040","currencyType":"USD","productPriceList":[{"ladder":1,"productPrice":"0.9975","usdPrice":0.9975,"currencyPrice":0.9975,"currencySymbol":"$"}]}`, "USD", 0.9975},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Product
			if err := json.Unmarshal([]byte(tt.raw), &p); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if got := p.Currency(); got != tt.want {
				t.Fatalf("expected currency %q, got %q", tt.want, got)
			}
			if got := p.ProductPriceList[0].Price(); got != tt.price {
				t.Fatalf("expected price %v, got %v", tt.price, got)
			}
			if p.ProductPriceList[0].ProductPrice != 0.9975 {
				t.Fatalf("expected USD product price 0.9975, got %v", p.ProductPriceList[0].ProductPrice)
			}
		})
	}
}

func TestProductLifecycle(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		p    *Product
		want Lifecycle
	}{
		{"normal", &Product{ProductCycle: "normal", IsForeignOnsale: &yes}, LifecycleActive},
		{"normal without other fields", &Product{ProductCycle: "normal"}, LifecycleActive},
		{"upper-case normal", &Product{ProductCycle: " NORMAL "}, LifecycleActive},
		{"stop_product", &Product{ProductCycle: "stop_product", IsNotOverstock: true, IsForeignOnsale: &yes}, LifecycleDiscontinued},
		{"stop_product not on sale", &Product{ProductCycle: "stop_product", IsNotOverstock: true, IsForeignOnsale: &no}, LifecycleDiscontinued},
		{"stop_product without overstock flag", &Product{ProductCycle: "stop_product"}, LifecycleDiscontinued},
		{"sold_out", &Product{ProductCycle: "sold_out", IsNotOverstock: true, IsForeignOnsale: &yes}, LifecycleNotRecommended},
		{"other cycle with overstock flag", &Product{ProductCycle: "not_recommend", IsNotOverstock: true}, LifecycleNotRecommended},
		{"other cycle without overstock flag", &Product{ProductCycle: "sold_out", IsForeignOnsale: &yes}, LifecycleUnknown},
		{"on_sale", &Product{ProductCycle: "on_sale", IsForeignOnsale: &yes}, LifecycleActive},
		{"on_sale with overstock flag", &Product{ProductCycle: "on_sale", IsNotOverstock: true}, LifecycleNotRecommended},
		{"normal with overstock flag", &Product{ProductCycle: "normal", IsNotOverstock: true}, LifecycleNotRecommended},
		{"empty cycle with overstock flag", &Product{IsNotOverstock: true}, LifecycleNotRecommended},
		{"empty cycle with lifecycle fields", &Product{ProductCode: "C1", IsForeignOnsale: &yes}, LifecycleActive},
		{"no lifecycle data", &Product{ProductCode: "C1"}, LifecycleUnknown},
		{"nil product", nil, LifecycleUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Lifecycle(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestProductLifecycleFromLiveRows(t *testing.T) {
	// Lifecycle fields of live detail responses: C6119803 (stop_product),
	// C49211289 (sold_out) and C1525 (normal).
	tests := []struct {
		raw       string
		lifecycle Lifecycle
		backorder bool
	}{
		{`{"productCode":"C6119803","productCycle":"stop_product","isNotOverstock":true,"isForeignOnsale":true}`, LifecycleDiscontinued, false},
		{`{"productCode":"C49211289","productCycle":"sold_out","isNotOverstock":true,"isForeignOnsale":true}`, LifecycleNotRecommended, false},
		{`{"productCode":"C1525","productCycle":"normal","isNotOverstock":false,"isForeignOnsale":true}`, LifecycleActive, true},
	}
	for _, tt := range tests {
		var p Product
		if err := json.Unmarshal([]byte(tt.raw), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if got := p.Lifecycle(); got != tt.lifecycle {
			t.Fatalf("%s: expected %q, got %q", p.ProductCode, tt.lifecycle, got)
		}
		if got := p.AllowsBackorder(); got != tt.backorder {
			t.Fatalf("%s: expected AllowsBackorder %v, got %v", p.ProductCode, tt.backorder, got)
		}
	}
}

func TestProductAllowsBackorder(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		p    *Product
		want bool
	}{
		{"normal", &Product{ProductCycle: "normal", IsForeignOnsale: &yes}, true},
		{"no flags", &Product{}, true},
		{"not overstock", &Product{IsNotOverstock: true, IsForeignOnsale: &yes}, false},
		{"not on sale overseas", &Product{IsForeignOnsale: &no}, false},
		{"nil product", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.AllowsBackorder(); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestFlashSalePriceAndDeliveryDays(t *testing.T) {
	var nilSale *FlashSale
	if nilSale.Price() != 0 {
		t.Fatal("expected 0 for a nil flash sale")
	}
	if _, _, ok := nilSale.DeliveryDays(); ok {
		t.Fatal("expected no delivery days for a nil flash sale")
	}

	usdOnly := &FlashSale{USDPrice: 0.0017}
	if usdOnly.Price() != 0.0017 {
		t.Fatalf("expected the USD price, got %v", usdOnly.Price())
	}
	if _, _, ok := usdOnly.DeliveryDays(); ok {
		t.Fatal("expected no delivery days without deliveryTimeWayDays")
	}

	oneValue := &FlashSale{DeliveryTimeWayDays: []int{5}}
	if minDays, maxDays, ok := oneValue.DeliveryDays(); !ok || minDays != 5 || maxDays != 5 {
		t.Fatalf("expected 5-5 days, got %d-%d (%v)", minDays, maxDays, ok)
	}
}

func TestFlashSaleAmount(t *testing.T) {
	tests := []struct {
		name         string
		sale         *FlashSale
		wantAmount   float64
		wantCurrency string
	}{
		{"nil offer", nil, 0, ""},
		{"sell price", &FlashSale{SellPrice: 0.0016, USDPrice: 0.0017, SellCurrencyType: "EUR", CurrencySymbol: "€"}, 0.0016, "EUR"},
		{"lower-case sell currency", &FlashSale{SellPrice: 0.012, SellCurrencyType: " cny "}, 0.012, "CNY"},
		{"sell price with symbol only", &FlashSale{SellPrice: 0.02, CurrencySymbol: "HK$"}, 0.02, "HKD"},
		{"sell price without currency", &FlashSale{SellPrice: 0.02}, 0.02, "USD"},
		// The sell currency is EUR, but the offer has no sell price. The
		// price falls back to usdPrice, which is in USD.
		{"USD fallback", &FlashSale{USDPrice: 0.0017, SellCurrencyType: "EUR", CurrencySymbol: "€"}, 0.0017, "USD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, currency := tt.sale.Amount()
			if amount != tt.wantAmount || currency != tt.wantCurrency {
				t.Fatalf("expected %v %q, got %v %q", tt.wantAmount, tt.wantCurrency, amount, currency)
			}
			if amount != tt.sale.Price() {
				t.Fatalf("expected the amount of Price %v, got %v", tt.sale.Price(), amount)
			}
		})
	}
}
