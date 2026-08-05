package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestNewMigrationRunner verifies dependency validation and proves the runner
// owns a copy of the supplied migration catalog.
func TestNewMigrationRunner(t *testing.T) {
	database, err := sql.Open(
		"pgx",
		"postgres://example:example@localhost/example",
	)
	if err != nil {
		t.Fatalf("create unconnected test pool: %v", err)
	}
	defer database.Close()

	catalog := []migrationDefinition{
		{
			Version: 1,
			Name:    "sentinel",
			UpSQL:   "SELECT 1",
			DownSQL: "SELECT 1",
		},
	}

	tests := []struct {
		// name labels one constructor boundary.
		name string
		// database is nil only for the missing-pool case.
		database *sql.DB
		// catalog is empty only for the missing-history case.
		catalog []migrationDefinition
		// expectedError identifies the stable constructor error.
		expectedError error
	}{
		{
			name:          "missing database",
			catalog:       catalog,
			expectedError: errMigrationDatabaseRequired,
		},
		{
			name:          "missing catalog",
			database:      database,
			expectedError: errMigrationCatalogRequired,
		},
		{
			name:     "valid dependencies",
			database: database,
			catalog:  catalog,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner, err := newMigrationRunner(
				test.database,
				test.catalog,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			if test.expectedError != nil {
				if runner != nil {
					t.Error("invalid dependencies returned a runner")
				}

				return
			}

			test.catalog[0].Name = "mutated"
			if runner.catalog[0].Name != "sentinel" {
				t.Error("runner catalog changed with caller slice")
			}
		})
	}
}

// TestMigrationLedgerContract protects the runner-owned table constraints and
// confirms the ledger never contains visitor inquiry fields.
func TestMigrationLedgerContract(t *testing.T) {
	for _, requiredFragment := range []string{
		"version bigint PRIMARY KEY",
		"name text NOT NULL UNIQUE",
		"checksum bytea NOT NULL",
		"octet_length(checksum) = 32",
		"applied_at timestamp with time zone",
	} {
		if !strings.Contains(
			migrationLedgerSQL,
			requiredFragment,
		) {
			t.Errorf(
				"migration ledger lacks %q",
				requiredFragment,
			)
		}
	}

	for _, forbiddenVisitorField := range []string{
		"email",
		"message",
		"discipline",
	} {
		if strings.Contains(
			strings.ToLower(migrationLedgerSQL),
			forbiddenVisitorField,
		) {
			t.Errorf(
				"migration ledger contains visitor field %q",
				forbiddenVisitorField,
			)
		}
	}
}

// TestCombineMigrationCleanupError verifies that schema failures take priority
// while a cleanup failure is still returned after successful work.
func TestCombineMigrationCleanupError(t *testing.T) {
	primary := errors.New("primary")
	cleanup := errors.New("cleanup")

	if actual := combineMigrationCleanupError(
		primary,
		cleanup,
	); !errors.Is(actual, primary) {
		t.Errorf("combined primary error: got %v, want %v", actual, primary)
	}
	if actual := combineMigrationCleanupError(
		nil,
		cleanup,
	); !errors.Is(actual, cleanup) {
		t.Errorf("combined cleanup error: got %v, want %v", actual, cleanup)
	}
}

// TestMigrationDatabaseError verifies that useful PostgreSQL and cancellation
// categories survive while sensitive server detail and transport text do not.
func TestMigrationDatabaseError(t *testing.T) {
	postgresCause := &pgconn.PgError{
		Code:    "42P01",
		Message: "relation does not exist",
		Detail:  "stage13-sensitive-row-detail",
	}
	postgresError := migrationDatabaseError(
		"apply migration 000001",
		postgresCause,
	)
	for _, expectedText := range []string{
		"apply migration 000001",
		"relation does not exist",
		"SQLSTATE 42P01",
	} {
		if !strings.Contains(postgresError.Error(), expectedText) {
			t.Errorf(
				"PostgreSQL error lacks %q: %v",
				expectedText,
				postgresError,
			)
		}
	}
	if strings.Contains(
		postgresError.Error(),
		postgresCause.Detail,
	) {
		t.Fatalf("PostgreSQL error exposes detail: %v", postgresError)
	}

	cancelled := migrationDatabaseError("read ledger", context.Canceled)
	if !errors.Is(cancelled, context.Canceled) {
		t.Errorf("cancellation was not preserved: %v", cancelled)
	}

	const transportSecret = "postgres://user:secret@host/database"
	redacted := migrationDatabaseError(
		"read ledger",
		errors.New(transportSecret),
	)
	if strings.Contains(redacted.Error(), transportSecret) {
		t.Fatalf("transport error was not redacted: %v", redacted)
	}
}
