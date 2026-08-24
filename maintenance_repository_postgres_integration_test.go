package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// TestPostgresMaintenanceRepositoryIntegration exercises the separately
// confirmed disposable database only. It proves real PostgreSQL transaction,
// database-clock expiry, strict cutoff, row-lock, digest, replay behavior, and
// the advisory-lock boundary for concurrent Contact writes without ever
// falling back to the development DATABASE_URL.
func TestPostgresMaintenanceRepositoryIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed maintenance integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close maintenance integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	repository, err := newPostgresMaintenanceRepository(database)
	if err != nil {
		t.Fatalf("create live maintenance repository: %v", err)
	}
	contactRepository, err := newPostgresInquiryRepository(database)
	if err != nil {
		t.Fatalf("create live Contact repository: %v", err)
	}

	var currentDatabase string
	if err := database.QueryRowContext(
		t.Context(),
		maintenanceCurrentDatabaseSQL,
	).Scan(&currentDatabase); err != nil {
		t.Fatal("read maintenance integration database identity")
	}

	userID := insertMigrationIntegrationAdminAccess(
		t,
		database,
		"stage25-maintenance@example.test",
		"owner",
		0x71,
		0x72,
	)
	insertMaintenanceIntegrationSession(
		t,
		database,
		userID,
		0x73,
		0x74,
		"CURRENT_TIMESTAMP - INTERVAL '3 hours'",
		"CURRENT_TIMESTAMP - INTERVAL '2 hours'",
		"NULL",
	)
	insertMaintenanceIntegrationSession(
		t,
		database,
		userID,
		0x75,
		0x76,
		"CURRENT_TIMESTAMP - INTERVAL '1 hour'",
		"CURRENT_TIMESTAMP + INTERVAL '1 hour'",
		"CURRENT_TIMESTAMP - INTERVAL '30 minutes'",
	)

	serverNow := readPostgresCurrentTime(t, database)
	cutoff := serverNow.Add(-24 * time.Hour)
	oldArchived := insertMaintenanceIntegrationInquiry(
		t, database, 0x81, inquiryStatusArchived,
		cutoff.Add(-time.Hour),
	)
	insertMaintenanceIntegrationInquiry(
		t, database, 0x82, inquiryStatusArchived, cutoff,
	)
	insertMaintenanceIntegrationInquiry(
		t, database, 0x83, inquiryStatusArchived,
		cutoff.Add(time.Hour),
	)
	reviewedID := insertMaintenanceIntegrationInquiry(
		t, database, 0x84, inquiryStatusReviewed,
		cutoff.Add(-2*time.Hour),
	)
	targetedArchived := insertMaintenanceIntegrationInquiry(
		t, database, 0x85, inquiryStatusArchived,
		cutoff.Add(2*time.Hour),
	)

	preview, err := repository.InspectRetention(t.Context(), cutoff)
	if err != nil {
		t.Fatalf("inspect live retention: %v", err)
	}
	if preview != (maintenanceRetentionResult{1, 1}) {
		t.Errorf("initial retention preview: got %#v, want 1/1", preview)
	}

	_, err = repository.ApplyRetention(
		t.Context(), cutoff, currentDatabase+"_wrong",
	)
	if !errors.Is(err, errMaintenanceDatabaseConfirmationMismatch) {
		t.Fatalf("wrong database confirmation: got %v", err)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "admin_sessions",
	); got != 3 {
		t.Errorf("sessions after refused apply: got %d, want 3", got)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "inquiries",
	); got != 5 {
		t.Errorf("inquiries after refused apply: got %d, want 5", got)
	}

	applied, err := repository.ApplyRetention(
		t.Context(), cutoff, currentDatabase,
	)
	if err != nil {
		t.Fatalf("apply live retention: %v", err)
	}
	if applied != (maintenanceRetentionResult{1, 1}) {
		t.Errorf("applied retention result: got %#v, want 1/1", applied)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "admin_sessions",
	); got != 2 {
		t.Errorf("retained active/revoked sessions: got %d, want 2", got)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "inquiries",
	); got != 4 {
		t.Errorf("retained inquiry rows: got %d, want 4", got)
	}
	assertMaintenanceIntegrationTombstone(
		t,
		database,
		oldArchived.submissionKey,
	)

	createResult, err := contactRepository.Create(
		t.Context(),
		oldArchived.submission,
	)
	if !errors.Is(err, errInquirySubmissionConflict) || createResult != 0 {
		t.Fatalf(
			"purged-key replay: result=%d error=%v, want safe conflict",
			createResult,
			err,
		)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "inquiries",
	); got != 4 {
		t.Errorf("purged-key replay recreated inquiry: got %d rows", got)
	}

	repeated, err := repository.ApplyRetention(
		t.Context(), cutoff, currentDatabase,
	)
	if err != nil || repeated != (maintenanceRetentionResult{}) {
		t.Fatalf("idempotent retention apply: result=%#v error=%v", repeated, err)
	}

	notArchived, err := repository.PurgeInquiry(
		t.Context(), reviewedID.id, currentDatabase,
	)
	if err != nil || notArchived.Purged {
		t.Fatalf("reviewed targeted purge: result=%#v error=%v", notArchived, err)
	}
	targeted, err := repository.PurgeInquiry(
		t.Context(), targetedArchived.id, currentDatabase,
	)
	if err != nil || !targeted.Purged {
		t.Fatalf("archived targeted purge: result=%#v error=%v", targeted, err)
	}
	assertMaintenanceIntegrationTombstone(
		t,
		database,
		targetedArchived.submissionKey,
	)
	repeatedTarget, err := repository.PurgeInquiry(
		t.Context(), targetedArchived.id, currentDatabase,
	)
	if err != nil || repeatedTarget.Purged {
		t.Fatalf("repeated targeted purge: result=%#v error=%v", repeatedTarget, err)
	}
	if got := countMaintenanceIntegrationRows(
		t, database, "inquiries",
	); got != 3 {
		t.Errorf("final retained inquiries: got %d, want 3", got)
	}

	assertContactWaitsForCommittedRetentionTombstone(
		t,
		database,
		contactRepository,
		serverNow,
	)
}

// assertContactWaitsForCommittedRetentionTombstone reproduces the critical
// cross-transaction ordering: a purge has deleted an archived row and written
// its tombstone but has not committed when a stale Contact POST begins. The
// waiting shared advisory lock proves the Contact INSERT statement has not yet
// taken its READ COMMITTED snapshot; after the exclusive transaction commits,
// that INSERT must see the tombstone and refuse to recreate personal data.
func assertContactWaitsForCommittedRetentionTombstone(
	t *testing.T,
	database *sql.DB,
	contactRepository *postgresInquiryRepository,
	updatedAt time.Time,
) {
	t.Helper()
	fixture := insertMaintenanceIntegrationInquiry(
		t,
		database,
		0x91,
		inquiryStatusArchived,
		updatedAt,
	)

	maintenanceTransaction, err := database.BeginTx(
		t.Context(),
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		t.Fatal("begin concurrent retention integration transaction")
	}
	defer func() { _ = maintenanceTransaction.Rollback() }()
	if _, err := maintenanceTransaction.ExecContext(
		t.Context(),
		acquireInquiryRetentionExclusiveLockSQL,
		inquiryRetentionAdvisoryLockID,
	); err != nil {
		t.Fatal("acquire concurrent retention integration lock")
	}

	submissionKeyHash := sha256.Sum256(fixture.submissionKey)
	if _, err := maintenanceTransaction.ExecContext(
		t.Context(),
		`INSERT INTO public.inquiry_submission_tombstones (
    submission_key_hash
) VALUES ($1)`,
		submissionKeyHash[:],
	); err != nil {
		t.Fatal("insert concurrent retention integration tombstone")
	}
	if _, err := maintenanceTransaction.ExecContext(
		t.Context(),
		"DELETE FROM public.inquiries WHERE id = $1",
		fixture.id,
	); err != nil {
		t.Fatal("delete concurrent retention integration inquiry")
	}

	type createOutcome struct {
		result inquiryCreateResult
		err    error
	}
	createContext, cancelCreate := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelCreate()
	createOutcomes := make(chan createOutcome, 1)
	go func() {
		result, err := contactRepository.Create(
			createContext,
			fixture.submission,
		)
		createOutcomes <- createOutcome{result: result, err: err}
	}()

	// Wait for PostgreSQL itself to report the Contact transaction's ungranted
	// shared advisory lock. This is deterministic evidence that the goroutine is
	// blocked at the required pre-INSERT boundary, not a timing-only assertion.
	lockWaitDeadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case outcome := <-createOutcomes:
			t.Fatalf(
				"Contact Create returned before purge commit: result=%d error=%v",
				outcome.result,
				outcome.err,
			)
		default:
		}

		var waitingSharedLocks int
		if err := database.QueryRowContext(
			t.Context(),
			`SELECT COUNT(*)
FROM pg_catalog.pg_locks
WHERE locktype = 'advisory'
  AND database = (
      SELECT oid
      FROM pg_catalog.pg_database
      WHERE datname = current_database()
  )
  AND mode = 'ShareLock'
  AND classid::bigint = (($1::bigint >> 32) & 4294967295)
  AND objid::bigint = ($1::bigint & 4294967295)
  AND objsubid = 1
  AND granted = FALSE`,
			inquiryRetentionAdvisoryLockID,
		).Scan(&waitingSharedLocks); err != nil {
			t.Fatal("inspect concurrent retention integration lock wait")
		}
		if waitingSharedLocks > 0 {
			break
		}
		if time.Now().After(lockWaitDeadline) {
			t.Fatal("Contact Create did not wait on the shared retention lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := maintenanceTransaction.Commit(); err != nil {
		t.Fatal("commit concurrent retention integration transaction")
	}
	select {
	case outcome := <-createOutcomes:
		if !errors.Is(outcome.err, errInquirySubmissionConflict) ||
			outcome.result != 0 {
			t.Fatalf(
				"Contact after committed purge: result=%d error=%v",
				outcome.result,
				outcome.err,
			)
		}
	case <-createContext.Done():
		t.Fatal("Contact Create did not resume after the purge committed")
	}

	var recreatedRows int64
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.inquiries WHERE submission_key = $1",
		fixture.submissionKey,
	).Scan(&recreatedRows); err != nil {
		t.Fatal("count concurrently purged inquiry key")
	}
	if recreatedRows != 0 {
		t.Errorf("concurrent Contact replay recreated %d personal row(s)", recreatedRows)
	}
	assertMaintenanceIntegrationTombstone(t, database, fixture.submissionKey)
}

// maintenanceIntegrationInquiry retains one synthetic row's coordinates so
// tests can replay its key without rereading personal columns from PostgreSQL.
type maintenanceIntegrationInquiry struct {
	id            int64
	submissionKey []byte
	submission    inquirySubmission
}

// insertMaintenanceIntegrationInquiry creates one explicitly synthetic row at
// a caller-chosen lifecycle state and timestamp.
func insertMaintenanceIntegrationInquiry(
	t *testing.T,
	database *sql.DB,
	keyByte byte,
	status inquiryStatus,
	updatedAt time.Time,
) maintenanceIntegrationInquiry {
	t.Helper()
	key := bytes.Repeat([]byte{keyByte}, inquirySubmissionTokenByteLength)
	submission := inquirySubmission{
		SubmissionKey: key,
		Name:          "Synthetic Stage Twenty Five Visitor",
		Email:         "stage25-retention@example.test",
		Discipline:    "products",
		Message:       "Synthetic retention integration data only.",
	}
	var id int64
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message,
    status,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING id`,
		submission.SubmissionKey,
		submission.Name,
		submission.Email,
		submission.Discipline,
		submission.Message,
		string(status),
		updatedAt,
	).Scan(&id); err != nil {
		t.Fatal("insert synthetic maintenance inquiry")
	}

	return maintenanceIntegrationInquiry{
		id:            id,
		submissionKey: key,
		submission:    submission,
	}
}

// insertMaintenanceIntegrationSession creates one synthetic session whose
// database-clock expressions exercise expiry and revocation independently.
func insertMaintenanceIntegrationSession(
	t *testing.T,
	database *sql.DB,
	userID int64,
	tokenByte byte,
	csrfByte byte,
	createdExpression string,
	expiresExpression string,
	revokedExpression string,
) {
	t.Helper()
	// Expressions are selected only from fixed literals at call sites. They are
	// kept out of production SQL and contain no operator or test input.
	query := `INSERT INTO public.admin_sessions (
    token_hash,
    user_id,
    csrf_token_hash,
    created_at,
    expires_at,
    revoked_at
) VALUES ($1, $2, $3, ` + createdExpression + `, ` +
		expiresExpression + `, ` + revokedExpression + `)`
	if _, err := database.ExecContext(
		t.Context(),
		query,
		bytes.Repeat([]byte{tokenByte}, adminSessionHashBytes),
		userID,
		bytes.Repeat([]byte{csrfByte}, adminSessionHashBytes),
	); err != nil {
		t.Fatal("insert synthetic maintenance session")
	}
}

// countMaintenanceIntegrationRows accepts only a closed fixture-table set and
// returns aggregate evidence without reading private row values.
func countMaintenanceIntegrationRows(
	t *testing.T,
	database *sql.DB,
	table string,
) int64 {
	t.Helper()
	query := ""
	switch table {
	case "admin_sessions":
		query = "SELECT COUNT(*) FROM public.admin_sessions"
	case "inquiries":
		query = "SELECT COUNT(*) FROM public.inquiries"
	default:
		t.Fatalf("unsupported maintenance integration relation %q", table)
	}
	var count int64
	if err := database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatal("count maintenance integration rows")
	}

	return count
}

// assertMaintenanceIntegrationTombstone proves the one-way key digest exists
// exactly once without exposing the original submission key in output.
func assertMaintenanceIntegrationTombstone(
	t *testing.T,
	database *sql.DB,
	submissionKey []byte,
) {
	t.Helper()
	expectedHash := sha256.Sum256(submissionKey)
	var storedHash []byte
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT submission_key_hash
FROM public.inquiry_submission_tombstones
WHERE submission_key_hash = $1`,
		expectedHash[:],
	).Scan(&storedHash); err != nil {
		t.Fatal("read synthetic maintenance tombstone")
	}
	if !bytes.Equal(storedHash, expectedHash[:]) {
		t.Error("stored maintenance tombstone does not match SHA-256 digest")
	}
}
