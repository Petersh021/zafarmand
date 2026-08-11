package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// validAdminProductHTTPForm returns the exact visible controls accepted by a
// Product mutation. Tests add the session CSRF and edit version separately.
func validAdminProductHTTPForm() url.Values {
	return url.Values{
		"slug":               {"stage-20-http-chair"},
		"name":               {"Stage 20 HTTP Chair"},
		"category":           {"Furniture"},
		"sort_order":         {"7"},
		"publication_status": {productPublicationStatusDraft},
		"description":        {""},
		"material":           {""},
		"dimensions":         {""},
	}
}

// addAdminProductHTTPToken copies one canonical CSRF value into a form without
// making tests share a mutable url.Values map.
func addAdminProductHTTPToken(
	values url.Values,
	csrfToken string,
) url.Values {
	copyValues := make(url.Values, len(values)+1)
	for name, entries := range values {
		copyValues[name] = append([]string(nil), entries...)
	}
	copyValues.Set("csrf_token", csrfToken)

	return copyValues
}

// TestAdminProductWriteRoutesRequireAuthentication proves every form and POST
// route redirects a signed-out browser before revealing or mutating a Product.
func TestAdminProductWriteRoutesRequireAuthentication(t *testing.T) {
	tests := []struct {
		// method and path select one Stage 20 endpoint.
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/admin/products/new"},
		{method: http.MethodPost, path: "/admin/products"},
		{method: http.MethodGet, path: "/admin/products/1/edit"},
		{method: http.MethodPost, path: "/admin/products/1"},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			app := newTestApplication(t)
			writer := newRecordingAdminProductWriter()
			app.adminProductWrites = writer
			request := adminHTTPNewRequest(test.method, test.path, nil, false)
			response := stage16ServeAdminRequest(t, app, request)

			if response.StatusCode != http.StatusSeeOther {
				t.Errorf("status: got %d, want 303", response.StatusCode)
			}
			if location := response.Header.Get("Location"); location != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", location)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
			if len(writer.createCallSnapshot()) != 0 || len(writer.updateCallSnapshot()) != 0 {
				t.Error("signed-out request reached Product writer")
			}
		})
	}
}

// TestAdminProductFormsRenderTrustedContracts covers both current roles, exact
// actions, session CSRF, lifecycle options, and the edit revision.
func TestAdminProductFormsRenderTrustedContracts(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			writer := newRecordingAdminProductWriter()
			fixture.app.adminProductWrites = writer

			newRequest := adminHTTPNewRequest(
				http.MethodGet,
				adminProductNewPath,
				nil,
				false,
				fixture.cookies()...,
			)
			newResponse := stage16ServeAdminRequest(t, fixture.app, newRequest)
			assertStage16AdminStatus(t, newResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				newResponse.Body,
				"Create Product",
				`action="/admin/products"`,
				`name="publication_status"`,
				`value="draft"`,
				`value="published"`,
				`value="archived"`,
			)
			if token := adminHTTPHiddenValue(t, newResponse.Body, "csrf_token"); token != fixture.csrfToken {
				t.Error("new Product form does not carry the session CSRF token")
			}
			assertStage16BodyOmits(t, newResponse.Body, `name="version"`)

			editRequest := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/products/1/edit",
				nil,
				false,
				fixture.cookies()...,
			)
			editResponse := stage16ServeAdminRequest(t, fixture.app, editRequest)
			assertStage16AdminStatus(t, editResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				editResponse.Body,
				"Edit Product",
				`action="/admin/products/1"`,
				"stage19-published-lamp",
				"Catalogue revision 2",
			)
			if version := adminHTTPHiddenValue(t, editResponse.Body, "version"); version != "2" {
				t.Errorf("edit version: got %q, want 2", version)
			}
			if token := adminHTTPHiddenValue(t, editResponse.Body, "csrf_token"); token != fixture.csrfToken {
				t.Error("edit form does not carry the session CSRF token")
			}
			if len(writer.createCallSnapshot()) != 0 || len(writer.updateCallSnapshot()) != 0 {
				t.Error("GET form route mutated a Product")
			}
		})
	}
}

// TestAdminProductCreateAcceptsValidatedForm verifies the exact writer call,
// bounded context, and fixed Post/Redirect/Get destination.
func TestAdminProductCreateAcceptsValidatedForm(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminProductWriter()
	writer.setCreateOutcome(adminProductWriteResult{ID: 37, Version: 1}, nil)
	fixture.app.adminProductWrites = writer
	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	values.Set("description", "A reviewed Stage 21 chair description.")
	values.Set("material", "Oak and linen")
	values.Set("dimensions", "800 × 520 × 600 mm")

	request := adminHTTPPostFormRequest(
		adminProductCreatePath,
		values,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/admin/products/37" {
		t.Errorf("Location: got %q, want /admin/products/37", location)
	}

	calls := writer.createCallSnapshot()
	if len(calls) != 1 {
		t.Fatalf("create calls: got %d, want 1", len(calls))
	}
	wantInput := adminProductWriteInput{
		Slug:              "stage-20-http-chair",
		Name:              "Stage 20 HTTP Chair",
		Category:          "Furniture",
		SortOrder:         7,
		PublicationStatus: productPublicationStatusDraft,
		Description:       "A reviewed Stage 21 chair description.",
		Material:          "Oak and linen",
		Dimensions:        "800 × 520 × 600 mm",
	}
	if calls[0].Input != wantInput || !calls[0].HasDeadline {
		t.Errorf("create call: got %#v, want input %#v with deadline", calls[0], wantInput)
	}
}

// TestAdminProductCreateAcceptsMaximumMultibyteContent proves the transport
// cap remains larger than every semantically valid form after URL percent
// encoding expands four-byte Unicode characters.
func TestAdminProductCreateAcceptsMaximumMultibyteContent(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminProductWriter()
	writer.setCreateOutcome(adminProductWriteResult{ID: 38, Version: 1}, nil)
	fixture.app.adminProductWrites = writer

	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	values.Set("description", strings.Repeat("\U0001F4A0", productDescriptionMaximumLength))
	values.Set("material", strings.Repeat("\U0001F4A0", productMaterialMaximumLength))
	values.Set("dimensions", strings.Repeat("\U0001F4A0", productDimensionsMaximumLength))
	encodedLength := len(values.Encode())
	if encodedLength <= 64*1024 || encodedLength > adminProductFormMaximumBytes {
		t.Fatalf(
			"encoded maximum form length: got %d, want above old cap and within current cap",
			encodedLength,
		)
	}

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminHTTPPostFormRequest(
			adminProductCreatePath,
			values,
			false,
			fixture.cookies()...,
		),
	)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if calls := writer.createCallSnapshot(); len(calls) != 1 ||
		calls[0].Input.Description != values.Get("description") ||
		calls[0].Input.Material != values.Get("material") ||
		calls[0].Input.Dimensions != values.Get("dimensions") {
		t.Errorf("maximum multibyte form writer calls: %#v", calls)
	}
}

// TestAdminProductUpdateAcceptsCurrentRevision verifies exact optimistic-lock
// arguments and the canonical detail redirect.
func TestAdminProductUpdateAcceptsCurrentRevision(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	writer.setUpdateOutcome(adminProductWriteResult{ID: 1, Version: 3}, nil)
	fixture.app.adminProductWrites = writer
	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	values.Set("version", "2")
	values.Set("publication_status", publishedProductStatus)

	request := adminHTTPPostFormRequest(
		"/admin/products/1",
		values,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/admin/products/1" {
		t.Errorf("Location: got %q, want /admin/products/1", location)
	}

	calls := writer.updateCallSnapshot()
	if len(calls) != 1 {
		t.Fatalf("update calls: got %d, want 1", len(calls))
	}
	if calls[0].ID != 1 || calls[0].ExpectedVersion != 2 ||
		calls[0].Input.PublicationStatus != publishedProductStatus ||
		!calls[0].HasDeadline {
		t.Errorf("update call: got %#v", calls[0])
	}
}

// TestAdminProductFormValidationIsEscaped verifies semantic errors return 422,
// restore safe visible text, and never call the writer.
func TestAdminProductFormValidationIsEscaped(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	values.Set("slug", "Bad Slug")
	values.Set("name", `<script>alert("stage20")</script>`)
	values.Set("category", " Furniture ")
	values.Set("sort_order", "01")
	values.Set("publication_status", "Published")
	values.Set("description", "safe prefix\x00hidden tail")

	request := adminHTTPPostFormRequest(
		adminProductCreatePath,
		values,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusUnprocessableEntity)
	assertStage16BodyContains(
		t,
		response.Body,
		"Check the Product fields",
		"lowercase letters or numbers",
		"trimmed category",
		"whole number",
		"Choose Draft, Published, or Archived",
		"Use at most 6000 characters",
		`&lt;script&gt;alert(`,
	)
	assertStage16BodyOmits(t, response.Body, `<script>alert(`)
	if len(writer.createCallSnapshot()) != 0 {
		t.Error("semantically invalid form reached Product writer")
	}
}

// TestAdminProductMutationsCheckCSRFFirst proves a present but incorrect token
// returns 403 before field semantics, record existence, or writing.
func TestAdminProductMutationsCheckCSRFFirst(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	wrongToken, _ := adminHTTPToken(0x7f)

	for _, test := range []struct {
		// path and values distinguish create and update forms.
		path   string
		values url.Values
	}{
		{
			path: adminProductCreatePath,
			values: addAdminProductHTTPToken(
				url.Values{
					"slug":               {"INVALID"},
					"name":               {""},
					"category":           {""},
					"sort_order":         {"0"},
					"publication_status": {"future"},
					"description":        {""},
					"material":           {""},
					"dimensions":         {""},
				},
				wrongToken,
			),
		},
		{
			path: "/admin/products/999",
			values: func() url.Values {
				values := addAdminProductHTTPToken(validAdminProductHTTPForm(), wrongToken)
				values.Set("version", "1")
				return values
			}(),
		},
	} {
		request := adminHTTPPostFormRequest(
			test.path,
			test.values,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)
		assertStage16AdminStatus(t, response, http.StatusForbidden)
	}
	if len(writer.createCallSnapshot()) != 0 || len(writer.updateCallSnapshot()) != 0 {
		t.Error("forged Product request reached writer")
	}
}

// TestAdminProductUpdateRejectsStaleRevision verifies the safe 409 recovery page
// and ensures neither submitted Product text nor CSRF material is reflected.
func TestAdminProductUpdateRejectsStaleRevision(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	writer.setUpdateOutcome(adminProductWriteResult{}, errAdminProductWriteConflict)
	fixture.app.adminProductWrites = writer
	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	values.Set("version", "2")
	values.Set("name", "Private stale Product value")

	request := adminHTTPPostFormRequest(
		"/admin/products/1",
		values,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusConflict)
	assertStage16BodyContains(
		t,
		response.Body,
		"changed in another session",
		`href="/admin/products/1/edit"`,
		`href="/admin/products/1"`,
	)
	assertStage16BodyOmits(
		t,
		response.Body,
		"Private stale Product value",
		`name="version"`,
	)
	if len(writer.updateCallSnapshot()) != 1 {
		t.Error("valid stale edit did not make exactly one guarded writer call")
	}
}

// TestAdminProductSlugConflictReturnsCorrectableForm verifies the expected
// uniqueness collision stays specific while no database diagnostic is exposed.
func TestAdminProductSlugConflictReturnsCorrectableForm(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminProductWriter()
	writer.setCreateOutcome(adminProductWriteResult{}, errAdminProductSlugConflict)
	fixture.app.adminProductWrites = writer
	values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)

	request := adminHTTPPostFormRequest(
		adminProductCreatePath,
		values,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)
	assertStage16AdminStatus(t, response, http.StatusConflict)
	assertStage16BodyContains(
		t,
		response.Body,
		"That slug is already used by another Product.",
		"stage-20-http-chair",
	)
	assertStage16BodyOmits(t, response.Body, "products_slug_unique", "23505")
}

// TestAdminProductWriteRequestBoundaries covers canonical URLs, content coding,
// strict form shape, and hidden-version grammar before any writer call.
func TestAdminProductWriteRequestBoundaries(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	writer := newRecordingAdminProductWriter()
	fixture.app.adminProductWrites = writer
	validCreate := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	validUpdate := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
	validUpdate.Set("version", "2")

	tests := []struct {
		// name identifies one rejected boundary.
		name string
		// request builds the exact malformed HTTP request.
		request func() *http.Request
		// status is the expected generic boundary response.
		status int
	}{
		{name: "new query", request: func() *http.Request {
			return adminHTTPNewRequest(http.MethodGet, "/admin/products/new?mode=create", nil, false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "edit leading zero", request: func() *http.Request {
			return adminHTTPNewRequest(http.MethodGet, "/admin/products/01/edit", nil, false, fixture.cookies()...)
		}, status: http.StatusNotFound},
		{name: "create query", request: func() *http.Request {
			return adminHTTPPostFormRequest("/admin/products?return=x", validCreate, false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "encoded update identity", request: func() *http.Request {
			return adminHTTPPostFormRequest("/admin/products/%31", validUpdate, false, fixture.cookies()...)
		}, status: http.StatusNotFound},
		{name: "unsupported coding", request: func() *http.Request {
			request := adminHTTPPostFormRequest(adminProductCreatePath, validCreate, false, fixture.cookies()...)
			request.Header.Set("Content-Encoding", "gzip")
			return request
		}, status: http.StatusUnsupportedMediaType},
		{name: "missing content type", request: func() *http.Request {
			return adminHTTPNewRequest(http.MethodPost, adminProductCreatePath, strings.NewReader(validCreate.Encode()), false, fixture.cookies()...)
		}, status: http.StatusUnsupportedMediaType},
		{name: "extra field", request: func() *http.Request {
			values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
			values.Set("return_to", "/admin")
			return adminHTTPPostFormRequest(adminProductCreatePath, values, false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "duplicate slug", request: func() *http.Request {
			values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
			values.Add("slug", "other")
			return adminHTTPPostFormRequest(adminProductCreatePath, values, false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "missing csrf field", request: func() *http.Request {
			return adminHTTPPostFormRequest(adminProductCreatePath, validAdminProductHTTPForm(), false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
		{name: "invalid version", request: func() *http.Request {
			values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
			values.Set("version", "02")
			return adminHTTPPostFormRequest("/admin/products/1", values, false, fixture.cookies()...)
		}, status: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := stage16ServeAdminRequest(t, fixture.app, test.request())
			assertStage16AdminStatus(t, response, test.status)
		})
	}
	if len(writer.createCallSnapshot()) != 0 || len(writer.updateCallSnapshot()) != 0 {
		t.Error("malformed Product request reached writer")
	}
}

// TestAdminProductWriteRepositoryOutcomesStayGeneric maps missing rows and
// dependency failures without reflecting unsafe diagnostic text.
func TestAdminProductWriteRepositoryOutcomesStayGeneric(t *testing.T) {
	tests := []struct {
		// name identifies create/update and the arranged safe category.
		name string
		// path is the mutation route.
		path string
		// update distinguishes which recording outcome is configured.
		update bool
		// dependencyError is returned by the recording writer.
		dependencyError error
		// wantStatus is the public HTTP mapping.
		wantStatus int
	}{
		{name: "missing update", path: "/admin/products/999", update: true, dependencyError: errAdminProductNotFound, wantStatus: http.StatusNotFound},
		{name: "failed update", path: "/admin/products/1", update: true, dependencyError: errAdminProductWriteFailed, wantStatus: http.StatusServiceUnavailable},
		{name: "failed create", path: adminProductCreatePath, dependencyError: errAdminProductWriteFailed, wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			writer := newRecordingAdminProductWriter()
			values := addAdminProductHTTPToken(validAdminProductHTTPForm(), fixture.csrfToken)
			if test.update {
				values.Set("version", "2")
				writer.setUpdateOutcome(adminProductWriteResult{}, test.dependencyError)
			} else {
				writer.setCreateOutcome(adminProductWriteResult{}, test.dependencyError)
			}
			fixture.app.adminProductWrites = writer

			request := adminHTTPPostFormRequest(test.path, values, false, fixture.cookies()...)
			response := stage16ServeAdminRequest(t, fixture.app, request)
			assertStage16AdminStatus(t, response, test.wantStatus)
			assertStage16BodyOmits(t, response.Body, "SQL", "password", "stage-20-http-chair")
		})
	}
}
