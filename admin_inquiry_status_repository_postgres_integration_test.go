package main

import (
	"errors"
	"testing"
	"time"
)

// TestPostgresAdminInquiryStatusUpdaterIntegration exercises behavior a result
// stub cannot prove: PostgreSQL's primary-key match count, database-clock
// timestamp change, same-status timestamp preservation, constraint-compatible
// transitions, and missing-row classification. It uses only synthetic records.
//
// The test inherits the repository's destructive two-variable opt-in and
// `_test` database-name guard. Ordinary go test runs skip it and never fall
// back to the development DATABASE_URL value.
func TestPostgresAdminInquiryStatusUpdaterIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin inquiry status integration database: %v", err)
	}
	// Cleanup functions execute last-in, first-out. Register pool closure before
	// schema cleanup so the cleanup helper can still use the live connection.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin inquiry status integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	// Stage 17 reuses the status, updated_at, timestamp constraint, and primary
	// key introduced by migration 1. Applying the complete catalog proves its
	// focused inquiry mutation remains compatible with the independent Product,
	// Interior, Architecture, and Site-content tables, revisions, and media
	// storage and retention support introduced by versions 000004-000010.
	applyRepositoryIntegrationMigrations(t, database)

	var migrationCount int
	var newestMigration int
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT COUNT(*), COALESCE(MAX(version), 0)
FROM public.schema_migrations`,
	).Scan(&migrationCount, &newestMigration); err != nil {
		t.Fatal("inspect synthetic status integration migration state")
	}
	if migrationCount != 10 || newestMigration != 10 {
		t.Fatalf(
			"migration state: got count=%d newest=%d, want 10/10",
			migrationCount,
			newestMigration,
		)
	}

	updater, err := newPostgresAdminInquiryStatusUpdater(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin inquiry status updater: %v", err)
	}
	reader, err := newPostgresAdminInquiryReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin inquiry reader: %v", err)
	}

	fixture := insertPostgresAdminInquiryFixture(t, database, 0)
	// Moving both fixture timestamps into the past makes the first real status
	// transition's clock advance deterministic without sleeping in the suite.
	baseline := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	setupResult, err := database.ExecContext(
		t.Context(),
		`UPDATE public.inquiries
SET created_at = $2, updated_at = $2, status = 'new'
WHERE id = $1`,
		fixture.ID,
		baseline,
	)
	if err != nil {
		t.Fatal("prepare synthetic inquiry status timestamps")
	}
	setupRows, err := setupResult.RowsAffected()
	if err != nil || setupRows != 1 {
		t.Fatalf(
			"prepare fixture rows: got %d/error=%v, want 1/nil",
			setupRows,
			err,
		)
	}
	fixture.Status = inquiryStatusNew
	fixture.CreatedAt = baseline
	fixture.UpdatedAt = baseline

	if err := updater.UpdateStatus(
		t.Context(),
		fixture.ID,
		inquiryStatusReviewed,
	); err != nil {
		t.Fatalf("change PostgreSQL inquiry status to reviewed: %v", err)
	}
	reviewed, err := reader.FindByID(t.Context(), fixture.ID)
	if err != nil {
		t.Fatalf("read reviewed PostgreSQL inquiry: %v", err)
	}
	if reviewed.Status != inquiryStatusReviewed {
		t.Errorf(
			"reviewed status: got %q, want %q",
			reviewed.Status,
			inquiryStatusReviewed,
		)
	}
	if !reviewed.UpdatedAt.After(baseline) {
		t.Errorf(
			"reviewed updated_at %s did not advance beyond baseline %s",
			reviewed.UpdatedAt,
			baseline,
		)
	}
	assertAdminInquiryStatusUpdatePreservedFields(t, reviewed, fixture)

	// Repeating the stored state must succeed but must not claim a second state
	// change by advancing updated_at.
	if err := updater.UpdateStatus(
		t.Context(),
		fixture.ID,
		inquiryStatusReviewed,
	); err != nil {
		t.Fatalf("repeat reviewed PostgreSQL inquiry status: %v", err)
	}
	repeated, err := reader.FindByID(t.Context(), fixture.ID)
	if err != nil {
		t.Fatalf("read repeated PostgreSQL inquiry status: %v", err)
	}
	if repeated != reviewed {
		t.Errorf(
			"same-status update changed stored detail: got %#v, want %#v",
			repeated,
			reviewed,
		)
	}

	// A second genuine transition proves another supported target is accepted.
	// PostgreSQL timestamp precision permits two fast statements to share one
	// instant, so this assertion requires non-regression rather than strict growth.
	if err := updater.UpdateStatus(
		t.Context(),
		fixture.ID,
		inquiryStatusArchived,
	); err != nil {
		t.Fatalf("change PostgreSQL inquiry status to archived: %v", err)
	}
	archived, err := reader.FindByID(t.Context(), fixture.ID)
	if err != nil {
		t.Fatalf("read archived PostgreSQL inquiry: %v", err)
	}
	if archived.Status != inquiryStatusArchived {
		t.Errorf(
			"archived status: got %q, want %q",
			archived.Status,
			inquiryStatusArchived,
		)
	}
	if archived.UpdatedAt.Before(reviewed.UpdatedAt) {
		t.Errorf(
			"archived updated_at regressed: got %s, previous %s",
			archived.UpdatedAt,
			reviewed.UpdatedAt,
		)
	}
	assertAdminInquiryStatusUpdatePreservedFields(t, archived, fixture)

	missingID := fixture.ID + 1_000_000
	err = updater.UpdateStatus(
		t.Context(),
		missingID,
		inquiryStatusReviewed,
	)
	if !errors.Is(err, errAdminInquiryNotFound) {
		t.Fatalf("missing status update error: got %v, want not-found sentinel", err)
	}

	var inquiryCount int
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT COUNT(*) FROM public.inquiries`,
	).Scan(&inquiryCount); err != nil {
		t.Fatal("count synthetic inquiries after status updates")
	}
	if inquiryCount != 1 {
		t.Errorf("inquiry count: got %d, want 1", inquiryCount)
	}
}

// assertAdminInquiryStatusUpdatePreservedFields proves the focused mutation did
// not alter the visitor payload, discipline, identity, or creation time. Status
// and updated_at are intentionally compared by the caller because they change
// across successful transitions.
func assertAdminInquiryStatusUpdatePreservedFields(
	t *testing.T,
	actual adminInquiryDetail,
	original adminInquiryDetail,
) {
	t.Helper()

	if actual.ID != original.ID ||
		actual.Name != original.Name ||
		actual.Email != original.Email ||
		actual.Discipline != original.Discipline ||
		actual.Message != original.Message ||
		!actual.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf(
			"status update altered non-status fields: got %#v, original %#v",
			actual,
			original,
		)
	}
}
