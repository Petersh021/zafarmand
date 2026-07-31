package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInteriorProjectDetailHelpers verifies the small data and routing helpers
// independently from HTTP and template behavior.
//
// Sentinel values make field association visible, while the exact-case lookup
// cases document that only trusted slugs from the application source can select
// a public record.
func TestInteriorProjectDetailHelpers(t *testing.T) {
	projects := []interiorProject{
		{
			Number:   "S-17",
			Slug:     "first-sentinel",
			Title:    "First sentinel interior",
			Typology: "First sentinel typology",
			Status:   "First sentinel status",
		},
		{
			Number:   "S-29",
			Slug:     "second-sentinel",
			Title:    "Second sentinel interior",
			Typology: "Second sentinel typology",
			Status:   "Second sentinel status",
		},
	}

	// The canonical path helper is shared by listing previews and the detail
	// handler. Testing the complete URL protects both callers from silently
	// adopting different route formats.
	expectedPath := "/interior-design/second-sentinel"
	if path := interiorProjectDetailPath(
		projects[1].Slug,
	); path != expectedPath {
		t.Errorf(
			"detail path: got %q, want %q",
			path,
			expectedPath,
		)
	}

	// An exact successful lookup must return the complete source record rather
	// than a partially reconstructed value.
	selected, exists := findInteriorProjectBySlug(
		projects,
		"second-sentinel",
	)
	if !exists {
		t.Fatal("exact trusted slug was not found")
	}
	if selected != projects[1] {
		t.Errorf(
			"selected project: got %#v, want %#v",
			selected,
			projects[1],
		)
	}

	// The view mapper intentionally removes routing-only data such as Slug while
	// preserving every truthful field needed by the detail template.
	detail := newInteriorProjectDetailData(selected)
	if detail.Number != selected.Number {
		t.Errorf(
			"detail number: got %q, want %q",
			detail.Number,
			selected.Number,
		)
	}
	if detail.Title != selected.Title {
		t.Errorf(
			"detail title: got %q, want %q",
			detail.Title,
			selected.Title,
		)
	}
	if detail.Typology != selected.Typology {
		t.Errorf(
			"detail typology: got %q, want %q",
			detail.Typology,
			selected.Typology,
		)
	}
	if detail.Status != selected.Status {
		t.Errorf(
			"detail status: got %q, want %q",
			detail.Status,
			selected.Status,
		)
	}

	// Visitor-controlled strings are never normalized or used as template
	// names. Case changes, suffixes, empty input, and a nil source all fail the
	// same whitelist lookup.
	unknownLookups := []struct {
		// name describes the boundary represented by this input.
		name string
		// projects is the exact source searched by the helper.
		projects []interiorProject
		// slug is the untrusted path value supplied to the lookup.
		slug string
	}{
		{
			name:     "case changed",
			projects: projects,
			slug:     "Second-sentinel",
		},
		{
			name:     "suffix added",
			projects: projects,
			slug:     "second-sentinel/extra",
		},
		{
			name:     "empty slug",
			projects: projects,
			slug:     "",
		},
		{
			name:     "unknown slug",
			projects: projects,
			slug:     "not-published",
		},
		{
			name:     "nil source",
			projects: nil,
			slug:     "second-sentinel",
		},
	}

	for _, test := range unknownLookups {
		t.Run(test.name, func(t *testing.T) {
			project, found := findInteriorProjectBySlug(
				test.projects,
				test.slug,
			)
			if found {
				t.Errorf(
					"unexpected project for slug %q: %#v",
					test.slug,
					project,
				)
			}
		})
	}
}

// TestInteriorProjectDetailRoutes verifies every temporary project detail
// through the real ServeMux.
//
// Deriving cases from app.interiorProjects proves that source records, listing
// paths, handler lookup, document metadata, active parent navigation, semantic
// detail markup, and visible facts remain associated as one vertical slice.
func TestInteriorProjectDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	for _, project := range app.interiorProjects {
		project := project
		t.Run(project.Title, func(t *testing.T) {
			path := interiorProjectDetailPath(project.Slug)
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

			// CurrentPath remains the real nested URL. NavigationPath separately
			// marks Interior Design as the containing section.
			expectedCurrentPath := `data-current-path="` +
				path +
				`"`
			if !strings.Contains(body, expectedCurrentPath) {
				t.Errorf(
					"response does not contain %q",
					expectedCurrentPath,
				)
			}

			expectedTitle := "<title>" +
				project.Title +
				" | Zafarmand</title>"
			if !strings.Contains(body, expectedTitle) {
				t.Errorf(
					"response does not contain %q",
					expectedTitle,
				)
			}

			// Both responsive navigation regions must identify Interior Design as
			// the parent location, without claiming that its landing URL is the
			// exact detail page.
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
				interiorAnchor := extractElementByMarker(
					t,
					navigation,
					`href="/interior-design"`,
					"a",
				)
				if !strings.Contains(
					extractOpeningTag(t, interiorAnchor),
					`aria-current="location"`,
				) {
					t.Errorf(
						"Interior link in %s lacks parent-location state",
						navigationMarker,
					)
				}
				if count := strings.Count(
					navigation,
					`aria-current="location"`,
				); count != 1 {
					t.Errorf(
						"parent-location count in %s: got %d, want 1",
						navigationMarker,
						count,
					)
				}
			}
			if strings.Contains(body, `aria-current="page"`) {
				t.Error(
					"nested Interior detail parent must not claim exact-page state",
				)
			}

			// Only the route-specific detail stylesheet belongs to this
			// composition. Landing and Product assets stay isolated.
			if count := strings.Count(
				body,
				`href="/static/css/interior-project-detail.css"`,
			); count != 1 {
				t.Errorf(
					"Interior detail stylesheet count: got %d, want 1",
					count,
				)
			}
			for _, unrelatedStyle := range []string{
				`href="/static/css/interior-design.css"`,
				`href="/static/css/discipline.css"`,
				`href="/static/css/products.css"`,
				`href="/static/css/product-detail.css"`,
				`href="/static/css/home.css"`,
			} {
				if strings.Contains(body, unrelatedStyle) {
					t.Errorf(
						"Interior detail unexpectedly contains %q",
						unrelatedStyle,
					)
				}
			}

			// The skip link and document outline rely on one main landmark with
			// one stable destination ID.
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
				t.Error(
					"Interior detail article does not reference its h1",
				)
			}

			heading := extractElementByMarker(
				t,
				article,
				`id="interior-project-title"`,
				"h1",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(heading),
				project.Title,
			) {
				t.Errorf(
					"Interior detail h1 does not contain %q",
					project.Title,
				)
			}
			if count := strings.Count(mainElement, "<h1"); count != 1 {
				t.Errorf(
					"h1 count: got %d, want 1",
					count,
				)
			}

			// A description list owns the three stable name/value facts. It does
			// not invent locations, dates, descriptions, or approved media.
			facts := extractElementByMarker(
				t,
				article,
				`class="interior-project-detail__facts"`,
				"dl",
			)
			normalizedFacts := normalizeHTMLWhitespace(facts)
			expectedFacts := []string{
				"<dt>Project number</dt> <dd>" +
					project.Number +
					"</dd>",
				"<dt>Typology</dt> <dd>" +
					project.Typology +
					"</dd>",
				"<dt>Publication status</dt> <dd>" +
					project.Status +
					"</dd>",
			}
			for _, expectedFact := range expectedFacts {
				if !strings.Contains(
					normalizedFacts,
					expectedFact,
				) {
					t.Errorf(
						"facts list does not contain %q",
						expectedFact,
					)
				}
			}
			if count := strings.Count(facts, "<dt>"); count != 3 {
				t.Errorf(
					"fact term count: got %d, want 3",
					count,
				)
			}
			if count := strings.Count(facts, "<dd>"); count != 3 {
				t.Errorf(
					"fact description count: got %d, want 3",
					count,
				)
			}

			// A native fragment URL returns directly to the real portfolio
			// section and remains usable without JavaScript or browser history.
			projectNavigation := extractElementByMarker(
				t,
				article,
				`class="interior-project-detail__navigation"`,
				"nav",
			)
			backLink := extractElementByMarker(
				t,
				projectNavigation,
				`href="/interior-design#selected-work"`,
				"a",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(backLink),
				"Back to Interior Design",
			) {
				t.Error(
					"Interior detail back link does not name its destination",
				)
			}

			// The structural visual field repeats information already present in
			// text, so the complete decorative region stays hidden from assistive
			// technology.
			media := extractElementByMarker(
				t,
				article,
				`class="interior-project-detail__media"`,
				"div",
			)
			if !strings.Contains(
				extractOpeningTag(t, media),
				`aria-hidden="true"`,
			) {
				t.Error(
					"Interior detail media is not hidden from assistive technology",
				)
			}

			// Placeholder URLs, scripting schemes, history-driven navigation,
			// and design composites are outside this server-rendered stage.
			for _, forbidden := range []string{
				`href="#"`,
				`javascript:`,
				`history.back`,
				"docs/reference",
			} {
				if strings.Contains(mainElement, forbidden) {
					t.Errorf(
						"Interior detail main contains forbidden value %q",
						forbidden,
					)
				}
			}
		})
	}
}

// TestInteriorProjectDetailHandlerUsesSlugPathValue proves that the handler
// reads the wildcard value supplied by ServeMux rather than manually splitting
// request.URL.Path.
//
// The request URL and explicit PathValue intentionally identify different
// records. Rendering the second title makes PathValue's authority observable.
func TestInteriorProjectDetailHandlerUsesSlugPathValue(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/interior-design/interior-study-01",
		nil,
	)
	request.SetPathValue(
		"slug",
		"interior-study-02",
	)

	app.interiorProjectDetailHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	heading := extractElementByMarker(
		t,
		extractMainElement(t, recorder.Body.String()),
		`id="interior-project-title"`,
		"h1",
	)
	normalizedHeading := normalizeHTMLWhitespace(heading)
	if !strings.Contains(
		normalizedHeading,
		"Interior Study 02",
	) {
		t.Error(
			"handler did not select the Interior project named by PathValue",
		)
	}
	if strings.Contains(
		normalizedHeading,
		"Interior Study 01",
	) {
		t.Error(
			"handler parsed URL.Path instead of using PathValue",
		)
	}
}

// TestUnknownInteriorProjectDetailRoutes verifies both handler-level unknown
// slugs and nested URLs that cannot match the one-segment ServeMux wildcard.
//
// The assertions deliberately avoid response text so a later custom 404 page
// can change its presentation without weakening the routing contract.
func TestUnknownInteriorProjectDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/interior-design/not-published",
		"/interior-design/Interior-study-01",
		"/interior-design/interior-study-01/extra",
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

			if recorder.Code != http.StatusNotFound {
				t.Fatalf(
					"status code: got %d, want %d",
					recorder.Code,
					http.StatusNotFound,
				)
			}
			body := recorder.Body.String()
			if strings.Contains(
				body,
				`class="interior-project-detail"`,
			) {
				t.Error(
					"unknown Interior project route renders detail presentation",
				)
			}
			if strings.Contains(
				body,
				`href="/static/css/interior-project-detail.css"`,
			) {
				t.Error(
					"unknown Interior project route loads detail stylesheet",
				)
			}
		})
	}
}

// TestInteriorProjectDetailRoutesAcceptHead verifies the ServeMux rule that a
// GET pattern also accepts HEAD for the same project resource.
//
// The handler-level recorder retains a rendered body, so status and content
// type are the stable contracts tested here. Body suppression belongs to the
// outer HTTP server.
func TestInteriorProjectDetailRoutesAcceptHead(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodHead,
		"/interior-design/interior-study-01",
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
	if contentType := recorder.Header().Get(
		"Content-Type",
	); !strings.HasPrefix(
		contentType,
		"text/html",
	) {
		t.Errorf(
			"Content-Type: got %q, want text/html prefix",
			contentType,
		)
	}
}

// TestInteriorProjectDetailTemplateUsesDataAndEscapesHTML renders the detail
// template with sentinel fields containing unsafe markup.
//
// Keeping assertions inside the one detail article proves that every visible
// value comes from the supplied view model and that html/template converts
// markup into inert text rather than executable elements.
func TestInteriorProjectDetailTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	projectDetail := &interiorProjectDetailData{
		Number:   "<em>S-09</em>",
		Title:    "<script>Unsafe interior title</script>",
		Typology: "<b>Unsafe interior typology</b>",
		Status:   "<i>Unsafe interior status</i>",
	}

	app.render(
		recorder,
		http.StatusOK,
		"interior-project-detail.html",
		pageData{
			Title:                 "Sentinel Interior project",
			CurrentPath:           "/interior-design/sentinel-project",
			NavigationPath:        "/interior-design",
			InteriorProjectDetail: projectDetail,
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	article := extractElementByMarker(
		t,
		extractMainElement(t, recorder.Body.String()),
		`class="interior-project-detail__article"`,
		"article",
	)
	normalizedArticle := normalizeHTMLWhitespace(article)

	expectedEscapedValues := []string{
		"&lt;em&gt;S-09&lt;/em&gt;",
		"&lt;script&gt;Unsafe interior title&lt;/script&gt;",
		"&lt;b&gt;Unsafe interior typology&lt;/b&gt;",
		"&lt;i&gt;Unsafe interior status&lt;/i&gt;",
	}
	for _, expected := range expectedEscapedValues {
		if !strings.Contains(normalizedArticle, expected) {
			t.Errorf(
				"Interior detail article does not contain escaped value %q",
				expected,
			)
		}
	}

	rawValues := []string{
		"<em>S-09</em>",
		"<script>Unsafe interior title</script>",
		"<b>Unsafe interior typology</b>",
		"<i>Unsafe interior status</i>",
	}
	for _, raw := range rawValues {
		if strings.Contains(article, raw) {
			t.Errorf(
				"Interior detail article contains raw markup %q",
				raw,
			)
		}
	}
}

// TestInteriorProjectDetailTemplatePreservesMainWithoutData protects the shared
// skip-link destination when a future handler accidentally omits its optional
// InteriorProjectDetail pointer.
//
// The defensive state keeps document structure valid without inventing an
// empty project article or heading.
func TestInteriorProjectDetailTemplatePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"interior-project-detail.html",
		pageData{
			Title:          "Empty Interior project detail",
			CurrentPath:    "/interior-design/empty",
			NavigationPath: "/interior-design",
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
		`class="interior-project-detail__article"`,
	) {
		t.Error(
			"nil InteriorProjectDetail must not render an empty article",
		)
	}
	if strings.Contains(mainElement, "<h1") {
		t.Error(
			"nil InteriorProjectDetail must not render an empty h1",
		)
	}
}

// TestInteriorProjectDetailPresentationDoesNotLeak verifies that the detail
// template's isolated cache entry and stylesheet remain absent from every other
// successful public page type.
func TestInteriorProjectDetailPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
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
				`href="/static/css/interior-project-detail.css"`,
			) {
				t.Error(
					"non-detail route loads Interior project detail stylesheet",
				)
			}
			if strings.Contains(
				body,
				`class="interior-project-detail"`,
			) {
				t.Error(
					"non-detail route renders Interior project detail presentation",
				)
			}
		})
	}
}

// TestInteriorProjectDetailStylesheetRoute verifies that the shared static file
// server exposes the detail stylesheet with its correct media type.
//
// A stable root selector also catches an empty or incorrectly mapped response
// without coupling the test to every visual declaration.
func TestInteriorProjectDetailStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/interior-project-detail.css",
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
			"Content-Type: got %q, want text/css prefix",
			contentType,
		)
	}
	if !strings.Contains(
		recorder.Body.String(),
		".interior-project-detail",
	) {
		t.Error(
			"Interior project detail stylesheet lacks its root selector",
		)
	}
}
