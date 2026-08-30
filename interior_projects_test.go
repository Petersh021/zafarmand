package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// extractInteriorPreviewArticles returns complete preview articles in document
// order. The template does not nest article elements, so this small scanner can
// keep semantic assertions independent from a third-party HTML parser.
func extractInteriorPreviewArticles(t *testing.T, source string) []string {
	t.Helper()

	const classMarker = `class="interior-preview`
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
			t.Fatal("interior-preview class is not inside an article")
		}
		articleEnd := strings.Index(
			remaining[articleStart:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatal("interior-preview article has no closing tag")
		}
		articleEnd += articleStart + len(closingMarker)
		articles = append(articles, remaining[articleStart:articleEnd])
		remaining = remaining[articleEnd:]
	}

	return articles
}

// TestInteriorProjectPresentationHelpers verifies explicit repository-to-view
// mapping, nullable year formatting, canonical paths, cover metadata, and order.
func TestInteriorProjectPresentationHelpers(t *testing.T) {
	projects := []catalogueInteriorProject{
		{
			ID:              11,
			PortfolioNumber: 1,
			Slug:            "first-interior",
			Title:           "First Interior",
			Typology:        "Residential",
			Location:        "Tehran",
			ProjectYear:     2033,
			ProjectStatus:   "Completed",
			Description:     "First reviewed description.",
			Cover: &interiorProjectCoverMetadata{
				Version: 4,
				Width:   1600,
				Height:  1000,
				AltText: "A fictional warm residential Interior",
				Caption: "First fictional cover.",
			},
		},
		{
			ID:              12,
			PortfolioNumber: 2,
			Slug:            "second-interior",
			Title:           "Second Interior",
			Typology:        "Cultural",
			ProjectStatus:   "Ongoing",
		},
	}

	previews := interiorProjectPreviews(projects)
	want := []interiorProjectPreviewData{
		{
			Number:        "01",
			Title:         "First Interior",
			Typology:      "Residential",
			Location:      "Tehran",
			YearLabel:     "2033",
			ProjectStatus: "Completed",
			Path:          "/interior-design/first-interior",
			Cover: &publicInteriorProjectCoverPageData{
				Path:    "/interior-design/first-interior/cover/4",
				Width:   1600,
				Height:  1000,
				AltText: "A fictional warm residential Interior",
				Caption: "First fictional cover.",
			},
		},
		{
			Number:        "02",
			Title:         "Second Interior",
			Typology:      "Cultural",
			ProjectStatus: "Ongoing",
			Path:          "/interior-design/second-interior",
		},
	}
	if !reflect.DeepEqual(previews, want) {
		t.Errorf("previews: got %#v, want %#v", previews, want)
	}
	if year := formatInteriorProjectYear(0); year != "" {
		t.Errorf("absent year label: got %q, want empty", year)
	}
	if number := formatInteriorProjectNumber(103); number != "103" {
		t.Errorf("three-digit project number: got %q, want 103", number)
	}
	if path := interiorProjectCoverPath("first-interior", 4); path !=
		"/interior-design/first-interior/cover/4" {
		t.Errorf("cover path: got %q", path)
	}
	if cover := newPublicInteriorProjectCoverPageData("second-interior", nil); cover != nil {
		t.Errorf("nil repository cover produced view data: %#v", cover)
	}
	if previews := interiorProjectPreviews(nil); len(previews) != 0 {
		t.Errorf("nil source preview count: got %d, want 0", len(previews))
	}
}

// TestPublishedInteriorProjectCatalogueValidation protects handlers from an
// injected reader that skips repository ordering, uniqueness, or row checks.
func TestPublishedInteriorProjectCatalogueValidation(t *testing.T) {
	valid := []catalogueInteriorProject{
		validCatalogueInteriorProject(1, 1, "first-interior"),
		validCatalogueInteriorProject(2, 2, "second-interior"),
	}
	if !isValidPublishedInteriorProjectCatalogue(valid) {
		t.Fatal("valid published Interior catalogue was rejected")
	}

	invalid := []struct {
		// name identifies the broken injected contract.
		name string
		// mutate changes an isolated valid copy.
		mutate func([]catalogueInteriorProject)
	}{
		{name: "number gap", mutate: func(projects []catalogueInteriorProject) {
			projects[1].PortfolioNumber = 3
		}},
		{name: "duplicate identity", mutate: func(projects []catalogueInteriorProject) {
			projects[1].ID = projects[0].ID
		}},
		{name: "duplicate slug", mutate: func(projects []catalogueInteriorProject) {
			projects[1].Slug = projects[0].Slug
		}},
		{name: "invalid stored field", mutate: func(projects []catalogueInteriorProject) {
			projects[0].ProjectStatus = ""
		}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			projects := cloneCatalogueInteriorProjects(valid)
			test.mutate(projects)
			if isValidPublishedInteriorProjectCatalogue(projects) {
				t.Errorf("invalid catalogue was accepted: %#v", projects)
			}
		})
	}
}

// TestInteriorDesignRouteRendersPublishedPortfolio verifies the complete
// repository-backed showcase, meaningful cover, honest structural fallback,
// semantic ordering, reference-card suppression, and bounded dependency call.
func TestInteriorDesignRouteRendersPublishedPortfolio(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	projects := []catalogueInteriorProject{
		{
			ID:              31,
			PortfolioNumber: 1,
			Slug:            "covered-residence",
			Title:           "Covered Residence",
			Typology:        "Residential",
			Location:        "Tehran",
			ProjectYear:     2032,
			ProjectStatus:   "Completed",
			Cover: &interiorProjectCoverMetadata{
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
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/interior-design", nil)

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
		`class="interior-work"`,
		"section",
	)
	portfolio := extractElementByMarker(
		t,
		work,
		`class="interior-portfolio"`,
		"ol",
	)
	if opening := extractOpeningTag(t, portfolio); !strings.Contains(
		opening,
		`aria-label="Published Interior project previews"`,
	) || !strings.Contains(opening, `role="list"`) {
		t.Errorf("portfolio semantics are incomplete: %s", opening)
	}
	articles := extractInteriorPreviewArticles(t, portfolio)
	if len(articles) != len(projects) {
		t.Fatalf("article count: got %d, want %d", len(articles), len(projects))
	}

	covered := articles[0]
	for _, expected := range []string{
		`href="/interior-design/covered-residence"`,
		"Covered Residence",
		"Residential",
		`src="/interior-design/covered-residence/cover/7"`,
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
		`class="interior-preview__media"`,
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
		`href="/interior-design/uncovered-gallery"`,
		"Uncovered Gallery",
		"Cultural",
	} {
		if !strings.Contains(normalizeHTMLWhitespace(uncovered), expected) {
			t.Errorf("fallback article does not contain %q", expected)
		}
	}
	if strings.Contains(uncovered, "<img") {
		t.Error("absent cover produced an image")
	}
	fallbackNumber := extractElementByMarker(
		t,
		uncovered,
		`class="interior-preview__fallback-number"`,
		"span",
	)
	if !strings.Contains(extractOpeningTag(t, fallbackNumber), `aria-hidden="true"`) ||
		!strings.Contains(normalizeHTMLWhitespace(fallbackNumber), "02") {
		t.Error("decorative fallback number is not hidden or correctly numbered")
	}
	if strings.Index(portfolio, "Covered Residence") >=
		strings.Index(portfolio, "Uncovered Gallery") {
		t.Error("template changed repository portfolio order")
	}
	for _, referenceOnly := range []string{
		"Hillside Residence",
		"Karimi Apartment",
		"Noor Office",
		"Atrium Lobby",
		`class="interior-portfolio--reference"`,
		`/static/images/interior-design/hillside-residence.jpg`,
	} {
		if strings.Contains(work, referenceOnly) {
			t.Errorf("published portfolio retained reference-only content %q", referenceOnly)
		}
	}
	if strings.Contains(mainElement, `href="#"`) ||
		strings.Contains(mainElement, "docs/reference") {
		t.Error("public Interior index contains placeholder navigation or reference assets")
	}
}

// TestInteriorDesignRouteRendersReferencePortfolio verifies zero published rows
// select exactly the four approved, noninteractive concept cards. These local
// previews provide no false detail path and never masquerade as database rows.
func TestInteriorDesignRouteRendersReferencePortfolio(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects(nil)
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/interior-design", nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	work := extractElementByMarker(
		t,
		mainElement,
		`class="interior-work"`,
		"section",
	)
	portfolio := extractElementByMarker(
		t,
		work,
		`class="interior-portfolio interior-portfolio--reference"`,
		"ol",
	)
	opening := extractOpeningTag(t, portfolio)
	if !strings.Contains(opening, `aria-label="Interior project concept previews"`) ||
		!strings.Contains(opening, `role="list"`) {
		t.Errorf("reference portfolio semantics are incomplete: %s", opening)
	}

	referenceItems := []interiorReferenceProjectPreviewData{
		{
			Title:     "Hillside Residence",
			Typology:  "Residential",
			ImagePath: "/static/images/interior-design/hillside-residence.jpg",
			Width:     1681,
			Height:    936,
			AltText: "Warm living room overlooking a wooded hillside through " +
				"a tall central window",
		},
		{
			Title:     "Karimi Apartment",
			Typology:  "Residential",
			ImagePath: "/static/images/interior-design/karimi-apartment.jpg",
			Width:     1678,
			Height:    937,
			AltText: "Low-lit apartment living area beside a dark timber " +
				"kitchen and full-height window",
		},
		{
			Title:     "Noor Office",
			Typology:  "Commercial",
			ImagePath: "/static/images/interior-design/noor-office.jpg",
			Width:     1681,
			Height:    936,
			AltText: "Dark open office with shared tables, a window wall, and " +
				"warm perimeter lighting",
		},
		{
			Title:     "Atrium Lobby",
			Typology:  "Hospitality",
			ImagePath: "/static/images/interior-design/atrium-lobby.jpg",
			Width:     1679,
			Height:    937,
			AltText: "Enclosed lobby lounge with curved seating, indoor trees, " +
				"and a garden-facing glass wall",
		},
	}
	articles := extractInteriorPreviewArticles(t, portfolio)
	if len(articles) != len(referenceItems) || len(articles) != 4 {
		t.Fatalf(
			"reference article count: got %d, want 4",
			len(articles),
		)
	}
	for index, item := range referenceItems {
		article := articles[index]
		for _, expected := range []string{
			item.Title,
			item.Typology,
			`src="` + item.ImagePath + `"`,
			`width="` + strconv.Itoa(item.Width) + `"`,
			`height="` + strconv.Itoa(item.Height) + `"`,
			`alt="` + item.AltText + `"`,
			`loading="lazy"`,
			`decoding="async"`,
		} {
			if !strings.Contains(normalizeHTMLWhitespace(article), expected) {
				t.Errorf("reference article %d does not contain %q", index+1, expected)
			}
		}
		for _, falseInteraction := range []string{
			"<a ",
			"<a\n",
			"<a>",
			"href=",
			`aria-disabled="true"`,
		} {
			if strings.Contains(article, falseInteraction) {
				t.Errorf(
					"reference article %d contains false interaction %q",
					index+1,
					falseInteraction,
				)
			}
		}
		if index > 0 && strings.Index(portfolio, referenceItems[index-1].Title) >=
			strings.Index(portfolio, item.Title) {
			t.Errorf("reference article %q is out of order", item.Title)
		}
	}

	allLabel := extractElementByMarker(
		t,
		work,
		`class="interior-work__all-label"`,
		"p",
	)
	if !strings.Contains(normalizeHTMLWhitespace(allLabel), "View all projects") {
		t.Error("reference closing label is missing")
	}
	if strings.Contains(allLabel, "<a") || strings.Contains(allLabel, "href=") {
		t.Error("reference closing label promises a nonexistent destination")
	}
}

// TestInteriorDesignRouteRendersReferenceHero verifies the route-specific
// photographic hero, labelled identity, open desktop rail, and real fragment
// cue that replace the shared discipline shell only on Interior Design.
func TestInteriorDesignRouteRendersReferenceHero(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	reader.setProjects(nil)
	app := newTestApplication(t)
	app.interiorProjects = reader
	recorder := httptest.NewRecorder()

	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/interior-design", nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	mainElement := extractMainElement(t, body)
	if !strings.Contains(extractOpeningTag(t, mainElement), `class="interior-page"`) {
		t.Error("Interior main does not own its route-specific page class")
	}

	hero := extractElementByMarker(t, mainElement, `class="interior-hero"`, "section")
	heroOpening := extractOpeningTag(t, hero)
	for _, attribute := range []string{
		`aria-labelledby="interior-title"`,
		`data-interior-hero`,
	} {
		if !strings.Contains(heroOpening, attribute) {
			t.Errorf("Interior hero opening tag does not contain %q", attribute)
		}
	}
	if count := strings.Count(hero, "<h1"); count != 1 {
		t.Errorf("Interior hero h1 count: got %d, want 1", count)
	}
	heading := extractElementByMarker(t, hero, `id="interior-title"`, "h1")
	if !strings.Contains(normalizeHTMLWhitespace(heading), "Interior Design") {
		t.Error("Interior hero h1 does not contain the route title")
	}
	for _, expected := range []string{
		"Thoughtful spaces. Refined living.",
		`src="/static/images/interior-design/interior-hero.jpg"`,
		`width="1426"`,
		`height="936"`,
		`fetchpriority="high"`,
		`decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(hero), expected) {
			t.Errorf("Interior hero does not contain %q", expected)
		}
	}

	menu := extractElementByMarker(
		t,
		hero,
		`class="interior-reference-menu"`,
		"details",
	)
	menuStartsOpen := false
	for _, attribute := range strings.Fields(extractOpeningTag(t, menu)) {
		if attribute == "open" {
			menuStartsOpen = true
			break
		}
	}
	if !menuStartsOpen {
		t.Error("Interior reference menu does not start open")
	}
	menuNavigation := extractElementByMarker(
		t,
		menu,
		`aria-label="Interior page navigation"`,
		"nav",
	)
	for _, destination := range []string{
		`href="#selected-work"`,
		`href="/contact"`,
	} {
		if !strings.Contains(menuNavigation, destination) {
			t.Errorf("Interior reference navigation does not contain %q", destination)
		}
	}
	if count := strings.Count(menuNavigation, "<a"); count != 2 {
		t.Errorf("Interior reference navigation link count: got %d, want 2", count)
	}

	socials := extractElementByMarker(
		t,
		menu,
		`class="interior-reference-menu__socials"`,
		"ul",
	)
	for _, required := range []string{
		`role="list"`,
		`interior-reference-menu__social-icon--instagram`,
		`interior-reference-menu__social-icon--pinterest`,
		`interior-reference-menu__social-icon--linkedin`,
		"Instagram",
		"Pinterest",
		"LinkedIn",
	} {
		if !strings.Contains(socials, required) {
			t.Errorf("Interior social list does not contain %q", required)
		}
	}
	if count := strings.Count(socials, "<svg"); count != 3 {
		t.Errorf("Interior social icon count: got %d, want 3", count)
	}
	if count := strings.Count(socials, `aria-hidden="true"`); count != 3 {
		t.Errorf("decorative Interior social icon count: got %d, want 3", count)
	}
	if count := strings.Count(socials, `focusable="false"`); count != 3 {
		t.Errorf("unfocusable Interior social icon count: got %d, want 3", count)
	}
	for _, platform := range []struct {
		icon  string
		label string
	}{
		{icon: "social-icon--instagram", label: ">Instagram</span>"},
		{icon: "social-icon--pinterest", label: ">Pinterest</span>"},
		{icon: "social-icon--linkedin", label: ">LinkedIn</span>"},
	} {
		iconPosition := strings.Index(socials, platform.icon)
		labelPosition := strings.Index(socials, platform.label)
		if iconPosition < 0 || labelPosition < 0 || iconPosition >= labelPosition {
			t.Errorf("Interior social icon %q is not before its visible label", platform.icon)
		}
	}
	for _, forbidden := range []string{
		"<a",
		"<button",
		"href=",
		"tabindex=",
		`role="img"`,
	} {
		if strings.Contains(socials, forbidden) {
			t.Errorf("non-interactive Interior social list contains %q", forbidden)
		}
	}

	scrollLink := extractElementByMarker(t, hero, `data-interior-scroll`, "a")
	if !strings.Contains(extractOpeningTag(t, scrollLink), `href="#selected-work"`) ||
		!strings.Contains(normalizeHTMLWhitespace(scrollLink), "Scroll to explore") {
		t.Error("Interior hero scroll cue does not name its real fragment destination")
	}
	work := extractElementByMarker(t, mainElement, `class="interior-work"`, "section")
	workOpening := extractOpeningTag(t, work)
	for _, attribute := range []string{
		`id="selected-work"`,
		`aria-labelledby="selected-work-title"`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(workOpening, attribute) {
			t.Errorf("Interior work opening tag does not contain %q", attribute)
		}
	}
	if strings.Contains(mainElement, `class="discipline-hero"`) ||
		strings.Contains(body, `href="/static/css/discipline.css"`) {
		t.Error("Interior route retained the removed shared discipline presentation")
	}
}

// TestInteriorDesignHandlerRejectsReaderFailures verifies repository errors,
// malformed injected catalogues, and an unavailable dependency all become a
// fixed 503 without leaking unsafe details.
func TestInteriorDesignHandlerRejectsReaderFailures(t *testing.T) {
	unsafeDetail := "postgres://private-interior-list"
	invalid := []catalogueInteriorProject{
		validCatalogueInteriorProject(1, 2, "number-gap"),
	}
	tests := []struct {
		// name identifies the failed dependency contract.
		name string
		// configure mutates the otherwise valid app dependency.
		configure func(*application, *recordingInteriorProjectCatalogueReader)
	}{
		{name: "repository error", configure: func(
			_ *application,
			reader *recordingInteriorProjectCatalogueReader,
		) {
			reader.setErrors(errors.New(unsafeDetail), nil)
		}},
		{name: "invalid catalogue", configure: func(
			_ *application,
			reader *recordingInteriorProjectCatalogueReader,
		) {
			reader.setProjects(invalid)
		}},
		{name: "nil dependency", configure: func(
			app *application,
			_ *recordingInteriorProjectCatalogueReader,
		) {
			app.interiorProjects = nil
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApplication(t)
			reader := newRecordingInteriorProjectCatalogueReader()
			app.interiorProjects = reader
			test.configure(app, reader)
			recorder := httptest.NewRecorder()
			app.interiorDesignHandler(
				recorder,
				httptest.NewRequest(http.MethodGet, "/interior-design", nil),
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
	nilApp.interiorDesignHandler(
		recorder,
		httptest.NewRequest(http.MethodGet, "/interior-design", nil),
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("nil application status: got %d, want 503", recorder.Code)
	}
}

// TestInteriorDesignTemplateEscapesManagedData renders sentinel content through
// html/template and proves meaningful image attributes remain inert data.
func TestInteriorDesignTemplateEscapesManagedData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	listing := &interiorProjectListingData{
		Heading: "<u>Unsafe heading</u>",
		Items: []interiorProjectPreviewData{
			{
				Number:        "A1",
				Path:          "/interior-design/safe-sentinel",
				Title:         "<b>Unsafe title</b>",
				Typology:      "<em>Unsafe typology</em>",
				Location:      "<script>Unsafe location</script>",
				YearLabel:     "2035",
				ProjectStatus: "<img src=x onerror=alert(1)>",
				Cover: &publicInteriorProjectCoverPageData{
					Path:    "/interior-design/safe-sentinel/cover/1",
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
		"interior-design.html",
		pageData{
			Title:       "Sentinel Interior Design",
			CurrentPath: "/interior-design",
			DisciplinePage: &disciplinePageData{
				Number:   "S-01",
				Name:     "Sentinel Interior Design",
				NextName: "Next sentinel",
				NextPath: "/architecture-design",
			},
			InteriorProjectListing: listing,
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

// TestInteriorDesignTemplatePreservesSectionWithoutListingData protects the
// defensive labelled work section when optional page data is accidentally nil.
func TestInteriorDesignTemplatePreservesSectionWithoutListingData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.render(
		recorder,
		http.StatusOK,
		"interior-design.html",
		pageData{
			Title:       "Interior Design",
			CurrentPath: "/interior-design",
			DisciplinePage: &disciplinePageData{
				Number:   "01",
				Name:     "Interior Design",
				NextName: "Architecture Design",
				NextPath: "/architecture-design",
			},
		},
	)

	mainElement := extractMainElement(t, recorder.Body.String())
	work := extractElementByMarker(t, mainElement, `id="selected-work"`, "section")
	if !strings.Contains(work, `id="selected-work-title"`) ||
		strings.Contains(work, `class="interior-portfolio"`) {
		t.Error("nil listing lost its labelled section or emitted an empty list")
	}
}

// TestInteriorDesignRouteAcceptsHead verifies ServeMux's GET-to-HEAD behavior
// keeps metadata and repository selection while the real HTTP writer suppresses
// response bytes. ResponseRecorder alone does not model wire-level suppression.
func TestInteriorDesignRouteAcceptsHead(t *testing.T) {
	reader := newRecordingInteriorProjectCatalogueReader()
	app := newTestApplication(t)
	app.interiorProjects = reader
	server := httptest.NewServer(app.routes())
	defer server.Close()

	request, err := http.NewRequest(http.MethodHead, server.URL+"/interior-design", nil)
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

// TestInteriorDesignPresentationIsolationAndStylesheet verifies route-specific
// CSS loads only on Interior and its static response contains Stage 22 selectors.
func TestInteriorDesignPresentationIsolationAndStylesheet(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	interior := httptest.NewRecorder()
	handler.ServeHTTP(
		interior,
		httptest.NewRequest(http.MethodGet, "/interior-design", nil),
	)
	if count := strings.Count(
		interior.Body.String(),
		`href="/static/css/interior-design.css"`,
	); count != 1 {
		t.Errorf("Interior stylesheet count: got %d, want 1", count)
	}

	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(home.Body.String(), "interior-design.css") ||
		strings.Contains(home.Body.String(), `class="interior-portfolio`) {
		t.Error("Interior listing presentation leaked into homepage")
	}

	stylesheet := httptest.NewRecorder()
	handler.ServeHTTP(
		stylesheet,
		httptest.NewRequest(
			http.MethodGet,
			"/static/css/interior-design.css",
			nil,
		),
	)
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("stylesheet status: got %d, want 200", stylesheet.Code)
	}
	interiorCSS := stylesheet.Body.String()
	for _, selector := range []string{
		".interior-hero",
		".interior-reference-menu",
		".interior-reference-menu__social-item",
		".interior-reference-menu__social-icon",
		".interior-work",
		".interior-portfolio",
		".interior-preview__image",
		".interior-preview__fallback-number",
		".interior-work__all-label",
	} {
		if !strings.Contains(interiorCSS, selector) {
			t.Errorf("stylesheet does not contain %q", selector)
		}
	}

	// The transparent route header must not intercept the native menu summary.
	headerRule := stage26CSSRule(
		t,
		interiorCSS,
		`body[data-current-path="/interior-design"] .site-header`,
	)
	if !strings.Contains(headerRule, "pointer-events: none;") {
		t.Error("Interior header still blocks pointer input to the menu summary")
	}

	linkRule := stage26CSSRule(
		t,
		interiorCSS,
		`body[data-current-path="/interior-design"] .discipline-nav__link`,
	)
	if !strings.Contains(linkRule, "pointer-events: auto;") {
		t.Error("Interior discipline links are not restored above the click-through header")
	}

	menuRule := stage26CSSRule(t, interiorCSS, ".interior-reference-menu")
	for _, required := range []string{"z-index: 5;", "pointer-events: none;"} {
		if !strings.Contains(menuRule, required) {
			t.Errorf("Interior menu rule does not contain %q", required)
		}
	}

	summaryRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-reference-menu__summary",
	)
	for _, required := range []string{
		"z-index: 2;",
		"width: var(--interior-menu-width);",
		"height: 6.5rem;",
		"pointer-events: auto;",
	} {
		if !strings.Contains(summaryRule, required) {
			t.Errorf("Interior menu summary rule does not contain %q", required)
		}
	}

	// Geometry stays tied to the viewport rather than to the menu rail width.
	identityRule := stage26CSSRule(t, interiorCSS, ".interior-hero__identity")
	for _, required := range []string{"top: 50%;", "left: 50%;"} {
		if !strings.Contains(identityRule, required) {
			t.Errorf("Interior identity rule does not contain %q", required)
		}
	}

	panelRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-reference-menu[open] .interior-reference-menu__panel",
	)
	for _, required := range []string{
		"display: flex;",
		"flex-direction: column;",
		"padding-block-start: clamp(10rem, 27vh, 15.5rem);",
		"padding-block-end: clamp(3.25rem, 6.5vh, 4.25rem);",
		"padding-inline: 0;",
	} {
		if !strings.Contains(panelRule, required) {
			t.Errorf("Interior menu panel rule does not contain %q", required)
		}
	}

	primaryRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-reference-menu__primary",
	)
	for _, required := range []string{
		"width: 100%;",
		"align-self: stretch;",
		"justify-items: center;",
		"text-align: center;",
	} {
		if !strings.Contains(primaryRule, required) {
			t.Errorf("Interior primary menu rule does not contain %q", required)
		}
	}

	socialRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-reference-menu__socials",
	)
	for _, required := range []string{
		"margin: auto 0 0 calc(",
	} {
		if !strings.Contains(socialRule, required) {
			t.Errorf("Interior social menu rule does not contain %q", required)
		}
	}

	// The approved project-name scale stays fixed while its typology grows.
	for selector, size := range map[string]string{
		".interior-preview__title":    "max(clamp(0.92rem, 1.35vw, 1.18rem), 0.65vw)",
		".interior-preview__typology": "max(clamp(0.95rem, 1.28vw, 1.2rem), 0.65vw)",
	} {
		rule := stage26CSSRule(t, interiorCSS, selector)
		if !strings.Contains(rule, "font-size: "+size+";") {
			t.Errorf("%s does not retain responsive font size %q", selector, size)
		}
	}

	iconRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-reference-menu__social-icon",
	)
	for _, required := range []string{
		"display: block;",
		"width: clamp(1rem, 0.9vw, 1.65rem);",
		"height: clamp(1rem, 0.9vw, 1.65rem);",
		"stroke: currentColor;",
		"stroke-width: 2.15;",
		"filter: drop-shadow(",
		"opacity: 1;",
	} {
		if !strings.Contains(iconRule, required) {
			t.Errorf("Interior social icon rule does not contain %q", required)
		}
	}

	scrollRule := stage26CSSRule(t, interiorCSS, ".interior-hero__scroll")
	if expected := "font-size: max(clamp(0.68rem, 0.6rem + 0.25vw, 0.9rem), 0.64vw);"; !strings.Contains(scrollRule, expected) {
		t.Errorf("Interior scroll cue does not retain %q", expected)
	}

	chevronRule := stage26CSSRule(
		t,
		interiorCSS,
		".interior-hero__scroll-chevron",
	)
	for _, dimension := range []string{"width", "height"} {
		expected := dimension + ": max(clamp(1.15rem, 1.05rem + 0.15vw, 1.3rem), 0.82vw);"
		if !strings.Contains(chevronRule, expected) {
			t.Errorf("Interior scroll chevron does not retain %q", expected)
		}
	}
}
