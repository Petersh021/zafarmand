package main

import (
	"errors"
	"reflect"
	"testing"
)

// TestPostgresSiteContentReaderIntegration exercises behavior a row stub cannot
// prove: migration seeds, published-and-covered feature eligibility, archive
// privacy, the managed-hero switch, exact media revisions, and Contact mapping.
//
// The shared two-variable opt-in and `_test` database-name guard make ordinary
// test runs skip this destructive cycle without consulting DATABASE_URL.
func TestPostgresSiteContentReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed site-content integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close site-content integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresSiteContentReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL site-content reader: %v", err)
	}
	homepage, err := reader.ReadHomepage(t.Context())
	if err != nil {
		t.Fatalf("read seeded Homepage singleton: %v", err)
	}
	wantSeededHomepage := publicHomepageContent{
		StudioName:     "Zafarmand",
		Descriptor:     "Design Studio",
		SEOTitle:       "Home | Zafarmand",
		SEODescription: "Zafarmand design studio",
		Features:       make([]publicHomepageFeature, 0),
	}
	if !reflect.DeepEqual(homepage, wantSeededHomepage) {
		t.Errorf("seeded Homepage: got %#v, want %#v", homepage, wantSeededHomepage)
	}
	contact, err := reader.ReadContact(t.Context())
	if err != nil {
		t.Fatalf("read seeded Contact singleton: %v", err)
	}
	wantSeededContact := publicContactContent{
		Eyebrow:        "Contact",
		Heading:        "Begin a conversation",
		Introduction:   "Choose a discipline and share the context Zafarmand should review.",
		SEOTitle:       "Contact | Zafarmand",
		SEODescription: "Zafarmand design studio",
	}
	if !reflect.DeepEqual(contact, wantSeededContact) {
		t.Errorf("seeded Contact: got %#v, want %#v", contact, wantSeededContact)
	}

	// The selected Interior row is both Published and cover-backed. Architecture
	// owns a cover but remains Draft, while Product is Published but coverless.
	// Only Interior may cross the public Homepage boundary.
	interior := insertPostgresInteriorProjectFixture(
		t,
		database,
		postgresInteriorProjectFixture{
			Slug:              "stage24-featured-interior",
			Title:             "Stage 24 Interior",
			Typology:          "Residential",
			ProjectStatus:     "Completed",
			SortOrder:         1,
			PublicationStatus: publishedInteriorProjectStatus,
		},
	)
	interiorCover := insertPostgresInteriorProjectCoverFixture(
		t,
		database,
		interior.ID,
		2,
	)
	architecture := insertPostgresArchitectureProjectFixture(
		t,
		database,
		postgresArchitectureProjectFixture{
			Slug:              "stage24-draft-architecture",
			Title:             "Stage 24 Draft Architecture",
			Typology:          "Cultural",
			ProjectStatus:     "Concept",
			SortOrder:         1,
			PublicationStatus: draftArchitectureProjectStatus,
		},
	)
	architectureCover := validTestArchitectureProjectCoverAsset(
		t,
		architecture.ID,
		3,
	)
	insertPostgresArchitectureProjectCoverFixture(
		t,
		database,
		architectureCover,
	)
	product := insertPostgresProductFixture(
		t,
		database,
		postgresProductFixture{
			Slug:              "stage24-coverless-product",
			Name:              "Stage 24 Coverless Product",
			Category:          "Objects",
			SortOrder:         1,
			PublicationStatus: publishedProductStatus,
		},
	)
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.homepage_content
SET featured_interior_project_id = $1,
    featured_architecture_project_id = $2,
    featured_product_id = $3,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $4`,
		interior.ID,
		architecture.ID,
		product.ID,
		siteContentSingletonID,
	); err != nil {
		t.Fatal("select synthetic Homepage feature fixtures")
	}

	homepage, err = reader.ReadHomepage(t.Context())
	if err != nil {
		t.Fatalf("read publication-filtered Homepage: %v", err)
	}
	wantInteriorFeature := publicHomepageFeature{
		Discipline:     homepageFeatureInterior,
		Slug:           interior.Slug,
		Title:          interior.Title,
		Classification: interior.Typology,
		Cover: &homepageFeatureCover{
			Version: interiorCover.Version,
			Width:   interiorCover.Width,
			Height:  interiorCover.Height,
			AltText: interiorCover.AltText,
		},
	}
	if !reflect.DeepEqual(
		homepage.Features,
		[]publicHomepageFeature{wantInteriorFeature},
	) {
		t.Errorf("eligible Homepage features: got %#v", homepage.Features)
	}

	// Archiving the selected owner removes both its card and cover metadata on
	// the very next read without clearing or exposing the private selected ID.
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.interior_projects
SET publication_status = $1,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2`,
		archivedInteriorProjectStatus,
		interior.ID,
	); err != nil {
		t.Fatal("archive synthetic selected Interior project")
	}
	homepage, err = reader.ReadHomepage(t.Context())
	if err != nil {
		t.Fatalf("read Homepage after archive: %v", err)
	}
	if homepage.Features == nil || len(homepage.Features) != 0 {
		t.Errorf("archived Homepage feature remains public: %#v", homepage.Features)
	}

	// A stored hero remains private until enabled. Once enabled, ordinary HTML
	// receives metadata and the exact media reader returns isolated normalized bytes.
	heroAsset := validTestHomepageHeroAsset(t, 4)
	if _, err := database.ExecContext(
		t.Context(),
		`INSERT INTO public.homepage_hero_images (
    homepage_content_id,
    version,
    content_type,
    content,
    byte_size,
    width,
    height,
    sha256,
    alt_text,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		siteContentSingletonID,
		heroAsset.Version,
		heroAsset.ContentType,
		heroAsset.Content,
		heroAsset.ByteSize,
		heroAsset.Width,
		heroAsset.Height,
		heroAsset.SHA256[:],
		heroAsset.AltText,
		heroAsset.CreatedAt,
		heroAsset.UpdatedAt,
	); err != nil {
		t.Fatal("insert synthetic managed Homepage hero")
	}
	homepage, err = reader.ReadHomepage(t.Context())
	if err != nil || homepage.Hero != nil {
		t.Fatalf("disabled managed hero leaked metadata: hero=%#v err=%v", homepage.Hero, err)
	}
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.homepage_content
SET managed_hero_enabled = TRUE,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1`,
		siteContentSingletonID,
	); err != nil {
		t.Fatal("enable synthetic managed Homepage hero")
	}
	homepage, err = reader.ReadHomepage(t.Context())
	if err != nil {
		t.Fatalf("read enabled managed Homepage hero metadata: %v", err)
	}
	wantHeroMetadata := &homepageHeroMetadata{
		Version: heroAsset.Version,
		Width:   heroAsset.Width,
		Height:  heroAsset.Height,
		AltText: heroAsset.AltText,
	}
	if !reflect.DeepEqual(homepage.Hero, wantHeroMetadata) {
		t.Errorf("enabled hero metadata: got %#v, want %#v", homepage.Hero, wantHeroMetadata)
	}
	gotHero, err := reader.FindHomepageHero(t.Context(), heroAsset.Version)
	if err != nil {
		t.Fatalf("find enabled exact Homepage hero: %v", err)
	}
	// PostgreSQL preserves each timestamptz instant but does not promise the same
	// Go location pointer supplied by this fixture. Compare one explicit UTC
	// representation so the assertion measures stored time rather than driver
	// presentation metadata.
	gotHero.CreatedAt = gotHero.CreatedAt.UTC()
	gotHero.UpdatedAt = gotHero.UpdatedAt.UTC()
	if !reflect.DeepEqual(gotHero, heroAsset) {
		t.Errorf("exact Homepage hero: got %#v, want %#v", gotHero, heroAsset)
	}
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.homepage_content
SET managed_hero_enabled = FALSE,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1`,
		siteContentSingletonID,
	); err != nil {
		t.Fatal("disable synthetic managed Homepage hero")
	}
	if _, err := reader.FindHomepageHero(t.Context(), heroAsset.Version); !errors.Is(err, errHomepageHeroNotFound) {
		t.Fatalf("disabled exact hero error: got %v, want not found", err)
	}

	wantContact := publicContactContent{
		Eyebrow:        "Studio contact",
		Heading:        "Start a project conversation",
		Introduction:   "Share the relevant context for a considered studio review.",
		Email:          "studio@example.com",
		PhoneDisplay:   "+98 21 5555 0101",
		PhoneE164:      "+982155550101",
		Address:        "Stage 24 Studio\nTehran",
		SEOTitle:       "Contact the Zafarmand Studio",
		SEODescription: "Contact Zafarmand about architecture, interiors, or products.",
	}
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.contact_content
SET eyebrow = $1,
    heading = $2,
    introduction = $3,
    contact_email = $4,
    phone_display = $5,
    phone_e164 = $6,
    address = $7,
    seo_title = $8,
    seo_description = $9,
    version = version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $10`,
		wantContact.Eyebrow,
		wantContact.Heading,
		wantContact.Introduction,
		wantContact.Email,
		wantContact.PhoneDisplay,
		wantContact.PhoneE164,
		wantContact.Address,
		wantContact.SEOTitle,
		wantContact.SEODescription,
		siteContentSingletonID,
	); err != nil {
		t.Fatal("update synthetic managed Contact content")
	}
	contact, err = reader.ReadContact(t.Context())
	if err != nil {
		t.Fatalf("read managed Contact content: %v", err)
	}
	if !reflect.DeepEqual(contact, wantContact) {
		t.Errorf("managed Contact: got %#v, want %#v", contact, wantContact)
	}
}
