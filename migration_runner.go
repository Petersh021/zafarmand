package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Migration runner constants identify its database-wide lock and bound cleanup
// work performed after an interrupted command.
const (
	// migrationAdvisoryLockID is the signed 64-bit representation of the ASCII
	// bytes "ZAFARMAN". Its stable application-specific value coordinates every
	// Zafarmand migration process that targets the same PostgreSQL database.
	migrationAdvisoryLockID int64 = 0x5a414641524d414e
	// migrationCleanupTimeout gives an unlock operation a fresh, short context
	// when the command's original context has already been cancelled.
	migrationCleanupTimeout = 5 * time.Second
)

// migrationLedgerSQL creates the minimal runner-owned history table. Business
// inquiry values never belong in this schema-management metadata.
const migrationLedgerSQL = `
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    version bigint PRIMARY KEY,
    name text NOT NULL UNIQUE,
    checksum bytea NOT NULL,
    applied_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT schema_migrations_version_positive
        CHECK (version > 0),
    CONSTRAINT schema_migrations_checksum_length
        CHECK (octet_length(checksum) = 32)
)`

// Migration runner errors describe missing internal dependencies or a
// concurrently held application advisory lock without exposing configuration.
var (
	// errMigrationDatabaseRequired prevents construction of a runner without a
	// database owner. It is a programming error rather than an operator secret.
	errMigrationDatabaseRequired = errors.New(
		"migration runner requires a database pool",
	)
	// errMigrationCatalogRequired prevents a command from reporting success when
	// no embedded schema history was compiled into the executable.
	errMigrationCatalogRequired = errors.New(
		"migration runner requires at least one migration",
	)
	// errMigrationRunnerBusy reports that another process owns the database's
	// application-specific advisory lock.
	errMigrationRunnerBusy = errors.New(
		"another Zafarmand migration command is already running",
	)
)

// migrationStatus pairs one embedded definition with its current ledger state
// for human-readable status output.
type migrationStatus struct {
	// Migration is the trusted definition compiled into this executable.
	Migration migrationDefinition
	// Applied is true only when the ledger contains this exact version, name, and
	// checksum as part of a validated catalog prefix.
	Applied bool
}

// migrationRunner owns schema operations for one database/sql pool and one
// immutable embedded catalog.
//
// It borrows the pool but never closes it; the migration command that opened
// the pool owns that lifecycle. Every operation pins one connection so the
// session advisory lock and its eventual unlock use the same PostgreSQL session.
type migrationRunner struct {
	// database supplies the pinned connection used by each migration operation.
	database *sql.DB
	// catalog is the ordered, validated migration history loaded from embedded SQL.
	catalog []migrationDefinition
}

// migrationSession is one pinned database/sql connection that currently owns
// the Zafarmand PostgreSQL advisory lock.
type migrationSession struct {
	// connection must execute every ledger and migration statement until release.
	connection *sql.Conn
}

// newMigrationRunner validates and copies its dependencies so callers cannot
// mutate the runner's migration order after construction.
func newMigrationRunner(
	database *sql.DB,
	catalog []migrationDefinition,
) (*migrationRunner, error) {
	if database == nil {
		return nil, errMigrationDatabaseRequired
	}
	if len(catalog) == 0 {
		return nil, errMigrationCatalogRequired
	}

	catalogCopy := make(
		[]migrationDefinition,
		len(catalog),
	)
	copy(catalogCopy, catalog)

	return &migrationRunner{
		database: database,
		catalog:  catalogCopy,
	}, nil
}

// Up validates the database ledger and applies every pending migration in
// ascending order.
//
// Each schema change and its ledger insert share one PostgreSQL transaction.
// A failed migration therefore leaves the database at the last fully applied
// version instead of recording partial progress.
func (runner *migrationRunner) Up(
	ctx context.Context,
) (applied []migrationDefinition, err error) {
	session, err := runner.openSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = combineMigrationCleanupError(
			err,
			session.Close(),
		)
	}()

	if err := session.ensureLedger(ctx); err != nil {
		return nil, err
	}

	current, err := session.readApplied(ctx)
	if err != nil {
		return nil, err
	}

	pending, err := planPendingMigrations(
		runner.catalog,
		current,
	)
	if err != nil {
		return nil, err
	}

	for _, migration := range pending {
		if err := session.applyUp(ctx, migration); err != nil {
			return nil, err
		}
	}

	return pending, nil
}

// Status validates the database ledger and reports every catalog migration as
// either applied or pending without executing business-schema migration SQL.
func (runner *migrationRunner) Status(
	ctx context.Context,
) (statuses []migrationStatus, err error) {
	session, err := runner.openSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = combineMigrationCleanupError(
			err,
			session.Close(),
		)
	}()

	if err := session.ensureLedger(ctx); err != nil {
		return nil, err
	}

	current, err := session.readApplied(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := planPendingMigrations(
		runner.catalog,
		current,
	); err != nil {
		return nil, err
	}

	statuses = make([]migrationStatus, 0, len(runner.catalog))
	for index, migration := range runner.catalog {
		statuses = append(
			statuses,
			migrationStatus{
				Migration: migration,
				Applied:   index < len(current),
			},
		)
	}

	return statuses, nil
}

// Down validates the ledger and rolls back exactly its newest applied
// migration.
//
// The command parser requires explicit confirmation before this method is
// called. The reverse SQL and matching ledger deletion share one transaction,
// so a failed rollback preserves both the schema and its applied record.
func (runner *migrationRunner) Down(
	ctx context.Context,
) (rolledBack *migrationDefinition, err error) {
	session, err := runner.openSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = combineMigrationCleanupError(
			err,
			session.Close(),
		)
	}()

	if err := session.ensureLedger(ctx); err != nil {
		return nil, err
	}

	current, err := session.readApplied(ctx)
	if err != nil {
		return nil, err
	}

	migration, err := planLatestRollback(
		runner.catalog,
		current,
	)
	if err != nil {
		return nil, err
	}
	if err := session.applyDown(ctx, *migration); err != nil {
		return nil, err
	}

	return migration, nil
}

// openSession pins one pool connection and acquires the application advisory
// lock before any migration-ledger statement is executed.
func (runner *migrationRunner) openSession(
	ctx context.Context,
) (*migrationSession, error) {
	connection, err := runner.database.Conn(ctx)
	if err != nil {
		return nil, migrationDatabaseError(
			"acquire PostgreSQL connection for migrations",
			err,
		)
	}

	var acquired bool
	err = connection.QueryRowContext(
		ctx,
		"SELECT pg_try_advisory_lock($1)",
		migrationAdvisoryLockID,
	).Scan(&acquired)
	if err != nil {
		_ = connection.Close()

		return nil, migrationDatabaseError(
			"acquire PostgreSQL migration advisory lock",
			err,
		)
	}
	if !acquired {
		_ = connection.Close()

		return nil, errMigrationRunnerBusy
	}

	return &migrationSession{
		connection: connection,
	}, nil
}

// Close releases the advisory lock on the same pinned PostgreSQL session and
// then returns that connection to the database/sql pool.
func (session *migrationSession) Close() error {
	cleanupContext, cancel := context.WithTimeout(
		context.Background(),
		migrationCleanupTimeout,
	)
	defer cancel()

	var unlocked bool
	unlockErr := session.connection.QueryRowContext(
		cleanupContext,
		"SELECT pg_advisory_unlock($1)",
		migrationAdvisoryLockID,
	).Scan(&unlocked)
	closeErr := session.connection.Close()

	if unlockErr != nil || !unlocked {
		return migrationDatabaseError(
			"release PostgreSQL migration advisory lock",
			unlockErr,
		)
	}
	if closeErr != nil {
		return migrationDatabaseError(
			"release PostgreSQL migration connection",
			closeErr,
		)
	}

	return nil
}

// ensureLedger creates the runner-owned metadata table after the advisory lock
// has serialized bootstrap across competing migration processes.
func (session *migrationSession) ensureLedger(
	ctx context.Context,
) error {
	if _, err := session.connection.ExecContext(
		ctx,
		migrationLedgerSQL,
	); err != nil {
		return migrationDatabaseError(
			"ensure PostgreSQL migration ledger",
			err,
		)
	}

	return nil
}

// readApplied loads immutable ledger identities in ascending version order and
// rejects any checksum whose byte length violates the Go-side contract.
func (session *migrationSession) readApplied(
	ctx context.Context,
) ([]appliedMigration, error) {
	rows, err := session.connection.QueryContext(
		ctx,
		`SELECT version, name, checksum
FROM public.schema_migrations
ORDER BY version`,
	)
	if err != nil {
		return nil, migrationDatabaseError(
			"read PostgreSQL migration ledger",
			err,
		)
	}
	defer rows.Close()

	applied := make([]appliedMigration, 0)
	for rows.Next() {
		var migration appliedMigration
		var checksum []byte
		if err := rows.Scan(
			&migration.Version,
			&migration.Name,
			&checksum,
		); err != nil {
			return nil, errors.New(
				"scan PostgreSQL migration ledger",
			)
		}
		if len(checksum) != len(migration.Checksum) {
			return nil, fmt.Errorf(
				"migration version %d has invalid checksum length",
				migration.Version,
			)
		}
		copy(migration.Checksum[:], checksum)
		applied = append(applied, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, migrationDatabaseError(
			"iterate PostgreSQL migration ledger",
			err,
		)
	}

	return applied, nil
}

// applyUp executes one forward migration and records its immutable identity in
// the same transaction.
func (session *migrationSession) applyUp(
	ctx context.Context,
	migration migrationDefinition,
) error {
	transaction, err := session.connection.BeginTx(ctx, nil)
	if err != nil {
		return migrationDatabaseError(
			fmt.Sprintf(
				"begin migration %06d transaction",
				migration.Version,
			),
			err,
		)
	}
	defer func() {
		// Rollback is a safe no-op after Commit and protects every earlier return.
		_ = transaction.Rollback()
	}()

	// pgx uses PostgreSQL's simple protocol for a zero-argument Exec, allowing a
	// reviewed migration file to contain multiple static statements. The catalog
	// separately forbids statements that manipulate this outer transaction.
	if _, err := transaction.ExecContext(
		ctx,
		migration.UpSQL,
	); err != nil {
		return migrationDatabaseError(
			fmt.Sprintf("apply migration %06d", migration.Version),
			err,
		)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO public.schema_migrations (version, name, checksum)
VALUES ($1, $2, $3)`,
		migration.Version,
		migration.Name,
		migration.Checksum[:],
	); err != nil {
		return migrationDatabaseError(
			fmt.Sprintf("record migration %06d", migration.Version),
			err,
		)
	}
	if err := transaction.Commit(); err != nil {
		return migrationDatabaseError(
			fmt.Sprintf("commit migration %06d", migration.Version),
			err,
		)
	}

	return nil
}

// applyDown executes one reverse migration and deletes exactly its ledger row
// in the same transaction.
func (session *migrationSession) applyDown(
	ctx context.Context,
	migration migrationDefinition,
) error {
	transaction, err := session.connection.BeginTx(ctx, nil)
	if err != nil {
		return migrationDatabaseError(
			fmt.Sprintf(
				"begin rollback %06d transaction",
				migration.Version,
			),
			err,
		)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	if _, err := transaction.ExecContext(
		ctx,
		migration.DownSQL,
	); err != nil {
		return migrationDatabaseError(
			fmt.Sprintf("roll back migration %06d", migration.Version),
			err,
		)
	}

	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM public.schema_migrations
WHERE version = $1`,
		migration.Version,
	)
	if err != nil {
		return migrationDatabaseError(
			fmt.Sprintf(
				"remove migration %06d ledger row",
				migration.Version,
			),
			err,
		)
	}
	deletedRows, err := result.RowsAffected()
	if err != nil {
		return migrationDatabaseError(
			fmt.Sprintf(
				"inspect migration %06d ledger deletion",
				migration.Version,
			),
			err,
		)
	}
	if deletedRows != 1 {
		return fmt.Errorf(
			"remove migration %06d ledger row: expected one row",
			migration.Version,
		)
	}
	if err := transaction.Commit(); err != nil {
		return migrationDatabaseError(
			fmt.Sprintf("commit rollback %06d", migration.Version),
			err,
		)
	}

	return nil
}

// migrationDatabaseError exposes safe PostgreSQL diagnostics without returning
// a driver error that could contain connection information or row details.
//
// Cancellation sentinels remain inspectable with errors.Is. Server errors keep
// only their SQLSTATE and primary message; detail, hint, query text, and every
// non-PostgreSQL transport cause remain redacted.
func migrationDatabaseError(operation string, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}

	var postgresError *pgconn.PgError
	if errors.As(cause, &postgresError) {
		message := strings.TrimSpace(postgresError.Message)
		if message == "" {
			message = "server error"
		}

		return fmt.Errorf(
			"%s: PostgreSQL %s (SQLSTATE %s)",
			operation,
			message,
			postgresError.Code,
		)
	}

	return errors.New(operation)
}

// combineMigrationCleanupError preserves the primary schema error while still
// surfacing an unlock/connection cleanup failure after successful work.
func combineMigrationCleanupError(
	primary error,
	cleanup error,
) error {
	if primary != nil {
		return primary
	}

	return cleanup
}
