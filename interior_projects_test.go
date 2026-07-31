package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// extractInteriorPreviewArticles returns complete interior-preview articles in
// their document order.
//
// Stage 8 does not nest article elements, so a direct opening/closing scan keeps
// this focused test helper independent from a third-party HTML parser. An empty
// result is valid and lets empty-state tests prove that no cards were emitted.
func extractInteriorPreviewArticles(
	t *testing.T,
	source string,
) []string {
	t.Helper()

	const classMarker = `class="interior-preview`
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

		articleStart := strings.LastIndex(
			remaining[:classPosition],
			"<article",
		)
		if articleStart == -1 {
			t.Fatal(
				"interior-preview class does not belong to an article",
			)
		}

		articleEnd := strings.Index(
			remaining[articleStart:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatal(
				"interior-preview article does not have a closing tag",
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

// TestInteriorProjectPreviewsPreservesOrderAndFields verifies the explicit
// source-to-template mapping independently from production fixture values.
//
// Sentinel records prove that every supported field remains associated with
// its source record and that ordering is stable. Nil input covers the future
// zero-row repository case without defining any database behavior.
func TestInteriorProjectPreviewsPreservesOrderAndFields(t *testing.T) {
	source := []interiorProject{
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

	previews := interiorProjectPreviews(source)
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

	if previews := interiorProjectPreviews(nil); len(previews) != 0 {
		t.Errorf(
			"nil source preview count: got %d, want 0",
			len(previews),
		)
	}
}

// TestInteriorDesignRouteRendersPortfolio verifies the complete public Stage 8
// response produced by interiorDesignHandler.
//
// Expected records are derived from the application's temporary source rather
// than duplicated as test fixtures. The assertions own semantic structure,
// stylesheet ordering, source association, and the listing-only stage boundary.
func TestInteriorDesignRouteRendersPortfolio(t *testing.T) {
	app := newTestApplication(t)
	// Replacing the application-owned source before the request proves the
	// handler reads its dependency instead of reconstructing production fixtures
	// internally. Two records also keep this contract independent from a fixed
	// production item count.
	app.interiorProjects = []interiorProject{
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
		"/interior-design",
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

	// The page builds on discipline.css. Both assets must appear exactly once,
	// and the route-specific file must follow the shared foundation in source
	// order so its intentionally narrower rules can compose safely.
	disciplineStyles := `href="/static/css/discipline.css"`
	interiorStyles := `href="/static/css/interior-design.css"`
	if count := strings.Count(body, disciplineStyles); count != 1 {
		t.Errorf(
			"discipline stylesheet count: got %d, want 1",
			count,
		)
	}
	if count := strings.Count(body, interiorStyles); count != 1 {
		t.Errorf(
			"Interior stylesheet count: got %d, want 1",
			count,
		)
	}
	if strings.Index(body, interiorStyles) <
		strings.Index(body, disciplineStyles) {
		t.Error(
			"Interior stylesheet must load after discipline stylesheet",
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

	// Reusing the shared work section must preserve its fragment destination and
	// aria-labelledby relationship while replacing only the inner content.
	if !strings.Contains(
		workOpening,
		`id="selected-work"`,
	) || !strings.Contains(
		workOpening,
		`aria-labelledby="selected-work-title"`,
	) {
		t.Error(
			"Interior work section does not own its id and heading label",
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
		"Interior project index",
	) {
		t.Error(
			"Interior work heading does not contain Interior project index",
		)
	}

	portfolio := extractElementByMarker(
		t,
		workElement,
		`class="interior-portfolio"`,
		"ol",
	)
	portfolioOpening := extractOpeningTag(t, portfolio)
	if !strings.Contains(
		portfolioOpening,
		`aria-label="Interior project previews"`,
	) {
		t.Error(
			"Interior portfolio does not have its accessible label",
		)
	}
	if !strings.Contains(
		portfolioOpening,
		`role="list"`,
	) {
		t.Error(
			"Interior portfolio does not preserve explicit list semantics",
		)
	}

	expectedItems := interiorProjectPreviews(app.interiorProjects)
	articles := extractInteriorPreviewArticles(t, portfolio)
	if len(articles) != len(expectedItems) {
		t.Fatalf(
			"Interior article count: got %d, want %d",
			len(articles),
			len(expectedItems),
		)
	}
	if count := strings.Count(
		portfolio,
		`class="interior-portfolio__item"`,
	); count != len(expectedItems) {
		t.Errorf(
			"Interior list-item count: got %d, want %d",
			count,
			len(expectedItems),
		)
	}

	for index, expected := range expectedItems {
		article := articles[index]
		normalizedArticle := normalizeHTMLWhitespace(article)

		// Matching slice positions proves that the template range preserves the
		// source's editorial order and keeps every mapped field in one article.
		expectedStrings := []string{
			expected.Typology + " / Project slot " + expected.Number,
			expected.Title,
			expected.Status,
		}
		for _, expectedString := range expectedStrings {
			if !strings.Contains(
				normalizedArticle,
				expectedString,
			) {
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
		if count := strings.Count(
			article,
			"<h3",
		); count != 1 {
			t.Errorf(
				"article %d title heading count: got %d, want 1",
				index,
				count,
			)
		}

		// The repeated editorial number is decorative because the same value is
		// present in visible metadata. Verify the media container, rather than a
		// descendant, owns the assistive-technology exclusion.
		media := extractElementByMarker(
			t,
			article,
			`class="interior-preview__media"`,
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
	}

	// Placeholder fragment links and design composites are never valid public
	// content, even though the reference image informed the grid composition.
	if strings.Contains(mainElement, `href="#"`) {
		t.Error(
			"Interior Design main must not contain a placeholder href",
		)
	}
	if strings.Contains(mainElement, "docs/reference") {
		t.Error(
			"Interior Design page must not render a reference composite",
		)
	}

}

// TestInteriorDesignTemplateUsesDataAndEscapesHTML renders the Interior wrapper
// with sentinel values that cannot be confused with production handler copy.
//
// Interleaved fields prove that each article uses its own preview record, while
// unsafe markup verifies that html/template converts managed text into inert
// visible characters rather than executable page elements.
func TestInteriorDesignTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	listing := &interiorProjectListingData{
		Eyebrow:      "Sentinel interiors eyebrow",
		Heading:      "Sentinel interiors heading",
		Introduction: "Sentinel interiors introduction",
		EmptyMessage: "Sentinel interiors empty message",
		Items: []interiorProjectPreviewData{
			{
				Number:   "A1",
				Title:    "<b>Unsafe interior title</b>",
				Typology: "<em>Unsafe typology</em>",
				Status:   "First sentinel status",
			},
			{
				Number:   "B2",
				Title:    "Second sentinel title",
				Typology: "Second sentinel typology",
				Status:   "<script>Unsafe status</script>",
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
				NextName: "Sentinel Next",
				NextPath: "/sentinel-next",
			},
			InteriorProjectListing: listing,
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	mainElement := extractMainElement(
		t,
		recorder.Body.String(),
	)
	normalizedMain := normalizeHTMLWhitespace(mainElement)

	for _, expected := range []string{
		listing.Eyebrow,
		listing.Heading,
		listing.Introduction,
	} {
		if !strings.Contains(normalizedMain, expected) {
			t.Errorf(
				"sentinel Interior main does not contain %q",
				expected,
			)
		}
	}

	articles := extractInteriorPreviewArticles(t, mainElement)
	if len(articles) != len(listing.Items) {
		t.Fatalf(
			"sentinel article count: got %d, want %d",
			len(articles),
			len(listing.Items),
		)
	}

	firstArticle := normalizeHTMLWhitespace(articles[0])
	for _, expected := range []string{
		"&lt;em&gt;Unsafe typology&lt;/em&gt; / Project slot A1",
		"&lt;b&gt;Unsafe interior title&lt;/b&gt;",
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
		"Second sentinel typology / Project slot B2",
		"Second sentinel title",
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
		"<b>Unsafe interior title</b>",
		"<em>Unsafe typology</em>",
		"<script>Unsafe status</script>",
	} {
		if strings.Contains(mainElement, unsafeMarkup) {
			t.Errorf(
				"sentinel Interior main contains raw markup %q",
				unsafeMarkup,
			)
		}
	}

}

// TestInteriorDesignTemplateHandlesEmptyListing verifies that nil and allocated
// zero-length item slices take the same explicit empty-data branch.
//
// This remains useful when a later repository returns zero published projects:
// the page keeps its labelled work section and never emits an empty list.
func TestInteriorDesignTemplateHandlesEmptyListing(t *testing.T) {
	app := newTestApplication(t)

	tests := []struct {
		// name distinguishes the two zero-item slice representations.
		name string
		// items is the exact slice value supplied to html/template.
		items []interiorProjectPreviewData
	}{
		{
			name:  "nil items",
			items: nil,
		},
		{
			name:  "empty items",
			items: []interiorProjectPreviewData{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			emptyMessage := "<strong>No sentinel interiors are published.</strong>"
			escapedEmptyMessage := "&lt;strong&gt;No sentinel interiors " +
				"are published.&lt;/strong&gt;"

			app.render(
				recorder,
				http.StatusOK,
				"interior-design.html",
				pageData{
					Title:       "Empty Interior Design",
					CurrentPath: "/interior-design",
					DisciplinePage: &disciplinePageData{
						Number:   "01",
						Name:     "Empty Interior Design",
						NextName: "Next Discipline",
						NextPath: "/next-discipline",
					},
					InteriorProjectListing: &interiorProjectListingData{
						Eyebrow:      "Empty eyebrow",
						Heading:      "Empty interior index",
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
				"Empty interior index",
			) {
				t.Error(
					"empty Interior heading does not use view data",
				)
			}
			if !strings.Contains(
				normalizeHTMLWhitespace(workElement),
				escapedEmptyMessage,
			) {
				t.Error(
					"empty Interior section does not escape and state its status",
				)
			}
			if strings.Contains(workElement, emptyMessage) {
				t.Error(
					"empty Interior section contains raw status markup",
				)
			}
			if strings.Contains(
				workElement,
				`class="interior-portfolio"`,
			) {
				t.Error(
					"empty Interior section must not emit an empty list",
				)
			}
			if len(
				extractInteriorPreviewArticles(t, workElement),
			) != 0 {
				t.Error(
					"empty Interior section must not emit preview articles",
				)
			}
		})
	}
}

// TestInteriorDesignTemplatePreservesSectionWithoutListingData protects the
// shared aria-labelledby contract if a future handler omits the optional
// InteriorProjectListing pointer.
//
// The fallback is defensive presentation only: it retains a truthful h2 and
// status without inventing cards or hiding a handler initialization mistake.
func TestInteriorDesignTemplatePreservesSectionWithoutListingData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"interior-design.html",
		pageData{
			Title:       "Interior fallback",
			CurrentPath: "/interior-design",
			DisciplinePage: &disciplinePageData{
				Number:   "01",
				Name:     "Interior fallback",
				NextName: "Next Discipline",
				NextPath: "/next-discipline",
			},
		},
	)

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
		"Interior project index",
	) {
		t.Error(
			"missing InteriorProjectListing fallback does not preserve its h2",
		)
	}
	if len(extractInteriorPreviewArticles(t, workElement)) != 0 {
		t.Error(
			"missing InteriorProjectListing fallback must not emit preview articles",
		)
	}
}

// TestInteriorPresentationDoesNotLeak verifies page-template cache and
// stylesheet isolation.
//
// interior-design.html defines the same named override used by Products.
// Parsing one template set per page must keep both its markup and stylesheet
// absent from every unrelated public route.
func TestInteriorPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/architecture-design",
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
				`href="/static/css/interior-design.css"`,
			) {
				t.Error(
					"non-Interior route loads Interior stylesheet",
				)
			}
			if strings.Contains(
				body,
				"interior-portfolio",
			) {
				t.Error(
					"non-Interior route renders Interior portfolio",
				)
			}
			if strings.Contains(
				body,
				"interior-preview",
			) {
				t.Error(
					"non-Interior route renders an Interior preview",
				)
			}
		})
	}
}

// TestInteriorDesignStylesheetRoute verifies that the shared static file server
// exposes the new route-specific stylesheet with the correct media type.
//
// One stable root selector catches an empty or mismapped response while leaving
// detailed visual declarations free to evolve after browser review.
func TestInteriorDesignStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/interior-design.css",
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
		".interior-portfolio",
	) {
		t.Error(
			"Interior stylesheet does not contain its portfolio selector",
		)
	}
}
