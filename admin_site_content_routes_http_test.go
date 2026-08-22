package main

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
)

// adminHomepageHeroMultipartPart describes one deterministic Stage 24 text or
// file part for strict multipart boundary tests.
type adminHomepageHeroMultipartPart struct {
	// Name is the exact form-data control name.
	Name string
	// Filename is nonempty only for the one browser file control.
	Filename string
	// Content contains the exact untrusted part bytes.
	Content []byte
	// TransferEncoding optionally adds a raw coding header for rejection tests.
	TransferEncoding string
}

// adminHomepageHeroMultipartRequest builds a browser-like multipart POST with
// authenticated cookies and the supplied ordered Stage 24 parts.
func adminHomepageHeroMultipartRequest(
	t *testing.T,
	path string,
	parts []adminHomepageHeroMultipartPart,
	cookies ...*http.Cookie,
) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		var destination interface {
			Write([]byte) (int, error)
		}
		var err error
		if part.TransferEncoding != "" {
			parameters := map[string]string{"name": part.Name}
			if part.Filename != "" {
				parameters["filename"] = part.Filename
			}
			header := make(textproto.MIMEHeader)
			header.Set(
				"Content-Disposition",
				mime.FormatMediaType("form-data", parameters),
			)
			header.Set("Content-Transfer-Encoding", part.TransferEncoding)
			destination, err = writer.CreatePart(header)
		} else if part.Filename != "" {
			destination, err = writer.CreateFormFile(part.Name, part.Filename)
		} else {
			destination, err = writer.CreateFormField(part.Name)
		}
		if err != nil {
			t.Fatalf("create Homepage hero multipart part %q: %v", part.Name, err)
		}
		if _, err := destination.Write(part.Content); err != nil {
			t.Fatalf("write Homepage hero multipart part %q: %v", part.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Homepage hero multipart body: %v", err)
	}

	request := adminHTTPNewRequest(
		http.MethodPost,
		path,
		bytes.NewReader(body.Bytes()),
		false,
		cookies...,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	return request
}

// adminSiteContentRouteRequest builds one browser-like URL-encoded request with
// the fixture's authenticated cookie pair.
func adminSiteContentRouteRequest(
	fixture adminHTTPAuthenticatedFixture,
	method string,
	path string,
	values url.Values,
) *http.Request {
	var body *bytes.Reader
	if values == nil {
		body = bytes.NewReader(nil)
	} else {
		body = bytes.NewReader([]byte(values.Encode()))
	}
	request := adminHTTPNewRequest(
		method,
		path,
		body,
		false,
		fixture.cookies()...,
	)
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return request
}

// validAdminHomepageHeroMultipartParts returns the exact four controls emitted
// by the managed-hero template.
func validAdminHomepageHeroMultipartParts(
	t *testing.T,
	csrfToken string,
	version string,
) []adminHomepageHeroMultipartPart {
	t.Helper()

	return []adminHomepageHeroMultipartPart{
		{Name: "csrf_token", Content: []byte(csrfToken)},
		{Name: "version", Content: []byte(version)},
		{Name: "alt_text", Content: []byte("A fictional managed Homepage hero")},
		{Name: "image", Filename: "fictional-hero.png", Content: testAdminHomepageHeroPNG(t)},
	}
}

// TestAdminSiteContentRoutesRequireAuthentication verifies every Stage 24 route
// authenticates before reading form bodies or revealing protected settings.
func TestAdminSiteContentRoutesRequireAuthentication(t *testing.T) {
	tests := []struct {
		// method is the route's supported browser method.
		method string
		// path is the exact protected Stage 24 route under test.
		path string
	}{
		{http.MethodGet, adminSiteContentNavigationPath},
		{http.MethodGet, adminHomepageContentPath},
		{http.MethodGet, adminHomepageContentEditPath},
		{http.MethodPost, adminHomepageContentPath},
		{http.MethodGet, adminHomepageHeroPath},
		{http.MethodPost, adminHomepageHeroPath},
		{http.MethodGet, adminHomepageHeroAssetPath(1)},
		{http.MethodGet, adminContactContentPath},
		{http.MethodGet, adminContactContentEditPath},
		{http.MethodPost, adminContactContentPath},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			app := newTestApplication(t)
			response := stage16ServeAdminRequest(
				t,
				app,
				adminHTTPNewRequest(test.method, test.path, nil, false),
			)
			if response.StatusCode != http.StatusSeeOther ||
				response.Header.Get("Location") != "/admin/login" {
				t.Errorf("response: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminSiteContentRoutesAllowCurrentRoles verifies Owner and Editor can
// reach every protected GET representation through the real route graph.
func TestAdminSiteContentRoutesAllowCurrentRoles(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			reader := newRecordingAdminSiteContentReader()
			reader.heroResult = validTestHomepageHeroAsset(t, 2)
			fixture.app.adminSiteContent = reader
			writer := newRecordingAdminSiteContentWriter()
			writer.contactResult = adminSiteContentWriteResult{Version: 2}
			fixture.app.adminSiteContentWrites = writer

			for _, path := range []string{
				adminSiteContentNavigationPath,
				adminHomepageContentPath,
				adminHomepageContentEditPath,
				adminHomepageHeroPath,
				adminHomepageHeroAssetPath(2),
				adminContactContentPath,
				adminContactContentEditPath,
			} {
				response := stage16ServeAdminRequest(
					t,
					fixture.app,
					adminSiteContentRouteRequest(
						fixture,
						http.MethodGet,
						path,
						nil,
					),
				)
				if response.StatusCode != http.StatusOK {
					t.Errorf("GET %s: status=%d body=%q", path, response.StatusCode, response.Body)
				}
				assertAdminHTTPSecurityHeaders(t, response.Header)
			}

			// The separate mutation allowlist deliberately grants both current
			// editorial roles and preserves the same authenticated no-store shell.
			contactValues := validAdminContactContentForm("1")
			contactValues.Set("csrf_token", fixture.csrfToken)
			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminSiteContentRouteRequest(
					fixture,
					http.MethodPost,
					adminContactContentPath,
					contactValues,
				),
			)
			if response.StatusCode != http.StatusSeeOther || writer.contactCalls != 1 {
				t.Errorf("Contact POST: status=%d calls=%d body=%q", response.StatusCode, writer.contactCalls, response.Body)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminSiteContentRolesFailClosed exercises the read and write allowlists
// with authenticated-looking future roles the real repository cannot issue.
func TestAdminSiteContentRolesFailClosed(t *testing.T) {
	for _, role := range []adminRole{"", "future-role"} {
		name := string(role)
		if name == "" {
			name = "empty role"
		}
		t.Run(name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingAdminSiteContentReader()
			writer := newRecordingAdminSiteContentWriter()
			app.adminSiteContent = reader
			app.adminSiteContentWrites = writer
			request := newAuthenticatedAdminSiteContentRequest(
				http.MethodGet,
				adminHomepageContentPath,
				bytes.NewReader(nil),
			)
			identity := authenticatedAdminRequest{
				Identity:  adminIdentity{Role: role},
				CSRFToken: adminSiteContentTestCSRF,
			}
			request = request.WithContext(context.WithValue(
				request.Context(),
				authenticatedAdminContextKey{},
				identity,
			))
			handler := requireAdminRoles(
				adminRoleOwner,
				adminRoleEditor,
			)(http.HandlerFunc(app.adminHomepageContentDetailHandler))
			response := newResponseRecorder(handler, request)
			if response.Code() != http.StatusForbidden || reader.homepageCalls != 0 {
				t.Errorf("read allowlist: status=%d calls=%d", response.Code(), reader.homepageCalls)
			}

			values := validAdminContactContentForm("1")
			request = newAdminSiteContentFormRequest(adminContactContentPath, values)
			request = request.WithContext(context.WithValue(
				request.Context(),
				authenticatedAdminContextKey{},
				identity,
			))
			handler = requireAdminRoles(
				adminRoleOwner,
				adminRoleEditor,
			)(http.HandlerFunc(app.adminContactContentUpdateHandler))
			response = newResponseRecorder(handler, request)
			if response.Code() != http.StatusForbidden || writer.contactCalls != 0 {
				t.Errorf("write allowlist: status=%d calls=%d", response.Code(), writer.contactCalls)
			}
		})
	}
}

// newResponseRecorder executes one handler and returns its recorder. Keeping it
// local avoids duplicating direct-middleware setup in role tests.
func newResponseRecorder(handler http.Handler, request *http.Request) *responseRecorder {
	recorder := &responseRecorder{header: make(http.Header), code: http.StatusOK}
	handler.ServeHTTP(recorder, request)

	return recorder
}

// responseRecorder is the minimal ResponseWriter needed by direct role tests.
// httptest.ResponseRecorder is intentionally not reused here to keep the helper
// result limited to the two asserted properties.
type responseRecorder struct {
	// header stores response fields written by the handler.
	header http.Header
	// code stores the last explicit response status.
	code int
	// body stores generic response text for ResponseWriter conformance.
	body bytes.Buffer
}

// Header returns the mutable response header map.
func (recorder *responseRecorder) Header() http.Header { return recorder.header }

// Write records response bytes and preserves the default 200 status.
func (recorder *responseRecorder) Write(content []byte) (int, error) {
	return recorder.body.Write(content)
}

// WriteHeader records the explicit response status.
func (recorder *responseRecorder) WriteHeader(status int) { recorder.code = status }

// Code returns the recorded response status for concise assertions.
func (recorder *responseRecorder) Code() int { return recorder.code }

// TestAdminSiteContentRouteMethodMatrix verifies ServeMux rejects unsupported
// methods before any protected repository operation.
func TestAdminSiteContentRouteMethodMatrix(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	reader := newRecordingAdminSiteContentReader()
	writer := newRecordingAdminSiteContentWriter()
	fixture.app.adminSiteContent = reader
	fixture.app.adminSiteContentWrites = writer

	for _, test := range []struct {
		// method is deliberately unsupported for the paired route.
		method string
		// path is the exact registered route being guarded by ServeMux.
		path string
	}{
		{http.MethodPost, adminSiteContentNavigationPath},
		{http.MethodPut, adminHomepageContentPath},
		{http.MethodPost, adminHomepageContentEditPath},
		{http.MethodDelete, adminHomepageHeroPath},
		{http.MethodPost, adminHomepageHeroAssetPath(1)},
		{http.MethodPut, adminContactContentPath},
		{http.MethodPost, adminContactContentEditPath},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminSiteContentRouteRequest(fixture, test.method, test.path, nil),
			)
			if response.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status: got %d, want 405; body=%q", response.StatusCode, response.Body)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
	if reader.homepageCalls != 0 || reader.contactCalls != 0 || reader.heroCalls != 0 ||
		writer.homepageCalls != 0 || writer.contactCalls != 0 || writer.heroCalls != 0 {
		t.Error("method-rejected request reached a Stage 24 dependency")
	}
}

// TestAdminSiteContentRejectsNoncanonicalRepresentations verifies escaped path
// aliases, leading-zero media revisions, and query-bearing edit URLs fail before
// any protected read.
func TestAdminSiteContentRejectsNoncanonicalRepresentations(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	app.adminSiteContent = reader

	tests := []struct {
		// name describes the noncanonical representation.
		name string
		// target is the raw request target presented to the handler.
		target string
		// pathValue simulates the value ServeMux extracts for a media route.
		pathValue string
		// handler is the focused endpoint that must reject before reading.
		handler http.HandlerFunc
		// wantStatus is the fixed public classification for the malformed URL.
		wantStatus int
	}{
		{name: "escaped Homepage alias", target: "/admin/site-content/%68omepage", handler: app.adminHomepageContentDetailHandler, wantStatus: http.StatusNotFound},
		{name: "leading-zero hero revision", target: adminHomepageHeroPath + "/03", pathValue: "03", handler: app.adminHomepageHeroAssetHandler, wantStatus: http.StatusNotFound},
		{name: "Contact edit query", target: adminContactContentEditPath + "?preview=1", handler: app.adminContactContentEditHandler, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newAuthenticatedAdminSiteContentRequest(
				http.MethodGet,
				test.target,
				bytes.NewReader(nil),
			)
			if test.pathValue != "" {
				request.SetPathValue("version", test.pathValue)
			}
			response := &responseRecorder{header: make(http.Header), code: http.StatusOK}
			test.handler(response, request)
			if response.Code() != test.wantStatus {
				t.Errorf("status: got %d, want %d", response.Code(), test.wantStatus)
			}
		})
	}
	if reader.homepageCalls != 0 || reader.contactCalls != 0 || reader.heroCalls != 0 {
		t.Error("noncanonical representation reached the protected reader")
	}
}

// TestAdminHomepageFormBoundaryMatrix verifies canonical URLs, media type,
// coding, byte cap, exact cardinality, and CSRF all fail before a mutation.
func TestAdminHomepageFormBoundaryMatrix(t *testing.T) {
	app := newTestApplication(t)
	writer := newRecordingAdminSiteContentWriter()
	app.adminSiteContentWrites = writer

	tests := []struct {
		// name describes the protocol boundary being violated.
		name string
		// target is the request target, including any forbidden query.
		target string
		// values supplies a normal URL-encoded form when rawBody is empty.
		values url.Values
		// mediaType overrides the normal browser form media type.
		mediaType string
		// coding adds an unsupported request content coding.
		coding string
		// rawBody supplies a deliberately malformed or oversized body.
		rawBody string
		// wantStatus is the safe response classification.
		wantStatus int
	}{
		{name: "query", target: adminHomepageContentPath + "?preview=1", values: validAdminHomepageContentForm("1"), wantStatus: http.StatusBadRequest},
		{name: "wrong media", target: adminHomepageContentPath, values: validAdminHomepageContentForm("1"), mediaType: "application/json", wantStatus: http.StatusUnsupportedMediaType},
		{name: "content coding", target: adminHomepageContentPath, values: validAdminHomepageContentForm("1"), coding: "gzip", wantStatus: http.StatusUnsupportedMediaType},
		{name: "missing field", target: adminHomepageContentPath, values: func() url.Values {
			values := validAdminHomepageContentForm("1")
			values.Del("descriptor")
			return values
		}(), wantStatus: http.StatusBadRequest},
		{name: "unknown field", target: adminHomepageContentPath, values: func() url.Values {
			values := validAdminHomepageContentForm("1")
			values.Set("future", "value")
			return values
		}(), wantStatus: http.StatusBadRequest},
		{name: "duplicate field", target: adminHomepageContentPath, values: func() url.Values {
			values := validAdminHomepageContentForm("1")
			values.Add("studio_name", "ambiguous")
			return values
		}(), wantStatus: http.StatusBadRequest},
		{name: "bad CSRF", target: adminHomepageContentPath, values: func() url.Values {
			values := validAdminHomepageContentForm("1")
			values.Set("csrf_token", "bad")
			return values
		}(), wantStatus: http.StatusForbidden},
		{name: "too large", target: adminHomepageContentPath, mediaType: "application/x-www-form-urlencoded", rawBody: "csrf_token=" + strings.Repeat("x", adminSiteContentFormMaximumBytes), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := test.rawBody
			if test.values != nil {
				body = test.values.Encode()
			}
			request := newAuthenticatedAdminSiteContentRequest(
				http.MethodPost,
				test.target,
				bytes.NewReader([]byte(body)),
			)
			mediaType := test.mediaType
			if mediaType == "" {
				mediaType = "application/x-www-form-urlencoded"
			}
			request.Header.Set("Content-Type", mediaType)
			if test.coding != "" {
				request.Header.Set("Content-Encoding", test.coding)
			}
			response := &responseRecorder{header: make(http.Header), code: http.StatusOK}
			app.adminHomepageContentUpdateHandler(response, request)
			if response.Code() != test.wantStatus {
				t.Errorf("status: got %d, want %d; body=%q", response.Code(), test.wantStatus, response.body.String())
			}
		})
	}
	if writer.homepageCalls != 0 {
		t.Errorf("boundary failures reached writer %d times", writer.homepageCalls)
	}
}

// TestAdminHomepageWriterOutcomeMatrix verifies transactional unavailability,
// conflict, generic failure, and invalid result mapping at the HTTP boundary.
func TestAdminHomepageWriterOutcomeMatrix(t *testing.T) {
	candidate := adminHomepageFeatureCandidate{
		Discipline:     homepageFeatureInterior,
		ID:             9,
		Slug:           "eligible-interior",
		Title:          "Eligible Interior",
		Classification: "Residential",
		SortOrder:      1,
		CoverVersion:   1,
	}
	tests := []struct {
		// name identifies the writer outcome being mapped.
		name string
		// writerErr is the controlled persistence result.
		writerErr error
		// result is the controlled success coordinate when no error is returned.
		result adminSiteContentWriteResult
		// wantStatus is the fixed HTTP classification.
		wantStatus int
		// marker is application-owned response copy proving the mapping.
		marker string
	}{
		{name: "conflict", writerErr: errAdminSiteContentWriteConflict, wantStatus: http.StatusConflict, marker: "Homepage content changed"},
		{name: "unavailable", writerErr: errAdminHomepageInteriorFeatureUnavailable, wantStatus: http.StatusUnprocessableEntity, marker: "Published Interior project"},
		{name: "dependency", writerErr: errors.New("private dependency detail"), wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
		{name: "invalid result", result: adminSiteContentWriteResult{Version: 99}, wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingAdminSiteContentReader()
			reader.candidateResult = []adminHomepageFeatureCandidate{candidate}
			writer := newRecordingAdminSiteContentWriter()
			writer.homepageError = test.writerErr
			writer.homepageResult = test.result
			app.adminSiteContent = reader
			app.adminSiteContentWrites = writer
			values := validAdminHomepageContentForm("1")
			values.Set("featured_interior_project_id", "9")
			response := &responseRecorder{header: make(http.Header), code: http.StatusOK}
			app.adminHomepageContentUpdateHandler(
				response,
				newAdminSiteContentFormRequest(adminHomepageContentPath, values),
			)
			if response.Code() != test.wantStatus || !strings.Contains(response.body.String(), test.marker) || writer.homepageCalls != 1 {
				t.Fatalf("response: status=%d calls=%d body=%q", response.Code(), writer.homepageCalls, response.body.String())
			}
			if strings.Contains(response.body.String(), "private dependency detail") {
				t.Error("response exposes dependency detail")
			}
		})
	}
}

// TestAdminContactWriterOutcomeMatrix verifies stale and operational Contact
// outcomes remain fixed, non-echoing, and free of dependency diagnostics.
func TestAdminContactWriterOutcomeMatrix(t *testing.T) {
	for _, test := range []struct {
		// name identifies the Contact writer outcome being mapped.
		name string
		// writerErr is the controlled persistence result.
		writerErr error
		// result is the controlled success coordinate when no error is returned.
		result adminSiteContentWriteResult
		// wantStatus is the fixed HTTP classification.
		wantStatus int
		// marker is application-owned response copy proving the mapping.
		marker string
	}{
		{name: "conflict", writerErr: errAdminSiteContentWriteConflict, wantStatus: http.StatusConflict, marker: "Contact content changed"},
		{name: "dependency", writerErr: errors.New("private Contact dependency"), wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
		{name: "invalid result", result: adminSiteContentWriteResult{Version: 99}, wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			writer := newRecordingAdminSiteContentWriter()
			writer.contactError = test.writerErr
			writer.contactResult = test.result
			app.adminSiteContentWrites = writer
			values := validAdminContactContentForm("1")
			secretSubmittedCopy := "PRIVATE-SUBMITTED-CONTACT-COPY"
			values.Set("introduction", secretSubmittedCopy)
			response := &responseRecorder{header: make(http.Header), code: http.StatusOK}
			app.adminContactContentUpdateHandler(
				response,
				newAdminSiteContentFormRequest(adminContactContentPath, values),
			)
			if response.Code() != test.wantStatus ||
				!strings.Contains(response.body.String(), test.marker) ||
				writer.contactCalls != 1 {
				t.Fatalf("response: status=%d calls=%d body=%q", response.Code(), writer.contactCalls, response.body.String())
			}
			if strings.Contains(response.body.String(), secretSubmittedCopy) ||
				strings.Contains(response.body.String(), "private Contact dependency") {
				t.Error("Contact failure response exposes submitted or dependency detail")
			}
		})
	}
}

// TestAdminContactRouteNormalizesBrowserTextareaLineEndings proves the routed
// browser protocol boundary converts CRLF textarea copy to canonical LF before
// persistence, while a malformed lone carriage return remains a safe 422 and
// never reaches the writer.
func TestAdminContactRouteNormalizesBrowserTextareaLineEndings(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminSiteContentWriter()
	writer.contactResult = adminSiteContentWriteResult{Version: 2}
	fixture.app.adminSiteContentWrites = writer

	values := validAdminContactContentForm("1")
	values.Set("csrf_token", fixture.csrfToken)
	values.Set("introduction", "First paragraph.\r\nSecond paragraph.")
	values.Set("address", "Studio 4\r\nTehran")
	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminSiteContentRouteRequest(
			fixture,
			http.MethodPost,
			adminContactContentPath,
			values,
		),
	)
	if response.StatusCode != http.StatusSeeOther || writer.contactCalls != 1 {
		t.Fatalf("CRLF response: status=%d calls=%d body=%q", response.StatusCode, writer.contactCalls, response.Body)
	}
	if writer.contactInput.Introduction != "First paragraph.\nSecond paragraph." ||
		writer.contactInput.Address != "Studio 4\nTehran" {
		t.Errorf("normalized Contact input: %#v", writer.contactInput)
	}

	values = validAdminContactContentForm("1")
	values.Set("csrf_token", fixture.csrfToken)
	values.Set("introduction", "First paragraph.\rSecond paragraph.")
	response = stage16ServeAdminRequest(
		t,
		fixture.app,
		adminSiteContentRouteRequest(
			fixture,
			http.MethodPost,
			adminContactContentPath,
			values,
		),
	)
	if response.StatusCode != http.StatusUnprocessableEntity ||
		writer.contactCalls != 1 {
		t.Errorf("lone-CR response: status=%d calls=%d body=%q", response.StatusCode, writer.contactCalls, response.Body)
	}
}

// TestAdminHomepageHeroMultipartBoundaryMatrix verifies exact multipart names,
// cardinality, codings, CSRF order, byte cap, and semantic image validation.
func TestAdminHomepageHeroMultipartBoundaryMatrix(t *testing.T) {
	for _, test := range []struct {
		// name identifies the strict multipart boundary being exercised.
		name string
		// parts builds the exact ordered multipart controls for this case.
		parts func(*testing.T, string) []adminHomepageHeroMultipartPart
		// coding adds an unsupported whole-request content coding.
		coding string
		// wantStatus is the safe response classification.
		wantStatus int
	}{
		{name: "missing image", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			return validAdminHomepageHeroMultipartParts(t, csrf, "1")[:3]
		}, wantStatus: http.StatusBadRequest},
		{name: "unknown field", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			return append(parts, adminHomepageHeroMultipartPart{Name: "caption", Content: []byte("not supported")})
		}, wantStatus: http.StatusBadRequest},
		{name: "duplicate alt", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			return append(parts, adminHomepageHeroMultipartPart{Name: "alt_text", Content: []byte("ambiguous")})
		}, wantStatus: http.StatusBadRequest},
		{name: "transfer coding", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			parts[2].TransferEncoding = "base64"
			return parts
		}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "bad CSRF", parts: func(t *testing.T, _ string) []adminHomepageHeroMultipartPart {
			return validAdminHomepageHeroMultipartParts(t, "bad", "1")
		}, wantStatus: http.StatusForbidden},
		{name: "invalid image", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			parts[3].Content = []byte("not an image")
			return parts
		}, wantStatus: http.StatusUnprocessableEntity},
		{name: "oversized image", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			parts[3].Content = bytes.Repeat([]byte{0x41}, reviewedCoverMaximumBytes+1)
			return parts
		}, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "oversized total envelope", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			parts := validAdminHomepageHeroMultipartParts(t, csrf, "1")
			// Exceeding the whole-request cap forces the file part's Close drain
			// through http.MaxBytesReader after the bounded file read completes.
			parts[3].Content = bytes.Repeat(
				[]byte{0x41},
				adminHomepageHeroRequestMaximumBytes+1,
			)
			return parts
		}, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "request coding", parts: func(t *testing.T, csrf string) []adminHomepageHeroMultipartPart {
			return validAdminHomepageHeroMultipartParts(t, csrf, "1")
		}, coding: "gzip", wantStatus: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			reader := newRecordingAdminSiteContentReader()
			writer := newRecordingAdminSiteContentWriter()
			fixture.app.adminSiteContent = reader
			fixture.app.adminSiteContentWrites = writer
			request := adminHomepageHeroMultipartRequest(
				t,
				adminHomepageHeroPath,
				test.parts(t, fixture.csrfToken),
				fixture.cookies()...,
			)
			if test.coding != "" {
				request.Header.Set("Content-Encoding", test.coding)
			}
			response := stage16ServeAdminRequest(t, fixture.app, request)
			if response.StatusCode != test.wantStatus {
				t.Errorf("status: got %d, want %d; body=%q", response.StatusCode, test.wantStatus, response.Body)
			}
			if writer.heroCalls != 0 {
				t.Errorf("rejected multipart reached writer %d times", writer.heroCalls)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminHomepageHeroRejectsUnsupportedEnvelope verifies a non-multipart
// body is rejected before CSRF, image decoding, or persistence.
func TestAdminHomepageHeroRejectsUnsupportedEnvelope(t *testing.T) {
	app := newTestApplication(t)
	writer := newRecordingAdminSiteContentWriter()
	app.adminSiteContentWrites = writer
	request := newAuthenticatedAdminSiteContentRequest(
		http.MethodPost,
		adminHomepageHeroPath,
		bytes.NewReader([]byte("csrf_token=not-multipart")),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := &responseRecorder{header: make(http.Header), code: http.StatusOK}
	app.adminHomepageHeroUploadHandler(response, request)
	if response.Code() != http.StatusUnsupportedMediaType || writer.heroCalls != 0 {
		t.Errorf("response: status=%d writer calls=%d", response.Code(), writer.heroCalls)
	}
}

// TestAdminHomepageHeroWriterOutcomeMatrix verifies fixed conflict and generic
// dependency mappings never echo uploaded bytes or errors.
func TestAdminHomepageHeroWriterOutcomeMatrix(t *testing.T) {
	for _, test := range []struct {
		// name identifies the hero writer outcome being mapped.
		name string
		// writerErr is the controlled persistence result.
		writerErr error
		// result is the controlled revision pair when no error is returned.
		result adminHomepageHeroWriteResult
		// wantStatus is the fixed HTTP classification.
		wantStatus int
		// marker is application-owned response copy proving the mapping.
		marker string
	}{
		{name: "conflict", writerErr: errAdminSiteContentWriteConflict, wantStatus: http.StatusConflict, marker: "Homepage hero changed"},
		{name: "dependency", writerErr: errors.New("private hero dependency"), wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
		{name: "invalid result", result: adminHomepageHeroWriteResult{HomepageVersion: 2}, wantStatus: http.StatusServiceUnavailable, marker: "service temporarily unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			reader := newRecordingAdminSiteContentReader()
			writer := newRecordingAdminSiteContentWriter()
			writer.heroError = test.writerErr
			writer.heroResult = test.result
			fixture.app.adminSiteContent = reader
			fixture.app.adminSiteContentWrites = writer
			request := adminHomepageHeroMultipartRequest(
				t,
				adminHomepageHeroPath,
				validAdminHomepageHeroMultipartParts(t, fixture.csrfToken, "1"),
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)
			if response.StatusCode != test.wantStatus || !strings.Contains(response.Body, test.marker) || writer.heroCalls != 1 {
				t.Fatalf("response: status=%d calls=%d body=%q", response.StatusCode, writer.heroCalls, response.Body)
			}
			if strings.Contains(response.Body, "private hero dependency") {
				t.Error("response exposes dependency detail")
			}
		})
	}
}
