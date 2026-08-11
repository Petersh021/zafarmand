package main

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// adminProductCoverMultipartPart describes one deterministic text or file part
// for strict multipart request tests.
type adminProductCoverMultipartPart struct {
	// Name is the form-data control name.
	Name string
	// Filename is nonempty only when the part should be a browser file control.
	Filename string
	// Content contains the exact untrusted bytes for this part.
	Content []byte
	// TransferEncoding optionally adds a raw coding header for rejection tests.
	TransferEncoding string
}

// adminProductCoverMultipartRequest creates a browser-like multipart POST with
// the supplied ordered parts and authenticated cookies.
func adminProductCoverMultipartRequest(
	t *testing.T,
	path string,
	parts []adminProductCoverMultipartPart,
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
			t.Fatalf("create multipart part %q: %v", part.Name, err)
		}
		if _, err := destination.Write(part.Content); err != nil {
			t.Fatalf("write multipart part %q: %v", part.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart fixture: %v", err)
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

// validAdminProductCoverMultipartParts returns the exact five controls emitted
// by product-cover-form.html.
func validAdminProductCoverMultipartParts(
	t *testing.T,
	csrfToken string,
	version string,
) []adminProductCoverMultipartPart {
	t.Helper()

	return []adminProductCoverMultipartPart{
		{Name: "csrf_token", Content: []byte(csrfToken)},
		{Name: "version", Content: []byte(version)},
		{Name: "alt_text", Content: []byte("A geometric chair study in warm light")},
		{Name: "caption", Content: []byte("Synthetic Stage 21 cover.")},
		{Name: "image", Filename: "synthetic-cover.png", Content: testProductCoverPNG(t)},
	}
}

// TestAdminProductCoverRoutesRequireAuthentication verifies that form, upload,
// and protected binary routes authenticate before revealing Product state.
func TestAdminProductCoverRoutesRequireAuthentication(t *testing.T) {
	for _, test := range []struct {
		// method and path select one Stage 21 endpoint.
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/admin/products/1/cover"},
		{method: http.MethodPost, path: "/admin/products/1/cover"},
		{method: http.MethodGet, path: "/admin/products/1/cover/1"},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			app := newTestApplication(t)
			request := adminHTTPNewRequest(test.method, test.path, nil, false)
			response := stage16ServeAdminRequest(t, app, request)
			if response.StatusCode != http.StatusSeeOther ||
				response.Header.Get("Location") != "/admin/login" {
				t.Errorf("response: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminProductCoverFormRendersTrustedContract covers both current roles,
// existing metadata, exact CSRF/revision fields, and read-only GET behavior.
func TestAdminProductCoverFormRendersTrustedContract(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			reader := newRecordingAdminProductReader()
			products := reader.products
			products[1].Cover = &productCoverMetadata{
				Version: 3,
				Width:   1600,
				Height:  1200,
				AltText: "A published lamp on a stone plinth",
				Caption: "Reviewed synthetic caption.",
			}
			reader.setProducts(products)
			fixture.app.adminProducts = reader
			writer := newRecordingAdminProductWriter()
			fixture.app.adminProductWrites = writer

			request := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/products/1/cover",
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)
			assertStage16AdminStatus(t, response, http.StatusOK)
			assertStage16BodyContains(
				t,
				response.Body,
				"Manage cover for Stage 19 Published Lamp",
				`action="/admin/products/1/cover"`,
				`enctype="multipart/form-data"`,
				`name="image"`,
				`accept="image/jpeg,image/png"`,
				`src="/admin/products/1/cover/3"`,
				"A published lamp on a stone plinth",
				"Replace cover image",
			)
			if token := adminHTTPHiddenValue(t, response.Body, "csrf_token"); token != fixture.csrfToken {
				t.Error("cover form does not carry the session CSRF token")
			}
			if version := adminHTTPHiddenValue(t, response.Body, "version"); version != "2" {
				t.Errorf("Product revision: got %q, want 2", version)
			}
			if len(writer.coverCallSnapshot()) != 0 {
				t.Error("cover form GET mutated media")
			}
		})
	}
}

// TestAdminProductCoverUploadAcceptsReviewedImage verifies decoder-derived
// writer input, bounded context, exact revisions, and canonical PRG redirect.
func TestAdminProductCoverUploadAcceptsReviewedImage(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminProductWriter()
	writer.setCoverOutcome(
		adminProductCoverWriteResult{
			ProductID:      1,
			ProductVersion: 3,
			CoverVersion:   1,
		},
		nil,
	)
	fixture.app.adminProductWrites = writer
	request := adminProductCoverMultipartRequest(
		t,
		"/admin/products/1/cover",
		validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2"),
		fixture.cookies()...,
	)

	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/admin/products/1" {
		t.Errorf("Location: got %q, want Product detail", location)
	}
	calls := writer.coverCallSnapshot()
	if len(calls) != 1 {
		t.Fatalf("cover writer calls: got %d, want 1", len(calls))
	}
	call := calls[0]
	if call.ID != 1 || call.ExpectedVersion != 2 || !call.HasDeadline ||
		call.Input.ContentType != productCoverPNGContentType ||
		call.Input.Width != 4 || call.Input.Height != 3 ||
		call.Input.AltText != "A geometric chair study in warm light" ||
		call.Input.Caption != "Synthetic Stage 21 cover." ||
		!isValidAdminProductCoverWriteInput(call.Input) {
		t.Errorf("cover writer call: got %#v", call)
	}
}

// TestAdminProductCoverValidationIsEscaped verifies 422 rendering, safe value
// restoration, file-name omission, and zero mutation on semantic errors.
func TestAdminProductCoverValidationIsEscaped(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	parts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")
	parts[2].Content = []byte(`<script>alert("cover")</script>`)
	parts[3].Content = []byte(" bad caption ")
	parts[4].Filename = "private-visitor-filename.png"
	parts[4].Content = []byte("not an image")

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminProductCoverMultipartRequest(
			t,
			"/admin/products/1/cover",
			parts,
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusUnprocessableEntity)
	assertStage16BodyContains(
		t,
		response.Body,
		"Check the cover fields",
		"Choose a complete JPEG or PNG",
		"remove leading or trailing whitespace",
		`&lt;script&gt;alert(&#34;cover&#34;)&lt;/script&gt;`,
	)
	assertStage16BodyOmits(
		t,
		response.Body,
		"private-visitor-filename.png",
		"not an image",
	)
	if len(writer.coverCallSnapshot()) != 0 {
		t.Error("invalid cover reached writer")
	}
}

// TestAdminProductCoverRequiresAltText proves the semantic upload boundary is
// enforced by the HTTP form before the writer receives otherwise valid bytes.
func TestAdminProductCoverRequiresAltText(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	parts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")
	parts[2].Content = nil

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminProductCoverMultipartRequest(
			t,
			"/admin/products/1/cover",
			parts,
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusUnprocessableEntity)
	assertStage16BodyContains(
		t,
		response.Body,
		"Enter trimmed alternative text between 1 and 300 characters.",
	)
	if len(writer.coverCallSnapshot()) != 0 {
		t.Error("cover without alt text reached writer")
	}
}

// TestAdminProductCoverChecksCSRFBeforeProductRead verifies a forged valid-size
// upload cannot learn Product existence or reach either persistence boundary.
func TestAdminProductCoverChecksCSRFBeforeProductRead(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	reader := newRecordingAdminProductReader()
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProducts = reader
	fixture.app.adminProductWrites = writer
	wrongToken, _ := adminHTTPToken(0x7f)

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminProductCoverMultipartRequest(
			t,
			"/admin/products/999/cover",
			validAdminProductCoverMultipartParts(t, wrongToken, "1"),
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusForbidden)
	if len(reader.findCallSnapshot()) != 0 || len(writer.coverCallSnapshot()) != 0 {
		t.Error("forged cover request reached Product dependencies")
	}
}

// TestAdminProductCoverRejectsStaleRevision verifies a pre-write conflict uses
// the fresh cover-management route and never calls the writer.
func TestAdminProductCoverRejectsStaleRevision(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminProductCoverMultipartRequest(
			t,
			"/admin/products/1/cover",
			validAdminProductCoverMultipartParts(t, fixture.csrfToken, "1"),
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusConflict)
	assertStage16BodyContains(
		t,
		response.Body,
		"changed in another session",
		`href="/admin/products/1/cover"`,
		"Open latest cover form",
	)
	if len(writer.coverCallSnapshot()) != 0 {
		t.Error("known-stale cover form reached writer")
	}
}

// TestAdminProductCoverMapsWriteRaceToConflict covers the narrow interval in
// which the protected pre-read is current but another writer changes the
// Product before the atomic cover statement acquires it.
func TestAdminProductCoverMapsWriteRaceToConflict(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	writer.setCoverOutcome(
		adminProductCoverWriteResult{},
		errAdminProductWriteConflict,
	)
	fixture.app.adminProductWrites = writer

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminProductCoverMultipartRequest(
			t,
			"/admin/products/1/cover",
			validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2"),
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusConflict)
	assertStage16BodyContains(
		t,
		response.Body,
		"changed in another session",
		`href="/admin/products/1/cover"`,
		"Open latest cover form",
	)
	if calls := writer.coverCallSnapshot(); len(calls) != 1 ||
		calls[0].ExpectedVersion != 2 {
		t.Errorf("racing cover writer calls: %#v", calls)
	}
}

// TestAdminProductCoverMultipartBoundaries covers exact paths, media types,
// codings, part names/cardinality, and request limits before mutation.
func TestAdminProductCoverMultipartBoundaries(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	validParts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")

	tests := []struct {
		// name identifies the rejected transport boundary.
		name string
		// request builds that exact malformed request.
		request func() *http.Request
		// status is the expected generic response.
		status int
	}{
		{name: "query", request: func() *http.Request {
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover?return=/admin", validParts, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "encoded identity", request: func() *http.Request {
			return adminProductCoverMultipartRequest(t, "/admin/products/%31/cover", validParts, fixture.cookies()...)
		}, status: http.StatusNotFound},
		{name: "wrong media type", request: func() *http.Request {
			return adminHTTPPostFormRequest("/admin/products/1/cover", validAdminProductHTTPForm(), false, fixture.cookies()...)
		}, status: http.StatusUnsupportedMediaType},
		{name: "content coding", request: func() *http.Request {
			request := adminProductCoverMultipartRequest(t, "/admin/products/1/cover", validParts, fixture.cookies()...)
			request.Header.Set("Content-Encoding", "gzip")
			return request
		}, status: http.StatusUnsupportedMediaType},
		{name: "part transfer coding", request: func() *http.Request {
			parts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")
			parts[2].TransferEncoding = "quoted-printable"
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", parts, fixture.cookies()...)
		}, status: http.StatusUnsupportedMediaType},
		{name: "missing caption", request: func() *http.Request {
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", append(append([]adminProductCoverMultipartPart(nil), validParts[:3]...), validParts[4]), fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "duplicate alt text", request: func() *http.Request {
			parts := append([]adminProductCoverMultipartPart(nil), validParts...)
			parts = append(parts, adminProductCoverMultipartPart{Name: "alt_text", Content: []byte("duplicate")})
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", parts, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "extra field", request: func() *http.Request {
			parts := append([]adminProductCoverMultipartPart(nil), validParts...)
			parts = append(parts, adminProductCoverMultipartPart{Name: "return_to", Content: []byte("/admin")})
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", parts, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "oversized image", request: func() *http.Request {
			parts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")
			parts[4].Content = make([]byte, productCoverMaximumBytes+1)
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", parts, fixture.cookies()...)
		}, status: http.StatusRequestEntityTooLarge},
		{name: "total request cap while draining part", request: func() *http.Request {
			parts := validAdminProductCoverMultipartParts(t, fixture.csrfToken, "2")
			parts[3].Content = make([]byte, adminProductCoverRequestMaximumBytes+1)
			return adminProductCoverMultipartRequest(t, "/admin/products/1/cover", parts, fixture.cookies()...)
		}, status: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := stage16ServeAdminRequest(t, fixture.app, test.request())
			assertStage16AdminStatus(t, response, test.status)
		})
	}
	if len(writer.coverCallSnapshot()) != 0 {
		t.Error("malformed multipart request reached writer")
	}
}

// TestAdminProductCoverAssetServesNoStorePreview verifies an all-state protected
// image response, exact bytes, fixed headers, and bounded repository call.
func TestAdminProductCoverAssetServesNoStorePreview(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	reader := newRecordingAdminProductReader()
	asset := validTestProductCoverAsset(t, 1, 3)
	reader.setCover(asset, nil)
	fixture.app.adminProducts = reader

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminHTTPNewRequest(
			http.MethodGet,
			"/admin/products/1/cover/3",
			nil,
			false,
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusOK)
	if response.Header.Get("Content-Type") != productCoverPNGContentType ||
		response.Header.Get("Cache-Control") != "no-store" ||
		!strings.Contains(
			response.Header.Get("Content-Security-Policy"),
			"img-src 'self'",
		) ||
		!bytes.Equal([]byte(response.Body), asset.Content) {
		t.Errorf("protected media response headers=%v bytes=%d", response.Header, len(response.Body))
	}
	calls := reader.coverCallSnapshot()
	if len(calls) != 1 || calls[0].ProductID != 1 ||
		calls[0].Version != 3 || !calls[0].HasDeadline {
		t.Errorf("protected cover calls: %#v", calls)
	}
}

// TestPublicProductCoverServesRevalidatedPublishedAsset verifies cache headers,
// conditional GET, exact bytes, and the published-only dependency call.
func TestPublicProductCoverServesRevalidatedPublishedAsset(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingProductCatalogueReader()
	asset := validTestProductCoverAsset(t, 5, 2)
	reader.setCover(asset, nil)
	app.products = reader

	request := adminHTTPNewRequest(
		http.MethodGet,
		"/products/stage-21-chair/cover/2",
		nil,
		false,
	)
	response := stage16ServeAdminRequest(t, app, request)
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != productCoverPNGContentType ||
		response.Header.Get("Cache-Control") != "public, max-age=0, must-revalidate" ||
		response.Header.Get("ETag") == "" ||
		!bytes.Equal([]byte(response.Body), asset.Content) {
		t.Errorf("public cover response: status=%d headers=%v", response.StatusCode, response.Header)
	}

	conditional := adminHTTPNewRequest(
		http.MethodGet,
		"/products/stage-21-chair/cover/2",
		nil,
		false,
	)
	conditional.Header.Set("If-None-Match", response.Header.Get("ETag"))
	notModified := stage16ServeAdminRequest(t, app, conditional)
	if notModified.StatusCode != http.StatusNotModified || notModified.Body != "" {
		t.Errorf("conditional response: status=%d body=%q", notModified.StatusCode, notModified.Body)
	}

	calls := reader.coverCallSnapshot()
	if len(calls) != 2 {
		t.Fatalf("public cover calls: got %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.Slug != "stage-21-chair" || call.Version != 2 || !call.HasDeadline {
			t.Errorf("public cover call: %#v", call)
		}
	}
}

// TestProductCoverHTTPFailuresStayGeneric covers hidden media, canonical URL
// rejection, and redacted service failures on public and protected routes.
func TestProductCoverHTTPFailuresStayGeneric(t *testing.T) {
	t.Run("public not found", func(t *testing.T) {
		app := newTestApplication(t)
		reader := newRecordingProductCatalogueReader()
		reader.setCover(productCoverAsset{}, errProductCoverNotFound)
		app.products = reader
		response := stage16ServeAdminRequest(
			t,
			app,
			adminHTTPNewRequest(http.MethodGet, "/products/hidden-product/cover/1", nil, false),
		)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("status: got %d, want 404", response.StatusCode)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("hidden-cover cache policy: got %q, want no-store", response.Header.Get("Cache-Control"))
		}
	})

	t.Run("public invalid query", func(t *testing.T) {
		app := newTestApplication(t)
		reader := newRecordingProductCatalogueReader()
		app.products = reader
		response := stage16ServeAdminRequest(
			t,
			app,
			adminHTTPNewRequest(http.MethodGet, "/products/stage-21-chair/cover/1?download=1", nil, false),
		)
		if response.StatusCode != http.StatusBadRequest || len(reader.coverCallSnapshot()) != 0 {
			t.Errorf("query response: status=%d calls=%d", response.StatusCode, len(reader.coverCallSnapshot()))
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("invalid-cover cache policy: got %q, want no-store", response.Header.Get("Cache-Control"))
		}
	})

	t.Run("protected dependency failure", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		reader := newRecordingAdminProductReader()
		reader.setCover(productCoverAsset{}, errors.New("unsafe-database-cover-detail"))
		fixture.app.adminProducts = reader
		response := stage16ServeAdminRequest(
			t,
			fixture.app,
			adminHTTPNewRequest(http.MethodGet, "/admin/products/1/cover/1", nil, false, fixture.cookies()...),
		)
		assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
		if strings.Contains(response.Body, "unsafe-database-cover-detail") {
			t.Error("protected media error leaked dependency detail")
		}
	})
}
