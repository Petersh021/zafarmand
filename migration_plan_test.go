package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// migrationPlanTestChecksum creates a readable deterministic checksum marker
// for planner tests without coupling them to the catalog loader's hash logic.
func migrationPlanTestChecksum(marker byte) [32]byte {
	var checksum [32]byte
	for index := range checksum {
		checksum[index] = marker
	}

	return checksum
}

// migrationPlanTestCatalog returns one stable three-version catalog shared by
// pending and rollback tests. Each definition has a distinct pair checksum so
// checksum drift cannot be mistaken for a version or name failure.
func migrationPlanTestCatalog() []migrationDefinition {
	return []migrationDefinition{
		{
			Version:  1,
			Name:     "create_inquiries",
			Checksum: migrationPlanTestChecksum(1),
		},
		{
			Version:  2,
			Name:     "add_inquiry_source",
			Checksum: migrationPlanTestChecksum(2),
		},
		{
			Version:  3,
			Name:     "add_inquiry_status_index",
			Checksum: migrationPlanTestChecksum(3),
		},
	}
}

// migrationPlanAppliedPrefix copies the first count catalog identities into
// the narrow shape that represents rows already recorded in the database.
func migrationPlanAppliedPrefix(
	catalog []migrationDefinition,
	count int,
) []appliedMigration {
	applied := make([]appliedMigration, count)
	for index := 0; index < count; index++ {
		applied[index] = appliedMigration{
			Version:  catalog[index].Version,
			Name:     catalog[index].Name,
			Checksum: catalog[index].Checksum,
		}
	}

	return applied
}

// migrationPlanVersions extracts only ordered versions so assertions remain
// independent from SQL fields owned by migrationDefinition elsewhere.
func migrationPlanVersions(
	migrations []migrationDefinition,
) []int64 {
	versions := make([]int64, len(migrations))
	for index, migration := range migrations {
		versions[index] = migration.Version
	}

	return versions
}

// TestPlanPendingMigrations verifies fresh, partially migrated, complete, and
// empty catalogs all produce the exact remaining ascending suffix.
func TestPlanPendingMigrations(t *testing.T) {
	catalog := migrationPlanTestCatalog()

	tests := []struct {
		// name identifies the valid database history under test.
		name string
		// catalog replaces the shared catalog only for the empty-catalog case.
		catalog []migrationDefinition
		// applied is the exact database ledger presented to the planner.
		applied []appliedMigration
		// wantVersions is the expected pending suffix in execution order.
		wantVersions []int64
	}{
		{
			name:         "fresh database returns complete catalog",
			catalog:      catalog,
			wantVersions: []int64{1, 2, 3},
		},
		{
			name:         "partial prefix returns remaining suffix",
			catalog:      catalog,
			applied:      migrationPlanAppliedPrefix(catalog, 1),
			wantVersions: []int64{2, 3},
		},
		{
			name:         "complete prefix returns no pending migrations",
			catalog:      catalog,
			applied:      migrationPlanAppliedPrefix(catalog, len(catalog)),
			wantVersions: []int64{},
		},
		{
			name:         "empty catalog and ledger return empty plan",
			catalog:      []migrationDefinition{},
			wantVersions: []int64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pending, err := planPendingMigrations(
				test.catalog,
				test.applied,
			)
			if err != nil {
				t.Fatalf("plan pending migrations: %v", err)
			}

			gotVersions := migrationPlanVersions(pending)
			if !reflect.DeepEqual(gotVersions, test.wantVersions) {
				t.Errorf(
					"pending versions: got %v, want %v",
					gotVersions,
					test.wantVersions,
				)
			}
		})
	}
}

// TestPlanPendingMigrationsReturnsCatalogCopy protects the catalog from command
// code that annotates or otherwise modifies the returned execution plan.
func TestPlanPendingMigrationsReturnsCatalogCopy(t *testing.T) {
	catalog := migrationPlanTestCatalog()
	pending, err := planPendingMigrations(catalog, nil)
	if err != nil {
		t.Fatalf("plan pending migrations: %v", err)
	}

	pending[0].Name = "mutated_plan_name"
	if catalog[0].Name != "create_inquiries" {
		t.Error("mutating pending plan changed the source catalog")
	}
}

// TestPlanPendingMigrationsRejectsInvalidAppliedState exercises each way a
// database ledger can stop being the catalog's exact immutable prefix.
func TestPlanPendingMigrationsRejectsInvalidAppliedState(t *testing.T) {
	catalog := migrationPlanTestCatalog()
	completePrefix := migrationPlanAppliedPrefix(catalog, len(catalog))

	tests := []struct {
		// name identifies the corrupt history boundary.
		name string
		// applied is the deliberately invalid database ledger.
		applied []appliedMigration
		// errorContains distinguishes the intended validation failure.
		errorContains string
	}{
		{
			name: "unknown version",
			applied: []appliedMigration{
				completePrefix[0],
				{
					Version:  99,
					Name:     "unknown",
					Checksum: migrationPlanTestChecksum(99),
				},
			},
			errorContains: "unknown version 99",
		},
		{
			name: "out of order versions",
			applied: []appliedMigration{
				completePrefix[1],
				completePrefix[0],
			},
			errorContains: "out of order",
		},
		{
			name: "gap in applied prefix",
			applied: []appliedMigration{
				completePrefix[0],
				completePrefix[2],
			},
			errorContains: "gap",
		},
		{
			name: "name mismatch",
			applied: []appliedMigration{
				{
					Version:  completePrefix[0].Version,
					Name:     "renamed_history",
					Checksum: completePrefix[0].Checksum,
				},
			},
			errorContains: "name mismatch",
		},
		{
			name: "checksum drift",
			applied: []appliedMigration{
				{
					Version:  completePrefix[0].Version,
					Name:     completePrefix[0].Name,
					Checksum: migrationPlanTestChecksum(42),
				},
			},
			errorContains: "checksum drift",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pending, err := planPendingMigrations(catalog, test.applied)
			if err == nil {
				t.Fatalf(
					"plan pending migrations unexpectedly succeeded with %v",
					migrationPlanVersions(pending),
				)
			}

			if !strings.Contains(err.Error(), test.errorContains) {
				t.Errorf(
					"error: got %q, want substring %q",
					err,
					test.errorContains,
				)
			}
		})
	}
}

// TestPlanLatestRollback verifies down planning chooses exactly the newest
// applied definition, never every migration or the next unapplied definition.
func TestPlanLatestRollback(t *testing.T) {
	catalog := migrationPlanTestCatalog()

	tests := []struct {
		// name identifies the valid prefix length.
		name string
		// appliedCount determines the applied catalog prefix.
		appliedCount int
		// wantVersion is the one rollback version expected.
		wantVersion int64
	}{
		{
			name:         "one applied selects first",
			appliedCount: 1,
			wantVersion:  1,
		},
		{
			name:         "partial prefix selects its latest",
			appliedCount: 2,
			wantVersion:  2,
		},
		{
			name:         "complete prefix selects catalog latest",
			appliedCount: len(catalog),
			wantVersion:  3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applied := migrationPlanAppliedPrefix(
				catalog,
				test.appliedCount,
			)
			rollback, err := planLatestRollback(catalog, applied)
			if err != nil {
				t.Fatalf("plan latest rollback: %v", err)
			}

			if rollback.Version != test.wantVersion {
				t.Errorf(
					"rollback version: got %d, want %d",
					rollback.Version,
					test.wantVersion,
				)
			}
		})
	}
}

// TestPlanLatestRollbackReturnsCatalogCopy proves a caller can prepare or
// annotate its selected down migration without mutating catalog history.
func TestPlanLatestRollbackReturnsCatalogCopy(t *testing.T) {
	catalog := migrationPlanTestCatalog()
	applied := migrationPlanAppliedPrefix(catalog, len(catalog))

	rollback, err := planLatestRollback(catalog, applied)
	if err != nil {
		t.Fatalf("plan latest rollback: %v", err)
	}

	rollback.Name = "mutated_rollback_name"
	if catalog[len(catalog)-1].Name != "add_inquiry_status_index" {
		t.Error("mutating rollback plan changed the source catalog")
	}
}

// TestPlanLatestRollbackRejectsEmptyAndInvalidState verifies an empty ledger
// returns its sentinel and corrupt history is rejected before any down selection.
func TestPlanLatestRollbackRejectsEmptyAndInvalidState(t *testing.T) {
	catalog := migrationPlanTestCatalog()

	t.Run("empty applied ledger", func(t *testing.T) {
		rollback, err := planLatestRollback(catalog, nil)
		if rollback != nil {
			t.Errorf(
				"rollback: got version %d, want nil",
				rollback.Version,
			)
		}
		if !errors.Is(err, errNoAppliedMigrations) {
			t.Errorf(
				"error: got %v, want errNoAppliedMigrations",
				err,
			)
		}
	})

	t.Run("invalid applied prefix", func(t *testing.T) {
		applied := migrationPlanAppliedPrefix(catalog, 1)
		applied[0].Checksum = migrationPlanTestChecksum(88)

		rollback, err := planLatestRollback(catalog, applied)
		if rollback != nil {
			t.Errorf(
				"rollback: got version %d, want nil",
				rollback.Version,
			)
		}
		if err == nil || !strings.Contains(
			err.Error(),
			"checksum drift",
		) {
			t.Errorf(
				"error: got %v, want checksum drift",
				err,
			)
		}
	})
}
