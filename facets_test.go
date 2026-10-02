package lcsc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
)

const testParamGroupPath = "/ftps/wm/product/query/param/group"

func paramNames(params []ParamFacet) []string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		names = append(names, p.Name)
	}
	return names
}

func valueNames(values []FacetValue) []string {
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, v.Name)
	}
	return names
}

func TestSearchFacetsDecodesFixture(t *testing.T) {
	// Trimmed live response for Capacitance 100nF or 100000pF in leaf
	// 1142.
	fixture := mustReadFixture(t, "paramgroup_1142_100nF_100000pF.json")

	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != testParamGroupPath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		body := decodeJSONBody(t, req)
		want := map[string]interface{}{
			"keyword":           "",
			"catalogIdList":     []interface{}{float64(1142)},
			"brandIdList":       []interface{}{},
			"encapValueList":    []interface{}{},
			"isStock":           false,
			"isOtherSuppliers":  false,
			"isAsianBrand":      false,
			"isDeals":           false,
			"isRohsCert":        false,
			"paramNameValueMap": map[string]interface{}{"Capacitance": []interface{}{"100nF", "100000pF"}},
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("unexpected body:\n got %v\nwant %v", body, want)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	facets, err := client.Search.Facets(context.Background(), &Filter{
		CatalogIDs: []int{1142},
		Params:     map[string][]string{"Capacitance": {"100nF", "100000pF"}},
	})
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}

	if facets.TotalCount != 9193 {
		t.Fatalf("expected total count 9193, got %d", facets.TotalCount)
	}
	if want := []string{"008004", "01005", "0201", "0204"}; !reflect.DeepEqual(facets.Packages, want) {
		t.Fatalf("expected packages %v, got %v", want, facets.Packages)
	}
	wantBrands := []FacetBrand{{ID: 18510, Name: "AIDE CAPACITOR"}, {ID: 11767, Name: "AMOTECH"}, {ID: 11714, Name: "CCTC"}}
	if !reflect.DeepEqual(facets.Manufacturers, wantBrands) {
		t.Fatalf("expected manufacturers %v, got %v", wantBrands, facets.Manufacturers)
	}
	if want := []string{"Bag-packed", "Box-packed", "Tape & Reel (TR)"}; !reflect.DeepEqual(facets.Packagings, want) {
		t.Fatalf("expected packagings %v, got %v", want, facets.Packagings)
	}
	// The order of the JSON object, not the alphabetic order.
	if got, want := paramNames(facets.Params), []string{"Capacitance", "Tolerance", "Voltage Rating", "Temperature Coefficient"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected parameters %v, got %v", want, got)
	}

	capacitance := facets.Param("Capacitance")
	if capacitance == nil {
		t.Fatal("expected the Capacitance facet")
	}
	if got, want := valueNames(capacitance.Values), []string{"100nF", "100000pF"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected values %v, got %v", want, got)
	}
	if v, ok := capacitance.Values[0].Number(); !ok || v != 100000 {
		t.Fatalf("expected standard value 100000, got %v %v", v, ok)
	}
	if capacitance.UnitConversion["nF"] != 1000 || capacitance.UnitConversion["pF"] != 1 {
		t.Fatalf("unexpected unit conversion %v", capacitance.UnitConversion)
	}
	if want := []string{"pF", "nF", "uF", "mF", "F"}; !reflect.DeepEqual(capacitance.Units, want) {
		t.Fatalf("expected units %v, got %v", want, capacitance.Units)
	}

	tolerance := facets.Param("tolerance")
	if tolerance == nil {
		t.Fatal("expected a case-insensitive match for tolerance")
	}
	if start, end, ok := tolerance.Values[0].Range(); !ok || start != -20 || end != 20 {
		t.Fatalf("expected range -20 to 20 for %q, got %v %v %v", tolerance.Values[0].Name, start, end, ok)
	}

	tc := facets.Param("Temperature Coefficient")
	if tc.Values[0].Name != "C0G" {
		t.Fatalf("unexpected first value %q", tc.Values[0].Name)
	}
	if _, ok := tc.Values[0].Number(); ok {
		t.Fatal("expected no number for C0G")
	}
	if _, _, ok := tc.Values[0].Range(); ok {
		t.Fatal("expected no range for C0G")
	}
	if facets.Param("Unknown") != nil {
		t.Fatal("expected no facet for an unknown name")
	}
}

func TestSearchFacetsWithGlobalKeyword(t *testing.T) {
	// Trimmed live response for "10k 0603" in leaf 1199 with stock only.
	fixture := mustReadFixture(t, "paramgroup_1199_10k_0603.json")

	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		body := decodeJSONBody(t, req)
		if body["globalKeyword"] != "10k 0603" || body["scene"] != "FULL_MATCH" || body["isStock"] != true {
			t.Errorf("unexpected body %v", body)
		}
		for _, key := range []string{"sortField", "sortType", "currentPage", "pageSize"} {
			if _, ok := body[key]; ok {
				t.Errorf("expected no %s in the facets body", key)
			}
		}
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	facets, err := client.Search.Facets(context.Background(), &Filter{
		GlobalKeyword: "10k 0603",
		CatalogIDs:    []int{1199},
		InStock:       true,
	})
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}
	if facets.TotalCount != 180 || !reflect.DeepEqual(facets.Packages, []string{"0603"}) {
		t.Fatalf("unexpected facets: total %d, packages %v", facets.TotalCount, facets.Packages)
	}
	want := []string{"Type", "Resistance", "Tolerance", "Operating Temperature", "Voltage Rating", "Power(Watts)", "Temperature Coefficient"}
	if got := paramNames(facets.Params); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected parameters %v, got %v", want, got)
	}
	resistance := facets.Param("Resistance")
	if got := valueNames(resistance.Values); !reflect.DeepEqual(got, []string{"10kΩ"}) {
		t.Fatalf("expected only the selected value 10kΩ, got %v", got)
	}
}

func TestSearchFacetsValidation(t *testing.T) {
	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":{"totalCount":0},"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if _, err := client.Search.Facets(ctx, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a nil filter, got %v", err)
	}
	// LCSC answers this filter with code 405 "Invalid field. Please check
	// again."
	if _, err := client.Search.Facets(ctx, &Filter{GlobalKeyword: "10k 0603"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a global keyword without category, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no request, got %d", got)
	}
}

func TestSearchFacetsIsCached(t *testing.T) {
	fixture := mustReadFixture(t, "paramgroup_1142_100nF_100000pF.json")

	var calls int32
	client := newCachedTestClient(newRecordingCache(), func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, fixture), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	filter := &Filter{CatalogIDs: []int{1142}}
	first, err := client.Search.Facets(ctx, filter)
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}
	second, err := client.Search.Facets(ctx, filter)
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected the cached facets to be equal:\n first %+v\nsecond %+v", first, second)
	}
}

// testFacet returns a facet with values from live LCSC responses.
func testFacet(t *testing.T, raw string) ParamFacet {
	t.Helper()
	var list paramFacetList
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("failed to decode facet: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one facet, got %d", len(list))
	}
	return list[0]
}

func TestParamFacetEquivalents(t *testing.T) {
	capacitance := testFacet(t, `{"Capacitance": {
		"paramDetailList": [
			{"name": "10nF", "isRange": false, "standardUnitValue": "10000"},
			{"name": "100nF", "isRange": false, "standardUnitValue": "100000"},
			{"name": "100000pF", "isRange": false, "standardUnitValue": "100000"},
			{"name": "0.1uF", "isRange": false, "standardUnitValue": "100000"},
			{"name": "100nF±10%", "isRange": false, "standardUnitValue": "100000"}
		],
		"unitList": ["pF", "nF", "uF"],
		"unitConversionMap": {"pF": 1, "nF": 1000, "uF": 1000000}
	}}`)
	rdson := testFacet(t, `{"RDS(on)": {
		"paramDetailList": [
			{"name": "11mΩ", "isRange": false, "standardUnitValue": "0.011"},
			{"name": "11mΩ@10V", "isRange": false, "standardUnitValue": "0.011"},
			{"name": "15mΩ@4.5V", "isRange": false, "standardUnitValue": "0.015"},
			{"name": "15mΩ@10V", "isRange": false, "standardUnitValue": "0.015"}
		],
		"unitList": ["Ω", "mΩ", "kΩ", "MΩ"],
		"unitConversionMap": {"Ω": 1, "mΩ": 0.001, "kΩ": 1000, "MΩ": 1000000}
	}}`)
	number := testFacet(t, `{"Number": {
		"paramDetailList": [
			{"name": "1 N-channel", "isRange": false, "standardUnitValue": "1"},
			{"name": "1 P-Channel", "isRange": false, "standardUnitValue": "1"}
		],
		"unitList": [],
		"unitConversionMap": {}
	}}`)
	temperature := testFacet(t, `{"Operating Temperature": {
		"paramDetailList": [
			{"name": "-55℃~+150℃", "isRange": true, "standardUnitValue": "-55", "standardUnitEndValue": "150"},
			{"name": "-55℃~+125℃", "isRange": true, "standardUnitValue": "-55", "standardUnitEndValue": "125"}
		],
		"unitList": ["℃"],
		"unitConversionMap": {"℃": 1}
	}}`)

	tests := []struct {
		name  string
		facet *ParamFacet
		value string
		want  []string
	}{
		{"same capacitance", &capacitance, "100nF", []string{"100nF", "100000pF", "0.1uF"}},
		{"other name first", &capacitance, "100000pF", []string{"100nF", "100000pF", "0.1uF"}},
		{"no other name", &capacitance, "10nF", []string{"10nF"}},
		{"value with a condition", &capacitance, "100nF±10%", []string{"100nF±10%"}},
		{"unit and condition", &rdson, "11mΩ", []string{"11mΩ"}},
		{"conditions differ", &rdson, "15mΩ@4.5V", []string{"15mΩ@4.5V"}},
		{"text after the number", &number, "1 N-channel", []string{"1 N-channel"}},
		{"ranges", &temperature, "-55℃~+150℃", []string{"-55℃~+150℃"}},
		{"unknown value", &capacitance, "220nF", nil},
		{"nil facet", nil, "100nF", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.facet.Equivalents(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Equivalents(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestFacetsExpandParams(t *testing.T) {
	facets := &Facets{Params: []ParamFacet{testFacet(t, `{"Capacitance": {
		"paramDetailList": [
			{"name": "100nF", "isRange": false, "standardUnitValue": "100000"},
			{"name": "100000pF", "isRange": false, "standardUnitValue": "100000"},
			{"name": "1uF", "isRange": false, "standardUnitValue": "1000000"}
		],
		"unitList": ["pF", "nF", "uF"],
		"unitConversionMap": {"pF": 1, "nF": 1000, "uF": 1000000}
	}}`)}}

	got := facets.ExpandParams(map[string][]string{
		"Capacitance":    {"100000pF", "1uF", "100nF"},
		"Voltage Rating": {"16V"},
	})
	want := map[string][]string{
		"Capacitance":    {"100000pF", "100nF", "1uF"},
		"Voltage Rating": {"16V"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandParams() = %v, want %v", got, want)
	}

	if facets.ExpandParams(nil) != nil {
		t.Fatal("expected nil for nil params")
	}
	var nilFacets *Facets
	if got := nilFacets.ExpandParams(map[string][]string{"A": {"1"}}); !reflect.DeepEqual(got, map[string][]string{"A": {"1"}}) {
		t.Fatalf("expected unchanged params for nil facets, got %v", got)
	}
}

func TestParamFacetListDecode(t *testing.T) {
	var list paramFacetList
	if err := json.Unmarshal([]byte(`null`), &list); err != nil || list != nil {
		t.Fatalf("expected nil list for null, got %v %v", list, err)
	}
	if err := json.Unmarshal([]byte(`{}`), &list); err != nil || len(list) != 0 {
		t.Fatalf("expected empty list for {}, got %v %v", list, err)
	}
	if err := json.Unmarshal([]byte(`{"Z": {"paramDetailList": []}, "A": {"paramDetailList": [{"name": "1", "standardUnitValue": 1}]}}`), &list); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got := paramNames(list); !reflect.DeepEqual(got, []string{"Z", "A"}) {
		t.Fatalf("expected the JSON key order, got %v", got)
	}
	if v, ok := list[1].Values[0].Number(); !ok || v != 1 {
		t.Fatalf("expected a numeric standard value to decode, got %v %v", v, ok)
	}
	if err := json.Unmarshal([]byte(`[]`), &list); err == nil {
		t.Fatal("expected an error for a JSON array")
	}
}
