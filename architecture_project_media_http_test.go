package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// validTestArchitectureProjectCoverAsset creates one internally consistent
// Architecture cover from a decoder-verified deterministic PNG fixture.
func validTestArchitectureProjectCoverAsset(
	t *testing.T,
	projectID int64,
	version int64,
) architectureProjectCoverAsset {
	t.Helper()

	content := testArchitectureProjectCoverPNG(t)
	inspection, err := inspectReviewedCover(content, true)
	if err != nil {
		t.Fatalf("inspect deterministic Architecture cover: %v", err)
	}
	createdAt := time.Date(2036, time.April, 5, 6, 7, 8, 0, time.UTC)

	return architectureProjectCoverAsset{
		ArchitectureProjectID: projectID,
		Version:               version,
		ContentType:           inspection.ContentType,
		Content:               append([]byte(nil), content...),
		ByteSize:              len(content),
		Width:                 inspection.Width,
		Height:                inspection.Height,
		SHA256:                inspection.SHA256,
		AltText: "A fictional Architecture facade with geometric " +
			"openings",
		Caption:   "Synthetic Stage 23 cover fixture.",
		CreatedAt: createdAt,
		UpdatedAt: createdAt.Add(time.Minute),
	}
}

// testArchitectureProjectCoverPNG returns a small real image whose decoded
// facts exercise the same reviewed-cover boundary as production uploads.
func testArchitectureProjectCoverPNG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetRGBA(
				x,
				y,
				color.RGBA{
					R: uint8(35 + x*20),
					G: uint8(55 + y*25),
					B: 85,
					A: 255,
				},
			)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode deterministic Architecture cover PNG: %v", err)
	}

	return encoded.Bytes()
}

// TestArchitectureProjectCoverRouteServesExactPublishedRevision verifies the real
// public route returns only the repository-approved bytes, complete integrity
// headers, a strong digest validator, and one deadline-bounded exact lookup.
func TestArchitectureProjectCoverRouteServesExactPublishedRevision(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	asset := validTestArchitectureProjectCoverAsset(t, 91, 7)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.architectureProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			architectureProjectCoverPath("stone-room", asset.Version),
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
}

// TestArchitectureProjectCoverRouteRevalidatesWithETags covers HTTP weak matching,
// wildcard matching, nonmatching validators, and the invariant headers on both
// 304 and retransmitted 200 responses.
func TestArchitectureProjectCoverRouteRevalidatesWithETags(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	asset := validTestArchitectureProjectCoverAsset(t, 92, 3)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.architectureProjects = reader
	path := architectureProjectCoverPath("etag-architecture", asset.Version)
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

	calls := reader.coverCallSnapshot()
	if len(calls) != len(tests) {
		t.Fatalf("cover calls: got %d, want %d", len(calls), len(tests))
	}
	for _, call := range calls {
		if call.Slug != "etag-architecture" || call.Version != asset.Version ||
			!call.HasDeadline {
			t.Errorf("conditional cover call: %#v", call)
		}
	}
}

// TestArchitectureProjectCoverRouteAcceptsHead uses a real httptest server because
// body suppression belongs to net/http's response writer, not ServeMux. The
// handler must still resolve the exact published revision and send GET headers.
func TestArchitectureProjectCoverRouteAcceptsHead(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	asset := validTestArchitectureProjectCoverAsset(t, 93, 2)
	reader.setCover(asset, nil)
	app := newTestApplication(t)
	app.architectureProjects = reader
	server := httptest.NewServer(app.routes())
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodHead,
		server.URL+architectureProjectCoverPath("head-architecture", asset.Version),
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
	calls := reader.coverCallSnapshot()
	if len(calls) != 1 || calls[0].Slug != "head-architecture" ||
		calls[0].Version != asset.Version || !calls[0].HasDeadline {
		t.Errorf("HEAD cover calls: %#v", calls)
	}
}

// TestArchitectureProjectCoverHandlerRejectsNoncanonicalRequests proves malformed
// path coordinates, equivalent escaped spellings, and query variants never
// reach persistence. Every handler-owned rejection remains explicitly no-store.
func TestArchitectureProjectCoverHandlerRejectsNoncanonicalRequests(t *testing.T) {
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
		{name: "uppercase slug", target: "/architecture-design/Stone-room/cover/1", slug: "Stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "zero version", target: "/architecture-design/stone-room/cover/0", slug: "stone-room", version: "0", wantStatus: http.StatusNotFound},
		{name: "leading-zero version", target: "/architecture-design/stone-room/cover/01", slug: "stone-room", version: "01", wantStatus: http.StatusNotFound},
		{name: "overflow version", target: "/architecture-design/stone-room/cover/9223372036854775808", slug: "stone-room", version: "9223372036854775808", wantStatus: http.StatusNotFound},
		{name: "equivalent escaped slug", target: "/architecture-design/%73tone-room/cover/1", slug: "stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "path coordinate mismatch", target: "/architecture-design/other-room/cover/1", slug: "stone-room", version: "1", wantStatus: http.StatusNotFound},
		{name: "query parameter", target: "/architecture-design/stone-room/cover/1?download=1", slug: "stone-room", version: "1", wantStatus: http.StatusBadRequest},
		{name: "empty query marker", target: "/architecture-design/stone-room/cover/1?", slug: "stone-room", version: "1", wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingArchitectureProjectCatalogueReader()
			app := newTestApplication(t)
			app.architectureProjects = reader
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.SetPathValue("slug", test.slug)
			request.SetPathValue("version", test.version)

			app.architectureProjectCoverHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control: got %q, want no-store", got)
			}
			if calls := reader.coverCallSnapshot(); len(calls) != 0 {
				t.Errorf("invalid request reached reader: %#v", calls)
			}
		})
	}
}

// TestArchitectureProjectCoverHandlerFailsClosed verifies missing and stale public
// media share 404, while dependency errors and malformed injected records share
// a redacted 503. Nil dependencies follow the same no-store service boundary.
func TestArchitectureProjectCoverHandlerFailsClosed(t *testing.T) {
	const unsafeDetail = "postgres://private-architecture-cover"
	tests := []struct {
		// name identifies the dependency outcome.
		name string
		// configure changes the default valid exact reader or application.
		configure func(*application, *recordingArchitectureProjectCatalogueReader)
		// nilApplication calls the method through a nil receiver.
		nilApplication bool
		// wantStatus is the public privacy-preserving result.
		wantStatus int
		// wantCalls records whether valid coordinates may reach persistence.
		wantCalls int
	}{
		{name: "missing or hidden", configure: func(_ *application, reader *recordingArchitectureProjectCatalogueReader) {
			reader.setCover(architectureProjectCoverAsset{}, errArchitectureProjectCoverNotFound)
		}, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "stale revision", configure: func(_ *application, reader *recordingArchitectureProjectCatalogueReader) {
			reader.setCover(validTestArchitectureProjectCoverAsset(t, 94, 2), nil)
		}, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "repository failure", configure: func(_ *application, reader *recordingArchitectureProjectCatalogueReader) {
			reader.setCover(architectureProjectCoverAsset{}, errors.New(unsafeDetail))
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "invalid asset", configure: func(_ *application, reader *recordingArchitectureProjectCatalogueReader) {
			asset := validTestArchitectureProjectCoverAsset(t, 94, 1)
			asset.AltText = ""
			reader.setCover(asset, nil)
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "mismatched returned revision", configure: func(_ *application, reader *recordingArchitectureProjectCatalogueReader) {
			asset := validTestArchitectureProjectCoverAsset(t, 94, 2)
			reader.setCoverResult(&asset)
		}, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "nil dependency", configure: func(app *application, _ *recordingArchitectureProjectCatalogueReader) {
			app.architectureProjects = nil
		}, wantStatus: http.StatusServiceUnavailable},
		{name: "nil application", nilApplication: true, wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingArchitectureProjectCatalogueReader()
			reader.setCover(validTestArchitectureProjectCoverAsset(t, 94, 1), nil)
			var app *application
			if !test.nilApplication {
				app = newTestApplication(t)
				app.architectureProjects = reader
			}
			if test.configure != nil {
				test.configure(app, reader)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				architectureProjectCoverPath("private-architecture", 1),
				nil,
			)
			request.SetPathValue("slug", "private-architecture")
			request.SetPathValue("version", "1")

			app.architectureProjectCoverHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Error("failed cover response is not explicitly no-store")
			}
			if strings.Contains(recorder.Body.String(), unsafeDetail) {
				t.Error("cover failure exposed repository diagnostics")
			}
			if calls := reader.coverCallSnapshot(); len(calls) != test.wantCalls {
				t.Errorf("cover calls: got %d, want %d", len(calls), test.wantCalls)
			} else if len(calls) == 1 && !calls[0].HasDeadline {
				t.Error("cover dependency call has no deadline")
			}
		})
	}
}

// TestArchitectureProjectCoverRouteKeepsExactBoundary verifies extra path segments
// and unsupported mutations are rejected by ServeMux before any media lookup.
func TestArchitectureProjectCoverRouteKeepsExactBoundary(t *testing.T) {
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
		{name: "extra segment", method: http.MethodGet, path: "/architecture-design/stone-room/cover/1/extra", wantStatus: http.StatusNotFound},
		{name: "unsupported post", method: http.MethodPost, path: "/architecture-design/stone-room/cover/1", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingArchitectureProjectCatalogueReader()
			app := newTestApplication(t)
			app.architectureProjects = reader
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
		})
	}
}
