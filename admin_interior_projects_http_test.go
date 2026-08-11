package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// configuredAdminInteriorProjectReader returns the all-state list and one
// Published detail used by protected HTTP tests. Every row is synthetic.
func configuredAdminInteriorProjectReader() *recordingAdminInteriorProjectReader {
	draft := validAdminInteriorProjectRecord(2, "quiet-residence", 1)
	draft.Title = "Quiet Residence"
	draft.PublicationStatus = draftInteriorProjectStatus
	published := validAdminInteriorProjectRecord(3, "courtyard-office", 2)
	published.Title = "Courtyard Office"
	published.PublicationStatus = publishedInteriorProjectStatus
	archived := validAdminInteriorProjectRecord(4, "former-gallery", 3)
	archived.Title = "Former Gallery"
	archived.PublicationStatus = archivedInteriorProjectStatus

	reader := newRecordingAdminInteriorProjectReader()
	reader.listResult = []adminInteriorProjectRecord{draft, published, archived}
	reader.findResult = published

	return reader
}

// TestAdminInteriorProjectReadRoutesRequireAuthentication proves project text
// and protected cover coordinates cannot be enumerated before session checks.
func TestAdminInteriorProjectReadRoutesRequireAuthentication(t *testing.T) {
	for _, path := range []string{
		adminInteriorProjectNavigationPath,
		adminInteriorProjectPath(3),
		adminInteriorProjectCoverAssetPath(3, 1),
	} {
		t.Run(path, func(t *testing.T) {
			app := newTestApplication(t)
			reader := configuredAdminInteriorProjectReader()
			app.adminInteriorProjects = reader

			response := stage16ServeAdminRequest(
				t,
				app,
				adminHTTPNewRequest(http.MethodGet, path, nil, false),
			)
			assertStage16AdminStatus(t, response, http.StatusSeeOther)
			if response.Header.Get("Location") != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", response.Header.Get("Location"))
			}
			if reader.listCalls != 0 || reader.findCalls != 0 || reader.coverCalls != 0 {
				t.Error("signed-out request reached protected Interior persistence")
			}
		})
	}
}

// TestAdminInteriorProjectReadRoutesRenderAllStates covers both allowed roles,
// active navigation, escaped content, truthful lifecycle labels, and the
// Published-only public detail link.
func TestAdminInteriorProjectReadRoutesRenderAllStates(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			reader := configuredAdminInteriorProjectReader()
			reader.listResult[0].Title = `Quiet <script>alert("x")</script>`
			fixture.app.adminInteriorProjects = reader

			listResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(
					http.MethodGet,
					adminInteriorProjectNavigationPath,
					nil,
					false,
					fixture.cookies()...,
				),
			)
			assertStage16AdminStatus(t, listResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				listResponse.Body,
				"Interior projects",
				`Quiet &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`,
				"Courtyard Office",
				"Former Gallery",
				"Draft",
				"Published",
				"Archived",
				`aria-current="page"`,
			)
			assertStage16BodyOmits(t, listResponse.Body, `<script>alert("x")</script>`)

			detailResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(
					http.MethodGet,
					adminInteriorProjectPath(3),
					nil,
					false,
					fixture.cookies()...,
				),
			)
			assertStage16AdminStatus(t, detailResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				detailResponse.Body,
				"Courtyard Office",
				"Current status: Published",
				`href="/interior-design/courtyard-office"`,
				`href="/admin/interior-projects/3/edit"`,
				`href="/admin/interior-projects/3/cover"`,
				"Project status",
			)
			if reader.listCalls != 1 || reader.findCalls != 1 || reader.findID != 3 {
				t.Errorf("reader calls: list=%d find=%d id=%d", reader.listCalls, reader.findCalls, reader.findID)
			}
			if _, ok := reader.listContext.Deadline(); !ok {
				t.Error("list repository context has no deadline")
			}
			if _, ok := reader.findContext.Deadline(); !ok {
				t.Error("detail repository context has no deadline")
			}
		})
	}
}

// TestAdminInteriorProjectReadRoutesRejectAlternateURLs keeps query-bearing,
// noncanonical identity, and unmatched suffix forms outside persistence.
func TestAdminInteriorProjectReadRoutesRejectAlternateURLs(t *testing.T) {
	tests := []struct {
		// path is one query-bearing, noncanonical, or unmatched URL spelling.
		path string
		// wantStatus distinguishes malformed queries from nonexistent paths.
		wantStatus int
	}{
		{path: "/admin/interior-projects?state=draft", wantStatus: http.StatusBadRequest},
		{path: "/admin/interior-projects/3?view=full", wantStatus: http.StatusBadRequest},
		{path: "/admin/interior-projects/03", wantStatus: http.StatusNotFound},
		{path: "/admin/interior-projects/0", wantStatus: http.StatusNotFound},
		{path: "/admin/interior-projects/3/history", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			reader := configuredAdminInteriorProjectReader()
			fixture.app.adminInteriorProjects = reader
			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(http.MethodGet, test.path, nil, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, response, test.wantStatus)
			if reader.listCalls != 0 || reader.findCalls != 0 {
				t.Error("alternate protected URL reached Interior reader")
			}
		})
	}
}

// TestAdminInteriorProjectReadFailuresStayGeneric distinguishes a missing row
// from dependency failure without disclosing the underlying diagnostic.
func TestAdminInteriorProjectReadFailuresStayGeneric(t *testing.T) {
	for _, test := range []struct {
		// name identifies the repository outcome under test.
		name string
		// errorValue is the configured protected-reader result.
		errorValue error
		// wantStatus is the response-safe protected HTTP classification.
		wantStatus int
	}{
		{name: "missing", errorValue: errAdminInteriorProjectNotFound, wantStatus: http.StatusNotFound},
		{name: "dependency", errorValue: errors.New("private database credential detail"), wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
			reader := configuredAdminInteriorProjectReader()
			reader.findError = test.errorValue
			fixture.app.adminInteriorProjects = reader
			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(http.MethodGet, adminInteriorProjectPath(99), nil, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, response, test.wantStatus)
			if strings.Contains(response.Body, "private database credential detail") {
				t.Error("protected response disclosed repository diagnostic")
			}
		})
	}
}
