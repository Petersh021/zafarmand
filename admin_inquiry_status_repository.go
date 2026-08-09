package main

import (
	"context"
	"database/sql"
	"errors"
)

// Status-update construction and operation errors are deliberately safe to
// return across the repository boundary. None wraps PostgreSQL diagnostics,
// which can contain connection details or values from a rejected database row.
var (
	// errAdminInquiryStatusUpdaterDatabaseRequired rejects construction without
	// the application-owned PostgreSQL pool.
	errAdminInquiryStatusUpdaterDatabaseRequired = errors.New(
		"create admin inquiry status updater: database is required",
	)
	// errAdminInquiryStatusUpdateInvalid identifies a nil context, non-positive
	// inquiry identity, or status outside the database's closed vocabulary before
	// any SQL operation is attempted.
	errAdminInquiryStatusUpdateInvalid = errors.New(
		"admin inquiry status update is invalid",
	)
	// errAdminInquiryStatusUpdateFailed collapses executor and result-inspection
	// failures without retaining private driver diagnostics.
	errAdminInquiryStatusUpdateFailed = errors.New(
		"admin inquiry status update failed",
	)
)

// updateAdminInquiryStatusSQL changes only the two columns required by the
// protected review workflow. Both the inquiry identity and requested state are
// positional parameters; administrator-controlled request text is never
// interpolated into the trusted SQL statement.
//
// Repeating the current status is an idempotent success. PostgreSQL still
// reports the matched row, while the CASE expression preserves updated_at so
// the timestamp continues to describe a real state change rather than a repeat
// click. A genuine transition uses the database clock and remains protected by
// the existing inquiries_timestamp_order constraint.
const updateAdminInquiryStatusSQL = `UPDATE public.inquiries
SET
    status = $2,
    updated_at = CASE
        WHEN status = $2 THEN updated_at
        ELSE CURRENT_TIMESTAMP
    END
WHERE id = $1`

// adminInquiryStatusUpdater is the smallest persistence authority needed by
// Stage 17. Keeping it separate from the public Contact writer and protected
// Stage 16 reader prevents either caller from acquiring status-mutation access.
type adminInquiryStatusUpdater interface {
	// UpdateStatus stores one supported review state for one positive inquiry ID.
	UpdateStatus(context.Context, int64, inquiryStatus) error
}

// adminInquiryStatusExecutor is the narrow part of database/sql needed by the
// updater. A small interface lets unit tests verify SQL, positional parameters,
// context propagation, and affected-row handling without a mocking dependency.
type adminInquiryStatusExecutor interface {
	// ExecContext runs one trusted parameterized status statement.
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// postgresAdminInquiryStatusUpdater borrows the process-wide PostgreSQL pool
// for concurrent protected requests. The process that opens the pool remains
// responsible for closing it after graceful HTTP shutdown.
type postgresAdminInquiryStatusUpdater struct {
	// executor is *sql.DB in production and a recording seam in unit tests.
	executor adminInquiryStatusExecutor
}

// Compile-time interface verification catches an accidental method-signature
// change before application composition can lose the status-write dependency.
var _ adminInquiryStatusUpdater = (*postgresAdminInquiryStatusUpdater)(nil)

// newPostgresAdminInquiryStatusUpdater adapts the application-owned database
// pool to the isolated status-write contract. Construction neither opens a new
// connection nor runs SQL; the shared pool connects only when used.
func newPostgresAdminInquiryStatusUpdater(
	database *sql.DB,
) (*postgresAdminInquiryStatusUpdater, error) {
	if database == nil {
		// Failing at startup is safer than retaining a repository that will panic
		// or silently fail during the first administrator mutation.
		return nil, errAdminInquiryStatusUpdaterDatabaseRequired
	}

	return &postgresAdminInquiryStatusUpdater{executor: database}, nil
}

// UpdateStatus applies one validated state to one inquiry and classifies the
// exact zero-, one-, or unexpected-row PostgreSQL outcome. The caller's context
// is forwarded unchanged so its HTTP deadline or cancellation controls the
// database operation.
func (updater *postgresAdminInquiryStatusUpdater) UpdateStatus(
	ctx context.Context,
	inquiryID int64,
	status inquiryStatus,
) error {
	if ctx == nil || inquiryID <= 0 || !status.valid() {
		// Request-derived identifiers and statuses remain untrusted even after an
		// HTTP handler has parsed them. Rejecting them here gives every future
		// caller the same fail-closed persistence boundary.
		return errAdminInquiryStatusUpdateInvalid
	}
	if updater == nil || updater.executor == nil {
		// A broken injected implementation is an operational failure, not invalid
		// administrator input and not evidence that an inquiry is absent.
		return errAdminInquiryStatusUpdateFailed
	}

	result, err := updater.executor.ExecContext(
		ctx,
		updateAdminInquiryStatusSQL,
		inquiryID,
		string(status),
	)
	if err != nil {
		// Do not wrap the driver error: constraint and connection diagnostics can
		// include database configuration or data from the affected record.
		return errAdminInquiryStatusUpdateFailed
	}
	if result == nil {
		// database/sql normally returns a result on success, but treating a broken
		// substitute defensively avoids a nil-interface panic below.
		return errAdminInquiryStatusUpdateFailed
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Result-inspection diagnostics receive the same redaction as execution
		// failures because callers need only a stable safe category.
		return errAdminInquiryStatusUpdateFailed
	}

	switch rowsAffected {
	case 0:
		// The primary-key predicate matched no inquiry. Reusing the reader's safe
		// not-found sentinel gives GET and POST handlers one missing-record category.
		return errAdminInquiryNotFound
	case 1:
		// PostgreSQL counts a matched row even when the requested status already
		// equals the stored status. Both a real transition and that intentional
		// idempotent replay are successful outcomes for this focused interface.
		return nil
	default:
		// A primary-key update cannot legitimately affect multiple or negative
		// rows. Refuse an impossible result instead of guessing that it succeeded.
		return errAdminInquiryStatusUpdateFailed
	}
}
