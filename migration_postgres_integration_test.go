package main

import (
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

	assertMigrationIntegrationStatus(t, runner, false)

	applied, err := runner.Up(t.Context())
	if err != nil {
		t.Fatalf("apply embedded migration: %v", err)
	}
	if len(applied) != 1 || applied[0].Version != 1 {
		t.Fatalf("applied migrations: got %#v, want version 1", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)

	// Repeating up must not execute or report an already-applied migration.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("repeat embedded migration: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("repeated up applied %d migration(s), want zero", len(applied))
	}

	assertMigrationIntegrationAtomicFailure(t, database, catalog)
	assertMigrationIntegrationStatus(t, runner, true)

	rolledBack, err := runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back embedded migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 1 {
		t.Fatalf("rolled-back migration: got %#v, want version 1", rolledBack)
	}
	assertMigrationIntegrationTableExists(t, database, "inquiries", false)
	assertMigrationIntegrationStatus(t, runner, false)

	if _, err := runner.Down(t.Context()); !errors.Is(err, errNoAppliedMigrations) {
		t.Fatalf("empty rollback: got %v, want no applied migrations", err)
	}

	// Reapply after rollback so the complete status/up/status/down/status/up
	// operator cycle finishes on the current schema before test cleanup.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("reapply embedded migration: %v", err)
	}
	if len(applied) != 1 || applied[0].Version != 1 {
		t.Fatalf("reapplied migrations: got %#v, want version 1", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true)
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
	var ledgerExists bool
	var atomicityProbeExists bool
	var missingProbeExists bool
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    current_database(),
    to_regclass('public.inquiries') IS NOT NULL,
    to_regclass('public.schema_migrations') IS NOT NULL,
    to_regclass('public.stage13_atomicity_probe') IS NOT NULL,
    to_regclass('public.stage13_intentionally_missing_table') IS NOT NULL`,
	).Scan(
		&currentDatabase,
		&inquiriesExist,
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
		ledgerExists ||
		atomicityProbeExists ||
		missingProbeExists {
		t.Fatal(
			"migration integration database contains a relation reserved " +
				"by the Stage 13 test",
		)
	}
}

// cleanupMigrationIntegrationSchema removes only the three exact tables owned
// or probed by this test from the already-confirmed disposable database.
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

// assertMigrationIntegrationStatus checks the single embedded migration's
// expected ledger state after the runner has validated the complete history.
func assertMigrationIntegrationStatus(
	t *testing.T,
	runner *migrationRunner,
	applied bool,
) {
	t.Helper()

	statuses, err := runner.Status(t.Context())
	if err != nil {
		t.Fatalf("read migration status: %v", err)
	}
	if len(statuses) != 1 ||
		statuses[0].Migration.Version != 1 ||
		statuses[0].Applied != applied {
		t.Fatalf(
			"migration status: got %#v, want version 1 applied=%t",
			statuses,
			applied,
		)
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

// assertMigrationIntegrationAtomicFailure proves that a later statement error
// rolls back an earlier DDL statement and does not create a ledger row.
func assertMigrationIntegrationAtomicFailure(
	t *testing.T,
	database *sql.DB,
	embeddedCatalog []migrationDefinition,
) {
	t.Helper()

	failingMigration := migrationDefinition{
		Version: 2,
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
	if ledgerRows != 1 {
		t.Errorf("ledger rows after failed migration: got %d, want 1", ledgerRows)
	}
}
