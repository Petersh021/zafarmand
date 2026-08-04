package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestArchitectureProjectDetailHelpers verifies the small data and routing helpers
// independently from HTTP and template behavior.
//
// Sentinel values make field association visible, while the exact-case lookup
// cases document that only trusted slugs from the application source can select
// a public record.
func TestArchitectureProjectDetailHelpers(t *testing.T) {
	projects := []architectureProject{
		{
			Number:   "S-17",
			Slug:     "first-sentinel",
			Title:    "First sentinel architecture",
			Typology: "First sentinel typology",
			Status:   "First sentinel status",
		},
		{
			Number:   "S-29",
			Slug:     "second-sentinel",
			Title:    "Second sentinel architecture",
			Typology: "Second sentinel typology",
			Status:   "Second sentinel status",
		},
	}

	// The canonical path helper is shared by listing previews and the detail
	// handler. Testing the complete URL protects both callers from silently
	// adopting different route formats.
	expectedPath := "/architecture-design/second-sentinel"
	if path := architectureProjectDetailPath(
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
	selected, exists := findArchitectureProjectBySlug(
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
	detail := newArchitectureProjectDetailData(selected)
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
		projects []architectureProject
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
			project, found := findArchitectureProjectBySlug(
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

// TestArchitectureProjectDetailRoutes verifies every temporary project detail
// through the real ServeMux.
//
// Deriving cases from app.architectureProjects proves that source records, listing
// paths, handler lookup, document metadata, active parent navigation, semantic
// detail markup, and visible facts remain associated as one vertical slice.
func TestArchitectureProjectDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	for _, project := range app.architectureProjects {
		project := project
		t.Run(project.Title, func(t *testing.T) {
			path := architectureProjectDetailPath(project.Slug)
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
			// marks Architecture Design as the containing section.
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

			// Both responsive navigation regions must identify Architecture Design as
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
				architectureAnchor := extractElementByMarker(
					t,
					navigation,
					`href="/architecture-design"`,
					"a",
				)
				if !strings.Contains(
					extractOpeningTag(t, architectureAnchor),
					`aria-current="location"`,
				) {
					t.Errorf(
						"Architecture link in %s lacks parent-location state",
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
					"nested Architecture detail parent must not claim exact-page state",
				)
			}

			// Only the route-specific detail stylesheet belongs to this
			// composition. Landing, Product, and Interior assets stay isolated.
			if count := strings.Count(
				body,
				`href="/static/css/architecture-project-detail.css"`,
			); count != 1 {
				t.Errorf(
					"Architecture detail stylesheet count: got %d, want 1",
					count,
				)
			}
			for _, unrelatedStyle := range []string{
				`href="/static/css/architecture-design.css"`,
				`href="/static/css/discipline.css"`,
				`href="/static/css/products.css"`,
				`href="/static/css/product-detail.css"`,
				`href="/static/css/interior-design.css"`,
				`href="/static/css/interior-project-detail.css"`,
				`href="/static/css/home.css"`,
			} {
				if strings.Contains(body, unrelatedStyle) {
					t.Errorf(
						"Architecture detail unexpectedly contains %q",
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
				`class="architecture-project-detail__article"`,
				"article",
			)
			if !strings.Contains(
				extractOpeningTag(t, article),
				`aria-labelledby="architecture-project-title"`,
			) {
				t.Error(
					"Architecture detail article does not reference its h1",
				)
			}

			heading := extractElementByMarker(
				t,
				article,
				`id="architecture-project-title"`,
				"h1",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(heading),
				project.Title,
			) {
				t.Errorf(
					"Architecture detail h1 does not contain %q",
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
				`class="architecture-project-detail__facts"`,
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
				`class="architecture-project-detail__navigation"`,
				"nav",
			)
			backLink := extractElementByMarker(
				t,
				projectNavigation,
				`href="/architecture-design#selected-work"`,
				"a",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(backLink),
				"Back to Architecture Design",
			) {
				t.Error(
					"Architecture detail back link does not name its destination",
				)
			}

			// The structural visual field repeats information already present in
			// text, so the complete decorative region stays hidden from assistive
			// technology.
			media := extractElementByMarker(
				t,
				article,
				`class="architecture-project-detail__media"`,
				"div",
			)
			if !strings.Contains(
				extractOpeningTag(t, media),
				`aria-hidden="true"`,
			) {
				t.Error(
					"Architecture detail media is not hidden from assistive technology",
				)
			}

			// Placeholder URLs, scripting schemes, history-driven navigation,
			// and design composites are outside this server-rendered stage.
			for _, forbidden := range []string{
				`href="#"`,
				`javascript:`,
				`history.back`,
				"docs/reference",
				"zafarmand-architecture-design.jpg",
			} {
				if strings.Contains(mainElement, forbidden) {
					t.Errorf(
						"Architecture detail main contains forbidden value %q",
						forbidden,
					)
				}
			}
		})
	}
}

// TestArchitectureProjectDetailHandlerUsesSlugPathValue proves that the handler
// reads the wildcard value supplied by ServeMux rather than manually splitting
// request.URL.Path.
//
// The request URL and explicit PathValue intentionally identify different
// records. Rendering the second title makes PathValue's authority observable.
func TestArchitectureProjectDetailHandlerUsesSlugPathValue(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/architecture-design/architecture-study-01",
		nil,
	)
	request.SetPathValue(
		"slug",
		"architecture-study-02",
	)

	app.architectureProjectDetailHandler(recorder, request)

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
		`id="architecture-project-title"`,
		"h1",
	)
	normalizedHeading := normalizeHTMLWhitespace(heading)
	if !strings.Contains(
		normalizedHeading,
		"Architecture Study 02",
	) {
		t.Error(
			"handler did not select the Architecture project named by PathValue",
		)
	}
	if strings.Contains(
		normalizedHeading,
		"Architecture Study 01",
	) {
		t.Error(
			"handler parsed URL.Path instead of using PathValue",
		)
	}
}

// TestUnknownArchitectureProjectDetailRoutes verifies both handler-level unknown
// slugs and nested URLs that cannot match the one-segment ServeMux wildcard.
//
// The assertions deliberately avoid response text so a later custom 404 page
// can change its presentation without weakening the routing contract.
func TestUnknownArchitectureProjectDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/architecture-design/not-published",
		"/architecture-design/Architecture-study-01",
		"/architecture-design/architecture-study-01/extra",
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
				`class="architecture-project-detail"`,
			) {
				t.Error(
					"unknown Architecture project route renders detail presentation",
				)
			}
			if strings.Contains(
				body,
				`href="/static/css/architecture-project-detail.css"`,
			) {
				t.Error(
					"unknown Architecture project route loads detail stylesheet",
				)
			}
		})
	}
}

// TestArchitectureProjectDetailRoutesAcceptHead verifies the ServeMux rule that a
// GET pattern also accepts HEAD for the same project resource.
//
// The handler-level recorder retains a rendered body, so status and content
// type are the stable contracts tested here. Body suppression belongs to the
// outer HTTP server.
func TestArchitectureProjectDetailRoutesAcceptHead(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodHead,
		"/architecture-design/architecture-study-01",
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

// TestArchitectureProjectDetailTemplateUsesDataAndEscapesHTML renders the detail
// template with sentinel fields containing unsafe markup.
//
// Keeping assertions inside the one detail article proves that every visible
// value comes from the supplied view model and that html/template converts
// markup into inert text rather than executable elements.
func TestArchitectureProjectDetailTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	projectDetail := &architectureProjectDetailData{
		Number:   "<em>S-09</em>",
		Title:    "<script>Unsafe architecture title</script>",
		Typology: "<b>Unsafe architecture typology</b>",
		Status:   "<i>Unsafe architecture status</i>",
	}

	app.render(
		recorder,
		http.StatusOK,
		"architecture-project-detail.html",
		pageData{
			Title:                     "Sentinel Architecture project",
			CurrentPath:               "/architecture-design/sentinel-project",
			NavigationPath:            "/architecture-design",
			ArchitectureProjectDetail: projectDetail,
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
		`class="architecture-project-detail__article"`,
		"article",
	)
	normalizedArticle := normalizeHTMLWhitespace(article)

	expectedEscapedValues := []string{
		"&lt;em&gt;S-09&lt;/em&gt;",
		"&lt;script&gt;Unsafe architecture title&lt;/script&gt;",
		"&lt;b&gt;Unsafe architecture typology&lt;/b&gt;",
		"&lt;i&gt;Unsafe architecture status&lt;/i&gt;",
	}
	for _, expected := range expectedEscapedValues {
		if !strings.Contains(normalizedArticle, expected) {
			t.Errorf(
				"Architecture detail article does not contain escaped value %q",
				expected,
			)
		}
	}

	rawValues := []string{
		"<em>S-09</em>",
		"<script>Unsafe architecture title</script>",
		"<b>Unsafe architecture typology</b>",
		"<i>Unsafe architecture status</i>",
	}
	for _, raw := range rawValues {
		if strings.Contains(article, raw) {
			t.Errorf(
				"Architecture detail article contains raw markup %q",
				raw,
			)
		}
	}
}

// TestArchitectureProjectDetailTemplatePreservesMainWithoutData protects the shared
// skip-link destination when a future handler accidentally omits its optional
// ArchitectureProjectDetail pointer.
//
// The defensive state keeps document structure valid without inventing an
// empty project article or heading.
func TestArchitectureProjectDetailTemplatePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"architecture-project-detail.html",
		pageData{
			Title:          "Empty Architecture project detail",
			CurrentPath:    "/architecture-design/empty",
			NavigationPath: "/architecture-design",
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
		`class="architecture-project-detail__article"`,
	) {
		t.Error(
			"nil ArchitectureProjectDetail must not render an empty article",
		)
	}
	if strings.Contains(mainElement, "<h1") {
		t.Error(
			"nil ArchitectureProjectDetail must not render an empty h1",
		)
	}
}

// TestArchitectureProjectDetailPresentationDoesNotLeak verifies that the detail
// template's isolated cache entry and stylesheet remain absent from every other
// successful public page type.
func TestArchitectureProjectDetailPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/interior-design",
		"/interior-design/interior-study-01",
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
				`href="/static/css/architecture-project-detail.css"`,
			) {
				t.Error(
					"non-detail route loads Architecture project detail stylesheet",
				)
			}
			if strings.Contains(
				body,
				`class="architecture-project-detail"`,
			) {
				t.Error(
					"non-detail route renders Architecture project detail presentation",
				)
			}
		})
	}
}

// TestArchitectureProjectDetailStylesheetRoute verifies that the shared static file
// server exposes the detail stylesheet with its correct media type.
//
// A stable root selector also catches an empty or incorrectly mapped response
// without coupling the test to every visual declaration.
func TestArchitectureProjectDetailStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/architecture-project-detail.css",
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
		".architecture-project-detail",
	) {
		t.Error(
			"Architecture project detail stylesheet lacks its root selector",
		)
	}
}
