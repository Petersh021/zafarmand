package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestProductViewModelsMapReviewedContentAndCover verifies revisioned paths and
// ensures repository routing/internal values do not need template logic.
func TestProductViewModelsMapReviewedContentAndCover(t *testing.T) {
	product := validCatalogueProduct(8, 2, "stage-21-object")
	product.Description = "A restrained object study."
	product.Material = "Stone and brushed brass"
	product.Dimensions = "240 × 180 × 90 mm"
	product.Cover = &productCoverMetadata{
		Version: 4,
		Width:   1600,
		Height:  1200,
		AltText: "A stone object with a brass edge on a dark plinth",
		Caption: "Object Study, reviewed cover.",
	}

	previews := productPreviews([]catalogueProduct{product})
	if len(previews) != 1 || previews[0].Cover == nil ||
		previews[0].Cover.Path != "/products/stage-21-object/cover/4" ||
		previews[0].Cover.Width != "1600" ||
		previews[0].Cover.AltText != product.Cover.AltText {
		t.Fatalf("preview cover mapping: %#v", previews)
	}
	detail := newProductDetailData(product)
	if detail.Description != product.Description ||
		detail.Material != product.Material ||
		detail.Dimensions != product.Dimensions ||
		detail.Cover == nil || detail.Cover.Caption != product.Cover.Caption {
		t.Errorf("detail mapping: %#v", detail)
	}

	withoutCover := product
	withoutCover.Cover = nil
	if preview := productPreviews([]catalogueProduct{withoutCover})[0]; preview.Cover != nil {
		t.Error("missing repository cover produced an image view model")
	}
}

// TestPublishedProductTemplatesRenderEscapedReviewedContent verifies list and
// detail pages emit semantic images and escaped plain text only after the
// published reader returns the record.
func TestPublishedProductTemplatesRenderEscapedReviewedContent(t *testing.T) {
	app := newTestApplication(t)
	reader := newRecordingProductCatalogueReader()
	product := validCatalogueProduct(1, 1, "stage-21-published-chair")
	product.Name = `Stage 21 <Chair>`
	product.Description = "First reviewed line.\nSecond <reviewed> line."
	product.Material = `Oak & linen`
	product.Dimensions = `800 × 520 × 600 mm`
	product.Cover = &productCoverMetadata{
		Version: 2,
		Width:   1800,
		Height:  2400,
		AltText: `An oak chair beside a "linen" wall`,
		Caption: `Study <one> & detail`,
	}
	reader.setProducts([]catalogueProduct{product})
	app.products = reader

	list := stage16ServeAdminRequest(
		t,
		app,
		adminHTTPNewRequest(http.MethodGet, "/products", nil, false),
	)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list status: got %d body=%q", list.StatusCode, list.Body)
	}
	for _, expected := range []string{
		`src="/products/stage-21-published-chair/cover/2"`,
		`width="1800"`,
		`height="2400"`,
		`alt="An oak chair beside a &#34;linen&#34; wall"`,
		`Stage 21 &lt;Chair&gt;`,
	} {
		if !strings.Contains(list.Body, expected) {
			t.Errorf("list body does not contain %q", expected)
		}
	}

	detail := stage16ServeAdminRequest(
		t,
		app,
		adminHTTPNewRequest(
			http.MethodGet,
			"/products/stage-21-published-chair",
			nil,
			false,
		),
	)
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("detail status: got %d body=%q", detail.StatusCode, detail.Body)
	}
	for _, expected := range []string{
		"About this Product",
		"First reviewed line.\nSecond &lt;reviewed&gt; line.",
		"Oak &amp; linen",
		"800 × 520 × 600 mm",
		`src="/products/stage-21-published-chair/cover/2"`,
		"Study &lt;one&gt; &amp; detail",
	} {
		if !strings.Contains(detail.Body, expected) {
			t.Errorf("detail body does not contain %q", expected)
		}
	}
	for _, forbidden := range []string{
		"<Chair>",
		"<reviewed>",
		"<one>",
	} {
		if strings.Contains(detail.Body, forbidden) {
			t.Errorf("detail body contains unescaped %q", forbidden)
		}
	}
}
