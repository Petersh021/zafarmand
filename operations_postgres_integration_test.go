package main

import (
	"errors"
	"testing"
)

// TestPostgresOperationalReadinessIntegration proves the read-only production
// checker against a separately confirmed disposable PostgreSQL database. It is
// skipped by the shared Stage 13 guard unless the operator supplies the exact
// isolated test URL and destructive-data acknowledgement.
func TestPostgresOperationalReadinessIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed readiness integration database: %v", err)
	}
	// Cleanup executes last-in, first-out, leaving the pool available for schema
	// cleanup before the final close.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close readiness integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	checker, err := newPostgresOperationalReadiness(database)
	if err != nil {
		t.Fatalf("create PostgreSQL readiness checker: %v", err)
	}
	// A reachable empty database is not ready because it has no migration ledger.
	if err := checker.Check(t.Context()); !errors.Is(
		err,
		errOperationalReadinessFailed,
	) {
		t.Fatalf("empty database readiness: got %v, want failure", err)
	}

	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		t.Fatalf("load embedded migration catalog: %v", err)
	}
	runner, err := newMigrationRunner(database, catalog)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}
	if err := checker.Check(t.Context()); err != nil {
		t.Fatalf("fully migrated database readiness: %v", err)
	}

	// Rolling back only the latest definition leaves a valid but incomplete
	// ledger, which must immediately remove the database from readiness.
	if _, err := runner.Down(t.Context()); err != nil {
		t.Fatalf("roll back latest migration: %v", err)
	}
	if err := checker.Check(t.Context()); !errors.Is(
		err,
		errOperationalReadinessFailed,
	) {
		t.Fatalf("pending database readiness: got %v, want failure", err)
	}
}
