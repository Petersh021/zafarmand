package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// adminSiteContentTestCSRF is a deterministic canonical 256-bit session secret
// used only by direct protected-handler tests.
var adminSiteContentTestCSRF = base64.RawURLEncoding.EncodeToString(
	bytes.Repeat([]byte{0x42}, adminTokenBytes),
)

// newAuthenticatedAdminSiteContentRequest adds the same trusted context value
// requireAdmin would establish after validating the two browser secrets.
func newAuthenticatedAdminSiteContentRequest(
	method string,
	target string,
	body *bytes.Reader,
) *http.Request {
	request := httptest.NewRequest(method, target, body)
	identity := authenticatedAdminRequest{
		Identity: adminIdentity{
			UserID: 1,
			Email:  "owner@example.com",
			Role:   adminRoleOwner,
		},
		CSRFToken: adminSiteContentTestCSRF,
	}
	ctx := context.WithValue(
		request.Context(),
		authenticatedAdminContextKey{},
		identity,
	)

	return request.WithContext(ctx)
}

// validAdminHomepageContentForm returns one exact URL-encoded Homepage edit.
func validAdminHomepageContentForm(version string) url.Values {
	return url.Values{
		"csrf_token":                       {adminSiteContentTestCSRF},
		"version":                          {version},
		"studio_name":                      {"Zafarmand"},
		"descriptor":                       {"Design Studio"},
		"hero_source":                      {"fallback"},
		"featured_interior_project_id":     {""},
		"featured_architecture_project_id": {""},
		"featured_product_id":              {""},
		"seo_title":                        {"Home | Zafarmand"},
		"seo_description":                  {"Zafarmand design studio"},
	}
}

// validAdminContactContentForm returns one exact URL-encoded Contact edit.
func validAdminContactContentForm(version string) url.Values {
	return url.Values{
		"csrf_token":      {adminSiteContentTestCSRF},
		"version":         {version},
		"eyebrow":         {"Contact"},
		"heading":         {"Begin a conversation"},
		"introduction":    {"Share the project context Zafarmand should review."},
		"contact_email":   {"studio@example.com"},
		"phone_display":   {"+98 21 5555 0101"},
		"phone_e164":      {"+982155550101"},
		"address":         {"Studio 4\nTehran"},
		"seo_title":       {"Contact | Zafarmand"},
		"seo_description": {"Contact the Zafarmand design studio"},
	}
}

// newAdminSiteContentFormRequest constructs an authenticated URL-encoded POST.
func newAdminSiteContentFormRequest(target string, values url.Values) *http.Request {
	request := newAuthenticatedAdminSiteContentRequest(
		http.MethodPost,
		target,
		bytes.NewReader([]byte(values.Encode())),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return request
}

// TestAdminSiteContentOverviewAndDetailsRender verifies the three protected GET
// pages expose canonical navigation, managed values, and no missing templates.
func TestAdminSiteContentOverviewAndDetailsRender(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	app.adminSiteContent = reader

	tests := []struct {
		// name identifies the protected representation.
		name string
		// target is the exact canonical request path.
		target string
		// handler renders the representation without routing indirection.
		handler http.HandlerFunc
		// markers are trusted copy or paths that must appear in the response.
		markers []string
	}{
		{
			name:    "overview",
			target:  adminSiteContentNavigationPath,
			handler: app.adminSiteContentOverviewHandler,
			markers: []string{"Site content", adminHomepageContentPath, adminContactContentPath},
		},
		{
			name:    "Homepage detail",
			target:  adminHomepageContentPath,
			handler: app.adminHomepageContentDetailHandler,
			markers: []string{
				"Homepage content",
				"Static fallback",
				"Home | Zafarmand",
				"Interior Design — slot not selected",
				"Architecture Design — slot not selected",
				"Product — slot not selected",
			},
		},
		{
			name:    "Contact detail",
			target:  adminContactContentPath,
			handler: app.adminContactContentDetailHandler,
			markers: []string{"Contact content", "Begin a conversation", "Not added"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := newAuthenticatedAdminSiteContentRequest(
				http.MethodGet,
				test.target,
				bytes.NewReader(nil),
			)
			test.handler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200; body=%s", response.Code, response.Body.String())
			}
			for _, marker := range test.markers {
				if !strings.Contains(response.Body.String(), marker) {
					t.Errorf("response does not contain %q", marker)
				}
			}
		})
	}
}

// TestAdminHomepageEditPreservesUnavailableSelection proves a stored reference
// that was eligible during the Homepage read remains visible and clearable when
// it disappears from the later candidate read; other eligible records remain
// ordinary choices.
func TestAdminHomepageEditPreservesUnavailableSelection(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	reader.homepageResult.FeaturedInterior = &adminHomepageFeatureSelection{
		Discipline:        homepageFeatureInterior,
		ID:                7,
		Slug:              "old-interior",
		Title:             "Old Interior",
		Classification:    "Residential",
		PublicationStatus: publishedInteriorProjectStatus,
		CoverVersion:      2,
		Eligible:          true,
	}
	reader.candidateResult = []adminHomepageFeatureCandidate{
		{
			Discipline:     homepageFeatureInterior,
			ID:             8,
			Slug:           "eligible-interior",
			Title:          "Eligible Interior",
			Classification: "Hospitality",
			SortOrder:      1,
			CoverVersion:   2,
		},
	}
	app.adminSiteContent = reader

	response := httptest.NewRecorder()
	request := newAuthenticatedAdminSiteContentRequest(
		http.MethodGet,
		adminHomepageContentEditPath,
		bytes.NewReader(nil),
	)
	app.adminHomepageContentEditHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, marker := range []string{
		"Unavailable — Old Interior",
		"Eligible Interior",
		"affect public <strong>/</strong> immediately",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("Homepage form does not contain %q", marker)
		}
	}
	if !strings.Contains(body, `value="7" selected`) {
		t.Error("stored unavailable option is not selected and clearable")
	}
}

// TestAppendStoredUnavailableAdminHomepageOptionReconcilesEligibilityRaces
// models both directions in which publication or cover eligibility can change
// between the Homepage read and candidate-list read. The option builder uses
// the later candidate identities, not the earlier eligibility snapshot, so the
// selected record is never silently replaced by None and never duplicated.
func TestAppendStoredUnavailableAdminHomepageOptionReconcilesEligibilityRaces(
	t *testing.T,
) {
	tests := []struct {
		// name identifies the eligibility transition represented by the fixture.
		name string
		// options is the candidate-list snapshot read after the Homepage record.
		options []adminHomepageFeatureOptionPageData
		// selection is the earlier stored Homepage selection snapshot.
		selection *adminHomepageFeatureSelection
		// wantUnavailable states whether the resulting sole option is the
		// application-owned unavailable representation.
		wantUnavailable bool
	}{
		{
			name:    "candidate disappears after Homepage read",
			options: nil,
			selection: &adminHomepageFeatureSelection{
				Discipline:        homepageFeatureInterior,
				ID:                7,
				Slug:              "race-interior",
				Title:             "Race Interior",
				Classification:    "Residential",
				PublicationStatus: publishedInteriorProjectStatus,
				CoverVersion:      2,
				Eligible:          true,
			},
			wantUnavailable: true,
		},
		{
			name: "candidate appears after Homepage read",
			options: []adminHomepageFeatureOptionPageData{
				{
					Value:    "7",
					Label:    "Race Interior — /race-interior",
					Selected: true,
				},
			},
			selection: &adminHomepageFeatureSelection{
				Discipline:        homepageFeatureInterior,
				ID:                7,
				Slug:              "race-interior",
				Title:             "Race Interior",
				Classification:    "Residential",
				PublicationStatus: draftInteriorProjectStatus,
				Eligible:          false,
			},
			wantUnavailable: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := appendStoredUnavailableAdminHomepageOption(
				test.options,
				test.selection,
				"7",
			)
			if len(options) != 1 {
				t.Fatalf("option count: got %d, want 1; options=%#v", len(options), options)
			}
			if !options[0].Selected || options[0].Unavailable != test.wantUnavailable {
				t.Errorf("reconciled option: got %#v, want selected with unavailable=%t", options[0], test.wantUnavailable)
			}
		})
	}
}

// TestAdminHomepageUpdateUsesCSRFValidationAndPRG verifies a successful exact
// form reaches the writer once and redirects with 303.
func TestAdminHomepageUpdateUsesCSRFValidationAndPRG(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	writer := newRecordingAdminSiteContentWriter()
	writer.homepageResult = adminSiteContentWriteResult{Version: 2}
	app.adminSiteContent = reader
	app.adminSiteContentWrites = writer

	response := httptest.NewRecorder()
	request := newAdminSiteContentFormRequest(
		adminHomepageContentPath,
		validAdminHomepageContentForm("1"),
	)
	app.adminHomepageContentUpdateHandler(response, request)
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != adminHomepageContentPath {
		t.Fatalf("response: status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if writer.homepageCalls != 1 || writer.homepageExpectedVersion != 1 ||
		writer.homepageInput.StudioName != "Zafarmand" ||
		writer.homepageInput.ManagedHeroEnabled {
		t.Errorf("writer call: calls=%d version=%d input=%#v", writer.homepageCalls, writer.homepageExpectedVersion, writer.homepageInput)
	}

	invalid := validAdminHomepageContentForm("1")
	invalid.Set("csrf_token", "wrong")
	response = httptest.NewRecorder()
	app.adminHomepageContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(adminHomepageContentPath, invalid),
	)
	if response.Code != http.StatusForbidden || writer.homepageCalls != 1 {
		t.Errorf("invalid CSRF: status=%d writer calls=%d", response.Code, writer.homepageCalls)
	}
}

// TestAdminHomepageUpdateReturnsAccessibleValidationAndFixedConflict verifies
// semantic 422 responses retain safe copy while stale input is never echoed.
func TestAdminHomepageUpdateReturnsAccessibleValidationAndFixedConflict(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	writer := newRecordingAdminSiteContentWriter()
	app.adminSiteContent = reader
	app.adminSiteContentWrites = writer

	invalid := validAdminHomepageContentForm("1")
	invalid.Set("studio_name", " Zafarmand ")
	response := httptest.NewRecorder()
	app.adminHomepageContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(adminHomepageContentPath, invalid),
	)
	if response.Code != http.StatusUnprocessableEntity || writer.homepageCalls != 0 ||
		!strings.Contains(response.Body.String(), `aria-invalid="true"`) {
		t.Fatalf("semantic response: status=%d calls=%d body=%s", response.Code, writer.homepageCalls, response.Body.String())
	}

	writer.homepageError = errAdminSiteContentWriteConflict
	secretSubmittedCopy := "PRIVATE-SUBMITTED-HOMEPAGE-COPY"
	stale := validAdminHomepageContentForm("1")
	stale.Set("descriptor", secretSubmittedCopy)
	response = httptest.NewRecorder()
	app.adminHomepageContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(adminHomepageContentPath, stale),
	)
	if response.Code != http.StatusConflict ||
		strings.Contains(response.Body.String(), secretSubmittedCopy) ||
		!strings.Contains(response.Body.String(), "Homepage content changed") {
		t.Fatalf("conflict response: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestAdminContactUpdateValidatesPublicDetailsAndPRG verifies paired phone
// validation, the immediate-public warning, and one successful mutation.
func TestAdminContactUpdateValidatesPublicDetailsAndPRG(t *testing.T) {
	app := newTestApplication(t)
	writer := newRecordingAdminSiteContentWriter()
	writer.contactResult = adminSiteContentWriteResult{Version: 2}
	app.adminSiteContentWrites = writer

	invalid := validAdminContactContentForm("1")
	invalid.Set("phone_e164", "")
	response := httptest.NewRecorder()
	app.adminContactContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(adminContactContentPath, invalid),
	)
	if response.Code != http.StatusUnprocessableEntity || writer.contactCalls != 0 ||
		!strings.Contains(response.Body.String(), "public and scrapeable") {
		t.Fatalf("invalid Contact response: status=%d calls=%d body=%s", response.Code, writer.contactCalls, response.Body.String())
	}

	response = httptest.NewRecorder()
	app.adminContactContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(
			adminContactContentPath,
			validAdminContactContentForm("1"),
		),
	)
	if response.Code != http.StatusSeeOther || writer.contactCalls != 1 ||
		writer.contactInput.ContactEmail != "studio@example.com" {
		t.Fatalf("valid Contact response: status=%d calls=%d input=%#v body=%s", response.Code, writer.contactCalls, writer.contactInput, response.Body.String())
	}
}

// TestAdminSiteContentRejectsNoncanonicalAndDuplicateForms verifies alternate
// representations and ambiguous field cardinality fail before any writer call.
func TestAdminSiteContentRejectsNoncanonicalAndDuplicateForms(t *testing.T) {
	app := newTestApplication(t)
	writer := newRecordingAdminSiteContentWriter()
	app.adminSiteContentWrites = writer

	response := httptest.NewRecorder()
	request := newAdminSiteContentFormRequest(
		adminContactContentPath+"?preview=1",
		validAdminContactContentForm("1"),
	)
	app.adminContactContentUpdateHandler(response, request)
	if response.Code != http.StatusBadRequest {
		t.Errorf("query status: got %d, want 400", response.Code)
	}

	values := validAdminContactContentForm("1")
	values.Add("heading", "ambiguous")
	response = httptest.NewRecorder()
	app.adminContactContentUpdateHandler(
		response,
		newAdminSiteContentFormRequest(adminContactContentPath, values),
	)
	if response.Code != http.StatusBadRequest || writer.contactCalls != 0 {
		t.Errorf("duplicate field: status=%d writer calls=%d", response.Code, writer.contactCalls)
	}
}

// newAdminHomepageHeroMultipartRequest constructs the exact four-part upload
// shape expected by the protected handler.
func newAdminHomepageHeroMultipartRequest(
	t *testing.T,
	version string,
	csrfToken string,
	altText string,
	image []byte,
) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"csrf_token": csrfToken,
		"version":    version,
		"alt_text":   altText,
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s multipart field: %v", name, err)
		}
	}
	part, err := writer.CreateFormFile("image", "hero.png")
	if err != nil {
		t.Fatalf("create hero file part: %v", err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatalf("write hero file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close hero multipart body: %v", err)
	}

	request := newAuthenticatedAdminSiteContentRequest(
		http.MethodPost,
		adminHomepageHeroPath,
		bytes.NewReader(body.Bytes()),
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	return request
}

// TestAdminHomepageHeroUploadNormalizesAndRedirects verifies the real image
// normalizer, optimistic writer input, immediate-public warning, and PRG result.
func TestAdminHomepageHeroUploadNormalizesAndRedirects(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	writer := newRecordingAdminSiteContentWriter()
	writer.heroResult = adminHomepageHeroWriteResult{
		HomepageVersion: 2,
		HeroVersion:     1,
	}
	app.adminSiteContent = reader
	app.adminSiteContentWrites = writer

	formResponse := httptest.NewRecorder()
	app.adminHomepageHeroFormHandler(
		formResponse,
		newAuthenticatedAdminSiteContentRequest(
			http.MethodGet,
			adminHomepageHeroPath,
			bytes.NewReader(nil),
		),
	)
	if formResponse.Code != http.StatusOK ||
		!strings.Contains(formResponse.Body.String(), "makes this image public") {
		t.Fatalf("hero form: status=%d body=%s", formResponse.Code, formResponse.Body.String())
	}

	response := httptest.NewRecorder()
	app.adminHomepageHeroUploadHandler(
		response,
		newAdminHomepageHeroMultipartRequest(
			t,
			"1",
			adminSiteContentTestCSRF,
			"A fictional managed Homepage hero",
			testAdminHomepageHeroPNG(t),
		),
	)
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != adminHomepageContentPath ||
		writer.heroCalls != 1 ||
		!isValidAdminHomepageHeroWriteInput(writer.heroInput) {
		t.Fatalf("hero upload: status=%d location=%q calls=%d input=%#v body=%s", response.Code, response.Header().Get("Location"), writer.heroCalls, writer.heroInput, response.Body.String())
	}
}

// TestAdminHomepageHeroAssetUsesExactPrivateRevision verifies safe 404 mapping
// and a successful exact no-store-compatible binary response.
func TestAdminHomepageHeroAssetUsesExactPrivateRevision(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingAdminSiteContentReader()
	reader.heroResult = validTestHomepageHeroAsset(t, 3)
	app.adminSiteContent = reader

	response := httptest.NewRecorder()
	request := newAuthenticatedAdminSiteContentRequest(
		http.MethodGet,
		adminHomepageHeroAssetPath(3),
		bytes.NewReader(nil),
	)
	request.SetPathValue("version", "3")
	app.adminHomepageHeroAssetHandler(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != reader.heroResult.ContentType ||
		!bytes.Equal(response.Body.Bytes(), reader.heroResult.Content) ||
		reader.heroVersion != 3 {
		t.Fatalf("asset response: status=%d type=%q version=%d", response.Code, response.Header().Get("Content-Type"), reader.heroVersion)
	}

	reader.heroError = errAdminHomepageHeroNotFound
	response = httptest.NewRecorder()
	request = newAuthenticatedAdminSiteContentRequest(
		http.MethodGet,
		adminHomepageHeroAssetPath(4),
		bytes.NewReader(nil),
	)
	request.SetPathValue("version", "4")
	app.adminHomepageHeroAssetHandler(response, request)
	if response.Code != http.StatusNotFound {
		t.Errorf("missing asset status: got %d, want 404", response.Code)
	}
}
