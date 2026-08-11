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
