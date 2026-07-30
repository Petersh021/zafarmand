package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// extractProductPreviewArticles returns each complete product-preview article
// from source in document order.
//
// The Stage 6 template does not nest article elements, so a direct opening and
// closing-tag scan is sufficient and keeps these tests independent from a
// third-party HTML parser. Returning an empty slice is intentional: empty-data
// tests can assert that no catalogue articles were emitted.
func extractProductPreviewArticles(
	t *testing.T,
	source string,
) []string {
	t.Helper()

	const openingMarker = `<article class="product-preview">`
	const closingMarker = "</article>"

	var articles []string
	remaining := source

	for {
		articleStart := strings.Index(
			remaining,
			openingMarker,
		)
		if articleStart == -1 {
			break
		}

		articleEnd := strings.Index(
			remaining[articleStart:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatal(
				"product-preview article does not have a closing tag",
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

// TestProductsRouteRendersCatalogue verifies the complete public Stage 6
// response produced by productsHandler.
//
// The assertions prove that route data becomes one labelled semantic list in
// the same order as the Go slice. They deliberately avoid product-detail links
// because no such server routes exist in this stage.
func TestProductsRouteRendersCatalogue(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/products",
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

	body := recorder.Body.String()

	// Products builds on discipline.css, so both stylesheets must appear once
	// and the specializing file must load after the shared foundation.
	disciplineStyles := `href="/static/css/discipline.css"`
	productStyles := `href="/static/css/products.css"`
	if count := strings.Count(body, disciplineStyles); count != 1 {
		t.Errorf(
			"discipline stylesheet count: got %d, want 1",
			count,
		)
	}
	if count := strings.Count(body, productStyles); count != 1 {
		t.Errorf(
			"products stylesheet count: got %d, want 1",
			count,
		)
	}

	disciplinePosition := strings.Index(body, disciplineStyles)
	productPosition := strings.Index(body, productStyles)
	if disciplinePosition == -1 ||
		productPosition == -1 ||
		productPosition < disciplinePosition {
		t.Error(
			"products stylesheet must load after discipline stylesheet",
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

	// The inherited selected-work section must continue to own the id and h2
	// association used by native fragment links and assistive technology.
	if !strings.Contains(
		workOpening,
		`id="selected-work"`,
	) || !strings.Contains(
		workOpening,
		`aria-labelledby="selected-work-title"`,
	) {
		t.Error(
			"Products work section does not own its id and heading label",
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
		"Product catalogue",
	) {
		t.Error(
			"Products work heading does not contain Product catalogue",
		)
	}

	catalogue := extractElementByMarker(
		t,
		workElement,
		`class="products-catalogue"`,
		"ol",
	)
	catalogueOpening := extractOpeningTag(t, catalogue)
	if !strings.Contains(
		catalogueOpening,
		`aria-label="Product catalogue previews"`,
	) {
		t.Error(
			"Products catalogue does not have its accessible label",
		)
	}
	if !strings.Contains(
		catalogueOpening,
		`role="list"`,
	) {
		t.Error(
			"Products catalogue does not preserve explicit list semantics",
		)
	}

	expectedItems := []struct {
		// number is the visible catalogue-slot sequence supplied by Go.
		number string
		// category is the article's h3 and broad product family.
		category string
		// status is the truthful temporary-content state.
		status string
	}{
		{
			number:   "01",
			category: "Furniture",
			status:   "Content in preparation",
		},
		{
			number:   "02",
			category: "Lighting",
			status:   "Content in preparation",
		},
		{
			number:   "03",
			category: "Objects",
			status:   "Content in preparation",
		},
		{
			number:   "04",
			category: "Materials",
			status:   "Content in preparation",
		},
	}

	articles := extractProductPreviewArticles(t, catalogue)
	if len(articles) != len(expectedItems) {
		t.Fatalf(
			"product article count: got %d, want %d",
			len(articles),
			len(expectedItems),
		)
	}

	for index, expected := range expectedItems {
		article := articles[index]
		normalizedArticle := normalizeHTMLWhitespace(article)

		// Matching by slice index proves template range preserves the handler's
		// editorial ordering and that fields stay associated inside one article.
		expectedStrings := []string{
			"Catalogue slot " + expected.number,
			expected.category,
			expected.status,
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

		categoryHeading := extractElementByMarker(
			t,
			article,
			expected.category,
			"h3",
		)
		if count := strings.Count(
			categoryHeading,
			"<h3",
		); count != 1 {
			t.Errorf(
				"article %d category heading count: got %d, want 1",
				index,
				count,
			)
		}
	}

	// Nonexistent detail routes must not be represented as fake controls.
	if strings.Contains(catalogue, "href=") {
		t.Error(
			"Products catalogue must not link before detail routes exist",
		)
	}
	if strings.Contains(catalogue, "<button") {
		t.Error(
			"Products catalogue must not expose an inactive button",
		)
	}
	if strings.Contains(mainElement, `href="#"`) {
		t.Error(
			"Products main must not contain a placeholder href",
		)
	}

	// Design composites stay in docs/reference and must never become public
	// image URLs merely because they informed the layout.
	if strings.Contains(
		mainElement,
		"docs/reference",
	) {
		t.Error(
			"Products page must not render a reference composite",
		)
	}
}

// TestProductsTemplateUsesDataAndEscapesHTML renders products.html with values
// that cannot be confused with production handler copy.
//
// The interleaved sentinel strings prove each preview reads the correct Go
// fields, while unsafe markup proves html/template escapes untrusted text
// instead of allowing it to become executable page structure.
func TestProductsTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	productListing := &productListingData{
		Eyebrow:      "Sentinel eyebrow",
		Heading:      "Sentinel catalogue",
		Introduction: "Sentinel introduction",
		EmptyMessage: "Sentinel empty message",
		Items: []productPreviewData{
			{
				Number:   "A1",
				Category: "<b>Unsafe category</b>",
				Status:   "First sentinel status",
			},
			{
				Number:   "B2",
				Category: "Second sentinel category",
				Status:   "<script>Unsafe status</script>",
			},
		},
	}

	app.render(
		recorder,
		http.StatusOK,
		"products.html",
		pageData{
			Title:       "Sentinel Products",
			CurrentPath: "/products",
			DisciplinePage: &disciplinePageData{
				Number:   "S-03",
				Name:     "Sentinel Products",
				NextName: "Sentinel Next",
				NextPath: "/sentinel-next",
			},
			ProductListing: productListing,
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
		"Sentinel eyebrow",
		"Sentinel catalogue",
		"Sentinel introduction",
	} {
		if !strings.Contains(normalizedMain, expected) {
			t.Errorf(
				"sentinel Products main does not contain %q",
				expected,
			)
		}
	}

	articles := extractProductPreviewArticles(t, mainElement)
	if len(articles) != len(productListing.Items) {
		t.Fatalf(
			"sentinel article count: got %d, want %d",
			len(articles),
			len(productListing.Items),
		)
	}

	firstArticle := normalizeHTMLWhitespace(articles[0])
	for _, expected := range []string{
		"Catalogue slot A1",
		"&lt;b&gt;Unsafe category&lt;/b&gt;",
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
		"Catalogue slot B2",
		"Second sentinel category",
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
		"<b>Unsafe category</b>",
		"<script>Unsafe status</script>",
	} {
		if strings.Contains(mainElement, unsafeMarkup) {
			t.Errorf(
				"sentinel Products main contains raw markup %q",
				unsafeMarkup,
			)
		}
	}

	// Production categories inside the scoped main would reveal hard-coded card
	// content instead of the custom ProductListing supplied by this test.
	for _, productionCategory := range []string{
		"Furniture",
		"Lighting",
		"Objects",
		"Materials",
	} {
		if strings.Contains(
			normalizedMain,
			productionCategory,
		) {
			t.Errorf(
				"sentinel Products main contains production category %q",
				productionCategory,
			)
		}
	}
}

// TestProductsTemplateHandlesEmptyListing verifies that both nil and empty
// item slices take the same explicit empty-data path.
//
// This behavior will remain useful when a later database query legitimately
// returns zero published products: the page keeps its labelled section and
// communicates the state without emitting an empty list.
func TestProductsTemplateHandlesEmptyListing(t *testing.T) {
	app := newTestApplication(t)

	tests := []struct {
		// name distinguishes a nil slice from an allocated zero-length slice.
		name string
		// items is the exact slice representation supplied to the template.
		items []productPreviewData
	}{
		{
			name:  "nil items",
			items: nil,
		},
		{
			name:  "empty items",
			items: []productPreviewData{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			emptyMessage := "<strong>No sentinel entries are published.</strong>"
			escapedEmptyMessage := "&lt;strong&gt;No sentinel entries " +
				"are published.&lt;/strong&gt;"

			app.render(
				recorder,
				http.StatusOK,
				"products.html",
				pageData{
					Title:       "Empty Products",
					CurrentPath: "/products",
					DisciplinePage: &disciplinePageData{
						Number:   "03",
						Name:     "Empty Products",
						NextName: "Next Discipline",
						NextPath: "/next-discipline",
					},
					ProductListing: &productListingData{
						Eyebrow:      "Empty eyebrow",
						Heading:      "Empty catalogue",
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

			mainElement := extractMainElement(
				t,
				recorder.Body.String(),
			)
			workElement := extractElementByMarker(
				t,
				mainElement,
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
				"Empty catalogue",
			) {
				t.Error(
					"empty Products heading does not use view data",
				)
			}

			if !strings.Contains(
				normalizeHTMLWhitespace(workElement),
				escapedEmptyMessage,
			) {
				t.Error(
					"empty Products section does not escape and state its status",
				)
			}
			if strings.Contains(workElement, emptyMessage) {
				t.Error(
					"empty Products section contains raw status markup",
				)
			}

			if strings.Contains(
				workElement,
				`class="products-catalogue"`,
			) {
				t.Error(
					"empty Products section must not emit an empty list",
				)
			}
			if len(
				extractProductPreviewArticles(t, workElement),
			) != 0 {
				t.Error(
					"empty Products section must not emit preview articles",
				)
			}
		})
	}
}

// TestProductsTemplatePreservesSectionWithoutListingData protects the shared
// aria-labelledby contract if a future Products handler omits ProductListing.
//
// The normal production handler always supplies the pointer. This fallback test
// covers defensive rendering only and does not replace application-level error
// handling or database validation in later stages.
func TestProductsTemplatePreservesSectionWithoutListingData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"products.html",
		pageData{
			Title:       "Products fallback",
			CurrentPath: "/products",
			DisciplinePage: &disciplinePageData{
				Number:   "03",
				Name:     "Products fallback",
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
		"Product catalogue",
	) {
		t.Error(
			"missing ProductListing fallback does not preserve its h2",
		)
	}
	if len(extractProductPreviewArticles(t, workElement)) != 0 {
		t.Error(
			"missing ProductListing fallback must not emit preview articles",
		)
	}
}

// TestProductPresentationDoesNotLeak verifies page-template cache isolation.
//
// products.html defines an override with the same name as the shared default
// block. Because newTemplateCache creates one template set per page, neither
// that markup nor products.css may appear on any unrelated public route.
func TestProductPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/interior-design",
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
				`href="/static/css/products.css"`,
			) {
				t.Error(
					"non-Products route loads products stylesheet",
				)
			}
			if strings.Contains(
				body,
				`class="products-catalogue"`,
			) {
				t.Error(
					"non-Products route renders products catalogue",
				)
			}
			if strings.Contains(
				body,
				`class="product-preview"`,
			) {
				t.Error(
					"non-Products route renders a product preview",
				)
			}
		})
	}
}

// TestProductsStylesheetRoute verifies that the existing static file server
// exposes the new page-specific stylesheet with the expected media type.
//
// Checking one stable root selector also catches a mistakenly empty or
// unrelated file while allowing declarations to evolve during visual tuning.
func TestProductsStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/products.css",
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
		".products-catalogue",
	) {
		t.Error(
			"products stylesheet does not contain its catalogue selector",
		)
	}
}
