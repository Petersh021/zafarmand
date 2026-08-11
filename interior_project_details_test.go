package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestInteriorProjectDetailPresentationHelper verifies every reviewed field,
// nullable year, canonical cover path, and routing-only field exclusion.
func TestInteriorProjectDetailPresentationHelper(t *testing.T) {
	project := catalogueInteriorProject{
		ID:              19,
		PortfolioNumber: 12,
		Slug:            "courtyard-residence",
		Title:           "Courtyard Residence",
		Typology:        "Residential",
		Location:        "Tehran",
		ProjectYear:     2031,
		ProjectStatus:   "Completed",
		Description:     "A fictional reviewed detail description.",
		Cover: &interiorProjectCoverMetadata{
			Version: 5,
			Width:   1920,
			Height:  1280,
			AltText: "A fictional courtyard framed by warm interior walls",
			Caption: "Fictional courtyard view.",
		},
	}

	actual := newInteriorProjectDetailData(project)
	want := interiorProjectDetailData{
		Number:        "12",
		Title:         project.Title,
		Typology:      project.Typology,
		Location:      project.Location,
		YearLabel:     "2031",
		ProjectStatus: project.ProjectStatus,
		Description:   project.Description,
		Cover: &publicInteriorProjectCoverPageData{
			Path:    "/interior-design/courtyard-residence/cover/5",
			Width:   1920,
			Height:  1280,
			AltText: project.Cover.AltText,
			Caption: project.Cover.Caption,
		},
	}
	if !reflect.DeepEqual(actual, want) {
		t.Errorf("detail view: got %#v, want %#v", actual, want)
	}

	project.ProjectYear = 0
	project.Cover = nil
	withoutOptionalMedia := newInteriorProjectDetailData(project)
	if withoutOptionalMedia.YearLabel != "" ||
		withoutOptionalMedia.Cover != nil {
		t.Errorf("absent year/cover produced %#v", withoutOptionalMedia)
	}
}

// TestInteriorProjectDetailRouteRendersReviewedProject verifies repository
// selection, parent navigation, semantic facts, description, meaningful cover,
// caption, canonical metadata, and deadline-bounded reading as one vertical flow.
func TestInteriorProjectDetailRouteRendersReviewedProject(t *testing.T) {
	project := catalogueInteriorProject{
		ID:              41,
		PortfolioNumber: 3,
		Slug:            "covered-courtyard",
		Title:           "Covered Courtyard",
		Typology:        "Residential",
		Location:        "Tehran",
		ProjectYear:     2030,
		ProjectStatus:   "Completed",
		Description:     "First reviewed line.\nSecond reviewed line.",
		Cover: &interiorProjectCoverMetadata{
			Version: 6,
			Width:   1800,
			Height:  1200,
			AltText: "Sunlight entering a fictional courtyard living space",
			Caption: "Fictional view toward the courtyard.",
		},
	}
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{project})
	app := newTestApplication(t)
	app.interiorProjects = reader
	path := interiorProjectDetailPath(project.Slug)
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, path, nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	calls := reader.findCallSnapshot()
	if len(calls) != 1 || calls[0].Slug != project.Slug ||
		!calls[0].HasDeadline {
		t.Errorf("detail calls: got %#v, want one bounded canonical lookup", calls)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`data-current-path="/interior-design/covered-courtyard"`,
		"<title>Covered Courtyard | Zafarmand</title>",
		`href="/static/css/interior-project-detail.css"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("response does not contain %q", expected)
		}
	}
	if strings.Contains(body, `aria-current="page"`) {
		t.Error("nested detail incorrectly marks its parent as the exact page")
	}
	if count := strings.Count(body, `aria-current="location"`); count != 2 {
		t.Errorf("parent-location count: got %d, want 2", count)
	}

	mainElement := extractMainElement(t, body)
	article := extractElementByMarker(
		t,
		mainElement,
		`class="interior-project-detail__article"`,
		"article",
	)
	if !strings.Contains(
		extractOpeningTag(t, article),
		`aria-labelledby="interior-project-title"`,
	) {
		t.Error("detail article is not labelled by its h1")
	}
	heading := extractElementByMarker(
		t,
		article,
		`id="interior-project-title"`,
		"h1",
	)
	if !strings.Contains(normalizeHTMLWhitespace(heading), project.Title) ||
		strings.Count(mainElement, "<h1") != 1 {
		t.Error("detail does not contain exactly one matching h1")
	}

	facts := extractElementByMarker(
		t,
		article,
		`class="interior-project-detail__facts"`,
		"dl",
	)
	normalizedFacts := normalizeHTMLWhitespace(facts)
	for _, expected := range []string{
		"<dt>Project number</dt> <dd>03</dd>",
		"<dt>Typology</dt> <dd>Residential</dd>",
		"<dt>Project status</dt> <dd>Completed</dd>",
		"<dt>Location</dt> <dd>Tehran</dd>",
		"<dt>Year</dt> <dd>2030</dd>",
	} {
		if !strings.Contains(normalizedFacts, expected) {
			t.Errorf("facts do not contain %q", expected)
		}
	}
	if strings.Count(facts, "<dt>") != 5 ||
		strings.Contains(facts, "Publication status") {
		t.Error("facts count drifted or exposed private publication lifecycle")
	}
	description := extractElementByMarker(
		t,
		article,
		`class="interior-project-detail__description"`,
		"p",
	)
	if !strings.Contains(description, "First reviewed line.\nSecond reviewed line.") ||
		strings.Contains(article, "Additional project information is not available") {
		t.Error("reviewed multiline description was replaced by empty-state copy")
	}

	figure := extractElementByMarker(
		t,
		article,
		`class="interior-project-detail__media interior-project-detail__media--image"`,
		"figure",
	)
	for _, expected := range []string{
		`src="/interior-design/covered-courtyard/cover/6"`,
		`width="1800"`,
		`height="1200"`,
		`alt="Sunlight entering a fictional courtyard living space"`,
		`decoding="async"`,
		"Fictional view toward the courtyard.",
	} {
		if !strings.Contains(normalizeHTMLWhitespace(figure), expected) {
			t.Errorf("cover figure does not contain %q", expected)
		}
	}
	if strings.Contains(extractOpeningTag(t, figure), `aria-hidden=`) ||
		strings.Contains(article, "interior-project-detail__media-number") {
		t.Error("meaningful cover is hidden or rendered with decorative fallback")
	}
	back := extractElementByMarker(
		t,
		article,
		`href="/interior-design#selected-work"`,
		"a",
	)
	if !strings.Contains(normalizeHTMLWhitespace(back), "Back to Interior Design") {
		t.Error("detail back link does not name its real fragment destination")
	}
}

// TestInteriorProjectDetailRouteOmitsAbsentOptionalFacts verifies SQL NULL year,
// empty location/description, and missing cover produce no invented facts,
// dangling elements, or inaccessible image alternative.
func TestInteriorProjectDetailRouteOmitsAbsentOptionalFacts(t *testing.T) {
	project := validCatalogueInteriorProject(51, 1, "minimal-interior")
	project.Location = ""
	project.ProjectYear = 0
	project.Description = ""
	project.Cover = nil
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{project})
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			interiorProjectDetailPath(project.Slug),
			nil,
		),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	facts := extractElementByMarker(
		t,
		mainElement,
		`class="interior-project-detail__facts"`,
		"dl",
	)
	if strings.Count(facts, "<dt>") != 3 ||
		strings.Contains(facts, "<dt>Location</dt>") ||
		strings.Contains(facts, "<dt>Year</dt>") {
		t.Error("absent optional facts emitted Location or Year rows")
	}
	if !strings.Contains(
		normalizeHTMLWhitespace(mainElement),
		"Additional project information is not available yet.",
	) || strings.Contains(mainElement, "interior-project-detail__description") {
		t.Error("absent description did not select its honest empty state")
	}
	if strings.Contains(mainElement, "<img") || strings.Contains(mainElement, "<figure") {
		t.Error("missing cover emitted meaningful image markup")
	}
	fallback := extractElementByMarker(
		t,
		mainElement,
		`class="interior-project-detail__media"`,
		"div",
	)
	if !strings.Contains(extractOpeningTag(t, fallback), `aria-hidden="true"`) ||
		!strings.Contains(normalizeHTMLWhitespace(fallback), "01") {
		t.Error("decorative cover fallback is not hidden or numbered")
	}
}

// TestInteriorProjectDetailEscapesManagedContent proves repository values remain
// inert across headings, facts, description, image attributes, and captions.
func TestInteriorProjectDetailEscapesManagedContent(t *testing.T) {
	project := validCatalogueInteriorProject(61, 1, "escaped-interior")
	project.Title = "<script>Unsafe title</script>"
	project.Typology = "<b>Unsafe typology</b>"
	project.Location = "<em>Unsafe location</em>"
	project.ProjectStatus = "<img src=x onerror=alert(1)>"
	project.Description = "<svg onload=alert(2)>Unsafe description</svg>"
	project.Cover = &interiorProjectCoverMetadata{
		Version: 1,
		Width:   4,
		Height:  3,
		AltText: `A room " onload="alert(3)`,
		Caption: "<iframe>Unsafe caption</iframe>",
	}
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{project})
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			interiorProjectDetailPath(project.Slug),
			nil,
		),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	for _, forbidden := range []string{
		"<script>Unsafe",
		"<b>Unsafe",
		"<em>Unsafe",
		"<img src=x onerror",
		"<svg onload",
		`onload="alert(3)`,
		"<iframe>Unsafe",
	} {
		if strings.Contains(mainElement, forbidden) {
			t.Errorf("managed value became active markup %q", forbidden)
		}
	}
	for _, escaped := range []string{
		"&lt;script&gt;Unsafe title&lt;/script&gt;",
		"&lt;b&gt;Unsafe typology&lt;/b&gt;",
		"&lt;em&gt;Unsafe location&lt;/em&gt;",
		"&lt;svg onload=alert(2)&gt;Unsafe description&lt;/svg&gt;",
		"&lt;iframe&gt;Unsafe caption&lt;/iframe&gt;",
		"&#34; onload=&#34;alert(3)",
	} {
		if !strings.Contains(mainElement, escaped) {
			t.Errorf("escaped response does not contain %q", escaped)
		}
	}
}

// TestInteriorProjectDetailHandlerUsesPathValue proves ServeMux's decoded
// wildcard, not manual URL splitting, selects the repository record.
func TestInteriorProjectDetailHandlerUsesPathValue(t *testing.T) {
	first := validCatalogueInteriorProject(71, 1, "first-interior")
	first.Title = "First Interior"
	second := validCatalogueInteriorProject(72, 2, "second-interior")
	second.Title = "Second Interior"
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{first, second})
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/interior-design/first-interior",
		nil,
	)
	request.SetPathValue("slug", second.Slug)

	app.interiorProjectDetailHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	heading := extractElementByMarker(
		t,
		extractMainElement(t, recorder.Body.String()),
		`id="interior-project-title"`,
		"h1",
	)
	if !strings.Contains(heading, second.Title) || strings.Contains(heading, first.Title) {
		t.Error("handler parsed URL.Path instead of using request PathValue")
	}
}

// TestInteriorProjectDetailHandlerErrors verifies canonical input rejection,
// public not-found privacy, redacted dependency failure, malformed injected
// results, missing dependency, and nested route rejection.
func TestInteriorProjectDetailHandlerErrors(t *testing.T) {
	invalidSlugs := []string{
		"",
		"Uppercase-Slug",
		"double--hyphen",
		"slash/inside",
		strings.Repeat("a", interiorProjectSlugMaximumLength+1),
	}
	for _, slug := range invalidSlugs {
		t.Run("invalid "+slug, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			app := newTestApplication(t)
			app.interiorProjects = reader
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/interior-design/x", nil)
			request.SetPathValue("slug", slug)
			app.interiorProjectDetailHandler(recorder, request)
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want 404", recorder.Code)
			}
			if calls := reader.findCallSnapshot(); len(calls) != 0 {
				t.Errorf("invalid slug reached reader: %#v", calls)
			}
		})
	}

	unsafeDetail := "postgres://private-interior-detail"
	tests := []struct {
		// name identifies the dependency outcome.
		name string
		// configure changes the app or reader.
		configure func(*application, *recordingInteriorProjectCatalogueReader)
		// want is the expected HTTP status.
		want int
	}{
		{name: "unknown", want: http.StatusNotFound},
		{name: "repository failure", want: http.StatusServiceUnavailable, configure: func(
			_ *application,
			reader *recordingInteriorProjectCatalogueReader,
		) {
			reader.setErrors(nil, errors.New(unsafeDetail))
		}},
		{name: "mismatched result", want: http.StatusServiceUnavailable, configure: func(
			_ *application,
			reader *recordingInteriorProjectCatalogueReader,
		) {
			project := validCatalogueInteriorProject(1, 1, "different-interior")
			reader.setFindResult(&project)
		}},
		{name: "invalid result", want: http.StatusServiceUnavailable, configure: func(
			_ *application,
			reader *recordingInteriorProjectCatalogueReader,
		) {
			project := validCatalogueInteriorProject(1, 1, "missing-interior")
			project.ProjectStatus = ""
			reader.setFindResult(&project)
		}},
		{name: "nil dependency", want: http.StatusServiceUnavailable, configure: func(
			app *application,
			_ *recordingInteriorProjectCatalogueReader,
		) {
			app.interiorProjects = nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingInteriorProjectCatalogueReader()
			reader.setProjects(nil)
			app := newTestApplication(t)
			app.interiorProjects = reader
			if test.configure != nil {
				test.configure(app, reader)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				"/interior-design/missing-interior",
				nil,
			)
			request.SetPathValue("slug", "missing-interior")
			app.interiorProjectDetailHandler(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.want)
			}
			if strings.Contains(recorder.Body.String(), unsafeDetail) {
				t.Error("detail error exposed repository diagnostics")
			}
			if app.interiorProjects != nil {
				calls := reader.findCallSnapshot()
				if len(calls) != 1 || !calls[0].HasDeadline {
					t.Errorf("detail calls: got %#v, want one bounded lookup", calls)
				}
			}
		})
	}

	app := newTestApplication(t)
	reader := newRecordingInteriorProjectCatalogueReader()
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/interior-design/interior-study-01/extra",
			nil,
		),
	)
	if recorder.Code != http.StatusNotFound || len(reader.findCallSnapshot()) != 0 {
		t.Error("nested unmatched URL did not remain a router-level 404")
	}

	var nilApp *application
	recorder = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/interior-design/known", nil)
	request.SetPathValue("slug", "known")
	nilApp.interiorProjectDetailHandler(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("nil application status: got %d, want 503", recorder.Code)
	}
}

// TestInteriorProjectDetailRouteAcceptsHead verifies real nested URLs retain
// handler selection and headers for HEAD while net/http suppresses body bytes.
func TestInteriorProjectDetailRouteAcceptsHead(t *testing.T) {
	project := validCatalogueInteriorProject(81, 1, "head-interior")
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{project})
	app := newTestApplication(t)
	app.interiorProjects = reader
	server := httptest.NewServer(app.routes())
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodHead,
		server.URL+interiorProjectDetailPath(project.Slug),
		nil,
	)
	if err != nil {
		t.Fatalf("create HEAD request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform HEAD request: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read HEAD response: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("HEAD status: got %d, want 200", response.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("HEAD body length: got %d, want 0", len(body))
	}
	if response.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("HEAD Content-Type: got %q", response.Header.Get("Content-Type"))
	}
	if calls := reader.findCallSnapshot(); len(calls) != 1 || !calls[0].HasDeadline {
		t.Errorf("HEAD detail calls: got %#v", calls)
	}
}

// TestInteriorProjectDetailTemplatePreservesMainWithoutData protects the public
// skip-link destination if a future handler accidentally omits detail data.
func TestInteriorProjectDetailTemplatePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.render(
		recorder,
		http.StatusOK,
		"interior-project-detail.html",
		pageData{
			Title:          "Missing Interior detail",
			CurrentPath:    "/interior-design/missing",
			NavigationPath: "/interior-design",
		},
	)

	body := recorder.Body.String()
	if strings.Count(body, "<main") != 1 ||
		strings.Count(body, `id="main-content"`) != 1 ||
		strings.Contains(body, "interior-project-detail__article") ||
		strings.Contains(body, "<h1") {
		t.Error("nil detail did not preserve one empty main landmark")
	}
}

// TestInteriorProjectDetailPresentationIsolationAndStylesheet verifies detail
// CSS stays route-local and exposes the reviewed-image/fallback selectors.
func TestInteriorProjectDetailPresentationIsolationAndStylesheet(t *testing.T) {
	project := validCatalogueInteriorProject(91, 1, "style-interior")
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects([]catalogueInteriorProject{project})
	app := newTestApplication(t)
	app.interiorProjects = reader
	handler := app.routes()
	detail := httptest.NewRecorder()
	handler.ServeHTTP(
		detail,
		httptest.NewRequest(
			http.MethodGet,
			interiorProjectDetailPath(project.Slug),
			nil,
		),
	)
	if count := strings.Count(
		detail.Body.String(),
		`href="/static/css/interior-project-detail.css"`,
	); count != 1 {
		t.Errorf("detail stylesheet count: got %d, want 1", count)
	}
	for _, unrelated := range []string{
		`href="/static/css/interior-design.css"`,
		`href="/static/css/products.css"`,
		`href="/static/css/product-detail.css"`,
	} {
		if strings.Contains(detail.Body.String(), unrelated) {
			t.Errorf("detail response contains unrelated stylesheet %q", unrelated)
		}
	}

	stylesheet := httptest.NewRecorder()
	handler.ServeHTTP(
		stylesheet,
		httptest.NewRequest(
			http.MethodGet,
			"/static/css/interior-project-detail.css",
			nil,
		),
	)
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("stylesheet status: got %d, want 200", stylesheet.Code)
	}
	for _, selector := range []string{
		".interior-project-detail__facts",
		".interior-project-detail__description",
		".interior-project-detail__media--image",
		".interior-project-detail__image",
		".interior-project-detail__caption",
	} {
		if !strings.Contains(stylesheet.Body.String(), selector) {
			t.Errorf("detail stylesheet does not contain %q", selector)
		}
	}
}
