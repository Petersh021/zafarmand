package main

import (
	"bytes"
	"errors"
	"testing"
)

// TestPostgresAdminProductWriterIntegration proves PostgreSQL behavior that
// scanner stubs cannot: identity/default revision creation, named slug conflict,
// version-guarded publication, rich content, cover replacement, stale-write
// rejection, and public visibility.
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
		Description:       "A fictional chair used only by the guarded live suite.",
		Material:          "Synthetic oak and test textile",
		Dimensions:        "800 × 520 × 600 mm",
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
		stored.Version != created.Version || stored.Description != draft.Description ||
		stored.Material != draft.Material || stored.Dimensions != draft.Dimensions ||
		stored.Cover != nil {
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

	// Cover mutation advances the Product revision and creates an independent
	// revision-specific cover path in the same PostgreSQL statement.
	coverInput := validAdminProductCoverWriteInput(t)
	createdCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		repeated.Version,
		coverInput,
	)
	if err != nil {
		t.Fatalf("create live Product cover: %v", err)
	}
	if createdCover != (adminProductCoverWriteResult{
		ProductID:      created.ID,
		ProductVersion: 4,
		CoverVersion:   1,
	}) {
		t.Fatalf("created cover result: got %#v", createdCover)
	}

	protectedRecord, err := adminReader.FindByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("read Product after cover creation: %v", err)
	}
	if protectedRecord.Version != createdCover.ProductVersion ||
		protectedRecord.Cover == nil ||
		protectedRecord.Cover.Version != createdCover.CoverVersion ||
		protectedRecord.Cover.AltText != coverInput.AltText {
		t.Errorf("protected Product cover metadata: %#v", protectedRecord)
	}
	publicRecord, err := publicReader.FindPublishedBySlug(t.Context(), draft.Slug)
	if err != nil {
		t.Fatalf("read published Product after cover creation: %v", err)
	}
	if publicRecord.Description != draft.Description ||
		publicRecord.Cover == nil ||
		publicRecord.Cover.Version != createdCover.CoverVersion {
		t.Errorf("public Product content/cover metadata: %#v", publicRecord)
	}
	publicCover, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	)
	if err != nil {
		t.Fatalf("read published cover bytes: %v", err)
	}
	if !bytes.Equal(publicCover.Content, coverInput.Content) ||
		publicCover.AltText != coverInput.AltText {
		t.Error("published cover bytes or reviewed metadata differ from writer input")
	}

	// Replacement increments both revisions. The old revision URL stops
	// resolving, while the new exact revision returns the replacement metadata.
	replacementContent, replacementInspection, err := normalizeProductCover(
		testProductCoverJPEG(t),
	)
	if err != nil {
		t.Fatalf("normalize replacement JPEG: %v", err)
	}
	replacement := adminProductCoverWriteInput{
		ContentType: replacementInspection.ContentType,
		Content:     replacementContent,
		ByteSize:    len(replacementContent),
		Width:       replacementInspection.Width,
		Height:      replacementInspection.Height,
		SHA256:      replacementInspection.SHA256,
	}
	replacement.AltText = "A replacement synthetic chair cover in side light"
	replacement.Caption = "Second reviewed Stage 21 cover revision."
	replacedCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProductVersion,
		replacement,
	)
	if err != nil {
		t.Fatalf("replace live Product cover: %v", err)
	}
	if replacedCover.ProductVersion != 5 || replacedCover.CoverVersion != 2 {
		t.Fatalf("replaced cover result: got %#v, want revisions 5/2", replacedCover)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	); !errors.Is(err, errProductCoverNotFound) {
		t.Errorf("old public cover revision: got %v, want not found", err)
	}
	newCover, err := adminReader.FindCoverByProductID(
		t.Context(),
		created.ID,
		replacedCover.CoverVersion,
	)
	if err != nil || newCover.AltText != replacement.AltText ||
		newCover.ContentType != productCoverJPEGContentType ||
		!bytes.Equal(newCover.Content, replacement.Content) {
		t.Errorf("replacement protected cover: asset=%#v err=%v", newCover, err)
	}
	publicReplacement, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		replacedCover.CoverVersion,
	)
	if err != nil || publicReplacement.ContentType != productCoverJPEGContentType ||
		!bytes.Equal(publicReplacement.Content, replacement.Content) {
		t.Errorf("replacement public cover: asset=%#v err=%v", publicReplacement, err)
	}

	if _, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProductVersion,
		coverInput,
	); !errors.Is(err, errAdminProductWriteConflict) {
		t.Errorf("stale cover replacement: got %v, want version conflict", err)
	}

	// Archiving keeps the protected cover available but removes both Product
	// metadata and exact image bytes from the published-only repository. This is
	// the persistence boundary behind the public handler's cache revalidation.
	archived := published
	archived.PublicationStatus = productPublicationStatusArchived
	archivedResult, err := writer.Update(
		t.Context(),
		created.ID,
		replacedCover.ProductVersion,
		archived,
	)
	if err != nil || archivedResult.Version != 6 {
		t.Fatalf("archive covered Product: result=%#v err=%v", archivedResult, err)
	}
	if _, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	); !errors.Is(err, errProductCatalogueNotFound) {
		t.Errorf("archived Product detail: got %v, want public not-found", err)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		replacedCover.CoverVersion,
	); !errors.Is(err, errProductCoverNotFound) {
		t.Errorf("archived Product cover: got %v, want public not-found", err)
	}
	protectedCover, err := adminReader.FindCoverByProductID(
		t.Context(),
		created.ID,
		replacedCover.CoverVersion,
	)
	if err != nil || protectedCover.AltText != replacement.AltText {
		t.Errorf("archived protected cover: asset=%#v err=%v", protectedCover, err)
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
