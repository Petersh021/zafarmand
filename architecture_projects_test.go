package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var architectureNonEmptyAltPattern = regexp.MustCompile(`\salt="[^"]+"`)

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

// TestArchitectureDesignRouteRendersReferenceHero verifies Architecture owns
// the supplied photographic composition while continuing to use the unchanged
// shared navigation rendered outside its main landmark.
func TestArchitectureDesignRouteRendersReferenceHero(t *testing.T) {
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
	body := recorder.Body.String()
	mainElement := extractMainElement(t, body)
	if !strings.Contains(extractOpeningTag(t, mainElement), `class="architecture-page"`) {
		t.Error("Architecture main does not own its route-specific page class")
	}

	hero := extractElementByMarker(
		t,
		mainElement,
		`class="architecture-hero"`,
		"section",
	)
	heroOpening := extractOpeningTag(t, hero)
	for _, attribute := range []string{
		`aria-labelledby="architecture-title"`,
		`data-architecture-hero`,
	} {
		if !strings.Contains(heroOpening, attribute) {
			t.Errorf("Architecture hero opening tag does not contain %q", attribute)
		}
	}
	if count := strings.Count(hero, "<h1"); count != 1 {
		t.Errorf("Architecture hero h1 count: got %d, want 1", count)
	}
	heading := extractElementByMarker(t, hero, `id="architecture-title"`, "h1")
	if !strings.Contains(normalizeHTMLWhitespace(heading), "Architecture Design") {
		t.Error("Architecture hero h1 does not contain the route title")
	}
	for _, expected := range []string{
		"Form. Function. Context.",
		`src="/static/images/architecture-design/architecture-hero.jpg"`,
		`width="1672"`,
		`height="941"`,
		`fetchpriority="high"`,
		`decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(hero), expected) {
			t.Errorf("Architecture hero does not contain %q", expected)
		}
	}
	if strings.Contains(hero, `loading="lazy"`) {
		t.Error("above-fold Architecture hero is lazy-loaded")
	}
	if !architectureNonEmptyAltPattern.MatchString(hero) {
		t.Error("Architecture hero image lacks nonempty alternative text")
	}

	scrollLink := extractElementByMarker(t, hero, `data-architecture-scroll`, "a")
	if !strings.Contains(extractOpeningTag(t, scrollLink), `href="#selected-work"`) ||
		!strings.Contains(normalizeHTMLWhitespace(scrollLink), "Scroll to explore") {
		t.Error("Architecture hero scroll cue does not name its real fragment destination")
	}
	work := extractElementByMarker(
		t,
		mainElement,
		`class="architecture-work"`,
		"section",
	)
	workOpening := extractOpeningTag(t, work)
	for _, attribute := range []string{
		`id="selected-work"`,
		`aria-labelledby="selected-work-title"`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(workOpening, attribute) {
			t.Errorf("Architecture work opening tag does not contain %q", attribute)
		}
	}
	if strings.Contains(mainElement, `class="discipline-hero"`) ||
		strings.Contains(body, `href="/static/css/discipline.css"`) {
		t.Error("Architecture route retained the removed shared discipline presentation")
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
		`class="architecture-work"`,
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
		`aria-label="Published Architecture project previews"`,
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
		"Residential",
		"Covered Residence",
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
		"div",
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
		"Cultural",
		"Uncovered Gallery",
	} {
		if !strings.Contains(normalizeHTMLWhitespace(uncovered), expected) {
			t.Errorf("fallback article does not contain %q", expected)
		}
	}
	if strings.Contains(uncovered, "<img") {
		t.Error("coverless published project unexpectedly produced an image")
	}
	fallbackNumber := extractElementByMarker(
		t,
		uncovered,
		`class="architecture-preview__fallback-number"`,
		"span",
	)
	if !strings.Contains(extractOpeningTag(t, fallbackNumber), `aria-hidden="true"`) {
		t.Error("decorative fallback number is not hidden from assistive technology")
	}
	if strings.Index(portfolio, "Covered Residence") >=
		strings.Index(portfolio, "Uncovered Gallery") {
		t.Error("template changed repository portfolio order")
	}
	if strings.Contains(portfolio, "architecture-portfolio--reference") ||
		strings.Contains(portfolio, "/static/images/architecture-design/") {
		t.Error("published projects did not take priority over reference previews")
	}
	if strings.Contains(mainElement, `href="#"`) ||
		strings.Contains(mainElement, "docs/reference") {
		t.Error("public Architecture index contains placeholder navigation or reference assets")
	}
}

// TestArchitectureDesignRouteRendersReferencePortfolio verifies an empty
// published catalogue uses the four approved, non-interactive concept images.
// These cards preserve the launch composition without pretending detail routes
// or database records exist.
func TestArchitectureDesignRouteRendersReferencePortfolio(t *testing.T) {
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
	work := extractElementByMarker(
		t,
		mainElement,
		`class="architecture-work"`,
		"section",
	)
	portfolio := extractElementByMarker(
		t,
		work,
		`class="architecture-portfolio architecture-portfolio--reference"`,
		"ol",
	)
	opening := extractOpeningTag(t, portfolio)
	if !strings.Contains(opening, `aria-label="Architecture project concept previews"`) ||
		!strings.Contains(opening, `role="list"`) {
		t.Errorf("reference portfolio semantics are incomplete: %s", opening)
	}

	referenceItems := []struct {
		title     string
		typology  string
		imagePath string
		width     string
		height    string
	}{
		{
			title:     "Mountain House",
			typology:  "Residential",
			imagePath: "/static/images/architecture-design/mountain-house.jpg",
			width:     "1600",
			height:    "686",
		},
		{
			title:     "Terra Office Building",
			typology:  "Commercial",
			imagePath: "/static/images/architecture-design/terra-office-building.jpg",
			width:     "1200",
			height:    "900",
		},
		{
			title:     "Silk Museum",
			typology:  "Cultural",
			imagePath: "/static/images/architecture-design/silk-museum.jpg",
			width:     "1200",
			height:    "900",
		},
		{
			title:     "Coastal Retreat",
			typology:  "Residential",
			imagePath: "/static/images/architecture-design/coastal-retreat.jpg",
			width:     "1200",
			height:    "900",
		},
	}
	articles := extractArchitecturePreviewArticles(t, portfolio)
	if len(articles) != len(referenceItems) {
		t.Fatalf("reference article count: got %d, want %d", len(articles), len(referenceItems))
	}
	for index, item := range referenceItems {
		article := articles[index]
		for _, expected := range []string{
			item.title,
			item.typology,
			`src="` + item.imagePath + `"`,
			`width="` + item.width + `"`,
			`height="` + item.height + `"`,
			`loading="lazy"`,
			`decoding="async"`,
		} {
			if !strings.Contains(normalizeHTMLWhitespace(article), expected) {
				t.Errorf("reference article %d does not contain %q", index+1, expected)
			}
		}
		if !architectureNonEmptyAltPattern.MatchString(article) {
			t.Errorf("reference article %d image lacks nonempty alternative text", index+1)
		}
		for _, falseInteraction := range []string{
			"<a ",
			"<a\n",
			"<a>",
			"href=",
			`aria-disabled="true"`,
			`role="link"`,
		} {
			if strings.Contains(article, falseInteraction) {
				t.Errorf("reference article %d contains false interaction %q", index+1, falseInteraction)
			}
		}
		if index > 0 && strings.Index(portfolio, referenceItems[index-1].title) >=
			strings.Index(portfolio, item.title) {
			t.Errorf("reference article %q is out of order", item.title)
		}
	}

	allLabel := extractElementByMarker(
		t,
		work,
		`class="architecture-work__all-label"`,
		"p",
	)
	if !strings.Contains(normalizeHTMLWhitespace(allLabel), "View all projects") {
		t.Error("reference closing label is missing")
	}
	if strings.Contains(allLabel, "<a") || strings.Contains(allLabel, "href=") {
		t.Error("reference closing label promises a nonexistent destination")
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
		Heading: "<u>Unsafe heading</u>",
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
		"&lt;u&gt;Unsafe heading&lt;/u&gt;",
		"&lt;b&gt;Unsafe title&lt;/b&gt;",
		"&lt;em&gt;Unsafe typology&lt;/em&gt;",
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

// TestArchitectureDesignPresentationIsolationAndStylesheet verifies the
// route-owned hero and project-grid CSS stays isolated while the shared menu
// remains outside this stylesheet.
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
	if count := strings.Count(
		architecture.Body.String(),
		`/static/images/architecture-design/architecture-hero.jpg`,
	); count != 2 {
		t.Errorf("Architecture hero URL count: got %d, want preload plus image", count)
	}
	if strings.Contains(
		architecture.Body.String(),
		`href="/static/css/discipline.css"`,
	) {
		t.Error("Architecture route still loads the removed shared discipline stylesheet")
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
	architectureCSS := stylesheet.Body.String()
	for _, selector := range []string{
		".architecture-page",
		".architecture-hero",
		".architecture-hero__image",
		".architecture-hero__identity",
		".architecture-hero__scroll",
		".architecture-work",
		".architecture-portfolio",
		".architecture-preview__media--image",
		".architecture-preview__fallback-number",
		".architecture-preview__image",
		".architecture-work__all-label",
	} {
		if !strings.Contains(architectureCSS, selector) {
			t.Errorf("stylesheet does not contain %q", selector)
		}
	}

	heroRule := stage26CSSRule(t, architectureCSS, ".architecture-hero")
	for _, required := range []string{
		"min-height: 100vh;",
		"min-height: 100svh;",
		"overflow: hidden;",
	} {
		if !strings.Contains(heroRule, required) {
			t.Errorf("Architecture hero rule does not contain %q", required)
		}
	}
	imageRule := stage26CSSRule(t, architectureCSS, ".architecture-hero__image")
	for _, required := range []string{
		"width: 100%;",
		"height: 100%;",
		"object-fit: cover;",
	} {
		if !strings.Contains(imageRule, required) {
			t.Errorf("Architecture hero image rule does not contain %q", required)
		}
	}
	identityRule := stage26CSSRule(t, architectureCSS, ".architecture-hero__identity")
	for _, required := range []string{
		"top: 43%;",
		"left: 50%;",
		"transform: translate(-50%, -50%);",
	} {
		if !strings.Contains(identityRule, required) {
			t.Errorf("Architecture identity rule does not contain %q", required)
		}
	}
	headerRule := stage26CSSRule(
		t,
		architectureCSS,
		`body[data-current-path="/architecture-design"] .site-header`,
	)
	for _, required := range []string{
		"position: absolute;",
		"background: transparent;",
		"box-shadow: none;",
		"pointer-events: none;",
	} {
		if !strings.Contains(headerRule, required) {
			t.Errorf("Architecture header rule does not contain %q", required)
		}
	}
	linkRule := stage26CSSRule(
		t,
		architectureCSS,
		`body[data-current-path="/architecture-design"] .discipline-nav__link`,
	)
	if !strings.Contains(linkRule, "pointer-events: auto;") {
		t.Error("Architecture discipline links are not restored above the click-through header")
	}
}
