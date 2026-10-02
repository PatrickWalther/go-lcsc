package lcsc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testViewerURL  = "https://www.lcsc.com/datasheet/lcsc_datasheet_2201101600_Raspberry-Pi-RP2040_C2040.pdf"
	testPreviewURL = "https://datasheet.lcsc.com/datasheet/pdf/3dad1d8f987a4e00bfc677055df1f959.pdf?productCode=C2040"
)

// pageResponseFor returns a response for req with a body and a content
// type. It sets the request of the response, as the HTTP transport does.
func pageResponseFor(req *http.Request, status int, contentType, body string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}

func redirectResponseFor(req *http.Request, status int, location string) *http.Response {
	resp := pageResponseFor(req, status, "", "")
	resp.Header.Set("Location", location)
	return resp
}

func htmlResponseFor(req *http.Request, body string) *http.Response {
	return pageResponseFor(req, http.StatusOK, "text/html; charset=utf-8", body)
}

func TestImageURLAtSize(t *testing.T) {
	const small = "https://assets.lcsc.com/images/lcsc/96x96/20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg"
	const large = "https://assets.lcsc.com/images/lcsc/900x900/20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg"

	tests := []struct {
		name string
		raw  string
		size ImageSize
		want string
	}{
		{"small to large", small, ImageSizeLarge, large},
		{"large to small", large, ImageSizeSmall, small},
		{"small to medium", small, ImageSizeMedium, "https://assets.lcsc.com/images/lcsc/224x224/20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg"},
		{"same size", large, ImageSizeLarge, large},
		{"old image folder", "https://assets.lcsc.com/images/szlcsc/900x900/20140620_C1525_front.jpg", ImageSizeSmall, "https://assets.lcsc.com/images/szlcsc/96x96/20140620_C1525_front.jpg"},
		{"upper-case file extension", "https://assets.lcsc.com/images/lcsc/96x96/20190225_YAGEO-AC0402DR-0710KL_C226679_front.JPG", ImageSizeLarge, "https://assets.lcsc.com/images/lcsc/900x900/20190225_YAGEO-AC0402DR-0710KL_C226679_front.JPG"},
		{"http and upper-case host", "http://ASSETS.LCSC.COM/images/lcsc/96x96/a.jpg", ImageSizeLarge, "http://ASSETS.LCSC.COM/images/lcsc/900x900/a.jpg"},
		{"query and fragment", "https://assets.lcsc.com/images/lcsc/96x96/a.jpg?v=96x96#96x96", ImageSizeLarge, "https://assets.lcsc.com/images/lcsc/900x900/a.jpg?v=96x96#96x96"},
		{"escaped file name", "https://assets.lcsc.com/images/lcsc/96x96/a%20b%2Bc.jpg", ImageSizeMedium, "https://assets.lcsc.com/images/lcsc/224x224/a%20b%2Bc.jpg"},
		{"spaces", "  " + small + " ", ImageSizeLarge, large},
		{"file name like a size", "https://assets.lcsc.com/images/lcsc/96x96", ImageSizeLarge, "https://assets.lcsc.com/images/lcsc/96x96"},
		{"no size segment", "https://assets.lcsc.com/images/lcsc/a.jpg", ImageSizeLarge, "https://assets.lcsc.com/images/lcsc/a.jpg"},
		{"not an image path", "https://assets.lcsc.com/lcsc/mwos/api/96x96/a.jpg", ImageSizeLarge, "https://assets.lcsc.com/lcsc/mwos/api/96x96/a.jpg"},
		{"other host", "https://embed.widencdn.net/img/rocelec/vfpdykkuhs/640px/a.png?keep=c", ImageSizeSmall, "https://embed.widencdn.net/img/rocelec/vfpdykkuhs/640px/a.png?keep=c"},
		{"host with suffix", "https://assets.lcsc.com.example.org/images/lcsc/96x96/a.jpg", ImageSizeLarge, "https://assets.lcsc.com.example.org/images/lcsc/96x96/a.jpg"},
		{"unknown size", small, ImageSize("640x640"), small},
		{"empty size", small, "", small},
		{"empty URL", "", ImageSizeLarge, ""},
		{"not a URL", "%zz", ImageSizeLarge, "%zz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ImageURLAtSize(tt.raw, tt.size); got != tt.want {
				t.Fatalf("ImageURLAtSize(%q, %q):\n got %q\nwant %q", tt.raw, tt.size, got, tt.want)
			}
		})
	}
}

func TestProductImageURL(t *testing.T) {
	// Detail responses send ProductImages (900x900). List rows send
	// ProductImageURL (96x96) and ProductImageURLBig (900x900).
	var detail Product
	if err := json.Unmarshal(fixtureResult(t, "product_detail_C1525_EUR.json"), &detail); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	const name = "20221227_Samsung-Electro-Mechanics-CL05B104KO5NNNC_C1525_front.jpg"
	if got := detail.ImageURL(ImageSizeSmall); got != "https://assets.lcsc.com/images/lcsc/96x96/"+name {
		t.Fatalf("unexpected detail image %q", got)
	}

	tests := []struct {
		name    string
		product *Product
		want    string
	}{
		{"nil product", nil, ""},
		{"no image", &Product{ProductImages: []string{" "}}, ""},
		{"big image first", &Product{ProductImageURL: "https://assets.lcsc.com/images/lcsc/96x96/a.jpg", ProductImageURLBig: "https://assets.lcsc.com/images/lcsc/900x900/b.jpg"}, "https://assets.lcsc.com/images/lcsc/224x224/b.jpg"},
		{"small image only", &Product{ProductImageURL: "https://assets.lcsc.com/images/lcsc/96x96/a.jpg"}, "https://assets.lcsc.com/images/lcsc/224x224/a.jpg"},
		{"other host", &Product{ProductImageURL: "https://example.com/a.jpg"}, "https://example.com/a.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.product.ImageURL(ImageSizeMedium); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// fixtureResult returns the result field of a fixture envelope.
func fixtureResult(t *testing.T, name string) json.RawMessage {
	t.Helper()
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(mustReadFixture(t, name)), &envelope); err != nil {
		t.Fatalf("failed to decode fixture %s: %v", name, err)
	}
	return envelope.Result
}

func TestResolveDatasheetURLWithoutRequest(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"direct hash URL", testPreviewURL, testPreviewURL},
		{"direct hash URL with spaces", " " + testPreviewURL + " ", testPreviewURL},
		{"wmsc PDF URL", "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/2304140030_STMicroelectronics-STM32F103CBT7_C1338636.pdf", "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/2304140030_STMicroelectronics-STM32F103CBT7_C1338636.pdf"},
		{"legacy URL", "https://datasheet.lcsc.com/lcsc/2201101600_Raspberry-Pi-RP2040_C2040.pdf", "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/2201101600_Raspberry-Pi-RP2040_C2040.pdf"},
		{"legacy URL with http", "http://DATASHEET.LCSC.COM/lcsc/1809212227_Changjiang-Electronics-Tech-CJ-DTC114YUA_C13490.pdf", "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/1809212227_Changjiang-Electronics-Tech-CJ-DTC114YUA_C13490.pdf"},
		{"legacy URL with escaped name", "https://datasheet.lcsc.com/lcsc/a%20b.pdf", "https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/a%20b.pdf"},
		{"legacy folder only", "https://datasheet.lcsc.com/lcsc/", "https://datasheet.lcsc.com/lcsc/"},
		{"viewer folder only", "https://www.lcsc.com/datasheet/", "https://www.lcsc.com/datasheet/"},
		{"product page", "https://www.lcsc.com/product-detail/C2040.html", "https://www.lcsc.com/product-detail/C2040.html"},
		{"other host", "https://www.ti.com/lit/ds/symlink/lm358.pdf", "https://www.ti.com/lit/ds/symlink/lm358.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("expected no request, got %s", req.URL)
				return nil, nil
			})
			defer func() { _ = client.Close() }()

			got, err := client.Product.ResolveDatasheetURL(context.Background(), tt.raw)
			if err != nil {
				t.Fatalf("resolve failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestResolveDatasheetURLInvalid(t *testing.T) {
	// JLCPCB sends "--" for a part with no datasheet.
	for _, raw := range []string{"", "   ", "--", "datasheet.lcsc.com/lcsc/a.pdf", "ftp://datasheet.lcsc.com/lcsc/a.pdf", "https://", "http://%zz"} {
		t.Run(raw, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("expected no request, got %s", req.URL)
				return nil, nil
			})
			defer func() { _ = client.Close() }()

			if _, err := client.Product.ResolveDatasheetURL(context.Background(), raw); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestResolveDatasheetURLViewerPage(t *testing.T) {
	// Trimmed live viewer page for C2040. The page also has a canonical
	// self-link that ends with ".pdf". The resolver must not return it.
	page := mustReadFixture(t, "datasheet_viewer_C2040.html")
	var calls int32
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.Method != http.MethodGet || req.URL.String() != testViewerURL {
			t.Errorf("unexpected request %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("unexpected user agent %q", got)
		}
		if got := req.Header.Get("Accept"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("unexpected accept header %q", got)
		}
		if got := req.Header.Get("Cookie"); got != "" {
			t.Errorf("expected no cookie, got %q", got)
		}
		return htmlResponseFor(req, page), nil
	})
	defer func() { _ = client.Close() }()

	// The client sends https also for an http viewer URL.
	got, err := client.Product.ResolveDatasheetURL(context.Background(), strings.Replace(testViewerURL, "https://", "http://", 1))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got != testPreviewURL {
		t.Fatalf("\n got %q\nwant %q", got, testPreviewURL)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("expected 1 request, got %d", n)
	}
	if client.httpClient.CheckRedirect != nil {
		t.Fatal("expected no change to the redirect rule of the HTTP client")
	}
}

func TestResolveDatasheetURLViewerIsCachedFor24Hours(t *testing.T) {
	page := mustReadFixture(t, "datasheet_viewer_C2040.html")
	cache := newRecordingCache()
	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return htmlResponseFor(req, page), nil
	})
	defer func() { _ = client.Close() }()

	for i := 0; i < 2; i++ {
		got, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL)
		if err != nil || got != testPreviewURL {
			t.Fatalf("call %d: got %q (%v)", i, got, err)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("expected 1 request, got %d", n)
	}
	if ttl := cache.ttls[cacheKeyForDatasheet(testViewerURL)]; ttl != 24*time.Hour {
		t.Fatalf("expected a TTL of 24 hours, got %v", ttl)
	}
}

func TestResolveDatasheetURLViewerRedirects(t *testing.T) {
	page := mustReadFixture(t, "datasheet_viewer_C2040.html")
	const shortViewer = "https://www.lcsc.com/datasheet/C2040.pdf"

	t.Run("redirect to the home page", func(t *testing.T) {
		// Live shape: LCSC answers the viewer page of an unknown product
		// with 307 to the home page. The client must not load the home
		// page.
		var calls int32
		client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return redirectResponseFor(req, http.StatusTemporaryRedirect, "https://www.lcsc.com/"), nil
		})
		defer func() { _ = client.Close() }()

		_, err := client.Product.ResolveDatasheetURL(context.Background(), "https://www.lcsc.com/datasheet/C99999999.pdf")
		if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "https://www.lcsc.com/") {
			t.Fatalf("expected ErrNotFound with the redirect target, got %v", err)
		}
		if n := atomic.LoadInt32(&calls); n != 1 {
			t.Fatalf("expected 1 request, got %d", n)
		}
	})

	t.Run("redirect to another viewer page", func(t *testing.T) {
		var paths []string
		client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.String())
			if req.URL.String() == shortViewer {
				return redirectResponseFor(req, http.StatusMovedPermanently, testViewerURL), nil
			}
			return htmlResponseFor(req, page), nil
		})
		defer func() { _ = client.Close() }()

		got, err := client.Product.ResolveDatasheetURL(context.Background(), shortViewer)
		if err != nil || got != testPreviewURL {
			t.Fatalf("got %q (%v)", got, err)
		}
		if len(paths) != 2 || paths[1] != testViewerURL {
			t.Fatalf("unexpected requests %v", paths)
		}
	})

	t.Run("redirect to the PDF file", func(t *testing.T) {
		var calls int32
		client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			if req.URL.String() == shortViewer {
				return redirectResponseFor(req, http.StatusFound, testPreviewURL), nil
			}
			// The client must not read the PDF body.
			return pageResponseFor(req, http.StatusOK, "application/pdf", "not read"), nil
		})
		defer func() { _ = client.Close() }()

		got, err := client.Product.ResolveDatasheetURL(context.Background(), shortViewer)
		if err != nil || got != testPreviewURL {
			t.Fatalf("got %q (%v)", got, err)
		}
		if n := atomic.LoadInt32(&calls); n != 2 {
			t.Fatalf("expected 2 requests, got %d", n)
		}
	})

	t.Run("redirect loop", func(t *testing.T) {
		var calls int32
		client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return redirectResponseFor(req, http.StatusFound, shortViewer), nil
		})
		defer func() { _ = client.Close() }()

		if _, err := client.Product.ResolveDatasheetURL(context.Background(), shortViewer); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if n := atomic.LoadInt32(&calls); n != maxPageRedirects+1 {
			t.Fatalf("expected %d requests, got %d", maxPageRedirects+1, n)
		}
	})
}

func TestResolveDatasheetURLViewerSendsPDF(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{"pdf content type", "application/pdf; charset=binary", ""},
		{"pdf body", "application/octet-stream", "%PDF-1.7\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
				return pageResponseFor(req, http.StatusOK, tt.contentType, tt.body), nil
			})
			defer func() { _ = client.Close() }()

			got, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL)
			if err != nil || got != testViewerURL {
				t.Fatalf("expected the page URL, got %q (%v)", got, err)
			}
		})
	}
}

func TestResolveDatasheetURLViewerWithoutPreview(t *testing.T) {
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		return htmlResponseFor(req, `<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{}}}</script></html>`), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveDatasheetURLViewerHTTPErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		recordRetrySleeps(t)
		var calls int32
		client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return pageResponseFor(req, http.StatusNotFound, "text/html", "<html>404</html>"), nil
		})
		defer func() { _ = client.Close() }()

		_, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL)
		var apiErr *APIError
		if !errors.Is(err, ErrNotFound) || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			t.Fatalf("expected an API error with ErrNotFound, got %v", err)
		}
		if n := atomic.LoadInt32(&calls); n != 1 {
			t.Fatalf("expected no retry, got %d requests", n)
		}
	})

	t.Run("server error then page", func(t *testing.T) {
		delays := recordRetrySleeps(t)
		page := mustReadFixture(t, "datasheet_viewer_C2040.html")
		var calls int32
		client := newRetryTestClient(func(req *http.Request) (*http.Response, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				resp := pageResponseFor(req, http.StatusServiceUnavailable, "text/html", "busy")
				resp.Header.Set("Retry-After", "2")
				return resp, nil
			}
			return htmlResponseFor(req, page), nil
		})
		defer func() { _ = client.Close() }()

		got, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL)
		if err != nil || got != testPreviewURL {
			t.Fatalf("got %q (%v)", got, err)
		}
		if n := atomic.LoadInt32(&calls); n != 2 {
			t.Fatalf("expected 2 requests, got %d", n)
		}
		if len(*delays) != 1 || (*delays)[0] != 2*time.Second {
			t.Fatalf("expected one wait of 2s from Retry-After, got %v", *delays)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
			resp := pageResponseFor(req, http.StatusTooManyRequests, "text/html", "")
			resp.Header.Set("Retry-After", "60")
			return resp, nil
		})
		defer func() { _ = client.Close() }()

		_, err := client.Product.ResolveDatasheetURL(context.Background(), testViewerURL)
		var apiErr *APIError
		if !errors.Is(err, ErrRateLimited) || !errors.As(err, &apiErr) || apiErr.RetryAfter != time.Minute {
			t.Fatalf("expected ErrRateLimited with Retry-After 60s, got %v", err)
		}
	})
}

func TestPreviewPDFURL(t *testing.T) {
	const pageURL = "https://www.lcsc.com/datasheet/C2040.pdf"
	tests := []struct {
		name string
		body string
		want string
	}{
		{"page data", `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"previewPdfUrl":"` + testPreviewURL + `"}}}</script>`, testPreviewURL},
		{"other attribute order", `<script type="application/json" id="__NEXT_DATA__" crossorigin="anonymous">{"props":{"pageProps":{"previewPdfUrl":"` + testPreviewURL + `"}}}</script>`, testPreviewURL},
		{"other structure", `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"data":{"previewPdfUrl":"` + testPreviewURL + `"}}}}</script>`, testPreviewURL},
		{"no page data", `<div data-x='{"previewPdfUrl" : "https:\/\/datasheet.lcsc.com\/datasheet\/pdf\/a.pdf"}'></div>`, "https://datasheet.lcsc.com/datasheet/pdf/a.pdf"},
		{"relative URL", `{"previewPdfUrl":"/datasheet/pdf/a.pdf?productCode=C2040"}`, "https://www.lcsc.com/datasheet/pdf/a.pdf?productCode=C2040"},
		{"empty value", `{"previewPdfUrl":""}`, ""},
		{"null value", `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"previewPdfUrl":null}}}</script>`, ""},
		{"not http", `{"previewPdfUrl":"javascript:alert(1)"}`, ""},
		{"no field", `<html></html>`, ""},
		{"broken page data", `<script id="__NEXT_DATA__">{"props":</script>`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := previewPDFURL([]byte(tt.body), pageURL); got != tt.want {
				t.Fatalf("\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestIsDatasheetRedirect(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{testViewerURL, true},
		{"https://lcsc.com/datasheet/C2040.pdf", true},
		{testPreviewURL, true},
		{"https://wmsc.lcsc.com/wmsc/upload/file/pdf/v2/lcsc/a.pdf", true},
		{"https://www.lcsc.com/", false},
		{"https://www.lcsc.com/datasheet/", false},
		{"https://datasheet.lcsc.com/lcsc/a.pdf", false},
		{"https://example.com/datasheet/pdf/a.pdf", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			u, err := url.Parse(tt.target)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if got := isDatasheetRedirect(u); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
	if isDatasheetRedirect(nil) {
		t.Fatal("expected false for a nil URL")
	}
}
