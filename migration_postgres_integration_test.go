package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
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
	// claiming that any schema stage has already been applied.
	assertMigrationIntegrationStatus(t, runner, false, false, false, false, false, false, false)

	applied, err := runner.Up(t.Context())
	if err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}
	if len(applied) != 7 ||
		applied[0].Version != 1 ||
		applied[1].Version != 2 ||
		applied[2].Version != 3 ||
		applied[3].Version != 4 ||
		applied[4].Version != 5 ||
		applied[5].Version != 6 ||
		applied[6].Version != 7 {
		t.Fatalf("applied migrations: got %#v, want versions 1 through 7", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, true)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)
	assertMigrationIntegrationTableExists(t, database, "admin_users", true)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", true)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", true)
	assertMigrationIntegrationTableExists(t, database, "interior_projects", true)
	assertMigrationIntegrationTableExists(
		t,
		database,
		"interior_project_cover_images",
		true,
	)
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
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"products_published_order_idx",
		true,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"interior_projects_published_order_idx",
		true,
	)
	assertMigrationIntegrationColumnExists(
		t,
		database,
		"inquiries",
		"submission_key",
		true,
	)
	assertMigrationIntegrationColumnExists(t, database, "products", "description", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "material", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "dimensions", true)
	assertMigrationIntegrationTableRowCount(t, database, "interior_projects", 0)
	assertMigrationIntegrationTableRowCount(
		t,
		database,
		"interior_project_cover_images",
		0,
	)

	// Repeating up must not execute or report any already-applied migration.
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
	productID := insertMigrationIntegrationProduct(
		t,
		database,
		"stage-eighteen-product",
		"Stage Eighteen Product",
		"Furniture",
		1,
	)
	assertMigrationIntegrationProduct(
		t,
		database,
		productID,
		"stage-eighteen-product",
		"Stage Eighteen Product",
		"Furniture",
		1,
		"draft",
	)
	assertMigrationIntegrationProductVersion(t, database, productID, 1)
	assertMigrationIntegrationProductConstraints(t, database)
	assertMigrationIntegrationProductContentAndCover(t, database, productID)
	interiorProjectID := insertMigrationIntegrationInteriorProject(
		t,
		database,
		"stage-twenty-two-interior",
		"Stage Twenty Two Interior",
		"Residential",
		"Completed",
		1,
	)
	assertMigrationIntegrationInteriorProject(
		t,
		database,
		interiorProjectID,
		"stage-twenty-two-interior",
		"Stage Twenty Two Interior",
		"Residential",
		"Completed",
		1,
	)
	assertMigrationIntegrationInteriorProjectConstraintsAndCover(
		t,
		database,
		interiorProjectID,
	)

	// Down first reverses Stage 22. Version 000007 removes the Interior cover
	// before its parent table while leaving every Product, administrator, and
	// inquiry relation and row at the complete migration-6 state.
	rolledBack, err := runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 22 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 7 {
		t.Fatalf("rolled-back migration: got %#v, want version 7", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, false)
	assertMigrationIntegrationTableExists(t, database, "interior_projects", false)
	assertMigrationIntegrationTableExists(
		t,
		database,
		"interior_project_cover_images",
		false,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"interior_projects_published_order_idx",
		false,
	)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", true)
	assertMigrationIntegrationTableRowCount(t, database, "product_cover_images", 1)
	assertMigrationIntegrationColumnExists(t, database, "products", "description", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", true)
	assertMigrationIntegrationAdminAccess(
		t,
		database,
		adminUserID,
		"owner@example.test",
		"owner",
		0x33,
		0x44,
	)
	assertMigrationIntegrationInquiryContact(
		t,
		database,
		firstInquiryID,
		"Stage Fourteen Visitor",
		"visitor@example.test",
	)

	// The next rollback reverses Stage 21. Version 000006 removes only cover storage and
	// optional editorial columns while preserving the Product row, version,
	// public index, administrator tables, and inquiry records.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 21 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 6 {
		t.Fatalf("rolled-back migration: got %#v, want version 6", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, false, false)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "description", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "material", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "dimensions", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", true)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"products_published_order_idx",
		true,
	)
	assertMigrationIntegrationProduct(
		t,
		database,
		productID,
		"stage-eighteen-product",
		"Stage Eighteen Product",
		"Furniture",
		1,
		"draft",
	)
	assertMigrationIntegrationProductVersion(t, database, productID, 1)

	// The next rollback removes only migration 000005's edit revision, retaining
	// the migration-4 Product row and all unrelated relations.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 20 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 5 {
		t.Fatalf("rolled-back migration: got %#v, want version 5", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, false, false, false)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", false)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"products_published_order_idx",
		true,
	)

	// The following one-step rollback reaches version 000004. Its strict table drop
	// removes Products and the partial index while unrelated schemas remain.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 18 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 4 {
		t.Fatalf("rolled-back migration: got %#v, want version 4", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, false, false, false, false)
	assertMigrationIntegrationTableExists(t, database, "products", false)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"products_published_order_idx",
		false,
	)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", true)
	assertMigrationIntegrationTableExists(t, database, "admin_users", true)
	assertMigrationIntegrationAdminAccess(
		t,
		database,
		adminUserID,
		"owner@example.test",
		"owner",
		0x33,
		0x44,
	)

	// The next one-step rollback reaches version 000003. It removes both admin
	// tables in dependency order while the inquiry schema and visitor records
	// remain completely unchanged.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 15 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 3 {
		t.Fatalf("rolled-back migration: got %#v, want version 3", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, false, false, false, false, false)
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

	// A third one-step rollback reaches version 000002. It removes only the
	// idempotency key while version 000001 and both inquiry rows remain.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 14 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 2 {
		t.Fatalf("rolled-back migration: got %#v, want version 2", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, false, false, false, false, false, false)
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

	// Reapplying with version 000001 still present plans versions 000002 through
	// 000007 in order. The key backfill preserves inquiry contact data, while the
	// recreated admin, Product, and Interior tables return empty after their
	// deliberate strict drops.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("reapply migrations 2 through 7: %v", err)
	}
	if len(applied) != 6 ||
		applied[0].Version != 2 ||
		applied[1].Version != 3 ||
		applied[2].Version != 4 ||
		applied[3].Version != 5 ||
		applied[4].Version != 6 ||
		applied[5].Version != 7 {
		t.Fatalf("reapplied migrations: got %#v, want versions 2 through 7", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, true)
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
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"products_published_order_idx",
		true,
	)
	assertMigrationIntegrationTableRowCount(t, database, "products", 0)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "description", true)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", true)
	assertMigrationIntegrationTableExists(t, database, "interior_projects", true)
	assertMigrationIntegrationTableExists(
		t,
		database,
		"interior_project_cover_images",
		true,
	)
	assertMigrationIntegrationIndexExists(
		t,
		database,
		"interior_projects_published_order_idx",
		true,
	)
	assertMigrationIntegrationTableRowCount(t, database, "interior_projects", 0)
	assertMigrationIntegrationTableRowCount(
		t,
		database,
		"interior_project_cover_images",
		0,
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

	// A synthetic version 000008 fails after executing DDL. PostgreSQL must roll
	// back that DDL while retaining all seven applied embedded versions.
	assertMigrationIntegrationAtomicFailure(t, database, catalog)
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, true)

	// Seven explicit rollback calls prove the runner reverses the catalog one
	// migration at a time: Interior projects/cover, Product content/cover,
	// Product revision, Products, admin access, inquiry key, then inquiries.
	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 22 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 7 {
		t.Fatalf("rolled-back migration: got %#v, want version 7", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, false)
	assertMigrationIntegrationTableExists(t, database, "interior_projects", false)
	assertMigrationIntegrationTableExists(
		t,
		database,
		"interior_project_cover_images",
		false,
	)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 21 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 6 {
		t.Fatalf("rolled-back migration: got %#v, want version 6", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, false, false)
	assertMigrationIntegrationTableExists(t, database, "product_cover_images", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "description", false)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 20 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 5 {
		t.Fatalf("rolled-back migration: got %#v, want version 5", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, false, false, false)
	assertMigrationIntegrationTableExists(t, database, "products", true)
	assertMigrationIntegrationColumnExists(t, database, "products", "version", false)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 18 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 4 {
		t.Fatalf("rolled-back migration: got %#v, want version 4", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, false, false, false, false)
	assertMigrationIntegrationTableExists(t, database, "products", false)
	assertMigrationIntegrationTableExists(t, database, "admin_sessions", true)
	assertMigrationIntegrationTableExists(t, database, "admin_users", true)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 15 migration before full rollback: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 3 {
		t.Fatalf("rolled-back migration: got %#v, want version 3", rolledBack)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, false, false, false, false, false)
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
	assertMigrationIntegrationStatus(t, runner, true, false, false, false, false, false, false)
	assertMigrationIntegrationTableExists(t, database, "inquiries", true)

	rolledBack, err = runner.Down(t.Context())
	if err != nil {
		t.Fatalf("roll back Stage 13 migration: %v", err)
	}
	if rolledBack == nil || rolledBack.Version != 1 {
		t.Fatalf("rolled-back migration: got %#v, want version 1", rolledBack)
	}
	assertMigrationIntegrationTableExists(t, database, "inquiries", false)
	assertMigrationIntegrationStatus(t, runner, false, false, false, false, false, false, false)

	if _, err := runner.Down(t.Context()); !errors.Is(err, errNoAppliedMigrations) {
		t.Fatalf("empty rollback: got %v, want no applied migrations", err)
	}

	// Reapply after the full rollback so the destructive operator cycle finishes
	// on the complete current schema before the test-owned cleanup executes.
	applied, err = runner.Up(t.Context())
	if err != nil {
		t.Fatalf("reapply embedded migrations after full rollback: %v", err)
	}
	if len(applied) != 7 ||
		applied[0].Version != 1 ||
		applied[1].Version != 2 ||
		applied[2].Version != 3 ||
		applied[3].Version != 4 ||
		applied[4].Version != 5 ||
		applied[5].Version != 6 ||
		applied[6].Version != 7 {
		t.Fatalf("reapplied migrations: got %#v, want versions 1 through 7", applied)
	}
	assertMigrationIntegrationStatus(t, runner, true, true, true, true, true, true, true)
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
	var productsExist bool
	var productCoversExist bool
	var interiorProjectsExist bool
	var interiorProjectCoversExist bool
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
    to_regclass('public.products') IS NOT NULL,
    to_regclass('public.product_cover_images') IS NOT NULL,
    to_regclass('public.interior_projects') IS NOT NULL,
    to_regclass('public.interior_project_cover_images') IS NOT NULL,
    to_regclass('public.schema_migrations') IS NOT NULL,
    to_regclass('public.stage13_atomicity_probe') IS NOT NULL,
    to_regclass('public.stage13_intentionally_missing_table') IS NOT NULL`,
	).Scan(
		&currentDatabase,
		&inquiriesExist,
		&adminUsersExist,
		&adminSessionsExist,
		&productsExist,
		&productCoversExist,
		&interiorProjectsExist,
		&interiorProjectCoversExist,
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
		productsExist ||
		productCoversExist ||
		interiorProjectsExist ||
		interiorProjectCoversExist ||
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
// probed by this test from the already-confirmed disposable database. Each
// cover child precedes its parent, while sessions precede users, preserving the
// same narrowly scoped foreign-key order as the real reverse migrations.
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
		"DROP TABLE IF EXISTS public.interior_project_cover_images",
		"DROP TABLE IF EXISTS public.interior_projects",
		"DROP TABLE IF EXISTS public.product_cover_images",
		"DROP TABLE IF EXISTS public.products",
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

// assertMigrationIntegrationTableRowCount reads only one of the exact owned
// relations used at call sites. The closed switch avoids interpolating even a
// test-provided identifier into SQL.
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
	case "products":
		query = "SELECT COUNT(*) FROM public.products"
	case "product_cover_images":
		query = "SELECT COUNT(*) FROM public.product_cover_images"
	case "interior_projects":
		query = "SELECT COUNT(*) FROM public.interior_projects"
	case "interior_project_cover_images":
		query = "SELECT COUNT(*) FROM public.interior_project_cover_images"
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

// insertMigrationIntegrationProduct writes one valid schema-only fixture while
// deliberately omitting publication status and timestamps. The later read
// proves PostgreSQL supplied the fail-closed draft and clock defaults.
func insertMigrationIntegrationProduct(
	t *testing.T,
	database *sql.DB,
	slug string,
	name string,
	category string,
	sortOrder int,
) int64 {
	t.Helper()

	var productID int64
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.products (slug, name, category, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING id`,
		slug,
		name,
		category,
		sortOrder,
	).Scan(&productID); err != nil {
		t.Fatal("insert migration integration Product")
	}
	if productID <= 0 {
		t.Fatalf("generated Product identity: got %d, want positive", productID)
	}

	return productID
}

// assertMigrationIntegrationProduct reads every version-000004 column needed
// to verify fixture mapping, the draft default, and equal initial timestamps.
func assertMigrationIntegrationProduct(
	t *testing.T,
	database *sql.DB,
	productID int64,
	expectedSlug string,
	expectedName string,
	expectedCategory string,
	expectedSortOrder int,
	expectedPublicationStatus string,
) {
	t.Helper()

	var slug string
	var name string
	var category string
	var sortOrder int
	var publicationStatus string
	var createdAt time.Time
	var updatedAt time.Time
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
FROM public.products
WHERE id = $1`,
		productID,
	).Scan(
		&slug,
		&name,
		&category,
		&sortOrder,
		&publicationStatus,
		&createdAt,
		&updatedAt,
	); err != nil {
		t.Fatal("read migration integration Product")
	}

	if slug != expectedSlug ||
		name != expectedName ||
		category != expectedCategory ||
		sortOrder != expectedSortOrder ||
		publicationStatus != expectedPublicationStatus {
		t.Errorf(
			"stored Product: got slug=%q name=%q category=%q order=%d status=%q",
			slug,
			name,
			category,
			sortOrder,
			publicationStatus,
		)
	}
	if createdAt.IsZero() || !updatedAt.Equal(createdAt) {
		t.Errorf(
			"initial Product timestamps: got created=%s updated=%s, want equal nonzero values",
			createdAt,
			updatedAt,
		)
	}
}

// assertMigrationIntegrationProductVersion proves migration 000005 backfills
// the default positive revision and rejects a non-positive direct update.
func assertMigrationIntegrationProductVersion(
	t *testing.T,
	database *sql.DB,
	productID int64,
	expectedVersion int64,
) {
	t.Helper()

	var version int64
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT version FROM public.products WHERE id = $1",
		productID,
	).Scan(&version); err != nil {
		t.Fatal("read migration integration Product version")
	}
	if version != expectedVersion {
		t.Errorf("Product version: got %d, want %d", version, expectedVersion)
	}

	_, err := database.ExecContext(
		t.Context(),
		"UPDATE public.products SET version = 0 WHERE id = $1",
		productID,
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresCheckViolationCode,
		"products_version_positive",
		"",
	)
}

// assertMigrationIntegrationProductContentAndCover proves migration 000006
// backfills empty editorial values, enforces representative named cover
// constraints and one-cover ownership, and cascades media with its Product.
func assertMigrationIntegrationProductContentAndCover(
	t *testing.T,
	database *sql.DB,
	productID int64,
) {
	t.Helper()

	var description string
	var material string
	var dimensions string
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT description, material, dimensions
FROM public.products
WHERE id = $1`,
		productID,
	).Scan(&description, &material, &dimensions); err != nil {
		t.Fatal("read migration-6 Product content defaults")
	}
	if description != "" || material != "" || dimensions != "" {
		t.Errorf(
			"migration-6 content defaults: description=%q material=%q dimensions=%q",
			description,
			material,
			dimensions,
		)
	}

	_, err := database.ExecContext(
		t.Context(),
		"UPDATE public.products SET description = ' untrimmed' WHERE id = $1",
		productID,
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresCheckViolationCode,
		"products_description_trimmed",
		"",
	)

	cover := validAdminProductCoverWriteInput(t)
	insertCover := func(ownerID int64) error {
		_, insertErr := database.ExecContext(
			t.Context(),
			`INSERT INTO public.product_cover_images (
    product_id, content_type, content, byte_size, width, height,
    sha256, alt_text, caption
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ownerID,
			cover.ContentType,
			cover.Content,
			cover.ByteSize,
			cover.Width,
			cover.Height,
			cover.SHA256[:],
			cover.AltText,
			cover.Caption,
		)

		return insertErr
	}
	if err := insertCover(productID); err != nil {
		t.Fatal("insert valid migration-6 Product cover")
	}
	assertPostgresConstraintError(
		t,
		insertCover(productID),
		postgresUniqueViolationCode,
		"product_cover_images_pkey",
		"",
	)

	for _, test := range []struct {
		// name identifies the direct malformed cover update.
		name string
		// assignment changes one migration-owned value.
		assignment string
		// constraint is the stable named check expected from PostgreSQL.
		constraint string
	}{
		{name: "unsupported type", assignment: "content_type = 'image/gif'", constraint: "product_cover_images_content_type_supported"},
		{name: "axis overflow", assignment: "width = 10001", constraint: "product_cover_images_width_supported"},
		{name: "empty alt text", assignment: "alt_text = ''", constraint: "product_cover_images_alt_text_length"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, updateErr := database.ExecContext(
				t.Context(),
				"UPDATE public.product_cover_images SET "+test.assignment+" WHERE product_id = $1",
				productID,
			)
			assertPostgresConstraintError(
				t,
				updateErr,
				postgresCheckViolationCode,
				test.constraint,
				"",
			)
		})
	}

	cascadeProductID := insertMigrationIntegrationProduct(
		t,
		database,
		"stage-twenty-one-cascade-product",
		"Stage Twenty One Cascade Product",
		"Test Furniture",
		99,
	)
	if err := insertCover(cascadeProductID); err != nil {
		t.Fatal("insert cover for cascade Product")
	}
	if _, err := database.ExecContext(
		t.Context(),
		"DELETE FROM public.products WHERE id = $1",
		cascadeProductID,
	); err != nil {
		t.Fatal("delete cascade Product")
	}
	var remaining int
	if err := database.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM public.product_cover_images WHERE product_id = $1",
		cascadeProductID,
	).Scan(&remaining); err != nil || remaining != 0 {
		t.Errorf("cascaded cover rows: got %d err=%v, want zero", remaining, err)
	}
}

// insertMigrationIntegrationInteriorProject writes one minimum valid Interior
// fixture while deliberately omitting optional content, publication state,
// revision, and timestamps. The following assertion therefore proves the
// migration's honest NULL/empty defaults rather than values supplied by Go.
func insertMigrationIntegrationInteriorProject(
	t *testing.T,
	database *sql.DB,
	slug string,
	title string,
	typology string,
	projectStatus string,
	sortOrder int,
) int64 {
	t.Helper()

	var projectID int64
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.interior_projects (
    slug,
    title,
    typology,
    project_status,
    sort_order
) VALUES ($1, $2, $3, $4, $5)
RETURNING id`,
		slug,
		title,
		typology,
		projectStatus,
		sortOrder,
	).Scan(&projectID); err != nil {
		t.Fatal("insert migration integration Interior project")
	}
	if projectID <= 0 {
		t.Fatalf("generated Interior project identity: got %d, want positive", projectID)
	}

	return projectID
}

// assertMigrationIntegrationInteriorProject reads the complete migration-7
// parent record. In addition to fixture mapping, it proves optional text begins
// empty, year remains SQL NULL, lifecycle begins Draft, revision begins at one,
// and both timestamps share the insertion transaction time.
func assertMigrationIntegrationInteriorProject(
	t *testing.T,
	database *sql.DB,
	projectID int64,
	expectedSlug string,
	expectedTitle string,
	expectedTypology string,
	expectedProjectStatus string,
	expectedSortOrder int,
) {
	t.Helper()

	var slug string
	var title string
	var typology string
	var location string
	var projectYear sql.NullInt64
	var projectStatus string
	var description string
	var sortOrder int
	var publicationStatus string
	var version int64
	var createdAt time.Time
	var updatedAt time.Time
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT
    slug,
    title,
    typology,
    location,
    project_year,
    project_status,
    description,
    sort_order,
    publication_status,
    version,
    created_at,
    updated_at
FROM public.interior_projects
WHERE id = $1`,
		projectID,
	).Scan(
		&slug,
		&title,
		&typology,
		&location,
		&projectYear,
		&projectStatus,
		&description,
		&sortOrder,
		&publicationStatus,
		&version,
		&createdAt,
		&updatedAt,
	); err != nil {
		t.Fatal("read migration integration Interior project")
	}

	if slug != expectedSlug ||
		title != expectedTitle ||
		typology != expectedTypology ||
		projectStatus != expectedProjectStatus ||
		sortOrder != expectedSortOrder ||
		location != "" ||
		projectYear.Valid ||
		description != "" ||
		publicationStatus != "draft" ||
		version != 1 {
		t.Error("stored Interior project violates expected values or defaults")
	}
	if createdAt.IsZero() || !updatedAt.Equal(createdAt) {
		t.Errorf(
			"initial Interior timestamps: got created=%s updated=%s, want equal nonzero values",
			createdAt,
			updatedAt,
		)
	}
}

// assertMigrationIntegrationInteriorProjectConstraintsAndCover bypasses the
// Stage 22 application validators to exercise PostgreSQL invariants directly.
// Each failed parent or cover statement changes only one named rule, keeping
// SQLSTATE and constraint diagnostics deterministic and educational.
func assertMigrationIntegrationInteriorProjectConstraintsAndCover(
	t *testing.T,
	database *sql.DB,
	projectID int64,
) {
	t.Helper()

	parentConstraintTests := []struct {
		// name identifies one isolated migration-owned invariant.
		name string
		// assignment is trusted test SQL selected only from this closed table.
		assignment string
		// constraint is the exact named check expected from PostgreSQL.
		constraint string
	}{
		{name: "slug length", assignment: "slug = repeat('s', 121)", constraint: "interior_projects_slug_length"},
		{name: "slug format", assignment: "slug = 'invalid--slug'", constraint: "interior_projects_slug_format"},
		{name: "title trimmed", assignment: "title = ' Padded title'", constraint: "interior_projects_title_trimmed"},
		{name: "title required", assignment: "title = ''", constraint: "interior_projects_title_length"},
		{name: "title length", assignment: "title = repeat('t', 161)", constraint: "interior_projects_title_length"},
		{name: "typology trimmed", assignment: "typology = ' Residential'", constraint: "interior_projects_typology_trimmed"},
		{name: "typology required", assignment: "typology = ''", constraint: "interior_projects_typology_length"},
		{name: "typology length", assignment: "typology = repeat('t', 81)", constraint: "interior_projects_typology_length"},
		{name: "location trimmed", assignment: "location = ' Tehran'", constraint: "interior_projects_location_trimmed"},
		{name: "location length", assignment: "location = repeat('l', 161)", constraint: "interior_projects_location_length"},
		{name: "year range", assignment: "project_year = 999", constraint: "interior_projects_project_year_supported"},
		{name: "year upper range", assignment: "project_year = 10000", constraint: "interior_projects_project_year_supported"},
		{name: "project status trimmed", assignment: "project_status = ' Completed'", constraint: "interior_projects_project_status_trimmed"},
		{name: "project status required", assignment: "project_status = ''", constraint: "interior_projects_project_status_length"},
		{name: "project status length", assignment: "project_status = repeat('s', 81)", constraint: "interior_projects_project_status_length"},
		{name: "description trimmed", assignment: "description = ' Padded description'", constraint: "interior_projects_description_trimmed"},
		{name: "description length", assignment: "description = repeat('d', 6001)", constraint: "interior_projects_description_length"},
		{name: "sort order positive", assignment: "sort_order = 0", constraint: "interior_projects_sort_order_positive"},
		{name: "publication status supported", assignment: "publication_status = 'private'", constraint: "interior_projects_publication_status_supported"},
		{name: "version positive", assignment: "version = 0", constraint: "interior_projects_version_positive"},
		{name: "timestamp order", assignment: "updated_at = created_at - INTERVAL '1 second'", constraint: "interior_projects_timestamp_order"},
	}
	for _, test := range parentConstraintTests {
		t.Run("Interior parent "+test.name, func(t *testing.T) {
			_, err := database.ExecContext(
				t.Context(),
				"UPDATE public.interior_projects SET "+test.assignment+" WHERE id = $1",
				projectID,
			)
			assertPostgresConstraintError(
				t,
				err,
				postgresCheckViolationCode,
				test.constraint,
				"",
			)
		})
	}

	// The canonical slug collision has its own named uniqueness boundary so the
	// writer and form can translate only this expected conflict without exposing
	// a general PostgreSQL diagnostic.
	_, err := database.ExecContext(
		t.Context(),
		`INSERT INTO public.interior_projects (
    slug, title, typology, project_status, sort_order
)
SELECT slug, 'Duplicate Interior', 'Residential', 'Completed', 2
FROM public.interior_projects
WHERE id = $1`,
		projectID,
	)
	assertPostgresConstraintError(
		t,
		err,
		postgresUniqueViolationCode,
		"interior_projects_slug_unique",
		"",
	)

	cover := validAdminInteriorProjectCoverWriteInput(t)
	insertCover := func(ownerID int64) error {
		_, insertErr := database.ExecContext(
			t.Context(),
			`INSERT INTO public.interior_project_cover_images (
    interior_project_id,
    content_type,
    content,
    byte_size,
    width,
    height,
    sha256,
    alt_text,
    caption
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ownerID,
			cover.ContentType,
			cover.Content,
			cover.ByteSize,
			cover.Width,
			cover.Height,
			cover.SHA256[:],
			cover.AltText,
			cover.Caption,
		)

		return insertErr
	}
	if err := insertCover(projectID); err != nil {
		t.Fatal("insert valid migration-7 Interior cover")
	}

	var coverVersion int64
	var coverCreatedAt time.Time
	var coverUpdatedAt time.Time
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT version, created_at, updated_at
FROM public.interior_project_cover_images
WHERE interior_project_id = $1`,
		projectID,
	).Scan(&coverVersion, &coverCreatedAt, &coverUpdatedAt); err != nil {
		t.Fatal("read migration-7 Interior cover defaults")
	}
	if coverVersion != 1 || coverCreatedAt.IsZero() ||
		!coverUpdatedAt.Equal(coverCreatedAt) {
		t.Error("initial Interior cover revision or timestamps violate defaults")
	}

	assertPostgresConstraintError(
		t,
		insertCover(projectID),
		postgresUniqueViolationCode,
		"interior_project_cover_images_pkey",
		"",
	)
	assertPostgresConstraintError(
		t,
		insertCover(math.MaxInt64),
		"23503",
		"interior_project_cover_images_project_id_foreign",
		"",
	)

	coverConstraintTests := []struct {
		// name identifies one exact stored-media rule.
		name string
		// assignment changes only values needed to isolate that rule.
		assignment string
		// constraint is the stable named check expected from PostgreSQL.
		constraint string
	}{
		{name: "version positive", assignment: "version = 0", constraint: "interior_project_cover_images_version_positive"},
		{name: "content type", assignment: "content_type = 'image/gif'", constraint: "interior_project_cover_images_content_type_supported"},
		{name: "byte size range", assignment: "content = ''::bytea, byte_size = 0", constraint: "interior_project_cover_images_byte_size_supported"},
		{name: "content size", assignment: "byte_size = byte_size + 1", constraint: "interior_project_cover_images_content_size_matches"},
		{name: "width range", assignment: "width = 0", constraint: "interior_project_cover_images_width_supported"},
		{name: "height range", assignment: "height = 10001", constraint: "interior_project_cover_images_height_supported"},
		{name: "pixel count", assignment: "width = 5001, height = 5001", constraint: "interior_project_cover_images_pixel_count_supported"},
		{name: "digest length", assignment: "sha256 = decode('00', 'hex')", constraint: "interior_project_cover_images_sha256_length"},
		{name: "alt text trimmed", assignment: "alt_text = ' Padded alternative'", constraint: "interior_project_cover_images_alt_text_trimmed"},
		{name: "alt text required", assignment: "alt_text = ''", constraint: "interior_project_cover_images_alt_text_length"},
		{name: "alt text length", assignment: "alt_text = repeat('a', 301)", constraint: "interior_project_cover_images_alt_text_length"},
		{name: "caption trimmed", assignment: "caption = ' Padded caption'", constraint: "interior_project_cover_images_caption_trimmed"},
		{name: "caption length", assignment: "caption = repeat('c', 501)", constraint: "interior_project_cover_images_caption_length"},
		{name: "timestamp order", assignment: "updated_at = created_at - INTERVAL '1 second'", constraint: "interior_project_cover_images_timestamp_order"},
	}
	for _, test := range coverConstraintTests {
		t.Run("Interior cover "+test.name, func(t *testing.T) {
			_, err := database.ExecContext(
				t.Context(),
				"UPDATE public.interior_project_cover_images SET "+test.assignment+" WHERE interior_project_id = $1",
				projectID,
			)
			assertPostgresConstraintError(
				t,
				err,
				postgresCheckViolationCode,
				test.constraint,
				"",
			)
		})
	}

	// A second disposable parent proves the narrowly scoped foreign key removes
	// its one child. The primary fixture and cover remain for the later rollback-
	// isolation assertion, so the test does not confuse cascade with table drop.
	cascadeProjectID := insertMigrationIntegrationInteriorProject(
		t,
		database,
		"stage-twenty-two-cascade-interior",
		"Stage Twenty Two Cascade Interior",
		"Hospitality",
		"Completed",
		99,
	)
	if err := insertCover(cascadeProjectID); err != nil {
		t.Fatal("insert cover for cascade Interior project")
	}
	if _, err := database.ExecContext(
		t.Context(),
		"DELETE FROM public.interior_projects WHERE id = $1",
		cascadeProjectID,
	); err != nil {
		t.Fatal("delete cascade Interior project")
	}
	var remaining int
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT COUNT(*)
FROM public.interior_project_cover_images
WHERE interior_project_id = $1`,
		cascadeProjectID,
	).Scan(&remaining); err != nil || remaining != 0 {
		t.Errorf("cascaded Interior cover rows: got %d err=%v, want zero", remaining, err)
	}
	assertMigrationIntegrationTableRowCount(t, database, "interior_projects", 1)
	assertMigrationIntegrationTableRowCount(
		t,
		database,
		"interior_project_cover_images",
		1,
	)
}

// assertMigrationIntegrationProductConstraints bypasses application validation
// to prove PostgreSQL rejects every malformed field shape owned by
// migration 000004. Each case violates only its named constraint so diagnostics
// remain deterministic across supported PostgreSQL versions.
func assertMigrationIntegrationProductConstraints(
	t *testing.T,
	database *sql.DB,
) {
	t.Helper()

	const directInsertSQL = `INSERT INTO public.products (
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

	baseline := time.Now().UTC().Truncate(time.Microsecond)
	tests := []struct {
		// name identifies the isolated database invariant under test.
		name string
		// slug, productName, and category supply the three constrained text values.
		slug        string
		productName string
		category    string
		// sortOrder and publicationStatus supply lifecycle and ordering values.
		sortOrder         int
		publicationStatus string
		// createdAt and updatedAt exercise the timestamp-order constraint.
		createdAt time.Time
		updatedAt time.Time
		// sqlState and constraint identify PostgreSQL's stable failure diagnostics.
		sqlState   string
		constraint string
	}{
		{
			name:              "slug length",
			slug:              strings.Repeat("a", 121),
			productName:       "Slug Length Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_slug_length",
		},
		{
			name:              "slug format",
			slug:              "invalid--slug",
			productName:       "Slug Format Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_slug_format",
		},
		{
			name:              "slug unique",
			slug:              "stage-eighteen-product",
			productName:       "Duplicate Slug Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresUniqueViolationCode,
			constraint:        "products_slug_unique",
		},
		{
			name:              "name trimmed",
			slug:              "name-trimmed-product",
			productName:       " Padded Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_name_trimmed",
		},
		{
			name:              "name length",
			slug:              "name-length-product",
			productName:       strings.Repeat("n", 161),
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_name_length",
		},
		{
			name:              "category trimmed",
			slug:              "category-trimmed-product",
			productName:       "Category Trim Product",
			category:          " Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_category_trimmed",
		},
		{
			name:              "category length",
			slug:              "category-length-product",
			productName:       "Category Length Product",
			category:          strings.Repeat("c", 81),
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_category_length",
		},
		{
			name:              "sort order positive",
			slug:              "sort-order-product",
			productName:       "Sort Order Product",
			category:          "Furniture",
			sortOrder:         0,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_sort_order_positive",
		},
		{
			name:              "publication status supported",
			slug:              "publication-status-product",
			productName:       "Publication Status Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "private",
			createdAt:         baseline,
			updatedAt:         baseline,
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_publication_status_supported",
		},
		{
			name:              "timestamp order",
			slug:              "timestamp-order-product",
			productName:       "Timestamp Order Product",
			category:          "Furniture",
			sortOrder:         2,
			publicationStatus: "draft",
			createdAt:         baseline,
			updatedAt:         baseline.Add(-time.Second),
			sqlState:          postgresCheckViolationCode,
			constraint:        "products_timestamp_order",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := database.ExecContext(
				t.Context(),
				directInsertSQL,
				test.slug,
				test.productName,
				test.category,
				test.sortOrder,
				test.publicationStatus,
				test.createdAt,
				test.updatedAt,
			)
			assertPostgresConstraintError(
				t,
				err,
				test.sqlState,
				test.constraint,
				"",
			)
		})
	}

	assertMigrationIntegrationTableRowCount(t, database, "products", 1)
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
		// Versions 000001 through 000007 are the real embedded history. The probe
		// therefore uses the next contiguous version instead of colliding with the
		// Stage 22 definition during catalog validation.
		Version: 8,
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
	if ledgerRows != 7 {
		t.Errorf("ledger rows after failed migration: got %d, want 7", ledgerRows)
	}
}
