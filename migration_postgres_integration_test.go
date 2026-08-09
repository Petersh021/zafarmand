package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Migration integration constants keep the destructive opt-in, expected
// acknowledgement, and cleanup deadline stable between test code and docs.
const (
	// migrationIntegrationURLEnvironmentName is intentionally different from
	// DATABASE_URL so a normal development or production database is never used
	// implicitly by the destructive integration test.
	migrationIntegrationURLEnvironmentName = "ZAFARMAND_TEST_DATABASE_URL"
	// migrationIntegrationConfirmationEnvironmentName provides a second,
	// independent opt-in before the test creates and drops public-schema tables.
	migrationIntegrationConfirmationEnvironmentName = "ZAFARMAND_TEST_DATABASE_CONFIRM"
	// migrationIntegrationConfirmationValue documents the exact disposable-data
	// acknowledgement required by the integration test and development guide.
	migrationIntegrationConfirmationValue = "stage13-disposable-database"
	// migrationIntegrationCleanupTimeout bounds best-effort schema cleanup even
	// when the test's own context has already been cancelled.
	migrationIntegrationCleanupTimeout = 15 * time.Second
	// migrationIntegrationPasswordHash is deliberately an inert test verifier,
	// not a usable password or production credential.
	migrationIntegrationPasswordHash = "integration-hash-not-a-credential"
)

// TestMigrationRunnerPostgresCycle exercises behavior that mocks cannot prove:
// PostgreSQL DDL, pgx multi-statement execution, advisory locking, ledger
// persistence, idempotency, rollback, and transaction atomicity.
//
// The test skips unless a separately confirmed disposable `_test` database is
// supplied. It never falls back to DATABASE_URL and refuses to begin if its
// server-side database name or prerequisite relation state is unsafe.
func TestMigrationRunnerPostgresCycle(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed migration integration database: %v", err)
	}
	// Cleanup functions execute last-in, first-out. Register pool closure first
	// so schema cleanup can still use the connection before it is released.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close migration integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		t.Fatalf("load embedded migration catalog: %v", err)
	}
	runner, err := newMigrationRunner(database, catalog)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}

	// Holding one session proves another pinned connection cannot acquire the
	// same application advisory lock concurrently.
	lockedSession, err := runner.openSession(t.Context())
	if err != nil {
		t.Fatalf("open first locked migration session: %v", err)
	}
	if _, err := runner.openSession(t.Context()); !errors.Is(err, errMigrationRunnerBusy) {
		_ = lockedSession.Close()
		t.Fatalf("second migration session: got %v, want busy", err)
	}
	if err := lockedSession.Close(); err != nil {
		t.Fatalf("close first locked migration session: %v", err)
	}

	// A fresh database exposes the complete embedded history in order without
	// claiming that Stages 13, 14, or 15 have already been applied.
	assertMigrationIntegrationStatus(t, runner, false, false, false)

	applied, err := runner.Up(t.Context())
	if err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}
	if len(applied) != 3 ||
		applied[0].Version != 1 ||
		applied[1].Version != 2 ||
		applied[2].Version != 3 {
		t.Fatalf("applied migrations: got %#v, want versions 1, 2, and 3", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)
	assertMigrationIntegrationTableExists(t, database, "admin_users", true)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", true)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_user_id_idx",
		true,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_expires_at_idx",
		true,
	)
	assertMigrationIntegrationColumnExists(
		t,
		database,
		"inquiries",
		"submission_key",
		true,
	)

	// Repeating up must not execute or report either already-applied migration.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("repeat embedded migrations: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("repeated up applied %d migration(s), want zero", len(applied))
	}

	// Two rows created under version 000002 become representative legacy rows
	// when that migration is rolled back. Distinct fixed-width keys satisfy the
	// current schema while the contact values give the rollback a visible data-
	// preservation contract to prove.
	firstInquiryID := insertMigrationIntegrationInquiry(
		t,
		database,
		0x11,
		"Stage Fourteen Visitor",
		"visitor@example.test",
	)
	secondInquiryID := insertMigrationIntegrationInquiry(
		t,
		database,
		0x22,
		"Second Stage Visitor",
		"second@example.test",
	)
	adminUserID := insertMigrationIntegrationAdminAccess(
		t,
		database,
		"owner@example.test",
		"owner",
		0x33,
		0x44,
	)
	assertMigrationIntegrationAdminAccess(
		t,
		database,
		adminUserID,
		"owner@example.test",
		"owner",
		0x33,
		0x44,
	)

	// Down intentionally rolls back only the newest applied migration. Version
	// 000003 owns both admin tables, so they disappear in dependency order while
	// the inquiry schema and both visitor records remain completely unchanged.
	rolledBack, err := runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 15 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 3 {
		t.Fatalf("rolled-back migration: got %#v, want version 3", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, false)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", false)
	assertMigrationIntegrationTableExists(t, database, "admin_users", false)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_user_id_idx",
		false,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_expires_at_idx",
		false,
	)
	assertMigrationIntegrationColumnExists(
		t,
		database,
		"inquiries",
		"submission_key",
		true,
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		firstInquiryID,
		"Stage Fourteen Visitor",
		"visitor@example.test",
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		secondInquiryID,
		"Second Stage Visitor",
		"second@example.test",
	)
	assertMigrationIntegrationInquiryKey(t, database, firstInquiryID, 0x11)
	assertMigrationIntegrationInquiryKey(t, database, secondInquiryID, 0x22)

	// A second one-step rollback reaches version 000002. It removes only the
	// idempotency key while version 000001 and both inquiry rows remain.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 14 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 2 {
		t.Fatalf("rolled-back migration: got %#v, want version 2", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, false, false)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)
	assertMigrationIntegrationColumnExists(
		t,
		database,
		"inquiries",
		"submission_key",
		false,
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		firstInquiryID,
		"Stage Fourteen Visitor",
		"visitor@example.test",
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		secondInquiryID,
		"Second Stage Visitor",
		"second@example.test",
	)

	// Reapplying with version 000001 still present plans versions 000002 and
	// 000003 in order. The key backfill preserves inquiry contact data, while the
	// admin tables and their indexes return empty after their deliberate drop.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("reapply Stage 14 and Stage 15 migrations: %v", err)
	}
	if len(applied) != 2 ||
		applied[0].Version != 2 ||
		applied[1].Version != 3 {
		t.Fatalf("reapplied migrations: got %#v, want versions 2 and 3", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true)
	assertMigrationIntegrationBackfilledKeys(t, database, 2)
	assertMigrationIntegrationTableExists(t, database, "admin_users", true)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", true)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_user_id_idx",
		true,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"admin_sessions_expires_at_idx",
		true,
	)
	assertMigrationIntegrationTableRowCount(t, database, "admin_users", 0)
	assertMigrationIntegrationTableRowCount(t, database, "admin_sessions", 0)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		firstInquiryID,
		"Stage Fourteen Visitor",
		"visitor@example.test",
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		secondInquiryID,
		"Second Stage Visitor",
		"second@example.test",
	)

	// Recreated tables retain the same foreign-key contract. Removing the user
	// revokes every owned session through the narrowly scoped delete cascade.
	reappliedAdminUserID := insertMigrationIntegrationAdminAccess(
		t,
		database,
		"editor@example.test",
		"editor",
		0x55,
		0x66,
	)
	assertMigrationIntegrationAdminAccess(
		t,
		database,
		reappliedAdminUserID,
		"editor@example.test",
		"editor",
		0x55,
		0x66,
	)
	deleteMigrationIntegrationAdminUser(
		t,
		database,
		reappliedAdminUserID,
	)
	assertMigrationIntegrationTableRowCount(t, database, "admin_sessions", 0)

	// A synthetic version 000004 fails after executing DDL. PostgreSQL must roll
	// back that DDL while retaining all three applied embedded versions.
	assertMigrationIntegrationAtomicFailure(t, database, catalog)
	assertMigrationIntegrationStatus(t, runner, true, true, true)

	// Three explicit rollback calls prove the runner reverses the catalog one
	// migration at a time: admin access, the inquiry key, then the inquiry table.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 15 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 3 {
		t.Fatalf("rolled-back migration: got %#v, want version 3", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, false)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", false)
	assertMigrationIntegrationTableExists(t, database, "admin_users", false)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 14 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 2 {
		t.Fatalf("rolled-back migration: got %#v, want version 2", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, false, false)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 13 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 1 {
		t.Fatalf("rolled-back migration: got %#v, want version 1", rolledBack)
	}
	assertMigrationIntegrationTableExists(t, database, "inquiries", false)
	assertMigrationIntegrationStatus(t, runner, false, false, false)

	if _, err := runner.Down(t.Context()); !errors.Is(err, errNoAppliedMigrations) {
		t.Fatalf("empty rollback: got %v, want no applied migrations", err)
	}

	// Reapply after the full rollback so the destructive operator cycle finishes
	// on the complete current schema before the test-owned cleanup executes.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("reapply embedded migrations after full rollback: %v", err)
	}
	if len(applied) != 3 ||
		applied[0].Version != 1 ||
		applied[1].Version != 2 ||
		applied[2].Version != 3 {
		t.Fatalf("reapplied migrations: got %#v, want versions 1, 2, and 3", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true)
}

// loadMigrationIntegrationConfig requires two explicit environment values and
// a database name ending in `_test` before returning destructive test config.
func loadMigrationIntegrationConfig(t *testing.T) databaseConfig {
	t.Helper()

	connectionString, exists := os.LookupEnv(
		migrationIntegrationURLEnvironmentName,
	)
	if !exists || strings.TrimSpace(connectionString) == "" {
		t.Skip(
			"PostgreSQL integration test requires " +
				migrationIntegrationURLEnvironmentName,
		)
	}
	if os.Getenv(
		migrationIntegrationConfirmationEnvironmentName,
	) != migrationIntegrationConfirmationValue {
		t.Fatalf(
			"set %s to the documented confirmation value before using the disposable test database",
			migrationIntegrationConfirmationEnvironmentName,
		)
	}

	parsedConfig, err := pgx.ParseConfig(connectionString)
	if err != nil {
		t.Fatal("migration integration database URL is invalid")
	}
	if !strings.HasSuffix(
		strings.ToLower(parsedConfig.Database),
		"_test",
	) {
		t.Fatal("migration integration database name must end in _test")
	}

	return databaseConfig{
		connectionString: strings.TrimSpace(connectionString),
		pingTimeout:      defaultDatabasePingTimeout,
	}
}

// requireMigrationIntegrationSchemaEmpty verifies the server-side database
// name and refuses to touch any schema that conflicts with the test's owned or
// deliberately absent relations.
func requireMigrationIntegrationSchemaEmpty(
	t *testing.T,
	database *sql.DB,
) {
	t.Helper()

	var currentDatabase string
	var inquiriesExist bool
	var adminUsersExist bool
	var adminSessionsExist bool
	var ledgerExists bool
	var atomicityProbeExists bool
	var missingProbeExists bool
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    current_database(),
    to_regclass('public.inquiries') IS NOT NULL,
    to_regclass('public.admin_users') IS NOT NULL,
    to_regclass('public.admin_sessions') IS NOT NULL,
    to_regclass('public.schema_migrations') IS NOT NULL,
    to_regclass('public.stage13_atomicity_probe') IS NOT NULL,
    to_regclass('public.stage13_intentionally_missing_table') IS NOT NULL`,
	).Scan(
		&currentDatabase,
		&inquiriesExist,
		&adminUsersExist,
		&adminSessionsExist,
		&ledgerExists,
		&atomicityProbeExists,
		&missingProbeExists,
	); err != nil {
		t.Fatal("inspect migration integration schema")
	}
	if !strings.HasSuffix(strings.ToLower(currentDatabase), "_test") {
		t.Fatal("connected migration integration database name must end in _test")
	}
	if inquiriesExist ||
		adminUsersExist ||
		adminSessionsExist ||
		ledgerExists ||
		atomicityProbeExists ||
		missingProbeExists {
		t.Fatal(
			"migration integration database contains a relation reserved " +
				"by the migration integration test",
		)
	}
}

// cleanupMigrationIntegrationSchema removes only the exact tables owned or
// probed by this test from the already-confirmed disposable database. Sessions
// precede users so cleanup respects the same foreign-key order as migration 3.
func cleanupMigrationIntegrationSchema(t *testing.T, database *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		migrationIntegrationCleanupTimeout,
	)
	defer cancel()

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Errorf("begin migration integration cleanup")
		return
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	for _, statement := range []string{
		"DROP TABLE IF EXISTS public.stage13_atomicity_probe",
		"DROP TABLE IF EXISTS public.admin_sessions",
		"DROP TABLE IF EXISTS public.admin_users",
		"DROP TABLE IF EXISTS public.inquiries",
		"DROP TABLE IF EXISTS public.schema_migrations",
	} {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			t.Errorf("clean migration integration schema")
			return
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Errorf("commit migration integration cleanup")
	}
}

// assertMigrationIntegrationStatus checks every embedded migration's ordered
// ledger state after the runner has validated the complete history.
//
// The Boolean arguments map positionally to contiguous versions beginning at
// version 000001. Keeping the expected state explicit at each call site makes
// the one-migration-at-a-time Down behavior visible in the integration cycle.
func assertMigrationIntegrationStatus(
	t *testing.T,
	runner *migrationRunner,
	expectedApplied ...bool,
) {
	t.Helper()

	statuses, err := runner.Status(t.Context())
	if err != nil {
		t.Fatalf("read migration status: %v", err)
	}
	if len(statuses) != len(expectedApplied) {
		t.Fatalf(
			"migration status count: got %d (%#v), want %d",
			len(statuses),
			statuses,
			len(expectedApplied),
		)
	}

	for index, applied := range expectedApplied {
		expectedVersion := int64(index + 1)
		if statuses[index].Migration.Version != expectedVersion ||
			statuses[index].Applied != applied {
			t.Fatalf(
				"migration status %d: got %#v, want version %d applied=%t",
				index,
				statuses[index],
				expectedVersion,
				applied,
			)
		}
	}
}

// assertMigrationIntegrationTableExists compares one owned public table with
// the expected PostgreSQL catalog state without interpolating identifiers.
func assertMigrationIntegrationTableExists(
	t *testing.T,
	database *sql.DB,
	tableName string,
	expected bool,
) {
	t.Helper()

	var exists bool
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT to_regclass($1) IS NOT NULL",
		"public."+tableName,
	).Scan(&exists); err != nil {
		t.Fatalf("inspect migration integration table %q", tableName)
	}
	if exists != expected {
		t.Errorf(
			"table %q existence: got %t, want %t",
			tableName,
			exists,
			expected,
		)
	}
}

// assertMigrationIntegrationColumnExists compares one owned public-table
// column with PostgreSQL's information schema. Table and column names remain
// query parameters so the helper never builds SQL from identifiers.
func assertMigrationIntegrationColumnExists(
	t *testing.T,
	database *sql.DB,
	tableName string,
	columnName string,
	expected bool,
) {
	t.Helper()

	var exists bool
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = $1
      AND column_name = $2
)`,
		tableName,
		columnName,
	).Scan(&exists); err != nil {
		t.Fatalf(
			"inspect migration integration column %q.%q",
			tableName,
			columnName,
		)
	}
	if exists != expected {
		t.Errorf(
			"column %q.%q existence: got %t, want %t",
			tableName,
			columnName,
			exists,
			expected,
		)
	}
}

// assertMigrationIntegrationIndexExists verifies one exact Stage 15 index in
// PostgreSQL's public schema. The name remains a query argument rather than
// becoming dynamically assembled SQL.
func assertMigrationIntegrationIndexExists(
	t *testing.T,
	database *sql.DB,
	indexName string,
	expected bool,
) {
	t.Helper()

	var exists bool
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT EXISTS (
    SELECT 1
    FROM pg_indexes
    WHERE schemaname = 'public'
      AND indexname = $1
)`,
		indexName,
	).Scan(&exists); err != nil {
		t.Fatalf("inspect migration integration index %q", indexName)
	}
	if exists != expected {
		t.Errorf(
			"index %q existence: got %t, want %t",
			indexName,
			exists,
			expected,
		)
	}
}

// assertMigrationIntegrationTableRowCount reads only one of the two exact
// Stage 15 relations used at call sites. The closed switch avoids interpolating
// even a test-provided identifier into SQL.
func assertMigrationIntegrationTableRowCount(
	t *testing.T,
	database *sql.DB,
	tableName string,
	expected int,
) {
	t.Helper()

	var query string
	switch tableName {
	case "admin_users":
		query = "SELECT COUNT(*) FROM public.admin_users"
	case "admin_sessions":
		query = "SELECT COUNT(*) FROM public.admin_sessions"
	default:
		t.Fatalf("unsupported migration integration row-count table %q", tableName)
	}

	var count int
	if err := database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("count migration integration table %q", tableName)
	}
	if count != expected {
		t.Errorf("table %q rows: got %d, want %d", tableName, count, expected)
	}
}

// insertMigrationIntegrationAdminAccess creates one normalized administrator
// and one hash-only session. Repeated fixture bytes are readable in diagnostics
// while still proving PostgreSQL's exact bytea mapping and length constraints.
func insertMigrationIntegrationAdminAccess(
	t *testing.T,
	database *sql.DB,
	email string,
	role string,
	tokenByte byte,
	csrfByte byte,
) int64 {
	t.Helper()

	var userID int64
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.admin_users (email, password_hash, role)
VALUES ($1, $2, $3)
RETURNING id`,
		email,
		migrationIntegrationPasswordHash,
		role,
	).Scan(&userID); err != nil {
		t.Fatal("insert migration integration administrator")
	}

	tokenHash := make([]byte, 32)
	csrfTokenHash := make([]byte, 32)
	for index := range tokenHash {
		tokenHash[index] = tokenByte
		csrfTokenHash[index] = csrfByte
	}
	if _, err := database.ExecContext(
		t.Context(),
		`INSERT INTO public.admin_sessions (
    token_hash,
    user_id,
    csrf_token_hash,
    expires_at
) VALUES ($1, $2, $3, CURRENT_TIMESTAMP + INTERVAL '1 hour')`,
		tokenHash,
		userID,
		csrfTokenHash,
	); err != nil {
		t.Fatal("insert migration integration admin session")
	}

	return userID
}

// assertMigrationIntegrationAdminAccess proves normalized user values, secure
// defaults, exact token digests, and ordered session timestamps survived the
// real PostgreSQL boundary. The fixture password hash is never logged.
func assertMigrationIntegrationAdminAccess(
	t *testing.T,
	database *sql.DB,
	userID int64,
	expectedEmail string,
	expectedRole string,
	tokenByte byte,
	csrfByte byte,
) {
	t.Helper()

	var email string
	var passwordHash string
	var role string
	var active bool
	var createdAt time.Time
	var updatedAt time.Time
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT email, password_hash, role, active, created_at, updated_at
FROM public.admin_users
WHERE id = $1`,
		userID,
	).Scan(
		&email,
		&passwordHash,
		&role,
		&active,
		&createdAt,
		&updatedAt,
	); err != nil {
		t.Fatal("read migration integration administrator")
	}
	if email != expectedEmail ||
		passwordHash != migrationIntegrationPasswordHash ||
		role != expectedRole ||
		!active ||
		!updatedAt.Equal(createdAt) {
		t.Error("stored migration integration administrator violates expected values or defaults")
	}

	expectedTokenHash := make([]byte, 32)
	expectedCSRFTokenHash := make([]byte, 32)
	for index := range expectedTokenHash {
		expectedTokenHash[index] = tokenByte
		expectedCSRFTokenHash[index] = csrfByte
	}

	var tokenHash []byte
	var csrfTokenHash []byte
	var sessionUserID int64
	var sessionCreatedAt time.Time
	var expiresAt time.Time
	var revokedAt sql.NullTime
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    token_hash,
    csrf_token_hash,
    user_id,
    created_at,
    expires_at,
    revoked_at
FROM public.admin_sessions
WHERE user_id = $1`,
		userID,
	).Scan(
		&tokenHash,
		&csrfTokenHash,
		&sessionUserID,
		&sessionCreatedAt,
		&expiresAt,
		&revokedAt,
	); err != nil {
		t.Fatal("read migration integration admin session")
	}
	if !bytes.Equal(tokenHash, expectedTokenHash) ||
		!bytes.Equal(csrfTokenHash, expectedCSRFTokenHash) ||
		sessionUserID != userID ||
		!expiresAt.After(sessionCreatedAt) ||
		revokedAt.Valid {
		t.Error("stored migration integration admin session violates expected values or defaults")
	}
}

// deleteMigrationIntegrationAdminUser removes one exact fixture identity and
// verifies PostgreSQL matched it. The caller separately proves the dependent
// session disappeared through ON DELETE CASCADE.
func deleteMigrationIntegrationAdminUser(
	t *testing.T,
	database *sql.DB,
	userID int64,
) {
	t.Helper()

	result, err := database.ExecContext(
		t.Context(),
		"DELETE FROM public.admin_users WHERE id = $1",
		userID,
	)
	if err != nil {
		t.Fatal("delete migration integration administrator")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatal("read deleted migration integration administrator count")
	}
	if rowsAffected != 1 {
		t.Fatalf(
			"deleted migration integration administrators: got %d, want 1",
			rowsAffected,
		)
	}
}

// insertMigrationIntegrationInquiry creates one deterministic Stage 14 row and
// returns its database identity for data-preservation assertions. Repeating one
// byte fills the required 32-byte key without embedding visitor data into it.
func insertMigrationIntegrationInquiry(
	t *testing.T,
	database *sql.DB,
	keyByte byte,
	name string,
	email string,
) int64 {
	t.Helper()

	submissionKey := make([]byte, 32)
	for index := range submissionKey {
		submissionKey[index] = keyByte
	}

	var inquiryID int64
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message
) VALUES ($1, $2, $3, $4, $5)
RETURNING id`,
		submissionKey,
		name,
		email,
		"products",
		"Migration integration test inquiry.",
	).Scan(&inquiryID); err != nil {
		t.Fatal("insert migration integration inquiry")
	}

	return inquiryID
}

// assertMigrationIntegrationInquiryContact proves a migration preserved the
// exact stored name and email for one inquiry. Failure text deliberately omits
// the values because integration diagnostics should not normalize logging PII.
func assertMigrationIntegrationInquiryContact(
	t *testing.T,
	database *sql.DB,
	inquiryID int64,
	expectedName string,
	expectedEmail string,
) {
	t.Helper()

	var name string
	var email string
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT name, email
FROM public.inquiries
WHERE id = $1`,
		inquiryID,
	).Scan(&name, &email); err != nil {
		t.Fatal("read migration integration inquiry contact")
	}
	if name != expectedName || email != expectedEmail {
		t.Error("migration changed the stored inquiry name or email")
	}
}

// assertMigrationIntegrationInquiryKey proves Stage 15 rollback left an exact
// pre-existing inquiry key unchanged. Repeating one known fixture byte avoids
// logging or embedding a visitor-derived value.
func assertMigrationIntegrationInquiryKey(
	t *testing.T,
	database *sql.DB,
	inquiryID int64,
	keyByte byte,
) {
	t.Helper()

	expectedKey := make([]byte, 32)
	for index := range expectedKey {
		expectedKey[index] = keyByte
	}

	var actualKey []byte
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT submission_key
FROM public.inquiries
WHERE id = $1`,
		inquiryID,
	).Scan(&actualKey); err != nil {
		t.Fatal("read migration integration inquiry key")
	}
	if !bytes.Equal(actualKey, expectedKey) {
		t.Error("Stage 15 rollback changed a stored inquiry submission key")
	}
}

// assertMigrationIntegrationBackfilledKeys verifies that every preserved row
// received a required-width key and that no two rows received the same value.
// Distinctness complements the database UNIQUE constraint with evidence that
// the legacy-row UPDATE populated multiple rows successfully in PostgreSQL.
func assertMigrationIntegrationBackfilledKeys(
	t *testing.T,
	database *sql.DB,
	expectedRows int,
) {
	t.Helper()

	var rowCount int
	var validLengthCount int
	var distinctKeyCount int
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    COUNT(*),
    COUNT(*) FILTER (WHERE octet_length(submission_key) = 32),
    COUNT(DISTINCT submission_key)
FROM public.inquiries`,
	).Scan(
		&rowCount,
		&validLengthCount,
		&distinctKeyCount,
	); err != nil {
		t.Fatal("inspect backfilled migration integration keys")
	}
	if rowCount != expectedRows ||
		validLengthCount != expectedRows ||
		distinctKeyCount != expectedRows {
		t.Errorf(
			"backfilled keys: rows=%d valid-length=%d distinct=%d, want %d each",
			rowCount,
			validLengthCount,
			distinctKeyCount,
			expectedRows,
		)
	}
}

// assertMigrationIntegrationAtomicFailure proves that a later statement error
// rolls back an earlier DDL statement and does not create a ledger row.
func assertMigrationIntegrationAtomicFailure(
	t *testing.T,
	database *sql.DB,
	embeddedCatalog []migrationDefinition,
) {
	t.Helper()

	failingMigration := migrationDefinition{
		// Versions 000001 through 000003 are the real embedded history. The probe
		// therefore uses the next contiguous version instead of colliding with the
		// Stage 15 definition during catalog validation.
		Version: 4,
		Name:    "prove_atomicity",
		UpSQL: `CREATE TABLE public.stage13_atomicity_probe (id bigint);
SELECT * FROM public.stage13_intentionally_missing_table;`,
		DownSQL: "DROP TABLE public.stage13_atomicity_probe;",
	}
	if err := validateMigrationSQLSafety(failingMigration.UpSQL); err != nil {
		t.Fatalf("validate atomicity probe: %v", err)
	}
	failingMigration.Checksum = migrationChecksum(
		failingMigration.Version,
		failingMigration.Name,
		failingMigration.UpSQL,
		failingMigration.DownSQL,
	)

	probeCatalog := append(
		append([]migrationDefinition(nil), embeddedCatalog...),
		failingMigration,
	)
	probeRunner, err := newMigrationRunner(database, probeCatalog)
	if err != nil {
		t.Fatalf("create atomicity probe runner: %v", err)
	}
	if _, err := probeRunner.Up(t.Context()); err == nil {
		t.Fatal("intentionally failing multi-statement migration succeeded")
	}

	assertMigrationIntegrationTableExists(
		t,
		database,
		"stage13_atomicity_probe",
		false,
	)

	var ledgerRows int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.schema_migrations",
	).Scan(&ledgerRows); err != nil {
		t.Fatal("count migration integration ledger rows")
	}
	if ledgerRows != 3 {
		t.Errorf("ledger rows after failed migration: got %d, want 3", ledgerRows)
	}
}
