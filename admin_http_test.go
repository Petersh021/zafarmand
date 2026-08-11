package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	// adminHTTPTestEmail is a normalized, non-production fixture identity.
	adminHTTPTestEmail = "owner@example.test"
	// adminHTTPTestPassword satisfies the real Stage 15 password policy while
	// remaining an obvious test-only value.
	adminHTTPTestPassword = "Test-Only-Admin-Password-15"
)

// adminHTTPHiddenValuePattern finds a named hidden input even though template
// formatting places its attributes on separate lines.
var adminHTTPHiddenValuePattern = regexp.MustCompile(
	`(?s)<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"[^>]*>`,
)

// newAdminHTTPTestApplication constructs the real template and route graph with
// caller-owned authentication dependencies and a database-free Contact writer.
func newAdminHTTPTestApplication(
	t *testing.T,
	repository adminRepository,
	passwords adminPasswordManager,
) *application {
	t.Helper()

	return newAdminHTTPTestApplicationWithInquiryReader(
		t,
		repository,
		passwords,
		newRecordingAdminInquiryReader(),
	)
}

// newAdminHTTPTestApplicationWithInquiryReader builds the real private route
// graph while allowing Stage 16 tests to observe the separate, read-only
// inquiry dependency. Earlier authentication tests keep using the simpler
// helper above because they do not need to arrange visitor records.
func newAdminHTTPTestApplicationWithInquiryReader(
	t *testing.T,
	repository adminRepository,
	passwords adminPasswordManager,
	adminInquiries adminInquiryReader,
) *application {
	t.Helper()

	inquiries := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app, err := newApplication(
		newRecordingProductCatalogueReader(),
		inquiries,
		repository,
		newRecordingAdminProductReader(),
		newRecordingAdminProductWriter(),
		adminInquiries,
		newRecordingAdminInquiryStatusUpdater(),
		passwords,
	)
	if err != nil {
		t.Fatalf("create admin HTTP test application: %v", err)
	}

	return app
}

// adminHTTPEntropy returns a finite deterministic stream containing one full
// token block for each supplied byte. Different bytes make session and CSRF
// tokens visibly independent in assertions.
func adminHTTPEntropy(values ...byte) io.Reader {
	data := make([]byte, 0, len(values)*adminTokenBytes)
	for _, value := range values {
		data = append(data, bytes.Repeat([]byte{value}, adminTokenBytes)...)
	}

	return bytes.NewReader(data)
}

// adminHTTPToken creates the canonical browser spelling and SHA-256 repository
// value for one readable deterministic test token.
func adminHTTPToken(value byte) (string, []byte) {
	raw := bytes.Repeat([]byte{value}, adminTokenBytes)
	digest := sha256.Sum256(raw)

	return base64.RawURLEncoding.EncodeToString(raw), digest[:]
}

// adminHTTPNewRequest creates an HTTP or TLS-marked request and attaches only
// the explicitly supplied browser cookies.
func adminHTTPNewRequest(
	method string,
	path string,
	body io.Reader,
	secure bool,
	cookies ...*http.Cookie,
) *http.Request {
	target := "http://example.test" + path
	if secure {
		target = "https://example.test" + path
	}

	request := httptest.NewRequest(method, target, body)
	if secure && request.TLS == nil {
		request.TLS = &tls.ConnectionState{}
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	return request
}

// adminHTTPPostFormRequest creates one URL-encoded request with the same
// content type emitted by an ordinary browser form submission.
func adminHTTPPostFormRequest(
	path string,
	values url.Values,
	secure bool,
	cookies ...*http.Cookie,
) *http.Request {
	request := adminHTTPNewRequest(
		http.MethodPost,
		path,
		strings.NewReader(values.Encode()),
		secure,
		cookies...,
	)
	request.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)

	return request
}

// adminHTTPResponseCookie returns one uniquely named Set-Cookie value or fails
// the calling test with the full set of cookie names for useful diagnostics.
func adminHTTPResponseCookie(
	t *testing.T,
	response *http.Response,
	name string,
) *http.Cookie {
	t.Helper()

	var result *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name != name {
			continue
		}
		if result != nil {
			t.Fatalf("response contains duplicate %q cookies", name)
		}

		result = cookie
	}
	if result == nil {
		names := make([]string, 0, len(response.Cookies()))
		for _, cookie := range response.Cookies() {
			names = append(names, cookie.Name)
		}
		t.Fatalf("response cookies %q do not contain %q", names, name)
	}

	return result
}

// adminHTTPHiddenValue extracts one hidden form value from rendered admin HTML.
func adminHTTPHiddenValue(
	t *testing.T,
	body string,
	name string,
) string {
	t.Helper()

	for _, match := range adminHTTPHiddenValuePattern.FindAllStringSubmatch(
		body,
		-1,
	) {
		if len(match) == 3 && match[1] == name {
			return html.UnescapeString(match[2])
		}
	}

	t.Fatalf("admin HTML does not contain hidden input %q", name)

	return ""
}

// assertAdminHTTPSecurityHeaders checks the private cache and browser policy
// applied outside ServeMux to every response under /admin.
func assertAdminHTTPSecurityHeaders(
	t *testing.T,
	headers http.Header,
) {
	t.Helper()

	expected := map[string]string{
		"Cache-Control":                "no-store",
		"Content-Security-Policy":      "default-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"X-Robots-Tag":                 "noindex, nofollow, noarchive",
	}
	for name, expectedValue := range expected {
		if actual := headers.Get(name); actual != expectedValue {
			t.Errorf(
				"%s: got %q, want %q",
				name,
				actual,
				expectedValue,
			)
		}
	}
}

// assertAdminHTTPCookieSecurity checks the attributes shared by live admin
// cookies while leaving path and transport expectations explicit at call sites.
func assertAdminHTTPCookieSecurity(
	t *testing.T,
	cookie *http.Cookie,
	path string,
	secure bool,
) {
	t.Helper()

	if cookie.Path != path {
		t.Errorf("cookie %q path: got %q, want %q", cookie.Name, cookie.Path, path)
	}
	if !cookie.HttpOnly {
		t.Errorf("cookie %q is not HttpOnly", cookie.Name)
	}
	if cookie.Secure != secure {
		t.Errorf(
			"cookie %q Secure: got %t, want %t",
			cookie.Name,
			cookie.Secure,
			secure,
		)
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf(
			"cookie %q SameSite: got %v, want Strict",
			cookie.Name,
			cookie.SameSite,
		)
	}
}

// TestAdminLoginPage verifies the anonymous CSRF pair, isolated template shell,
// private response headers, and HTTP/TLS cookie attributes.
func TestAdminLoginPage(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "HTTP"
		if secure {
			name = "HTTPS"
		}

		t.Run(name, func(t *testing.T) {
			repository := newRecordingAdminRepository()
			passwords := newTestAdminPasswordManager(t)
			app := newAdminHTTPTestApplication(t, repository, passwords)
			app.adminEntropy = adminHTTPEntropy(0x11)
			fixedNow := time.Date(2030, 4, 5, 6, 7, 8, 0, time.UTC)
			app.now = func() time.Time { return fixedNow }

			request := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/login",
				nil,
				secure,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusOK,
				)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
			if contentType := response.Header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
				t.Errorf("Content-Type: got %q", contentType)
			}

			body := recorder.Body.String()
			hiddenToken := adminHTTPHiddenValue(t, body, "csrf_token")
			cookie := adminHTTPResponseCookie(
				t,
				response,
				adminLoginCSRFTokenCookieName,
			)
			if hiddenToken != cookie.Value {
				t.Error("login form and protected cookie contain different CSRF tokens")
			}
			if _, _, ok := decodeAndHashAdminToken(hiddenToken); !ok {
				t.Error("login form contains a non-canonical CSRF token")
			}
			assertAdminHTTPCookieSecurity(
				t,
				cookie,
				"/admin/login",
				secure,
			)
			if cookie.MaxAge != int(adminLoginCSRFLifetime/time.Second) {
				t.Errorf("login CSRF MaxAge: got %d", cookie.MaxAge)
			}
			if !cookie.Expires.Equal(fixedNow.Add(adminLoginCSRFLifetime)) {
				t.Errorf("login CSRF expiry: got %s", cookie.Expires)
			}

			for _, expected := range []string{
				"Studio administration",
				`action="/admin/login"`,
				`type="password"`,
				`/static/css/admin.css`,
				`name="robots" content="noindex, nofollow, noarchive"`,
			} {
				if !strings.Contains(body, expected) {
					t.Errorf("login HTML does not contain %q", expected)
				}
			}
			for _, forbidden := range []string{
				"/static/css/main.css",
				"/static/css/navigation.css",
				"/static/js/main.js",
				adminSessionCookieName,
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("login HTML exposes public/session value %q", forbidden)
				}
			}
		})
	}
}

// TestAdminRoutesRejectUnsupportedMethods verifies ServeMux's method boundary
// and confirms its generated 405 responses retain all admin security headers.
func TestAdminRoutesRejectUnsupportedMethods(t *testing.T) {
	app := newTestApplication(t)
	tests := []struct {
		// method is unsupported for path.
		method string
		// path identifies one method-aware admin endpoint.
		path string
		// expectedAllow must appear in ServeMux's Allow header.
		expectedAllow string
	}{
		{method: http.MethodPut, path: "/admin/login", expectedAllow: "GET"},
		{method: http.MethodPost, path: "/admin", expectedAllow: "GET"},
		{method: http.MethodGet, path: "/admin/logout", expectedAllow: "POST"},
	}

	for _, test := range tests {
		name := test.method + " " + test.path
		t.Run(name, func(t *testing.T) {
			request := adminHTTPNewRequest(
				test.method,
				test.path,
				nil,
				false,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusMethodNotAllowed,
				)
			}
			if !strings.Contains(
				response.Header.Get("Allow"),
				test.expectedAllow,
			) {
				t.Errorf("Allow: got %q", response.Header.Get("Allow"))
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminLoginRejectsMalformedForms checks media-type enforcement, exact
// field cardinality, unknown-field rejection, and the bounded request body.
func TestAdminLoginRejectsMalformedForms(t *testing.T) {
	csrfToken, _ := adminHTTPToken(0x21)
	csrfCookie := &http.Cookie{
		Name:  adminLoginCSRFTokenCookieName,
		Value: csrfToken,
	}
	validValues := url.Values{
		"csrf_token": {csrfToken},
		"email":      {adminHTTPTestEmail},
		"password":   {adminHTTPTestPassword},
	}

	tests := []struct {
		// name labels one protocol or parsing boundary.
		name string
		// contentType is omitted or changed for media-type failures.
		contentType string
		// body is the complete request entity.
		body string
		// expectedStatus distinguishes 415 media errors from malformed forms.
		expectedStatus int
	}{
		{
			name:           "missing content type",
			body:           validValues.Encode(),
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "plain text content type",
			contentType:    "text/plain",
			body:           validValues.Encode(),
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "malformed content type",
			contentType:    `application/x-www-form-urlencoded; charset="`,
			body:           validValues.Encode(),
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:        "missing email",
			contentType: "application/x-www-form-urlencoded",
			body: url.Values{
				"csrf_token": {csrfToken},
				"password":   {adminHTTPTestPassword},
			}.Encode(),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "duplicate email",
			contentType: "application/x-www-form-urlencoded",
			body: url.Values{
				"csrf_token": {csrfToken},
				"email": {
					adminHTTPTestEmail,
					"second@example.test",
				},
				"password": {adminHTTPTestPassword},
			}.Encode(),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "duplicate CSRF field",
			contentType: "application/x-www-form-urlencoded",
			body: url.Values{
				"csrf_token": {csrfToken, csrfToken},
				"email":      {adminHTTPTestEmail},
				"password":   {adminHTTPTestPassword},
			}.Encode(),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "unknown field",
			contentType: "application/x-www-form-urlencoded",
			body: func() string {
				values := url.Values{}
				for key, entries := range validValues {
					values[key] = append([]string(nil), entries...)
				}
				values.Set("redirect", "https://attacker.example")

				return values.Encode()
			}(),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "body exceeds cap",
			contentType: "application/x-www-form-urlencoded",
			body:        "padding=" + strings.Repeat("x", adminMaximumFormBytes),
			// MaxBytesReader commits the more precise 413 status before ParseForm
			// returns its size error to the shared malformed-form branch.
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newRecordingAdminRepository()
			app := newAdminHTTPTestApplication(
				t,
				repository,
				newTestAdminPasswordManager(t),
			)
			request := adminHTTPNewRequest(
				http.MethodPost,
				"/admin/login",
				strings.NewReader(test.body),
				false,
				csrfCookie,
			)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != test.expectedStatus {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					test.expectedStatus,
				)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
			if len(repository.createdSessions) != 0 {
				t.Error("malformed login created an administrator session")
			}
		})
	}
}

// TestAdminLoginRejectsMissingOrInvalidCSRF verifies that syntactically valid
// credentials cannot reach account lookup without the anonymous double-submit
// pair.
func TestAdminLoginRejectsMissingOrInvalidCSRF(t *testing.T) {
	validToken, _ := adminHTTPToken(0x31)
	otherToken, _ := adminHTTPToken(0x32)
	tests := []struct {
		// name labels one invalid double-submit state.
		name string
		// submitted is present exactly once in the form, even when empty.
		submitted string
		// cookie is nil when the protected browser copy is absent.
		cookie *http.Cookie
	}{
		{name: "missing cookie", submitted: validToken},
		{
			name:      "empty submitted token",
			submitted: "",
			cookie: &http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: validToken,
			},
		},
		{
			name:      "malformed cookie token",
			submitted: validToken,
			cookie: &http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: "not-base64!",
			},
		},
		{
			name:      "mismatched canonical token",
			submitted: validToken,
			cookie: &http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: otherToken,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newRecordingAdminRepository()
			app := newAdminHTTPTestApplication(
				t,
				repository,
				newTestAdminPasswordManager(t),
			)
			cookies := []*http.Cookie{}
			if test.cookie != nil {
				cookies = append(cookies, test.cookie)
			}
			request := adminHTTPPostFormRequest(
				"/admin/login",
				url.Values{
					"csrf_token": {test.submitted},
					"email":      {adminHTTPTestEmail},
					"password":   {adminHTTPTestPassword},
				},
				false,
				cookies...,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusForbidden,
				)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
			if len(repository.createdSessions) != 0 {
				t.Error("CSRF failure created an administrator session")
			}
		})
	}
}

// TestAdminLoginFailuresRemainAccountNeutral compares unknown, incorrect, and
// inactive-equivalent results. They must share one 401 body and disclose no
// repository state, password verifier, or session token.
func TestAdminLoginFailuresRemainAccountNeutral(t *testing.T) {
	loginCSRFToken, _ := adminHTTPToken(0x41)
	loginCSRFCookie := &http.Cookie{
		Name:  adminLoginCSRFTokenCookieName,
		Value: loginCSRFToken,
	}
	type failureSetup func(
		*recordingAdminRepository,
		adminPasswordManager,
	) string
	tests := []struct {
		// name labels the account-neutral failure source.
		name string
		// setup configures repository state and returns the submitted password.
		setup failureSetup
	}{
		{
			name: "unknown account",
			setup: func(
				_ *recordingAdminRepository,
				_ adminPasswordManager,
			) string {
				return adminHTTPTestPassword
			},
		},
		{
			name: "incorrect password",
			setup: func(
				repository *recordingAdminRepository,
				passwords adminPasswordManager,
			) string {
				repository.addUser(
					t,
					passwords,
					adminHTTPTestEmail,
					adminHTTPTestPassword,
					adminRoleOwner,
				)

				return "Different-Test-Password-15"
			},
		},
		{
			name: "inactive equivalent",
			setup: func(
				repository *recordingAdminRepository,
				passwords adminPasswordManager,
			) string {
				repository.addUser(
					t,
					passwords,
					adminHTTPTestEmail,
					adminHTTPTestPassword,
					adminRoleOwner,
				)
				repository.findUserError = errAdminUserNotFound

				return adminHTTPTestPassword
			},
		},
	}

	var sharedBody string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newRecordingAdminRepository()
			passwords := newTestAdminPasswordManager(t)
			password := test.setup(repository, passwords)
			app := newAdminHTTPTestApplication(t, repository, passwords)
			app.adminEntropy = adminHTTPEntropy(0x42)

			request := adminHTTPPostFormRequest(
				"/admin/login",
				url.Values{
					"csrf_token": {loginCSRFToken},
					"email":      {adminHTTPTestEmail},
					"password":   {password},
				},
				false,
				loginCSRFCookie,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusUnauthorized,
				)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "Email or password is incorrect.") {
				t.Error("login failure lacks the generic authentication message")
			}
			for _, forbidden := range []string{
				"not found",
				"inactive",
				"password hash",
				password,
			} {
				if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
					t.Errorf("login failure exposes %q", forbidden)
				}
			}
			if len(repository.createdSessions) != 0 {
				t.Error("failed authentication created a session")
			}
			for _, cookie := range response.Cookies() {
				if cookie.Name == adminSessionCookieName ||
					cookie.Name == adminSessionCSRFTokenCookieName {
					t.Errorf("failed authentication issued %q", cookie.Name)
				}
			}

			if sharedBody == "" {
				sharedBody = body
			} else if body != sharedBody {
				t.Error("account-neutral failures rendered distinguishable bodies")
			}
		})
	}
}

// TestAdminLoginSuccess verifies the complete transition from anonymous CSRF
// protection to a hash-only database session and two narrowly scoped cookies.
func TestAdminLoginSuccess(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "HTTP"
		if secure {
			name = "HTTPS"
		}

		t.Run(name, func(t *testing.T) {
			repository := newRecordingAdminRepository()
			passwords := newTestAdminPasswordManager(t)
			user := repository.addUser(
				t,
				passwords,
				adminHTTPTestEmail,
				adminHTTPTestPassword,
				adminRoleOwner,
			)
			app := newAdminHTTPTestApplication(t, repository, passwords)
			app.adminEntropy = adminHTTPEntropy(0x51, 0x52)
			fixedNow := time.Now().UTC().Truncate(time.Second)
			app.now = func() time.Time { return fixedNow }
			loginCSRFToken, _ := adminHTTPToken(0x50)
			loginCSRFCookie := &http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: loginCSRFToken,
			}

			request := adminHTTPPostFormRequest(
				"/admin/login",
				url.Values{
					"csrf_token": {loginCSRFToken},
					"email":      {" OWNER@EXAMPLE.TEST "},
					"password":   {adminHTTPTestPassword},
				},
				secure,
				loginCSRFCookie,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf(
					"status: got %d, want %d; body=%q",
					response.StatusCode,
					http.StatusSeeOther,
					recorder.Body.String(),
				)
			}
			if location := response.Header.Get("Location"); location != "/admin" {
				t.Errorf("Location: got %q, want /admin", location)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)

			sessionCookie := adminHTTPResponseCookie(
				t,
				response,
				adminSessionCookieName,
			)
			csrfCookie := adminHTTPResponseCookie(
				t,
				response,
				adminSessionCSRFTokenCookieName,
			)
			for _, cookie := range []*http.Cookie{sessionCookie, csrfCookie} {
				assertAdminHTTPCookieSecurity(t, cookie, "/admin", secure)
				if cookie.MaxAge < 1 ||
					cookie.MaxAge > int(adminSessionLifetime/time.Second) {
					t.Errorf("cookie %q MaxAge: got %d", cookie.Name, cookie.MaxAge)
				}
				if !cookie.Expires.Equal(fixedNow.Add(adminSessionLifetime)) {
					t.Errorf("cookie %q expiry: got %s", cookie.Name, cookie.Expires)
				}
			}
			if sessionCookie.Value == csrfCookie.Value {
				t.Error("session and CSRF cookies reuse one raw secret")
			}
			clearedLoginCookie := adminHTTPResponseCookie(
				t,
				response,
				adminLoginCSRFTokenCookieName,
			)
			assertAdminHTTPCookieSecurity(
				t,
				clearedLoginCookie,
				"/admin/login",
				secure,
			)
			if clearedLoginCookie.MaxAge >= 0 || clearedLoginCookie.Value != "" {
				t.Error("successful login did not expire the anonymous CSRF cookie")
			}

			if len(repository.createdSessions) != 1 {
				t.Fatalf(
					"created sessions: got %d, want 1",
					len(repository.createdSessions),
				)
			}
			stored := repository.createdSessions[0]
			_, sessionHash, sessionOK := decodeAndHashAdminToken(sessionCookie.Value)
			_, csrfHash, csrfOK := decodeAndHashAdminToken(csrfCookie.Value)
			if !sessionOK || !csrfOK {
				t.Fatal("successful login emitted a non-canonical session token")
			}
			if stored.UserID != user.ID ||
				!bytes.Equal(stored.TokenHash, sessionHash) ||
				!bytes.Equal(stored.CSRFTokenHash, csrfHash) ||
				!stored.ExpiresAt.Equal(fixedNow.Add(adminSessionLifetime)) {
				t.Errorf("stored session does not match hashed browser secrets: %#v", stored)
			}
			if bytes.Contains(stored.TokenHash, []byte(sessionCookie.Value)) ||
				bytes.Contains(stored.CSRFTokenHash, []byte(csrfCookie.Value)) {
				t.Error("repository retained a raw browser secret")
			}
			for _, privateValue := range []string{
				adminHTTPTestEmail,
				adminHTTPTestPassword,
			} {
				if strings.Contains(response.Header.Get("Location"), privateValue) {
					t.Errorf("redirect exposes %q", privateValue)
				}
				for _, cookie := range response.Cookies() {
					if strings.Contains(cookie.Value, privateValue) {
						t.Errorf("cookie %q exposes %q", cookie.Name, privateValue)
					}
				}
			}
		})
	}
}

// adminHTTPAuthenticatedFixture contains one complete in-memory account,
// session, cookie pair, and application for protected-route tests.
type adminHTTPAuthenticatedFixture struct {
	// app is the real private handler graph under test.
	app *application
	// repository retains session creation and revocation evidence.
	repository *recordingAdminRepository
	// adminInquiries records protected Stage 16 list and detail reads.
	adminInquiries *recordingAdminInquiryReader
	// user is the normalized authenticated identity.
	user adminUser
	// sessionToken is the raw browser bearer value, never stored by repository.
	sessionToken string
	// csrfToken is the independent raw browser/form value.
	csrfToken string
	// sessionHash is the repository key derived from sessionToken.
	sessionHash []byte
	// csrfHash is the persisted digest derived from csrfToken.
	csrfHash []byte
	// expiresAt is the absolute shared browser/database deadline.
	expiresAt time.Time
}

// newAdminHTTPAuthenticatedFixture creates one valid session without exercising
// login, allowing protected-route tests to isolate middleware and logout logic.
func newAdminHTTPAuthenticatedFixture(
	t *testing.T,
	role adminRole,
) adminHTTPAuthenticatedFixture {
	t.Helper()

	return newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		role,
		newRecordingAdminInquiryReader(),
	)
}

// newAdminHTTPAuthenticatedFixtureWithInquiryReader creates the same valid
// session while exposing a caller-owned inquiry reader for Stage 16 request,
// privacy, pagination, and failure assertions.
func newAdminHTTPAuthenticatedFixtureWithInquiryReader(
	t *testing.T,
	role adminRole,
	adminInquiries *recordingAdminInquiryReader,
) adminHTTPAuthenticatedFixture {
	t.Helper()

	repository := newRecordingAdminRepository()
	passwords := newTestAdminPasswordManager(t)
	user := repository.addUser(
		t,
		passwords,
		adminHTTPTestEmail,
		adminHTTPTestPassword,
		role,
	)
	app := newAdminHTTPTestApplicationWithInquiryReader(
		t,
		repository,
		passwords,
		adminInquiries,
	)
	now := time.Now().UTC()
	repository.now = func() time.Time { return now }
	app.now = func() time.Time { return now }
	sessionToken, sessionHash := adminHTTPToken(0x61)
	csrfToken, csrfHash := adminHTTPToken(0x62)
	expiresAt := now.Add(2 * time.Hour)
	err := repository.CreateSession(
		t.Context(),
		adminSession{
			TokenHash:     sessionHash,
			UserID:        user.ID,
			CSRFTokenHash: csrfHash,
			ExpiresAt:     expiresAt,
		},
	)
	if err != nil {
		t.Fatalf("create authenticated HTTP fixture: %v", err)
	}

	return adminHTTPAuthenticatedFixture{
		app:            app,
		repository:     repository,
		adminInquiries: adminInquiries,
		user:           user,
		sessionToken:   sessionToken,
		csrfToken:      csrfToken,
		sessionHash:    sessionHash,
		csrfHash:       csrfHash,
		expiresAt:      expiresAt,
	}
}

// cookies returns the two browser secrets required by requireAdmin.
func (fixture adminHTTPAuthenticatedFixture) cookies() []*http.Cookie {
	return []*http.Cookie{
		{
			Name:  adminSessionCookieName,
			Value: fixture.sessionToken,
		},
		{
			Name:  adminSessionCSRFTokenCookieName,
			Value: fixture.csrfToken,
		},
	}
}

// assertAdminHTTPRedirectClearsSession verifies the fail-closed redirect and
// both path-correct cookie deletions shared by invalid session states.
func assertAdminHTTPRedirectClearsSession(
	t *testing.T,
	response *http.Response,
) {
	t.Helper()

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
	for _, name := range []string{
		adminSessionCookieName,
		adminSessionCSRFTokenCookieName,
	} {
		cookie := adminHTTPResponseCookie(t, response, name)
		assertAdminHTTPCookieSecurity(t, cookie, "/admin", false)
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Errorf("redirect did not expire cookie %q", name)
		}
	}
}

// TestAdminDashboardRejectsUnusableSessions covers missing, malformed, revoked,
// expired, and mismatched browser/database session states.
func TestAdminDashboardRejectsUnusableSessions(t *testing.T) {
	t.Run("missing cookies", func(t *testing.T) {
		app := newTestApplication(t)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
		)
		recorder := httptest.NewRecorder()
		app.routes().ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		assertAdminHTTPRedirectClearsSession(t, response)
		assertAdminHTTPSecurityHeaders(t, response.Header)
	})

	t.Run("malformed tokens", func(t *testing.T) {
		app := newTestApplication(t)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
			&http.Cookie{Name: adminSessionCookieName, Value: "bad!"},
			&http.Cookie{Name: adminSessionCSRFTokenCookieName, Value: "bad!"},
		)
		recorder := httptest.NewRecorder()
		app.routes().ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		assertAdminHTTPRedirectClearsSession(t, response)
	})

	t.Run("revoked or unknown session", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		if err := fixture.repository.DeleteSession(
			t.Context(),
			fixture.sessionHash,
		); err != nil {
			t.Fatalf("pre-revoke fixture session: %v", err)
		}
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
			fixture.cookies()...,
		)
		recorder := httptest.NewRecorder()
		fixture.app.routes().ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		assertAdminHTTPRedirectClearsSession(t, response)
	})

	t.Run("absolute expiry", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		// The repository clock still considers the row usable, while the HTTP clock
		// reaches its exact absolute boundary and must fail closed.
		fixture.app.now = func() time.Time { return fixture.expiresAt }
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
			fixture.cookies()...,
		)
		recorder := httptest.NewRecorder()
		fixture.app.routes().ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		assertAdminHTTPRedirectClearsSession(t, response)
		if len(fixture.repository.revokedHashes) != 1 ||
			!bytes.Equal(
				fixture.repository.revokedHashes[0],
				fixture.sessionHash,
			) {
			t.Error("expired session was not revoked by its exact digest")
		}
	})

	t.Run("CSRF digest mismatch", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		otherCSRFToken, _ := adminHTTPToken(0x63)
		cookies := fixture.cookies()
		cookies[1].Value = otherCSRFToken
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
			cookies...,
		)
		recorder := httptest.NewRecorder()
		fixture.app.routes().ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		assertAdminHTTPRedirectClearsSession(t, response)
		if len(fixture.repository.revokedHashes) != 1 ||
			!bytes.Equal(
				fixture.repository.revokedHashes[0],
				fixture.sessionHash,
			) {
			t.Error("mismatched session pair was not revoked by bearer digest")
		}
	})
}

// TestAdminDashboardRendersAuthenticatedIdentity verifies both role labels, the
// session-bound logout token, and isolation from public navigation/assets.
func TestAdminDashboardRendersAuthenticatedIdentity(t *testing.T) {
	tests := []struct {
		// role is the trusted authorization value returned by the repository.
		role adminRole
		// label is the human-readable text expected in the private shell.
		label string
	}{
		{role: adminRoleOwner, label: "Owner"},
		{role: adminRoleEditor, label: "Editor"},
	}

	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			fixture := newAdminHTTPAuthenticatedFixture(t, test.role)
			request := adminHTTPNewRequest(
				http.MethodGet,
				"/admin",
				nil,
				false,
				fixture.cookies()...,
			)
			recorder := httptest.NewRecorder()
			fixture.app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusOK,
				)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
			body := recorder.Body.String()
			for _, expected := range []string{
				"Administration overview",
				adminHTTPTestEmail,
				test.label,
				`href="/admin/inquiries"`,
				"Open inquiry inbox",
				`action="/admin/logout"`,
				`/static/css/admin.css`,
				`name="robots" content="noindex, nofollow, noarchive"`,
			} {
				if !strings.Contains(body, expected) {
					t.Errorf("dashboard HTML does not contain %q", expected)
				}
			}
			if token := adminHTTPHiddenValue(
				t,
				body,
				"csrf_token",
			); token != fixture.csrfToken {
				t.Error("dashboard logout form contains the wrong CSRF token")
			}
			for _, forbidden := range []string{
				fixture.sessionToken,
				fixture.user.PasswordHash,
				adminHTTPTestPassword,
				"/static/css/main.css",
				"/static/css/navigation.css",
				"/static/js/main.js",
				"site-drawer",
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("dashboard HTML exposes public/private value %q", forbidden)
				}
			}
		})
	}
}

// TestAdminLogoutRevokesSessionAndClearsCookies verifies the complete POST-only
// logout mutation over HTTPS, including digest selection and cookie deletion.
func TestAdminLogoutRevokesSessionAndClearsCookies(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	request := adminHTTPPostFormRequest(
		"/admin/logout",
		url.Values{"csrf_token": {fixture.csrfToken}},
		true,
		fixture.cookies()...,
	)
	recorder := httptest.NewRecorder()
	fixture.app.routes().ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"status: got %d, want %d; body=%q",
			response.StatusCode,
			http.StatusSeeOther,
			recorder.Body.String(),
		)
	}
	if location := response.Header.Get("Location"); location != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", location)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
	if len(fixture.repository.revokedHashes) != 1 ||
		!bytes.Equal(
			fixture.repository.revokedHashes[0],
			fixture.sessionHash,
		) {
		t.Error("logout did not revoke the exact bearer-token digest")
	}
	for _, name := range []string{
		adminSessionCookieName,
		adminSessionCSRFTokenCookieName,
	} {
		cookie := adminHTTPResponseCookie(t, response, name)
		assertAdminHTTPCookieSecurity(t, cookie, "/admin", true)
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Errorf("logout did not expire cookie %q", name)
		}
	}
}

// TestAdminLogoutRejectsInvalidCSRF verifies that a valid bearer session cannot
// authorize logout with a different canonical form token.
func TestAdminLogoutRejectsInvalidCSRF(t *testing.T) {
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	otherToken, _ := adminHTTPToken(0x64)
	request := adminHTTPPostFormRequest(
		"/admin/logout",
		url.Values{"csrf_token": {otherToken}},
		false,
		fixture.cookies()...,
	)
	recorder := httptest.NewRecorder()
	fixture.app.routes().ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Errorf(
			"status: got %d, want %d",
			response.StatusCode,
			http.StatusForbidden,
		)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
	if len(fixture.repository.revokedHashes) != 0 {
		t.Error("bad logout CSRF revoked the authenticated session")
	}
	if len(response.Cookies()) != 0 {
		t.Error("bad logout CSRF unexpectedly changed browser cookies")
	}
}

// TestAdminRepositoryFailuresReturnServiceUnavailable verifies every database
// operation used by login, middleware, and logout fails closed without changing
// authentication cookies or exposing repository detail.
func TestAdminRepositoryFailuresReturnServiceUnavailable(t *testing.T) {
	t.Run("login user lookup", func(t *testing.T) {
		repository := newRecordingAdminRepository()
		repository.findUserError = errors.New(
			"unsafe lookup detail for " + adminHTTPTestEmail,
		)
		app := newAdminHTTPTestApplication(
			t,
			repository,
			newTestAdminPasswordManager(t),
		)
		csrfToken, _ := adminHTTPToken(0x71)
		request := adminHTTPPostFormRequest(
			"/admin/login",
			url.Values{
				"csrf_token": {csrfToken},
				"email":      {adminHTTPTestEmail},
				"password":   {adminHTTPTestPassword},
			},
			false,
			&http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: csrfToken,
			},
		)
		assertAdminHTTPServiceUnavailable(t, app, request)
	})

	t.Run("login session creation", func(t *testing.T) {
		repository := newRecordingAdminRepository()
		passwords := newTestAdminPasswordManager(t)
		repository.addUser(
			t,
			passwords,
			adminHTTPTestEmail,
			adminHTTPTestPassword,
			adminRoleOwner,
		)
		repository.createSessionErr = errors.New(
			"unsafe insert detail for " + adminHTTPTestEmail,
		)
		app := newAdminHTTPTestApplication(t, repository, passwords)
		app.adminEntropy = adminHTTPEntropy(0x72, 0x73)
		csrfToken, _ := adminHTTPToken(0x70)
		request := adminHTTPPostFormRequest(
			"/admin/login",
			url.Values{
				"csrf_token": {csrfToken},
				"email":      {adminHTTPTestEmail},
				"password":   {adminHTTPTestPassword},
			},
			false,
			&http.Cookie{
				Name:  adminLoginCSRFTokenCookieName,
				Value: csrfToken,
			},
		)
		assertAdminHTTPServiceUnavailable(t, app, request)
		if len(repository.createdSessions) != 0 {
			t.Error("failed session insertion was recorded as durable")
		}
	})

	t.Run("dashboard session lookup", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.repository.findSessionError = errors.New(
			"unsafe session lookup detail",
		)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin",
			nil,
			false,
			fixture.cookies()...,
		)
		assertAdminHTTPServiceUnavailable(t, fixture.app, request)
	})

	t.Run("logout session revocation", func(t *testing.T) {
		fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
		fixture.repository.deleteSessionErr = errors.New(
			"unsafe session delete detail",
		)
		request := adminHTTPPostFormRequest(
			"/admin/logout",
			url.Values{"csrf_token": {fixture.csrfToken}},
			false,
			fixture.cookies()...,
		)
		assertAdminHTTPServiceUnavailable(t, fixture.app, request)
		if len(fixture.repository.revokedHashes) != 0 {
			t.Error("failed session revocation was recorded as successful")
		}
	})
}

// assertAdminHTTPServiceUnavailable executes one request and checks the generic
// credential-free 503 boundary shared by administrator repository failures.
func assertAdminHTTPServiceUnavailable(
	t *testing.T,
	app *application,
	request *http.Request,
) {
	t.Helper()

	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf(
			"status: got %d, want %d; body=%q",
			response.StatusCode,
			http.StatusServiceUnavailable,
			recorder.Body.String(),
		)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
	body := recorder.Body.String()
	if !strings.Contains(body, "service temporarily unavailable") {
		t.Error("repository failure lacks generic service-unavailable text")
	}
	for _, privateValue := range []string{
		adminHTTPTestEmail,
		adminHTTPTestPassword,
		"unsafe",
	} {
		if strings.Contains(body, privateValue) {
			t.Errorf("repository failure exposes %q", privateValue)
		}
	}
	if len(response.Cookies()) != 0 {
		t.Error("repository failure unexpectedly changed authentication cookies")
	}
}

// adminHTTPErrorReader returns one controlled entropy failure so token errors
// can be checked for redaction without depending on operating-system entropy.
type adminHTTPErrorReader struct {
	// err contains deliberately unsafe detail that must not cross the helper.
	err error
}

// Read implements io.Reader and always returns the controlled failure.
func (reader adminHTTPErrorReader) Read(_ []byte) (int, error) {
	return 0, reader.err
}

// TestAdminTokenHelpers verifies generation, SHA-256 mapping, strict canonical
// decoding, fixed length, and entropy-error redaction.
func TestAdminTokenHelpers(t *testing.T) {
	expectedRaw := bytes.Repeat([]byte{0x81}, adminTokenBytes)
	expectedDigest := sha256.Sum256(expectedRaw)
	token, digest, err := generateAdminToken(bytes.NewReader(expectedRaw))
	if err != nil {
		t.Fatalf("generate deterministic admin token: %v", err)
	}
	if token != base64.RawURLEncoding.EncodeToString(expectedRaw) {
		t.Errorf("generated token: got %q", token)
	}
	if !bytes.Equal(digest, expectedDigest[:]) {
		t.Error("generated token digest does not match SHA-256")
	}

	decoded, decodedDigest, ok := decodeAndHashAdminToken(token)
	if !ok || !bytes.Equal(decoded, expectedRaw) ||
		!bytes.Equal(decodedDigest, expectedDigest[:]) {
		t.Error("canonical token did not survive strict decode and hash")
	}

	// A 32-byte raw token ends with four meaningful Base64 bits and two zero pad
	// bits. Changing only a discarded bit creates an alternative spelling that a
	// permissive decoder could map to the same bytes; Strict must reject it.
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	lastIndex := strings.IndexByte(alphabet, token[len(token)-1])
	if lastIndex < 0 || lastIndex%4 != 0 || lastIndex+1 >= len(alphabet) {
		t.Fatalf("unexpected canonical final Base64 character %q", token[len(token)-1])
	}
	nonCanonical := token[:len(token)-1] + string(alphabet[lastIndex+1])
	invalidTokens := []string{
		"",
		"not-base64!",
		token + "=",
		base64.RawURLEncoding.EncodeToString(expectedRaw[:31]),
		nonCanonical,
	}
	for _, invalid := range invalidTokens {
		raw, hash, valid := decodeAndHashAdminToken(invalid)
		if valid || raw != nil || hash != nil {
			t.Errorf("invalid token %q was accepted", invalid)
		}
	}

	unsafeEntropyDetail := "secret entropy implementation detail"
	for _, reader := range []io.Reader{
		nil,
		bytes.NewReader(expectedRaw[:adminTokenBytes-1]),
		adminHTTPErrorReader{err: errors.New(unsafeEntropyDetail)},
	} {
		generated, generatedHash, err := generateAdminToken(reader)
		if err == nil {
			t.Error("failing entropy source generated a token")
		}
		if generated != "" || generatedHash != nil {
			t.Error("failing entropy source returned partial token material")
		}
		if strings.Contains(err.Error(), unsafeEntropyDetail) {
			t.Error("token error exposes entropy-reader detail")
		}
	}
}

// TestNewApplicationRequiresAdminDependencies protects authentication and
// private Product/inquiry dependencies at construction so no protected route
// can start in a bypassable or partially wired state.
func TestNewApplicationRequiresAdminDependencies(t *testing.T) {
	inquiries := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}

	t.Run("nil admin repository", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			nil,
			newRecordingAdminProductReader(),
			newRecordingAdminProductWriter(),
			newRecordingAdminInquiryReader(),
			newRecordingAdminInquiryStatusUpdater(),
			newTestAdminPasswordManager(t),
		)
		requireErrorIs(t, err, errAdminRepositoryRequired)
		if app != nil {
			t.Error("nil admin repository returned a usable application")
		}
	})

	t.Run("nil admin product reader", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			newRecordingAdminRepository(),
			nil,
			newRecordingAdminProductWriter(),
			newRecordingAdminInquiryReader(),
			newRecordingAdminInquiryStatusUpdater(),
			newTestAdminPasswordManager(t),
		)
		requireErrorIs(t, err, errAdminProductReaderRequired)
		if app != nil {
			t.Error("nil admin product reader returned a usable application")
		}
	})

	t.Run("nil admin product writer", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			newRecordingAdminRepository(),
			newRecordingAdminProductReader(),
			nil,
			newRecordingAdminInquiryReader(),
			newRecordingAdminInquiryStatusUpdater(),
			newTestAdminPasswordManager(t),
		)
		requireErrorIs(t, err, errAdminProductWriterRequired)
		if app != nil {
			t.Error("nil admin product writer returned a usable application")
		}
	})

	t.Run("nil admin inquiry reader", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			newRecordingAdminRepository(),
			newRecordingAdminProductReader(),
			newRecordingAdminProductWriter(),
			nil,
			newRecordingAdminInquiryStatusUpdater(),
			newTestAdminPasswordManager(t),
		)
		requireErrorIs(t, err, errAdminInquiryReaderRequired)
		if app != nil {
			t.Error("nil admin inquiry reader returned a usable application")
		}
	})

	t.Run("nil admin inquiry status updater", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			newRecordingAdminRepository(),
			newRecordingAdminProductReader(),
			newRecordingAdminProductWriter(),
			newRecordingAdminInquiryReader(),
			nil,
			newTestAdminPasswordManager(t),
		)
		requireErrorIs(t, err, errAdminInquiryStatusUpdaterRequired)
		if app != nil {
			t.Error("nil admin inquiry status updater returned a usable application")
		}
	})

	t.Run("nil password manager", func(t *testing.T) {
		app, err := newApplication(
			newRecordingProductCatalogueReader(),
			inquiries,
			newRecordingAdminRepository(),
			newRecordingAdminProductReader(),
			newRecordingAdminProductWriter(),
			newRecordingAdminInquiryReader(),
			newRecordingAdminInquiryStatusUpdater(),
			nil,
		)
		requireErrorIs(t, err, errAdminPasswordManagerRequired)
		if app != nil {
			t.Error("nil password manager returned a usable application")
		}
	})
}

// TestAdminGeneratedErrorsRetainSecurityHeaders checks admin-only 404 and 405
// responses produced by ServeMux rather than an application handler.
func TestAdminGeneratedErrorsRetainSecurityHeaders(t *testing.T) {
	app := newTestApplication(t)
	tests := []struct {
		// method and path select one generated response.
		method string
		path   string
		// expectedStatus is 404 for an unknown route or 405 for a known route.
		expectedStatus int
	}{
		{
			method:         http.MethodGet,
			path:           "/admin/not-a-route",
			expectedStatus: http.StatusNotFound,
		},
		{
			method:         http.MethodPatch,
			path:           "/admin/login",
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := adminHTTPNewRequest(
				test.method,
				test.path,
				nil,
				false,
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != test.expectedStatus {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					test.expectedStatus,
				)
			}
			assertAdminHTTPSecurityHeaders(t, response.Header)
		})
	}
}

// TestAdminSecurityHeadersDoNotLeakToSimilarPublicPath verifies the prefix
// middleware distinguishes /admin from an unrelated public-looking path.
func TestAdminSecurityHeadersDoNotLeakToSimilarPublicPath(t *testing.T) {
	app := newTestApplication(t)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/administrator",
		nil,
		false,
	)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") == "no-store" ||
		response.Header.Get("Content-Security-Policy") != "" {
		t.Error("admin security policy leaked to /administrator")
	}
}
