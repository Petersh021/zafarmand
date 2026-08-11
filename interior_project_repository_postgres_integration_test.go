package main

import (
	"database/sql"
	"reflect"
	"testing"
)

// postgresInteriorProjectFixture describes one synthetic row inserted directly
// into the guarded integration database. Sort and lifecycle values remain setup
// inputs and cannot leak through catalogueInteriorProject.
type postgresInteriorProjectFixture struct {
	// ID is PostgreSQL's generated project identity.
	ID int64
	// Slug is the canonical fictional public path value.
	Slug string
	// Title is recognizable test-only heading copy.
	Title string
	// Typology proves required category mapping.
	Typology string
	// Location is optional reviewed location copy.
	Location string
	// ProjectYear is zero when SQL should receive NULL.
	ProjectYear int
	// ProjectStatus is required editorial state, not publication state.
	ProjectStatus string
	// Description proves optional long-form mapping.
	Description string
	// SortOrder controls window order independently from insertion identity.
	SortOrder int
	// PublicationStatus controls public eligibility.
	PublicationStatus string
}

// TestPostgresInteriorProjectCatalogueReaderIntegration exercises PostgreSQL
// behavior stubs cannot prove: unseeded migration state, published-only window
// numbering, ID tie-breaking, nullable years, LEFT JOIN cover metadata, exact
// media revisions, and publication-state privacy.
//
// The shared destructive opt-in requires a confirmed `_test` database. This
// test never falls back to DATABASE_URL and ordinary go test runs skip it.
func TestPostgresInteriorProjectCatalogueReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed Interior integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close Interior integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresInteriorProjectCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL Interior catalogue reader: %v", err)
	}

	initial, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list initial Interior catalogue: %v", err)
	}
	if initial == nil || len(initial) != 0 {
		t.Fatalf("initial projects: got %#v, want allocated empty slice", initial)
	}
	if count := countPostgresInteriorProjectFixtures(t, database); count != 0 {
		t.Fatalf("migration seeded %d Interior row(s), want zero", count)
	}

	// Draft and Archived rows intentionally sort before public rows. If the SQL
	// numbered before filtering, the visible result would contain gaps.
	fixtures := []postgresInteriorProjectFixture{
		{
			Slug:              "stage22-courtyard-home",
			Title:             "Stage 22 Courtyard Home",
			Typology:          "Residential",
			Location:          "Tehran",
			ProjectYear:       2033,
			ProjectStatus:     "Completed",
			Description:       "A fictional courtyard Interior used for integration testing.",
			SortOrder:         30,
			PublicationStatus: publishedInteriorProjectStatus,
		},
		{
			Slug:              "stage22-draft-hotel",
			Title:             "Stage 22 Draft Hotel",
			Typology:          "Hospitality",
			ProjectStatus:     "In progress",
			SortOrder:         2,
			PublicationStatus: draftInteriorProjectStatus,
		},
		{
			Slug:              "stage22-gallery",
			Title:             "Stage 22 Gallery",
			Typology:          "Cultural",
			Location:          "Shiraz",
			ProjectYear:       0,
			ProjectStatus:     "Completed",
			Description:       "A fictional gallery with an intentionally absent project year.",
			SortOrder:         10,
			PublicationStatus: publishedInteriorProjectStatus,
		},
		{
			Slug:              "stage22-archived-workplace",
			Title:             "Stage 22 Archived Workplace",
			Typology:          "Workplace",
			ProjectStatus:     "Completed",
			SortOrder:         1,
			PublicationStatus: archivedInteriorProjectStatus,
		},
		{
			Slug:              "stage22-reading-room",
			Title:             "Stage 22 Reading Room",
			Typology:          "Cultural",
			Location:          "Isfahan",
			ProjectYear:       2034,
			ProjectStatus:     "Completed",
			SortOrder:         10,
			PublicationStatus: publishedInteriorProjectStatus,
		},
	}
	for index := range fixtures {
		fixtures[index] = insertPostgresInteriorProjectFixture(
			t,
			database,
			fixtures[index],
		)
	}

	cover := insertPostgresInteriorProjectCoverFixture(
		t,
		database,
		fixtures[2].ID,
		3,
	)
	// A Draft owner may retain a reviewed protected cover, but the public media
	// query must not reveal either its bytes or existence.
	insertPostgresInteriorProjectCoverFixture(
		t,
		database,
		fixtures[1].ID,
		1,
	)

	projects, err := reader.ListPublished(t.Context())
	if err != nil {
		t.Fatalf("list live published Interior projects: %v", err)
	}
	expected := []catalogueInteriorProject{
		catalogueInteriorProjectFromFixture(fixtures[2], 1),
		catalogueInteriorProjectFromFixture(fixtures[4], 2),
		catalogueInteriorProjectFromFixture(fixtures[0], 3),
	}
	expected[0].Cover = &interiorProjectCoverMetadata{
		Version: cover.Version,
		Width:   cover.Width,
		Height:  cover.Height,
		AltText: cover.AltText,
		Caption: cover.Caption,
	}
	if !reflect.DeepEqual(projects, expected) {
		t.Errorf("published projects: got %#v, want %#v", projects, expected)
	}

	// Detail lookups retain the number from the entire published window rather
	// than assigning every selected project position one.
	for _, expectedProject := range expected {
		actual, err := reader.FindPublishedBySlug(
			t.Context(),
			expectedProject.Slug,
		)
		if err != nil {
			t.Fatalf("find published Interior %q: %v", expectedProject.Slug, err)
		}
		if !reflect.DeepEqual(actual, expectedProject) {
			t.Errorf(
				"detail %q: got %#v, want %#v",
				expectedProject.Slug,
				actual,
				expectedProject,
			)
		}
	}

	for _, slug := range []string{
		"stage22-missing-interior",
		fixtures[1].Slug,
		fixtures[3].Slug,
	} {
		project, err := reader.FindPublishedBySlug(t.Context(), slug)
		if err != errInteriorProjectCatalogueNotFound ||
			project != (catalogueInteriorProject{}) {
			t.Errorf("hidden detail %q: project=%#v err=%v", slug, project, err)
		}
	}

	actualCover, err := reader.FindPublishedCover(
		t.Context(),
		fixtures[2].Slug,
		cover.Version,
	)
	if err != nil {
		t.Fatalf("find published Interior cover: %v", err)
	}
	if !interiorProjectCoverAssetsEqual(actualCover, cover) {
		t.Errorf("published cover: got %#v, want %#v", actualCover, cover)
	}
	for _, lookup := range []struct {
		// slug is the owning public coordinate.
		slug string
		// version is the requested exact revision.
		version int64
	}{
		{slug: fixtures[2].Slug, version: cover.Version + 1},
		{slug: fixtures[1].Slug, version: 1},
		{slug: fixtures[3].Slug, version: 1},
	} {
		asset, err := reader.FindPublishedCover(
			t.Context(),
			lookup.slug,
			lookup.version,
		)
		if err != errInteriorProjectCoverNotFound ||
			!reflect.DeepEqual(asset, interiorProjectCoverAsset{}) {
			t.Errorf("hidden cover %#v: asset=%#v err=%v", lookup, asset, err)
		}
	}

	// Archiving after a successful image read must hide both HTML and bytes on
	// the next public query, while the cover row remains durably stored.
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.interior_projects
SET publication_status = 'archived'
WHERE id = $1`,
		fixtures[2].ID,
	); err != nil {
		t.Fatal("archive synthetic Interior fixture")
	}
	if _, err := reader.FindPublishedBySlug(t.Context(), fixtures[2].Slug); err != errInteriorProjectCatalogueNotFound {
		t.Errorf("archived detail error: got %v, want not found", err)
	}
	if _, err := reader.FindPublishedCover(
		t.Context(),
		fixtures[2].Slug,
		cover.Version,
	); err != errInteriorProjectCoverNotFound {
		t.Errorf("archived cover error: got %v, want not found", err)
	}
	if count := countPostgresInteriorProjectCovers(
		t,
		database,
		fixtures[2].ID,
	); count != 1 {
		t.Errorf("archiving changed durable cover count: got %d, want 1", count)
	}
}

// interiorProjectCoverAssetsEqual compares timestamp instants with time.Equal
// so an equivalent PostgreSQL scan location does not make a valid asset appear
// different from the deterministic UTC fixture.
func interiorProjectCoverAssetsEqual(
	actual interiorProjectCoverAsset,
	expected interiorProjectCoverAsset,
) bool {
	return actual.InteriorProjectID == expected.InteriorProjectID &&
		actual.Version == expected.Version &&
		actual.ContentType == expected.ContentType &&
		reflect.DeepEqual(actual.Content, expected.Content) &&
		actual.ByteSize == expected.ByteSize &&
		actual.Width == expected.Width &&
		actual.Height == expected.Height &&
		actual.SHA256 == expected.SHA256 &&
		actual.AltText == expected.AltText &&
		actual.Caption == expected.Caption &&
		actual.CreatedAt.Equal(expected.CreatedAt) &&
		actual.UpdatedAt.Equal(expected.UpdatedAt)
}

// insertPostgresInteriorProjectFixture writes one parameterized constraint-
// valid row and returns its generated identity.
func insertPostgresInteriorProjectFixture(
	t *testing.T,
	database *sql.DB,
	fixture postgresInteriorProjectFixture,
) postgresInteriorProjectFixture {
	t.Helper()

	var projectYear any
	if fixture.ProjectYear != 0 {
		projectYear = fixture.ProjectYear
	}
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.interior_projects (
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
		t.Fatal("insert synthetic PostgreSQL Interior project")
	}
	if fixture.ID <= 0 {
		t.Fatal("PostgreSQL returned non-positive Interior project identity")
	}

	return fixture
}

// insertPostgresInteriorProjectCoverFixture persists one deterministic exact
// image revision and returns the complete expected public asset.
func insertPostgresInteriorProjectCoverFixture(
	t *testing.T,
	database *sql.DB,
	projectID int64,
	version int64,
) interiorProjectCoverAsset {
	t.Helper()

	asset := validTestInteriorProjectCoverAsset(t, projectID, version)
	if _, err := database.ExecContext(
		t.Context(),
		`INSERT INTO public.interior_project_cover_images (
    interior_project_id,
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
		asset.InteriorProjectID,
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
		t.Fatal("insert synthetic PostgreSQL Interior cover")
	}

	return asset
}

// catalogueInteriorProjectFromFixture maps public fields plus one expected
// window position without copying sort order or publication state.
func catalogueInteriorProjectFromFixture(
	fixture postgresInteriorProjectFixture,
	portfolioNumber int64,
) catalogueInteriorProject {
	return catalogueInteriorProject{
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
}

// countPostgresInteriorProjectFixtures verifies the migration remains unseeded.
func countPostgresInteriorProjectFixtures(t *testing.T, database *sql.DB) int {
	t.Helper()

	var count int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.interior_projects",
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL Interior fixtures")
	}

	return count
}

// countPostgresInteriorProjectCovers verifies lifecycle edits do not delete an
// otherwise valid protected cover row.
func countPostgresInteriorProjectCovers(
	t *testing.T,
	database *sql.DB,
	projectID int64,
) int {
	t.Helper()

	var count int
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT COUNT(*)
FROM public.interior_project_cover_images
WHERE interior_project_id = $1`,
		projectID,
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL Interior cover fixtures")
	}

	return count
}
