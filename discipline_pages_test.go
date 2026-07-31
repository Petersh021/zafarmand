package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// extractMainElement returns the response substring from the one opening main
// tag through its closing tag.
//
// Scoping route-specific assertions to main prevents repeated discipline names
// and paths in the shared header or drawer from producing false positives.
// Tests fail immediately when the landmark is missing or malformed so callers
// never continue with an invalid substring boundary.
func extractMainElement(
	t *testing.T,
	body string,
) string {
	t.Helper()

	mainStart := strings.Index(body, "<main")
	if mainStart == -1 {
		t.Fatal("response does not contain a main element")
	}

	mainEnd := strings.Index(
		body[mainStart:],
		"</main>",
	)
	if mainEnd == -1 {
		t.Fatal("response main element is not closed")
	}

	return body[mainStart : mainStart+mainEnd+len("</main>")]
}

// normalizeHTMLWhitespace collapses template indentation and line breaks into
// single spaces.
//
// The browser treats these whitespace runs equivalently, so normalized strings
// let tests verify visible field order without coupling assertions to formatting
// decisions made for readable templates.
func normalizeHTMLWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// extractElementByMarker isolates one complete non-void HTML element whose
// opening tag or text contains marker.
//
// The helper verifies the expected tag name before searching for its closing
// tag. This lets tests associate ids, aria attributes, href values, and visible
// text with the element that is supposed to own them instead of merely counting
// unrelated strings anywhere in the response.
func extractElementByMarker(
	t *testing.T,
	source string,
	marker string,
	tagName string,
) string {
	t.Helper()

	markerPosition := strings.Index(source, marker)
	if markerPosition == -1 {
		t.Fatalf(
			"source does not contain marker %q",
			marker,
		)
	}

	elementStart := strings.LastIndex(
		source[:markerPosition],
		"<",
	)
	if elementStart == -1 {
		t.Fatalf(
			"marker %q does not follow an opening tag",
			marker,
		)
	}

	expectedOpening := "<" + tagName
	if !strings.HasPrefix(
		source[elementStart:],
		expectedOpening,
	) {
		t.Fatalf(
			"marker %q belongs to %q, want %q",
			marker,
			source[elementStart:markerPosition],
			expectedOpening,
		)
	}

	closingTag := "</" + tagName + ">"
	elementEnd := strings.Index(
		source[elementStart:],
		closingTag,
	)
	if elementEnd == -1 {
		t.Fatalf(
			"%s element for marker %q is not closed",
			tagName,
			marker,
		)
	}

	return source[elementStart : elementStart+elementEnd+len(closingTag)]
}

// extractOpeningTag returns the first start tag from an already isolated
// element. Callers use it to prove attributes belong to the landmark or heading
// being tested rather than to one of its descendants.
func extractOpeningTag(
	t *testing.T,
	element string,
) string {
	t.Helper()

	openingEnd := strings.Index(element, ">")
	if openingEnd == -1 {
		t.Fatal("element does not contain a complete opening tag")
	}

	return element[:openingEnd+1]
}

// TestDisciplinePageRoutes verifies that all three real URLs retain the shared
// Stage 5 shell with their own Go-supplied editorial data.
//
// This test complements TestPageRoutes: that existing test owns the common
// document title, CurrentPath, and aria-current contract, while this table owns
// discipline-page semantics, stylesheet isolation, and route-cycle data.
// Products and Interior Design now specialize their work content, so the table
// also states which visible h2 and default-status behavior belongs to each route.
func TestDisciplinePageRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	tests := []struct {
		// name labels failures with the discipline being exercised.
		name string
		// path is the real server-rendered landing-page URL.
		path string
		// number is the zero-padded editorial position expected in the hero.
		number string
		// nextName is the visible destination in the route-cycle navigation.
		nextName string
		// nextPath is the registered URL paired with nextName.
		nextPath string
		// workHeading is the visible h2 labelling the selected-work section.
		workHeading string
		// usesDefaultWork is true for routes still using the Stage 5 empty state.
		usesDefaultWork bool
	}{
		{
			name:            "Products",
			path:            "/products",
			number:          "03",
			nextName:        "Interior Design",
			nextPath:        "/interior-design",
			workHeading:     "Product catalogue",
			usesDefaultWork: false,
		},
		{
			name:            "Interior Design",
			path:            "/interior-design",
			number:          "01",
			nextName:        "Architecture Design",
			nextPath:        "/architecture-design",
			workHeading:     "Interior project index",
			usesDefaultWork: false,
		},
		{
			name:            "Architecture Design",
			path:            "/architecture-design",
			number:          "02",
			nextName:        "Products",
			nextPath:        "/products",
			workHeading:     "Selected work",
			usesDefaultWork: true,
		},
	}

	for _, test := range tests {
		// A named subtest keeps one shared contract while making any
		// route-specific regression immediately identifiable.
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				test.path,
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

			// One page-specific stylesheet proves the wrapper called the shared
			// style partial exactly once. home.css must remain homepage-only.
			if count := strings.Count(
				body,
				`href="/static/css/discipline.css"`,
			); count != 1 {
				t.Errorf(
					"discipline stylesheet count: got %d, want 1",
					count,
				)
			}

			if strings.Contains(
				body,
				`href="/static/css/home.css"`,
			) {
				t.Error(
					"discipline page must not load the homepage stylesheet",
				)
			}

			// A document needs one main landmark and one unique skip-link target.
			// Counting the full response catches accidental nested or duplicated
			// landmarks before route-specific assertions inspect its substring.
			if count := strings.Count(body, "<main"); count != 1 {
				t.Errorf(
					"main element count: got %d, want 1",
					count,
				)
			}

			if count := strings.Count(
				body,
				`id="main-content"`,
			); count != 1 {
				t.Errorf(
					"main-content id count: got %d, want 1",
					count,
				)
			}

			mainElement := extractMainElement(t, body)
			mainOpening := extractOpeningTag(t, mainElement)

			// Association matters as much as uniqueness: the main element itself
			// must own the skip target, and the base skip link must reference it.
			if !strings.Contains(
				mainOpening,
				`id="main-content"`,
			) {
				t.Error(
					"main element does not own id=\"main-content\"",
				)
			}

			if count := strings.Count(
				body,
				`href="#main-content"`,
			); count != 1 {
				t.Errorf(
					"skip-link target count: got %d, want 1",
					count,
				)
			}

			// Isolate the actual hero section and h1 so the label reference,
			// target id, and route-specific title are proven on their intended
			// semantic elements.
			heroElement := extractElementByMarker(
				t,
				mainElement,
				`class="discipline-hero"`,
				"section",
			)
			heroOpening := extractOpeningTag(t, heroElement)
			if !strings.Contains(
				heroOpening,
				`aria-labelledby="discipline-title"`,
			) {
				t.Error(
					"discipline hero does not reference discipline-title",
				)
			}

			if count := strings.Count(
				mainElement,
				`class="discipline-hero"`,
			); count != 1 {
				t.Errorf(
					"discipline hero count: got %d, want 1",
					count,
				)
			}

			headingElement := extractElementByMarker(
				t,
				heroElement,
				`id="discipline-title"`,
				"h1",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(headingElement),
				test.name,
			) {
				t.Errorf(
					"discipline h1 does not contain %q",
					test.name,
				)
			}

			if count := strings.Count(heroElement, "<h1"); count != 1 {
				t.Errorf(
					"h1 count: got %d, want 1",
					count,
				)
			}

			// Scope the sequence lookup to its eyebrow so the same number in the
			// decorative watermark cannot satisfy the visible-data assertion.
			eyebrowElement := extractElementByMarker(
				t,
				heroElement,
				`class="discipline-hero__eyebrow"`,
				"p",
			)
			sequenceElement := extractElementByMarker(
				t,
				eyebrowElement,
				test.number,
				"span",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(sequenceElement),
				test.number,
			) {
				t.Errorf(
					"discipline sequence does not contain %q",
					test.number,
				)
			}

			// The selected-work boundary remains structurally shared even when
			// Products replaces its inner content through a named template block.
			workElement := extractElementByMarker(
				t,
				mainElement,
				`class="discipline-work"`,
				"section",
			)
			workOpening := extractOpeningTag(t, workElement)
			if !strings.Contains(
				workOpening,
				`id="selected-work"`,
			) || !strings.Contains(
				workOpening,
				`aria-labelledby="selected-work-title"`,
			) {
				t.Error(
					"selected-work section does not own its id and label",
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
				test.workHeading,
			) {
				t.Errorf(
					"selected-work h2 does not contain %q",
					test.workHeading,
				)
			}

			// Only routes that still use the shared default block should expose
			// its neutral status. Specialized listing routes own dedicated copy.
			defaultStatus := "Selected entries are being prepared " +
				"for publication."
			hasDefaultStatus := strings.Contains(
				normalizeHTMLWhitespace(workElement),
				defaultStatus,
			)
			if test.usesDefaultWork && !hasDefaultStatus {
				t.Error(
					"default selected-work status is missing",
				)
			}
			if !test.usesDefaultWork && hasDefaultStatus {
				t.Error(
					"specialized route unexpectedly renders the default work status",
				)
			}

			// Isolating the labelled nav and its two native anchors proves each
			// visible name is paired with the href it describes.
			routeNavigation := extractElementByMarker(
				t,
				heroElement,
				`class="discipline-route-nav"`,
				"nav",
			)
			routeNavigationOpening := extractOpeningTag(
				t,
				routeNavigation,
			)
			if !strings.Contains(
				routeNavigationOpening,
				`aria-label="Discipline navigation"`,
			) {
				t.Error(
					"discipline route navigation has no accessible label",
				)
			}

			overviewAnchor := extractElementByMarker(
				t,
				routeNavigation,
				`href="/#disciplines"`,
				"a",
			)
			normalizedOverview := normalizeHTMLWhitespace(
				overviewAnchor,
			)
			if !strings.Contains(
				normalizedOverview,
				"Overview",
			) || !strings.Contains(
				normalizedOverview,
				"All disciplines",
			) {
				t.Error(
					"overview anchor does not pair its label and destination",
				)
			}

			expectedNextPath := `href="` +
				test.nextPath +
				`"`
			nextAnchor := extractElementByMarker(
				t,
				routeNavigation,
				expectedNextPath,
				"a",
			)
			normalizedNext := normalizeHTMLWhitespace(nextAnchor)
			if !strings.Contains(
				normalizedNext,
				"Next discipline",
			) || !strings.Contains(
				normalizedNext,
				test.nextName,
			) {
				t.Errorf(
					"next anchor does not pair path %q with name %q",
					test.nextPath,
					test.nextName,
				)
			}

			// Prove the active state belongs to the current route in both shared
			// navigation regions rather than merely counting two aria-current
			// attributes somewhere in the response.
			for _, navigationMarker := range []string{
				`class="discipline-nav"`,
				`class="drawer-disciplines"`,
			} {
				navigation := extractElementByMarker(
					t,
					body,
					navigationMarker,
					"nav",
				)
				currentAnchor := extractElementByMarker(
					t,
					navigation,
					`href="`+test.path+`"`,
					"a",
				)
				if !strings.Contains(
					extractOpeningTag(t, currentAnchor),
					`aria-current="page"`,
				) {
					t.Errorf(
						"current link %q in %s lacks aria-current",
						test.path,
						navigationMarker,
					)
				}

				if count := strings.Count(
					navigation,
					`aria-current="page"`,
				); count != 1 {
					t.Errorf(
						"active link count in %s: got %d, want 1",
						navigationMarker,
						count,
					)
				}
			}

			// Empty fragment links are never valid progressive enhancement.
			// Real future media remains free to enter the vertical-slice tests.
			if strings.Contains(mainElement, `href="#"`) {
				t.Error(
					"shared discipline shell must not render href=\"#\"",
				)
			}

			// Send the advertised next destination through the real router so a
			// typo cannot create a polished-looking broken link.
			nextRecorder := httptest.NewRecorder()
			nextRequest := httptest.NewRequest(
				http.MethodGet,
				test.nextPath,
				nil,
			)
			handler.ServeHTTP(nextRecorder, nextRequest)

			if nextRecorder.Code != http.StatusOK {
				t.Errorf(
					"next path %q status: got %d, want %d",
					test.nextPath,
					nextRecorder.Code,
					http.StatusOK,
				)
			}
		})
	}
}

// TestDisciplinePageTemplateUsesData renders every thin page wrapper with the
// same sentinel values.
//
// Exercising all cache keys proves each wrapper delegates to the shared partial
// and reads DisciplinePage instead of hard-coding its production route body.
func TestDisciplinePageTemplateUsesData(t *testing.T) {
	app := newTestApplication(t)
	disciplinePage := &disciplinePageData{
		Number:   "S-08",
		Name:     "Sentinel Discipline",
		NextName: "Next Sentinel",
		NextPath: "/next-sentinel",
	}

	pageNames := []string{
		"products.html",
		"interior-design.html",
		"architecture-design.html",
	}

	for _, pageName := range pageNames {
		// Each subtest starts with a fresh recorder because an HTTP response
		// writer cannot be reused after its status and body have been committed.
		t.Run(pageName, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			app.render(
				recorder,
				http.StatusOK,
				pageName,
				pageData{
					Title:          disciplinePage.Name,
					CurrentPath:    "/sentinel-discipline",
					DisciplinePage: disciplinePage,
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
			normalizedMain := normalizeHTMLWhitespace(
				mainElement,
			)
			expectedContent := []string{
				"S-08",
				"Sentinel Discipline",
				"Next Sentinel",
				`href="/next-sentinel"`,
			}

			for _, content := range expectedContent {
				if !strings.Contains(normalizedMain, content) {
					t.Errorf(
						"sentinel main does not contain %q",
						content,
					)
				}
			}

			// Production names inside the scoped main would reveal hard-coded
			// landing content. Shared header occurrences remain outside this
			// deliberately isolated check.
			productionNames := []string{
				"Products",
				"Interior Design",
				"Architecture Design",
			}
			for _, name := range productionNames {
				if strings.Contains(normalizedMain, name) {
					t.Errorf(
						"sentinel main unexpectedly contains "+
							"production name %q",
						name,
					)
				}
			}
		})
	}
}

// TestDisciplinePagePreservesMainWithoutData protects the base skip-link
// contract when a future handler has no discipline landing data.
//
// The optional "with" block may omit dynamic sections, but it must never remove
// id="main-content" or emit empty hero fields outside that conditional block.
func TestDisciplinePagePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"products.html",
		pageData{
			Title:       "Empty Discipline",
			CurrentPath: "/products",
		},
	)

	body := recorder.Body.String()
	mainElement := extractMainElement(t, body)

	if count := strings.Count(
		body,
		`id="main-content"`,
	); count != 1 {
		t.Errorf(
			"main-content id count: got %d, want 1",
			count,
		)
	}

	if strings.Contains(
		mainElement,
		`class="discipline-hero"`,
	) {
		t.Error(
			"nil DisciplinePage must not render an empty hero",
		)
	}

	if strings.Contains(mainElement, "<h1") {
		t.Error(
			"nil DisciplinePage must not render an empty h1",
		)
	}
}

// TestDisciplinePresentationDoesNotLeakIntoHomepage verifies that parsing the
// shared partial into every page template set does not execute its blocks on /.
func TestDisciplinePresentationDoesNotLeakIntoHomepage(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()

	if strings.Contains(
		body,
		`href="/static/css/discipline.css"`,
	) {
		t.Error(
			"homepage must not load the discipline stylesheet",
		)
	}

	if strings.Contains(
		body,
		`class="discipline-page"`,
	) {
		t.Error(
			"homepage must not render the discipline page structure",
		)
	}

	if count := strings.Count(
		body,
		`href="/static/css/home.css"`,
	); count != 1 {
		t.Errorf(
			"homepage stylesheet count: got %d, want 1",
			count,
		)
	}
}

// TestDisciplineStylesheetRoute verifies the new shared asset is reachable
// through the real /static/ file-server mapping with a CSS response type.
func TestDisciplineStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/discipline.css",
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
	if !strings.Contains(contentType, "text/css") {
		t.Errorf(
			"content type: got %q, want text/css",
			contentType,
		)
	}

	// A stable root selector confirms the response is the intended file rather
	// than another successful static asset.
	if !strings.Contains(
		recorder.Body.String(),
		".discipline-page",
	) {
		t.Error(
			"stylesheet response does not contain discipline-page rules",
		)
	}
}
