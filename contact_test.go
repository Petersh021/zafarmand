package main

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
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
	// submissionToken is the separate per-form idempotency key emitted alongside
	// the CSRF field.
	submissionToken string
	// body is the initial rendered Contact document for semantic assertions.
	body string
}

// TestInquirySubmissionRepositoryFailure verifies a temporary PostgreSQL
// failure never becomes a success claim, preserves safe form values and the
// idempotency key, and lets the same request resolve as a replay after recovery.
func TestInquirySubmissionRepositoryFailure(t *testing.T) {
	privateDriverError := errors.New(
		"driver detail contains private@example.com and password=secret",
	)
	repository := &recordingInquiryRepository{
		err: privateDriverError,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquirySessionPostValues(session)
	values.Set("name", `Database <Visitor>`)
	values.Set("email", "private@example.com")

	failureRecorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		values,
	)
	if failureRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"repository failure status: got %d, want %d",
			failureRecorder.Code,
			http.StatusServiceUnavailable,
		)
	}
	if location := failureRecorder.Header().Get("Location"); location != "" {
		t.Errorf("repository failure unexpectedly redirects to %q", location)
	}
	body := failureRecorder.Body.String()
	if !strings.Contains(body, "The inquiry could not be saved") ||
		!strings.Contains(body, `role="alert"`) {
		t.Error("repository failure lacks its generic accessible message")
	}
	if strings.Contains(body, privateDriverError.Error()) ||
		strings.Contains(body, "password=secret") {
		t.Error("repository failure exposes private driver detail")
	}
	if strings.Contains(body, `Database <Visitor>`) ||
		!strings.Contains(body, `Database &lt;Visitor&gt;`) {
		t.Error("repository failure does not safely restore the visitor name")
	}
	if !strings.Contains(body, `value="private@example.com"`) {
		t.Error("repository failure does not restore the visitor email")
	}
	if restoredToken := contactHiddenFieldValue(
		t,
		body,
		inquirySubmissionFieldName,
	); restoredToken != session.submissionToken {
		t.Error("repository failure replaced the idempotency token")
	}
	for _, cookie := range failureRecorder.Result().Cookies() {
		if cookie.Name == inquirySuccessFlashCookieName && cookie.MaxAge >= 0 {
			t.Error("repository failure issues a success receipt")
		}
	}

	// The same token may represent an uncertain first database outcome. A replay
	// is therefore a successful durable state and receives the normal 303.
	repository.setOutcome(inquiryCreateResultReplay, nil)
	retryRecorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		values,
	)
	if retryRecorder.Code != http.StatusSeeOther {
		t.Fatalf(
			"repository replay status: got %d, want %d",
			retryRecorder.Code,
			http.StatusSeeOther,
		)
	}
	submissions := repository.snapshot()
	if len(submissions) != 2 {
		t.Fatalf(
			"repository attempts: got %d, want 2",
			len(submissions),
		)
	}
	if string(submissions[0].SubmissionKey) !=
		string(submissions[1].SubmissionKey) {
		t.Error("retry did not preserve the original submission key")
	}
}

// TestInquirySubmissionConflict verifies a same-key/different-payload result
// never claims success or recommends an impossible same-key retry. The 409
// response keeps escaped name/email values and supplies a fresh valid key that
// can represent a deliberately new inquiry.
func TestInquirySubmissionConflict(t *testing.T) {
	repository := &recordingInquiryRepository{
		err: errInquirySubmissionConflict,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquirySessionPostValues(session)
	values.Set("name", `Conflict <Visitor>`)
	values.Set("email", "conflict.visitor@example.com")

	conflictRecorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		values,
	)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf(
			"repository conflict status: got %d, want %d",
			conflictRecorder.Code,
			http.StatusConflict,
		)
	}
	if location := conflictRecorder.Header().Get("Location"); location != "" {
		t.Errorf("repository conflict unexpectedly redirects to %q", location)
	}
	body := conflictRecorder.Body.String()
	if !strings.Contains(body, "Please review this new inquiry") ||
		!strings.Contains(body, `role="alert"`) {
		t.Error("repository conflict lacks distinct accessible guidance")
	}
	if !strings.Contains(body, `Conflict &lt;Visitor&gt;`) ||
		!strings.Contains(body, `value="conflict.visitor@example.com"`) {
		t.Error("repository conflict does not safely restore name and email")
	}
	if strings.Contains(body, "Please try submitting it again.") {
		t.Error("permanent key conflict recommends the impossible same-key retry")
	}
	freshToken := contactHiddenFieldValue(
		t,
		body,
		inquirySubmissionFieldName,
	)
	if _, valid := decodeInquirySubmissionToken(freshToken); !valid {
		t.Error("repository conflict did not render a valid fresh submission key")
	}
	if freshToken == session.submissionToken {
		t.Error("repository conflict retained its permanently conflicting key")
	}
	for _, cookie := range conflictRecorder.Result().Cookies() {
		if cookie.Name == inquirySuccessFlashCookieName && cookie.MaxAge >= 0 {
			t.Error("repository conflict issues a success receipt")
		}
	}

	// Submitting the reviewed fields with the freshly issued key is a distinct
	// request and may now follow the normal durable-success redirect.
	repository.setOutcome(inquiryCreateResultCreated, nil)
	values.Set(inquirySubmissionFieldName, freshToken)
	retryRecorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		values,
	)
	if retryRecorder.Code != http.StatusSeeOther {
		t.Fatalf(
			"fresh-key retry status: got %d, want %d",
			retryRecorder.Code,
			http.StatusSeeOther,
		)
	}
	submissions := repository.snapshot()
	if len(submissions) != 2 {
		t.Fatalf("repository attempts: got %d, want 2", len(submissions))
	}
	if string(submissions[0].SubmissionKey) ==
		string(submissions[1].SubmissionKey) {
		t.Error("new inquiry reused the conflicting submission key")
	}
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
	submissionToken := contactHiddenFieldValue(
		t,
		body,
		inquirySubmissionFieldName,
	)

	return contactTestSession{
		cookie:          csrfCookie,
		token:           token,
		submissionToken: submissionToken,
		body:            body,
	}
}

// contactCSRFTokenFromBody locates the token value associated with the known
// hidden field in rendered Contact HTML.
func contactCSRFTokenFromBody(
	t *testing.T,
	body string,
) string {
	t.Helper()

	return contactHiddenFieldValue(
		t,
		body,
		inquiryCSRFFieldName,
	)
}

// contactHiddenFieldValue extracts one named hidden value from rendered Contact
// HTML. Keeping this small parser field-aware prevents the CSRF and submission
// tokens from being confused when their markup appears beside each other.
func contactHiddenFieldValue(
	t *testing.T,
	body string,
	fieldName string,
) string {
	t.Helper()

	fieldPosition := strings.Index(
		body,
		`name="`+fieldName+`"`,
	)
	if fieldPosition == -1 {
		t.Fatalf(
			"response does not contain hidden field %q",
			fieldName,
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

// validInquiryPostValues returns a complete valid POST body paired with the two
// explicit hidden tokens issued by a Contact GET.
func validInquiryPostValues(
	csrfToken string,
	submissionToken string,
) url.Values {
	return url.Values{
		inquiryCSRFFieldName: {csrfToken},
		inquirySubmissionFieldName: {
			submissionToken,
		},
		"name":       {"  Test Visitor  "},
		"email":      {"  visitor@example.com  "},
		"discipline": {"architecture-design"},
		"message":    {"  A structural project inquiry.  "},
	}
}

// validInquirySessionPostValues pairs both protected hidden values with the
// exact GET session that issued them.
func validInquirySessionPostValues(
	session contactTestSession,
) url.Values {
	return validInquiryPostValues(
		session.token,
		session.submissionToken,
	)
}

// serveInquirySubmission sends one standard URL-encoded Contact POST through
// the real router with the supplied CSRF session cookie.
func serveInquirySubmission(
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

	// The cookie and hidden field must hold the same valid 32-byte CSRF token,
	// while the per-form idempotency token is a separate valid 32-byte value.
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
	if _, valid := decodeInquirySubmissionToken(
		session.submissionToken,
	); !valid {
		t.Error("submission token is not a valid 32-byte base64url value")
	}
	if session.submissionToken == session.token {
		t.Error("submission idempotency token reuses the CSRF token")
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
		"Begin a conversation",
	) {
		t.Error("Contact h1 does not state the inquiry task")
	}

	// Only the route-specific Contact stylesheet should accompany the shared
	// foundation, while an initial GET must not invent a saved confirmation.
	if count := strings.Count(
		body,
		`href="/static/css/contact.css"`,
	); count != 1 {
		t.Errorf("Contact stylesheet count: got %d, want 1", count)
	}
	if strings.Contains(
		mainElement,
		`class="inquiry-confirmation"`,
	) {
		t.Error("initial Contact GET unexpectedly claims a saved inquiry")
	}
	if strings.Contains(
		mainElement,
		`id="contact-form-response"`,
	) {
		t.Error("initial Contact GET unexpectedly renders a response target")
	}
	if !strings.Contains(
		normalizeHTMLWhitespace(mainElement),
		"Submitting this form stores your inquiry for studio review. "+
			"It does not guarantee email delivery or a response time.",
	) {
		t.Error("Contact page lacks its truthful processing notice")
	}

	// A query string is not evidence of persistence. Only the signed one-time
	// receipt issued after repository success may render the saved claim.
	forgedRecorder := httptest.NewRecorder()
	forgedRequest := httptest.NewRequest(
		http.MethodGet,
		"/contact?submitted=1",
		nil,
	)
	handler.ServeHTTP(forgedRecorder, forgedRequest)
	if strings.Contains(
		forgedRecorder.Body.String(),
		`class="inquiry-confirmation"`,
	) {
		t.Error("query marker forges a saved inquiry confirmation")
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
		`name="submission_token"`,
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
		"Submit inquiry",
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
// state, duplicate detection, and both random token shapes without involving
// routing or template presentation.
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

	// The exact option order is part of the Contact template contract, and label
	// lookup resolves visible copy from that trusted server-owned list.
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
	if label, exists := inquiryDisciplineLabel(
		form.Discipline,
	); !exists || label != "Products" {
		t.Errorf(
			"discipline label: got %q, exists %t",
			label,
			exists,
		)
	}

	// A complete valid form has no errors, while repeated known values are
	// identified independently from field validation.
	if formErrors := validateInquiryForm(form); hasInquiryFormErrors(formErrors) {
		t.Errorf("valid form errors: got %#v", formErrors)
	}
	conflictingPageData := newContactPageData(
		"csrf-token",
		"submission-token",
		form,
		inquiryFormErrors{Name: "Correct this name."},
		inquirySubmissionStateSucceeded,
	)
	if !conflictingPageData.HasErrors ||
		conflictingPageData.SubmissionSucceeded ||
		conflictingPageData.SubmissionFailed ||
		conflictingPageData.SubmissionConflict {
		t.Error("field errors do not suppress conflicting repository outcomes")
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
	if _, valid := decodeInquiryCSRFToken(
		strings.Repeat("A", 42) + "B",
	); valid {
		t.Error("non-canonical CSRF token passed strict decoding")
	}
	submissionToken, err := newInquirySubmissionToken()
	if err != nil {
		t.Fatalf("create submission token: %v", err)
	}
	if _, valid := decodeInquirySubmissionToken(
		submissionToken,
	); !valid {
		t.Error("new submission token does not satisfy its decoder")
	}
	if _, valid := decodeInquirySubmissionToken("not-a-token"); valid {
		t.Error("malformed submission token passed decoding")
	}
	// Thirty-two zero bytes canonically end in A. Changing only the unused
	// padding bits to B can decode to the same bytes with permissive base64, but
	// must not create a second textual identity for one database key.
	if _, valid := decodeInquirySubmissionToken(
		strings.Repeat("A", 42) + "B",
	); valid {
		t.Error("non-canonical submission token passed strict decoding")
	}
}

// TestInquirySubmissionPost verifies that a valid POST writes only normalized
// body values, returns a 303, and reveals success only on the signed redirected
// GET without placing a visitor's name or email in the URL or receipt.
func TestInquirySubmissionPost(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquirySessionPostValues(session)
	// Hidden fields are untrusted. This valid 32-byte key deliberately contains
	// reversible visitor text so the test can prove the success cookie replaces
	// it with independent server randomness rather than serializing it.
	craftedSubmissionKey := []byte("visitor@example.com|private-data")
	if len(craftedSubmissionKey) != inquirySubmissionTokenByteLength {
		t.Fatal("crafted submission-key fixture must contain 32 bytes")
	}
	craftedSubmissionToken := base64.RawURLEncoding.EncodeToString(
		craftedSubmissionKey,
	)
	values.Set(inquirySubmissionFieldName, craftedSubmissionToken)

	// Conflicting query values prove the handler reads only r.PostForm after
	// ParseForm rather than the merged query-and-body collection.
	recorder := serveInquirySubmission(
		handler,
		session,
		"/contact?name=Query+Imposter&discipline=products",
		values,
	)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf(
			"valid submission status: got %d, want %d",
			recorder.Code,
			http.StatusSeeOther,
		)
	}
	if location := recorder.Header().Get(
		"Location",
	); location != "/contact#contact-form-response" {
		t.Errorf(
			"valid submission Location: got %q, want Contact response fragment",
			location,
		)
	}
	if cacheControl := recorder.Header().Get(
		"Cache-Control",
	); cacheControl != "no-store" {
		t.Errorf(
			"valid submission Cache-Control: got %q, want no-store",
			cacheControl,
		)
	}

	// The repository receives exactly one normalized submission. These assertions
	// explicitly protect the visitor name/email column order requested for the
	// Stage 14 handoff.
	submissions := repository.snapshot()
	if len(submissions) != 1 {
		t.Fatalf(
			"repository call count: got %d, want 1",
			len(submissions),
		)
	}
	submission := submissions[0]
	if submission.Name != "Test Visitor" ||
		submission.Email != "visitor@example.com" ||
		submission.Discipline != "architecture-design" ||
		submission.Message != "A structural project inquiry." {
		t.Errorf("persisted normalized submission: got %#v", submission)
	}
	expectedKey, valid := decodeInquirySubmissionToken(
		craftedSubmissionToken,
	)
	if !valid {
		t.Fatal("test session submission token became invalid")
	}
	if string(submission.SubmissionKey) != string(expectedKey) {
		t.Error("repository received a different submission key")
	}

	// The redirect response and receipt contain no personal values. The opaque
	// cookie is then supplied to the GET just as a browser would do.
	for _, privateValue := range []string{
		"Test Visitor",
		"visitor@example.com",
		"Query Imposter",
	} {
		if strings.Contains(recorder.Body.String(), privateValue) ||
			strings.Contains(recorder.Header().Get("Location"), privateValue) {
			t.Errorf("redirect leaks visitor value %q", privateValue)
		}
	}
	var successCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == inquirySuccessFlashCookieName {
			successCookie = cookie
			break
		}
	}
	if successCookie == nil {
		t.Fatalf("submission did not set %q", inquirySuccessFlashCookieName)
	}
	if strings.Contains(successCookie.Value, "visitor") ||
		strings.Contains(successCookie.Value, "Test") {
		t.Error("success receipt contains recognizable visitor data")
	}
	parts := strings.Split(successCookie.Value, inquirySuccessFlashSeparator)
	if len(parts) != 2 {
		t.Fatalf("success receipt part count: got %d, want 2", len(parts))
	}
	receiptNonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode success receipt nonce: %v", err)
	}
	if string(receiptNonce) == string(craftedSubmissionKey) {
		t.Error("success receipt copied the visitor-controlled submission key")
	}

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/contact", nil)
	getRequest.AddCookie(session.cookie)
	getRequest.AddCookie(successCookie)
	handler.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf(
			"redirected GET status: got %d, want %d",
			getRecorder.Code,
			http.StatusOK,
		)
	}
	confirmation := extractElementByMarker(
		t,
		getRecorder.Body.String(),
		`class="inquiry-confirmation"`,
		"section",
	)
	confirmationOpening := extractOpeningTag(t, confirmation)
	for _, attribute := range []string{
		`id="contact-form-response"`,
		`role="status"`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(confirmationOpening, attribute) {
			t.Errorf("confirmation lacks %q", attribute)
		}
	}
	confirmationText := normalizeHTMLWhitespace(confirmation)
	if !strings.Contains(
		confirmationText,
		"Your inquiry has been saved for the studio to review.",
	) {
		t.Error("redirected GET lacks truthful saved confirmation")
	}
	for _, privateValue := range []string{
		"Test Visitor",
		"visitor@example.com",
		"A structural project inquiry.",
	} {
		if strings.Contains(getRecorder.Body.String(), privateValue) {
			t.Errorf("success page reflects private value %q", privateValue)
		}
	}
}

// TestInquiryValidationResponses exercises every required, length, address,
// encoding, and whitelist boundary through the real POST route.
func TestInquiryValidationResponses(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
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
			name:          "name PostgreSQL NUL boundary",
			field:         "name",
			value:         "Visitor\x00Name",
			expectedError: "Name contains an unsupported character.",
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
			name:          "email PostgreSQL NUL boundary",
			field:         "email",
			value:         "visitor\x00@example.com",
			expectedError: "Email contains an unsupported character.",
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
		{
			name:          "message PostgreSQL NUL boundary",
			field:         "message",
			value:         "Project context\x00must stop before persistence.",
			expectedError: "Message contains an unsupported character.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validInquirySessionPostValues(session)
			values.Set(test.field, test.value)
			recorder := serveInquirySubmission(
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
			if strings.Contains(body, `class="inquiry-confirmation"`) {
				t.Error("invalid request renders a saved confirmation")
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

	if submissions := repository.snapshot(); len(submissions) != 0 {
		t.Errorf(
			"validation failures called repository %d times, want 0",
			len(submissions),
		)
	}
}

// TestInquiryValidationEscapesVisitorValues renders markup-like input in a 422
// response and proves html/template keeps the restored name and message inert.
func TestInquiryValidationEscapesVisitorValues(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
	handler := app.routes()
	session := newContactTestSession(t, handler)
	values := validInquirySessionPostValues(session)
	values.Set("name", `<script>alert("name")</script>`)
	values.Set("email", "not-an-email")
	values.Set("message", `<img src=x onerror="alert('message')">`)

	recorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		values,
	)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"escaped validation status: got %d, want %d",
			recorder.Code,
			http.StatusUnprocessableEntity,
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
	if submissions := repository.snapshot(); len(submissions) != 0 {
		t.Errorf(
			"invalid escaped form called repository %d times, want 0",
			len(submissions),
		)
	}
}

// TestInquiryRequestBoundaries verifies malformed, ambiguous, oversized, and
// unsupported request bodies are rejected before any repository write.
func TestInquiryRequestBoundaries(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
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
			body:           validInquirySessionPostValues(session).Encode(),
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
				`class="inquiry-confirmation"`,
			) {
				t.Error("rejected request renders a saved confirmation")
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
	duplicateValues := validInquirySessionPostValues(session)
	duplicateValues.Add("email", "second@example.com")
	duplicateRecorder := serveInquirySubmission(
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

	// A malformed idempotency key must not be replaced after a possible retry;
	// the handler rejects it before normalization or repository access.
	malformedTokenValues := validInquirySessionPostValues(session)
	malformedTokenValues.Set(
		inquirySubmissionFieldName,
		"malformed",
	)
	malformedTokenRecorder := serveInquirySubmission(
		handler,
		session,
		"/contact",
		malformedTokenValues,
	)
	if malformedTokenRecorder.Code != http.StatusBadRequest {
		t.Errorf(
			"malformed submission token status: got %d, want %d",
			malformedTokenRecorder.Code,
			http.StatusBadRequest,
		)
	}

	if submissions := repository.snapshot(); len(submissions) != 0 {
		t.Errorf(
			"request boundary failures called repository %d times, want 0",
			len(submissions),
		)
	}
}

// TestInquiryCSRFBoundaries verifies a POST must contain one valid hidden token
// matching one valid protected cookie; query-only and malformed values fail.
func TestInquiryCSRFBoundaries(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
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
			values: validInquirySessionPostValues(session),
		},
		{
			name: "missing body token",
			path: "/contact",
			values: func() url.Values {
				values := validInquirySessionPostValues(session)
				values.Del(inquiryCSRFFieldName)
				return values
			}(),
			cookie: session.cookie,
		},
		{
			name: "mismatched token",
			path: "/contact",
			values: func() url.Values {
				values := validInquiryPostValues(
					otherToken,
					session.submissionToken,
				)
				return values
			}(),
			cookie: session.cookie,
		},
		{
			name: "malformed matching pair",
			path: "/contact",
			values: func() url.Values {
				values := validInquiryPostValues(
					"malformed",
					session.submissionToken,
				)
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
				values := validInquirySessionPostValues(session)
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
				`class="inquiry-confirmation"`,
			) {
				t.Error("CSRF failure renders a saved confirmation")
			}
		})
	}

	if submissions := repository.snapshot(); len(submissions) != 0 {
		t.Errorf(
			"CSRF failures called repository %d times, want 0",
			len(submissions),
		)
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
