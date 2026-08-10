package main

import (
	"context"
	"sync"
	"time"
)

// recordingAdminProductReader is the database-free all-status Product reader
// shared by protected HTTP and application-construction tests.
type recordingAdminProductReader struct {
	// mu protects configured outcomes and recorded calls.
	mu sync.Mutex
	// products is copied on assignment and list return.
	products []adminProductRecord
	// listErr controls the next list outcome.
	listErr error
	// findErr controls every detail outcome when non-nil.
	findErr error
	// listCalls records whether each request supplied a deadline.
	listCalls []recordingAdminProductListCall
	// findCalls records the exact requested identity and deadline state.
	findCalls []recordingAdminProductFindCall
}

// recordingAdminProductListCall captures one list dependency invocation.
type recordingAdminProductListCall struct {
	// HasDeadline proves the HTTP layer bounded the repository operation.
	HasDeadline bool
}

// recordingAdminProductFindCall captures one detail dependency invocation.
type recordingAdminProductFindCall struct {
	// ID is the exact canonical identity supplied by the handler.
	ID int64
	// HasDeadline proves the HTTP layer bounded the repository operation.
	HasDeadline bool
}

// newRecordingAdminProductReader returns deterministic fictional records in
// `(sort_order, id)` order. Production never calls this helper.
func newRecordingAdminProductReader() *recordingAdminProductReader {
	createdAt := time.Date(2026, time.January, 2, 10, 30, 0, 0, time.UTC)

	return &recordingAdminProductReader{
		products: []adminProductRecord{
			{
				ID:                2,
				Slug:              "stage19-draft-chair",
				Name:              "Stage 19 Draft Chair",
				Category:          "Furniture",
				SortOrder:         1,
				PublicationStatus: productPublicationStatusDraft,
				CreatedAt:         createdAt,
				UpdatedAt:         createdAt,
			},
			{
				ID:                1,
				Slug:              "stage19-published-lamp",
				Name:              "Stage 19 Published Lamp",
				Category:          "Lighting",
				SortOrder:         2,
				PublicationStatus: publishedProductStatus,
				CreatedAt:         createdAt,
				UpdatedAt:         createdAt.Add(time.Hour),
			},
			{
				ID:                3,
				Slug:              "stage19-archived-object",
				Name:              "Stage 19 Archived Object",
				Category:          "Objects",
				SortOrder:         3,
				PublicationStatus: productPublicationStatusArchived,
				CreatedAt:         createdAt,
				UpdatedAt:         createdAt.Add(2 * time.Hour),
			},
		},
	}
}

// List implements adminProductReader with an isolated record slice.
func (reader *recordingAdminProductReader) List(
	ctx context.Context,
) ([]adminProductRecord, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.listCalls = append(
		reader.listCalls,
		recordingAdminProductListCall{HasDeadline: hasDeadline},
	)

	return append([]adminProductRecord(nil), reader.products...), reader.listErr
}

// FindByID implements adminProductReader and shares the production not-found
// category for an identity absent from the fixture.
func (reader *recordingAdminProductReader) FindByID(
	ctx context.Context,
	id int64,
) (adminProductRecord, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.findCalls = append(
		reader.findCalls,
		recordingAdminProductFindCall{
			ID:          id,
			HasDeadline: hasDeadline,
		},
	)
	if reader.findErr != nil {
		return adminProductRecord{}, reader.findErr
	}
	for _, product := range reader.products {
		if product.ID == id {
			return product, nil
		}
	}

	return adminProductRecord{}, errAdminProductNotFound
}

// setProducts replaces the fixture with an isolated slice.
func (reader *recordingAdminProductReader) setProducts(
	products []adminProductRecord,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.products = append([]adminProductRecord(nil), products...)
}

// setErrors configures independent list and detail failures.
func (reader *recordingAdminProductReader) setErrors(listErr error, findErr error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.listErr = listErr
	reader.findErr = findErr
}

// listCallSnapshot returns an isolated record of list invocations.
func (reader *recordingAdminProductReader) listCallSnapshot() []recordingAdminProductListCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingAdminProductListCall(nil), reader.listCalls...)
}

// findCallSnapshot returns an isolated record of detail invocations.
func (reader *recordingAdminProductReader) findCallSnapshot() []recordingAdminProductFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingAdminProductFindCall(nil), reader.findCalls...)
}
