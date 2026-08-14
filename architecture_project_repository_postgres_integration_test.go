package main

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

// postgresArchitectureProjectFixture describes one synthetic row inserted
// directly into the guarded integration database. Private lifecycle and order
// fields remain setup-only and cannot leak through the public result type.
type postgresArchitectureProjectFixture struct {
	ID                int64
	Slug              string
	Title             string
	Typology          string
	Location          string
	ProjectYear       int
	ProjectStatus     string
	Description       string
	SortOrder         int
	PublicationStatus string
	Cover             *architectureProjectCoverAsset
}

// TestPostgresArchitectureProjectCatalogueReaderIntegration exercises the
// database behavior a stub cannot prove: migration 8 is schema-only, public
// numbering follows a filtered deterministic window, private rows stay hidden,
// nullable values and cover metadata map correctly, and exact media revisions
// require a currently published owner.
//
// The shared migration test helper requires both a dedicated `_test` database
// URL and an explicit destructive confirmation. Normal `go test` runs skip the
// test and never fall back to a development DATABASE_URL.
func TestPostgresArchitectureProjectCatalogueReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed Architecture integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close Architecture integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresArchitectureProjectCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL Architecture reader: %v", err)
	}
	initial, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list initial Architecture catalogue: %v", err)
	}
	if initial == nil || len(initial) != 0 {
		t.Fatalf("initial Architecture rows: got %#v, want allocated empty", initial)
	}
	if count := countPostgresArchitectureProjectFixtures(t, database); count != 0 {
		t.Fatalf("migration seeded %d Architecture row(s), want zero", count)
	}
	if count := countPostgresArchitectureProjectCoverFixtures(t, database); count != 0 {
		t.Fatalf("migration seeded %d Architecture cover(s), want zero", count)
	}

	// Insertion order differs from editorial order. Hidden fixtures sort first so
	// filtering after ROW_NUMBER would create visible numbering gaps.
	fixtures := []postgresArchitectureProjectFixture{
		{
			Slug:              "stage23-cultural-centre",
			Title:             "Stage 23 Cultural Centre",
			Typology:          "Cultural",
			Location:          "Shiraz",
			ProjectYear:       2024,
			ProjectStatus:     "Completed",
			Description:       "Synthetic published project for repository verification.",
			SortOrder:         30,
			PublicationStatus: publishedArchitectureProjectStatus,
		},
		{
			Slug:              "stage23-draft-house",
			Title:             "Stage 23 Draft House",
			Typology:          "Residential",
			ProjectStatus:     "Concept",
			SortOrder:         1,
			PublicationStatus: draftArchitectureProjectStatus,
		},
		{
			Slug:              "stage23-courtyard-house",
			Title:             "Stage 23 Courtyard House",
			Typology:          "Residential",
			Location:          "Tehran",
			ProjectYear:       2026,
			ProjectStatus:     "In progress",
			Description:       "Synthetic ordered project with a reviewed cover.",
			SortOrder:         10,
			PublicationStatus: publishedArchitectureProjectStatus,
		},
		{
			Slug:              "stage23-archived-pavilion",
			Title:             "Stage 23 Archived Pavilion",
			Typology:          "Civic",
			ProjectStatus:     "Completed",
			SortOrder:         2,
			PublicationStatus: archivedArchitectureProjectStatus,
		},
		{
			Slug:              "stage23-museum-study",
			Title:             "Stage 23 Museum Study",
			Typology:          "Museum",
			ProjectStatus:     "Competition",
			SortOrder:         10,
			PublicationStatus: publishedArchitectureProjectStatus,
		},
	}
	for index := range fixtures {
		fixtures[index] = insertPostgresArchitectureProjectFixture(
			t,
			database,
			fixtures[index],
		)
	}

	// Covers on Draft and Archived owners prove the media join cannot make a
	// private record public. One published cover proves metadata and bytes paths.
	for _, index := range []int{1, 2, 3} {
		asset := validArchitectureProjectCoverAsset(t)
		asset.ArchitectureProjectID = fixtures[index].ID
		asset.Version = int64(index + 3)
		fixtures[index].Cover = &asset
		insertPostgresArchitectureProjectCoverFixture(t, database, asset)
	}

	projects, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list live published Architecture projects: %v", err)
	}
	expected := []catalogueArchitectureProject{
		catalogueArchitectureProjectFromFixture(fixtures[2], 1),
		catalogueArchitectureProjectFromFixture(fixtures[4], 2),
		catalogueArchitectureProjectFromFixture(fixtures[0], 3),
	}
	if !reflect.DeepEqual(projects, expected) {
		t.Errorf("published Architecture rows: got %#v, want %#v", projects, expected)
	}

	for _, expectedProject := range expected {
		actual, err := reader.FindPublishedBySlug(t.Context(), expectedProject.Slug)
		if err != nil {
			t.Fatalf("find published Architecture project %q: %v", expectedProject.Slug, err)
		}
		if !reflect.DeepEqual(actual, expectedProject) {
			t.Errorf("detail %q: got %#v, want %#v", expectedProject.Slug, actual, expectedProject)
		}
	}
	for _, slug := range []string{
		"stage23-missing-project",
		fixtures[1].Slug,
		fixtures[3].Slug,
	} {
		project, err := reader.FindPublishedBySlug(t.Context(), slug)
		if !errors.Is(err, errArchitectureProjectCatalogueNotFound) {
			t.Errorf("hidden project %q error: got %v", slug, err)
		}
		if project != (catalogueArchitectureProject{}) {
			t.Errorf("hidden project %q exposed %#v", slug, project)
		}
	}

	publishedCover := *fixtures[2].Cover
	actualCover, err := reader.FindPublishedCover(
		t.Context(),
		fixtures[2].Slug,
		publishedCover.Version,
	)
	if err != nil {
		t.Fatalf("find published Architecture cover: %v", err)
	}
	// PostgreSQL timestamptz stores an instant rather than an IANA location. A
	// server may return the same instant with its configured zone, so normalize
	// both scanned timestamps before comparing the complete deterministic asset.
	actualCover.CreatedAt = actualCover.CreatedAt.UTC()
	actualCover.UpdatedAt = actualCover.UpdatedAt.UTC()
	if !reflect.DeepEqual(actualCover, publishedCover) {
		t.Errorf("published cover: got %#v, want %#v", actualCover, publishedCover)
	}
	for _, request := range []struct {
		slug    string
		version int64
	}{
		{slug: fixtures[2].Slug, version: publishedCover.Version + 1},
		{slug: fixtures[1].Slug, version: fixtures[1].Cover.Version},
		{slug: fixtures[3].Slug, version: fixtures[3].Cover.Version},
		{slug: "stage23-missing-project", version: 1},
	} {
		asset, err := reader.FindPublishedCover(t.Context(), request.slug, request.version)
		if !errors.Is(err, errArchitectureProjectCoverNotFound) {
			t.Errorf("hidden cover %#v error: got %v", request, err)
		}
		if !reflect.DeepEqual(asset, architectureProjectCoverAsset{}) {
			t.Errorf("hidden cover %#v exposed %#v", request, asset)
		}
	}
}

// insertPostgresArchitectureProjectFixture inserts one constraint-valid row
// and returns its generated identity without exposing driver diagnostics.
func insertPostgresArchitectureProjectFixture(
	t *testing.T,
	database *sql.DB,
	fixture postgresArchitectureProjectFixture,
) postgresArchitectureProjectFixture {
	t.Helper()

	var projectYear any
	if fixture.ProjectYear != 0 {
		projectYear = fixture.ProjectYear
	}
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.architecture_projects (
    slug,
    title,
    typology,
    location,
    project_year,
    project_status,
    description,
    sort_order,
    publication_status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id`,
		fixture.Slug,
		fixture.Title,
		fixture.Typology,
		fixture.Location,
		projectYear,
		fixture.ProjectStatus,
		fixture.Description,
		fixture.SortOrder,
		fixture.PublicationStatus,
	).Scan(&fixture.ID); err != nil {
		t.Fatal("insert synthetic PostgreSQL Architecture project")
	}
	if fixture.ID <= 0 {
		t.Fatal("PostgreSQL returned a non-positive Architecture identity")
	}
	return fixture
}

// insertPostgresArchitectureProjectCoverFixture writes one fully validated
// image record using parameters for every binary or editorial value.
func insertPostgresArchitectureProjectCoverFixture(
	t *testing.T,
	database *sql.DB,
	asset architectureProjectCoverAsset,
) {
	t.Helper()
	if !isValidArchitectureProjectCoverAsset(asset) {
		t.Fatal("integration setup attempted to insert an invalid cover")
	}

	if _, err := database.ExecContext(
		t.Context(),
		`INSERT INTO public.architecture_project_cover_images (
    architecture_project_id,
    version,
    content_type,
    content,
    byte_size,
    width,
    height,
    sha256,
    alt_text,
    caption,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		asset.ArchitectureProjectID,
		asset.Version,
		asset.ContentType,
		asset.Content,
		asset.ByteSize,
		asset.Width,
		asset.Height,
		asset.SHA256[:],
		asset.AltText,
		asset.Caption,
		asset.CreatedAt,
		asset.UpdatedAt,
	); err != nil {
		t.Fatal("insert synthetic PostgreSQL Architecture cover")
	}
}

// catalogueArchitectureProjectFromFixture constructs only the public projection
// and expected list-wide number from a private integration fixture.
func catalogueArchitectureProjectFromFixture(
	fixture postgresArchitectureProjectFixture,
	portfolioNumber int64,
) catalogueArchitectureProject {
	project := catalogueArchitectureProject{
		ID:              fixture.ID,
		PortfolioNumber: portfolioNumber,
		Slug:            fixture.Slug,
		Title:           fixture.Title,
		Typology:        fixture.Typology,
		Location:        fixture.Location,
		ProjectYear:     fixture.ProjectYear,
		ProjectStatus:   fixture.ProjectStatus,
		Description:     fixture.Description,
	}
	if fixture.Cover != nil {
		project.Cover = &architectureProjectCoverMetadata{
			Version: fixture.Cover.Version,
			Width:   fixture.Cover.Width,
			Height:  fixture.Cover.Height,
			AltText: fixture.Cover.AltText,
			Caption: fixture.Cover.Caption,
		}
	}
	return project
}

// countPostgresArchitectureProjectFixtures verifies migration emptiness.
func countPostgresArchitectureProjectFixtures(
	t *testing.T,
	database *sql.DB,
) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.architecture_projects",
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL Architecture projects")
	}
	return count
}

// countPostgresArchitectureProjectCoverFixtures verifies cover-table emptiness.
func countPostgresArchitectureProjectCoverFixtures(
	t *testing.T,
	database *sql.DB,
) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.architecture_project_cover_images",
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL Architecture covers")
	}
	return count
}
