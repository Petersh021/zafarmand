package main

import (
	"strings"
	"testing"
)

// TestValidateMigrationSQLSafetyAcceptsQuotedTransactionWords verifies that
// comments, identifiers, literals, and function bodies cannot be mistaken for
// executable transaction-control statements.
func TestValidateMigrationSQLSafetyAcceptsQuotedTransactionWords(
	t *testing.T,
) {
	safeSQL := `
-- COMMIT and ROLLBACK are documentation here.
/* BEGIN; /* nested COMMIT; */ ROLLBACK; */
CREATE TABLE "commit" (
    message text DEFAULT 'ROLLBACK; BEGIN;',
    escaped text DEFAULT E'COMMIT\' is still text'
);
DO $migration_body$
BEGIN
    RAISE NOTICE 'ROLLBACK;';
END
$migration_body$;
INSERT INTO "commit" (message) VALUES ('safe');
`

	if err := validateMigrationSQLSafety(safeSQL); err != nil {
		t.Fatalf("safe migration SQL was rejected: %v", err)
	}
}

// TestValidateMigrationSQLSafetyRejectsTransactionControl verifies every
// PostgreSQL transaction boundary and savepoint form owned by the Go runner.
func TestValidateMigrationSQLSafetyRejectsTransactionControl(t *testing.T) {
	tests := []string{
		"BEGIN;",
		"START TRANSACTION;",
		"COMMIT;",
		"END WORK;",
		"ROLLBACK;",
		"ABORT;",
		"PREPARE TRANSACTION 'migration';",
		"SAVEPOINT migration_step;",
		"RELEASE SAVEPOINT migration_step;",
		"SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;",
		"CREATE TABLE example (id bigint); COMMIT;",
		// PostgreSQL treats `$tag$` as part of each unquoted table identifier here.
		// The scanner must not mistake the first occurrence for a quote opener and
		// skip the real transaction command between the two identifiers.
		"CREATE TABLE public.probe$tag$ (id bigint); COMMIT; " +
			"DROP TABLE public.probe$tag$;",
	}

	for _, sqlText := range tests {
		t.Run(sqlText, func(t *testing.T) {
			err := validateMigrationSQLSafety(sqlText)
			if err == nil {
				t.Fatal("transaction-control SQL was accepted")
			}
			if !strings.Contains(
				err.Error(),
				"runner owns transaction boundaries",
			) {
				t.Errorf("error lacks ownership guidance: %v", err)
			}
		})
	}
}

// TestValidateMigrationSQLSafetyRejectsNonExecutableText verifies that comments
// and statement separators cannot become a ledger-only migration.
func TestValidateMigrationSQLSafetyRejectsNonExecutableText(t *testing.T) {
	for _, sqlText := range []string{
		"-- documentation only\n",
		"/* outer /* nested */ comment */",
		"; ; ;",
	} {
		t.Run(sqlText, func(t *testing.T) {
			if err := validateMigrationSQLSafety(sqlText); err == nil {
				t.Fatal("non-executable migration text was accepted")
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsUnsafeSQL verifies that transaction safety is
// enforced for both directions while migration filenames remain valid.
func TestLoadMigrationCatalogRejectsUnsafeSQL(t *testing.T) {
	tests := []struct {
		// name identifies the unsafe migration direction.
		name string
		// upSQL is the exact forward test file.
		upSQL string
		// downSQL is the exact reverse test file.
		downSQL string
	}{
		{
			name:    "up controls transaction",
			upSQL:   "CREATE TABLE example (id bigint); COMMIT;",
			downSQL: "DROP TABLE example;",
		},
		{
			name:    "down controls transaction",
			upSQL:   "CREATE TABLE example (id bigint);",
			downSQL: "ROLLBACK;",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileSystem := migrationTestFileSystem(
				test.upSQL,
				test.downSQL,
			)

			if _, err := loadMigrationCatalog(
				fileSystem,
				"migrations",
			); err == nil {
				t.Fatal("catalog accepted unsafe transaction SQL")
			}
		})
	}
}
