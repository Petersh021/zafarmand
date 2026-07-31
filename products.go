package main

// product is the temporary application-level source record used by the
// Products catalogue and detail routes.
//
// This is deliberately smaller than the future database model described in the
// project roadmap. It contains only truthful structural fields needed during
// Stage 7 and omits prices, specifications, media, publication controls, and
// administrative state. application.products owns the ordered slice so both
// listing and detail handlers read the same source without mutable globals.
type product struct {
	// Number is the zero-padded editorial position shared by list and detail views.
	Number string
	// Slug is the exact case-sensitive path value accepted after /products/.
	Slug string
	// Name is a neutral temporary catalogue name, not final published copy.
	Name string
	// Category identifies the broad product family represented by the record.
	Category string
	// Status truthfully describes the record as an in-progress catalogue preview.
	Status string
}

// temporaryProducts returns the ordered source records used until PostgreSQL
// and the admin publishing workflow are introduced.
//
// Returning a fresh slice during application construction keeps seed creation
// explicit and prevents package-level mutable state. The neutral "Study" names
// and preview status distinguish these records from approved final products.
func temporaryProducts() []product {
	return []product{
		{
			Number:   "01",
			Slug:     "furniture-study-01",
			Name:     "Furniture Study 01",
			Category: "Furniture",
			Status:   "Catalogue preview",
		},
		{
			Number:   "02",
			Slug:     "lighting-study-01",
			Name:     "Lighting Study 01",
			Category: "Lighting",
			Status:   "Catalogue preview",
		},
		{
			Number:   "03",
			Slug:     "object-study-01",
			Name:     "Object Study 01",
			Category: "Objects",
			Status:   "Catalogue preview",
		},
		{
			Number:   "04",
			Slug:     "material-study-01",
			Name:     "Material Study 01",
			Category: "Materials",
			Status:   "Catalogue preview",
		},
	}
}

// productDetailPath builds the canonical public URL for one trusted temporary
// product slug.
//
// Slugs originate from temporaryProducts rather than visitor input. Keeping
// path construction in one helper prevents listing links and detail handlers
// from drifting to different URL formats.
func productDetailPath(slug string) string {
	return "/products/" + slug
}

// productPreviews maps ordered source records into the smaller view model
// required by products.html.
//
// Allocating the result at the exact source length preserves order and avoids
// exposing fields that the catalogue does not need. A nil or empty source
// naturally returns an empty slice, which activates the template's tested empty
// state.
func productPreviews(products []product) []productPreviewData {
	previews := make(
		[]productPreviewData,
		len(products),
	)

	for index, product := range products {
		previews[index] = productPreviewData{
			Number:   product.Number,
			Name:     product.Name,
			Category: product.Category,
			Status:   product.Status,
			Path:     productDetailPath(product.Slug),
		}
	}

	return previews
}

// findProductBySlug performs an exact, case-sensitive lookup in the ordered
// temporary source.
//
// A linear search is intentionally sufficient for four in-memory records and
// keeps ordering and lookup backed by one slice. The boolean distinguishes a
// real zero-value record from an unknown slug so the handler can return 404
// before attempting template rendering.
func findProductBySlug(
	products []product,
	slug string,
) (product, bool) {
	for _, product := range products {
		if product.Slug == slug {
			return product, true
		}
	}

	return product{}, false
}

// newProductDetailData maps an application source record to the intentionally
// narrow detail-template view model.
//
// Keeping this conversion explicit prevents the template from depending on
// routing fields such as Slug and leaves a clear seam for a future database or
// repository record to produce the same stable presentation shape.
func newProductDetailData(
	product product,
) productDetailData {
	return productDetailData{
		Number:   product.Number,
		Name:     product.Name,
		Category: product.Category,
		Status:   product.Status,
	}
}
