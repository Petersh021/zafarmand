package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// recordingAdminInquiryStatusCall is immutable evidence of one updater call.
// The deadline flag proves the HTTP layer bounded the database operation
// without retaining a request context after the handler returns.
type recordingAdminInquiryStatusCall struct {
	// InquiryID is the canonical positive path identity passed to persistence.
	InquiryID int64
	// Status is the exact closed-vocabulary state selected by the administrator.
	Status inquiryStatus
	// HasDeadline records whether context.WithTimeout protected the call.
	HasDeadline bool
}

// recordingAdminInquiryStatusUpdater is a concurrency-safe, database-free test
// double shared by Stage 17 tests and older application-construction helpers.
type recordingAdminInquiryStatusUpdater struct {
	// mu protects arranged errors and recorded calls under the race detector.
	mu sync.Mutex
	// calls contains independent value snapshots in request order.
	calls []recordingAdminInquiryStatusCall
	// updateErr injects one safe or deliberately unsafe dependency failure.
	updateErr error
}

// newRecordingAdminInquiryStatusUpdater returns a successful empty updater.
// Existing tests can inject it when they do not exercise the mutation route.
func newRecordingAdminInquiryStatusUpdater() *recordingAdminInquiryStatusUpdater {
	return &recordingAdminInquiryStatusUpdater{}
}

// UpdateStatus implements adminInquiryStatusUpdater and records only the
// numeric identity, trusted status value, and deadline property needed by tests.
func (updater *recordingAdminInquiryStatusUpdater) UpdateStatus(
	ctx context.Context,
	inquiryID int64,
	status inquiryStatus,
) error {
	_, hasDeadline := ctx.Deadline()

	updater.mu.Lock()
	defer updater.mu.Unlock()

	updater.calls = append(
		updater.calls,
		recordingAdminInquiryStatusCall{
			InquiryID:   inquiryID,
			Status:      status,
			HasDeadline: hasDeadline,
		},
	)

	return updater.updateErr
}

// setError arranges the next and subsequent calls to return one error without
// exposing mutable test-double fields to concurrent request tests.
func (updater *recordingAdminInquiryStatusUpdater) setError(err error) {
	updater.mu.Lock()
	defer updater.mu.Unlock()

	updater.updateErr = err
}

// callSnapshot returns an independent slice so assertions never race with a
// handler appending another call.
func (updater *recordingAdminInquiryStatusUpdater) callSnapshot() []recordingAdminInquiryStatusCall {
	updater.mu.Lock()
	defer updater.mu.Unlock()

	return append([]recordingAdminInquiryStatusCall(nil), updater.calls...)
}

// stage17HTTPResponse retains the observable result after closing the recorder
// response body, keeping every test's resource lifetime explicit.
type stage17HTTPResponse struct {
	// StatusCode is the final result from ServeMux, middleware, or handler.
	StatusCode int
	// Header includes both route output and the outer private-response policy.
	Header http.Header
	// Body is rendered HTML or a deliberately generic error message.
	Body string
}

// stage17ServeAdminRequest exercises the complete routing and middleware stack
// so method, authentication, authorization, and security headers remain covered.
func stage17ServeAdminRequest(
	t *testing.T,
	app *application,
	request *http.Request,
) stage17HTTPResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()

	return stage17HTTPResponse{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       recorder.Body.String(),
	}
}

// stage17AuthenticatedFixture creates the established real session fixture and
// replaces only its status updater with a caller-observable test double.
func stage17AuthenticatedFixture(
	t *testing.T,
	role adminRole,
	updater *recordingAdminInquiryStatusUpdater,
) adminHTTPAuthenticatedFixture {
	t.Helper()

	fixture := newAdminHTTPAuthenticatedFixture(t, role)
	fixture.app.adminInquiryStatuses = updater

	return fixture
}

// stage17ValidStatusRequest creates the exact native form emitted by the detail
// template, including the authenticated session's hidden CSRF value.
func stage17ValidStatusRequest(
	fixture adminHTTPAuthenticatedFixture,
	path string,
	status inquiryStatus,
) *http.Request {
	return adminHTTPPostFormRequest(
		path,
		url.Values{
			"csrf_token": {fixture.csrfToken},
			"status":     {string(status)},
		},
		false,
		fixture.cookies()...,
	)
}

// assertStage17Status checks both the expected outcome and the no-store browser
// policy applied to every private response, including generated errors.
func assertStage17Status(
	t *testing.T,
	response stage17HTTPResponse,
	want int,
) {
	t.Helper()

	if response.StatusCode != want {
		t.Errorf(
			"status: got %d, want %d; body=%q",
			response.StatusCode,
			want,
			response.Body,
		)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
}

// assertStage17UpdateCalls compares the exact persistence calls while keeping
// the common zero-call authorization and validation assertion concise.
func assertStage17UpdateCalls(
	t *testing.T,
	updater *recordingAdminInquiryStatusUpdater,
	want []recordingAdminInquiryStatusCall,
) {
	t.Helper()

	got := updater.callSnapshot()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status update calls: got %#v, want %#v", got, want)
	}
}

// TestAdminInquiryStatusUpdateAllowsCurrentRolesAndStatuses proves that each
// explicitly authorized role can choose every state in the closed vocabulary.
// The test also verifies the fixed Post/Redirect/Get target and repository
// timeout boundary for every successful mutation.
func TestAdminInquiryStatusUpdateAllowsCurrentRolesAndStatuses(t *testing.T) {
	roles := []adminRole{adminRoleOwner, adminRoleEditor}
	statuses := []inquiryStatus{
		inquiryStatusNew,
		inquiryStatusReviewed,
		inquiryStatusArchived,
	}

	for _, role := range roles {
		for _, status := range statuses {
			name := string(role) + "_selects_" + string(status)
			t.Run(name, func(t *testing.T) {
				updater := newRecordingAdminInquiryStatusUpdater()
				fixture := stage17AuthenticatedFixture(t, role, updater)
				request := stage17ValidStatusRequest(
					fixture,
					"/admin/inquiries/47/status",
					status,
				)
				response := stage17ServeAdminRequest(t, fixture.app, request)

				assertStage17Status(t, response, http.StatusSeeOther)
				if location := response.Header.Get("Location"); location != "/admin/inquiries/47" {
					t.Errorf(
						"Location: got %q, want /admin/inquiries/47",
						location,
					)
				}
				assertStage17UpdateCalls(
					t,
					updater,
					[]recordingAdminInquiryStatusCall{{
						InquiryID:   47,
						Status:      status,
						HasDeadline: true,
					}},
				)
			})
		}
	}
}

// TestAdminInquiryStatusUpdateRequiresAuthentication verifies that a
// syntactically valid mutation without the two browser secrets redirects to
// login before any updater can observe the inquiry identity or requested state.
func TestAdminInquiryStatusUpdateRequiresAuthentication(t *testing.T) {
	updater := newRecordingAdminInquiryStatusUpdater()
	repository := newRecordingAdminRepository()
	app := newAdminHTTPTestApplication(
		t,
		repository,
		newTestAdminPasswordManager(t),
	)
	app.adminInquiryStatuses = updater
	csrfToken, _ := adminHTTPToken(0x75)
	request := adminHTTPPostFormRequest(
		"/admin/inquiries/47/status",
		url.Values{
			"csrf_token": {csrfToken},
			"status":     {string(inquiryStatusReviewed)},
		},
		false,
	)
	response := stage17ServeAdminRequest(t, app, request)

	assertStage17Status(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", location)
	}
	assertStage17UpdateCalls(t, updater, nil)
}

// TestAdminInquiryStatusHandlerFailsClosedWithoutContext calls the handler
// directly to guard its middleware invariant. A future route regression cannot
// turn missing authentication context into an authorized mutation.
func TestAdminInquiryStatusHandlerFailsClosedWithoutContext(t *testing.T) {
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
	request := adminHTTPPostFormRequest(
		"/admin/inquiries/47/status",
		url.Values{
			"csrf_token": {fixture.csrfToken},
			"status":     {string(inquiryStatusReviewed)},
		},
		false,
	)
	request.SetPathValue("id", "47")
	recorder := httptest.NewRecorder()
	fixture.app.adminInquiryStatusUpdateHandler(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf(
			"status: got %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
	assertStage17UpdateCalls(t, updater, nil)
}

// TestAdminInquiryStatusRouteRejectsOtherMethods verifies that only POST can
// reach the mutation. ServeMux supplies the method error before authentication,
// while the outer admin policy still protects the generated response.
func TestAdminInquiryStatusRouteRejectsOtherMethods(t *testing.T) {
	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			fixture := stage17AuthenticatedFixture(
				t,
				adminRoleOwner,
				updater,
			)
			request := adminHTTPNewRequest(
				method,
				"/admin/inquiries/47/status",
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, http.StatusMethodNotAllowed)
			if !strings.Contains(response.Header.Get("Allow"), http.MethodPost) {
				t.Errorf(
					"Allow: got %q, want POST capability",
					response.Header.Get("Allow"),
				)
			}
			assertStage17UpdateCalls(t, updater, nil)
		})
	}

	t.Run("extra path segment", func(t *testing.T) {
		updater := newRecordingAdminInquiryStatusUpdater()
		fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
		request := stage17ValidStatusRequest(
			fixture,
			"/admin/inquiries/47/status/extra",
			inquiryStatusReviewed,
		)
		response := stage17ServeAdminRequest(t, fixture.app, request)

		assertStage17Status(t, response, http.StatusNotFound)
		assertStage17UpdateCalls(t, updater, nil)
	})
}

// TestAdminInquiryStatusUpdateRequiresCanonicalTarget rejects malformed,
// overflowing, and percent-encoded identities as absent resources. It also
// rejects every query spelling before parsing a form or calling persistence.
func TestAdminInquiryStatusUpdateRequiresCanonicalTarget(t *testing.T) {
	invalidPaths := []string{
		"/admin/inquiries/0/status",
		"/admin/inquiries/-1/status",
		"/admin/inquiries/+1/status",
		"/admin/inquiries/01/status",
		"/admin/inquiries/1.0/status",
		"/admin/inquiries/abc/status",
		"/admin/inquiries/%34%37/status",
		"/admin/inquiries/9223372036854775808/status",
	}

	for _, path := range invalidPaths {
		t.Run(path, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			fixture := stage17AuthenticatedFixture(t, adminRoleEditor, updater)
			request := stage17ValidStatusRequest(
				fixture,
				path,
				inquiryStatusReviewed,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, http.StatusNotFound)
			assertStage17UpdateCalls(t, updater, nil)
		})
	}

	invalidQueries := []string{
		"/admin/inquiries/47/status?",
		"/admin/inquiries/47/status?return=/admin",
		"/admin/inquiries/47/status?status=reviewed",
	}
	for _, path := range invalidQueries {
		t.Run(path, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
			request := stage17ValidStatusRequest(
				fixture,
				path,
				inquiryStatusArchived,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, http.StatusBadRequest)
			if strings.Contains(response.Body, request.URL.RawQuery) &&
				request.URL.RawQuery != "" {
				t.Error("validation response reflects the private query")
			}
			assertStage17UpdateCalls(t, updater, nil)
		})
	}
}

// TestAdminInquiryStatusUpdateRejectsMalformedForms covers media type, content
// coding, bounded body parsing, and the exact two-field cardinality contract.
// Every failure occurs before CSRF interpretation or persistence.
func TestAdminInquiryStatusUpdateRejectsMalformedForms(t *testing.T) {
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
	validValues := url.Values{
		"csrf_token": {fixture.csrfToken},
		"status":     {string(inquiryStatusReviewed)},
	}
	tests := []struct {
		// name identifies one protocol boundary.
		name string
		// body is sent unchanged rather than being rebuilt by the form helper.
		body string
		// contentType is absent when the request deliberately omits the header.
		contentType string
		// contentEncoding exercises the route's explicit no-coding policy.
		contentEncoding string
		// want distinguishes malformed input, size, and media failures.
		want int
	}{
		{
			name: "missing content type",
			body: validValues.Encode(),
			want: http.StatusUnsupportedMediaType,
		},
		{
			name:        "plain text content type",
			body:        validValues.Encode(),
			contentType: "text/plain",
			want:        http.StatusUnsupportedMediaType,
		},
		{
			name:        "malformed content type",
			body:        validValues.Encode(),
			contentType: `application/x-www-form-urlencoded; charset="`,
			want:        http.StatusUnsupportedMediaType,
		},
		{
			name:            "unsupported content encoding",
			body:            validValues.Encode(),
			contentType:     "application/x-www-form-urlencoded",
			contentEncoding: "gzip",
			want:            http.StatusUnsupportedMediaType,
		},
		{
			name:        "malformed percent encoding",
			body:        "csrf_token=%zz&status=reviewed",
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusBadRequest,
		},
		{
			name: "missing status",
			body: url.Values{
				"csrf_token": {fixture.csrfToken},
			}.Encode(),
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusBadRequest,
		},
		{
			name: "duplicate status",
			body: url.Values{
				"csrf_token": {fixture.csrfToken},
				"status":     {"new", "reviewed"},
			}.Encode(),
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusBadRequest,
		},
		{
			name: "duplicate csrf token",
			body: url.Values{
				"csrf_token": {fixture.csrfToken, fixture.csrfToken},
				"status":     {"reviewed"},
			}.Encode(),
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusBadRequest,
		},
		{
			name: "unknown field",
			body: url.Values{
				"csrf_token": {fixture.csrfToken},
				"status":     {"reviewed"},
				"return":     {"https://attacker.example"},
			}.Encode(),
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusBadRequest,
		},
		{
			name:        "body exceeds cap",
			body:        "padding=" + strings.Repeat("x", adminMaximumFormBytes),
			contentType: "application/x-www-form-urlencoded",
			want:        http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := adminHTTPNewRequest(
				http.MethodPost,
				"/admin/inquiries/47/status",
				strings.NewReader(test.body),
				false,
				fixture.cookies()...,
			)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.contentEncoding != "" {
				request.Header.Set("Content-Encoding", test.contentEncoding)
			}
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, test.want)
		})
	}
	assertStage17UpdateCalls(t, updater, nil)
}

// TestAdminInquiryStatusUpdateRejectsInvalidCSRFBeforeOtherWork proves that a
// structurally valid but forged token returns 403 before status validation or
// the updater can reveal whether an inquiry exists. Missing and duplicate
// fields are already covered by the strict-form test as 400 shape failures.
func TestAdminInquiryStatusUpdateRejectsInvalidCSRFBeforeOtherWork(t *testing.T) {
	validOtherToken, _ := adminHTTPToken(0x76)
	tests := []struct {
		// name identifies canonical mismatch versus malformed token syntax.
		name string
		// submitted is the one structurally present hidden-field value.
		submitted string
	}{
		{name: "empty token", submitted: ""},
		{name: "malformed Base64", submitted: "not-base64!"},
		{name: "wrong decoded length", submitted: "YQ"},
		{name: "padded alternate spelling", submitted: validOtherToken + "="},
		{name: "different canonical token", submitted: validOtherToken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			// Even a configured not-found result must remain unobservable because
			// the invalid token prevents the dependency call entirely.
			updater.setError(errAdminInquiryNotFound)
			fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
			request := adminHTTPPostFormRequest(
				"/admin/inquiries/999/status",
				url.Values{
					"csrf_token": {test.submitted},
					// This invalid state proves CSRF is checked first as well.
					"status": {"future-state"},
				},
				false,
				fixture.cookies()...,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, http.StatusForbidden)
			if (test.submitted != "" && strings.Contains(response.Body, test.submitted)) ||
				strings.Contains(response.Body, "future-state") {
				t.Error("CSRF response reflects an attacker-controlled form value")
			}
			assertStage17UpdateCalls(t, updater, nil)
		})
	}
}

// TestAdminInquiryStatusUpdateRejectsUnsupportedStatuses requires an exact,
// case-sensitive machine value. The handler intentionally does not trim or
// normalize crafted input into one of the database-supported states.
func TestAdminInquiryStatusUpdateRejectsUnsupportedStatuses(t *testing.T) {
	invalidStatuses := []string{
		"",
		"Reviewed",
		"reviewed ",
		" reviewed",
		"REVIEWED",
		"deleted",
		"reviewed\x00",
		"new/reviewed",
	}

	for _, status := range invalidStatuses {
		name := status
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			fixture := stage17AuthenticatedFixture(t, adminRoleEditor, updater)
			request := adminHTTPPostFormRequest(
				"/admin/inquiries/47/status",
				url.Values{
					"csrf_token": {fixture.csrfToken},
					"status":     {status},
				},
				false,
				fixture.cookies()...,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, http.StatusBadRequest)
			if status != "" && strings.Contains(response.Body, status) {
				t.Error("status validation response reflects the submitted value")
			}
			assertStage17UpdateCalls(t, updater, nil)
		})
	}
}

// TestAdminSessionCSRFTokenValidation exercises the shared mutation helper in
// isolation. Only the exact canonical 32-byte token spelling may compare equal.
func TestAdminSessionCSRFTokenValidation(t *testing.T) {
	validToken, _ := adminHTTPToken(0x77)
	otherToken, _ := adminHTTPToken(0x78)
	tests := []struct {
		// name records the expected/submitted encoding condition.
		name string
		// expected is the authenticated context value.
		expected string
		// submitted is the hidden form value.
		submitted string
		// want is true only for the identical canonical pair.
		want bool
	}{
		{
			name:      "matching canonical tokens",
			expected:  validToken,
			submitted: validToken,
			want:      true,
		},
		{name: "different token", expected: validToken, submitted: otherToken},
		{name: "empty expected", expected: "", submitted: validToken},
		{name: "empty submitted", expected: validToken, submitted: ""},
		{name: "malformed expected", expected: "!", submitted: validToken},
		{name: "malformed submitted", expected: validToken, submitted: "!"},
		{
			name:      "padded submitted spelling",
			expected:  validToken,
			submitted: validToken + "=",
		},
		{name: "short decoded token", expected: validToken, submitted: "YQ"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adminSessionCSRFTokenIsValid(
				test.expected,
				test.submitted,
			); got != test.want {
				t.Errorf("valid: got %t, want %t", got, test.want)
			}
		})
	}
}

// TestAdminInquiryStatusUpdateMapsUpdaterOutcomes keeps a missing record
// distinguishable from operational failures while ensuring no dependency text,
// status value, token, or visitor identity reaches the browser response.
func TestAdminInquiryStatusUpdateMapsUpdaterOutcomes(t *testing.T) {
	unsafeErrorText := "driver failed for visitor@example.test status=archived token=secret"
	tests := []struct {
		// name identifies the safe persistence category under test.
		name string
		// updateErr is returned after the updater records the exact call.
		updateErr error
		// want is 404 only for the explicit missing-row sentinel.
		want int
	}{
		{
			name:      "inquiry not found",
			updateErr: errAdminInquiryNotFound,
			want:      http.StatusNotFound,
		},
		{
			name:      "unsafe dependency failure",
			updateErr: errors.New(unsafeErrorText),
			want:      http.StatusServiceUnavailable,
		},
		{
			name:      "updater invalid argument contract failure",
			updateErr: errAdminInquiryStatusUpdateInvalid,
			want:      http.StatusServiceUnavailable,
		},
		{
			name:      "updater operational failure",
			updateErr: errAdminInquiryStatusUpdateFailed,
			want:      http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			updater.setError(test.updateErr)
			fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
			request := stage17ValidStatusRequest(
				fixture,
				"/admin/inquiries/47/status",
				inquiryStatusArchived,
			)
			response := stage17ServeAdminRequest(t, fixture.app, request)

			assertStage17Status(t, response, test.want)
			if response.Header.Get("Location") != "" {
				t.Errorf(
					"failed update unexpectedly redirects to %q",
					response.Header.Get("Location"),
				)
			}
			for _, privateValue := range []string{
				unsafeErrorText,
				"visitor@example.test",
				fixture.csrfToken,
				fixture.sessionToken,
			} {
				if strings.Contains(response.Body, privateValue) {
					t.Errorf("failure response exposes %q", privateValue)
				}
			}
			if test.want == http.StatusServiceUnavailable &&
				!strings.Contains(response.Body, "service temporarily unavailable") {
				t.Error("503 response does not contain the generic service message")
			}
			assertStage17UpdateCalls(
				t,
				updater,
				[]recordingAdminInquiryStatusCall{{
					InquiryID:   47,
					Status:      inquiryStatusArchived,
					HasDeadline: true,
				}},
			)
		})
	}
}

// TestAdminInquiryStatusUpdateFailsClosedWithoutUpdater verifies the runtime
// guard independently from newApplication's constructor-time nil rejection.
// This state can only be created by direct mutation in a package-level test.
func TestAdminInquiryStatusUpdateFailsClosedWithoutUpdater(t *testing.T) {
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
	fixture.app.adminInquiryStatuses = nil
	request := stage17ValidStatusRequest(
		fixture,
		"/admin/inquiries/47/status",
		inquiryStatusReviewed,
	)
	response := stage17ServeAdminRequest(t, fixture.app, request)

	assertStage17Status(t, response, http.StatusServiceUnavailable)
	if !strings.Contains(response.Body, "service temporarily unavailable") {
		t.Error("nil updater response does not contain the generic service message")
	}
	for _, privateValue := range []string{
		fixture.csrfToken,
		fixture.sessionToken,
		adminHTTPTestEmail,
	} {
		if strings.Contains(response.Body, privateValue) {
			t.Errorf("nil updater response exposes %q", privateValue)
		}
	}
	assertStage17UpdateCalls(t, updater, nil)
}

// TestAdminInquiryStatusWriterRolesFailClosed exercises the mutation's own
// allowlist with authenticated-looking context values that the real repository
// would never issue. Empty and future roles must not reach the handler.
func TestAdminInquiryStatusWriterRolesFailClosed(t *testing.T) {
	roles := []adminRole{"", "future-role"}

	for _, role := range roles {
		name := string(role)
		if name == "" {
			name = "empty role"
		}
		t.Run(name, func(t *testing.T) {
			updater := newRecordingAdminInquiryStatusUpdater()
			fixture := stage17AuthenticatedFixture(t, adminRoleOwner, updater)
			request := stage17ValidStatusRequest(
				fixture,
				"/admin/inquiries/47/status",
				inquiryStatusReviewed,
			)
			request.SetPathValue("id", "47")
			requestIdentity := authenticatedAdminRequest{
				Identity:  adminIdentity{Role: role},
				CSRFToken: fixture.csrfToken,
			}
			request = request.WithContext(context.WithValue(
				request.Context(),
				authenticatedAdminContextKey{},
				requestIdentity,
			))
			handler := requireAdminRoles(
				adminRoleOwner,
				adminRoleEditor,
			)(http.HandlerFunc(fixture.app.adminInquiryStatusUpdateHandler))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusForbidden {
				t.Errorf(
					"status: got %d, want %d",
					recorder.Code,
					http.StatusForbidden,
				)
			}
			assertStage17UpdateCalls(t, updater, nil)
		})
	}
}

// TestAdminInquiryDetailRendersExplicitStatusActions verifies the server-owned
// form contract on a real protected detail response. The current state remains
// visible but is deliberately omitted from the two available mutation buttons.
func TestAdminInquiryDetailRendersExplicitStatusActions(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	inquiry := stage16Inquiry(47)
	inquiry.Status = inquiryStatusReviewed
	reader.setInquiries(inquiry)
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleEditor,
		reader,
	)
	fixture.app.adminInquiryStatuses = updater
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries/47",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage17ServeAdminRequest(t, fixture.app, request)

	assertStage17Status(t, response, http.StatusOK)
	for _, expected := range []string{
		"Current status: Reviewed",
		`action="/admin/inquiries/47/status"`,
		`method="post"`,
		`value="new"`,
		"Mark as new",
		`value="archived"`,
		"Mark as archived",
	} {
		if !strings.Contains(response.Body, expected) {
			t.Errorf("detail HTML does not contain %q", expected)
		}
	}
	if count := strings.Count(response.Body, `name="status"`); count != 2 {
		t.Errorf("status action count: got %d, want 2", count)
	}
	for _, omitted := range []string{
		`value="reviewed"`,
		"Mark as reviewed",
		fixture.sessionToken,
		adminHTTPTestPassword,
		"submission_key",
		"<script",
	} {
		if strings.Contains(response.Body, omitted) {
			t.Errorf("detail HTML unexpectedly contains %q", omitted)
		}
	}
	if token := adminHTTPHiddenValue(
		t,
		response.Body,
		"csrf_token",
	); token != fixture.csrfToken {
		t.Error("status form contains the wrong session CSRF token")
	}
	assertStage17UpdateCalls(t, updater, nil)
}

// TestAdminInquiryDetailGETAndRefreshNeverWriteStatus protects the central
// Stage 17 behavior: reading a new inquiry does not silently mark it reviewed.
// A browser refresh repeats only the protected detail read.
func TestAdminInquiryDetailGETAndRefreshNeverWriteStatus(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	reader.setInquiries(stage16Inquiry(47))
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleOwner,
		reader,
	)
	fixture.app.adminInquiryStatuses = updater

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/inquiries/47",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage17ServeAdminRequest(t, fixture.app, request)
		assertStage17Status(t, response, http.StatusOK)
	}

	assertStage17UpdateCalls(t, updater, nil)
	listCalls, findCalls := reader.callSnapshot()
	if !reflect.DeepEqual(listCalls, []int64(nil)) {
		t.Errorf("list calls: got %v, want none", listCalls)
	}
	if !reflect.DeepEqual(findCalls, []int64{47, 47}) {
		t.Errorf("detail calls: got %v, want [47 47]", findCalls)
	}
}

// TestAdminInquiryStatusUpdateUsesPostRedirectGet follows a successful 303 and
// refreshes the resulting detail page. Only the original explicit POST may
// cross the updater boundary; both later browser requests are ordinary reads.
func TestAdminInquiryStatusUpdateUsesPostRedirectGet(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	reader.setInquiries(stage16Inquiry(47))
	updater := newRecordingAdminInquiryStatusUpdater()
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleOwner,
		reader,
	)
	fixture.app.adminInquiryStatuses = updater
	postRequest := stage17ValidStatusRequest(
		fixture,
		"/admin/inquiries/47/status",
		inquiryStatusReviewed,
	)
	postResponse := stage17ServeAdminRequest(t, fixture.app, postRequest)

	assertStage17Status(t, postResponse, http.StatusSeeOther)
	location := postResponse.Header.Get("Location")
	if location != "/admin/inquiries/47" {
		t.Fatalf("Location: got %q, want /admin/inquiries/47", location)
	}

	// The first GET follows the 303; the second models a browser refresh of the
	// destination. Neither request contains a form body or calls UpdateStatus.
	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		getRequest := adminHTTPNewRequest(
			http.MethodGet,
			location,
			nil,
			false,
			fixture.cookies()...,
		)
		getResponse := stage17ServeAdminRequest(t, fixture.app, getRequest)
		assertStage17Status(t, getResponse, http.StatusOK)
	}

	assertStage17UpdateCalls(
		t,
		updater,
		[]recordingAdminInquiryStatusCall{{
			InquiryID:   47,
			Status:      inquiryStatusReviewed,
			HasDeadline: true,
		}},
	)
	listCalls, findCalls := reader.callSnapshot()
	if len(listCalls) != 0 {
		t.Errorf("list calls: got %v, want none", listCalls)
	}
	if !reflect.DeepEqual(findCalls, []int64{47, 47}) {
		t.Errorf("detail calls: got %v, want [47 47]", findCalls)
	}
}
