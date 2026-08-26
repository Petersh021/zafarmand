package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestInteriorProjectCoverRouteServesExactPublishedRevision verifies the real
// public route returns only the repository-approved bytes, complete integrity
// headers, a strong digest validator, and one deadline-bounded exact lookup.
func TestInteriorProjectCoverRouteServesExactPublishedRevision(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	asset := validTestInteriorProjectCoverAsset(t, 91, 7)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			interiorProjectCoverPath("stone-room", asset.Version),
			nil,
		),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", recorder.Code, recorder.Body.String())
	}
	wantETag := `"` + hex.EncodeToString(asset.SHA256[:]) + `"`
	wantHeaders := map[string]string{
		"Cache-Control":                "public, max-age=0, must-revalidate",
		"Content-Type":                 asset.ContentType,
		"Content-Length":               strconv.Itoa(asset.ByteSize),
		"ETag":                         wantETag,
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for name, want := range wantHeaders {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	if !bytes.Equal(recorder.Body.Bytes(), asset.Content) {
		t.Error("response body differs from the exact repository cover bytes")
	}
	calls := reader.coverCallSnapshot()
	if len(calls) != 1 || calls[0].Slug != "stone-room" ||
		calls[0].Version != asset.Version || !calls[0].HasDeadline {
		t.Errorf("cover calls: got %#v, want one bounded exact lookup", calls)
	}
	metadataCalls := reader.coverMetadataCallSnapshot()
	if len(metadataCalls) != 1 || metadataCalls[0].Slug != "stone-room" ||
		metadataCalls[0].Version != asset.Version || !metadataCalls[0].HasDeadline {
		t.Errorf("cover metadata calls: got %#v, want one bounded exact lookup", metadataCalls)
	}
}

// TestInteriorProjectCoverRouteRevalidatesWithETags covers HTTP weak matching,
// wildcard matching, nonmatching validators, and the invariant headers on both
// 304 and retransmitted 200 responses.
func TestInteriorProjectCoverRouteRevalidatesWithETags(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	asset := validTestInteriorProjectCoverAsset(t, 92, 3)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.interiorProjects = reader
	path := interiorProjectCoverPath("etag-interior", asset.Version)
	etag := `"` + hex.EncodeToString(asset.SHA256[:]) + `"`
	tests := []struct {
		// name identifies the conditional request form.
		name string
		// ifNoneMatch is the complete browser validator field.
		ifNoneMatch string
		// wantStatus distinguishes cache validation from retransmission.
		wantStatus int
	}{
		{name: "strong current validator", ifNoneMatch: etag, wantStatus: http.StatusNotModified},
		{name: "weak validator list", ifNoneMatch: `"other", W/` + etag, wantStatus: http.StatusNotModified},
		{name: "wildcard", ifNoneMatch: "*", wantStatus: http.StatusNotModified},
		{name: "different validator", ifNoneMatch: `"different"`, wantStatus: http.StatusOK},
		{name: "malformed validator", ifNoneMatch: etag + "garbage", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("If-None-Match", test.ifNoneMatch)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Header().Get("ETag") != etag ||
				recorder.Header().Get("Cache-Control") !=
					"public, max-age=0, must-revalidate" {
				t.Errorf("revalidation headers: %#v", recorder.Header())
			}
			if test.wantStatus == http.StatusNotModified && recorder.Body.Len() != 0 {
				t.Errorf("304 body length: got %d, want 0", recorder.Body.Len())
			}
			if test.wantStatus == http.StatusOK &&
				!bytes.Equal(recorder.Body.Bytes(), asset.Content) {
				t.Error("nonmatching validator did not receive complete cover bytes")
			}
		})
	}

	metadataCalls := reader.coverMetadataCallSnapshot()
	if len(metadataCalls) != len(tests) {
		t.Fatalf("metadata calls: got %d, want %d", len(metadataCalls), len(tests))
	}
	for _, call := range metadataCalls {
		if call.Slug != "etag-interior" || call.Version != asset.Version ||
			!call.HasDeadline {
			t.Errorf("conditional metadata call: %#v", call)
		}
	}
	if calls := reader.coverCallSnapshot(); len(calls) != 2 {
		t.Fatalf("content calls: got %d, want only 2 nonmatching validators", len(calls))
	}
}

// TestInteriorProjectCoverFailsClosedAfterMetadata proves the second public
// read cannot leak a raced archive, dependency error, or changed representation.
func TestInteriorProjectCoverFailsClosedAfterMetadata(t *testing.T) {
	const privateDetail = "private-interior-dependency-detail-sentinel"
	base := validTestInteriorProjectCoverAsset(t, 92, 3)
	tests := []struct {
		// name identifies one content-bearing lookup outcome.
		name string
		// configure changes only the second phase.
		configure func(*recordingInteriorProjectCatalogueReader)
		// wantStatus is the safe public result.
		wantStatus int
		// privateDetail is checked only for the dependency-error case.
		privateDetail string
	}{
		{
			name: "archived between reads",
			configure: func(reader *recordingInteriorProjectCatalogueReader) {
				reader.setCoverContent(interiorProjectCoverAsset{}, errInteriorProjectCoverNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "private content error",
			configure: func(reader *recordingInteriorProjectCatalogueReader) {
				reader.setCoverContent(interiorProjectCoverAsset{}, errors.New(privateDetail))
			},
			wantStatus:    http.StatusServiceUnavailable,
			privateDetail: privateDetail,
		},
		{
			name: "changed metadata",
			configure: func(reader *recordingInteriorProjectCatalogueReader) {
				changed := cloneInteriorProjectCoverAsset(base)
				changed.Caption = "Metadata changed between public reads."
				reader.setCoverContent(changed, nil)
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			reader.setCover(base, nil)
			test.configure(reader)
			app := newTestApplication(t)
			app.interiorProjects = reader
			recorder := httptest.NewRecorder()

			app.routes().ServeHTTP(
				recorder,
				httptest.NewRequest(
					http.MethodGet,
					interiorProjectCoverPath("etag-interior", base.Version),
					nil,
				),
			)

			assertReviewedCoverSecondPhaseFailure(
				t,
				recorder.Code,
				recorder.Header(),
				recorder.Body.Bytes(),
				test.wantStatus,
				base.Content,
				test.privateDetail,
			)
			metadataCalls := reader.coverMetadataCallSnapshot()
			contentCalls := reader.coverCallSnapshot()
			if len(metadataCalls) != 1 || len(contentCalls) != 1 ||
				!metadataCalls[0].HasDeadline || !contentCalls[0].HasDeadline {
				t.Errorf("two-phase calls: metadata=%#v content=%#v", metadataCalls, contentCalls)
			}
		})
	}
}

// TestInteriorProjectCoverRouteAcceptsHead uses a real httptest server because
// body suppression belongs to net/http's response writer, not ServeMux. The
// handler must still resolve the exact published revision and send GET headers.
func TestInteriorProjectCoverRouteAcceptsHead(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	asset := validTestInteriorProjectCoverAsset(t, 93, 2)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.interiorProjects = reader
	server := httptest.NewServer(app.routes())
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodHead,
		server.URL+interiorProjectCoverPath("head-interior", asset.Version),
		nil,
	)
	if err != nil {
		t.Fatalf("create HEAD request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform HEAD request: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read HEAD response: %v", err)
	}

	if response.StatusCode != http.StatusOK || len(body) != 0 {
		t.Errorf("HEAD response: status=%d body-bytes=%d", response.StatusCode, len(body))
	}
	if response.Header.Get("Content-Type") != asset.ContentType ||
		response.Header.Get("Content-Length") != strconv.Itoa(asset.ByteSize) ||
		response.Header.Get("ETag") == "" {
		t.Errorf("HEAD response headers: %#v", response.Header)
	}
	metadataCalls := reader.coverMetadataCallSnapshot()
	if len(metadataCalls) != 1 || metadataCalls[0].Slug != "head-interior" ||
		metadataCalls[0].Version != asset.Version || !metadataCalls[0].HasDeadline {
		t.Errorf("HEAD metadata calls: %#v", metadataCalls)
	}
	if calls := reader.coverCallSnapshot(); len(calls) != 0 {
		t.Errorf("HEAD loaded content: %#v", calls)
	}
}

// TestInteriorProjectCoverHandlerRejectsNoncanonicalRequests proves malformed
// path coordinates, equivalent escaped spellings, and query variants never
// reach persistence. Every handler-owned rejection remains explicitly no-store.
func TestInteriorProjectCoverHandlerRejectsNoncanonicalRequests(t *testing.T) {
	tests := []struct {
		// name identifies the untrusted URL boundary.
		name string
		// target is the request target whose EscapedPath is validated.
		target string
		// slug and version simulate ServeMux wildcard values.
		slug    string
		version string
		// wantStatus distinguishes malformed coordinates from forbidden queries.
		wantStatus int
	}{
		{name: "uppercase slug", target: "/interior-design/Stone-room/cover/1", slug: "Stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "zero version", target: "/interior-design/stone-room/cover/0", slug: "stone-room", version: "0", wantStatus: http.StatusNotFound},
		{name: "leading-zero version", target: "/interior-design/stone-room/cover/01", slug: "stone-room", version: "01", wantStatus: http.StatusNotFound},
		{name: "overflow version", target: "/interior-design/stone-room/cover/9223372036854775808", slug: "stone-room", version: "9223372036854775808", wantStatus: http.StatusNotFound},
		{name: "equivalent escaped slug", target: "/interior-design/%73tone-room/cover/1", slug: "stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "path coordinate mismatch", target: "/interior-design/other-room/cover/1", slug: "stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "query parameter", target: "/interior-design/stone-room/cover/1?download=1", slug: "stone-room", version: "1", wantStatus: http.StatusBadRequest},
		{name: "empty query marker", target: "/interior-design/stone-room/cover/1?", slug: "stone-room", version: "1", wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			app := newTestApplication(t)
			app.interiorProjects = reader
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.SetPathValue("slug", test.slug)
			request.SetPathValue("version", test.version)

			app.interiorProjectCoverHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control: got %q, want no-store", got)
			}
			if calls := reader.coverCallSnapshot(); len(calls) != 0 {
				t.Errorf("invalid request reached reader: %#v", calls)
			}
			if calls := reader.coverMetadataCallSnapshot(); len(calls) != 0 {
				t.Errorf("invalid request reached metadata reader: %#v", calls)
			}
		})
	}
}

// TestInteriorProjectCoverHandlerFailsClosed verifies missing and stale public
// media share 404, while dependency errors and malformed injected records share
// a redacted 503. Nil dependencies follow the same no-store service boundary.
func TestInteriorProjectCoverHandlerFailsClosed(t *testing.T) {
	const unsafeDetail = "postgres://private-interior-cover"
	tests := []struct {
		// name identifies the dependency outcome.
		name string
		// configure changes the default valid exact reader or application.
		configure func(*application, *recordingInteriorProjectCatalogueReader)
		// nilApplication calls the method through a nil receiver.
		nilApplication bool
		// wantStatus is the public privacy-preserving result.
		wantStatus int
		// wantCalls records whether valid coordinates may reach persistence.
		wantCalls int
	}{
		{name: "missing or hidden", configure: func(_ *application, reader *recordingInteriorProjectCatalogueReader) {
			reader.setCover(interiorProjectCoverAsset{}, errInteriorProjectCoverNotFound)
		}, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "stale revision", configure: func(_ *application, reader *recordingInteriorProjectCatalogueReader) {
			reader.setCover(validTestInteriorProjectCoverAsset(t, 94, 2), nil)
		}, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "repository failure", configure: func(_ *application, reader *recordingInteriorProjectCatalogueReader) {
			reader.setCover(interiorProjectCoverAsset{}, errors.New(unsafeDetail))
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "invalid asset", configure: func(_ *application, reader *recordingInteriorProjectCatalogueReader) {
			asset := validTestInteriorProjectCoverAsset(t, 94, 1)
			asset.AltText = ""
			reader.setCover(asset, nil)
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "mismatched returned revision", configure: func(_ *application, reader *recordingInteriorProjectCatalogueReader) {
			asset := validTestInteriorProjectCoverAsset(t, 94, 2)
			reader.setCoverResult(&asset)
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "nil dependency", configure: func(app *application, _ *recordingInteriorProjectCatalogueReader) {
			app.interiorProjects = nil
		}, wantStatus: http.StatusServiceUnavailable},
		{name: "nil application", nilApplication: true, wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			reader.setCover(validTestInteriorProjectCoverAsset(t, 94, 1), nil)
			var app *application
			if !test.nilApplication {
				app = newTestApplication(t)
				app.interiorProjects = reader
			}
			if test.configure != nil {
				test.configure(app, reader)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				interiorProjectCoverPath("private-interior", 1),
				nil,
			)
			request.SetPathValue("slug", "private-interior")
			request.SetPathValue("version", "1")

			app.interiorProjectCoverHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Error("failed cover response is not explicitly no-store")
			}
			if strings.Contains(recorder.Body.String(), unsafeDetail) {
				t.Error("cover failure exposed repository diagnostics")
			}
			if calls := reader.coverMetadataCallSnapshot(); len(calls) != test.wantCalls {
				t.Errorf("metadata calls: got %d, want %d", len(calls), test.wantCalls)
			} else if len(calls) == 1 && !calls[0].HasDeadline {
				t.Error("metadata dependency call has no deadline")
			}
			if calls := reader.coverCallSnapshot(); len(calls) != 0 {
				t.Errorf("failed metadata lookup loaded content: %#v", calls)
			}
		})
	}
}

// TestInteriorProjectCoverRouteKeepsExactBoundary verifies extra path segments
// and unsupported mutations are rejected by ServeMux before any media lookup.
func TestInteriorProjectCoverRouteKeepsExactBoundary(t *testing.T) {
	tests := []struct {
		// name identifies the route mismatch.
		name string
		// method is intentionally GET or an unsupported mutation.
		method string
		// path is the complete public request target.
		path string
		// wantStatus is ServeMux's generated response.
		wantStatus int
	}{
		{name: "extra segment", method: http.MethodGet, path: "/interior-design/stone-room/cover/1/extra", wantStatus: http.StatusNotFound},
		{name: "unsupported post", method: http.MethodPost, path: "/interior-design/stone-room/cover/1", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			app := newTestApplication(t)
			app.interiorProjects = reader
			recorder := httptest.NewRecorder()

			app.routes().ServeHTTP(
				recorder,
				httptest.NewRequest(test.method, test.path, nil),
			)

			if recorder.Code != test.wantStatus {
				t.Errorf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if calls := reader.coverCallSnapshot(); len(calls) != 0 {
				t.Errorf("unmatched route reached reader: %#v", calls)
			}
			if calls := reader.coverMetadataCallSnapshot(); len(calls) != 0 {
				t.Errorf("unmatched route reached metadata reader: %#v", calls)
			}
		})
	}
}
