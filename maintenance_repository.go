package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Maintenance errors are stable, credential-free categories. PostgreSQL
// diagnostics are never wrapped because they may contain connection details or
// values from a rejected private row.
var (
	errMaintenanceRepositoryDatabaseRequired = errors.New(
		"create maintenance repository: database is required",
	)
	errMaintenanceRepositoryInvalid = errors.New(
		"maintenance request is invalid",
	)
	errMaintenanceDatabaseConfirmationMismatch = errors.New(
		"maintenance database confirmation does not match",
	)
	errMaintenanceRepositoryFailed = errors.New(
		"maintenance database operation failed",
	)
)

// Maintenance SQL projects only aggregate counts or the database identity used
// for destructive confirmation. No visitor field or token reaches command
// output, while every cutoff and record identity remains a bound parameter.
const (
	maintenanceCurrentDatabaseSQL = `SELECT current_database()`

	inspectMaintenanceRetentionSQL = `SELECT
    (
        SELECT COUNT(*)
        FROM public.admin_sessions
        WHERE expires_at <= CURRENT_TIMESTAMP
    ),
    (
        SELECT COUNT(*)
        FROM public.inquiries
        WHERE status = 'archived'
          AND updated_at < $1
    )`

	deleteExpiredAdminSessionsSQL = `WITH deleted AS (
    DELETE FROM public.admin_sessions
    WHERE expires_at <= CURRENT_TIMESTAMP
    RETURNING 1
)
SELECT COUNT(*) FROM deleted`

	// purgeArchivedInquiriesBeforeSQL locks every eligible row, records or reuses
	// its one-way submission-key tombstone, and only then deletes the private row.
	purgeArchivedInquiriesBeforeSQL = `WITH eligible AS MATERIALIZED (
    SELECT
        id,
        sha256(submission_key) AS submission_key_hash
    FROM public.inquiries
    WHERE status = 'archived'
      AND updated_at < $1
    FOR UPDATE
),
recorded AS (
    INSERT INTO public.inquiry_submission_tombstones (
        submission_key_hash
    )
    SELECT submission_key_hash
    FROM eligible
    ON CONFLICT (submission_key_hash) DO NOTHING
    RETURNING submission_key_hash
),
protected AS MATERIALIZED (
    SELECT eligible.id
    FROM eligible
    JOIN recorded
      USING (submission_key_hash)

    UNION ALL

    SELECT eligible.id
    FROM eligible
    JOIN public.inquiry_submission_tombstones AS tombstones
      USING (submission_key_hash)
),
deleted AS (
    DELETE FROM public.inquiries AS inquiries
    USING protected
    WHERE inquiries.id = protected.id
    RETURNING 1
)
SELECT COUNT(*) FROM deleted`

	// purgeArchivedInquiryByIDSQL applies the same tombstone-before-delete rule to
	// one archived ID and leaves missing or non-archived records untouched.
	purgeArchivedInquiryByIDSQL = `WITH eligible AS MATERIALIZED (
    SELECT
        id,
        sha256(submission_key) AS submission_key_hash
    FROM public.inquiries
    WHERE id = $1
      AND status = 'archived'
    FOR UPDATE
),
recorded AS (
    INSERT INTO public.inquiry_submission_tombstones (
        submission_key_hash
    )
    SELECT submission_key_hash
    FROM eligible
    ON CONFLICT (submission_key_hash) DO NOTHING
    RETURNING submission_key_hash
),
protected AS MATERIALIZED (
    SELECT eligible.id
    FROM eligible
    JOIN recorded
      USING (submission_key_hash)

    UNION ALL

    SELECT eligible.id
    FROM eligible
    JOIN public.inquiry_submission_tombstones AS tombstones
      USING (submission_key_hash)
),
deleted AS (
    DELETE FROM public.inquiries AS inquiries
    USING protected
    WHERE inquiries.id = protected.id
    RETURNING 1
)
SELECT COUNT(*) FROM deleted`
)

// maintenanceRetentionResult contains only aggregate operational evidence.
type maintenanceRetentionResult struct {
	// ExpiredSessions counts only rows no authentication request can use.
	ExpiredSessions int64
	// ArchivedInquiries counts only archived rows strictly before the cutoff.
	ArchivedInquiries int64
}

// maintenancePurgeInquiryResult deliberately does not return the supplied ID
// or any stored value. A false result neutrally covers missing and non-archived
// records.
type maintenancePurgeInquiryResult struct {
	// Purged is true only when one exact archived row was removed.
	Purged bool
}

// maintenanceRepository is the offline data-minimization authority. It remains
// separate from the server repositories so public and admin HTTP handlers never
// acquire deletion capability.
type maintenanceRepository interface {
	InspectRetention(
		context.Context,
		time.Time,
	) (maintenanceRetentionResult, error)
	ApplyRetention(
		context.Context,
		time.Time,
		string,
	) (maintenanceRetentionResult, error)
	PurgeInquiry(
		context.Context,
		int64,
		string,
	) (maintenancePurgeInquiryResult, error)
}

// maintenanceTransaction is the exact database/sql transaction surface needed
// to keep confirmation, tombstone creation, and deletion in one commit.
type maintenanceTransaction interface {
	ExecContext(
		context.Context,
		string,
		...any,
	) (sql.Result, error)
	QueryRowContext(
		context.Context,
		string,
		...any,
	) maintenanceRowScanner
	Commit() error
	Rollback() error
}

// maintenanceRowScanner is the one-row result behavior shared by *sql.Row and
// deterministic test doubles.
type maintenanceRowScanner interface {
	Scan(...any) error
}

// sqlMaintenanceTransaction adapts database/sql's concrete *sql.Row return to
// the narrow scanner interface used by deterministic repository tests.
type sqlMaintenanceTransaction struct {
	transaction *sql.Tx
}

// ExecContext delegates lock statements to the owned database transaction.
func (transaction *sqlMaintenanceTransaction) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	return transaction.transaction.ExecContext(ctx, query, arguments...)
}

// QueryRowContext adapts *sql.Row to the narrow maintenance scanner interface.
func (transaction *sqlMaintenanceTransaction) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) maintenanceRowScanner {
	return transaction.transaction.QueryRowContext(ctx, query, arguments...)
}

// Commit makes the confirmed maintenance transaction durable.
func (transaction *sqlMaintenanceTransaction) Commit() error {
	return transaction.transaction.Commit()
}

// Rollback releases writes and advisory locks after any incomplete operation.
func (transaction *sqlMaintenanceTransaction) Rollback() error {
	return transaction.transaction.Rollback()
}

// maintenanceBeginTransaction adapts a command-owned pool to a testable
// transaction factory without widening the repository to arbitrary SQL.
type maintenanceBeginTransaction func(
	context.Context,
	*sql.TxOptions,
) (maintenanceTransaction, error)

// postgresMaintenanceRepository borrows a command-owned pool and opens one
// transaction for each preview or mutation.
type postgresMaintenanceRepository struct {
	// begin opens the single transaction that owns either a read-only preview or
	// destructive confirmation, advisory locking, and all writes.
	begin maintenanceBeginTransaction
}

// Compile-time interface verification protects command composition if the
// concrete repository signature changes.
var _ maintenanceRepository = (*postgresMaintenanceRepository)(nil)

// newPostgresMaintenanceRepository borrows a command-owned pool without
// closing it and prepares a fresh transaction factory for each operation.
func newPostgresMaintenanceRepository(
	database *sql.DB,
) (*postgresMaintenanceRepository, error) {
	if database == nil {
		return nil, errMaintenanceRepositoryDatabaseRequired
	}

	return &postgresMaintenanceRepository{
		begin: func(
			ctx context.Context,
			options *sql.TxOptions,
		) (maintenanceTransaction, error) {
			transaction, err := database.BeginTx(ctx, options)
			if err != nil {
				return nil, err
			}

			return &sqlMaintenanceTransaction{transaction: transaction}, nil
		},
	}, nil
}

// InspectRetention performs a read-only transaction so preview code cannot
// accidentally gain write authority through a future statement change.
func (repository *postgresMaintenanceRepository) InspectRetention(
	ctx context.Context,
	cutoff time.Time,
) (maintenanceRetentionResult, error) {
	if ctx == nil || cutoff.IsZero() {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryInvalid
	}
	transaction, err := repository.beginTransaction(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return maintenanceRetentionResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()

	var result maintenanceRetentionResult
	err = transaction.QueryRowContext(
		ctx,
		inspectMaintenanceRetentionSQL,
		cutoff,
	).Scan(
		&result.ExpiredSessions,
		&result.ArchivedInquiries,
	)
	if err != nil || !result.valid() {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryFailed
	}
	if err := transaction.Commit(); err != nil {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryFailed
	}

	return result, nil
}

// ApplyRetention confirms the server-reported database and commits expired
// session cleanup plus archived-inquiry tombstoning/deletion atomically.
func (repository *postgresMaintenanceRepository) ApplyRetention(
	ctx context.Context,
	cutoff time.Time,
	confirmedDatabase string,
) (maintenanceRetentionResult, error) {
	if ctx == nil || cutoff.IsZero() || confirmedDatabase == "" {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryInvalid
	}
	// READ COMMITTED makes the exclusive advisory-lock statement a distinct
	// boundary before either deletion statement takes its database snapshot.
	transaction, err := repository.beginTransaction(
		ctx,
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		return maintenanceRetentionResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()

	if err := confirmMaintenanceDatabase(
		ctx,
		transaction,
		confirmedDatabase,
	); err != nil {
		return maintenanceRetentionResult{}, err
	}
	if err := acquireInquiryRetentionExclusiveLock(
		ctx,
		transaction,
	); err != nil {
		return maintenanceRetentionResult{}, err
	}

	var result maintenanceRetentionResult
	if err := transaction.QueryRowContext(
		ctx,
		deleteExpiredAdminSessionsSQL,
	).Scan(&result.ExpiredSessions); err != nil {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryFailed
	}
	if err := transaction.QueryRowContext(
		ctx,
		purgeArchivedInquiriesBeforeSQL,
		cutoff,
	).Scan(&result.ArchivedInquiries); err != nil || !result.valid() {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryFailed
	}
	if err := transaction.Commit(); err != nil {
		return maintenanceRetentionResult{}, errMaintenanceRepositoryFailed
	}

	return result, nil
}

// PurgeInquiry removes one deliberately archived inquiry while leaving a
// digest-only replay tombstone. Missing and non-archived IDs are neutral no-ops.
func (repository *postgresMaintenanceRepository) PurgeInquiry(
	ctx context.Context,
	inquiryID int64,
	confirmedDatabase string,
) (maintenancePurgeInquiryResult, error) {
	if ctx == nil || inquiryID <= 0 || confirmedDatabase == "" {
		return maintenancePurgeInquiryResult{}, errMaintenanceRepositoryInvalid
	}
	transaction, err := repository.beginTransaction(
		ctx,
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		return maintenancePurgeInquiryResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()

	if err := confirmMaintenanceDatabase(
		ctx,
		transaction,
		confirmedDatabase,
	); err != nil {
		return maintenancePurgeInquiryResult{}, err
	}
	if err := acquireInquiryRetentionExclusiveLock(
		ctx,
		transaction,
	); err != nil {
		return maintenancePurgeInquiryResult{}, err
	}

	var deleted int64
	if err := transaction.QueryRowContext(
		ctx,
		purgeArchivedInquiryByIDSQL,
		inquiryID,
	).Scan(&deleted); err != nil || deleted < 0 || deleted > 1 {
		return maintenancePurgeInquiryResult{}, errMaintenanceRepositoryFailed
	}
	if err := transaction.Commit(); err != nil {
		return maintenancePurgeInquiryResult{}, errMaintenanceRepositoryFailed
	}

	return maintenancePurgeInquiryResult{Purged: deleted == 1}, nil
}

// beginTransaction validates the repository seam and collapses driver details
// into the maintenance-safe failure category.
func (repository *postgresMaintenanceRepository) beginTransaction(
	ctx context.Context,
	options *sql.TxOptions,
) (maintenanceTransaction, error) {
	if repository == nil || repository.begin == nil {
		return nil, errMaintenanceRepositoryFailed
	}
	transaction, err := repository.begin(ctx, options)
	if err != nil || transaction == nil {
		return nil, errMaintenanceRepositoryFailed
	}

	return transaction, nil
}

// confirmMaintenanceDatabase compares the operator acknowledgement with
// PostgreSQL's identity inside the same transaction that may later delete data.
func confirmMaintenanceDatabase(
	ctx context.Context,
	transaction maintenanceTransaction,
	confirmedDatabase string,
) error {
	var currentDatabase string
	if err := transaction.QueryRowContext(
		ctx,
		maintenanceCurrentDatabaseSQL,
	).Scan(&currentDatabase); err != nil || currentDatabase == "" {
		return errMaintenanceRepositoryFailed
	}
	if currentDatabase != confirmedDatabase {
		return errMaintenanceDatabaseConfirmationMismatch
	}

	return nil
}

// acquireInquiryRetentionExclusiveLock waits for every cooperating current
// Contact transaction and prevents a new cooperating writer from starting its
// INSERT snapshot until maintenance commits its tombstone and deletion. The
// offline runbook remains mandatory for older or out-of-band writers.
func acquireInquiryRetentionExclusiveLock(
	ctx context.Context,
	transaction maintenanceTransaction,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		acquireInquiryRetentionExclusiveLockSQL,
		inquiryRetentionAdvisoryLockID,
	); err != nil {
		return errMaintenanceRepositoryFailed
	}

	return nil
}

// valid rejects impossible negative aggregate counts before they reach output.
func (result maintenanceRetentionResult) valid() bool {
	return result.ExpiredSessions >= 0 && result.ArchivedInquiries >= 0
}
