package main

import (
	"database/sql"
	"errors"
	"testing"
)

// postgresProductFixture describes one synthetic row inserted directly into
// the guarded integration database. PublicationStatus and SortOrder are test
// setup inputs only; catalogueProduct deliberately cannot expose either field.
type postgresProductFixture struct {
	// ID is PostgreSQL's generated product identity returned after insertion.
	ID int64
	// Slug is the canonical synthetic public path segment.
	Slug string
	// Name is recognizable test-only catalogue copy.
	Name string
	// Category proves independent text-field mapping.
	Category string
	// SortOrder controls the window order independently from insertion order.
	SortOrder int
	// PublicationStatus determines whether the public reader may see the row.
	PublicationStatus string
}

// TestPostgresProductCatalogueReaderIntegration exercises behavior that stubs
// cannot prove: the migration creates an unseeded table, PostgreSQL computes a
// shared published-only window, equal sort values use ID as a deterministic
// tie-breaker, non-public rows disappear, and missing details map safely.
//
// The test reuses the migration suite's two-part destructive opt-in and `_test`
// database-name guard. Ordinary go test runs skip it, and it never falls back
// to DATABASE_URL or touches a development database.
func TestPostgresProductCatalogueReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed product integration database: %v", err)
	}
	// Cleanup functions execute last-in, first-out. Register pool closure before
	// schema cleanup so the latter can still use the live connection.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close product integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	// The shared helper applies the complete current embedded catalog and proves
	// a second Up is idempotent before product behavior is exercised.
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresProductCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL product catalogue reader: %v", err)
	}

	// Migration 4 is schema-only. A fresh database must not publish placeholder
	// business records merely to make the Products page look populated.
	initialProducts, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list initial PostgreSQL product catalogue: %v", err)
	}
	if initialProducts == nil || len(initialProducts) != 0 {
		t.Fatalf(
			"initial products: got %#v, want allocated empty slice",
			initialProducts,
		)
	}
	if count := countPostgresProductFixtures(t, database); count != 0 {
		t.Fatalf("migration seeded %d product row(s), want zero", count)
	}

	// Insert order intentionally differs from editorial order. Draft and archived
	// fixtures receive earlier sort values so an incorrectly filtered window
	// would create visible catalogue-number gaps.
	fixtures := []postgresProductFixture{
		{
			Slug:              "stage18-lounge-chair",
			Name:              "Stage 18 Lounge Chair",
			Category:          "Furniture",
			SortOrder:         30,
			PublicationStatus: publishedProductStatus,
		},
		{
			Slug:              "stage18-draft-light",
			Name:              "Stage 18 Draft Light",
			Category:          "Lighting",
			SortOrder:         2,
			PublicationStatus: "draft",
		},
		{
			Slug:              "stage18-table-lamp",
			Name:              "Stage 18 Table Lamp",
			Category:          "Lighting",
			SortOrder:         10,
			PublicationStatus: publishedProductStatus,
		},
		{
			Slug:              "stage18-archived-object",
			Name:              "Stage 18 Archived Object",
			Category:          "Objects",
			SortOrder:         1,
			PublicationStatus: "archived",
		},
		{
			Slug:              "stage18-stone-vessel",
			Name:              "Stage 18 Stone Vessel",
			Category:          "Objects",
			SortOrder:         10,
			PublicationStatus: publishedProductStatus,
		},
	}
	for index := range fixtures {
		fixtures[index] = insertPostgresProductFixture(
			t,
			database,
			fixtures[index],
		)
	}

	products, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list live published products: %v", err)
	}
	// Published fixtures sort as table lamp (10, lower ID), stone vessel (10,
	// higher ID), then lounge chair (30), independent from insertion order.
	expected := []catalogueProduct{
		catalogueProductFromFixture(fixtures[2], 1),
		catalogueProductFromFixture(fixtures[4], 2),
		catalogueProductFromFixture(fixtures[0], 3),
	}
	if len(products) != len(expected) {
		t.Fatalf(
			"published product count: got %d, want %d",
			len(products),
			len(expected),
		)
	}
	for index := range expected {
		if products[index] != expected[index] {
			t.Errorf(
				"published product %d: got %#v, want %#v",
				index,
				products[index],
				expected[index],
			)
		}
	}

	// Every detail lookup must preserve the catalogue number produced by the
	// full published window rather than renumbering its one matching row to 1.
	for _, expectedProduct := range expected {
		actual, err := reader.FindPublishedBySlug(
			t.Context(),
			expectedProduct.Slug,
		)
		if err != nil {
			t.Fatalf(
				"find published product %q: %v",
				expectedProduct.Slug,
				err,
			)
		}
		if actual != expectedProduct {
			t.Errorf(
				"detail %q: got %#v, want %#v",
				expectedProduct.Slug,
				actual,
				expectedProduct,
			)
		}
	}

	// Unknown, draft, and archived slugs intentionally share one result category
	// so a public caller cannot use the detail route to enumerate private state.
	for _, slug := range []string{
		"stage18-missing-product",
		fixtures[1].Slug,
		fixtures[3].Slug,
	} {
		product, err := reader.FindPublishedBySlug(t.Context(), slug)
		if !errors.Is(err, errProductCatalogueNotFound) {
			t.Errorf(
				"hidden product %q error: got %v, want not-found sentinel",
				slug,
				err,
			)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("hidden product %q exposed %#v", slug, product)
		}
	}
}

// insertPostgresProductFixture writes one synthetic, constraint-valid product
// and returns the same fixture with its generated identity. Parameters keep all
// test text separate from the trusted SQL statement.
func insertPostgresProductFixture(
	t *testing.T,
	database *sql.DB,
	fixture postgresProductFixture,
) postgresProductFixture {
	t.Helper()

	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.products (
    slug,
    name,
    category,
    sort_order,
    publication_status
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id`,
		fixture.Slug,
		fixture.Name,
		fixture.Category,
		fixture.SortOrder,
		fixture.PublicationStatus,
	).Scan(&fixture.ID); err != nil {
		// The driver error is intentionally omitted because this shared opt-in
		// database could contain diagnostics beyond the synthetic row.
		t.Fatal("insert synthetic PostgreSQL product fixture")
	}
	if fixture.ID <= 0 {
		t.Fatal("PostgreSQL returned a non-positive product identity")
	}

	return fixture
}

// catalogueProductFromFixture maps only public fields plus an expected window
// position. The helper cannot accidentally copy publication or sort state into
// the repository result type because catalogueProduct has no such fields.
func catalogueProductFromFixture(
	fixture postgresProductFixture,
	catalogueNumber int64,
) catalogueProduct {
	return catalogueProduct{
		ID:              fixture.ID,
		CatalogueNumber: catalogueNumber,
		Slug:            fixture.Slug,
		Name:            fixture.Name,
		Category:        fixture.Category,
	}
}

// countPostgresProductFixtures confirms both the migration's initial empty
// state and the exact number of directly inserted synthetic rows.
func countPostgresProductFixtures(t *testing.T, database *sql.DB) int {
	t.Helper()

	var count int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.products",
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL product fixtures")
	}

	return count
}
