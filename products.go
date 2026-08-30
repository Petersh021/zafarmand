package main

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	// publishedProductStatusLabel is trusted interface text, not a database
	// value. The repository exposes only published records, so templates never
	// need the internal draft/published/archived vocabulary.
	publishedProductStatusLabel = "Published"
)

// errProductCatalogueReaderRequired prevents the server from starting with
// public Product routes that have no durable read dependency.
var errProductCatalogueReaderRequired = errors.New(
	"create application: product catalogue reader is required",
)

// productDetailPath builds the canonical public URL for one validated
// repository slug.
//
// Keeping path construction in one helper prevents listing links and detail
// handlers from drifting to different URL formats.
func productDetailPath(slug string) string {
	return "/products/" + slug
}

// productCoverPath builds the revision-specific public image URL from one
// canonical slug and positive cover revision.
func productCoverPath(slug string, version int64) string {
	return productDetailPath(slug) + "/cover/" + strconv.FormatInt(version, 10)
}

// productPreviews maps ordered source records into the smaller view model
// required by products.html.
//
// Allocating the result at the exact source length preserves order and avoids
// exposing fields that the catalogue does not need. A nil or empty source
// naturally returns an empty slice, which activates the template's tested empty
// state.
func productPreviews(products []catalogueProduct) []productPreviewData {
	previews := make(
		[]productPreviewData,
		len(products),
	)

	for index, product := range products {
		preview := productPreviewData{
			Number:   formatProductCatalogueNumber(product.CatalogueNumber),
			Name:     product.Name,
			Category: product.Category,
			Status:   publishedProductStatusLabel,
			Path:     productDetailPath(product.Slug),
		}
		if product.Cover != nil {
			cover := newProductCoverPageData(product.Slug, *product.Cover)
			preview.Cover = &cover
		}
		previews[index] = preview
	}

	return previews
}

// productReferenceCollections returns fresh values for the four collection
// still lifes in the approved Products concept.
//
// These values intentionally contain no route. No collection pages exist, so
// callers cannot accidentally turn the reference labels into dead links.
func productReferenceCollections() []productReferenceCollectionData {
	return []productReferenceCollectionData{
		{
			Name:      "Furniture",
			ImagePath: "/static/images/products/collection-furniture.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Sculptural walnut and brown upholstered lounge chair " +
				"in a warm plaster studio",
		},
		{
			Name:      "Lighting",
			ImagePath: "/static/images/products/collection-lighting.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Pale sculptural pendant lamp with a softly glowing " +
				"cylindrical core",
		},
		{
			Name:      "Accessories",
			ImagePath: "/static/images/products/collection-accessories.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Two blackened sculptural bowls arranged on a pale " +
				"stone plinth",
		},
		{
			Name:      "Materials",
			ImagePath: "/static/images/products/collection-materials.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Translucent ivory linen beside rough travertine and " +
				"finely textured stone",
		},
	}
}

// productReferencePreviews returns fresh non-interactive values for the five
// concept Products shown only while the published catalogue is empty.
//
// Prices and paths are deliberately absent. Commerce and fictional Product
// detail destinations cannot leak across the PostgreSQL publication boundary.
func productReferencePreviews() []productReferencePreviewData {
	return []productReferencePreviewData{
		{
			Name:      "Pivot Lounge Chair",
			Category:  "Furniture",
			ImagePath: "/static/images/products/pivot-lounge-chair.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Low charcoal leather lounge chair with a geometric " +
				"blackened walnut frame",
		},
		{
			Name:      "Noir Pendant Lamp",
			Category:  "Lighting",
			ImagePath: "/static/images/products/noir-pendant-lamp.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Wide matte-black dome pendant lamp with a short " +
				"antique-brass neck",
		},
		{
			Name:      "Travertine Coffee Table",
			Category:  "Furniture",
			ImagePath: "/static/images/products/travertine-coffee-table.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Low round pale travertine coffee table supported by " +
				"three cylindrical legs",
		},
		{
			Name:      "Bronze Bowl",
			Category:  "Accessories",
			ImagePath: "/static/images/products/bronze-bowl.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Shallow hand-cast blackened bronze bowl with a " +
				"softly irregular rim",
		},
		{
			Name:      "Terra Vase",
			Category:  "Accessories",
			ImagePath: "/static/images/products/terra-vase.jpg",
			Width:     1448,
			Height:    1086,
			AltText: "Dark umber earthenware vase with a rounded body and " +
				"small loop handle",
		},
	}
}

// isValidPublishedProductCatalogue verifies the complete contract returned by
// any injected reader before the public list renders it. Production performs
// the same checks while scanning, but repeating this small boundary check makes
// tests and future implementations fail closed as well.
func isValidPublishedProductCatalogue(products []catalogueProduct) bool {
	seenIDs := make(map[int64]struct{}, len(products))
	seenSlugs := make(map[string]struct{}, len(products))

	for index, product := range products {
		if !isValidCatalogueProduct(product) ||
			product.CatalogueNumber != int64(index+1) {
			return false
		}
		if _, exists := seenIDs[product.ID]; exists {
			return false
		}
		if _, exists := seenSlugs[product.Slug]; exists {
			return false
		}

		seenIDs[product.ID] = struct{}{}
		seenSlugs[product.Slug] = struct{}{}
	}

	return true
}

// formatProductCatalogueNumber converts PostgreSQL's positive row number to
// the editorial label shared by listing and detail views. Two digits are a
// minimum width, so larger honest catalogues are never truncated.
func formatProductCatalogueNumber(number int64) string {
	return fmt.Sprintf("%02d", number)
}

// newProductDetailData maps an application source record to the intentionally
// narrow detail-template view model.
//
// Keeping this conversion explicit prevents the template from depending on
// routing fields such as Slug or internal identity and keeps later Product
// fields from entering HTML without an explicit presentation decision.
func newProductDetailData(
	product catalogueProduct,
) productDetailData {
	return productDetailData{
		Number:      formatProductCatalogueNumber(product.CatalogueNumber),
		Name:        product.Name,
		Category:    product.Category,
		Status:      publishedProductStatusLabel,
		Description: product.Description,
		Material:    product.Material,
		Dimensions:  product.Dimensions,
		Cover:       optionalProductCoverPageData(product.Slug, product.Cover),
	}
}

// optionalProductCoverPageData converts a nil repository cover to a nil view
// model and otherwise returns an isolated trusted image contract.
func optionalProductCoverPageData(
	slug string,
	metadata *productCoverMetadata,
) *productCoverPageData {
	if metadata == nil {
		return nil
	}

	cover := newProductCoverPageData(slug, *metadata)
	return &cover
}

// newProductCoverPageData derives the revision-specific media path outside
// templates.
func newProductCoverPageData(
	slug string,
	metadata productCoverMetadata,
) productCoverPageData {
	return productCoverPageData{
		Path:    productCoverPath(slug, metadata.Version),
		AltText: metadata.AltText,
		Caption: metadata.Caption,
		Width:   strconv.Itoa(metadata.Width),
		Height:  strconv.Itoa(metadata.Height),
	}
}
