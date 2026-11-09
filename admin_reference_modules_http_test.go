package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestAdminReferenceModulesExposeCompletePreviewControls keeps the specialised
// Zafarmand workspace tools present on their intended exact routes. Every control
// remains client-side; the route-level mutation rejection is covered by the
// shared reference-workspace tests.
func TestAdminReferenceModulesExposeCompletePreviewControls(t *testing.T) {
	modules := []struct {
		path     string
		expected []string
	}{
		{path: "/admin/analytics", expected: []string{`data-reference-module="analytics"`, `data-analytics-metric`, `data-analytics-period-label`, `data-analytics-range="30d"`, `data-analytics-range="90d"`, `data-analytics-range="12m"`, `data-reference-action="email-report"`}},
		{path: "/admin/customers", expected: []string{`data-reference-module="customers"`, `data-customer-segment`, `data-customer-visible`, `data-customer-card`, `data-reference-action="profile"`, `data-reference-action="review-profile"`, `data-reference-action="new-segment"`}},
		{path: "/admin/finances", expected: []string{`data-reference-module="finances"`, `data-finance-period`, `data-finance-period-label`, `data-reference-action="finance-export"`, `data-reference-action="new-expense"`}},
		{path: "/admin/invoices", expected: []string{`data-reference-module="invoices"`, `data-invoice-card`, `data-invoice-status-label`, `data-reference-action="new-invoice"`, `data-reference-action="invoice-detail"`}},
		{path: "/admin/discounts", expected: []string{`data-reference-module="discounts"`, `data-reference-action="new-discount"`, `data-discount-toggle`, `aria-pressed="true"`}},
		{path: "/admin/chats", expected: []string{`data-reference-module="chats"`, `data-chat-filter="open"`, `data-chat-filter="closed"`, `data-chat-search`, `data-chat-thread`, `data-chat-close`, `data-chat-quick`, `data-chat-form`}},
		{path: "/admin/email", expected: []string{`data-reference-module="email"`, `data-email-search`, `data-email-message`, `data-email-detail-subject`, `data-reference-action="compose-email"`, `data-reference-action="reply-email"`, `data-reference-action="flag-email"`}},
		{path: "/admin/apps", expected: []string{`data-reference-module="apps"`, `data-app-review`, `data-app-review-count`}},
		{path: "/admin/settings", expected: []string{`data-reference-module="settings"`, `data-settings-density`, `data-settings-locale`, `data-settings-motion`, `data-settings-notifications`, `data-settings-reset`}},
		{path: "/admin/help", expected: []string{`data-reference-module="help"`, `data-help-search`, `data-help-faq`, `data-reference-action="support-ticket"`}},
	}

	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	for _, module := range modules {
		module := module
		t.Run(module.path, func(t *testing.T) {
			request := adminHTTPNewRequest(http.MethodGet, module.path, nil, false, fixture.cookies()...)
			response := stage17ServeAdminRequest(t, fixture.app, request)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusOK, response.Body)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)

			for _, expected := range append(module.expected,
				`data-reference-dialog`,
				`data-reference-toast`,
				`/static/js/admin-reference.js?v=1`,
				`No backend business record is changed`,
			) {
				if !strings.Contains(response.Body, expected) {
					t.Errorf("HTML does not contain %q", expected)
				}
			}
			for _, forbidden := range []string{` onclick=`, ` onsubmit=`, `<script>`, `https://`, `http://`} {
				if strings.Contains(response.Body, forbidden) {
					t.Errorf("HTML contains unsafe or remote markup %q", forbidden)
				}
			}
		})
	}
}
