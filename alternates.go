package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AlternateService handles cross-reference alternate operations.
type AlternateService service

const (
	// defaultAlternatesPageSize is the page size that the client sends when
	// the request has no page size.
	defaultAlternatesPageSize = 100

	// maxAlternatesPageSize is the largest page size that LCSC accepts.
	// LCSC answers a larger page size with envelope code 405 "Product
	// search error."
	maxAlternatesPageSize = 100
)

// MatchType is the code that LCSC gives to a cross-reference alternate. It
// tells how the alternate relates to the original product. LCSC does not
// document the codes. The labels come from the LCSC web client:
//
//   - "2": Alt. Packaging
//   - "5": Direct
//   - "6": Upgrade
//   - any other code: Similar
//
// The codes "1", "3", "4", "5" and "6" occur in live responses.
type MatchType string

const (
	// MatchTypeAltPackaging is the code for the same part in a different
	// packaging.
	MatchTypeAltPackaging MatchType = "2"

	// MatchTypeDirect is the code for a direct replacement.
	MatchTypeDirect MatchType = "5"

	// MatchTypeUpgrade is the code for a replacement with better
	// parameters, for example a higher voltage rating or a smaller
	// tolerance.
	MatchTypeUpgrade MatchType = "6"
)

// Label returns the label that the LCSC site shows for the code:
// "Alt. Packaging", "Direct", "Upgrade" or "Similar". It returns an empty
// string when m is empty, because the product is then not an alternate.
func (m MatchType) Label() string {
	switch MatchType(strings.TrimSpace(string(m))) {
	case "":
		return ""
	case MatchTypeAltPackaging:
		return "Alt. Packaging"
	case MatchTypeDirect:
		return "Direct"
	case MatchTypeUpgrade:
		return "Upgrade"
	default:
		return "Similar"
	}
}

// IsDropIn reports whether the alternate can replace the original product
// with no change to the design. It is true for [MatchTypeAltPackaging] and
// [MatchTypeDirect]. The rule comes from the labels (inferred). Use
// [DiffParameters] to compare the parameters.
func (m MatchType) IsDropIn() bool {
	switch MatchType(strings.TrimSpace(string(m))) {
	case MatchTypeAltPackaging, MatchTypeDirect:
		return true
	default:
		return false
	}
}

// Match returns MatchType as a [MatchType], so that the caller can use
// [MatchType.Label] and [MatchType.IsDropIn].
func (p *Product) Match() MatchType {
	if p == nil {
		return ""
	}
	return MatchType(strings.TrimSpace(string(p.MatchType)))
}

// AlternatesRequest contains the parameters for [AlternateService.List].
type AlternatesRequest struct {
	// ProductCode is the LCSC product code of the original product, for
	// example "C1525". The client changes it to upper case, because LCSC
	// finds no product for a lower-case code.
	ProductCode string

	// InStockOnly limits the list to alternates with LCSC retail stock.
	// LCSC does not count JLCPCB stock. An alternate with LCSC stock 0 can
	// have JLCPCB stock.
	InStockOnly bool

	// Page is the page number. The first page is 1. The client sends 1
	// when Page is 0.
	Page int

	// PageSize is the number of alternates on one page, from 1 to 100. The
	// client sends 100 when PageSize is 0. The client returns
	// [ErrInvalidRequest] for a value above 100 and does not send the
	// request.
	PageSize int
}

// AlternatesResponse contains the cross-reference alternates of a product.
type AlternatesResponse struct {
	// Original is the product that the request names (rawMaterial).
	Original Product

	// Alternates holds the alternates on the requested page in the server
	// order. Each alternate has a MatchType (see [Product.Match]). The
	// server order is not always grouped by match type, so sort the list
	// when the order is important.
	Alternates []Product

	// InStockCount is the number of alternates with LCSC retail stock. It
	// does not change with [AlternatesRequest.InStockOnly].
	InStockCount int

	// TotalCount is the number of alternates on all pages (totalRow). LCSC
	// returned at most 99 alternates for the products that were checked
	// (inferred).
	TotalCount int

	// ActualTotalCount is the real number of alternates (actualTotalRow).
	// It is TotalCount when the response does not send actualTotalRow. The
	// name and the rule are the same as in [ListResponse] and
	// [SearchResponse]. For the products that were checked, both counts
	// had the same value.
	ActualTotalCount int
}

type alternatesWrapper struct {
	RawMaterial  *Product `json:"rawMaterial"`
	InStockCount int      `json:"inStockCount"`
	PageInfo     struct {
		TotalRow       int       `json:"totalRow"`
		ActualTotalRow *int      `json:"actualTotalRow"`
		DataList       []Product `json:"dataList"`
	} `json:"pageInfo"`
}

// List returns the cross-reference alternates of a product. It uses the
// /product/alternate/part/list endpoint, which the LCSC cross-reference
// tool uses.
//
// LCSC ignores the sort fields of this endpoint, so the request has no sort
// options. The server order is not always grouped by match type.
//
// List returns [ErrNotFound] when LCSC does not know the product code. A
// known product with no alternates gives a response with an empty
// Alternates list and no error.
func (s *AlternateService) List(ctx context.Context, req *AlternatesRequest) (*AlternatesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidRequest)
	}

	productCode := strings.ToUpper(strings.TrimSpace(req.ProductCode))
	if productCode == "" {
		return nil, fmt.Errorf("%w: productCode is required", ErrInvalidRequest)
	}

	page := req.Page
	switch {
	case page < 0:
		return nil, fmt.Errorf("%w: page must not be negative, got %d", ErrInvalidRequest, page)
	case page == 0:
		page = 1
	}

	pageSize := req.PageSize
	switch {
	case pageSize < 0 || pageSize > maxAlternatesPageSize:
		return nil, fmt.Errorf("%w: pageSize must be from 1 to %d, got %d", ErrInvalidRequest, maxAlternatesPageSize, pageSize)
	case pageSize == 0:
		pageSize = defaultAlternatesPageSize
	}

	client := s.client
	cacheKey := cacheKeyForAlternates(client.currency, productCode, req.InStockOnly, page, pageSize)
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var resp AlternatesResponse
			if err := json.Unmarshal(cached, &resp); err == nil {
				return &resp, nil
			}
		}
	}

	params := url.Values{}
	params.Set("productCode", productCode)
	params.Set("inStockOnly", strconv.FormatBool(req.InStockOnly))
	params.Set("currentPage", strconv.Itoa(page))
	params.Set("pageSize", strconv.Itoa(pageSize))

	var wrapper alternatesWrapper
	if err := client.do(ctx, http.MethodGet, "/product/alternate/part/list", params, nil, &wrapper); err != nil {
		return nil, err
	}

	// LCSC sends rawMaterial null and an empty list for an unknown code.
	if wrapper.RawMaterial == nil || strings.TrimSpace(wrapper.RawMaterial.ProductCode) == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, productCode)
	}

	resp := &AlternatesResponse{
		Original:         *wrapper.RawMaterial,
		Alternates:       wrapper.PageInfo.DataList,
		InStockCount:     wrapper.InStockCount,
		TotalCount:       wrapper.PageInfo.TotalRow,
		ActualTotalCount: wrapper.PageInfo.TotalRow,
	}
	if wrapper.PageInfo.ActualTotalRow != nil {
		resp.ActualTotalCount = *wrapper.PageInfo.ActualTotalRow
	}

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(resp); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return resp, nil
}

func cacheKeyForAlternates(currency, productCode string, inStockOnly bool, page, pageSize int) string {
	key := fmt.Sprintf("%s|%t|%d|%d", productCode, inStockOnly, page, pageSize)
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("alternates:%s:%s", strings.ToUpper(currency), hex.EncodeToString(hash[:8]))
}

// ParameterDiffKind tells how a parameter of an alternate differs from the
// same parameter of the original product.
type ParameterDiffKind string

const (
	// ParameterChanged means that both products have a value and that the
	// values are different.
	ParameterChanged ParameterDiffKind = "changed"

	// ParameterMissing means that the original product has a value and that
	// the alternate has no value.
	ParameterMissing ParameterDiffKind = "missing"

	// ParameterAdded means that the alternate has a value and that the
	// original product has no value.
	ParameterAdded ParameterDiffKind = "added"
)

// ParameterDiff is one parameter difference between an original product
// and an alternate. [DiffParameters] makes it.
type ParameterDiff struct {
	// Kind tells how the parameter differs.
	Kind ParameterDiffKind

	// Name is the English name of the parameter. It comes from the
	// original product. For [ParameterAdded], it comes from the alternate.
	Name string

	// ParamCode is the LCSC identifier of the parameter. It comes from the
	// same product as Name.
	ParamCode string

	// OriginalValue is the value of the original product as LCSC sends it.
	// It is empty when the original product does not have the parameter.
	OriginalValue string

	// AlternateValue is the value of the alternate as LCSC sends it. It is
	// empty when the alternate does not have the parameter.
	AlternateValue string

	// IsMain is true when one of the two products marks the parameter as a
	// key parameter.
	IsMain bool
}

// DiffParameters compares the parameters (ParamVOList) of original and alt.
// It returns only the parameters that are different. A nil product has no
// parameters.
//
// DiffParameters matches the parameters in two steps:
//
//  1. It matches parameters with the same ParamCode.
//  2. It matches the remaining parameters with the same name. The name
//     comparison ignores case, spaces and punctuation. This step is
//     necessary because LCSC uses different codes for the same parameter
//     in different categories.
//
// Two values are equal when their text is equal after DiffParameters
// removes the spaces. The comparison does not ignore case, because "1mΩ"
// and "1MΩ" are different values. Two values are also equal when both
// products have the same numeric search value (ParamValueEnForSearch), for
// example "100nF" and "0.1µF". DiffParameters uses the search value only
// when both texts are one number with an optional unit, and when both
// units are the same unit with an optional SI prefix. LCSC removes the
// test condition and other text from the search value: "21mΩ@2.5V" and
// "21mΩ@10V" both have 0.021, and "100,000 cycles" has 10. Such values are
// equal only when the text is equal. An empty value and "-" count as no
// value.
//
// The result lists the parameters of the original product in their order.
// The parameters that only the alternate has come after them, in the order
// of the alternate. DiffParameters does not compare the package
// (EncapStandard), because LCSC does not send the package as a parameter.
func DiffParameters(original, alt *Product) []ParameterDiff {
	var originalParams, altParams []Parameter
	if original != nil {
		originalParams = original.ParamVOList
	}
	if alt != nil {
		altParams = alt.ParamVOList
	}

	matches := matchParameters(originalParams, altParams)
	matched := make([]bool, len(altParams))

	var diffs []ParameterDiff
	for i := range originalParams {
		o := &originalParams[i]
		j := matches[i]
		if j < 0 {
			if hasParameterValue(o) {
				diffs = append(diffs, newParameterDiff(ParameterMissing, o, nil))
			}
			continue
		}

		matched[j] = true
		a := &altParams[j]
		switch {
		case !hasParameterValue(o) && !hasParameterValue(a):
		case !hasParameterValue(a):
			diffs = append(diffs, newParameterDiff(ParameterMissing, o, a))
		case !hasParameterValue(o):
			diffs = append(diffs, newParameterDiff(ParameterAdded, o, a))
		case !sameParameterValue(o, a):
			diffs = append(diffs, newParameterDiff(ParameterChanged, o, a))
		}
	}

	for j := range altParams {
		if !matched[j] && hasParameterValue(&altParams[j]) {
			diffs = append(diffs, newParameterDiff(ParameterAdded, nil, &altParams[j]))
		}
	}

	return diffs
}

// matchParameters returns, for each parameter of original, the index of the
// matching parameter of alt, or -1 when no parameter matches. It matches on
// ParamCode first and on the normalized name second. It uses each parameter
// of alt only one time.
func matchParameters(original, alt []Parameter) []int {
	matches := make([]int, len(original))
	for i := range matches {
		matches[i] = -1
	}
	used := make([]bool, len(alt))

	find := func(key func(*Parameter) string) {
		altKeys := make([]string, len(alt))
		for j := range alt {
			altKeys[j] = key(&alt[j])
		}
		for i := range original {
			if matches[i] >= 0 {
				continue
			}
			want := key(&original[i])
			if want == "" {
				continue
			}
			for j := range alt {
				if !used[j] && altKeys[j] == want {
					matches[i] = j
					used[j] = true
					break
				}
			}
		}
	}

	find(func(p *Parameter) string { return strings.ToLower(strings.TrimSpace(p.ParamCode)) })
	find(func(p *Parameter) string { return normalizeParameterName(p.ParamNameEn) })
	return matches
}

func newParameterDiff(kind ParameterDiffKind, original, alt *Parameter) ParameterDiff {
	diff := ParameterDiff{Kind: kind}
	source := original
	if kind == ParameterAdded || source == nil {
		source = alt
	}
	diff.Name = strings.TrimSpace(source.ParamNameEn)
	diff.ParamCode = strings.TrimSpace(source.ParamCode)
	if original != nil {
		diff.OriginalValue = strings.TrimSpace(original.ParamValueEn)
		diff.IsMain = original.IsMain
	}
	if alt != nil {
		diff.AlternateValue = strings.TrimSpace(alt.ParamValueEn)
		diff.IsMain = diff.IsMain || alt.IsMain
	}
	return diff
}

// hasParameterValue reports whether p has a value. LCSC sends "-" when a
// product has no value for a parameter.
func hasParameterValue(p *Parameter) bool {
	v := strings.TrimSpace(p.ParamValueEn)
	return v != "" && v != "-"
}

// sameParameterValue reports whether o and a have the same value. See
// [DiffParameters] for the rules.
func sameParameterValue(o, a *Parameter) bool {
	if removeSpaces(o.ParamValueEn) == removeSpaces(a.ParamValueEn) {
		return true
	}
	// The search value does not keep the test condition or other text of
	// the value. Use it only for two plain quantities in the same unit.
	ou, ok := plainQuantityUnit(o.ParamValueEn)
	if !ok {
		return false
	}
	au, ok := plainQuantityUnit(a.ParamValueEn)
	if !ok || !sameUnitBase(ou, au) {
		return false
	}
	ov, ok := parameterNumber(o)
	if !ok {
		return false
	}
	av, ok := parameterNumber(a)
	if !ok {
		return false
	}
	return math.Abs(ov-av) <= 1e-9*math.Max(math.Abs(ov), math.Abs(av))
}

// parameterNumber returns the numeric search value of p. LCSC sends null or
// -1 for some values that are not a number, for example "X7R".
func parameterNumber(p *Parameter) (float64, bool) {
	if p.ParamValueEnForSearch == nil || *p.ParamValueEnForSearch == -1 {
		return 0, false
	}
	return *p.ParamValueEnForSearch, true
}

// unitSymbols holds the characters other than letters that the unit of a
// plain quantity can contain, for example in "25℃", "1%" or "40nV/√Hz".
const unitSymbols = "%°℃℉/√²³"

// plainQuantityUnit returns the unit of s when s is one number with an
// optional unit, for example "100nF", "0.1 µF", "-40℃" or "8". The unit
// contains only letters and the characters in unitSymbols. ok is false for
// a value with a test condition ("21mΩ@10V"), a range ("1V~5V"), a
// tolerance ("±1%"), a number with a separator ("100,000 cycles") or other
// text ("1 N-channel").
func plainQuantityUnit(s string) (unit string, ok bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || (end == 0 && (c == '-' || c == '+')) {
			end++
			continue
		}
		break
	}
	if _, err := strconv.ParseFloat(s[:end], 64); err != nil {
		return "", false
	}
	unit = strings.TrimLeftFunc(s[end:], unicode.IsSpace)
	for _, r := range unit {
		if !unicode.IsLetter(r) && !strings.ContainsRune(unitSymbols, r) {
			return "", false
		}
	}
	return unit, true
}

// siPrefixes holds the SI prefixes of LCSC values. LCSC writes the micro
// prefix as "µ" (U+00B5), "μ" (U+03BC) or "u".
const siPrefixes = "pnuµμmkKMG"

// sameUnitBase reports whether a and b are the same unit after the
// removal of an optional SI prefix, for example "nF" and "µF", "mΩ" and
// "Ω", or "" and "".
func sameUnitBase(a, b string) bool {
	for _, x := range unitBases(a) {
		for _, y := range unitBases(b) {
			if x == y {
				return true
			}
		}
	}
	return false
}

// unitBases returns unit, and also unit without its first character when
// that character is an SI prefix and other characters follow it.
func unitBases(unit string) []string {
	bases := []string{unit}
	r, size := utf8.DecodeRuneInString(unit)
	if size > 0 && size < len(unit) && strings.ContainsRune(siPrefixes, r) {
		bases = append(bases, unit[size:])
	}
	return bases
}

// normalizeParameterName changes name to lower case and keeps only letters
// and digits.
func normalizeParameterName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// removeSpaces removes all white space from s.
func removeSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
