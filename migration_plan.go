package main

import (
	"errors"
	"fmt"
)

// errNoAppliedMigrations reports that a down plan has no migration to select.
//
// The sentinel lets the command layer distinguish a clean, already-empty
// database from corrupt migration history without comparing error text.
var errNoAppliedMigrations = errors.New("no applied migrations to roll back")

// appliedMigration is the immutable migration identity read from the database
// ledger.
//
// SQL text is intentionally absent. The embedded catalog owns executable SQL,
// while the ledger records only enough trusted metadata to prove that applied
// history is the exact prefix of the current catalog.
type appliedMigration struct {
	// Version is the positive, monotonically increasing migration number.
	Version int64
	// Name is the stable descriptive portion of the migration filename.
	Name string
	// Checksum identifies the complete immutable up/down migration pair.
	Checksum [32]byte
}

// planPendingMigrations validates the applied ledger against the embedded
// catalog and returns every unapplied definition in ascending catalog order.
//
// A valid ledger must be an exact prefix: it cannot skip a catalog version,
// reorder versions, refer to unknown history, rename an applied migration, or
// change an applied checksum. Returning a copy keeps later command code from
// mutating the catalog's backing array while it executes the plan.
func planPendingMigrations(
	catalog []migrationDefinition,
	applied []appliedMigration,
) ([]migrationDefinition, error) {
	if err := validateAppliedMigrationPrefix(catalog, applied); err != nil {
		return nil, err
	}

	pending := make(
		[]migrationDefinition,
		len(catalog)-len(applied),
	)
	copy(pending, catalog[len(applied):])

	return pending, nil
}

// planLatestRollback validates the applied ledger and selects only its newest
// catalog definition for a one-step rollback.
//
// Returning a pointer to a copy prevents rollback preparation from modifying
// the embedded catalog. An empty valid ledger returns errNoAppliedMigrations;
// callers may treat that sentinel as a truthful no-op rather than corruption.
func planLatestRollback(
	catalog []migrationDefinition,
	applied []appliedMigration,
) (*migrationDefinition, error) {
	if err := validateAppliedMigrationPrefix(catalog, applied); err != nil {
		return nil, err
	}

	if len(applied) == 0 {
		return nil, errNoAppliedMigrations
	}

	latest := catalog[len(applied)-1]

	return &latest, nil
}

// validateAppliedMigrationPrefix enforces the one legal relationship between
// database history and the embedded catalog: applied rows are the catalog's
// exact leading sequence.
//
// Ordering is checked before individual identities so a reversed or duplicated
// ledger receives an explicit order error. Known versions at later catalog
// positions identify a gap, while versions absent from the catalog identify
// unknown history. Names and checksums are compared only after versions align.
func validateAppliedMigrationPrefix(
	catalog []migrationDefinition,
	applied []appliedMigration,
) error {
	for index := 1; index < len(applied); index++ {
		previousVersion := applied[index-1].Version
		currentVersion := applied[index].Version
		if currentVersion <= previousVersion {
			return fmt.Errorf(
				"applied migrations are out of order at index %d: version %d follows version %d",
				index,
				currentVersion,
				previousVersion,
			)
		}
	}

	catalogPositions := make(
		map[int64]int,
		len(catalog),
	)
	for index, migration := range catalog {
		catalogPositions[migration.Version] = index
	}

	for index, actual := range applied {
		if index >= len(catalog) {
			return fmt.Errorf(
				"applied migration at index %d has unknown version %d beyond the catalog",
				index,
				actual.Version,
			)
		}

		catalogIndex, exists := catalogPositions[actual.Version]
		if !exists {
			return fmt.Errorf(
				"applied migration at index %d has unknown version %d",
				index,
				actual.Version,
			)
		}

		if catalogIndex != index {
			return fmt.Errorf(
				"applied migration gap at index %d: expected catalog version %d but found version %d",
				index,
				catalog[index].Version,
				actual.Version,
			)
		}

		expected := catalog[index]
		if actual.Name != expected.Name {
			return fmt.Errorf(
				"applied migration version %d name mismatch: got %q, want %q",
				actual.Version,
				actual.Name,
				expected.Name,
			)
		}

		if actual.Checksum != expected.Checksum {
			return fmt.Errorf(
				"applied migration version %d checksum drift",
				actual.Version,
			)
		}
	}

	return nil
}
