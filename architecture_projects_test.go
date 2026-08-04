package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// extractArchitecturePreviewArticles returns complete architecture-preview
// articles in their document order.
//
// The listing template does not nest article elements, so a direct scan keeps
// this focused helper independent from a third-party HTML parser. Returning an
// empty slice is intentional: empty-state and isolation tests can use the same
// helper to prove that no project cards were emitted.
func extractArchitecturePreviewArticles(
	t *testing.T,
	source string,
) []string {
	t.Helper()

	const classMarker = `class="architecture-preview"`
	const closingMarker = "</article>"

	var articles []string
	remaining := source

	for {
		classPosition := strings.Index(
			remaining,
			classMarker,
		)
		if classPosition == -1 {
			break
		}

		// Search backward from the route-specific class so an unrelated article
		// earlier in the document can never be mistaken for the preview boundary.
		articleStart := strings.LastIndex(
			remaining[:classPosition],
			"<article",
		)
		if articleStart == -1 {
			t.Fatal(
				"architecture-preview class does not belong to an article",
			)
		}

		articleEnd := strings.Index(
			remaining[articleStart:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatal(
				"architecture-preview article does not have a closing tag",
			)
		}

		articleEnd += articleStart + len(closingMarker)
		articles = append(
			articles,
			remaining[articleStart:articleEnd],
		)
		remaining = remaining[articleEnd:]
	}

	return articles
}

// TestArchitectureProjectPreviewsPreservesOrderAndFields verifies the explicit
// source-to-template mapping without relying on production fixture values.
//
// Interleaved sentinel fields prove that values stay associated with their
// source record and that editorial ordering survives the conversion. Nil input
// represents a future zero-row repository response and must naturally produce
// no preview values.
func TestArchitectureProjectPreviewsPreservesOrderAndFields(t *testing.T) {
	source := []architectureProject{
		{
			Number:   "A-17",
			Title:    "First sentinel title",
			Typology: "First sentinel typology",
			Status:   "First sentinel status",
		},
		{
			Number:   "B-29",
			Title:    "Second sentinel title",
			Typology: "Second sentinel typology",
			Status:   "Second sentinel status",
		},
	}

	previews := architectureProjectPreviews(source)
	if len(previews) != len(source) {
		t.Fatalf(
			"preview count: got %d, want %d",
			len(previews),
			len(source),
		)
	}

	for index, project := range source {
		preview := previews[index]

		if preview.Number != project.Number {
			t.Errorf(
				"preview %d number: got %q, want %q",
				index,
				preview.Number,
				project.Number,
			)
		}
		if preview.Title != project.Title {
			t.Errorf(
				"preview %d title: got %q, want %q",
				index,
				preview.Title,
				project.Title,
			)
		}
		if preview.Typology != project.Typology {
			t.Errorf(
				"preview %d typology: got %q, want %q",
				index,
				preview.Typology,
				project.Typology,
			)
		}
		if preview.Status != project.Status {
			t.Errorf(
				"preview %d status: got %q, want %q",
				index,
				preview.Status,
				project.Status,
			)
		}
	}

	if previews := architectureProjectPreviews(nil); len(previews) != 0 {
		t.Errorf(
			"nil source preview count: got %d, want 0",
			len(previews),
		)
	}
}

// TestArchitectureDesignRouteRendersPortfolio verifies the complete public
// Stage 10 response produced by architectureDesignHandler.
//
// Replacing the application-owned source proves the handler uses its injected
// dependency instead of rebuilding temporary records internally. The remaining
// assertions own stylesheet ordering, shared-section semantics, record order,
// and the deliberate absence of detail navigation during this listing stage.
func TestArchitectureDesignRouteRendersPortfolio(t *testing.T) {
	app := newTestApplication(t)
	app.architectureProjects = []architectureProject{
		{
			Number:   "R-17",
			Title:    "Route sentinel one",
			Typology: "Route typology one",
			Status:   "Route status one",
		},
		{
			Number:   "R-29",
			Title:    "Route sentinel two",
			Typology: "Route typology two",
			Status:   "Route status two",
		},
	}

	handler := app.routes()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/architecture-design",
		nil,
	)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	body := recorder.Body.String()

	// Architecture composes the shared discipline surface, so both stylesheets
	// must occur once and the narrower route stylesheet must load second.
	disciplineStyles := `href="/static/css/discipline.css"`
	architectureStyles := `href="/static/css/architecture-design.css"`
	if count := strings.Count(body, disciplineStyles); count != 1 {
		t.Errorf(
			"discipline stylesheet count: got %d, want 1",
			count,
		)
	}
	if count := strings.Count(body, architectureStyles); count != 1 {
		t.Errorf(
			"Architecture stylesheet count: got %d, want 1",
			count,
		)
	}
	if disciplinePosition, architecturePosition := strings.Index(
		body,
		disciplineStyles,
	), strings.Index(
		body,
		architectureStyles,
	); disciplinePosition == -1 ||
		architecturePosition == -1 ||
		architecturePosition < disciplinePosition {
		t.Error(
			"Architecture stylesheet must load after discipline stylesheet",
		)
	}

	mainElement := extractMainElement(t, body)
	workElement := extractElementByMarker(
		t,
		mainElement,
		`class="discipline-work"`,
		"section",
	)
	workOpening := extractOpeningTag(t, workElement)

	// Overriding the inner work block must retain the fragment target and
	// aria-labelledby contract owned by the shared discipline partial.
	if !strings.Contains(
		workOpening,
		`id="selected-work"`,
	) || !strings.Contains(
		workOpening,
		`aria-labelledby="selected-work-title"`,
	) {
		t.Error(
			"Architecture work section does not own its id and heading label",
		)
	}

	workHeading := extractElementByMarker(
		t,
		workElement,
		`id="selected-work-title"`,
		"h2",
	)
	if !strings.Contains(
		normalizeHTMLWhitespace(workHeading),
		"Architecture project index",
	) {
		t.Error(
			"Architecture work heading does not contain Architecture project index",
		)
	}

	portfolio := extractElementByMarker(
		t,
		workElement,
		`class="architecture-portfolio"`,
		"ol",
	)
	portfolioOpening := extractOpeningTag(t, portfolio)
	if !strings.Contains(
		portfolioOpening,
		`aria-label="Architecture project previews"`,
	) {
		t.Error(
			"Architecture portfolio does not have its accessible label",
		)
	}
	if !strings.Contains(portfolioOpening, `role="list"`) {
		t.Error(
			"Architecture portfolio does not preserve explicit list semantics",
		)
	}

	expectedItems := architectureProjectPreviews(app.architectureProjects)
	articles := extractArchitecturePreviewArticles(t, portfolio)
	if len(articles) != len(expectedItems) {
		t.Fatalf(
			"Architecture article count: got %d, want %d",
			len(articles),
			len(expectedItems),
		)
	}
	if count := strings.Count(
		portfolio,
		`class="architecture-portfolio__item"`,
	); count != len(expectedItems) {
		t.Errorf(
			"Architecture list-item count: got %d, want %d",
			count,
			len(expectedItems),
		)
	}

	for index, expected := range expectedItems {
		article := articles[index]
		normalizedArticle := normalizeHTMLWhitespace(article)

		// Comparing by slice position proves that template range preserves
		// editorial ordering and keeps all mapped values in the same article.
		for _, expectedString := range []string{
			expected.Typology + " / Project slot " + expected.Number,
			expected.Title,
			expected.Status,
		} {
			if !strings.Contains(normalizedArticle, expectedString) {
				t.Errorf(
					"article %d does not contain %q",
					index,
					expectedString,
				)
			}
		}

		titleHeading := extractElementByMarker(
			t,
			article,
			expected.Title,
			"h3",
		)
		if !strings.Contains(
			normalizeHTMLWhitespace(titleHeading),
			expected.Title,
		) {
			t.Errorf(
				"article %d h3 does not contain title %q",
				index,
				expected.Title,
			)
		}
		if count := strings.Count(article, "<h3"); count != 1 {
			t.Errorf(
				"article %d title heading count: got %d, want 1",
				index,
				count,
			)
		}

		// The visual field repeats the visible editorial number for decoration,
		// so the container itself must be excluded from the accessibility tree.
		media := extractElementByMarker(
			t,
			article,
			`class="architecture-preview__media"`,
			"div",
		)
		if !strings.Contains(
			extractOpeningTag(t, media),
			`aria-hidden="true"`,
		) {
			t.Errorf(
				"article %d media is not hidden from assistive technology",
				index,
			)
		}

		// Stage 10 has no registered Architecture detail route. Keeping each card
		// noninteractive avoids fake URLs and gives Stage 11 a clear responsibility.
		// Complete anchor prefixes avoid treating the enclosing <article> text as
		// an anchor merely because both element names begin with the letter "a".
		hasAnchor := strings.Contains(article, "<a ") ||
			strings.Contains(article, "<a>") ||
			strings.Contains(article, "<a\n") ||
			strings.Contains(article, "<a\r")
		if hasAnchor ||
			strings.Contains(article, `href=`) ||
			strings.Contains(article, `role="link"`) ||
			strings.Contains(article, `tabindex=`) {
			t.Errorf(
				"article %d must remain noninteractive until detail routes exist",
				index,
			)
		}
	}

	// The Architecture reference is design documentation, not a public asset,
	// and placeholder fragment links must never stand in for future project URLs.
	if strings.Contains(mainElement, `href="#"`) {
		t.Error(
			"Architecture Design main must not contain a placeholder href",
		)
	}
	if strings.Contains(mainElement, "docs/reference") ||
		strings.Contains(mainElement, "zafarmand-architecture-design.jpg") {
		t.Error(
			"Architecture Design page must not render a reference composite",
		)
	}

	// An apparent project path must remain a normal 404 during the listing-only
	// stage. This explicitly prevents the tests from silently accepting a detail
	// route that was implemented ahead of the agreed vertical slice.
	detailRecorder := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(
		http.MethodGet,
		"/architecture-design/architecture-study-01",
		nil,
	)
	handler.ServeHTTP(detailRecorder, detailRequest)
	if detailRecorder.Code != http.StatusNotFound {
		t.Errorf(
			"future detail path status: got %d, want %d",
			detailRecorder.Code,
			http.StatusNotFound,
		)
	}
}

// TestArchitectureDesignTemplateUsesDataAndEscapesHTML renders the Architecture
// wrapper with values that cannot be confused with production handler copy.
//
// Unsafe markup proves html/template emits managed values as inert visible text,
// while interleaved sentinels prove the template reads each preview record
// instead of hard-coding the temporary application source.
func TestArchitectureDesignTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	listing := &architectureProjectListingData{
		Eyebrow:      "Sentinel architecture eyebrow",
		Heading:      "Sentinel architecture heading",
		Introduction: "Sentinel architecture introduction",
		EmptyMessage: "Sentinel architecture empty message",
		Items: []architectureProjectPreviewData{
			{
				Number:   "A1",
				Title:    "<b>Unsafe architecture title</b>",
				Typology: "<em>Unsafe architecture typology</em>",
				Status:   "First sentinel status",
			},
			{
				Number:   "B2",
				Title:    "Second sentinel architecture title",
				Typology: "Second sentinel architecture typology",
				Status:   "<script>Unsafe status</script>",
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
				Number:   "S-02",
				Name:     "Sentinel Architecture Design",
				NextName: "Sentinel Next",
				NextPath: "/sentinel-next",
			},
			ArchitectureProjectListing: listing,
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	mainElement := extractMainElement(t, recorder.Body.String())
	normalizedMain := normalizeHTMLWhitespace(mainElement)

	for _, expected := range []string{
		listing.Eyebrow,
		listing.Heading,
		listing.Introduction,
	} {
		if !strings.Contains(normalizedMain, expected) {
			t.Errorf(
				"sentinel Architecture main does not contain %q",
				expected,
			)
		}
	}

	articles := extractArchitecturePreviewArticles(t, mainElement)
	if len(articles) != len(listing.Items) {
		t.Fatalf(
			"sentinel article count: got %d, want %d",
			len(articles),
			len(listing.Items),
		)
	}

	firstArticle := normalizeHTMLWhitespace(articles[0])
	for _, expected := range []string{
		"&lt;em&gt;Unsafe architecture typology&lt;/em&gt; / Project slot A1",
		"&lt;b&gt;Unsafe architecture title&lt;/b&gt;",
		"First sentinel status",
	} {
		if !strings.Contains(firstArticle, expected) {
			t.Errorf(
				"first sentinel article does not contain %q",
				expected,
			)
		}
	}

	secondArticle := normalizeHTMLWhitespace(articles[1])
	for _, expected := range []string{
		"Second sentinel architecture typology / Project slot B2",
		"Second sentinel architecture title",
		"&lt;script&gt;Unsafe status&lt;/script&gt;",
	} {
		if !strings.Contains(secondArticle, expected) {
			t.Errorf(
				"second sentinel article does not contain %q",
				expected,
			)
		}
	}

	for _, unsafeMarkup := range []string{
		"<b>Unsafe architecture title</b>",
		"<em>Unsafe architecture typology</em>",
		"<script>Unsafe status</script>",
	} {
		if strings.Contains(mainElement, unsafeMarkup) {
			t.Errorf(
				"sentinel Architecture main contains raw markup %q",
				unsafeMarkup,
			)
		}
	}

	// Production values inside the scoped main would reveal card content coded
	// directly into HTML instead of supplied by ArchitectureProjectListing.
	for _, productionValue := range []string{
		"Architecture Study 01",
		"Residential",
		"Commercial",
		"Cultural",
		"Civic",
		"Portfolio preview",
	} {
		if strings.Contains(normalizedMain, productionValue) {
			t.Errorf(
				"sentinel Architecture main contains production value %q",
				productionValue,
			)
		}
	}
}

// TestArchitectureDesignTemplateHandlesEmptyListing verifies that nil and
// allocated zero-length item slices use the same explicit empty-data branch.
//
// A future database can legitimately return zero published projects. The page
// must keep its labelled work section and communicate that state without
// emitting an empty semantic list or fabricated preview cards.
func TestArchitectureDesignTemplateHandlesEmptyListing(t *testing.T) {
	app := newTestApplication(t)

	tests := []struct {
		// name distinguishes the two valid zero-item slice representations.
		name string
		// items is the exact slice value exposed to html/template.
		items []architectureProjectPreviewData
	}{
		{
			name:  "nil items",
			items: nil,
		},
		{
			name:  "empty items",
			items: []architectureProjectPreviewData{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			emptyMessage := "<strong>No sentinel architecture is published.</strong>"
			escapedEmptyMessage := "&lt;strong&gt;No sentinel architecture " +
				"is published.&lt;/strong&gt;"

			app.render(
				recorder,
				http.StatusOK,
				"architecture-design.html",
				pageData{
					Title:       "Empty Architecture Design",
					CurrentPath: "/architecture-design",
					DisciplinePage: &disciplinePageData{
						Number:   "02",
						Name:     "Empty Architecture Design",
						NextName: "Next Discipline",
						NextPath: "/next-discipline",
					},
					ArchitectureProjectListing: &architectureProjectListingData{
						Eyebrow:      "Empty eyebrow",
						Heading:      "Empty architecture index",
						Introduction: "Empty introduction",
						EmptyMessage: emptyMessage,
						Items:        test.items,
					},
				},
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status code: got %d, want %d",
					recorder.Code,
					http.StatusOK,
				)
			}

			workElement := extractElementByMarker(
				t,
				extractMainElement(t, recorder.Body.String()),
				`class="discipline-work"`,
				"section",
			)
			workHeading := extractElementByMarker(
				t,
				workElement,
				`id="selected-work-title"`,
				"h2",
			)

			if !strings.Contains(
				normalizeHTMLWhitespace(workHeading),
				"Empty architecture index",
			) {
				t.Error(
					"empty Architecture heading does not use view data",
				)
			}
			if !strings.Contains(
				normalizeHTMLWhitespace(workElement),
				escapedEmptyMessage,
			) {
				t.Error(
					"empty Architecture section does not escape and state its status",
				)
			}
			if strings.Contains(workElement, emptyMessage) {
				t.Error(
					"empty Architecture section contains raw status markup",
				)
			}
			if strings.Contains(
				workElement,
				`class="architecture-portfolio"`,
			) {
				t.Error(
					"empty Architecture section must not emit an empty list",
				)
			}
			if len(
				extractArchitecturePreviewArticles(t, workElement),
			) != 0 {
				t.Error(
					"empty Architecture section must not emit preview articles",
				)
			}
		})
	}
}

// TestArchitectureDesignTemplatePreservesSectionWithoutListingData protects
// the shared aria-labelledby contract if a future handler omits the optional
// ArchitectureProjectListing pointer.
//
// The production handler supplies this value. The fallback is defensive
// presentation only: it retains a truthful h2 and status without inventing
// records or concealing the missing handler initialization.
func TestArchitectureDesignTemplatePreservesSectionWithoutListingData(
	t *testing.T,
) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:       "Architecture fallback",
			CurrentPath: "/architecture-design",
			DisciplinePage: &disciplinePageData{
				Number:   "02",
				Name:     "Architecture fallback",
				NextName: "Next Discipline",
				NextPath: "/next-discipline",
			},
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	workElement := extractElementByMarker(
		t,
		extractMainElement(t, recorder.Body.String()),
		`class="discipline-work"`,
		"section",
	)
	workHeading := extractElementByMarker(
		t,
		workElement,
		`id="selected-work-title"`,
		"h2",
	)

	if !strings.Contains(
		normalizeHTMLWhitespace(workHeading),
		"Architecture project index",
	) {
		t.Error(
			"missing ArchitectureProjectListing fallback does not preserve its h2",
		)
	}
	if len(extractArchitecturePreviewArticles(t, workElement)) != 0 {
		t.Error(
			"missing ArchitectureProjectListing fallback must not emit articles",
		)
	}
}

// TestArchitecturePresentationDoesNotLeak verifies page-template cache and
// route-stylesheet isolation.
//
// architecture-design.html defines the same named work-block override as the
// other discipline wrappers. Separate template sets must keep its stylesheet
// and markup absent from the homepage and every Product or Interior route.
func TestArchitecturePresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/interior-design",
		"/interior-design/interior-study-01",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				path,
				nil,
			)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status code: got %d, want %d",
					recorder.Code,
					http.StatusOK,
				)
			}

			body := recorder.Body.String()
			if strings.Contains(
				body,
				`href="/static/css/architecture-design.css"`,
			) {
				t.Error(
					"non-Architecture route loads Architecture stylesheet",
				)
			}
			if strings.Contains(
				body,
				`class="architecture-portfolio"`,
			) {
				t.Error(
					"non-Architecture route renders Architecture portfolio",
				)
			}
			if len(
				extractArchitecturePreviewArticles(t, body),
			) != 0 {
				t.Error(
					"non-Architecture route renders an Architecture preview",
				)
			}
		})
	}
}

// TestArchitectureDesignStylesheetRoute verifies that the existing static file
// server exposes the new route-specific stylesheet with a CSS media type.
//
// One stable root selector catches an empty or incorrectly mapped response while
// allowing the detailed grid and decorative declarations to evolve after
// responsive browser review.
func TestArchitectureDesignStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/architecture-design.css",
		nil,
	)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	contentType := recorder.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/css") {
		t.Errorf(
			"Content-Type: got %q, want prefix %q",
			contentType,
			"text/css",
		)
	}

	if !strings.Contains(
		recorder.Body.String(),
		".architecture-portfolio",
	) {
		t.Error(
			"Architecture stylesheet does not contain its portfolio selector",
		)
	}
}
