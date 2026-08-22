package main

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL integration constants describe the inquiry default and the stable
// constraint diagnostics shared by the real inquiry and migration-cycle tests.
const (
	// postgresInquiryDefaultStatus is the initial review-queue state supplied by
	// PostgreSQL when the repository deliberately omits the status column.
	postgresInquiryDefaultStatus = "new"
	// postgresCheckViolationCode is PostgreSQL's stable SQLSTATE for any failed
	// CHECK constraint owned by the embedded migration catalog.
	postgresCheckViolationCode = "23514"
	// postgresNotNullViolationCode is PostgreSQL's stable SQLSTATE for a missing
	// value in the required submission-key column.
	postgresNotNullViolationCode = "23502"
	// postgresUniqueViolationCode is PostgreSQL's stable SQLSTATE for a duplicate
	// value protected by a named migration-owned uniqueness constraint.
	postgresUniqueViolationCode = "23505"
	// postgresInquiryKeyLengthConstraint names the migration-owned key-width
	// constraint whose diagnostics are asserted below.
	postgresInquiryKeyLengthConstraint = "inquiries_submission_key_length"
	// postgresInquiryKeyUniqueConstraint names the migration-owned uniqueness
	// constraint whose diagnostics are asserted below.
	postgresInquiryKeyUniqueConstraint = "inquiries_submission_key_unique"
)

// postgresInquiryRecord contains every persisted value needed to prove that a
// repository write and an idempotent replay preserve the original inquiry.
type postgresInquiryRecord struct {
	// ID is PostgreSQL's generated internal identity for the stored inquiry.
	ID int64
	// SubmissionKey is the exact opaque 32-byte idempotency key.
	SubmissionKey []byte
	// Name is the normalized test visitor name written by the repository.
	Name string
	// Email is the normalized test reply address written by the repository.
	Email string
	// Discipline is the supported machine value written by the repository.
	Discipline string
	// Message is the normalized project summary written by the repository.
	Message string
	// Status is PostgreSQL's default review-queue state.
	Status string
	// CreatedAt records when PostgreSQL accepted the original inquiry.
	CreatedAt time.Time
	// UpdatedAt initially matches CreatedAt because no update has occurred.
	UpdatedAt time.Time
}

// TestPostgresInquiryRepositoryIntegration exercises the real PostgreSQL
// boundary that an executor stub cannot prove: embedded migrations, bytea
// mapping, database defaults, idempotent replay behavior, and key constraints.
//
// The test reuses the Stage 13 two-part opt-in and `_test` database-name guard.
// It therefore skips during ordinary `go test ./...` runs and never falls back
// to the development or production DATABASE_URL value.
func TestPostgresInquiryRepositoryIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed inquiry integration database: %v", err)
	}
	// Test cleanup executes last-in, first-out. Register pool closure before the
	// schema cleanup so the latter can still use this live connection.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close inquiry integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	applyRepositoryIntegrationMigrations(t, database)

	repository, err := newPostgresInquiryRepository(database)
	if err != nil {
		t.Fatalf("create PostgreSQL inquiry repository: %v", err)
	}

	// These readable fixed-width test keys make an unexpected mapping easy to
	// diagnose while containing no visitor data or credentials.
	firstKey := []byte("stage14-first-key-00000000000000")
	secondKey := []byte("stage14-second-key-0000000000000")
	if len(firstKey) != inquirySubmissionTokenByteLength ||
		len(secondKey) != inquirySubmissionTokenByteLength {
		t.Fatal("integration fixture submission keys must each contain 32 bytes")
	}

	firstSubmission := inquirySubmission{
		SubmissionKey: firstKey,
		Name:          "Stage Fourteen Visitor",
		Email:         "stage14.visitor@example.com",
		Discipline:    "architecture-design",
		Message:       "A normalized inquiry persisted by the live repository test.",
	}
	serverTimeBefore := readPostgresCurrentTime(t, database)
	createResult, err := repository.Create(t.Context(), firstSubmission)
	if err != nil {
		t.Fatalf("create first live inquiry: %v", err)
	}
	if createResult != inquiryCreateResultCreated {
		t.Fatalf(
			"first repository result: got %d, want created",
			createResult,
		)
	}
	serverTimeAfter := readPostgresCurrentTime(t, database)

	originalRecord := readPostgresInquiryByKey(t, database, firstKey)
	assertPostgresInquiryMatchesSubmission(
		t,
		originalRecord,
		firstSubmission,
	)
	if originalRecord.Status != postgresInquiryDefaultStatus {
		t.Errorf(
			"default inquiry status: got %q, want %q",
			originalRecord.Status,
			postgresInquiryDefaultStatus,
		)
	}
	if originalRecord.CreatedAt.Before(serverTimeBefore) ||
		originalRecord.CreatedAt.After(serverTimeAfter) {
		t.Errorf(
			"created_at %s is outside PostgreSQL insert interval %s..%s",
			originalRecord.CreatedAt,
			serverTimeBefore,
			serverTimeAfter,
		)
	}
	if !originalRecord.UpdatedAt.Equal(originalRecord.CreatedAt) {
		t.Errorf(
			"initial updated_at %s does not equal created_at %s",
			originalRecord.UpdatedAt,
			originalRecord.CreatedAt,
		)
	}

	// Reusing the first key with the same normalized payload is a genuine browser
	// retry. It must report replay and retain the original row unchanged.
	createResult, err = repository.Create(t.Context(), firstSubmission)
	if err != nil {
		t.Fatalf("replay first live inquiry: %v", err)
	}
	if createResult != inquiryCreateResultReplay {
		t.Fatalf(
			"replayed repository result: got %d, want replay",
			createResult,
		)
	}

	replayedRecord := readPostgresInquiryByKey(t, database, firstKey)
	assertPostgresInquiryRecordsEqual(t, replayedRecord, originalRecord)
	if rowCount := countPostgresInquiries(t, database); rowCount != 1 {
		t.Errorf("row count after replay: got %d, want 1", rowCount)
	}

	// A hidden form key remains untrusted. Deliberately changing the visitor's
	// name and email under the same key must return the safe repository failure,
	// retain the original row, and never claim that the changed data was saved.
	collidingSubmission := inquirySubmission{
		SubmissionKey: firstKey,
		Name:          "Replacement Name Must Not Persist",
		Email:         "replacement.must.not.persist@example.com",
		Discipline:    "products",
		Message:       "This collision payload must not replace the original row.",
	}
	createResult, err = repository.Create(t.Context(), collidingSubmission)
	if !errors.Is(err, errInquirySubmissionConflict) {
		t.Fatalf(
			"changed-payload collision error: got %v, want conflict sentinel",
			err,
		)
	}
	if createResult != 0 {
		t.Errorf("changed-payload collision result: got %d, want zero", createResult)
	}
	collisionRecord := readPostgresInquiryByKey(t, database, firstKey)
	assertPostgresInquiryRecordsEqual(t, collisionRecord, originalRecord)
	if rowCount := countPostgresInquiries(t, database); rowCount != 1 {
		t.Errorf("row count after changed-payload collision: got %d, want 1", rowCount)
	}

	// A different valid key represents a genuinely new submission, even when it
	// is written through the same repository and connection pool.
	secondSubmission := inquirySubmission{
		SubmissionKey: secondKey,
		Name:          "Second Stage Fourteen Visitor",
		Email:         "stage14.second@example.com",
		Discipline:    "interior-design",
		Message:       "A second inquiry with a distinct idempotency key.",
	}
	createResult, err = repository.Create(t.Context(), secondSubmission)
	if err != nil {
		t.Fatalf("create second live inquiry: %v", err)
	}
	if createResult != inquiryCreateResultCreated {
		t.Fatalf(
			"distinct-key repository result: got %d, want created",
			createResult,
		)
	}
	secondRecord := readPostgresInquiryByKey(t, database, secondKey)
	assertPostgresInquiryMatchesSubmission(t, secondRecord, secondSubmission)
	if secondRecord.ID == originalRecord.ID {
		t.Error("distinct submission key reused the first inquiry identity")
	}
	if rowCount := countPostgresInquiries(t, database); rowCount != 2 {
		t.Errorf("row count after distinct key: got %d, want 2", rowCount)
	}

	assertPostgresInquiryKeyConstraints(t, database, firstSubmission)
	if rowCount := countPostgresInquiries(t, database); rowCount != 2 {
		t.Errorf("row count after rejected keys: got %d, want 2", rowCount)
	}
}

// applyRepositoryIntegrationMigrations applies the complete embedded catalog and
// proves a repeated run is idempotent before repository behavior is exercised.
func applyRepositoryIntegrationMigrations(t *testing.T, database *sql.DB) {
	t.Helper()

	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		t.Fatalf("load repository integration migration catalog: %v", err)
	}
	if len(catalog) != 9 ||
		catalog[0].Version != 1 ||
		catalog[1].Version != 2 ||
		catalog[2].Version != 3 ||
		catalog[3].Version != 4 ||
		catalog[4].Version != 5 ||
		catalog[5].Version != 6 ||
		catalog[6].Version != 7 ||
		catalog[7].Version != 8 ||
		catalog[8].Version != 9 {
		t.Fatalf(
			"migration catalog: got %#v, want versions 1 through 9",
			catalog,
		)
	}

	runner, err := newMigrationRunner(database, catalog)
	if err != nil {
		t.Fatalf("create repository integration migration runner: %v", err)
	}
	applied, err := runner.Up(t.Context())
	if err != nil {
		t.Fatalf("apply repository integration migrations: %v", err)
	}
	if len(applied) != len(catalog) {
		t.Fatalf(
			"applied migration count: got %d, want %d",
			len(applied),
			len(catalog),
		)
	}
	for index, definition := range applied {
		if definition.Version != catalog[index].Version ||
			definition.Name != catalog[index].Name {
			t.Errorf(
				"applied migration %d: got %06d_%s, want %06d_%s",
				index,
				definition.Version,
				definition.Name,
				catalog[index].Version,
				catalog[index].Name,
			)
		}
	}

	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("repeat repository integration migrations: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf(
			"repeated migration run applied %d migration(s), want zero",
			len(applied),
		)
	}
}

// readPostgresCurrentTime reads time from the database server so timestamp
// assertions do not depend on the Go process and PostgreSQL sharing a clock.
func readPostgresCurrentTime(t *testing.T, database *sql.DB) time.Time {
	t.Helper()

	var currentTime time.Time
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT CURRENT_TIMESTAMP",
	).Scan(&currentTime); err != nil {
		t.Fatal("read PostgreSQL current time")
	}

	return currentTime
}

// readPostgresInquiryByKey loads one exact inquiry without interpolating its
// opaque key into trusted SQL text.
func readPostgresInquiryByKey(
	t *testing.T,
	database *sql.DB,
	submissionKey []byte,
) postgresInquiryRecord {
	t.Helper()

	var record postgresInquiryRecord
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    id,
    submission_key,
    name,
    email,
    discipline,
    message,
    status,
    created_at,
    updated_at
FROM public.inquiries
WHERE submission_key = $1`,
		submissionKey,
	).Scan(
		&record.ID,
		&record.SubmissionKey,
		&record.Name,
		&record.Email,
		&record.Discipline,
		&record.Message,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		t.Fatal("read persisted PostgreSQL inquiry")
	}

	return record
}

// assertPostgresInquiryMatchesSubmission compares every repository-supplied
// value, including the normalized test user's name and email, with PostgreSQL.
func assertPostgresInquiryMatchesSubmission(
	t *testing.T,
	record postgresInquiryRecord,
	submission inquirySubmission,
) {
	t.Helper()

	if !bytes.Equal(record.SubmissionKey, submission.SubmissionKey) {
		t.Error("stored submission key does not match repository input")
	}
	if record.Name != submission.Name {
		t.Errorf("stored user name: got %q, want %q", record.Name, submission.Name)
	}
	if record.Email != submission.Email {
		t.Errorf("stored email: got %q, want %q", record.Email, submission.Email)
	}
	if record.Discipline != submission.Discipline {
		t.Errorf(
			"stored discipline: got %q, want %q",
			record.Discipline,
			submission.Discipline,
		)
	}
	if record.Message != submission.Message {
		t.Errorf("stored message: got %q, want %q", record.Message, submission.Message)
	}
}

// assertPostgresInquiryRecordsEqual proves a replay retained the entire
// original row, including its generated identity, defaults, and timestamps.
func assertPostgresInquiryRecordsEqual(
	t *testing.T,
	actual postgresInquiryRecord,
	expected postgresInquiryRecord,
) {
	t.Helper()

	if actual.ID != expected.ID ||
		!bytes.Equal(actual.SubmissionKey, expected.SubmissionKey) ||
		actual.Name != expected.Name ||
		actual.Email != expected.Email ||
		actual.Discipline != expected.Discipline ||
		actual.Message != expected.Message ||
		actual.Status != expected.Status ||
		!actual.CreatedAt.Equal(expected.CreatedAt) ||
		!actual.UpdatedAt.Equal(expected.UpdatedAt) {
		t.Errorf(
			"replayed row changed:\n got  %#v\n want %#v",
			actual,
			expected,
		)
	}
}

// countPostgresInquiries returns the total rows in the disposable integration
// table so replay and rejected-constraint paths can prove they added nothing.
func countPostgresInquiries(t *testing.T, database *sql.DB) int {
	t.Helper()

	var count int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.inquiries",
	).Scan(&count); err != nil {
		t.Fatal("count PostgreSQL inquiry rows")
	}

	return count
}

// assertPostgresInquiryKeyConstraints bypasses the idempotent repository SQL
// only to prove that PostgreSQL itself rejects short, missing, and duplicate
// submission keys with the constraints introduced by migration 000002.
func assertPostgresInquiryKeyConstraints(
	t *testing.T,
	database *sql.DB,
	validSubmission inquirySubmission,
) {
	t.Helper()

	const directInsertSQL = `INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message
)
VALUES ($1, $2, $3, $4, $5)`

	shortKey := validSubmission.SubmissionKey[:len(validSubmission.SubmissionKey)-1]
	_, err := database.ExecContext(
		t.Context(),
		directInsertSQL,
		shortKey,
		"Short Key Fixture",
		"short-key@example.com",
		"products",
		"This direct insert must fail its fixed-width key check.",
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresCheckViolationCode,
		postgresInquiryKeyLengthConstraint,
		"",
	)

	_, err = database.ExecContext(
		t.Context(),
		directInsertSQL,
		nil,
		"Missing Key Fixture",
		"missing-key@example.com",
		"products",
		"This direct insert must fail the required key rule.",
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresNotNullViolationCode,
		"",
		"submission_key",
	)

	_, err = database.ExecContext(
		t.Context(),
		directInsertSQL,
		validSubmission.SubmissionKey,
		"Duplicate Key Fixture",
		"duplicate-key@example.com",
		"products",
		"This direct insert must fail the unique key rule.",
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresUniqueViolationCode,
		postgresInquiryKeyUniqueConstraint,
		"",
	)
}

// assertPostgresConstraintError verifies stable SQLSTATE and schema-owned
// diagnostic identifiers without depending on locale-specific error text.
func assertPostgresConstraintError(
	t *testing.T,
	err error,
	expectedCode string,
	expectedConstraint string,
	expectedColumn string,
) {
	t.Helper()

	if err == nil {
		t.Fatalf("constraint operation succeeded; want SQLSTATE %s", expectedCode)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("constraint error type: got %T, want *pgconn.PgError", err)
	}
	if postgresError.Code != expectedCode {
		t.Errorf(
			"constraint SQLSTATE: got %q, want %q",
			postgresError.Code,
			expectedCode,
		)
	}
	if expectedConstraint != "" &&
		postgresError.ConstraintName != expectedConstraint {
		t.Errorf(
			"constraint name: got %q, want %q",
			postgresError.ConstraintName,
			expectedConstraint,
		)
	}
	if expectedColumn != "" && postgresError.ColumnName != expectedColumn {
		t.Errorf(
			"constraint column: got %q, want %q",
			postgresError.ColumnName,
			expectedColumn,
		)
	}
}
