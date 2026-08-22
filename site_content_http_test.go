package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHomepageRendersManagedSEOHeroAndFeatures verifies the complete Stage 24
// public mapping, escaping, fixed feature order, canonical links, and cache rule.
func TestHomepageRendersManagedSEOHeroAndFeatures(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	content := validRepositoryHomepageContent()
	content.StudioName = `Zafarmand <Studio>`
	content.SEOTitle = `Zafarmand & Selected Work`
	content.SEODescription = `Architecture & interiors selected by Zafarmand.`
	content.Features[2].Title = `Folded <Chair>`
	reader.setHomepage(content, nil)
	app.siteContent = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?utm_source=stage24", nil)
	request.Host = "attacker.invalid"

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Homepage status: got %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Errorf("Homepage cache policy: got %q", got)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`<title>Zafarmand &amp; Selected Work</title>`,
		`name="description"`,
		`content="Architecture &amp; interiors selected by Zafarmand."`,
		`rel="canonical"`,
		`href="/"`,
		`Zafarmand &lt;Studio&gt;`,
		`src="/homepage/hero/3"`,
		`alt="Stone interior opening toward a planted courtyard"`,
		`href="/interior-design/quiet-courtyard"`,
		`href="/architecture-design/garden-pavilion"`,
		`href="/products/folded-chair"`,
		`Folded &lt;Chair&gt;`,
		`src="/products/folded-chair/cover/6"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("managed Homepage lacks %q", expected)
		}
	}
	interiorPosition := strings.Index(body, "Quiet Courtyard")
	architecturePosition := strings.Index(body, "Garden Pavilion")
	productPosition := strings.Index(body, "Folded &lt;Chair&gt;")
	if interiorPosition < 0 || architecturePosition <= interiorPosition ||
		productPosition <= architecturePosition {
		t.Error("Homepage features do not follow Interior, Architecture, Product order")
	}
	for _, privateValue := range []string{
		"featured_product_id",
		"publication_status",
		"draft",
		"archived",
	} {
		if strings.Contains(body, privateValue) {
			t.Errorf("Homepage exposes private value %q", privateValue)
		}
	}
	if strings.Contains(body, "attacker.invalid") ||
		strings.Contains(body, "utm_source") {
		t.Error("Homepage canonical metadata trusts request Host or query values")
	}
	calls := reader.callSnapshot()
	if len(calls) != 1 || calls[0].Operation != "homepage" ||
		!calls[0].HasDeadline {
		t.Errorf("Homepage dependency calls: %#v", calls)
	}
}

// TestHomepageUsesCheckedInFallbackWithoutEligibleFeatures verifies a disabled
// managed hero and empty public feature set preserve the honest Stage 4 page.
func TestHomepageUsesCheckedInFallbackWithoutEligibleFeatures(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	app.siteContent = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("fallback Homepage status: got %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `src="`+homeFallbackHeroPath+`"`) ||
		!strings.Contains(body, `alt="`+homeFallbackHeroAlt+`"`) {
		t.Error("fallback Homepage does not use the checked-in reviewed hero")
	}
	if strings.Contains(body, `class="home-featured"`) {
		t.Error("fallback Homepage renders an empty featured-content promise")
	}
}

// TestHomepageFailsClosedOnContentProblems verifies repository diagnostics and
// malformed substituted content receive one redacted, non-cacheable 503.
func TestHomepageFailsClosedOnContentProblems(t *testing.T) {
	privateDetail := "unsafe Homepage SQL detail featured draft id=99"
	tests := []struct {
		// name identifies the dependency failure.
		name string
		// content is the substituted projection.
		content publicHomepageContent
		// err is the configured reader result.
		err error
	}{
		{
			name:    "database failure",
			content: publicHomepageContent{},
			err:     errors.New(privateDetail),
		},
		{
			name: "malformed stored SEO",
			content: publicHomepageContent{
				StudioName:     "Zafarmand",
				Descriptor:     "Design Studio",
				SEOTitle:       "",
				SEODescription: "Private draft description",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingSiteContentReader()
			reader.setHomepage(test.content, test.err)
			app.siteContent = reader
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable ||
				recorder.Body.String() != "service temporarily unavailable\n" {
				t.Errorf(
					"Homepage failure: status=%d body=%q",
					recorder.Code,
					recorder.Body.String(),
				)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Homepage failure cache policy: got %q", got)
			}
			if strings.Contains(recorder.Body.String(), privateDetail) ||
				strings.Contains(recorder.Body.String(), "Private draft") {
				t.Error("Homepage failure exposes stored or driver detail")
			}
		})
	}
}

// TestContactRendersManagedDetailsAndSEO verifies semantic optional information,
// literal mailto/tel schemes, escaping, exact canonical metadata, and no-store.
func TestContactRendersManagedDetailsAndSEO(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	contact := publicContactContent{
		Eyebrow:        `Contact & Visit`,
		Heading:        `Begin a <conversation>`,
		Introduction:   "Share the context & discipline for studio review.\nA second reviewed line.",
		Email:          "studio@example.com",
		PhoneDisplay:   "+98 21 5555 0101",
		PhoneE164:      "+982155550101",
		Address:        "North <Studio>\nTehran & Region",
		SEOTitle:       "Contact & Studio | Zafarmand",
		SEODescription: "Contact Zafarmand for architecture & design inquiries.",
	}
	reader.setContact(contact, nil)
	app.siteContent = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/contact?utm_source=stage24",
		nil,
	)
	request.Host = "attacker.invalid"

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Contact status: got %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Contact cache policy: got %q, want no-store", got)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`<title>Contact &amp; Studio | Zafarmand</title>`,
		`content="Contact Zafarmand for architecture &amp; design inquiries."`,
		`href="/contact"`,
		`Contact &amp; Visit`,
		`Begin a &lt;conversation&gt;`,
		"studio review.\nA second reviewed line.",
		`href="mailto:studio@example.com"`,
		`href="tel:&#43;982155550101"`,
		`North &lt;Studio&gt;`,
		`Tehran &amp; Region`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("managed Contact lacks %q", expected)
		}
	}
	if strings.Contains(body, `href="https://`) {
		t.Error("managed Contact invents an external direct-contact destination")
	}
	if strings.Contains(body, "attacker.invalid") ||
		strings.Contains(body, "utm_source") {
		t.Error("Contact canonical metadata trusts request Host or query values")
	}
	calls := reader.callSnapshot()
	if len(calls) != 1 || calls[0].Operation != "contact" ||
		!calls[0].HasDeadline {
		t.Errorf("Contact dependency calls: %#v", calls)
	}
}

// TestContactEscapesMailboxURIDelimiters proves administrator-managed mailbox
// punctuation cannot become mailto headers, fragments, or a second decoded
// escape sequence. The visible label remains unchanged; only the href address
// component is percent-escaped by application code.
func TestContactEscapesMailboxURIDelimiters(t *testing.T) {
	tests := []struct {
		// name identifies the URI delimiter or encoded-control form under test.
		name string
		// email is the reviewed visible mailbox value supplied by the reader.
		email string
		// escaped is the exact safe address component expected in the link.
		escaped string
	}{
		{
			name:    "query delimiter",
			email:   "studio?subject=review@example.com",
			escaped: "studio%3Fsubject=review@example.com",
		},
		{
			name:    "fragment delimiter",
			email:   "studio#private@example.com",
			escaped: "studio%23private@example.com",
		},
		{
			name:    "encoded line break text",
			email:   "studio%0d%0a@example.com",
			escaped: "studio%250d%250a@example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingSiteContentReader()
			contact := reader.contact
			contact.Email = test.email
			reader.setContact(contact, nil)
			app.siteContent = reader
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/contact", nil)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"Contact status: got %d, want 200; body=%q",
					recorder.Code,
					recorder.Body.String(),
				)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `href="mailto:`+test.escaped+`"`) {
				t.Errorf("safe mailto component missing from body: %q", body)
			}
			if strings.Contains(body, `href="mailto:`+test.email+`"`) {
				t.Errorf("raw URI delimiter reached mailto href: %q", body)
			}
			if !strings.Contains(body, test.email) {
				t.Errorf("visible reviewed mailbox changed unexpectedly: %q", body)
			}
		})
	}
}

// TestContactMethodMismatchRemainsNoStore verifies the outer privacy wrapper
// covers ServeMux's generated 405 response before no route handler executes.
func TestContactMethodMismatchRemainsNoStore(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	app.siteContent = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/contact", nil)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /contact status: got %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("PUT /contact cache policy: got %q, want no-store", got)
	}
	if calls := reader.callSnapshot(); len(calls) != 0 {
		t.Errorf("ServeMux method mismatch reached content reader %d time(s)", len(calls))
	}
}

// TestContactOmitsAbsentDirectInformation verifies the seeded empty values do
// not render a blank semantic address or non-working links.
func TestContactOmitsAbsentDirectInformation(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingSiteContentReader()
	app.siteContent = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/contact", nil)

	app.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if strings.Contains(body, `class="contact-page__details"`) ||
		strings.Contains(body, `href="mailto:`) ||
		strings.Contains(body, `href="tel:`) {
		t.Error("empty managed Contact details render a blank public region")
	}
}

// TestContactContentFailureDoesNotReflectVisitorPII proves a managed-content
// outage on a validation response returns plain generic copy rather than the
// already parsed visitor form values.
func TestContactContentFailureDoesNotReflectVisitorPII(t *testing.T) {
	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}
	app := newTestApplicationWithInquiryRepository(t, repository)
	reader := newRecordingSiteContentReader()
	app.siteContent = reader
	handler := app.routes()
	session := newContactTestSession(t, handler)
	reader.setContact(
		publicContactContent{},
		errors.New("driver detail contains visitor@example.com"),
	)
	values := validInquirySessionPostValues(session)
	values.Set("name", "Private Visitor")
	values.Set("email", "visitor@example.com")
	values.Set("message", "")
	recorder := serveInquirySubmission(handler, session, "/contact", values)

	if recorder.Code != http.StatusServiceUnavailable ||
		recorder.Body.String() != "service temporarily unavailable\n" {
		t.Errorf(
			"Contact content failure: status=%d body=%q",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Contact content failure cache policy: got %q", got)
	}
	for _, privateValue := range []string{
		"Private Visitor",
		"visitor@example.com",
		"driver detail",
	} {
		if strings.Contains(recorder.Body.String(), privateValue) {
			t.Errorf("Contact content failure exposes %q", privateValue)
		}
	}
	if submissions := repository.snapshot(); len(submissions) != 0 {
		t.Errorf("invalid form reached inquiry repository %d time(s)", len(submissions))
	}
}

// TestNonStage24PageRetainsMetadataFallback verifies Product SEO is not silently
// broadened before per-record requirements are designed.
func TestNonStage24PageRetainsMetadataFallback(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/products", nil)

	app.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, `<title>Products | Zafarmand</title>`) ||
		!strings.Contains(body, `content="Zafarmand design studio"`) {
		t.Error("non-Stage24 page lost established metadata fallback")
	}
	if strings.Contains(body, `rel="canonical"`) {
		t.Error("non-Stage24 page received an unplanned managed canonical tag")
	}
}
