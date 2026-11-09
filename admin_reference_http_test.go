package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestAdminReferenceWorkspacesRequireAuthentication keeps every reference
// preview behind the same database-backed session boundary as the real admin
// tools. Unknown future modules must be registered and reviewed explicitly.
func TestAdminReferenceWorkspacesRequireAuthentication(t *testing.T) {
	app := newTestApplication(t)

	for _, path := range adminReferenceWorkspacePaths {
		t.Run(path, func(t *testing.T) {
			request := adminHTTPNewRequest(
				http.MethodGet,
				path,
				nil,
				false,
			)
			response := stage17ServeAdminRequest(t, app, request)

			if response.StatusCode != http.StatusSeeOther {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusSeeOther,
				)
			}
			if location := response.Header.Get("Location"); location != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", location)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminReferenceWorkspacesRenderForCurrentRoles verifies the complete
// authenticated route graph, explicit role allowlist, active navigation state,
// and honest preview labeling for every not-yet-connected business module.
func TestAdminReferenceWorkspacesRenderForCurrentRoles(t *testing.T) {
	workspaces := []struct {
		path  string
		title string
	}{
		{path: "/admin/orders", title: "Orders"},
		{path: "/admin/analytics", title: "Analytics"},
		{path: "/admin/customers", title: "Customers"},
		{path: "/admin/finances", title: "Finances"},
		{path: "/admin/invoices", title: "Invoices"},
		{path: "/admin/discounts", title: "Discounts"},
		{path: "/admin/chats", title: "Chats"},
		{path: "/admin/email", title: "Email"},
		{path: "/admin/apps", title: "Apps"},
		{path: "/admin/settings", title: "Settings"},
		{path: "/admin/help", title: "Help center"},
	}

	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		role := role
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)

			for _, workspace := range workspaces {
				workspace := workspace
				t.Run(workspace.path, func(t *testing.T) {
					request := adminHTTPNewRequest(
						http.MethodGet,
						workspace.path,
						nil,
						false,
						fixture.cookies()...,
					)
					response := stage17ServeAdminRequest(t, fixture.app, request)

					if response.StatusCode != http.StatusOK {
						t.Fatalf(
							"status: got %d, want %d; body=%q",
							response.StatusCode,
							http.StatusOK,
							response.Body,
						)
					}
					assertAdminHTTPSecurityHeaders(t, response.Header)

					for _, expected := range []string{
						workspace.title,
						"Preview data",
						"Demonstration workspace",
						"Read-only preview",
						adminHTTPTestEmail,
						`action="/admin/logout"`,
						`/static/js/admin-shell.js`,
					} {
						if !strings.Contains(response.Body, expected) {
							t.Errorf("HTML does not contain %q", expected)
						}
					}

					activeLink := regexp.MustCompile(
						`(?s)<a[^>]*href="` +
							regexp.QuoteMeta(workspace.path) +
							`"[^>]*aria-current="page"[^>]*>`,
					)
					if !activeLink.MatchString(response.Body) {
						t.Errorf(
							"navigation does not mark %q as the current page",
							workspace.path,
						)
					}
				})
			}
		})
	}
}

// TestAdminReferenceWorkspacesRejectMutations confirms the preview layer adds
// no accidental write surface. ServeMux generates 405 before any handler can
// interpret a request body, while the outer admin security policy still holds.
func TestAdminReferenceWorkspacesRejectMutations(t *testing.T) {
	app := newTestApplication(t)

	for _, path := range adminReferenceWorkspacePaths {
		t.Run(path, func(t *testing.T) {
			request := adminHTTPNewRequest(
				http.MethodPost,
				path,
				strings.NewReader("ignored=true"),
				false,
			)
			response := stage17ServeAdminRequest(t, app, request)

			if response.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusMethodNotAllowed,
				)
			}
			if allow := response.Header.Get("Allow"); !strings.Contains(allow, "GET") {
				t.Errorf("Allow: got %q, want GET", allow)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}
