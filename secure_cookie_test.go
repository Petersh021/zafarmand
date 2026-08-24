package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// secureCookieTestScenario represents every trusted and untrusted way a
// request may claim HTTPS at the application boundary.
type secureCookieTestScenario struct {
	name           string
	directTLS      bool
	externalHTTPS  bool
	forwardedProto bool
	expectSecure   bool
}

// request builds one scenario without allowing an untrusted forwarding header
// to install the private operational context marker.
func (scenario secureCookieTestScenario) request(
	method string,
	path string,
) *http.Request {
	target := "http://example.test" + path
	if scenario.directTLS {
		target = "https://example.test" + path
	}
	request := httptest.NewRequest(method, target, nil)
	if scenario.externalHTTPS {
		request = requestWithExternalHTTPS(request)
	}
	if scenario.forwardedProto {
		request.Header.Set("X-Forwarded-Proto", "https")
	}

	return request
}

// assertSecureCookieNames checks every named Set-Cookie value produced by one
// helper, including deletion cookies whose transport policy must remain equal
// to the value used when the browser received the original cookie.
func assertSecureCookieNames(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	expectedSecure bool,
	names ...string,
) {
	t.Helper()

	remaining := make(map[string]struct{}, len(names))
	for _, name := range names {
		remaining[name] = struct{}{}
	}
	for _, cookie := range recorder.Result().Cookies() {
		if _, expected := remaining[cookie.Name]; !expected {
			continue
		}
		if cookie.Secure != expectedSecure {
			t.Errorf(
				"cookie %q Secure: got %t, want %t",
				cookie.Name,
				cookie.Secure,
				expectedSecure,
			)
		}
		delete(remaining, cookie.Name)
	}
	for name := range remaining {
		t.Errorf("response did not set cookie %q", name)
	}
}

// TestCookieSecurityUsesOnlyTrustedHTTPSState covers every cookie family. It
// proves direct TLS and the explicit edge marker set Secure while a forged
// X-Forwarded-Proto header has no effect.
func TestCookieSecurityUsesOnlyTrustedHTTPSState(t *testing.T) {
	scenarios := []secureCookieTestScenario{
		{name: "plain HTTP"},
		{name: "direct TLS", directTLS: true, expectSecure: true},
		{name: "declared external HTTPS", externalHTTPS: true, expectSecure: true},
		{name: "spoofed forwarding header", forwardedProto: true},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			app := &application{
				adminEntropy: repeatingAdminEntropy(0x37),
				now: func() time.Time {
					return time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC)
				},
			}

			loginRequest := scenario.request(
				http.MethodGet,
				"/admin/login",
			)
			loginRecorder := httptest.NewRecorder()
			if _, err := app.issueAdminLoginCSRFToken(
				loginRecorder,
				loginRequest,
			); err != nil {
				t.Fatalf("issue admin login CSRF token: %v", err)
			}
			assertSecureCookieNames(
				t,
				loginRecorder,
				scenario.expectSecure,
				adminLoginCSRFTokenCookieName,
			)

			sessionRequest := scenario.request(
				http.MethodPost,
				"/admin/login",
			)
			sessionRecorder := httptest.NewRecorder()
			setAdminSessionCookies(
				sessionRecorder,
				sessionRequest,
				"test-session-token",
				"test-csrf-token",
				time.Now().UTC().Add(adminSessionLifetime),
			)
			assertSecureCookieNames(
				t,
				sessionRecorder,
				scenario.expectSecure,
				adminSessionCookieName,
				adminSessionCSRFTokenCookieName,
			)

			clearRecorder := httptest.NewRecorder()
			clearAdminSessionCookies(
				clearRecorder,
				scenario.request(http.MethodPost, "/admin/logout"),
			)
			assertSecureCookieNames(
				t,
				clearRecorder,
				scenario.expectSecure,
				adminSessionCookieName,
				adminSessionCSRFTokenCookieName,
			)

			contactRecorder := httptest.NewRecorder()
			writeInquiryCSRFCookie(
				contactRecorder,
				scenario.request(http.MethodGet, "/contact"),
				"test-contact-csrf-token",
			)
			assertSecureCookieNames(
				t,
				contactRecorder,
				scenario.expectSecure,
				inquiryCSRFCookieName,
			)

			flash := deterministicInquirySuccessFlash(t)
			flashRecorder := httptest.NewRecorder()
			if err := flash.issue(
				flashRecorder,
				scenario.request(http.MethodPost, "/contact"),
			); err != nil {
				t.Fatalf("issue inquiry success receipt: %v", err)
			}
			assertSecureCookieNames(
				t,
				flashRecorder,
				scenario.expectSecure,
				inquirySuccessFlashCookieName,
			)

			flashDeleteRecorder := httptest.NewRecorder()
			deleteInquirySuccessFlashCookie(
				flashDeleteRecorder,
				scenario.request(http.MethodGet, "/contact"),
			)
			assertSecureCookieNames(
				t,
				flashDeleteRecorder,
				scenario.expectSecure,
				inquirySuccessFlashCookieName,
			)
		})
	}
}

// TestContactCSRFReuseUpgradesOnlyForTrustedHTTPS verifies the reuse-specific
// branch reissues a Secure cookie for the declared edge but not for a spoofed
// forwarding header.
func TestContactCSRFReuseUpgradesOnlyForTrustedHTTPS(t *testing.T) {
	token, err := newInquiryCSRFToken()
	if err != nil {
		t.Fatalf("create Contact CSRF fixture: %v", err)
	}

	for _, scenario := range []secureCookieTestScenario{
		{name: "declared external HTTPS", externalHTTPS: true, expectSecure: true},
		{name: "spoofed forwarding header", forwardedProto: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := scenario.request(http.MethodGet, "/contact")
			request.AddCookie(&http.Cookie{
				Name:  inquiryCSRFCookieName,
				Value: token,
			})
			recorder := httptest.NewRecorder()

			actual, err := ensureInquiryCSRFToken(recorder, request)
			if err != nil {
				t.Fatalf("reuse Contact CSRF token: %v", err)
			}
			if actual != token {
				t.Error("valid Contact CSRF token was replaced")
			}
			if scenario.expectSecure {
				assertSecureCookieNames(
					t,
					recorder,
					true,
					inquiryCSRFCookieName,
				)
			} else if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
				t.Errorf("untrusted HTTPS claim reissued %d cookies", len(cookies))
			}
		})
	}
}
