package main

import (
	"errors"
	"testing"
)

// TestPostgresAdminProductReaderIntegration proves the behavior that unit stubs
// cannot: migrations 4–6 start empty, the protected projection reads all
// lifecycle states and revisions, PostgreSQL applies `(sort_order, id)` ordering,
// and positive-ID detail lookup maps a genuine missing row safely.
//
// The shared two-part opt-in and `_test` database-name guard make an ordinary
// `go test` skip this function. It never falls back to DATABASE_URL.
func TestPostgresAdminProductReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin Product integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin Product integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresAdminProductReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Product reader: %v", err)
	}

	initial, err := reader.List(t.Context())
	if err != nil {
		t.Fatalf("list initial protected Product catalogue: %v", err)
	}
	if initial == nil || len(initial) != 0 {
		t.Fatalf("initial Products: got %#v, want allocated empty slice", initial)
	}

	// Insertion order differs from editorial order, and two rows intentionally
	// share a sort value so the generated ID proves the deterministic tie-breaker.
	fixtures := []postgresProductFixture{
		{
			Slug:              "stage19-published-light",
			Name:              "Stage 19 Published Light",
			Category:          "Lighting",
			SortOrder:         20,
			PublicationStatus: publishedProductStatus,
		},
		{
			Slug:              "stage19-draft-chair",
			Name:              "Stage 19 Draft Chair",
			Category:          "Furniture",
			SortOrder:         20,
			PublicationStatus: productPublicationStatusDraft,
		},
		{
			Slug:              "stage19-archived-object",
			Name:              "Stage 19 Archived Object",
			Category:          "Objects",
			SortOrder:         5,
			PublicationStatus: productPublicationStatusArchived,
		},
	}
	for index := range fixtures {
		fixtures[index] = insertPostgresProductFixture(t, database, fixtures[index])
	}

	products, err := reader.List(t.Context())
	if err != nil {
		t.Fatalf("list live protected Products: %v", err)
	}
	expectedOrder := []postgresProductFixture{fixtures[2], fixtures[0], fixtures[1]}
	if len(products) != len(expectedOrder) {
		t.Fatalf("Product count: got %d, want %d", len(products), len(expectedOrder))
	}
	for index, expected := range expectedOrder {
		assertAdminProductMatchesPostgresFixture(t, products[index], expected)
	}

	for _, expected := range fixtures {
		product, err := reader.FindByID(t.Context(), expected.ID)
		if err != nil {
			t.Fatalf("find protected Product %d: %v", expected.ID, err)
		}
		assertAdminProductMatchesPostgresFixture(t, product, expected)
	}

	missing, err := reader.FindByID(t.Context(), fixtures[2].ID+1000)
	if !errors.Is(err, errAdminProductNotFound) {
		t.Errorf("missing Product error: got %v, want not-found sentinel", err)
	}
	if missing != (adminProductRecord{}) {
		t.Errorf("missing Product exposed %#v", missing)
	}
}

// assertAdminProductMatchesPostgresFixture compares all fixture-controlled
// values and validates database-owned timestamps without printing stored text
// in a failure produced by this opt-in test.
func assertAdminProductMatchesPostgresFixture(
	t *testing.T,
	actual adminProductRecord,
	expected postgresProductFixture,
) {
	t.Helper()

	if actual.ID != expected.ID ||
		actual.Slug != expected.Slug ||
		actual.Name != expected.Name ||
		actual.Category != expected.Category ||
		actual.SortOrder != expected.SortOrder ||
		actual.PublicationStatus != expected.PublicationStatus ||
		actual.Version != 1 {
		t.Error("protected Product projection does not match its synthetic fixture")
	}
	if actual.CreatedAt.IsZero() ||
		actual.UpdatedAt.IsZero() ||
		actual.UpdatedAt.Before(actual.CreatedAt) {
		t.Error("protected Product timestamps violate Product schema invariants")
	}
}
