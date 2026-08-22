package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestHomepageHeroHandlerServesRevalidatedCurrentAsset verifies exact bytes,
// security/cache headers, conditional GET, and a bounded dependency read.
func TestHomepageHeroHandlerServesRevalidatedCurrentAsset(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	asset := validTestHomepageHeroAsset(t, 5)
	reader.setHero(asset, nil)
	app.siteContent = reader

	request := httptest.NewRequest(
		http.MethodGet,
		homepageHeroPath(asset.Version),
		nil,
	)
	request.SetPathValue("version", strconv.FormatInt(asset.Version, 10))
	recorder := httptest.NewRecorder()
	app.homepageHeroHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Homepage hero status: got %d, want 200", recorder.Code)
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
		t.Error("Homepage hero response differs from exact normalized bytes")
	}

	conditional := httptest.NewRequest(
		http.MethodGet,
		homepageHeroPath(asset.Version),
		nil,
	)
	conditional.SetPathValue("version", strconv.FormatInt(asset.Version, 10))
	conditional.Header.Set("If-None-Match", "W/"+wantETag)
	notModified := httptest.NewRecorder()
	app.homepageHeroHandler(notModified, conditional)
	if notModified.Code != http.StatusNotModified ||
		notModified.Body.Len() != 0 {
		t.Errorf(
			"conditional Homepage hero: status=%d body=%q",
			notModified.Code,
			notModified.Body.String(),
		)
	}

	calls := reader.callSnapshot()
	if len(calls) != 2 {
		t.Fatalf("Homepage hero calls: got %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.Operation != "hero" || call.Version != asset.Version ||
			!call.HasDeadline {
			t.Errorf("Homepage hero call: %#v", call)
		}
	}
}

// TestHomepageHeroHandlerRejectsNonCanonicalRequests verifies path and query
// validation happens before the repository and every rejection remains no-store.
func TestHomepageHeroHandlerRejectsNonCanonicalRequests(t *testing.T) {
	tests := []struct {
		// name identifies the malformed coordinate.
		name string
		// target is the complete request target.
		target string
		// pathValue is the decoded ServeMux wildcard supplied to the handler.
		pathValue string
		// wantStatus distinguishes malformed paths from unsupported queries.
		wantStatus int
	}{
		{name: "leading zero", target: "/homepage/hero/01", pathValue: "01", wantStatus: http.StatusNotFound},
		{name: "zero", target: "/homepage/hero/0", pathValue: "0", wantStatus: http.StatusNotFound},
		{name: "escaped digit", target: "/homepage/hero/%31", pathValue: "1", wantStatus: http.StatusNotFound},
		{name: "query", target: "/homepage/hero/1?download=1", pathValue: "1", wantStatus: http.StatusBadRequest},
		{name: "empty query marker", target: "/homepage/hero/1?", pathValue: "1", wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingSiteContentReader()
			app.siteContent = reader
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.SetPathValue("version", test.pathValue)
			recorder := httptest.NewRecorder()

			app.homepageHeroHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("cache policy: got %q, want no-store", got)
			}
			if calls := reader.callSnapshot(); len(calls) != 0 {
				t.Errorf("invalid request reached reader %d time(s)", len(calls))
			}
		})
	}
}

// TestHomepageHeroHandlerMapsSafeRepositoryOutcomes verifies disabled/stale
// media is 404 while operational or malformed results become a redacted 503.
func TestHomepageHeroHandlerMapsSafeRepositoryOutcomes(t *testing.T) {
	privateDetail := "unsafe hero database detail password=secret"
	tests := []struct {
		// name identifies one repository outcome.
		name string
		// arrange configures the public reader.
		arrange func(*recordingSiteContentReader)
		// wantStatus is the public response category.
		wantStatus int
	}{
		{
			name: "disabled missing or stale",
			arrange: func(reader *recordingSiteContentReader) {
				reader.setHero(homepageHeroAsset{}, errHomepageHeroNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "database failure",
			arrange: func(reader *recordingSiteContentReader) {
				reader.setHero(homepageHeroAsset{}, errors.New(privateDetail))
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "malformed substituted asset",
			arrange: func(reader *recordingSiteContentReader) {
				asset := validTestHomepageHeroAsset(t, 3)
				asset.ByteSize++
				reader.setHero(asset, nil)
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingSiteContentReader()
			test.arrange(reader)
			app.siteContent = reader
			request := httptest.NewRequest(
				http.MethodGet,
				homepageHeroPath(3),
				nil,
			)
			request.SetPathValue("version", "3")
			recorder := httptest.NewRecorder()

			app.homepageHeroHandler(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("failure cache policy: got %q, want no-store", got)
			}
			if strings.Contains(recorder.Body.String(), privateDetail) ||
				strings.Contains(recorder.Body.String(), "password=secret") {
				t.Error("Homepage hero response exposes repository diagnostics")
			}
			calls := reader.callSnapshot()
			if len(calls) != 1 || !calls[0].HasDeadline {
				t.Errorf("Homepage hero calls: %#v", calls)
			}
		})
	}
}
