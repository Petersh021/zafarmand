package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// contactTestSession contains the cookie and matching form token issued by one
// test GET /contact response.
type contactTestSession struct {
	// cookie is the HttpOnly half of the double-submit CSRF pair.
	cookie *http.Cookie
	// token is the matching base64url value emitted in the hidden form field.
	token string
	// body is the initial rendered Contact document for semantic assertions.
	body string
}

// newContactTestSession requests the real Contact route and extracts its CSRF
// cookie and hidden token.
//
// Centralizing this browser-like setup keeps POST tests focused on the request
// boundary they intend to exercise without bypassing token generation.
func newContactTestSession(
	t *testing.T,
	handler http.Handler,
) contactTestSession {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/contact",
		nil,
	)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"GET /contact status: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	var csrfCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == inquiryCSRFCookieName {
			csrfCookie = cookie
			break
		}
	}
	if csrfCookie == nil {
		t.Fatalf(
			"GET /contact did not set cookie %q",
			inquiryCSRFCookieName,
		)
	}

	body := recorder.Body.String()
	token := contactCSRFTokenFromBody(t, body)

	return contactTestSession{
		cookie: csrfCookie,
		token:  token,
		body:   body,
	}
}

// contactCSRFTokenFromBody locates the token value associated with the known
// hidden field in rendered Contact HTML.
func contactCSRFTokenFromBody(
	t *testing.T,
	body string,
) string {
	t.Helper()

	fieldPosition := strings.Index(
		body,
		`name="`+inquiryCSRFFieldName+`"`,
	)
	if fieldPosition == -1 {
		t.Fatalf(
			"response does not contain hidden field %q",
			inquiryCSRFFieldName,
		)
	}

	remainingField := body[fieldPosition:]
	valueMarker := `value="`
	valuePosition := strings.Index(
		remainingField,
		valueMarker,
	)
	if valuePosition == -1 {
		t.Fatal("CSRF hidden field does not contain a value")
	}

	valueStart := valuePosition + len(valueMarker)
	valueEnd := strings.Index(
		remainingField[valueStart:],
		`"`,
	)
	if valueEnd == -1 {
		t.Fatal("CSRF hidden field value is not closed")
	}

	return remainingField[valueStart : valueStart+valueEnd]
}

// validInquiryPostValues returns a complete valid POST body paired with the
// supplied Contact-session token.
func validInquiryPostValues(token string) url.Values {
	return url.Values{
		inquiryCSRFFieldName: {token},
		"name":               {"  Test Visitor  "},
		"email":              {"  visitor@example.com  "},
		"discipline":         {"architecture-design"},
		"message":            {"  A structural inquiry preview.  "},
	}
}

// serveInquiryPreview sends one standard URL-encoded Contact POST through the
// real router with the supplied CSRF session cookie.
func serveInquiryPreview(
	handler http.Handler,
	session contactTestSession,
	path string,
	values url.Values,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(values.Encode()),
	)
	request.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	request.AddCookie(session.cookie)

	handler.ServeHTTP(recorder, request)

	return recorder
}

// TestContactPageRoute verifies the complete initial GET contract: metadata,
// active drawer navigation, isolated presentation, semantic form structure,
// limits, trusted discipline choices, and CSRF cookie properties.
func TestContactPageRoute(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	body := session.body

	// Contact pages may later reflect personal form values, so even the initial
	// response establishes the same explicit no-store cache policy.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/contact", nil)
	handler.ServeHTTP(recorder, request)
	if cacheControl := recorder.Header().Get(
		"Cache-Control",
	); cacheControl != "no-store" {
		t.Errorf(
			"Cache-Control: got %q, want no-store",
			cacheControl,
		)
	}

	// The cookie and hidden field must hold the same valid 32-byte random token.
	if session.cookie.Value != session.token {
		t.Errorf(
			"CSRF pair differs: cookie %q, field %q",
			session.cookie.Value,
			session.token,
		)
	}
	if _, valid := decodeInquiryCSRFToken(session.token); !valid {
		t.Error("Contact form token is not a valid 32-byte base64url token")
	}
	if session.cookie.Path != "/contact" {
		t.Errorf(
			"CSRF cookie Path: got %q, want /contact",
			session.cookie.Path,
		)
	}
	if !session.cookie.HttpOnly {
		t.Error("CSRF cookie is not HttpOnly")
	}
	if session.cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf(
			"CSRF cookie SameSite: got %v, want Lax",
			session.cookie.SameSite,
		)
	}
	if session.cookie.Secure {
		t.Error("plain-HTTP development request unexpectedly sets Secure cookie")
	}

	// The Contact template owns exactly one main/skip target and one h1.
	if count := strings.Count(body, "<main"); count != 1 {
		t.Errorf("main count: got %d, want 1", count)
	}
	if count := strings.Count(
		body,
		`id="main-content"`,
	); count != 1 {
		t.Errorf("main-content id count: got %d, want 1", count)
	}
	mainElement := extractMainElement(t, body)
	if count := strings.Count(mainElement, "<h1"); count != 1 {
		t.Errorf("Contact h1 count: got %d, want 1", count)
	}
	if !strings.Contains(
		mainElement,
		`id="contact-page-title"`,
	) {
		t.Error("Contact main does not contain its labelled h1")
	}
	if !strings.Contains(
		normalizeHTMLWhitespace(mainElement),
		"Prepare your inquiry",
	) {
		t.Error("Contact h1 does not state the preview-only task")
	}

	// Only the route-specific Contact stylesheet should accompany the shared
	// foundation, while an initial GET must not invent a completed preview.
	if count := strings.Count(
		body,
		`href="/static/css/contact.css"`,
	); count != 1 {
		t.Errorf("Contact stylesheet count: got %d, want 1", count)
	}
	if strings.Contains(
		mainElement,
		`class="inquiry-preview"`,
	) {
		t.Error("initial Contact GET unexpectedly renders an inquiry preview")
	}
	if strings.Contains(
		mainElement,
		`id="contact-form-response"`,
	) {
		t.Error("initial Contact GET unexpectedly renders a response target")
	}
	if !strings.Contains(
		normalizeHTMLWhitespace(mainElement),
		"The server processes this information only to create the preview "+
			"response. It is not delivered to the studio or saved.",
	) {
		t.Error("Contact page lacks its truthful processing notice")
	}

	// The native form posts to the same canonical route and exposes every
	// handler-owned size limit as a browser hint without replacing Go validation.
	form := extractElementByMarker(
		t,
		mainElement,
		`class="inquiry-form"`,
		"form",
	)
	formOpening := extractOpeningTag(t, form)
	for _, attribute := range []string{
		`action="/contact#contact-form-response"`,
		`method="post"`,
	} {
		if !strings.Contains(formOpening, attribute) {
			t.Errorf("Contact form opening tag lacks %q", attribute)
		}
	}
	for _, fieldContract := range []string{
		`name="name"`,
		`name="email"`,
		`name="discipline"`,
		`name="message"`,
		`maxlength="100"`,
		`maxlength="254"`,
		`maxlength="3000"`,
		`type="email"`,
		`autocomplete="name"`,
		`autocomplete="email"`,
	} {
		if !strings.Contains(form, fieldContract) {
			t.Errorf("Contact form lacks %q", fieldContract)
		}
	}
	if count := strings.Count(
		form,
		`name="discipline"`,
	); count != 3 {
		t.Errorf("discipline radio count: got %d, want 3", count)
	}

	// Contact is a real studio-level drawer destination. It receives one exact
	// aria-current state because no duplicate desktop Contact link exists.
	drawerNavigation := extractElementByMarker(
		t,
		body,
		`class="drawer-navigation"`,
		"nav",
	)
	contactAnchor := extractElementByMarker(
		t,
		drawerNavigation,
		`href="/contact"`,
		"a",
	)
	if !strings.Contains(
		extractOpeningTag(t, contactAnchor),
		`aria-current="page"`,
	) {
		t.Error("drawer Contact link lacks exact-page current state")
	}
}

// TestContactCSRFTokenReuseAndTLS verifies that valid tokens survive multiple
// open tabs while direct HTTPS requests upgrade the same token with the Secure
// attribute.
func TestContactCSRFTokenReuseAndTLS(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	firstSession := newContactTestSession(t, handler)

	// A second GET carrying the valid cookie must reuse it and avoid replacing
	// the token, preserving forms already open in another browser tab.
	secondRecorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(
		http.MethodGet,
		"/contact",
		nil,
	)
	secondRequest.AddCookie(firstSession.cookie)
	handler.ServeHTTP(secondRecorder, secondRequest)
	secondToken := contactCSRFTokenFromBody(
		t,
		secondRecorder.Body.String(),
	)
	if secondToken != firstSession.token {
		t.Errorf(
			"reused CSRF token: got %q, want %q",
			secondToken,
			firstSession.token,
		)
	}
	if cookies := secondRecorder.Result().Cookies(); len(cookies) != 0 {
		t.Errorf(
			"valid CSRF session replacement cookie count: got %d, want 0",
			len(cookies),
		)
	}

	// Supplying TLS state models a later direct HTTPS request. Reissuing the same
	// token upgrades the cookie policy without invalidating the form already open
	// from the HTTP development response.
	tlsRecorder := httptest.NewRecorder()
	tlsRequest := httptest.NewRequest(http.MethodGet, "/contact", nil)
	tlsRequest.TLS = &tls.ConnectionState{}
	tlsRequest.AddCookie(firstSession.cookie)
	handler.ServeHTTP(tlsRecorder, tlsRequest)
	tlsToken := contactCSRFTokenFromBody(
		t,
		tlsRecorder.Body.String(),
	)
	if tlsToken != firstSession.token {
		t.Errorf(
			"TLS-upgraded form token: got %q, want %q",
			tlsToken,
			firstSession.token,
		)
	}
	var secureCookie *http.Cookie
	for _, cookie := range tlsRecorder.Result().Cookies() {
		if cookie.Name == inquiryCSRFCookieName {
			secureCookie = cookie
			break
		}
	}
	if secureCookie == nil {
		t.Fatal("HTTPS Contact response lacks CSRF cookie")
	}
	if !secureCookie.Secure {
		t.Error("HTTPS Contact response CSRF cookie is not Secure")
	}
	if secureCookie.Value != firstSession.token {
		t.Errorf(
			"TLS-upgraded cookie token: got %q, want %q",
			secureCookie.Value,
			firstSession.token,
		)
	}
}

// TestInquiryHelpers verifies normalization, whitelist mapping, validation
// state, preview mapping, duplicate detection, and random token shape without
// involving routing or template presentation.
func TestInquiryHelpers(t *testing.T) {
	// Normalization must read body values, trim only their outer whitespace, and
	// preserve the selected trusted machine value.
	postForm := url.Values{
		"name":       {"  Sentinel Visitor  "},
		"email":      {"  sentinel@example.com  "},
		"discipline": {"  products  "},
		"message":    {"  Sentinel message  "},
	}
	form := normalizeInquiryForm(postForm)
	if form.Name != "Sentinel Visitor" ||
		form.Email != "sentinel@example.com" ||
		form.Discipline != "products" ||
		form.Message != "Sentinel message" {
		t.Errorf("normalized form: got %#v", form)
	}

	// The exact option order is part of the Contact template contract, and the
	// preview resolves visible copy from that trusted server-owned list.
	options := inquiryDisciplineOptions()
	if len(options) != 3 {
		t.Fatalf("discipline option count: got %d, want 3", len(options))
	}
	expectedValues := []string{
		"interior-design",
		"architecture-design",
		"products",
	}
	for index, expectedValue := range expectedValues {
		if options[index].Value != expectedValue {
			t.Errorf(
				"option %d value: got %q, want %q",
				index,
				options[index].Value,
				expectedValue,
			)
		}
	}
	preview := newInquiryPreview(form)
	if preview.DisciplineLabel != "Products" ||
		preview.Name != form.Name ||
		preview.Email != form.Email ||
		preview.Message != form.Message {
		t.Errorf("preview mapping: got %#v", preview)
	}

	// A complete valid form has no errors, while repeated known values are
	// identified independently from field validation.
	if formErrors := validateInquiryForm(form); hasInquiryFormErrors(formErrors) {
		t.Errorf("valid form errors: got %#v", formErrors)
	}
	postForm.Add("email", "duplicate@example.com")
	if !inquiryFormHasDuplicateValues(postForm) {
		t.Error("duplicate known form value was not detected")
	}
	if inquiryFormHasDuplicateValues(url.Values{
		"unrelated": {"one", "two"},
	}) {
		t.Error("duplicate unrelated value was treated as a known form field")
	}

	// Token generation must always produce a decodable 32-byte value, while a
	// malformed token must fail the same decoder used by GET and POST handlers.
	token, err := newInquiryCSRFToken()
	if err != nil {
		t.Fatalf("create CSRF token: %v", err)
	}
	if _, valid := decodeInquiryCSRFToken(token); !valid {
		t.Error("new CSRF token does not satisfy its decoder")
	}
	if _, valid := decodeInquiryCSRFToken("not-a-token"); valid {
		t.Error("malformed CSRF token passed decoding")
	}
}

// TestInquiryPreviewPost verifies that a valid POST is normalized and rendered
// as a truthful 200 preview without redirecting, sending, or saving its data.
func TestInquiryPreviewPost(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquiryPostValues(session.token)

	// Conflicting query values prove the handler reads only r.PostForm after
	// ParseForm rather than the merged query-and-body collection.
	recorder := serveInquiryPreview(
		handler,
		session,
		"/contact?name=Query+Imposter&discipline=products",
		values,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"valid preview status: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Errorf("valid preview unexpectedly redirects to %q", location)
	}
	if cacheControl := recorder.Header().Get(
		"Cache-Control",
	); cacheControl != "no-store" {
		t.Errorf(
			"valid preview Cache-Control: got %q, want no-store",
			cacheControl,
		)
	}

	// The preview contains normalized body values and the trusted label, while
	// its visible notice explicitly denies both delivery and persistence.
	mainElement := extractMainElement(t, recorder.Body.String())
	preview := extractElementByMarker(
		t,
		mainElement,
		`class="inquiry-preview"`,
		"section",
	)
	normalizedPreview := normalizeHTMLWhitespace(preview)
	previewOpening := extractOpeningTag(t, preview)
	for _, responseAttribute := range []string{
		`id="contact-form-response"`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(previewOpening, responseAttribute) {
			t.Errorf(
				"valid preview response target lacks %q",
				responseAttribute,
			)
		}
	}
	for _, expected := range []string{
		"Test Visitor",
		"visitor@example.com",
		"Architecture Design",
		"A structural inquiry preview.",
		"This preview has not been delivered to the studio or saved.",
	} {
		if !strings.Contains(normalizedPreview, expected) {
			t.Errorf("inquiry preview does not contain %q", expected)
		}
	}
	if strings.Contains(preview, "Query Imposter") {
		t.Error("query-string name overrode the POST-body value")
	}
	if strings.Contains(
		strings.ToLower(preview),
		"delivered successfully",
	) {
		t.Error("preview falsely claims successful delivery")
	}
}

// TestInquiryValidationResponses exercises every required, length, address,
// encoding, and whitelist boundary through the real POST route.
func TestInquiryValidationResponses(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)

	tests := []struct {
		// name labels the validation boundary in verbose test output.
		name string
		// field is replaced in an otherwise valid form body.
		field string
		// value is the exact visitor-controlled replacement.
		value string
		// expectedError is the server-owned message associated with the field.
		expectedError string
	}{
		{
			name:          "name required",
			field:         "name",
			value:         "   ",
			expectedError: "Enter your name.",
		},
		{
			name:          "name maximum",
			field:         "name",
			value:         strings.Repeat("é", inquiryNameMaxLength+1),
			expectedError: "Name must be 100 characters or fewer.",
		},
		{
			name:          "name encoding",
			field:         "name",
			value:         string([]byte{0xff}),
			expectedError: "Name contains invalid text encoding.",
		},
		{
			name:          "email required",
			field:         "email",
			value:         "",
			expectedError: "Enter your email address.",
		},
		{
			name:          "email syntax",
			field:         "email",
			value:         "Visitor <visitor@example.com>",
			expectedError: "Enter one valid email address.",
		},
		{
			name:          "email maximum",
			field:         "email",
			value:         strings.Repeat("a", inquiryEmailMaxLength+1),
			expectedError: "Enter one valid email address.",
		},
		{
			name:          "email encoding",
			field:         "email",
			value:         string([]byte{0xff}),
			expectedError: "Email contains invalid text encoding.",
		},
		{
			name:          "discipline required",
			field:         "discipline",
			value:         "",
			expectedError: "Choose a design discipline.",
		},
		{
			name:          "discipline whitelist",
			field:         "discipline",
			value:         "landscape-design",
			expectedError: "Choose a design discipline.",
		},
		{
			name:          "message required",
			field:         "message",
			value:         "  ",
			expectedError: "Enter an inquiry message.",
		},
		{
			name:          "message maximum",
			field:         "message",
			value:         strings.Repeat("m", inquiryMessageMaxLength+1),
			expectedError: "Message must be 3000 characters or fewer.",
		},
		{
			name:          "message encoding",
			field:         "message",
			value:         string([]byte{0xff}),
			expectedError: "Message contains invalid text encoding.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validInquiryPostValues(session.token)
			values.Set(test.field, test.value)
			recorder := serveInquiryPreview(
				handler,
				session,
				"/contact",
				values,
			)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf(
					"status: got %d, want %d",
					recorder.Code,
					http.StatusUnprocessableEntity,
				)
			}

			body := recorder.Body.String()
			if !strings.Contains(body, test.expectedError) {
				t.Errorf(
					"response does not contain %q",
					test.expectedError,
				)
			}
			if !strings.Contains(
				body,
				`class="inquiry-form__error-summary"`,
			) {
				t.Error("validation response lacks error summary")
			}
			errorSummary := extractElementByMarker(
				t,
				body,
				`class="inquiry-form__error-summary"`,
				"div",
			)
			errorSummaryOpening := extractOpeningTag(t, errorSummary)
			for _, responseAttribute := range []string{
				`id="contact-form-response"`,
				`tabindex="-1"`,
			} {
				if !strings.Contains(
					errorSummaryOpening,
					responseAttribute,
				) {
					t.Errorf(
						"validation response target lacks %q",
						responseAttribute,
					)
				}
			}
			if test.field == "discipline" && !strings.Contains(
				errorSummary,
				`href="#contact-discipline-interior-design"`,
			) {
				t.Error(
					"discipline summary does not target its first radio control",
				)
			}
			if !strings.Contains(body, `aria-invalid="true"`) {
				t.Error("validation response lacks an invalid-field state")
			}
			if strings.Contains(body, `class="inquiry-preview"`) {
				t.Error("invalid request renders a completed preview")
			}
			if cacheControl := recorder.Header().Get(
				"Cache-Control",
			); cacheControl != "no-store" {
				t.Errorf(
					"Cache-Control: got %q, want no-store",
					cacheControl,
				)
			}
		})
	}
}

// TestInquiryPreviewEscapesVisitorValues renders valid markup-like input and
// proves html/template displays it as inert text in both the form and preview.
func TestInquiryPreviewEscapesVisitorValues(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquiryPostValues(session.token)
	values.Set("name", `<script>alert("name")</script>`)
	values.Set("message", `<img src=x onerror="alert('message')">`)

	recorder := serveInquiryPreview(
		handler,
		session,
		"/contact",
		values,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"escaped preview status: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	body := recorder.Body.String()
	for _, unsafeValue := range []string{
		`<script>alert("name")</script>`,
		`<img src=x onerror="alert('message')">`,
	} {
		if strings.Contains(body, unsafeValue) {
			t.Errorf("response contains raw visitor markup %q", unsafeValue)
		}
	}
	for _, escapedFragment := range []string{
		"&lt;script&gt;alert",
		"&lt;img src=x onerror=",
	} {
		if !strings.Contains(body, escapedFragment) {
			t.Errorf("response lacks escaped visitor text %q", escapedFragment)
		}
	}
}

// TestInquiryRequestBoundaries verifies malformed, ambiguous, oversized, and
// unsupported request bodies are rejected before any preview is rendered.
func TestInquiryRequestBoundaries(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)

	tests := []struct {
		// name labels one protocol boundary.
		name string
		// contentType is sent exactly as the request header value.
		contentType string
		// body is the raw visitor-controlled request entity.
		body string
		// expectedStatus documents the correct HTTP classification.
		expectedStatus int
	}{
		{
			name:           "missing content type",
			contentType:    "",
			body:           validInquiryPostValues(session.token).Encode(),
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "JSON content type",
			contentType:    "application/json",
			body:           `{}`,
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "malformed URL encoding",
			contentType:    "application/x-www-form-urlencoded",
			body:           "name=%zz",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "oversized body",
			contentType: "application/x-www-form-urlencoded",
			body: "message=" + strings.Repeat(
				"m",
				int(inquiryRequestBodyLimit),
			),
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				"/contact",
				strings.NewReader(test.body),
			)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			request.AddCookie(session.cookie)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.expectedStatus {
				t.Fatalf(
					"status: got %d, want %d",
					recorder.Code,
					test.expectedStatus,
				)
			}
			if strings.Contains(
				recorder.Body.String(),
				`class="inquiry-preview"`,
			) {
				t.Error("rejected request renders an inquiry preview")
			}
			if cacheControl := recorder.Header().Get(
				"Cache-Control",
			); cacheControl != "no-store" {
				t.Errorf(
					"Cache-Control: got %q, want no-store",
					cacheControl,
				)
			}
		})
	}

	// A repeated known value is a distinct ambiguity boundary and returns 400
	// even when each individual value would otherwise be valid.
	duplicateValues := validInquiryPostValues(session.token)
	duplicateValues.Add("email", "second@example.com")
	duplicateRecorder := serveInquiryPreview(
		handler,
		session,
		"/contact",
		duplicateValues,
	)
	if duplicateRecorder.Code != http.StatusBadRequest {
		t.Errorf(
			"duplicate field status: got %d, want %d",
			duplicateRecorder.Code,
			http.StatusBadRequest,
		)
	}
}

// TestInquiryCSRFBoundaries verifies a POST must contain one valid hidden token
// matching one valid protected cookie; query-only and malformed values fail.
func TestInquiryCSRFBoundaries(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	otherToken, err := newInquiryCSRFToken()
	if err != nil {
		t.Fatalf("create mismatched CSRF token: %v", err)
	}

	tests := []struct {
		// name labels the missing or mismatched half of the token pair.
		name string
		// path may carry an intentionally untrusted query-only token.
		path string
		// values become the exact URL-encoded POST body.
		values url.Values
		// cookie is omitted or modified to represent the boundary.
		cookie *http.Cookie
	}{
		{
			name:   "missing cookie",
			path:   "/contact",
			values: validInquiryPostValues(session.token),
		},
		{
			name: "missing body token",
			path: "/contact",
			values: func() url.Values {
				values := validInquiryPostValues(session.token)
				values.Del(inquiryCSRFFieldName)
				return values
			}(),
			cookie: session.cookie,
		},
		{
			name: "mismatched token",
			path: "/contact",
			values: func() url.Values {
				values := validInquiryPostValues(otherToken)
				return values
			}(),
			cookie: session.cookie,
		},
		{
			name: "malformed matching pair",
			path: "/contact",
			values: func() url.Values {
				values := validInquiryPostValues("malformed")
				return values
			}(),
			cookie: &http.Cookie{
				Name:  inquiryCSRFCookieName,
				Value: "malformed",
			},
		},
		{
			name: "query token ignored",
			path: "/contact?csrf_token=" + url.QueryEscape(session.token),
			values: func() url.Values {
				values := validInquiryPostValues(session.token)
				values.Del(inquiryCSRFFieldName)
				return values
			}(),
			cookie: session.cookie,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				test.path,
				strings.NewReader(test.values.Encode()),
			)
			request.Header.Set(
				"Content-Type",
				"application/x-www-form-urlencoded",
			)
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf(
					"status: got %d, want %d",
					recorder.Code,
					http.StatusForbidden,
				)
			}
			if strings.Contains(
				recorder.Body.String(),
				`class="inquiry-preview"`,
			) {
				t.Error("CSRF failure renders an inquiry preview")
			}
		})
	}
}

// TestContactMethodContracts verifies GET patterns accept HEAD, POST is a real
// route, and unrelated methods still receive ServeMux's automatic 405.
func TestContactMethodContracts(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	// HEAD shares the GET route and its privacy/content-type contract.
	headRecorder := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/contact", nil)
	handler.ServeHTTP(headRecorder, headRequest)
	if headRecorder.Code != http.StatusOK {
		t.Errorf(
			"HEAD status: got %d, want %d",
			headRecorder.Code,
			http.StatusOK,
		)
	}
	if contentType := headRecorder.Header().Get(
		"Content-Type",
	); !strings.HasPrefix(contentType, "text/html") {
		t.Errorf(
			"HEAD Content-Type: got %q, want text/html prefix",
			contentType,
		)
	}
	if cacheControl := headRecorder.Header().Get(
		"Cache-Control",
	); cacheControl != "no-store" {
		t.Errorf(
			"HEAD Cache-Control: got %q, want no-store",
			cacheControl,
		)
	}

	// PUT matches neither method-aware Contact pattern and must not fall through
	// to either rendering handler.
	putRecorder := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/contact", nil)
	handler.ServeHTTP(putRecorder, putRequest)
	if putRecorder.Code != http.StatusMethodNotAllowed {
		t.Errorf(
			"PUT status: got %d, want %d",
			putRecorder.Code,
			http.StatusMethodNotAllowed,
		)
	}
}

// TestContactTemplatePreservesMainWithoutData protects the shared skip-link
// destination if a future handler omits the optional Contact view model.
func TestContactTemplatePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"contact.html",
		pageData{
			Title:       "Empty Contact",
			CurrentPath: "/contact",
		},
	)

	body := recorder.Body.String()
	if count := strings.Count(
		body,
		`id="main-content"`,
	); count != 1 {
		t.Errorf("main-content id count: got %d, want 1", count)
	}
	mainElement := extractMainElement(t, body)
	if strings.Contains(mainElement, `class="inquiry-form"`) {
		t.Error("nil Contact data renders an empty inquiry form")
	}
	if strings.Contains(mainElement, "<h1") {
		t.Error("nil Contact data renders an empty h1")
	}
}

// TestContactPresentationDoesNotLeak verifies the Contact template and isolated
// stylesheet remain absent from every previously completed public page type.
func TestContactPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/interior-design",
		"/interior-design/interior-study-01",
		"/architecture-design",
		"/architecture-design/architecture-study-01",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status: got %d, want %d",
					recorder.Code,
					http.StatusOK,
				)
			}

			body := recorder.Body.String()
			if strings.Contains(
				body,
				`href="/static/css/contact.css"`,
			) {
				t.Error("non-Contact page loads Contact stylesheet")
			}
			if strings.Contains(body, `class="contact-page"`) {
				t.Error("non-Contact page renders Contact presentation")
			}
		})
	}
}

// TestContactStylesheetRoute verifies the existing static file server exposes
// contact.css with its correct media type and stable root selector.
func TestContactStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/contact.css",
		nil,
	)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}
	if contentType := recorder.Header().Get(
		"Content-Type",
	); !strings.HasPrefix(contentType, "text/css") {
		t.Errorf(
			"Content-Type: got %q, want text/css prefix",
			contentType,
		)
	}
	if !strings.Contains(
		recorder.Body.String(),
		".contact-page",
	) {
		t.Error("Contact stylesheet lacks its root selector")
	}
}
