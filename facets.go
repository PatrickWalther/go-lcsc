package lcsc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// Facets holds the filter values that exist for a filter. LCSC sends them
// from /product/query/param/group. For a dimension that the filter uses,
// LCSC sends only the selected values.
type Facets struct {
	// TotalCount is the number of products that match the filter.
	TotalCount int

	// Packages holds the package names, for example "0603".
	Packages []string

	// Manufacturers holds the manufacturers with their ids.
	Manufacturers []FacetBrand

	// Packagings holds the packaging names, for example
	// "Tape & Reel (TR)".
	Packagings []string

	// Params holds the parameter facets in the server order.
	Params []ParamFacet
}

// FacetBrand is a manufacturer in [Facets].
type FacetBrand struct {
	// ID is the LCSC brand id. Use it in [Filter.BrandIDs].
	ID int

	// Name is the manufacturer name.
	Name string
}

// ParamFacet holds the values of one parameter in [Facets].
type ParamFacet struct {
	// Name is the parameter name. Use it as a key of [Filter.Params].
	Name string

	// Values holds the values in the server order.
	Values []FacetValue

	// Units holds the units that LCSC knows for the parameter, for example
	// "pF", "nF" and "uF". It is empty for a parameter without units.
	Units []string

	// UnitConversion maps a unit to its factor for the standard unit. For
	// capacitance, the standard unit is pF, so "nF" gives 1000.
	UnitConversion map[string]float64
}

// FacetValue is one value of a [ParamFacet].
type FacetValue struct {
	// Name is the value as LCSC shows it, for example "100nF". Use it as a
	// value of [Filter.Params].
	Name string `json:"name"`

	// IsRange is true when the value is a range, for example "±5%" or
	// "-55℃~+125℃".
	IsRange bool `json:"isRange"`

	// StandardUnitValue is the value in the standard unit as LCSC sends
	// it, for example "100000" for 100nF. For a range, it is the start of
	// the range. It is empty when LCSC has no number for the value.
	StandardUnitValue FlexString `json:"standardUnitValue"`

	// StandardUnitEndValue is the end of a range in the standard unit. It
	// is empty for a value that is not a range.
	StandardUnitEndValue FlexString `json:"standardUnitEndValue"`
}

// Number returns StandardUnitValue as a number. ok is false when the value
// has no number.
func (v FacetValue) Number() (value float64, ok bool) {
	return parseFacetNumber(string(v.StandardUnitValue))
}

// Range returns the start and the end of a range value in the standard
// unit. ok is false when the value is not a range or has no numbers.
func (v FacetValue) Range() (start, end float64, ok bool) {
	if !v.IsRange {
		return 0, 0, false
	}
	start, okStart := parseFacetNumber(string(v.StandardUnitValue))
	end, okEnd := parseFacetNumber(string(v.StandardUnitEndValue))
	if !okStart || !okEnd {
		return 0, 0, false
	}
	return start, end, true
}

func parseFacetNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// Param returns the facet of the parameter with the name. The comparison
// uses the exact name first and ignores case second. It returns nil when no
// facet has the name.
func (f *Facets) Param(name string) *ParamFacet {
	if f == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	for i := range f.Params {
		if f.Params[i].Name == name {
			return &f.Params[i]
		}
	}
	for i := range f.Params {
		if strings.EqualFold(f.Params[i].Name, name) {
			return &f.Params[i]
		}
	}
	return nil
}

// Equivalents returns the names of all values with the same quantity as
// the value name, in the server order. LCSC gives one quantity more than
// one name, for example "100nF" and "100000pF". A filter with only one
// name misses the products with the other names.
//
// Two names are equivalent only when both are a number followed by a unit
// of the facet (see Units), the values are not ranges, and both have the
// same StandardUnitValue. So "15mΩ@4.5V" and "15mΩ@10V" are not
// equivalent, although LCSC gives both the value 0.015.
//
// Equivalents returns only name when name is not equivalent to another
// value. It returns nil when the facet has no value with the name.
func (p *ParamFacet) Equivalents(name string) []string {
	if p == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	index := -1
	for i := range p.Values {
		if p.Values[i].Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}

	want, ok := p.quantity(&p.Values[index])
	if !ok {
		return []string{name}
	}
	var names []string
	for i := range p.Values {
		if i == index {
			names = append(names, name)
			continue
		}
		if got, ok := p.quantity(&p.Values[i]); ok && sameQuantity(got, want) {
			names = append(names, p.Values[i].Name)
		}
	}
	return names
}

// quantity returns the standard value of v when v is a single number with
// a unit of the facet.
func (p *ParamFacet) quantity(v *FacetValue) (float64, bool) {
	if v.IsRange || !p.isNumberWithUnit(v.Name) {
		return 0, false
	}
	return v.Number()
}

// isNumberWithUnit reports whether name is a number followed by a unit of
// the facet, for example "100nF".
func (p *ParamFacet) isNumberWithUnit(name string) bool {
	name = strings.TrimSpace(name)
	end := 0
	for end < len(name) {
		c := name[end]
		if (c >= '0' && c <= '9') || c == '.' || (end == 0 && (c == '-' || c == '+')) {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return false
	}
	if _, err := strconv.ParseFloat(name[:end], 64); err != nil {
		return false
	}
	unit := strings.TrimLeftFunc(name[end:], unicode.IsSpace)
	if unit == "" {
		return false
	}
	if _, ok := p.UnitConversion[unit]; ok {
		return true
	}
	for _, u := range p.Units {
		if u == unit {
			return true
		}
	}
	return false
}

func sameQuantity(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(math.Abs(a), math.Abs(b))
}

// ExpandParams returns a copy of params in which each value also has its
// equivalent names (see [ParamFacet.Equivalents]). Names and values that
// the facets do not have stay unchanged. Each value occurs only one time.
func (f *Facets) ExpandParams(params map[string][]string) map[string][]string {
	if params == nil {
		return nil
	}
	out := make(map[string][]string, len(params))
	for name, values := range params {
		facet := f.Param(name)
		seen := map[string]bool{}
		var expanded []string
		add := func(v string) {
			if !seen[v] {
				seen[v] = true
				expanded = append(expanded, v)
			}
		}
		for _, v := range values {
			add(v)
			for _, eq := range facet.Equivalents(v) {
				add(eq)
			}
		}
		out[name] = expanded
	}
	return out
}

type facetsWrapper struct {
	TotalCount   int              `json:"totalCount"`
	Package      []facetName      `json:"Package"`
	Manufacturer []facetBrandJSON `json:"Manufacturer"`
	Packaging    []facetName      `json:"Packaging"`
	Params       paramFacetList   `json:"paramNameValueMap"`
}

type facetName struct {
	Name string `json:"name"`
}

type facetBrandJSON struct {
	// LCSC sends the id as a JSON string. json.Number accepts a string and
	// a number.
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}

type paramFacetJSON struct {
	ParamDetailList   []FacetValue       `json:"paramDetailList"`
	UnitList          []string           `json:"unitList"`
	UnitConversionMap map[string]float64 `json:"unitConversionMap"`
}

// paramFacetList decodes paramNameValueMap into a list. It keeps the
// order of the JSON object keys, because LCSC sends the parameters in the
// order of the site.
type paramFacetList []ParamFacet

// UnmarshalJSON implements json.Unmarshaler for paramFacetList.
func (l *paramFacetList) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*l = nil
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("cannot unmarshal %v into a parameter facet map", tok)
	}

	var list paramFacetList
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := tok.(string)
		if !ok {
			return fmt.Errorf("unexpected parameter facet key %v", tok)
		}
		var raw paramFacetJSON
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("parameter facet %q: %w", name, err)
		}
		list = append(list, ParamFacet{
			Name:           name,
			Values:         raw.ParamDetailList,
			Units:          raw.UnitList,
			UnitConversion: raw.UnitConversionMap,
		})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*l = list
	return nil
}

// Facets returns the filter values that exist for the filter. It uses
// /product/query/param/group, which the LCSC category pages use for the
// filter lists. The filter rules of [SearchService.List] apply: a
// GlobalKeyword needs CatalogIDs, and the client returns
// [ErrInvalidRequest] without a request when the filter breaks a rule.
//
// The client caches the response for CacheConfig.SearchTTL.
func (s *SearchService) Facets(ctx context.Context, f *Filter) (*Facets, error) {
	body, err := newQueryListBody(f)
	if err != nil {
		return nil, err
	}

	client := s.client
	cacheKey, keyErr := cacheKeyForBody("facets", client.currency, body)
	useCache := keyErr == nil && client.cacheConfig.Enabled && client.cache != nil
	if useCache {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var facets Facets
			if err := json.Unmarshal(cached, &facets); err == nil {
				return &facets, nil
			}
		}
	}

	var wrapper facetsWrapper
	if err := client.do(ctx, http.MethodPost, "/product/query/param/group", nil, body, &wrapper); err != nil {
		return nil, err
	}

	facets := &Facets{
		TotalCount: wrapper.TotalCount,
		Params:     wrapper.Params,
	}
	for _, p := range wrapper.Package {
		facets.Packages = append(facets.Packages, p.Name)
	}
	for _, p := range wrapper.Packaging {
		facets.Packagings = append(facets.Packagings, p.Name)
	}
	for _, m := range wrapper.Manufacturer {
		id, _ := strconv.Atoi(strings.TrimSpace(m.ID.String()))
		facets.Manufacturers = append(facets.Manufacturers, FacetBrand{ID: id, Name: m.Name})
	}

	if useCache {
		if data, err := json.Marshal(facets); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return facets, nil
}
