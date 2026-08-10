package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// assertAdminProductCalls verifies which protected persistence operation ran.
// Authentication, routing, and syntax failures must leave both call slices empty.
func assertAdminProductCalls(
	t *testing.T,
	reader *recordingAdminProductReader,
	wantList int,
	wantFind []int64,
) {
	t.Helper()

	listCalls := reader.listCallSnapshot()
	findCalls := reader.findCallSnapshot()
	if len(listCalls) != wantList {
		t.Errorf("list calls: got %d, want %d", len(listCalls), wantList)
	}
	for index, call := range listCalls {
		if !call.HasDeadline {
			t.Errorf("list call %d has no request-derived deadline", index)
		}
	}
	if len(findCalls) != len(wantFind) {
		t.Errorf("find calls: got %d, want %d", len(findCalls), len(wantFind))
		return
	}
	for index, call := range findCalls {
		if call.ID != wantFind[index] {
			t.Errorf("find call %d ID: got %d, want %d", index, call.ID, wantFind[index])
		}
		if !call.HasDeadline {
			t.Errorf("find call %d has no request-derived deadline", index)
		}
	}
}

// TestAdminProductRoutesRequireAuthentication proves that Product state cannot
// be enumerated before the shared session middleware succeeds.
func TestAdminProductRoutesRequireAuthentication(t *testing.T) {
	for _, path := range []string{"/admin/products", "/admin/products/1"} {
		t.Run(path, func(t *testing.T) {
			reader := newRecordingAdminProductReader()
			app := newAdminHTTPTestApplication(
				t,
				newRecordingAdminRepository(),
				newTestAdminPasswordManager(t),
			)
			app.adminProducts = reader

			request := adminHTTPNewRequest(http.MethodGet, path, nil, false)
			response := stage16ServeAdminRequest(t, app, request)

			assertStage16AdminStatus(t, response, http.StatusSeeOther)
			if location := response.Header.Get("Location"); location != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", location)
			}
			assertAdminProductCalls(t, reader, 0, nil)
		})
	}
}

// TestAdminProductRoutesAllowCurrentReaderRoles covers both explicit Stage 19
// roles, active navigation, shared logout, all-state listing, and detail reads.
func TestAdminProductRoutesAllowCurrentReaderRoles(t *testing.T) {
	tests := []struct {
		// name is the stable subtest label.
		name string
		// role is the repository-backed authorization value.
		role adminRole
		// roleLabel is the trusted text rendered by the shared shell.
		roleLabel string
	}{
		{name: "owner", role: adminRoleOwner, roleLabel: "Owner"},
		{name: "editor", role: adminRoleEditor, roleLabel: "Editor"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminProductReader()
			fixture := newAdminHTTPAuthenticatedFixture(t, test.role)
			fixture.app.adminProducts = reader

			listRequest := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/products",
				nil,
				false,
				fixture.cookies()...,
			)
			listResponse := stage16ServeAdminRequest(t, fixture.app, listRequest)
			assertStage16AdminStatus(t, listResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				listResponse.Body,
				"Product catalogue",
				"Stage 19 Draft Chair",
				"Stage 19 Published Lamp",
				"Stage 19 Archived Object",
				test.roleLabel,
				`href="/admin/products/1"`,
				`aria-current="page"`,
				`action="/admin/logout"`,
			)

			detailRequest := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/products/1",
				nil,
				false,
				fixture.cookies()...,
			)
			detailResponse := stage16ServeAdminRequest(t, fixture.app, detailRequest)
			assertStage16AdminStatus(t, detailResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				detailResponse.Body,
				"Stage 19 Published Lamp",
				"Current status: Published",
				`href="/products/stage19-published-lamp"`,
				`href="/admin/products"`,
			)
			assertAdminProductCalls(t, reader, 1, []int64{1})
		})
	}
}

// TestAdminProductRoutesAcceptOnlyCanonicalURLs prevents alternate query and ID
// spellings from reaching the repository or creating duplicate protected URLs.
func TestAdminProductRoutesAcceptOnlyCanonicalURLs(t *testing.T) {
	tests := []struct {
		// name describes the rejected URL form.
		name string
		// path is supplied to the real request parser and ServeMux.
		path string
		// wantStatus distinguishes malformed queries from missing resource shapes.
		wantStatus int
	}{
		{name: "list query", path: "/admin/products?status=draft", wantStatus: http.StatusBadRequest},
		{name: "list bare query", path: "/admin/products?", wantStatus: http.StatusBadRequest},
		{name: "detail query", path: "/admin/products/1?view=full", wantStatus: http.StatusBadRequest},
		{name: "detail bare query", path: "/admin/products/1?", wantStatus: http.StatusBadRequest},
		{name: "zero identity", path: "/admin/products/0", wantStatus: http.StatusNotFound},
		{name: "negative identity", path: "/admin/products/-1", wantStatus: http.StatusNotFound},
		{name: "leading zero", path: "/admin/products/01", wantStatus: http.StatusNotFound},
		{name: "encoded digit", path: "/admin/products/%31", wantStatus: http.StatusNotFound},
		{name: "non decimal", path: "/admin/products/one", wantStatus: http.StatusNotFound},
		{name: "overflow", path: "/admin/products/9223372036854775808", wantStatus: http.StatusNotFound},
		{name: "extra segment", path: "/admin/products/1/history", wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminProductReader()
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			fixture.app.adminProducts = reader
			request := adminHTTPNewRequest(
				http.MethodGet,
				test.path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			assertStage16AdminStatus(t, response, test.wantStatus)
			assertAdminProductCalls(t, reader, 0, nil)
		})
	}
}

// TestAdminProductRoutesRejectPOST verifies that ServeMux advertises only GET
// and its automatic HEAD equivalent before a protected read can occur.
func TestAdminProductRoutesRejectPOST(t *testing.T) {
	for _, path := range []string{"/admin/products", "/admin/products/1"} {
		t.Run(path, func(t *testing.T) {
			reader := newRecordingAdminProductReader()
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			fixture.app.adminProducts = reader
			request := adminHTTPNewRequest(
				http.MethodPost,
				path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			assertStage16AdminStatus(t, response, http.StatusMethodNotAllowed)
			if allow := response.Header.Get("Allow"); allow != http.MethodGet+", "+http.MethodHead {
				t.Errorf("Allow: got %q, want %q", allow, http.MethodGet+", "+http.MethodHead)
			}
			assertAdminProductCalls(t, reader, 0, nil)
		})
	}
}

// TestAdminProductListEscapesStoredTextAndHandlesEmptyState verifies contextual
// escaping, deterministic order, and the truthful unseeded-database interface.
func TestAdminProductListEscapesStoredTextAndHandlesEmptyState(t *testing.T) {
	t.Run("escaped ordered records", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		products := []adminProductRecord{
			validAdminProductRecord(7, "first-product", 1, productPublicationStatusDraft),
			validAdminProductRecord(8, "second-product", 2, productPublicationStatusArchived),
		}
		products[0].Name = "First <script>alert(1)</script>"
		products[1].Name = "Second Product"
		reader.setProducts(products)
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
		fixture.app.adminProducts = reader

		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/products",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)
		assertStage16AdminStatus(t, response, http.StatusOK)
		assertStage16BodyContains(
			t,
			response.Body,
			"First &lt;script&gt;alert(1)&lt;/script&gt;",
			"Second Product",
			"Draft",
			"Archived",
		)
		assertStage16BodyOmits(t, response.Body, "<script>alert(1)</script>", "password_hash", "submission_key")
		if first := strings.Index(response.Body, "First &lt;script&gt;"); first < 0 {
			t.Error("first Product marker is absent")
		} else if second := strings.Index(response.Body, "Second Product"); second < first {
			t.Error("Product cards do not preserve repository order")
		}
	})

	t.Run("empty database", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		reader.setProducts([]adminProductRecord{})
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = reader

		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/products",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)
		assertStage16AdminStatus(t, response, http.StatusOK)
		assertStage16BodyContains(t, response.Body, "No Products to display", "No Products have been created yet.")
		assertStage16BodyOmits(t, response.Body, "Stage 19 Draft Chair")
	})
}

// TestAdminProductDetailSeparatesProtectedAndPublicVisibility ensures only a
// Published record receives a public link while all three states remain readable.
func TestAdminProductDetailSeparatesProtectedAndPublicVisibility(t *testing.T) {
	tests := []struct {
		// id selects the fixture record.
		id int64
		// label is the visible current-state text.
		label string
		// publicPath is present only for the Published fixture.
		publicPath string
		// visibility is the trusted explanation shown to administrators.
		visibility string
	}{
		{id: 2, label: "Draft", visibility: "This Product is not visible on the public website."},
		{id: 1, label: "Published", publicPath: "/products/stage19-published-lamp", visibility: "This Product is visible in the public catalogue."},
		{id: 3, label: "Archived", visibility: "This Product remains stored but is hidden from the public website."},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			reader := newRecordingAdminProductReader()
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			fixture.app.adminProducts = reader
			request := adminHTTPNewRequest(
				http.MethodGet,
				adminProductPath(test.id),
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			assertStage16AdminStatus(t, response, http.StatusOK)
			assertStage16BodyContains(t, response.Body, "Current status: "+test.label, test.visibility, "Read-only catalogue stage")
			if test.publicPath == "" {
				assertStage16BodyOmits(t, response.Body, `href="/products/`)
			} else {
				assertStage16BodyContains(t, response.Body, `href="`+test.publicPath+`"`)
			}
		})
	}
}

// TestAdminProductReadFailuresStayGeneric maps missing rows separately while
// preventing repository diagnostics or stored malformed data from reaching HTML.
func TestAdminProductReadFailuresStayGeneric(t *testing.T) {
	unsafeDetail := "secret-product-database-diagnostic"

	t.Run("missing detail", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = reader
		request := adminHTTPNewRequest(http.MethodGet, "/admin/products/999", nil, false, fixture.cookies()...)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusNotFound)
		assertAdminProductCalls(t, reader, 0, []int64{999})
	})

	t.Run("list dependency failure", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		reader.setErrors(errors.New(unsafeDetail), nil)
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = reader
		request := adminHTTPNewRequest(http.MethodGet, "/admin/products", nil, false, fixture.cookies()...)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
		assertStage16BodyOmits(t, response.Body, unsafeDetail, "Stage 19 Draft Chair")
	})

	t.Run("detail dependency failure", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		reader.setErrors(nil, errors.New(unsafeDetail))
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = reader
		request := adminHTTPNewRequest(http.MethodGet, "/admin/products/1", nil, false, fixture.cookies()...)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
		assertStage16BodyOmits(t, response.Body, unsafeDetail, "Stage 19 Published Lamp")
	})

	t.Run("malformed substituted list", func(t *testing.T) {
		reader := newRecordingAdminProductReader()
		invalid := validAdminProductRecord(1, "invalid-product", 1, "future")
		reader.setProducts([]adminProductRecord{invalid})
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = reader
		request := adminHTTPNewRequest(http.MethodGet, "/admin/products", nil, false, fixture.cookies()...)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
		assertStage16BodyOmits(t, response.Body, "future", "invalid-product")
	})

	t.Run("missing runtime dependency", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.app.adminProducts = nil
		request := adminHTTPNewRequest(http.MethodGet, "/admin/products", nil, false, fixture.cookies()...)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
	})
}
