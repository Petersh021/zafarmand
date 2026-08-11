package main

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// adminInteriorProjectCoverMultipartPart describes one deterministic raw
// browser part without depending on Product-specific HTTP test support.
type adminInteriorProjectCoverMultipartPart struct {
	// Name is the exact multipart control name.
	Name string
	// Filename marks the single browser file part when nonempty.
	Filename string
	// Content contains the exact untrusted bytes presented to the parser.
	Content []byte
	// TransferEncoding optionally adds a raw coding header for rejection tests.
	TransferEncoding string
}

// adminInteriorProjectCoverMultipartRequest builds one browser-like multipart
// POST while preserving part order and any deliberately unsafe raw header.
func adminInteriorProjectCoverMultipartRequest(
	t *testing.T,
	path string,
	parts []adminInteriorProjectCoverMultipartPart,
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
			t.Fatalf("create Interior cover multipart part %q: %v", part.Name, err)
		}
		if _, err := destination.Write(part.Content); err != nil {
			t.Fatalf("write Interior cover multipart part %q: %v", part.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Interior cover multipart fixture: %v", err)
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

// validAdminInteriorProjectCoverMultipartParts mirrors the exact five controls
// emitted by the native Interior cover form. All content is synthetic.
func validAdminInteriorProjectCoverMultipartParts(
	t *testing.T,
	csrfToken string,
	projectVersion string,
) []adminInteriorProjectCoverMultipartPart {
	t.Helper()

	return []adminInteriorProjectCoverMultipartPart{
		{Name: "csrf_token", Content: []byte(csrfToken)},
		{Name: "version", Content: []byte(projectVersion)},
		{Name: "alt_text", Content: []byte("Warm daylight crossing a fictional residential atrium")},
		{Name: "caption", Content: []byte("Synthetic Stage 22 cover.")},
		{Name: "image", Filename: "synthetic-interior.png", Content: testAdminInteriorProjectCoverPNG(t)},
	}
}

// TestAdminInteriorProjectCoverRoutesRequireAuthentication ensures the form,
// upload, and exact protected asset all authenticate before repository access.
func TestAdminInteriorProjectCoverRoutesRequireAuthentication(t *testing.T) {
	for _, test := range []struct {
		// method is the exact route method exercised while signed out.
		method string
		// path is the canonical protected form, upload, or asset address.
		path string
	}{
		{method: http.MethodGet, path: adminInteriorProjectCoverPath(3)},
		{method: http.MethodPost, path: adminInteriorProjectCoverPath(3)},
		{method: http.MethodGet, path: adminInteriorProjectCoverAssetPath(3, 1)},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			app := newTestApplication(t)
			reader := configuredAdminInteriorProjectReader()
			writer := newRecordingAdminInteriorProjectWriter()
			app.adminInteriorProjects = reader
			app.adminInteriorProjectWrites = writer
			response := stage16ServeAdminRequest(
				t,
				app,
				adminHTTPNewRequest(test.method, test.path, nil, false),
			)
			assertStage16AdminStatus(t, response, http.StatusSeeOther)
			if response.Header.Get("Location") != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", response.Header.Get("Location"))
			}
			if reader.findCalls != 0 || reader.coverCalls != 0 || writer.coverCalls != 0 {
				t.Error("signed-out cover route reached Interior persistence")
			}
		})
	}
}

// TestAdminInteriorProjectCoverFormRendersCurrentContract covers both writer
// roles, protected preview metadata, CSRF, revision, and read-only GET behavior.
func TestAdminInteriorProjectCoverFormRendersCurrentContract(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			reader := configuredAdminInteriorProjectReader()
			reader.findResult.Cover = &interiorProjectCoverMetadata{
				Version: 2,
				Width:   1600,
				Height:  1000,
				AltText: "A fictional courtyard office with filtered light",
				Caption: "Reviewed synthetic caption.",
			}
			writer := newRecordingAdminInteriorProjectWriter()
			fixture.app.adminInteriorProjects = reader
			fixture.app.adminInteriorProjectWrites = writer

			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(http.MethodGet, adminInteriorProjectCoverPath(3), nil, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, response, http.StatusOK)
			assertStage16BodyContains(
				t,
				response.Body,
				"Manage cover for Courtyard Office",
				`action="/admin/interior-projects/3/cover"`,
				`enctype="multipart/form-data"`,
				`name="image"`,
				`accept="image/jpeg,image/png"`,
				`src="/admin/interior-projects/3/cover/2"`,
				"Replace cover image",
			)
			if adminHTTPHiddenValue(t, response.Body, "csrf_token") != fixture.csrfToken ||
				adminHTTPHiddenValue(t, response.Body, "version") != "3" {
				t.Error("cover form omitted its session CSRF or current project revision")
			}
			if writer.coverCalls != 0 {
				t.Error("cover form GET mutated media")
			}
		})
	}
}

// TestAdminInteriorProjectCoverUploadAcceptsReviewedImage verifies generic
// media normalization, exact optimistic coordinates, deadline, and PRG.
func TestAdminInteriorProjectCoverUploadAcceptsReviewedImage(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	reader := configuredAdminInteriorProjectReader()
	writer := newRecordingAdminInteriorProjectWriter()
	writer.coverResult = adminInteriorProjectCoverWriteResult{
		ProjectID:      3,
		ProjectVersion: 4,
		CoverVersion:   1,
	}
	fixture.app.adminInteriorProjects = reader
	fixture.app.adminInteriorProjectWrites = writer

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminInteriorProjectCoverMultipartRequest(
			t,
			adminInteriorProjectCoverPath(3),
			validAdminInteriorProjectCoverMultipartParts(t, fixture.csrfToken, "3"),
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if response.Header.Get("Location") != adminInteriorProjectPath(3) {
		t.Errorf("Location: got %q, want Interior detail", response.Header.Get("Location"))
	}
	if writer.coverCalls != 1 || writer.coverID != 3 || writer.coverExpectedVersion != 3 {
		t.Fatalf("cover call: count=%d id=%d version=%d", writer.coverCalls, writer.coverID, writer.coverExpectedVersion)
	}
	if writer.coverInput.ContentType != reviewedCoverPNGContentType ||
		writer.coverInput.Width != 3 || writer.coverInput.Height != 2 ||
		writer.coverInput.AltText != "Warm daylight crossing a fictional residential atrium" ||
		!isValidAdminInteriorProjectCoverWriteInput(writer.coverInput) {
		t.Errorf("normalized cover input: %#v", writer.coverInput)
	}
	if _, ok := writer.coverContext.Deadline(); !ok {
		t.Error("cover repository context has no deadline")
	}
}

// TestAdminInteriorProjectCoverUploadRejectsUntrustedMultipart protects exact
// cardinality, CSRF, transfer coding, image validation, escaped text, and zero
// mutation for every rejected browser request.
func TestAdminInteriorProjectCoverUploadRejectsUntrustedMultipart(t *testing.T) {
	for _, test := range []struct {
		// name identifies the isolated malformed or unsafe input boundary.
		name string
		// change applies one deliberate mutation to an otherwise valid form.
		change func([]adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart
		// wantStatus is the safe HTTP classification for that rejection.
		wantStatus int
	}{
		{name: "duplicate", wantStatus: http.StatusBadRequest, change: func(parts []adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart {
			return append(parts, parts[2])
		}},
		{name: "unknown", wantStatus: http.StatusBadRequest, change: func(parts []adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart {
			parts[2].Name = "future_alt"
			return parts
		}},
		{name: "transfer encoding", wantStatus: http.StatusUnsupportedMediaType, change: func(parts []adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart {
			parts[3].TransferEncoding = "base64"
			return parts
		}},
		{name: "wrong CSRF", wantStatus: http.StatusForbidden, change: func(parts []adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart {
			parts[0].Content = []byte("wrong")
			return parts
		}},
		{name: "invalid fields", wantStatus: http.StatusUnprocessableEntity, change: func(parts []adminInteriorProjectCoverMultipartPart) []adminInteriorProjectCoverMultipartPart {
			parts[2].Content = []byte(` <script>alert("alt")</script> `)
			parts[3].Content = []byte(" bad caption ")
			parts[4].Filename = "private-browser-name.png"
			parts[4].Content = []byte("not an image")
			return parts
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			reader := configuredAdminInteriorProjectReader()
			writer := newRecordingAdminInteriorProjectWriter()
			fixture.app.adminInteriorProjects = reader
			fixture.app.adminInteriorProjectWrites = writer
			parts := test.change(validAdminInteriorProjectCoverMultipartParts(t, fixture.csrfToken, "3"))

			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminInteriorProjectCoverMultipartRequest(t, adminInteriorProjectCoverPath(3), parts, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, response, test.wantStatus)
			if writer.coverCalls != 0 {
				t.Error("rejected multipart request reached Interior cover writer")
			}
			if test.name == "invalid fields" {
				if !strings.Contains(response.Body, `&lt;script&gt;alert(&#34;alt&#34;)&lt;/script&gt;`) ||
					strings.Contains(response.Body, `<script>alert("alt")</script>`) ||
					strings.Contains(response.Body, "private-browser-name.png") {
					t.Error("validation response did not escape reviewed text or omitted browser filename")
				}
			}
		})
	}
}

// TestAdminInteriorProjectCoverConflictUsesFixedRecovery confirms stale media
// never reaches the writer and the browser file selection is not echoed.
func TestAdminInteriorProjectCoverConflictUsesFixedRecovery(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	reader := configuredAdminInteriorProjectReader()
	writer := newRecordingAdminInteriorProjectWriter()
	fixture.app.adminInteriorProjects = reader
	fixture.app.adminInteriorProjectWrites = writer
	parts := validAdminInteriorProjectCoverMultipartParts(t, fixture.csrfToken, "2")
	parts[4].Filename = "stale-private-name.png"

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminInteriorProjectCoverMultipartRequest(t, adminInteriorProjectCoverPath(3), parts, fixture.cookies()...),
	)
	assertStage16AdminStatus(t, response, http.StatusConflict)
	assertStage16BodyContains(
		t,
		response.Body,
		"changed in another session",
		"Open latest cover form",
		`href="/admin/interior-projects/3/cover"`,
	)
	assertStage16BodyOmits(t, response.Body, "stale-private-name.png")
	if writer.coverCalls != 0 {
		t.Error("preflight revision conflict reached Interior cover writer")
	}
}

// TestAdminInteriorProjectCoverAssetServesExactProtectedRevision verifies the
// reader coordinate, validated media headers, bytes, and canonical URL policy.
func TestAdminInteriorProjectCoverAssetServesExactProtectedRevision(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	reader := configuredAdminInteriorProjectReader()
	asset := validInteriorProjectCoverAsset(t, 3, 2)
	reader.coverResult = asset
	fixture.app.adminInteriorProjects = reader

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminHTTPNewRequest(http.MethodGet, adminInteriorProjectCoverAssetPath(3, 2), nil, false, fixture.cookies()...),
	)
	assertStage16AdminStatus(t, response, http.StatusOK)
	if response.Header.Get("Content-Type") != asset.ContentType ||
		response.Header.Get("Content-Length") == "" || response.Body != string(asset.Content) {
		t.Errorf("asset response: type=%q length=%q bytes=%d", response.Header.Get("Content-Type"), response.Header.Get("Content-Length"), len(response.Body))
	}
	if reader.coverCalls != 1 || reader.coverID != 3 || reader.coverVersion != 2 {
		t.Errorf("cover read: calls=%d id=%d version=%d", reader.coverCalls, reader.coverID, reader.coverVersion)
	}
	if _, ok := reader.coverContext.Deadline(); !ok {
		t.Error("cover read context has no deadline")
	}
}
