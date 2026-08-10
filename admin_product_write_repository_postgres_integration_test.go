package main

import (
	"errors"
	"testing"
)

// TestPostgresAdminProductWriterIntegration proves PostgreSQL behavior that
// scanner stubs cannot: identity/default revision creation, named slug conflict,
// version-guarded publication, stale-write rejection, and public visibility.
//
// The shared two-variable opt-in and `_test` database-name guard make ordinary
// test runs skip this function without consulting the development DATABASE_URL.
func TestPostgresAdminProductWriterIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin Product writer integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin Product writer integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	writer, err := newPostgresAdminProductWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Product writer: %v", err)
	}
	adminReader, err := newPostgresAdminProductReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Product reader: %v", err)
	}
	publicReader, err := newPostgresProductCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL public Product reader: %v", err)
	}

	draft := adminProductWriteInput{
		Slug:              "stage-20-integration-chair",
		Name:              "Stage 20 Integration Chair",
		Category:          "Furniture",
		SortOrder:         7,
		PublicationStatus: productPublicationStatusDraft,
	}
	created, err := writer.Create(t.Context(), draft)
	if err != nil {
		t.Fatalf("create live Product: %v", err)
	}
	if created.ID <= 0 || created.Version != 1 {
		t.Fatalf("created Product result: got %#v, want positive ID/version 1", created)
	}

	stored, err := adminReader.FindByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("read created protected Product: %v", err)
	}
	if stored.Slug != draft.Slug || stored.PublicationStatus != draft.PublicationStatus ||
		stored.Version != created.Version {
		t.Error("created protected Product does not match the fictional input")
	}
	if _, err := publicReader.FindPublishedBySlug(t.Context(), draft.Slug); !errors.Is(err, errProductCatalogueNotFound) {
		t.Errorf("draft public lookup: got %v, want public not-found", err)
	}

	duplicate := draft
	duplicate.Name = "Different Synthetic Product"
	if _, err := writer.Create(t.Context(), duplicate); !errors.Is(err, errAdminProductSlugConflict) {
		t.Errorf("duplicate slug: got %v, want slug conflict", err)
	}

	published := draft
	published.PublicationStatus = publishedProductStatus
	updated, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		published,
	)
	if err != nil {
		t.Fatalf("publish live Product: %v", err)
	}
	if updated.ID != created.ID || updated.Version != 2 {
		t.Fatalf("published Product result: got %#v, want same ID/version 2", updated)
	}
	if _, err := publicReader.FindPublishedBySlug(t.Context(), draft.Slug); err != nil {
		t.Fatalf("published Product is absent from public reader: %v", err)
	}

	stale := published
	stale.Name = "Stale Browser Value"
	if _, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		stale,
	); !errors.Is(err, errAdminProductWriteConflict) {
		t.Errorf("stale edit: got %v, want version conflict", err)
	}
	stored, err = adminReader.FindByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("read Product after stale edit: %v", err)
	}
	if stored.Name != published.Name || stored.Version != updated.Version {
		t.Error("stale edit changed the current Product")
	}

	// An explicit same-value save is still a deliberate revision. Incrementing
	// prevents another tab holding version 2 from overwriting this later save.
	repeated, err := writer.Update(
		t.Context(),
		created.ID,
		updated.Version,
		published,
	)
	if err != nil {
		t.Fatalf("repeat current live Product values: %v", err)
	}
	if repeated.Version != 3 {
		t.Errorf("same-value save version: got %d, want 3", repeated.Version)
	}

	if _, err := writer.Update(
		t.Context(),
		created.ID+1000,
		1,
		published,
	); !errors.Is(err, errAdminProductNotFound) {
		t.Errorf("missing Product update: got %v, want not found", err)
	}
}
