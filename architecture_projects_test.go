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

// validCatalogueArchitectureProject returns one deterministic reviewed public
// record. Keeping this test-only constructor beside the public HTTP tests makes
// optional-field cases concise without publishing fictional production data.
func validCatalogueArchitectureProject(
	id int64,
	number int64,
	slug string,
) catalogueArchitectureProject {
	return catalogueArchitectureProject{
		ID:              id,
		PortfolioNumber: number,
		Slug:            slug,
		Title:           "Stage Twenty-Three Architecture",
		Typology:        "Residential",
		Location:        "Tehran",
		ProjectYear:     2035,
		ProjectStatus:   "Completed",
		Description: "A fictional reviewed Architecture project used only " +
			"by tests.",
	}
}

// extractArchitecturePreviewArticles returns complete preview articles in document
// order. The template does not nest article elements, so this small scanner can
// keep semantic assertions independent from a third-party HTML parser.
func extractArchitecturePreviewArticles(t *testing.T, source string) []string {
	t.Helper()

	const classMarker = `class="architecture-preview`
	const closingMarker = "</article>"
	remaining := source
	var articles []string
	for {
		classPosition := strings.Index(remaining, classMarker)
		if classPosition == -1 {
			break
		}
		articleStart := strings.LastIndex(
			remaining[:classPosition],
			"<article",
		)
		if articleStart == -1 {
			t.Fatal("architecture-preview class is not inside an article")
		}
		articleEnd := strings.Index(
			remaining[articleStart:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatal("architecture-preview article has no closing tag")
		}
		articleEnd += articleStart + len(closingMarker)
		articles = append(articles, remaining[articleStart:articleEnd])
		remaining = remaining[articleEnd:]
	}

	return articles
}

// TestArchitectureProjectPresentationHelpers verifies explicit repository-to-view
// mapping, nullable year formatting, canonical paths, cover metadata, and order.
func TestArchitectureProjectPresentationHelpers(t *testing.T) {
	projects := []catalogueArchitectureProject{
		{
			ID:              11,
			PortfolioNumber: 1,
			Slug:            "first-architecture",
			Title:           "First Architecture",
			Typology:        "Residential",
			Location:        "Tehran",
			ProjectYear:     2033,
			ProjectStatus:   "Completed",
			Description:     "First reviewed description.",
			Cover: &architectureProjectCoverMetadata{
				Version: 4,
				Width:   1600,
				Height:  1000,
				AltText: "A fictional warm residential Architecture",
				Caption: "First fictional cover.",
			},
		},
		{
			ID:              12,
			PortfolioNumber: 2,
			Slug:            "second-architecture",
			Title:           "Second Architecture",
			Typology:        "Cultural",
			ProjectStatus:   "Ongoing",
		},
	}

	previews := architectureProjectPreviews(projects)
	want := []architectureProjectPreviewData{
		{
			Number:        "01",
			Title:         "First Architecture",
			Typology:      "Residential",
			Location:      "Tehran",
			YearLabel:     "2033",
			ProjectStatus: "Completed",
			Path:          "/architecture-design/first-architecture",
			Cover: &publicArchitectureProjectCoverPageData{
				Path:    "/architecture-design/first-architecture/cover/4",
				Width:   1600,
				Height:  1000,
				AltText: "A fictional warm residential Architecture",
				Caption: "First fictional cover.",
			},
		},
		{
			Number:        "02",
			Title:         "Second Architecture",
			Typology:      "Cultural",
			ProjectStatus: "Ongoing",
			Path:          "/architecture-design/second-architecture",
		},
	}
	if !reflect.DeepEqual(previews, want) {
		t.Errorf("previews: got %#v, want %#v", previews, want)
	}
	if year := formatArchitectureProjectYear(0); year != "" {
		t.Errorf("absent year label: got %q, want empty", year)
	}
	if number := formatArchitectureProjectNumber(103); number != "103" {
		t.Errorf("three-digit project number: got %q, want 103", number)
	}
	if path := architectureProjectCoverPath("first-architecture", 4); path !=
		"/architecture-design/first-architecture/cover/4" {
		t.Errorf("cover path: got %q", path)
	}
	if cover := newPublicArchitectureProjectCoverPageData("second-architecture", nil); cover != nil {
		t.Errorf("nil repository cover produced view data: %#v", cover)
	}
	if previews := architectureProjectPreviews(nil); len(previews) != 0 {
		t.Errorf("nil source preview count: got %d, want 0", len(previews))
	}
}

// TestPublishedArchitectureProjectCatalogueValidation protects handlers from an
// injected reader that skips repository ordering, uniqueness, or row checks.
func TestPublishedArchitectureProjectCatalogueValidation(t *testing.T) {
	valid := []catalogueArchitectureProject{
		validCatalogueArchitectureProject(1, 1, "first-architecture"),
		validCatalogueArchitectureProject(2, 2, "second-architecture"),
	}
	if !isValidPublishedArchitectureProjectCatalogue(valid) {
		t.Fatal("valid published Architecture catalogue was rejected")
	}

	invalid := []struct {
		// name identifies the broken injected contract.
		name string
		// mutate changes an isolated valid copy.
		mutate func([]catalogueArchitectureProject)
	}{
		{name: "number gap", mutate: func(projects []catalogueArchitectureProject) {
			projects[1].PortfolioNumber = 3
		}},
		{name: "duplicate identity", mutate: func(projects []catalogueArchitectureProject) {
			projects[1].ID = projects[0].ID
		}},
		{name: "duplicate slug", mutate: func(projects []catalogueArchitectureProject) {
			projects[1].Slug = projects[0].Slug
		}},
		{name: "invalid stored field", mutate: func(projects []catalogueArchitectureProject) {
			projects[0].ProjectStatus = ""
		}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			projects := cloneCatalogueArchitectureProjects(valid)
			test.mutate(projects)
			if isValidPublishedArchitectureProjectCatalogue(projects) {
				t.Errorf("invalid catalogue was accepted: %#v", projects)
			}
		})
	}
}

// TestArchitectureDesignRouteRendersPublishedPortfolio verifies the complete
// repository-backed index, optional facts, meaningful cover, honest fallback,
// semantic ordering, and bounded dependency call.
func TestArchitectureDesignRouteRendersPublishedPortfolio(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	projects := []catalogueArchitectureProject{
		{
			ID:              31,
			PortfolioNumber: 1,
			Slug:            "covered-residence",
			Title:           "Covered Residence",
			Typology:        "Residential",
			Location:        "Tehran",
			ProjectYear:     2032,
			ProjectStatus:   "Completed",
			Cover: &architectureProjectCoverMetadata{
				Version: 7,
				Width:   1800,
				Height:  1200,
				AltText: "Sunlight crossing a fictional stone living room",
				Caption: "Not rendered in the compact card.",
			},
		},
		{
			ID:              32,
			PortfolioNumber: 2,
			Slug:            "uncovered-gallery",
			Title:           "Uncovered Gallery",
			Typology:        "Cultural",
			ProjectStatus:   "Ongoing",
		},
	}
	reader.setProjects(projects)
	app := newTestApplication(t)
	app.architectureProjects = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/architecture-design", nil)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	calls := reader.listCallSnapshot()
	if len(calls) != 1 || !calls[0].HasDeadline {
		t.Errorf("list calls: got %#v, want one deadline-bounded call", calls)
	}
	body := recorder.Body.String()
	mainElement := extractMainElement(t, body)
	work := extractElementByMarker(
		t,
		mainElement,
		`class="discipline-work"`,
		"section",
	)
	portfolio := extractElementByMarker(
		t,
		work,
		`class="architecture-portfolio"`,
		"ol",
	)
	if opening := extractOpeningTag(t, portfolio); !strings.Contains(
		opening,
		`aria-label="Architecture project previews"`,
	) || !strings.Contains(opening, `role="list"`) {
		t.Errorf("portfolio semantics are incomplete: %s", opening)
	}
	articles := extractArchitecturePreviewArticles(t, portfolio)
	if len(articles) != len(projects) {
		t.Fatalf("article count: got %d, want %d", len(articles), len(projects))
	}

	covered := articles[0]
	for _, expected := range []string{
		`href="/architecture-design/covered-residence"`,
		"Residential / Project 01",
		"Covered Residence",
		"Completed / Tehran / 2032",
		`src="/architecture-design/covered-residence/cover/7"`,
		`width="1800"`,
		`height="1200"`,
		`alt="Sunlight crossing a fictional stone living room"`,
		`loading="lazy"`,
		`decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(covered), expected) {
			t.Errorf("covered article does not contain %q", expected)
		}
	}
	coveredMedia := extractElementByMarker(
		t,
		covered,
		`class="architecture-preview__media architecture-preview__media--image"`,
		"figure",
	)
	if strings.Contains(extractOpeningTag(t, coveredMedia), `aria-hidden=`) {
		t.Error("meaningful covered media is hidden from assistive technology")
	}
	if strings.Contains(covered, "Not rendered in the compact card") {
		t.Error("listing unexpectedly rendered the detail-only cover caption")
	}

	uncovered := articles[1]
	for _, expected := range []string{
		`href="/architecture-design/uncovered-gallery"`,
		"Cultural / Project 02",
		"Uncovered Gallery",
		"Ongoing",
	} {
		if !strings.Contains(normalizeHTMLWhitespace(uncovered), expected) {
			t.Errorf("fallback article does not contain %q", expected)
		}
	}
	if strings.Contains(uncovered, "<img") ||
		strings.Contains(normalizeHTMLWhitespace(uncovered), "Ongoing /") {
		t.Error("absent optional fields produced an image or dangling separator")
	}
	fallbackMedia := extractElementByMarker(
		t,
		uncovered,
		`class="architecture-preview__media"`,
		"div",
	)
	if !strings.Contains(extractOpeningTag(t, fallbackMedia), `aria-hidden="true"`) {
		t.Error("decorative fallback is not hidden from assistive technology")
	}
	if strings.Index(portfolio, "Covered Residence") >=
		strings.Index(portfolio, "Uncovered Gallery") {
		t.Error("template changed repository portfolio order")
	}
	if strings.Contains(mainElement, `href="#"`) ||
		strings.Contains(mainElement, "docs/reference") {
		t.Error("public Architecture index contains placeholder navigation or reference assets")
	}
}

// TestArchitectureDesignRouteRendersEmptyPublishedState verifies zero published
// rows are a successful truthful page rather than seeded content or an error.
func TestArchitectureDesignRouteRendersEmptyPublishedState(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	reader.setProjects(nil)
	app := newTestApplication(t)
	app.architectureProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/architecture-design", nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	if strings.Contains(mainElement, `class="architecture-portfolio"`) ||
		strings.Contains(mainElement, `class="architecture-preview`) {
		t.Error("empty published query rendered a portfolio list or card")
	}
	empty := extractElementByMarker(
		t,
		mainElement,
		`class="architecture-portfolio__empty"`,
		"p",
	)
	if !strings.Contains(
		normalizeHTMLWhitespace(empty),
		"Architecture project entries are being prepared for publication.",
	) {
		t.Error("empty state does not contain truthful publication copy")
	}
}

// TestArchitectureDesignHandlerRejectsReaderFailures verifies repository errors,
// malformed injected catalogues, and an unavailable dependency all become a
// fixed 503 without leaking unsafe details.
func TestArchitectureDesignHandlerRejectsReaderFailures(t *testing.T) {
	unsafeDetail := "postgres://private-architecture-list"
	invalid := []catalogueArchitectureProject{
		validCatalogueArchitectureProject(1, 2, "number-gap"),
	}
	tests := []struct {
		// name identifies the failed dependency contract.
		name string
		// configure mutates the otherwise valid app dependency.
		configure func(*application, *recordingArchitectureProjectCatalogueReader)
	}{
		{name: "repository error", configure: func(
			_ *application,
			reader *recordingArchitectureProjectCatalogueReader,
		) {
			reader.setErrors(errors.New(unsafeDetail), nil)
		}},
		{name: "invalid catalogue", configure: func(
			_ *application,
			reader *recordingArchitectureProjectCatalogueReader,
		) {
			reader.setProjects(invalid)
		}},
		{name: "nil dependency", configure: func(
			app *application,
			_ *recordingArchitectureProjectCatalogueReader,
		) {
			app.architectureProjects = nil
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingArchitectureProjectCatalogueReader()
			app.architectureProjects = reader
			test.configure(app, reader)
			recorder := httptest.NewRecorder()
			app.architectureDesignHandler(
				recorder,
				httptest.NewRequest(http.MethodGet, "/architecture-design", nil),
			)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status: got %d, want 503", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), unsafeDetail) {
				t.Error("service response exposed repository detail")
			}
		})
	}

	var nilApp *application
	recorder := httptest.NewRecorder()
	nilApp.architectureDesignHandler(
		recorder,
		httptest.NewRequest(http.MethodGet, "/architecture-design", nil),
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("nil application status: got %d, want 503", recorder.Code)
	}
}

// TestArchitectureDesignTemplateEscapesManagedData renders sentinel content through
// html/template and proves meaningful image attributes remain inert data.
func TestArchitectureDesignTemplateEscapesManagedData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	listing := &architectureProjectListingData{
		Eyebrow:      "Sentinel architectures eyebrow",
		Heading:      "Sentinel architectures heading",
		Introduction: "Sentinel architectures introduction",
		EmptyMessage: "Sentinel empty copy",
		Items: []architectureProjectPreviewData{
			{
				Number:        "A1",
				Path:          "/architecture-design/safe-sentinel",
				Title:         "<b>Unsafe title</b>",
				Typology:      "<em>Unsafe typology</em>",
				Location:      "<script>Unsafe location</script>",
				YearLabel:     "2035",
				ProjectStatus: "<img src=x onerror=alert(1)>",
				Cover: &publicArchitectureProjectCoverPageData{
					Path:    "/architecture-design/safe-sentinel/cover/1",
					Width:   4,
					Height:  3,
					AltText: `A room " onload="alert(1)`,
				},
			},
		},
	}

	app.render(
		recorder,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:       "Sentinel Architecture Design",
			CurrentPath: "/architecture-design",
			DisciplinePage: &disciplinePageData{
				Number:   "S-01",
				Name:     "Sentinel Architecture Design",
				NextName: "Next sentinel",
				NextPath: "/architecture-design",
			},
			ArchitectureProjectListing: listing,
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	for _, forbidden := range []string{
		"<b>Unsafe",
		"<em>Unsafe",
		"<script>Unsafe",
		"<img src=x onerror",
		`onload="alert(1)`,
	} {
		if strings.Contains(mainElement, forbidden) {
			t.Errorf("managed value became active markup %q", forbidden)
		}
	}
	for _, escaped := range []string{
		"&lt;b&gt;Unsafe title&lt;/b&gt;",
		"&lt;em&gt;Unsafe typology&lt;/em&gt;",
		"&lt;script&gt;Unsafe location&lt;/script&gt;",
		"&#34; onload=&#34;alert(1)",
	} {
		if !strings.Contains(mainElement, escaped) {
			t.Errorf("escaped response does not contain %q", escaped)
		}
	}
}

// TestArchitectureDesignTemplatePreservesSectionWithoutListingData protects the
// defensive labelled work section when optional page data is accidentally nil.
func TestArchitectureDesignTemplatePreservesSectionWithoutListingData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.render(
		recorder,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:       "Architecture Design",
			CurrentPath: "/architecture-design",
			DisciplinePage: &disciplinePageData{
				Number:   "01",
				Name:     "Architecture Design",
				NextName: "Architecture Design",
				NextPath: "/architecture-design",
			},
		},
	)

	mainElement := extractMainElement(t, recorder.Body.String())
	work := extractElementByMarker(t, mainElement, `id="selected-work"`, "section")
	if !strings.Contains(work, `id="selected-work-title"`) ||
		strings.Contains(work, `class="architecture-portfolio"`) {
		t.Error("nil listing lost its labelled section or emitted an empty list")
	}
}

// TestArchitectureDesignRouteAcceptsHead verifies ServeMux's GET-to-HEAD behavior
// keeps metadata and repository selection while the real HTTP writer suppresses
// response bytes. ResponseRecorder alone does not model wire-level suppression.
func TestArchitectureDesignRouteAcceptsHead(t *testing.T) {
	reader := newRecordingArchitectureProjectCatalogueReader()
	app := newTestApplication(t)
	app.architectureProjects = reader
	server := httptest.NewServer(app.routes())
	defer server.Close()

	request, err := http.NewRequest(http.MethodHead, server.URL+"/architecture-design", nil)
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
	if calls := reader.listCallSnapshot(); len(calls) != 1 || !calls[0].HasDeadline {
		t.Errorf("HEAD list calls: got %#v", calls)
	}
}

// TestArchitectureDesignPresentationIsolationAndStylesheet verifies route-specific
// CSS loads only on Architecture and its static response contains Stage 23 selectors.
func TestArchitectureDesignPresentationIsolationAndStylesheet(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	architecture := httptest.NewRecorder()
	handler.ServeHTTP(
		architecture,
		httptest.NewRequest(http.MethodGet, "/architecture-design", nil),
	)
	if count := strings.Count(
		architecture.Body.String(),
		`href="/static/css/architecture-design.css"`,
	); count != 1 {
		t.Errorf("Architecture stylesheet count: got %d, want 1", count)
	}

	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(home.Body.String(), "architecture-design.css") ||
		strings.Contains(home.Body.String(), `class="architecture-portfolio`) {
		t.Error("Architecture listing presentation leaked into homepage")
	}

	stylesheet := httptest.NewRecorder()
	handler.ServeHTTP(
		stylesheet,
		httptest.NewRequest(
			http.MethodGet,
			"/static/css/architecture-design.css",
			nil,
		),
	)
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("stylesheet status: got %d, want 200", stylesheet.Code)
	}
	for _, selector := range []string{
		".architecture-portfolio",
		".architecture-preview__media--image",
		".architecture-preview__image",
		".architecture-portfolio__empty",
	} {
		if !strings.Contains(stylesheet.Body.String(), selector) {
			t.Errorf("stylesheet does not contain %q", selector)
		}
	}
}
