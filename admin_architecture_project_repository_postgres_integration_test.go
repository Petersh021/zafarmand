package main

import (
	"bytes"
	"errors"
	"testing"
)

// TestPostgresAdminArchitectureProjectPersistenceIntegration proves behavior
// unit scanners cannot: nullable-year creation, named slug conflicts, all-state
// reads, optimistic publication, atomic cover replacement, stale-write
// rejection, and public privacy against real PostgreSQL statements.
//
// The shared explicit opt-in and `_test` database-name guard make ordinary test
// runs skip this function without reading the development DATABASE_URL.
func TestPostgresAdminArchitectureProjectPersistenceIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin Architecture integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin Architecture integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	writer, err := newPostgresAdminArchitectureProjectWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Architecture writer: %v", err)
	}
	adminReader, err := newPostgresAdminArchitectureProjectReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Architecture reader: %v", err)
	}
	publicReader, err := newPostgresArchitectureProjectCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL public Architecture reader: %v", err)
	}

	initial, err := adminReader.List(t.Context())
	if err != nil || initial == nil || len(initial) != 0 {
		t.Fatalf("initial protected projects: projects=%#v err=%v", initial, err)
	}

	draft := adminArchitectureProjectWriteInput{
		Slug:              "stage23-admin-courtyard",
		Title:             "Stage 23 Admin Courtyard",
		Typology:          "Residential",
		Location:          "",
		ProjectYear:       0,
		ProjectStatus:     "In progress",
		Description:       "A fictional Architecture project used only by the guarded suite.",
		SortOrder:         7,
		PublicationStatus: draftArchitectureProjectStatus,
	}
	created, err := writer.Create(t.Context(), draft)
	if err != nil || created.ID <= 0 || created.Version != 1 {
		t.Fatalf("create live Architecture project: result=%#v err=%v", created, err)
	}

	stored, err := adminReader.FindByID(t.Context(), created.ID)
	if err != nil || stored.Slug != draft.Slug || stored.ProjectYear != 0 ||
		stored.PublicationStatus != draft.PublicationStatus ||
		stored.Version != created.Version || stored.Cover != nil {
		t.Fatalf("created protected project: project=%#v err=%v", stored, err)
	}
	if _, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	); !errors.Is(err, errArchitectureProjectCatalogueNotFound) {
		t.Errorf("Draft public lookup: got %v, want not found", err)
	}

	duplicate := draft
	duplicate.Title = "Different Fictional Architecture"
	if _, err := writer.Create(
		t.Context(),
		duplicate,
	); !errors.Is(err, errAdminArchitectureProjectSlugConflict) {
		t.Errorf("duplicate slug: got %v, want safe conflict", err)
	}

	published := draft
	published.Location = "Tehran"
	published.ProjectYear = 2026
	published.ProjectStatus = "Completed"
	published.PublicationStatus = publishedArchitectureProjectStatus
	updated, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		published,
	)
	if err != nil || updated.ID != created.ID || updated.Version != 2 {
		t.Fatalf("publish live Architecture project: result=%#v err=%v", updated, err)
	}
	publicProject, err := publicReader.FindPublishedBySlug(t.Context(), draft.Slug)
	if err != nil || publicProject.ProjectYear != published.ProjectYear ||
		publicProject.ProjectStatus != published.ProjectStatus {
		t.Errorf("published public project: project=%#v err=%v", publicProject, err)
	}

	stale := published
	stale.Title = "Stale Browser Architecture"
	if _, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		stale,
	); !errors.Is(err, errAdminArchitectureProjectWriteConflict) {
		t.Errorf("stale text edit: got %v, want version conflict", err)
	}

	coverInput := validAdminArchitectureProjectCoverWriteInput(t)
	createdCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		updated.Version,
		coverInput,
	)
	if err != nil || createdCover != (adminArchitectureProjectCoverWriteResult{
		ProjectID:      created.ID,
		ProjectVersion: 3,
		CoverVersion:   1,
	}) {
		t.Fatalf("create live Architecture cover: result=%#v err=%v", createdCover, err)
	}

	protectedCover, err := adminReader.FindCoverByProjectID(
		t.Context(),
		created.ID,
		createdCover.CoverVersion,
	)
	if err != nil || !bytes.Equal(protectedCover.Content, coverInput.Content) ||
		protectedCover.AltText != coverInput.AltText {
		t.Errorf("protected cover: asset=%#v err=%v", protectedCover, err)
	}
	publicCover, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	)
	if err != nil || !bytes.Equal(publicCover.Content, coverInput.Content) {
		t.Errorf("public cover: asset=%#v err=%v", publicCover, err)
	}
	publicCoverMetadata, err := publicReader.FindPublishedCoverMetadata(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	)
	wantPublicCoverMetadata := publicCover.responseMetadata()
	publicCoverMetadata.CreatedAt = publicCoverMetadata.CreatedAt.UTC()
	publicCoverMetadata.UpdatedAt = publicCoverMetadata.UpdatedAt.UTC()
	wantPublicCoverMetadata.CreatedAt = wantPublicCoverMetadata.CreatedAt.UTC()
	wantPublicCoverMetadata.UpdatedAt = wantPublicCoverMetadata.UpdatedAt.UTC()
	if err != nil || publicCoverMetadata != wantPublicCoverMetadata {
		t.Errorf("public cover metadata: got %#v err=%v", publicCoverMetadata, err)
	}

	replacement := coverInput
	replacement.AltText = "A replacement fictional Architecture cover"
	replacement.Caption = "Second reviewed Stage 23 cover revision."
	replacedCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProjectVersion,
		replacement,
	)
	if err != nil || replacedCover.ProjectVersion != 4 ||
		replacedCover.CoverVersion != 2 {
		t.Fatalf("replace live Architecture cover: result=%#v err=%v", replacedCover, err)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	); !errors.Is(err, errArchitectureProjectCoverNotFound) {
		t.Errorf("old public cover revision: got %v, want not found", err)
	}
	if _, err := publicReader.FindPublishedCoverMetadata(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	); !errors.Is(err, errArchitectureProjectCoverNotFound) {
		t.Errorf("old public cover metadata: got %v, want not found", err)
	}
	if _, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProjectVersion,
		coverInput,
	); !errors.Is(err, errAdminArchitectureProjectWriteConflict) {
		t.Errorf("stale cover replacement: got %v, want version conflict", err)
	}

	archived := published
	archived.PublicationStatus = archivedArchitectureProjectStatus
	archivedResult, err := writer.Update(
		t.Context(),
		created.ID,
		replacedCover.ProjectVersion,
		archived,
	)
	if err != nil || archivedResult.Version != 5 {
		t.Fatalf("archive covered Architecture: result=%#v err=%v", archivedResult, err)
	}
	if _, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	); !errors.Is(err, errArchitectureProjectCatalogueNotFound) {
		t.Errorf("Archived public detail: got %v, want not found", err)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		replacedCover.CoverVersion,
	); !errors.Is(err, errArchitectureProjectCoverNotFound) {
		t.Errorf("Archived public cover: got %v, want not found", err)
	}
	if _, err := publicReader.FindPublishedCoverMetadata(
		t.Context(),
		draft.Slug,
		replacedCover.CoverVersion,
	); !errors.Is(err, errArchitectureProjectCoverNotFound) {
		t.Errorf("Archived public cover metadata: got %v, want not found", err)
	}
	protectedCover, err = adminReader.FindCoverByProjectID(
		t.Context(),
		created.ID,
		replacedCover.CoverVersion,
	)
	if err != nil || protectedCover.AltText != replacement.AltText {
		t.Errorf("Archived protected cover: asset=%#v err=%v", protectedCover, err)
	}
}
