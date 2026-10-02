package lcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ImageSize is the size of an LCSC product image. LCSC stores each product
// image in three sizes under the same file name.
type ImageSize string

const (
	// ImageSizeSmall is the 96x96 image. List rows send it in
	// ProductImageURL.
	ImageSizeSmall ImageSize = "96x96"

	// ImageSizeMedium is the 224x224 image.
	ImageSizeMedium ImageSize = "224x224"

	// ImageSizeLarge is the 900x900 image. Detail responses send it in
	// ProductImages. List rows send it in ProductImageURLBig.
	ImageSizeLarge ImageSize = "900x900"
)

// valid reports whether s is one of the three LCSC image sizes.
func (s ImageSize) valid() bool {
	switch s {
	case ImageSizeSmall, ImageSizeMedium, ImageSizeLarge:
		return true
	default:
		return false
	}
}

// lcscAssetsHost is the host of the LCSC product images.
const lcscAssetsHost = "assets.lcsc.com"

// ImageURLAtSize returns the URL of the same LCSC product image at another
// size. It replaces only the size segment of the path, for example
// "96x96" in
//
//	https://assets.lcsc.com/images/lcsc/96x96/20221227_..._C1525_front.jpg
//
// with size. It does not send a request. The image URLs are not signed and
// need no Referer.
//
// ImageURLAtSize returns raw with no change in these cases:
//
//   - raw is not on the host assets.lcsc.com.
//   - The path does not start with /images/ or has no size segment.
//   - size is not [ImageSizeSmall], [ImageSizeMedium] or
//     [ImageSizeLarge].
//
// All three sizes exist for the image names under /images/lcsc/. Some old
// images are under /images/szlcsc/. The function also changes them, but the
// 224x224 size was not checked for them (inferred).
func ImageURLAtSize(raw string, size ImageSize) string {
	if !size.valid() {
		return raw
	}
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(u.Hostname(), lcscAssetsHost) {
		return raw
	}

	// Change the raw string and not the parsed URL, so that the encoding
	// of the file name does not change.
	hostStart := strings.Index(trimmed, "//")
	if hostStart < 0 {
		return raw
	}
	pathStart := strings.IndexByte(trimmed[hostStart+2:], '/')
	if pathStart < 0 {
		return raw
	}
	pathStart += hostStart + 2
	pathEnd := len(trimmed)
	if i := strings.IndexAny(trimmed[pathStart:], "?#"); i >= 0 {
		pathEnd = pathStart + i
	}

	// The segments are "", "images", the image folder, the size and the
	// file name. The last segment is the file name, so it is not a size.
	segments := strings.Split(trimmed[pathStart:pathEnd], "/")
	if len(segments) < 4 || segments[1] != "images" {
		return raw
	}
	for i := 2; i < len(segments)-1; i++ {
		if ImageSize(segments[i]).valid() {
			segments[i] = string(size)
			return trimmed[:pathStart] + strings.Join(segments, "/") + trimmed[pathEnd:]
		}
	}
	return raw
}

// ImageURL returns the URL of the product image at size. It uses the first
// image URL that it finds: ProductImageURLBig, the first entry of
// ProductImages, then ProductImageURL. It changes the size with
// [ImageURLAtSize]. A URL that ImageURLAtSize cannot change comes back with
// no change. ImageURL returns an empty string when the product has no
// image.
func (p *Product) ImageURL(size ImageSize) string {
	if p == nil {
		return ""
	}
	candidates := []string{p.ProductImageURLBig}
	candidates = append(candidates, p.ProductImages...)
	candidates = append(candidates, p.ProductImageURL)
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return ImageURLAtSize(candidate, size)
		}
	}
	return ""
}

const (
	// datasheetCacheTTL is the cache time of a datasheet URL that
	// [ProductService.ResolveDatasheetURL] gets from a viewer page.
	datasheetCacheTTL = 24 * time.Hour

	// legacyDatasheetBaseURL replaces https://datasheet.lcsc.com/lcsc/ or
	// https://datasheet.lcsc.com/szlcsc/ in a legacy datasheet URL. The
	// legacy URL sends a redirect to the HTML viewer. The URL with this
	// base sends the PDF file.
	legacyDatasheetBaseURL = "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/"
)

// legacyDatasheetPrefixes holds the path prefixes of the legacy datasheet
// URLs on datasheet.lcsc.com. A /szlcsc/ URL sends a redirect to the
// /lcsc/ URL with the same file name.
var legacyDatasheetPrefixes = []string{"/lcsc/", "/szlcsc/"}

var (
	// nextDataPattern finds the JSON data of a Next.js page.
	nextDataPattern = regexp.MustCompile(`(?s)<script[^>]*\bid="__NEXT_DATA__"[^>]*>(.*?)</script>`)

	// previewPDFPattern finds the previewPdfUrl field in a page. The client
	// uses it when the page data does not have the expected structure.
	previewPDFPattern = regexp.MustCompile(`"previewPdfUrl"\s*:\s*("(?:[^"\\]|\\.)*")`)
)

// datasheetURLKind is the form of an LCSC datasheet URL.
type datasheetURLKind int

const (
	// datasheetOther is a URL that the resolver does not change.
	datasheetOther datasheetURLKind = iota

	// datasheetLegacy is https://datasheet.lcsc.com/lcsc/{file}.pdf or
	// https://datasheet.lcsc.com/szlcsc/{file}.pdf.
	datasheetLegacy

	// datasheetViewer is an HTML viewer page under
	// https://www.lcsc.com/datasheet/.
	datasheetViewer
)

// classifyDatasheetURL returns the form of u.
func classifyDatasheetURL(u *url.URL) datasheetURLKind {
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	switch {
	case legacyDatasheetFile(u) != "":
		return datasheetLegacy
	case (host == "www.lcsc.com" || host == "lcsc.com") && strings.HasPrefix(path, "/datasheet/") && len(path) > len("/datasheet/"):
		return datasheetViewer
	default:
		return datasheetOther
	}
}

// legacyDatasheetFile returns the escaped file path of a legacy datasheet
// URL, for example "{file}.pdf" for https://datasheet.lcsc.com/lcsc/{file}.pdf.
// It returns an empty string when u is not a legacy datasheet URL.
func legacyDatasheetFile(u *url.URL) string {
	if strings.ToLower(u.Hostname()) != "datasheet.lcsc.com" {
		return ""
	}
	path := u.EscapedPath()
	for _, prefix := range legacyDatasheetPrefixes {
		if file := strings.TrimPrefix(path, prefix); file != path && file != "" {
			return file
		}
	}
	return ""
}

// isDatasheetRedirect reports whether the client follows a redirect from a
// datasheet viewer page to u. The client follows a redirect to another
// viewer page or to a PDF file on an LCSC host. It does not follow other
// redirects. For an unknown product, LCSC sends a redirect to the home
// page.
func isDatasheetRedirect(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	switch {
	case classifyDatasheetURL(u) == datasheetViewer:
		return true
	case host == "datasheet.lcsc.com" && strings.HasPrefix(path, "/datasheet/pdf/"):
		return true
	case host == "wmsc.lcsc.com" && strings.HasPrefix(path, "/wmsc/upload/file/pdf/"):
		return true
	default:
		return false
	}
}

// ResolveDatasheetURL returns a URL that sends the datasheet PDF file. The
// LCSC and JLCPCB data use several URL forms for one datasheet:
//
//  1. https://datasheet.lcsc.com/datasheet/pdf/{hash}.pdf?productCode={C}.
//     [Product.PdfURL] uses this form. It sends the PDF file.
//     ResolveDatasheetURL returns it with no change.
//  2. https://datasheet.lcsc.com/lcsc/{file}.pdf or
//     https://datasheet.lcsc.com/szlcsc/{file}.pdf (legacy forms). They
//     send a redirect to the HTML viewer. ResolveDatasheetURL returns
//     https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/{file}.pdf, which
//     sends the PDF file. It does not send a request.
//  3. https://www.lcsc.com/datasheet/{name}.pdf (viewer form). The JLCPCB
//     part data uses this form. The page is HTML, although the name ends
//     with ".pdf". ResolveDatasheetURL sends one GET request for the page
//     and returns the previewPdfUrl value of the page data (form 1).
//
// ResolveDatasheetURL returns any other absolute http or https URL with no
// change and does not send a request. It returns [ErrInvalidRequest] for an
// empty value or a value that is not an absolute http or https URL. For
// example, JLCPCB sends "--" when a part has no datasheet.
//
// For a viewer page of an unknown product, LCSC sends a redirect to the
// home page. ResolveDatasheetURL then returns [ErrNotFound]. It also
// returns ErrNotFound when the page has no previewPdfUrl.
//
// The request uses the rate limiter and the retry rules of the client. The
// client caches the result of a viewer page for 24 hours.
func (s *ProductService) ResolveDatasheetURL(ctx context.Context, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: datasheet URL is required", ErrInvalidRequest)
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: datasheet URL must be an absolute http or https URL, got %q", ErrInvalidRequest, trimmed)
	}

	switch classifyDatasheetURL(u) {
	case datasheetLegacy:
		return legacyDatasheetBaseURL + legacyDatasheetFile(u), nil
	case datasheetViewer:
		return s.resolveDatasheetViewer(ctx, u)
	default:
		return trimmed, nil
	}
}

// resolveDatasheetViewer gets the URL of the PDF file from a datasheet
// viewer page.
func (s *ProductService) resolveDatasheetViewer(ctx context.Context, u *url.URL) (string, error) {
	pageURL := *u
	pageURL.Scheme = "https"
	pageURL.Fragment = ""
	target := pageURL.String()

	client := s.client
	cacheKey := cacheKeyForDatasheet(target)
	useCache := client.cacheConfig.Enabled && client.cache != nil
	if useCache {
		if cached, ok := client.cache.Get(cacheKey); ok && len(cached) > 0 {
			return string(cached), nil
		}
	}

	page, err := client.getPage(ctx, target, isDatasheetRedirect)
	if err != nil {
		return "", err
	}

	var pdfURL string
	switch {
	case page.StatusCode >= 300 && page.StatusCode < 400:
		return "", fmt.Errorf("%w: LCSC sends a redirect from the datasheet page %s to %q", ErrNotFound, target, page.Location)
	case page.isPDF():
		// LCSC sent the PDF file, directly or after a redirect.
		pdfURL = page.URL
	default:
		pdfURL = previewPDFURL(page.Body, page.URL)
	}
	if pdfURL == "" {
		return "", fmt.Errorf("%w: no datasheet PDF URL in the page %s", ErrNotFound, target)
	}

	if useCache {
		client.cache.Set(cacheKey, []byte(pdfURL), datasheetCacheTTL)
	}
	return pdfURL, nil
}

// previewPDFURL returns the previewPdfUrl value of a datasheet viewer page.
// It reads props.pageProps.previewPdfUrl from the __NEXT_DATA__ script.
// When that field is empty, it uses the first previewPdfUrl field in the
// page. It resolves a relative URL against pageURL. It returns an empty
// string when the page has no http or https previewPdfUrl.
func previewPDFURL(body []byte, pageURL string) string {
	var value string
	if match := nextDataPattern.FindSubmatch(body); match != nil {
		var data struct {
			Props struct {
				PageProps struct {
					PreviewPdfURL string `json:"previewPdfUrl"`
				} `json:"pageProps"`
			} `json:"props"`
		}
		if err := json.Unmarshal(match[1], &data); err == nil {
			value = strings.TrimSpace(data.Props.PageProps.PreviewPdfURL)
		}
	}
	if value == "" {
		if match := previewPDFPattern.FindSubmatch(body); match != nil {
			var s string
			if err := json.Unmarshal(match[1], &s); err == nil {
				value = strings.TrimSpace(s)
			}
		}
	}
	if value == "" {
		return ""
	}

	ref, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if !ref.IsAbs() {
		base, err := url.Parse(pageURL)
		if err != nil {
			return ""
		}
		ref = base.ResolveReference(ref)
		value = ref.String()
	}
	if ref.Scheme != "http" && ref.Scheme != "https" {
		return ""
	}
	return value
}

// cacheKeyForDatasheet makes the cache key of a viewer page URL. The result
// does not depend on the currency.
func cacheKeyForDatasheet(pageURL string) string {
	hash := sha256.Sum256([]byte(pageURL))
	return "datasheet:" + hex.EncodeToString(hash[:8])
}
