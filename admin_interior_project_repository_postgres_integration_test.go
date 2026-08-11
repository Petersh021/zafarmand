package main

import (
	"bytes"
	"errors"
	"testing"
)

// TestPostgresAdminInteriorProjectPersistenceIntegration proves behavior unit
// scanners cannot: nullable-year creation, named slug conflicts, all-state reads,
// version-guarded publication, atomic cover replacement, stale-write rejection,
// and published-only privacy across real PostgreSQL statements.
//
// The shared two-variable opt-in and `_test` database-name guard make ordinary
// test runs skip this function without reading the development DATABASE_URL.
func TestPostgresAdminInteriorProjectPersistenceIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin Interior integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin Interior integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	writer, err := newPostgresAdminInteriorProjectWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Interior writer: %v", err)
	}
	adminReader, err := newPostgresAdminInteriorProjectReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin Interior reader: %v", err)
	}
	publicReader, err := newPostgresInteriorProjectCatalogueReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL public Interior reader: %v", err)
	}

	initial, err := adminReader.List(t.Context())
	if err != nil {
		t.Fatalf("list initial protected Interior projects: %v", err)
	}
	if initial == nil || len(initial) != 0 {
		t.Fatalf("initial protected projects: got %#v, want allocated empty slice", initial)
	}

	draft := adminInteriorProjectWriteInput{
		Slug:              "stage22-admin-courtyard",
		Title:             "Stage 22 Admin Courtyard",
		Typology:          "Residential",
		Location:          "",
		ProjectYear:       0,
		ProjectStatus:     "In progress",
		Description:       "A fictional Interior project used only by the guarded suite.",
		SortOrder:         7,
		PublicationStatus: draftInteriorProjectStatus,
	}
	created, err := writer.Create(t.Context(), draft)
	if err != nil {
		t.Fatalf("create live Interior project: %v", err)
	}
	if created.ID <= 0 || created.Version != 1 {
		t.Fatalf("created result: got %#v, want positive ID/version 1", created)
	}

	stored, err := adminReader.FindByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("read created protected Interior project: %v", err)
	}
	if stored.Slug != draft.Slug || stored.ProjectYear != 0 ||
		stored.ProjectStatus != draft.ProjectStatus ||
		stored.PublicationStatus != draft.PublicationStatus ||
		stored.Version != created.Version || stored.Cover != nil {
		t.Error("created protected project does not match fictional Draft input")
	}
	if _, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	); !errors.Is(err, errInteriorProjectCatalogueNotFound) {
		t.Errorf("Draft public lookup: got %v, want not found", err)
	}

	duplicate := draft
	duplicate.Title = "Different Fictional Interior"
	if _, err := writer.Create(
		t.Context(),
		duplicate,
	); !errors.Is(err, errAdminInteriorProjectSlugConflict) {
		t.Errorf("duplicate slug: got %v, want safe conflict", err)
	}

	published := draft
	published.Location = "Tehran"
	published.ProjectYear = 2026
	published.ProjectStatus = "Completed"
	published.PublicationStatus = publishedInteriorProjectStatus
	updated, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		published,
	)
	if err != nil {
		t.Fatalf("publish live Interior project: %v", err)
	}
	if updated.ID != created.ID || updated.Version != 2 {
		t.Fatalf("published result: got %#v, want same ID/version 2", updated)
	}
	publicProject, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	)
	if err != nil || publicProject.ProjectYear != published.ProjectYear ||
		publicProject.ProjectStatus != published.ProjectStatus {
		t.Errorf("published public project: project=%#v err=%v", publicProject, err)
	}

	stale := published
	stale.Title = "Stale Browser Interior"
	if _, err := writer.Update(
		t.Context(),
		created.ID,
		created.Version,
		stale,
	); !errors.Is(err, errAdminInteriorProjectWriteConflict) {
		t.Errorf("stale text edit: got %v, want version conflict", err)
	}
	stored, err = adminReader.FindByID(t.Context(), created.ID)
	if err != nil || stored.Title != published.Title ||
		stored.Version != updated.Version {
		t.Errorf("project after stale edit: project=%#v err=%v", stored, err)
	}

	coverInput := validAdminInteriorProjectCoverWriteInput(t)
	createdCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		updated.Version,
		coverInput,
	)
	if err != nil {
		t.Fatalf("create live Interior cover: %v", err)
	}
	if createdCover != (adminInteriorProjectCoverWriteResult{
		ProjectID:      created.ID,
		ProjectVersion: 3,
		CoverVersion:   1,
	}) {
		t.Fatalf("created cover result: got %#v", createdCover)
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

	replacementContent, replacementInspection, err := normalizeReviewedCover(
		testAdminInteriorProjectCoverJPEG(t),
	)
	if err != nil {
		t.Fatalf("normalize replacement Interior JPEG: %v", err)
	}
	replacement := adminInteriorProjectCoverWriteInput{
		ContentType: replacementInspection.ContentType,
		Content:     replacementContent,
		ByteSize:    len(replacementContent),
		Width:       replacementInspection.Width,
		Height:      replacementInspection.Height,
		SHA256:      replacementInspection.SHA256,
		AltText:     "A replacement fictional Interior cover in side light",
		Caption:     "Second reviewed Stage 22 cover revision.",
	}
	replacedCover, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProjectVersion,
		replacement,
	)
	if err != nil {
		t.Fatalf("replace live Interior cover: %v", err)
	}
	if replacedCover.ProjectVersion != 4 || replacedCover.CoverVersion != 2 {
		t.Fatalf("replacement result: got %#v, want revisions 4/2", replacedCover)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		createdCover.CoverVersion,
	); !errors.Is(err, errInteriorProjectCoverNotFound) {
		t.Errorf("old public cover revision: got %v, want not found", err)
	}
	if _, err := writer.UpsertCover(
		t.Context(),
		created.ID,
		createdCover.ProjectVersion,
		coverInput,
	); !errors.Is(err, errAdminInteriorProjectWriteConflict) {
		t.Errorf("stale cover replacement: got %v, want version conflict", err)
	}

	archived := published
	archived.PublicationStatus = archivedInteriorProjectStatus
	archivedResult, err := writer.Update(
		t.Context(),
		created.ID,
		replacedCover.ProjectVersion,
		archived,
	)
	if err != nil || archivedResult.Version != 5 {
		t.Fatalf("archive covered Interior: result=%#v err=%v", archivedResult, err)
	}
	if _, err := publicReader.FindPublishedBySlug(
		t.Context(),
		draft.Slug,
	); !errors.Is(err, errInteriorProjectCatalogueNotFound) {
		t.Errorf("Archived public detail: got %v, want not found", err)
	}
	if _, err := publicReader.FindPublishedCover(
		t.Context(),
		draft.Slug,
		replacedCover.CoverVersion,
	); !errors.Is(err, errInteriorProjectCoverNotFound) {
		t.Errorf("Archived public cover: got %v, want not found", err)
	}
	protectedCover, err = adminReader.FindCoverByProjectID(
		t.Context(),
		created.ID,
		replacedCover.CoverVersion,
	)
	if err != nil || protectedCover.AltText != replacement.AltText {
		t.Errorf("Archived protected cover: asset=%#v err=%v", protectedCover, err)
	}

	if _, err := writer.Update(
		t.Context(),
		created.ID+1000,
		1,
		published,
	); !errors.Is(err, errAdminInteriorProjectNotFound) {
		t.Errorf("missing Interior update: got %v, want not found", err)
	}
}
