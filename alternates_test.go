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

func newAlternatesTestClient(t *testing.T, fn roundTripFunc, opts ...ClientOption) *Client {
	t.Helper()
	base := []ClientOption{
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(fn)),
		WithoutRetry(),
		WithoutCache(),
	}
	client := NewClient(append(base, opts...)...)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestAlternatesListDecodesFixture(t *testing.T) {
	// Trimmed live response for C1525 (all alternates, page size 100). The
	// server sent 99 alternates. The fixture keeps 6 of them.
	fixture := mustReadFixture(t, "alternates_C1525.json")
	var calls int32
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", req.Method)
		}
		if req.URL.Path != "/ftps/wm/product/alternate/part/list" {
			t.Errorf("unexpected path: %s", req.URL.Path)
		}
		q := req.URL.Query()
		want := map[string]string{
			"productCode": "C1525",
			"inStockOnly": "false",
			"currentPage": "1",
			"pageSize":    "100",
		}
		for key, value := range want {
			if got := q.Get(key); got != value {
				t.Errorf("query %s: got %q, want %q", key, got, value)
			}
		}
		if len(q) != len(want) {
			t.Errorf("unexpected query parameters: %v", q)
		}
		if got := req.Header.Get("Cookie"); got != "currencyCode=USD" {
			t.Errorf("unexpected cookie %q", got)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})

	// LCSC finds no product for a lower-case code, so the client changes
	// the code to upper case.
	resp, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: " c1525 "})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}

	original := resp.Original
	if original.ProductCode != "C1525" || original.ProductID != 1877 || original.ProductModel != "CL05B104KO5NNNC" {
		t.Fatalf("unexpected original: %s %d %s", original.ProductCode, original.ProductID, original.ProductModel)
	}
	if original.MatchType != "" || original.Match() != "" || original.Match().Label() != "" {
		t.Fatalf("expected no match type on the original, got %q", original.MatchType)
	}
	if len(original.ParamVOList) != 4 {
		t.Fatalf("expected 4 original parameters, got %d", len(original.ParamVOList))
	}
	if resp.InStockCount != 52 || resp.TotalCount != 99 {
		t.Fatalf("unexpected counts: in stock %d, total %d", resp.InStockCount, resp.TotalCount)
	}

	wantAlternates := []struct {
		code      string
		productID int64
		matchType MatchType
		label     string
		dropIn    bool
		stock     int
	}{
		{"C106994", 108210, "5", "Direct", true, 20600},
		{"C71629", 72737, "5", "Direct", true, 997500},
		{"C2181015", 2273082, "6", "Upgrade", false, 0},
		{"C913742", 984061, "6", "Upgrade", false, 5300},
		{"C105883", 107098, "4", "Similar", false, 2010400},
		{"C126527", 137807, "4", "Similar", false, 56000},
	}
	if len(resp.Alternates) != len(wantAlternates) {
		t.Fatalf("expected %d alternates, got %d", len(wantAlternates), len(resp.Alternates))
	}
	for i, want := range wantAlternates {
		alt := &resp.Alternates[i]
		if alt.ProductCode != want.code || alt.ProductID != want.productID {
			t.Errorf("alternate %d: got %s (%d), want %s (%d)", i, alt.ProductCode, alt.ProductID, want.code, want.productID)
		}
		if alt.Match() != want.matchType || alt.Match().Label() != want.label || alt.Match().IsDropIn() != want.dropIn {
			t.Errorf("alternate %d: got match %q label %q drop-in %v, want %q %q %v",
				i, alt.Match(), alt.Match().Label(), alt.Match().IsDropIn(), want.matchType, want.label, want.dropIn)
		}
		if alt.StockNumber != want.stock {
			t.Errorf("alternate %d: got stock %d, want %d", i, alt.StockNumber, want.stock)
		}
		if alt.EncapStandard != "0402" || alt.Lifecycle() != LifecycleActive || alt.Currency() != "USD" {
			t.Errorf("alternate %d: unexpected package %q, lifecycle %q or currency %q", i, alt.EncapStandard, alt.Lifecycle(), alt.Currency())
		}
		if len(alt.ProductPriceList) == 0 || alt.ProductPriceList[0].Price() <= 0 {
			t.Errorf("alternate %d: expected prices", i)
		}
		if len(alt.ParamVOList) != 4 {
			t.Errorf("alternate %d: expected 4 parameters, got %d", i, len(alt.ParamVOList))
		}
	}
}

func TestAlternatesListSendsOptions(t *testing.T) {
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if q.Get("productCode") != "C6186" || q.Get("inStockOnly") != "true" || q.Get("currentPage") != "2" || q.Get("pageSize") != "50" {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		if q.Get("sortField") != "" || q.Get("sortType") != "" {
			t.Errorf("expected no sort parameters, got %s", req.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{
			"rawMaterial":{"productId":6186,"productCode":"C6186"},
			"inStockCount":86,
			"pageInfo":{"currPage":2,"pageRow":50,"totalPage":2,"totalRow":86,"dataList":[],"actualTotalRow":86}
		},"ok":true}`), nil
	})

	resp, err := client.Alternates.List(context.Background(), &AlternatesRequest{
		ProductCode: "C6186",
		InStockOnly: true,
		Page:        2,
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if resp.InStockCount != 86 || resp.TotalCount != 86 {
		t.Fatalf("unexpected counts: in stock %d, total %d", resp.InStockCount, resp.TotalCount)
	}
}

func TestAlternatesListValidation(t *testing.T) {
	var calls int32
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":200,"result":null}`), nil
	})

	tests := []struct {
		name string
		req  *AlternatesRequest
	}{
		{"nil request", nil},
		{"empty code", &AlternatesRequest{ProductCode: "  "}},
		{"page size above 100", &AlternatesRequest{ProductCode: "C1525", PageSize: 101}},
		{"negative page size", &AlternatesRequest{ProductCode: "C1525", PageSize: -1}},
		{"negative page", &AlternatesRequest{ProductCode: "C1525", Page: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Alternates.List(context.Background(), tt.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no request for invalid input, got %d", got)
	}
}

func TestAlternatesListAcceptsMaxPageSize(t *testing.T) {
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("pageSize"); got != "100" {
			t.Errorf("expected page size 100, got %q", got)
		}
		return jsonResponse(http.StatusOK, `{"code":200,"result":{"rawMaterial":{"productCode":"C1525"},"inStockCount":0,"pageInfo":{"totalRow":0,"dataList":[]}}}`), nil
	})

	if _, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C1525", PageSize: 100}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
}

func TestAlternatesListTotalCount(t *testing.T) {
	tests := []struct {
		name     string
		pageInfo string
		want     int
	}{
		{"actualTotalRow present", `{"totalRow":99,"actualTotalRow":99,"dataList":[]}`, 99},
		{"actualTotalRow differs", `{"totalRow":50,"actualTotalRow":120,"dataList":[]}`, 120},
		{"actualTotalRow missing", `{"totalRow":13,"dataList":[]}`, 13},
		{"actualTotalRow null", `{"totalRow":13,"actualTotalRow":null,"dataList":[]}`, 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"code":200,"msg":null,"result":{"rawMaterial":{"productCode":"C25804"},"inStockCount":5,"pageInfo":` + tt.pageInfo + `},"ok":true}`
			client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			})
			resp, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C25804"})
			if err != nil {
				t.Fatalf("list failed: %v", err)
			}
			if resp.TotalCount != tt.want {
				t.Fatalf("expected total count %d, got %d", tt.want, resp.TotalCount)
			}
		})
	}
}

func TestAlternatesListUnknownCodeReturnsNotFound(t *testing.T) {
	// Live response for an unknown code (also for a lower-case code).
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"rawMaterial":null,"inStockCount":0,"pageInfo":{"currPage":1,"pageRow":5,"totalPage":1,"totalRow":0,"dataList":[],"actualTotalRow":0}},"ok":true}`), nil
	})

	_, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C999999999"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAlternatesListProductWithoutAlternates(t *testing.T) {
	// Live response shape for C2040, which has no cross-reference
	// alternates.
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"rawMaterial":{"productId":2392,"productCode":"C2040","productModel":"RP2040"},"inStockCount":0,"pageInfo":{"currPage":1,"pageRow":25,"totalPage":1,"totalRow":0,"dataList":[],"actualTotalRow":0}},"ok":true}`), nil
	})

	resp, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C2040", PageSize: 25})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if resp.Original.ProductID != 2392 || len(resp.Alternates) != 0 || resp.TotalCount != 0 {
		t.Fatalf("unexpected response: original %d, %d alternates, total %d", resp.Original.ProductID, len(resp.Alternates), resp.TotalCount)
	}
}

func TestAlternatesListEnvelope405IsNotRetried(t *testing.T) {
	var calls int32
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":405,"msg":"Product search error.","result":null,"ok":false}`), nil
	}, WithRetryConfig(RetryConfig{MaxRetries: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, Multiplier: 1}))

	_, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C1525"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Product search error." {
		t.Fatalf("expected the LCSC message in the API error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

func TestAlternatesListCaching(t *testing.T) {
	fixture := mustReadFixture(t, "alternates_C1525.json")
	var calls int32
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, fixture), nil
	}, WithCache(NewMemoryCache(time.Minute)), WithCacheConfig(CacheConfig{
		Enabled:    true,
		SearchTTL:  time.Minute,
		DetailsTTL: time.Minute,
	}))

	ctx := context.Background()
	first, err := client.Alternates.List(ctx, &AlternatesRequest{ProductCode: "C1525"})
	if err != nil {
		t.Fatalf("first list failed: %v", err)
	}
	second, err := client.Alternates.List(ctx, &AlternatesRequest{ProductCode: "c1525", PageSize: 100, Page: 1})
	if err != nil {
		t.Fatalf("second list failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 request with a cache hit, got %d", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached response differs from the decoded response")
	}
	if second.Alternates[0].Match() != MatchTypeDirect {
		t.Fatalf("expected the cache to keep the match type, got %q", second.Alternates[0].MatchType)
	}

	if _, err := client.Alternates.List(ctx, &AlternatesRequest{ProductCode: "C1525", InStockOnly: true}); err != nil {
		t.Fatalf("third list failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected a new request for other options, got %d requests", got)
	}
}

func TestCacheKeyForAlternates(t *testing.T) {
	base := cacheKeyForAlternates("USD", "C1525", false, 1, 100)
	others := []string{
		cacheKeyForAlternates("EUR", "C1525", false, 1, 100),
		cacheKeyForAlternates("USD", "C1526", false, 1, 100),
		cacheKeyForAlternates("USD", "C1525", true, 1, 100),
		cacheKeyForAlternates("USD", "C1525", false, 2, 100),
		cacheKeyForAlternates("USD", "C1525", false, 1, 50),
	}
	for i, other := range others {
		if other == base {
			t.Fatalf("key %d: expected a different key, got %q", i, other)
		}
	}
	if got := cacheKeyForAlternates("usd", "C1525", false, 1, 100); got != base {
		t.Fatalf("expected the currency case to have no effect, got %q and %q", got, base)
	}
}

func TestMatchTypeLabelAndIsDropIn(t *testing.T) {
	tests := []struct {
		matchType MatchType
		label     string
		dropIn    bool
	}{
		{"2", "Alt. Packaging", true},
		{"5", "Direct", true},
		{"6", "Upgrade", false},
		{"1", "Similar", false},
		{"3", "Similar", false},
		{"4", "Similar", false},
		{"7", "Similar", false},
		{" 5 ", "Direct", true},
		{"", "", false},
		{"  ", "", false},
	}
	for _, tt := range tests {
		if got := tt.matchType.Label(); got != tt.label {
			t.Errorf("%q: got label %q, want %q", tt.matchType, got, tt.label)
		}
		if got := tt.matchType.IsDropIn(); got != tt.dropIn {
			t.Errorf("%q: got drop-in %v, want %v", tt.matchType, got, tt.dropIn)
		}
	}
	if MatchTypeAltPackaging != "2" || MatchTypeDirect != "5" || MatchTypeUpgrade != "6" {
		t.Fatal("unexpected match type constants")
	}
}

func TestProductMatch(t *testing.T) {
	var nilProduct *Product
	if got := nilProduct.Match(); got != "" {
		t.Fatalf("expected an empty match type for a nil product, got %q", got)
	}
	p := &Product{MatchType: " 6"}
	if got := p.Match(); got != MatchTypeUpgrade {
		t.Fatalf("expected %q, got %q", MatchTypeUpgrade, got)
	}
}

func searchValue(v float64) *float64 {
	return &v
}

func TestDiffParametersFixture(t *testing.T) {
	fixture := mustReadFixture(t, "alternates_C1525.json")
	client := newAlternatesTestClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fixture), nil
	})
	resp, err := client.Alternates.List(context.Background(), &AlternatesRequest{ProductCode: "C1525"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	byCode := map[string]*Product{}
	for i := range resp.Alternates {
		byCode[resp.Alternates[i].ProductCode] = &resp.Alternates[i]
	}

	tests := []struct {
		code string
		want []ParameterDiff
	}{
		{"C106994", nil},
		{"C71629", nil},
		{"C2181015", []ParameterDiff{{
			Kind: ParameterChanged, Name: "Voltage Rating", ParamCode: "param_10924_n",
			OriginalValue: "16V", AlternateValue: "50V", IsMain: true,
		}}},
		{"C913742", []ParameterDiff{{
			Kind: ParameterChanged, Name: "Tolerance", ParamCode: "param_10923_s",
			OriginalValue: "±10%", AlternateValue: "±5%", IsMain: true,
		}}},
		{"C105883", []ParameterDiff{{
			Kind: ParameterChanged, Name: "Voltage Rating", ParamCode: "param_10924_n",
			OriginalValue: "16V", AlternateValue: "25V", IsMain: true,
		}}},
	}
	for _, tt := range tests {
		alt := byCode[tt.code]
		if alt == nil {
			t.Fatalf("fixture has no alternate %s", tt.code)
		}
		if got := DiffParameters(&resp.Original, alt); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v, want %+v", tt.code, got, tt.want)
		}
	}
}

func TestDiffParametersMatchesOnNameWhenCodesDiffer(t *testing.T) {
	// Parameters from the live C7593 (NE555DR) alternates list. The
	// alternate C42436866 is in another category and uses other codes for
	// some parameters.
	original := &Product{ParamVOList: []Parameter{
		{ParamCode: "param_32640_n", ParamNameEn: "Supply Current", ParamValueEn: "2mA", ParamValueEnForSearch: searchValue(0.002)},
		{ParamCode: "param_32641_s", ParamNameEn: "Operating Temperature", ParamValueEn: "0℃~+70℃"},
		{ParamCode: "param_33225", ParamNameEn: "Features", ParamValueEn: "Adjustable duty cycle oscillation;Reset function"},
		{ParamCode: "param_32639_n", ParamNameEn: "Output Current", ParamValueEn: "200mA", ParamValueEnForSearch: searchValue(0.2)},
		{ParamCode: "param_32637_n", ParamNameEn: "Timer Number", ParamValueEn: "1", ParamValueEnForSearch: searchValue(1)},
		{ParamCode: "param_32638_s", ParamNameEn: "Voltage - Supply", ParamValueEn: "4.5V~16V"},
	}}
	alt := &Product{ParamVOList: []Parameter{
		{ParamCode: "param_14698_s", ParamNameEn: "Voltage - Supply", ParamValueEn: "4.5V~16V", IsMain: true},
		{ParamCode: "param_33223", ParamNameEn: "Features", ParamValueEn: "Shutdown control", ParamValueEnForSearch: searchValue(-1)},
		{ParamCode: "param_32639_n", ParamNameEn: "Output Current", ParamValueEn: "200mA", ParamValueEnForSearch: searchValue(0.2)},
		{ParamCode: "param_32637_n", ParamNameEn: "Timer Number", ParamValueEn: "1", ParamValueEnForSearch: searchValue(1)},
		{ParamCode: "param_13246_n", ParamNameEn: "Current - Supply", ParamValueEn: "3mA", ParamValueEnForSearch: searchValue(0.003)},
		{ParamCode: "param_14700_s", ParamNameEn: "Operating Temperature", ParamValueEn: "0℃~+70℃"},
	}}

	want := []ParameterDiff{
		{Kind: ParameterMissing, Name: "Supply Current", ParamCode: "param_32640_n", OriginalValue: "2mA"},
		{Kind: ParameterChanged, Name: "Features", ParamCode: "param_33225",
			OriginalValue: "Adjustable duty cycle oscillation;Reset function", AlternateValue: "Shutdown control"},
		{Kind: ParameterAdded, Name: "Current - Supply", ParamCode: "param_13246_n", AlternateValue: "3mA"},
	}
	if got := DiffParameters(original, alt); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestDiffParametersRules(t *testing.T) {
	tests := []struct {
		name     string
		original []Parameter
		alt      []Parameter
		want     []ParameterDiff
	}{
		{
			name:     "equal numeric search values",
			original: []Parameter{{ParamCode: "param_10951_n", ParamNameEn: "Capacitance", ParamValueEn: "100nF", ParamValueEnForSearch: searchValue(100000)}},
			alt:      []Parameter{{ParamCode: "param_10951_n", ParamNameEn: "Capacitance", ParamValueEn: "0.1µF", ParamValueEnForSearch: searchValue(100000)}},
		},
		{
			name:     "spaces in the value",
			original: []Parameter{{ParamNameEn: "Voltage - Supply", ParamValueEn: "4.5V~16V"}},
			alt:      []Parameter{{ParamNameEn: "Voltage - Supply", ParamValueEn: " 4.5V ~ 16V "}},
		},
		{
			name:     "case in the value",
			original: []Parameter{{ParamCode: "p1", ParamNameEn: "Resistance", ParamValueEn: "1mΩ"}},
			alt:      []Parameter{{ParamCode: "p1", ParamNameEn: "Resistance", ParamValueEn: "1MΩ"}},
			want: []ParameterDiff{{Kind: ParameterChanged, Name: "Resistance", ParamCode: "p1",
				OriginalValue: "1mΩ", AlternateValue: "1MΩ"}},
		},
		{
			name:     "-1 is not a numeric value",
			original: []Parameter{{ParamCode: "p1", ParamNameEn: "Type", ParamValueEn: "Thick Film Resistor", ParamValueEnForSearch: searchValue(-1)}},
			alt:      []Parameter{{ParamCode: "p1", ParamNameEn: "Type", ParamValueEn: "Thin Film Resistor", ParamValueEnForSearch: searchValue(-1)}},
			want: []ParameterDiff{{Kind: ParameterChanged, Name: "Type", ParamCode: "p1",
				OriginalValue: "Thick Film Resistor", AlternateValue: "Thin Film Resistor"}},
		},
		{
			name:     "dash value in the alternate",
			original: []Parameter{{ParamCode: "param_11202", ParamNameEn: "Type", ParamValueEn: "Thick Film Resistor", IsMain: true}},
			alt:      []Parameter{{ParamCode: "param_11202", ParamNameEn: "Type", ParamValueEn: "-"}},
			want: []ParameterDiff{{Kind: ParameterMissing, Name: "Type", ParamCode: "param_11202",
				OriginalValue: "Thick Film Resistor", AlternateValue: "-", IsMain: true}},
		},
		{
			name:     "dash value in the original",
			original: []Parameter{{ParamCode: "p1", ParamNameEn: "Output Current", ParamValueEn: "-"}},
			alt:      []Parameter{{ParamCode: "p1", ParamNameEn: "Output Current", ParamValueEn: "200mA", IsMain: true}},
			want: []ParameterDiff{{Kind: ParameterAdded, Name: "Output Current", ParamCode: "p1",
				OriginalValue: "-", AlternateValue: "200mA", IsMain: true}},
		},
		{
			name:     "dash value in both",
			original: []Parameter{{ParamCode: "p1", ParamNameEn: "Output Current", ParamValueEn: "-"}},
			alt:      []Parameter{{ParamCode: "p1", ParamNameEn: "Output Current", ParamValueEn: " - "}},
		},
		{
			name:     "unmatched parameters without a value",
			original: []Parameter{{ParamCode: "p1", ParamNameEn: "Features", ParamValueEn: ""}},
			alt:      []Parameter{{ParamCode: "p2", ParamNameEn: "Noise", ParamValueEn: "-"}},
		},
		{
			name: "code match before name match",
			original: []Parameter{
				{ParamCode: "p1", ParamNameEn: "Output Current", ParamValueEn: "1A"},
			},
			alt: []Parameter{
				{ParamCode: "p9", ParamNameEn: "Output Current", ParamValueEn: "2A"},
				{ParamCode: "P1", ParamNameEn: "Current - Output", ParamValueEn: "1A"},
			},
			want: []ParameterDiff{{Kind: ParameterAdded, Name: "Output Current", ParamCode: "p9", AlternateValue: "2A"}},
		},
		{
			name: "name comparison ignores case and punctuation",
			original: []Parameter{
				{ParamCode: "p1", ParamNameEn: "Power(Watts)", ParamValueEn: "62.5mW", ParamValueEnForSearch: searchValue(0.0625)},
			},
			alt: []Parameter{
				{ParamCode: "p2", ParamNameEn: "power (watts)", ParamValueEn: "100mW", ParamValueEnForSearch: searchValue(0.1)},
			},
			want: []ParameterDiff{{Kind: ParameterChanged, Name: "Power(Watts)", ParamCode: "p1",
				OriginalValue: "62.5mW", AlternateValue: "100mW"}},
		},
		{
			name: "each alternate parameter matches one time",
			original: []Parameter{
				{ParamNameEn: "Features", ParamValueEn: "A"},
				{ParamNameEn: "Features", ParamValueEn: "B"},
			},
			alt: []Parameter{
				{ParamNameEn: "Features", ParamValueEn: "A"},
			},
			want: []ParameterDiff{{Kind: ParameterMissing, Name: "Features", OriginalValue: "B"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffParameters(&Product{ParamVOList: tt.original}, &Product{ParamVOList: tt.alt})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestDiffParametersNilProducts(t *testing.T) {
	if got := DiffParameters(nil, nil); got != nil {
		t.Fatalf("expected no differences, got %+v", got)
	}

	p := &Product{ParamVOList: []Parameter{{ParamCode: "p1", ParamNameEn: "Capacitance", ParamValueEn: "100nF", IsMain: true}}}
	wantAdded := []ParameterDiff{{Kind: ParameterAdded, Name: "Capacitance", ParamCode: "p1", AlternateValue: "100nF", IsMain: true}}
	if got := DiffParameters(nil, p); !reflect.DeepEqual(got, wantAdded) {
		t.Fatalf("got %+v, want %+v", got, wantAdded)
	}
	wantMissing := []ParameterDiff{{Kind: ParameterMissing, Name: "Capacitance", ParamCode: "p1", OriginalValue: "100nF", IsMain: true}}
	if got := DiffParameters(p, nil); !reflect.DeepEqual(got, wantMissing) {
		t.Fatalf("got %+v, want %+v", got, wantMissing)
	}
	if got := DiffParameters(p, p); got != nil {
		t.Fatalf("expected no differences for the same product, got %+v", got)
	}
}
