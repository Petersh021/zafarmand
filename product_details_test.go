package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProductDetailRoutes verifies every public temporary product slug through
// the real ServeMux.
//
// The table proves that path selection, document metadata, canonical route
// state, active parent navigation, semantic detail structure, and record fields
// all stay associated with the same temporary product.
func TestProductDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	tests := []struct {
		// path is the complete public product detail URL.
		path string
		// number is the catalogue position selected by the route slug.
		number string
		// name is both the document-title prefix and visible h1.
		name string
		// category is the broad family shown in the facts list.
		category string
		// status is the truthful temporary publication state.
		status string
	}{
		{
			path:     "/products/furniture-study-01",
			number:   "01",
			name:     "Furniture Study 01",
			category: "Furniture",
			status:   "Catalogue preview",
		},
		{
			path:     "/products/lighting-study-01",
			number:   "02",
			name:     "Lighting Study 01",
			category: "Lighting",
			status:   "Catalogue preview",
		},
		{
			path:     "/products/object-study-01",
			number:   "03",
			name:     "Object Study 01",
			category: "Objects",
			status:   "Catalogue preview",
		},
		{
			path:     "/products/material-study-01",
			number:   "04",
			name:     "Material Study 01",
			category: "Materials",
			status:   "Catalogue preview",
		},
	}

	for _, test := range tests {
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

			// CurrentPath remains the actual nested URL even though
			// NavigationPath keeps Products active in both shared nav regions.
			expectedCurrentPath := `data-current-path="` +
				test.path +
				`"`
			if !strings.Contains(body, expectedCurrentPath) {
				t.Errorf(
					"response does not contain %q",
					expectedCurrentPath,
				)
			}

			expectedTitle := "<title>" +
				test.name +
				" | Zafarmand</title>"
			if !strings.Contains(body, expectedTitle) {
				t.Errorf(
					"response does not contain %q",
					expectedTitle,
				)
			}

			// Inspect the Products anchor inside each responsive navigation region.
			// This proves the parent-location state cannot accidentally migrate to
			// either of the other discipline links while still passing a global
			// attribute count.
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
				productsAnchor := extractElementByMarker(
					t,
					navigation,
					`href="/products"`,
					"a",
				)
				if !strings.Contains(
					extractOpeningTag(t, productsAnchor),
					`aria-current="location"`,
				) {
					t.Errorf(
						"Products link in %s lacks parent-location state",
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

			if strings.Contains(
				body,
				`aria-current="page"`,
			) {
				t.Error(
					"nested detail parent must not claim to be the exact page",
				)
			}

			// The detail stylesheet belongs exactly once on this page, while
			// landing-page styles remain isolated from the new composition.
			if count := strings.Count(
				body,
				`href="/static/css/product-detail.css"`,
			); count != 1 {
				t.Errorf(
					"product detail stylesheet count: got %d, want 1",
					count,
				)
			}
			for _, unrelatedStyle := range []string{
				`href="/static/css/products.css"`,
				`href="/static/css/discipline.css"`,
				`href="/static/css/home.css"`,
			} {
				if strings.Contains(body, unrelatedStyle) {
					t.Errorf(
						"detail page unexpectedly contains %q",
						unrelatedStyle,
					)
				}
			}

			// A document must retain exactly one main landmark and skip target.
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
				`class="product-detail__article"`,
				"article",
			)
			articleOpening := extractOpeningTag(t, article)
			if !strings.Contains(
				articleOpening,
				`aria-labelledby="product-detail-title"`,
			) {
				t.Error(
					"product detail article does not reference its h1",
				)
			}

			heading := extractElementByMarker(
				t,
				article,
				`id="product-detail-title"`,
				"h1",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(heading),
				test.name,
			) {
				t.Errorf(
					"product detail h1 does not contain %q",
					test.name,
				)
			}
			if count := strings.Count(mainElement, "<h1"); count != 1 {
				t.Errorf(
					"h1 count: got %d, want 1",
					count,
				)
			}

			facts := extractElementByMarker(
				t,
				article,
				`class="product-detail__facts"`,
				"dl",
			)
			normalizedFacts := normalizeHTMLWhitespace(facts)
			expectedFacts := []string{
				"<dt>Catalogue number</dt> <dd>" +
					test.number +
					"</dd>",
				"<dt>Category</dt> <dd>" +
					test.category +
					"</dd>",
				"<dt>Publication status</dt> <dd>" +
					test.status +
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

			// The one product navigation region contains a real fragment link
			// back to the catalogue rather than a JavaScript history command.
			productNavigation := extractElementByMarker(
				t,
				article,
				`class="product-detail__navigation"`,
				"nav",
			)
			backLink := extractElementByMarker(
				t,
				productNavigation,
				`href="/products#selected-work"`,
				"a",
			)
			if !strings.Contains(
				normalizeHTMLWhitespace(backLink),
				"Back to products",
			) {
				t.Error(
					"product navigation link does not name its destination",
				)
			}

			if strings.Contains(mainElement, `href="#"`) {
				t.Error(
					"product detail main contains a placeholder href",
				)
			}
			if strings.Contains(mainElement, "docs/reference") {
				t.Error(
					"product detail page renders a reference composite",
				)
			}
		})
	}
}

// TestProductDetailHandlerUsesSlugPathValue proves that productDetailHandler
// reads the wildcard value supplied by ServeMux rather than manually splitting
// request.URL.Path.
//
// The URL and explicit PathValue deliberately select different products. A
// Lighting response demonstrates that PathValue is the authoritative input.
func TestProductDetailHandlerUsesSlugPathValue(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/products/furniture-study-01",
		nil,
	)
	request.SetPathValue(
		"slug",
		"lighting-study-01",
	)

	app.productDetailHandler(recorder, request)

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
	heading := extractElementByMarker(
		t,
		mainElement,
		`id="product-detail-title"`,
		"h1",
	)
	normalizedHeading := normalizeHTMLWhitespace(heading)
	if !strings.Contains(
		normalizedHeading,
		"Lighting Study 01",
	) {
		t.Error(
			"handler did not select the product named by PathValue",
		)
	}
	if strings.Contains(
		normalizedHeading,
		"Furniture Study 01",
	) {
		t.Error(
			"handler parsed the URL path instead of using PathValue",
		)
	}
}

// TestUnknownProductDetailRoutes verifies both handler-level unknown slugs and
// nested URLs that do not match the one-segment ServeMux wildcard.
//
// The test asserts only status and absence of detail presentation, leaving a
// future custom 404 design free to change its response text.
func TestUnknownProductDetailRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/products/not-published",
		"/products/Furniture-study-01",
		"/products/furniture-study-01/extra",
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
			if strings.Contains(
				recorder.Body.String(),
				`class="product-detail"`,
			) {
				t.Error(
					"unknown product route renders detail presentation",
				)
			}
		})
	}
}

// TestProductDetailRoutesAcceptHead verifies the Go ServeMux rule that a GET
// pattern also accepts HEAD for the same resource.
//
// Status and content type are the stable contract; body suppression belongs to
// the HTTP server layer and is not coupled to the template handler here.
func TestProductDetailRoutesAcceptHead(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodHead,
		"/products/furniture-study-01",
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

// TestProductDetailTemplateUsesDataAndEscapesHTML renders the detail template
// with sentinel values that contain unsafe markup.
//
// Scoping assertions to the one article proves every visible field reads the
// supplied productDetailData record, while html/template converts markup into
// inert text instead of executable elements.
func TestProductDetailTemplateUsesDataAndEscapesHTML(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	productDetail := &productDetailData{
		Number:   "<em>S-07</em>",
		Name:     "<script>Unsafe product</script>",
		Category: "<b>Unsafe category</b>",
		Status:   "<i>Unsafe status</i>",
	}

	app.render(
		recorder,
		http.StatusOK,
		"product-detail.html",
		pageData{
			Title:          "Sentinel product",
			CurrentPath:    "/products/sentinel-product",
			NavigationPath: "/products",
			ProductDetail:  productDetail,
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
	article := extractElementByMarker(
		t,
		mainElement,
		`class="product-detail__article"`,
		"article",
	)
	normalizedArticle := normalizeHTMLWhitespace(article)

	expectedEscapedValues := []string{
		"&lt;em&gt;S-07&lt;/em&gt;",
		"&lt;script&gt;Unsafe product&lt;/script&gt;",
		"&lt;b&gt;Unsafe category&lt;/b&gt;",
		"&lt;i&gt;Unsafe status&lt;/i&gt;",
	}
	for _, expected := range expectedEscapedValues {
		if !strings.Contains(normalizedArticle, expected) {
			t.Errorf(
				"sentinel detail article does not contain %q",
				expected,
			)
		}
	}

	unsafeValues := []string{
		"<em>S-07</em>",
		"<script>Unsafe product</script>",
		"<b>Unsafe category</b>",
		"<i>Unsafe status</i>",
	}
	for _, unsafeValue := range unsafeValues {
		if strings.Contains(article, unsafeValue) {
			t.Errorf(
				"sentinel detail article contains raw markup %q",
				unsafeValue,
			)
		}
	}

	heading := extractElementByMarker(
		t,
		article,
		`id="product-detail-title"`,
		"h1",
	)
	if !strings.Contains(
		normalizeHTMLWhitespace(heading),
		"&lt;script&gt;Unsafe product&lt;/script&gt;",
	) {
		t.Error(
			"sentinel product name is not associated with the h1",
		)
	}
}

// TestProductDetailTemplatePreservesMainWithoutData protects the shared
// skip-link target if future handler code omits ProductDetail.
//
// Dynamic article fields disappear with the nil pointer, but the page retains
// one empty main landmark instead of emitting an empty h1 or malformed article.
func TestProductDetailTemplatePreservesMainWithoutData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	app.render(
		recorder,
		http.StatusOK,
		"product-detail.html",
		pageData{
			Title:          "Empty product detail",
			CurrentPath:    "/products/empty",
			NavigationPath: "/products",
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
		`class="product-detail__article"`,
	) {
		t.Error(
			"nil ProductDetail must not render an empty article",
		)
	}
	if strings.Contains(mainElement, "<h1") {
		t.Error(
			"nil ProductDetail must not render an empty h1",
		)
	}
}

// TestProductDetailPresentationDoesNotLeak verifies that the detail page's
// isolated template set and stylesheet are absent from every top-level route.
func TestProductDetailPresentationDoesNotLeak(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	paths := []string{
		"/",
		"/products",
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
				`href="/static/css/product-detail.css"`,
			) {
				t.Error(
					"top-level route loads product detail stylesheet",
				)
			}
			if strings.Contains(
				body,
				`class="product-detail"`,
			) {
				t.Error(
					"top-level route renders product detail presentation",
				)
			}
		})
	}
}

// TestProductDetailStylesheetRoute verifies that the shared static file server
// exposes product-detail.css with the correct media type and a stable selector.
func TestProductDetailStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/product-detail.css",
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
		".product-detail",
	) {
		t.Error(
			"product detail stylesheet does not contain its root selector",
		)
	}
}
