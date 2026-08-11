package main

import (
	"context"
	"sync"
	"time"
)

// recordingProductCatalogueReader is the database-free published catalogue
// double shared by public-route and application-construction tests. Its mutex
// keeps snapshots reliable if a future test serves requests concurrently.
type recordingProductCatalogueReader struct {
	// mu protects every configurable result and recorded call below.
	mu sync.Mutex
	// products is copied on both assignment and return so callers cannot mutate
	// the fixture through shared slice storage.
	products []catalogueProduct
	// listErr is the configured ListPublished failure.
	listErr error
	// findErr is the configured FindPublishedBySlug failure.
	findErr error
	// coverAsset is the configured complete public media result.
	coverAsset productCoverAsset
	// coverErr is the configured FindPublishedCover failure.
	coverErr error
	// listCalls records the deadline state observed by each list operation.
	listCalls []recordingProductListCall
	// findCalls records each canonical detail lookup and its deadline state.
	findCalls []recordingProductFindCall
	// coverCalls records exact public media lookups and deadline state.
	coverCalls []recordingProductCoverFindCall
}

// recordingProductListCall captures the context property that proves the HTTP
// handler bounded a public list read before invoking its dependency.
type recordingProductListCall struct {
	// Deadline is the absolute context deadline when one was present.
	Deadline time.Time
	// HasDeadline distinguishes a real deadline from time.Time's zero value.
	HasDeadline bool
}

// recordingProductFindCall captures one product-detail dependency call.
type recordingProductFindCall struct {
	// Slug is the exact canonical path value supplied by the handler.
	Slug string
	// Deadline is the absolute context deadline when one was present.
	Deadline time.Time
	// HasDeadline distinguishes a real deadline from time.Time's zero value.
	HasDeadline bool
}

// recordingProductCoverFindCall captures one published cover dependency call.
type recordingProductCoverFindCall struct {
	// Slug is the canonical owning Product path value.
	Slug string
	// Version is the requested exact cover revision.
	Version int64
	// HasDeadline proves the HTTP layer bounded the media read.
	HasDeadline bool
}

// newRecordingProductCatalogueReader returns the four fictional structural
// fixtures retained only for deterministic UI tests. Production never calls
// this helper and never receives automatic seed content.
func newRecordingProductCatalogueReader() *recordingProductCatalogueReader {
	return &recordingProductCatalogueReader{
		products: []catalogueProduct{
			{
				ID:              1,
				CatalogueNumber: 1,
				Slug:            "furniture-study-01",
				Name:            "Furniture Study 01",
				Category:        "Furniture",
			},
			{
				ID:              2,
				CatalogueNumber: 2,
				Slug:            "lighting-study-01",
				Name:            "Lighting Study 01",
				Category:        "Lighting",
			},
			{
				ID:              3,
				CatalogueNumber: 3,
				Slug:            "object-study-01",
				Name:            "Object Study 01",
				Category:        "Objects",
			},
			{
				ID:              4,
				CatalogueNumber: 4,
				Slug:            "material-study-01",
				Name:            "Material Study 01",
				Category:        "Materials",
			},
		},
	}
}

// ListPublished implements productCatalogueReader and returns an isolated copy
// of the configured catalogue after recording whether the handler set a
// deadline.
func (reader *recordingProductCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueProduct, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.listCalls = append(
		reader.listCalls,
		recordingProductListCall{
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)

	products := append([]catalogueProduct(nil), reader.products...)

	return products, reader.listErr
}

// FindPublishedBySlug implements productCatalogueReader without PostgreSQL. A
// missing fixture uses the same safe category as the production repository.
func (reader *recordingProductCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueProduct, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.findCalls = append(
		reader.findCalls,
		recordingProductFindCall{
			Slug:        slug,
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)
	if reader.findErr != nil {
		return catalogueProduct{}, reader.findErr
	}

	for _, product := range reader.products {
		if product.Slug == slug {
			return product, nil
		}
	}

	return catalogueProduct{}, errProductCatalogueNotFound
}

// FindPublishedCover implements the binary public-read boundary without a
// database and returns an isolated byte slice.
func (reader *recordingProductCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (productCoverAsset, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.coverCalls = append(
		reader.coverCalls,
		recordingProductCoverFindCall{
			Slug:        slug,
			Version:     version,
			HasDeadline: hasDeadline,
		},
	)
	if reader.coverErr != nil {
		return productCoverAsset{}, reader.coverErr
	}
	if reader.coverAsset.ProductID <= 0 ||
		reader.coverAsset.Version != version {
		return productCoverAsset{}, errProductCoverNotFound
	}

	return cloneProductCoverAsset(reader.coverAsset), nil
}

// setProducts replaces the fixture with an isolated copy.
func (reader *recordingProductCatalogueReader) setProducts(
	products []catalogueProduct,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.products = append([]catalogueProduct(nil), products...)
}

// setErrors configures independent list and detail outcomes.
func (reader *recordingProductCatalogueReader) setErrors(
	listErr error,
	findErr error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.listErr = listErr
	reader.findErr = findErr
}

// setCover configures one isolated public media result and error category.
func (reader *recordingProductCatalogueReader) setCover(
	asset productCoverAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.coverAsset = cloneProductCoverAsset(asset)
	reader.coverErr = err
}

// coverCallSnapshot returns an isolated record of public media lookups.
func (reader *recordingProductCatalogueReader) coverCallSnapshot() []recordingProductCoverFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingProductCoverFindCall(nil), reader.coverCalls...)
}

// listCallSnapshot returns an isolated record of list invocations.
func (reader *recordingProductCatalogueReader) listCallSnapshot() []recordingProductListCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingProductListCall(nil), reader.listCalls...)
}

// findCallSnapshot returns an isolated record of detail invocations.
func (reader *recordingProductCatalogueReader) findCallSnapshot() []recordingProductFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingProductFindCall(nil), reader.findCalls...)
}
