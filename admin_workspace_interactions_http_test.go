package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestAdminDashboardRendersFunctionalPreviewControls protects the dashboard
// controls that are intentionally interactive in the browser. The assertions
// keep range switching, goal editing, stock actions, and the shared command
// shell from silently degrading back into decorative labels.
func TestAdminDashboardRendersFunctionalPreviewControls(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage17ServeAdminRequest(t, fixture.app, request)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusOK, response.Body)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)

	for _, expected := range []string{
		`/static/css/admin-shell.css?v=1`,
		`/static/js/admin-dashboard.js?v=1`,
		`data-admin-command-palette`,
		`data-admin-command-input`,
		`data-admin-theme-toggle`,
		`data-admin-current-date`,
		`data-dashboard-chart-scale`,
		`data-dashboard-chart-area`,
		`data-dashboard-chart-comparison`,
		`data-dashboard-chart-line`,
		`data-dashboard-chart-point`,
		`data-dashboard-chart-labels`,
		`data-dashboard-goal-dialog`,
		`data-dashboard-goal-form`,
		`data-dashboard-restock=`,
		`data-dashboard-toast`,
		`Best sellers`,
	} {
		if !strings.Contains(response.Body, expected) {
			t.Errorf("dashboard HTML does not contain %q", expected)
		}
	}

	if count := strings.Count(response.Body, `data-dashboard-range=`); count != 4 {
		t.Errorf("dashboard range controls: got %d, want 4", count)
	}
	for _, period := range []string{"12m", "6m", "30d", "7d"} {
		pattern := regexp.MustCompile(
			`<button[^>]*data-dashboard-range="` + period + `"[^>]*aria-pressed="(?:true|false)"[^>]*>`,
		)
		if !pattern.MatchString(response.Body) {
			t.Errorf("dashboard lacks an accessible %q range button", period)
		}
	}
	if count := strings.Count(response.Body, `data-dashboard-range=`+`"7d" aria-pressed="true"`); count != 1 {
		t.Errorf("dashboard default range selection: got %d active 7d controls, want 1", count)
	}
}

// TestAdminOrdersWorkspaceRendersCompleteBrowserWorkflow verifies that the
// Orders route exposes the full local-preview workflow: filters, bulk actions,
// board movement with a keyboard alternative, returns, details, and CSV export.
func TestAdminOrdersWorkspaceRendersCompleteBrowserWorkflow(t *testing.T) {
	for _, role := range []adminRole{adminRoleOwner, adminRoleEditor} {
		role := role
		t.Run(string(role), func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, role)
			request := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/orders",
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			if response.StatusCode != http.StatusOK {
				t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusOK, response.Body)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)

			for _, expected := range []string{
				`/static/css/admin-orders.css?v=1`,
				`/static/js/admin-orders.js?v=1`,
				`role="tablist"`,
				`data-orders-search`,
				`data-orders-status-filter`,
				`data-orders-channel-filter`,
				`data-orders-date-filter`,
				`data-orders-sort`,
				`data-orders-select-all`,
				`data-orders-bulk-action`,
				`data-orders-export`,
				`data-orders-reset-board`,
				`data-orders-detail-dialog`,
				`data-orders-new-dialog`,
				`data-returns-filter`,
				`data-return-action=`,
				`data-return-details`,
				`data-return-detail-dialog`,
				`data-return-dialog-id`,
				`data-return-dialog-customer`,
				`data-return-dialog-status`,
				`data-orders-announcer`,
				`Preview data is saved only in this browser`,
			} {
				if !strings.Contains(response.Body, expected) {
					t.Errorf("Orders HTML does not contain %q", expected)
				}
			}

			if count := strings.Count(response.Body, `data-orders-tab=`); count != 4 {
				t.Errorf("order workspace tabs: got %d, want 4", count)
			}
			if count := strings.Count(response.Body, `data-orders-panel=`); count != 4 {
				t.Errorf("order workspace panels: got %d, want 4", count)
			}
			if count := strings.Count(response.Body, `data-order-column=`); count != 4 {
				t.Errorf("packing-board columns: got %d, want 4", count)
			}
			if count := strings.Count(response.Body, `draggable="true"`); count < 4 {
				t.Errorf("draggable order cards: got %d, want at least 4", count)
			}
			if count := strings.Count(response.Body, `data-order-move`); count < 4 {
				t.Errorf("keyboard/touch move selectors: got %d, want at least 4", count)
			}
			if count := strings.Count(response.Body, `data-return-details`); count != 3 {
				t.Errorf("return detail controls: got %d, want 3", count)
			}
			if count := strings.Count(response.Body, `data-return-detail-dialog`); count != 1 {
				t.Errorf("return detail dialogs: got %d, want 1", count)
			}

			for _, forbidden := range []string{
				` onclick=`,
				` ondragstart=`,
				` ondrop=`,
				`<script>`,
				`<script type=`,
				`https://`,
				`http://`,
			} {
				if strings.Contains(response.Body, forbidden) {
					t.Errorf("Orders HTML contains unsafe or remote markup %q", forbidden)
				}
			}
		})
	}
}

// TestAdminReferenceWorkspaceRendersInteractivePreviewTools keeps the generic
// module template honest: its tabs, record filters, sorting, and export control
// must remain real controls backed by the self-hosted preview controller.
func TestAdminReferenceWorkspaceRendersInteractivePreviewTools(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/analytics",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage17ServeAdminRequest(t, fixture.app, request)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusOK, response.Body)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)

	for _, expected := range []string{
		`/static/js/admin-reference.js?v=1`,
		`role="tablist"`,
		`role="tabpanel"`,
		`data-reference-tab`,
		`data-reference-search`,
		`data-reference-sort`,
		`data-reference-export`,
		`data-reference-reset`,
		`data-reference-result-count`,
		`data-reference-announcer`,
		`No backend business record is changed`,
	} {
		if !strings.Contains(response.Body, expected) {
			t.Errorf("reference workspace HTML does not contain %q", expected)
		}
	}
	if count := strings.Count(response.Body, `role="tab"`); count < 2 {
		t.Errorf("reference workspace tabs: got %d, want at least 2", count)
	}
	if count := strings.Count(response.Body, `aria-selected="true"`); count != 1 {
		t.Errorf("selected reference tabs: got %d, want 1", count)
	}
}
