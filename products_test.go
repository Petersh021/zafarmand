package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var productsNonEmptyAltPattern = regexp.MustCompile(`alt="[^"]+"`)

// extractArticlesByClassPrefix returns complete, non-nested articles whose
// class attribute starts with the stable component class.
func extractArticlesByClassPrefix(
	t *testing.T,
	source string,
	classPrefix string,
) []string {
	t.Helper()

	openingMarker := `<article class="` + classPrefix
	const closingMarker = "</article>"
	var articles []string
	remaining := source
	for {
		articleStart := strings.Index(remaining, openingMarker)
		if articleStart == -1 {
			break
		}
		openingEnd := strings.Index(remaining[articleStart:], ">")
		if openingEnd == -1 {
			t.Fatalf("%s article has no complete opening tag", classPrefix)
		}
		articleEnd := strings.Index(
			remaining[articleStart+openingEnd:],
			closingMarker,
		)
		if articleEnd == -1 {
			t.Fatalf("%s article has no closing tag", classPrefix)
		}
		articleEnd += articleStart + openingEnd + len(closingMarker)
		articles = append(articles, remaining[articleStart:articleEnd])
		remaining = remaining[articleEnd:]
	}

	return articles
}

func extractProductPreviewArticles(t *testing.T, source string) []string {
	t.Helper()
	return extractArticlesByClassPrefix(t, source, "product-preview")
}

func extractProductCollectionArticles(t *testing.T, source string) []string {
	t.Helper()
	return extractArticlesByClassPrefix(t, source, "products-collection-card")
}

// TestProductsRouteRendersReferenceHero verifies Products owns the supplied
// photographic composition while the unchanged shared menu stays outside main.
func TestProductsRouteRendersReferenceHero(t *testing.T) {
	reader := newRecordingProductCatalogueReader()
	reader.setProducts(nil)
	app := newTestApplicationWithProductCatalogueReader(t, reader)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/products", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()
	mainElement := extractMainElement(t, body)
	if !strings.Contains(extractOpeningTag(t, mainElement), `class="products-page"`) {
		t.Error("Products main lacks its route-specific page class")
	}
	hero := extractElementByMarker(t, mainElement, `data-products-hero`, "section")
	if !strings.Contains(
		extractOpeningTag(t, hero),
		`aria-labelledby="products-title"`,
	) {
		t.Error("Products hero is not labelled by its h1")
	}
	if count := strings.Count(hero, "<h1"); count != 1 {
		t.Errorf("Products hero h1 count: got %d, want 1", count)
	}
	heading := extractElementByMarker(t, hero, `id="products-title"`, "h1")
	if !strings.Contains(normalizeHTMLWhitespace(heading), "Products") {
		t.Error("Products hero h1 does not contain the route title")
	}
	for _, expected := range []string{
		"Curated. Timeless. Essential.",
		`src="/static/images/products/products-hero.jpg"`,
		`width="1672"`,
		`height="941"`,
		`fetchpriority="high"`,
		`decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(hero), expected) {
			t.Errorf("Products hero does not contain %q", expected)
		}
	}
	if strings.Contains(hero, `loading="lazy"`) {
		t.Error("above-fold Products hero is lazy-loaded")
	}
	if !productsNonEmptyAltPattern.MatchString(hero) {
		t.Error("Products hero image lacks nonempty alternative text")
	}

	scrollLink := extractElementByMarker(t, hero, `data-products-scroll`, "a")
	if !strings.Contains(extractOpeningTag(t, scrollLink), `href="#selected-work"`) ||
		!strings.Contains(normalizeHTMLWhitespace(scrollLink), "Scroll to explore") {
		t.Error("Products scroll cue does not name its real fragment destination")
	}
	work := extractElementByMarker(t, mainElement, `id="selected-work"`, "section")
	if !strings.Contains(extractOpeningTag(t, work), `tabindex="-1"`) {
		t.Error("Products selected-work target is not focusable")
	}
	if strings.Contains(mainElement, `class="discipline-hero"`) ||
		strings.Contains(body, `href="/static/css/discipline.css"`) {
		t.Error("Products retained the removed shared discipline presentation")
	}
	if count := strings.Count(body, `href="/static/css/products.css"`); count != 1 {
		t.Errorf("Products stylesheet count: got %d, want 1", count)
	}
	if count := strings.Count(body, `/static/images/products/products-hero.jpg`); count != 2 {
		t.Errorf("Products hero URL count: got %d, want preload plus image", count)
	}
	assertProductReadDeadline(t, reader.listCallSnapshot())
}

// TestProductsRouteRendersReferenceShowcase verifies an empty published
// catalogue receives four collections and five non-interactive Product cards.
func TestProductsRouteRendersReferenceShowcase(t *testing.T) {
	reader := newRecordingProductCatalogueReader()
	reader.setProducts(nil)
	app := newTestApplicationWithProductCatalogueReader(t, reader)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/products", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	extractElementByMarker(t, mainElement, `id="selected-work"`, "section")

	collections := extractElementByMarker(
		t,
		mainElement,
		`aria-label="Product collection concepts"`,
		"ul",
	)
	if !strings.Contains(extractOpeningTag(t, collections), `role="list"`) {
		t.Error("Product collections lack explicit list semantics")
	}
	collectionItems := []struct {
		name      string
		imagePath string
	}{
		{"Furniture", "/static/images/products/collection-furniture.jpg"},
		{"Lighting", "/static/images/products/collection-lighting.jpg"},
		{"Accessories", "/static/images/products/collection-accessories.jpg"},
		{"Materials", "/static/images/products/collection-materials.jpg"},
	}
	collectionArticles := extractProductCollectionArticles(t, collections)
	if len(collectionArticles) != len(collectionItems) {
		t.Fatalf(
			"collection article count: got %d, want %d",
			len(collectionArticles),
			len(collectionItems),
		)
	}
	for index, item := range collectionItems {
		article := collectionArticles[index]
		assertProductReferenceImage(
			t,
			article,
			index+1,
			item.name,
			item.imagePath,
		)
		if !strings.Contains(normalizeHTMLWhitespace(article), "View collection") {
			t.Errorf("collection article %d lacks its honest callout", index+1)
		}
		assertNonInteractiveProductConcept(t, article, "collection", index+1)
		if index > 0 && strings.Index(collections, collectionItems[index-1].name) >=
			strings.Index(collections, item.name) {
			t.Errorf("collection %q is out of order", item.name)
		}
	}

	referenceProducts := extractElementByMarker(
		t,
		mainElement,
		`aria-label="Product concept previews"`,
		"ul",
	)
	referenceOpening := extractOpeningTag(t, referenceProducts)
	if !strings.Contains(referenceOpening, "products-grid--reference") ||
		!strings.Contains(referenceOpening, `role="list"`) {
		t.Errorf("reference Product list semantics are incomplete: %s", referenceOpening)
	}
	productItems := []struct {
		name      string
		imagePath string
	}{
		{"Pivot Lounge Chair", "/static/images/products/pivot-lounge-chair.jpg"},
		{"Noir Pendant Lamp", "/static/images/products/noir-pendant-lamp.jpg"},
		{"Travertine Coffee Table", "/static/images/products/travertine-coffee-table.jpg"},
		{"Bronze Bowl", "/static/images/products/bronze-bowl.jpg"},
		{"Terra Vase", "/static/images/products/terra-vase.jpg"},
	}
	productArticles := extractProductPreviewArticles(t, referenceProducts)
	if len(productArticles) != len(productItems) {
		t.Fatalf(
			"reference Product article count: got %d, want %d",
			len(productArticles),
			len(productItems),
		)
	}
	for index, item := range productItems {
		article := productArticles[index]
		assertProductReferenceImage(
			t,
			article,
			index+1,
			item.name,
			item.imagePath,
		)
		assertNonInteractiveProductConcept(t, article, "reference Product", index+1)
		if index > 0 && strings.Index(referenceProducts, productItems[index-1].name) >=
			strings.Index(referenceProducts, item.name) {
			t.Errorf("reference Product %q is out of order", item.name)
		}
	}

	allLabel := extractElementByMarker(t, mainElement, "View all products", "p")
	if strings.Contains(allLabel, "<a") || strings.Contains(allLabel, "href=") {
		t.Error("View all products label promises a nonexistent destination")
	}
	if strings.Contains(mainElement, "Product entries are being prepared for publication.") {
		t.Error("reference composition regressed to the old text-only empty state")
	}
	assertProductReadDeadline(t, reader.listCallSnapshot())
}

func assertProductReferenceImage(
	t *testing.T,
	article string,
	position int,
	name string,
	imagePath string,
) {
	t.Helper()
	for _, expected := range []string{
		name,
		`src="` + imagePath + `"`,
		`width="1448"`,
		`height="1086"`,
		`loading="lazy"`,
		`decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(article), expected) {
			t.Errorf("reference article %d does not contain %q", position, expected)
		}
	}
	if !productsNonEmptyAltPattern.MatchString(article) {
		t.Errorf("reference article %d image lacks nonempty alternative text", position)
	}
}

func assertNonInteractiveProductConcept(
	t *testing.T,
	article string,
	kind string,
	position int,
) {
	t.Helper()
	for _, falseInteraction := range []string{
		"<a ",
		"<a\n",
		"<a>",
		"<button",
		"href=",
		`aria-disabled="true"`,
		`role="link"`,
	} {
		if strings.Contains(article, falseInteraction) {
			t.Errorf(
				"%s article %d contains false interaction %q",
				kind,
				position,
				falseInteraction,
			)
		}
	}
}

// TestProductsRoutePrioritizesPublishedCatalogue proves any published result
// replaces all five reference Products without hiding the four collections.
func TestProductsRoutePrioritizesPublishedCatalogue(t *testing.T) {
	reader := newRecordingProductCatalogueReader()
	products := []catalogueProduct{
		{
			ID: 31, CatalogueNumber: 1, Slug: "covered-chair",
			Name: "Covered Chair", Category: "Furniture",
			Cover: &productCoverMetadata{
				Version: 7, Width: 1800, Height: 1200,
				AltText: "A fictional chair on a stone plinth",
				Caption: "Not rendered in the preview.",
			},
		},
		{
			ID: 32, CatalogueNumber: 2, Slug: "uncovered-lamp",
			Name: "Uncovered Lamp", Category: "Lighting",
		},
	}
	reader.setProducts(products)
	app := newTestApplicationWithProductCatalogueReader(t, reader)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/products", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	mainElement := extractMainElement(t, recorder.Body.String())
	extractElementByMarker(t, mainElement, `id="selected-work"`, "section")
	collections := extractElementByMarker(
		t,
		mainElement,
		`aria-label="Product collection concepts"`,
		"ul",
	)
	if count := len(extractProductCollectionArticles(t, collections)); count != 4 {
		t.Errorf("published route collection count: got %d, want 4", count)
	}
	catalogue := extractElementByMarker(
		t,
		mainElement,
		`aria-label="Published Product previews"`,
		"ol",
	)
	opening := extractOpeningTag(t, catalogue)
	if !strings.Contains(opening, `role="list"`) ||
		strings.Contains(opening, "products-grid--reference") {
		t.Errorf("published Product list semantics are incorrect: %s", opening)
	}
	articles := extractProductPreviewArticles(t, catalogue)
	if len(articles) != len(products) {
		t.Fatalf("published article count: got %d, want %d", len(articles), len(products))
	}
	for _, expected := range []string{
		`href="/products/covered-chair"`, "Covered Chair", "Furniture",
		`src="/products/covered-chair/cover/7"`, `width="1800"`,
		`height="1200"`, `alt="A fictional chair on a stone plinth"`,
		`loading="lazy"`, `decoding="async"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(articles[0]), expected) {
			t.Errorf("covered Product article does not contain %q", expected)
		}
	}
	if strings.Contains(articles[0], "Not rendered in the preview") {
		t.Error("published preview renders detail-only cover caption")
	}
	for _, expected := range []string{
		`href="/products/uncovered-lamp"`, "Uncovered Lamp", "Lighting",
	} {
		if !strings.Contains(normalizeHTMLWhitespace(articles[1]), expected) {
			t.Errorf("coverless Product article does not contain %q", expected)
		}
	}
	if strings.Contains(articles[1], "<img") {
		t.Error("coverless published Product unexpectedly produced an image")
	}
	if strings.Index(catalogue, "Covered Chair") >= strings.Index(catalogue, "Uncovered Lamp") {
		t.Error("template changed repository catalogue order")
	}
	for _, referencePath := range []string{
		"pivot-lounge-chair.jpg", "noir-pendant-lamp.jpg",
		"travertine-coffee-table.jpg", "bronze-bowl.jpg", "terra-vase.jpg",
	} {
		if strings.Contains(catalogue, referencePath) {
			t.Errorf("published catalogue contains reference asset %q", referencePath)
		}
	}
	if strings.Contains(catalogue, `href="#"`) || strings.Contains(catalogue, "docs/reference") {
		t.Error("published catalogue contains placeholder navigation or a source composite")
	}
	assertProductReadDeadline(t, reader.listCallSnapshot())
}

// TestProductsRouteEscapesPublishedData proves stored plain text cannot become
// executable markup in the redesigned database-backed cards.
func TestProductsRouteEscapesPublishedData(t *testing.T) {
	reader := newRecordingProductCatalogueReader()
	reader.setProducts([]catalogueProduct{
		{
			ID: 41, CatalogueNumber: 1, Slug: "unsafe-product",
			Name:     "<b>Unsafe product</b>",
			Category: "<em>Unsafe category</em>",
		},
	})
	app := newTestApplicationWithProductCatalogueReader(t, reader)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/products", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	catalogue := extractElementByMarker(
		t,
		extractMainElement(t, recorder.Body.String()),
		`aria-label="Published Product previews"`,
		"ol",
	)
	for _, escaped := range []string{
		"&lt;b&gt;Unsafe product&lt;/b&gt;",
		"&lt;em&gt;Unsafe category&lt;/em&gt;",
		`href="/products/unsafe-product"`,
	} {
		if !strings.Contains(normalizeHTMLWhitespace(catalogue), escaped) {
			t.Errorf("published catalogue does not contain %q", escaped)
		}
	}
	for _, unsafeMarkup := range []string{
		"<b>Unsafe product</b>", "<em>Unsafe category</em>",
	} {
		if strings.Contains(catalogue, unsafeMarkup) {
			t.Errorf("published catalogue contains raw markup %q", unsafeMarkup)
		}
	}
	if strings.Contains(catalogue, "Pivot Lounge Chair") ||
		strings.Contains(catalogue, "pivot-lounge-chair.jpg") {
		t.Error("published catalogue mixed in reference Product content")
	}
}

// TestProductPresentationDoesNotLeak verifies the route-owned hero, showcase,
// and Product imagery stay isolated from unrelated public pages.
func TestProductPresentationDoesNotLeak(t *testing.T) {
	handler := newTestApplication(t).routes()
	for _, path := range []string{
		"/", "/interior-design", "/architecture-design", "/contact",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, path, nil),
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200", recorder.Code)
			}
			for _, marker := range []string{
				`href="/static/css/products.css"`, `class="products-hero`,
				`class="products-collection-grid`, `class="products-grid`,
				`/static/images/products/products-hero.jpg`,
			} {
				if strings.Contains(recorder.Body.String(), marker) {
					t.Errorf("non-Products route contains Products presentation %q", marker)
				}
			}
		})
	}
}

// TestProductsStylesheetRoute verifies Products CSS owns no shared menu rules.
func TestProductsStylesheetRoute(t *testing.T) {
	handler := newTestApplication(t).routes()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/static/css/products.css", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
		t.Errorf("Content-Type: got %q, want prefix text/css", contentType)
	}
	stylesheet := recorder.Body.String()
	for _, selector := range []string{
		".products-page", ".products-hero", ".products-collection-grid",
		".products-grid", ".product-preview",
	} {
		if !strings.Contains(stylesheet, selector) {
			t.Errorf("Products stylesheet does not contain %q", selector)
		}
	}
	for _, forbidden := range []string{
		".site-reference-menu", ".home-reference-menu", ".interior-reference-menu",
	} {
		if strings.Contains(stylesheet, forbidden) {
			t.Errorf("Products stylesheet changes shared menu selector %q", forbidden)
		}
	}
	heroRule := stage26CSSRule(t, stylesheet, ".products-hero")
	for _, required := range []string{
		"min-height: 100vh;", "min-height: 100svh;", "overflow: hidden;",
	} {
		if !strings.Contains(heroRule, required) {
			t.Errorf("Products hero rule does not contain %q", required)
		}
	}
	imageRule := stage26CSSRule(t, stylesheet, ".products-hero__image")
	for _, required := range []string{
		"width: 100%;", "height: 100%;", "object-fit: cover;",
	} {
		if !strings.Contains(imageRule, required) {
			t.Errorf("Products hero image rule does not contain %q", required)
		}
	}
}
