package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// validAdminInteriorProjectHTTPForm is the exact visible form contract. CSRF
// and the edit-only revision are added by each test explicitly.
func validAdminInteriorProjectHTTPForm() url.Values {
	return url.Values{
		"slug":               {"atrium-residence"},
		"title":              {"Atrium Residence"},
		"typology":           {"Residential"},
		"location":           {"Tehran"},
		"project_year":       {"2025"},
		"project_status":     {"Completed"},
		"description":        {"A reviewed fictional Interior project."},
		"sort_order":         {"7"},
		"publication_status": {draftInteriorProjectStatus},
	}
}

// adminInteriorProjectHTTPFormWithToken copies a form before adding the
// session token so subtests never share mutable url.Values state.
func adminInteriorProjectHTTPFormWithToken(
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

// TestAdminInteriorProjectWriteRoutesRequireAuthentication verifies all text
// form and mutation endpoints redirect before reading or changing project data.
func TestAdminInteriorProjectWriteRoutesRequireAuthentication(t *testing.T) {
	for _, test := range []struct {
		// method is the exact form or mutation method exercised while signed out.
		method string
		// path is the canonical protected create or edit address.
		path string
	}{
		{method: http.MethodGet, path: adminInteriorProjectNewPath},
		{method: http.MethodPost, path: adminInteriorProjectCreatePath},
		{method: http.MethodGet, path: adminInteriorProjectPath(3) + "/edit"},
		{method: http.MethodPost, path: adminInteriorProjectPath(3)},
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
			if reader.findCalls != 0 || writer.createCalls != 0 || writer.updateCalls != 0 {
				t.Error("signed-out write route reached Interior persistence")
			}
		})
	}
}

// TestAdminInteriorProjectFormsRenderTrustedContracts checks both permitted
// roles, closed lifecycle options, session CSRF, and edit revision output.
func TestAdminInteriorProjectFormsRenderTrustedContracts(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			reader := configuredAdminInteriorProjectReader()
			fixture.app.adminInteriorProjects = reader

			newResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(http.MethodGet, adminInteriorProjectNewPath, nil, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, newResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				newResponse.Body,
				"Create Interior project",
				`action="/admin/interior-projects"`,
				`name="project_year"`,
				`name="project_status"`,
				`value="draft"`,
				`value="published"`,
				`value="archived"`,
			)
			if adminHTTPHiddenValue(t, newResponse.Body, "csrf_token") != fixture.csrfToken {
				t.Error("new Interior form omitted the session CSRF token")
			}
			assertStage16BodyOmits(t, newResponse.Body, `name="version"`)

			editResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPNewRequest(http.MethodGet, adminInteriorProjectPath(3)+"/edit", nil, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, editResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				editResponse.Body,
				"Edit Interior project",
				`action="/admin/interior-projects/3"`,
				"Courtyard Office",
				"Interior revision 3",
			)
			if adminHTTPHiddenValue(t, editResponse.Body, "version") != "3" {
				t.Error("edit form omitted the current Interior project revision")
			}
			if reader.findCalls != 1 || reader.findID != 3 {
				t.Errorf("edit lookup: calls=%d id=%d", reader.findCalls, reader.findID)
			}
		})
	}
}

// TestAdminInteriorProjectCreateAcceptsValidatedForm verifies exact typed input,
// request-derived deadline, nullable-year representation, and PRG destination.
func TestAdminInteriorProjectCreateAcceptsValidatedForm(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
	writer := newRecordingAdminInteriorProjectWriter()
	writer.createResult = adminInteriorProjectWriteResult{ID: 41, Version: 1}
	fixture.app.adminInteriorProjectWrites = writer
	values := adminInteriorProjectHTTPFormWithToken(validAdminInteriorProjectHTTPForm(), fixture.csrfToken)
	values.Set("project_year", "")

	response := stage16ServeAdminRequest(
		t,
		fixture.app,
		adminHTTPPostFormRequest(adminInteriorProjectCreatePath, values, false, fixture.cookies()...),
	)
	assertStage16AdminStatus(t, response, http.StatusSeeOther)
	if response.Header.Get("Location") != "/admin/interior-projects/41" {
		t.Errorf("Location: got %q, want new detail", response.Header.Get("Location"))
	}
	want := adminInteriorProjectWriteInput{
		Slug:              "atrium-residence",
		Title:             "Atrium Residence",
		Typology:          "Residential",
		Location:          "Tehran",
		ProjectYear:       0,
		ProjectStatus:     "Completed",
		Description:       "A reviewed fictional Interior project.",
		SortOrder:         7,
		PublicationStatus: draftInteriorProjectStatus,
	}
	if writer.createCalls != 1 || writer.createInput != want {
		t.Errorf("create call: count=%d input=%#v, want %#v", writer.createCalls, writer.createInput, want)
	}
	if _, ok := writer.createContext.Deadline(); !ok {
		t.Error("create repository context has no deadline")
	}
}

// TestAdminInteriorProjectUpdateUsesOptimisticRevision verifies exact update
// coordinates and the fixed conflict recovery page for a stale writer result.
func TestAdminInteriorProjectUpdateUsesOptimisticRevision(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		writer := newRecordingAdminInteriorProjectWriter()
		writer.updateResult = adminInteriorProjectWriteResult{ID: 3, Version: 4}
		fixture.app.adminInteriorProjectWrites = writer
		values := adminInteriorProjectHTTPFormWithToken(validAdminInteriorProjectHTTPForm(), fixture.csrfToken)
		values.Set("version", "3")
		values.Set("publication_status", publishedInteriorProjectStatus)

		response := stage16ServeAdminRequest(
			t,
			fixture.app,
			adminHTTPPostFormRequest(adminInteriorProjectPath(3), values, false, fixture.cookies()...),
		)
		assertStage16AdminStatus(t, response, http.StatusSeeOther)
		if response.Header.Get("Location") != adminInteriorProjectPath(3) {
			t.Errorf("Location: got %q, want detail", response.Header.Get("Location"))
		}
		if writer.updateCalls != 1 || writer.updateID != 3 ||
			writer.updateExpectedVersion != 3 ||
			writer.updateInput.PublicationStatus != publishedInteriorProjectStatus {
			t.Errorf("update call: count=%d id=%d version=%d input=%#v", writer.updateCalls, writer.updateID, writer.updateExpectedVersion, writer.updateInput)
		}
	})

	t.Run("stale", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleEditor)
		writer := newRecordingAdminInteriorProjectWriter()
		writer.updateError = errAdminInteriorProjectWriteConflict
		fixture.app.adminInteriorProjectWrites = writer
		values := adminInteriorProjectHTTPFormWithToken(validAdminInteriorProjectHTTPForm(), fixture.csrfToken)
		values.Set("version", "3")

		response := stage16ServeAdminRequest(
			t,
			fixture.app,
			adminHTTPPostFormRequest(adminInteriorProjectPath(3), values, false, fixture.cookies()...),
		)
		assertStage16AdminStatus(t, response, http.StatusConflict)
		assertStage16BodyContains(
			t,
			response.Body,
			"changed in another session",
			`href="/admin/interior-projects/3/edit"`,
			`href="/admin/interior-projects/3"`,
		)
		assertStage16BodyOmits(t, response.Body, "A reviewed fictional Interior project.")
	})
}

// TestAdminInteriorProjectMutationRejectsUntrustedForms covers strict field
// cardinality, CSRF, semantic errors, escaped restoration, and zero writes.
func TestAdminInteriorProjectMutationRejectsUntrustedForms(t *testing.T) {
	for _, test := range []struct {
		// name identifies the isolated form-shape or semantic boundary.
		name string
		// change applies one deliberate mutation to an otherwise valid form.
		change func(url.Values)
		// wantStatus is the safe HTTP classification for that rejection.
		wantStatus int
	}{
		{name: "unknown field", wantStatus: http.StatusBadRequest, change: func(values url.Values) { values.Set("future_control", "1") }},
		{name: "duplicate field", wantStatus: http.StatusBadRequest, change: func(values url.Values) { values["title"] = []string{"one", "two"} }},
		{name: "wrong CSRF", wantStatus: http.StatusForbidden, change: func(values url.Values) { values.Set("csrf_token", "wrong") }},
		{name: "unsupported lifecycle", wantStatus: http.StatusUnprocessableEntity, change: func(values url.Values) { values.Set("publication_status", "future") }},
		{name: "noncanonical year", wantStatus: http.StatusUnprocessableEntity, change: func(values url.Values) { values.Set("project_year", "02025") }},
		{name: "escaped title", wantStatus: http.StatusUnprocessableEntity, change: func(values url.Values) { values.Set("title", ` <script>alert("title")</script> `) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
			writer := newRecordingAdminInteriorProjectWriter()
			fixture.app.adminInteriorProjectWrites = writer
			values := adminInteriorProjectHTTPFormWithToken(validAdminInteriorProjectHTTPForm(), fixture.csrfToken)
			test.change(values)
			response := stage16ServeAdminRequest(
				t,
				fixture.app,
				adminHTTPPostFormRequest(adminInteriorProjectCreatePath, values, false, fixture.cookies()...),
			)
			assertStage16AdminStatus(t, response, test.wantStatus)
			if writer.createCalls != 0 {
				t.Error("rejected form reached Interior writer")
			}
			if test.name == "escaped title" {
				if !strings.Contains(response.Body, `&lt;script&gt;alert(&#34;title&#34;)&lt;/script&gt;`) ||
					strings.Contains(response.Body, `<script>alert("title")</script>`) {
					t.Error("invalid project title was not restored through contextual escaping")
				}
			}
		})
	}
}
