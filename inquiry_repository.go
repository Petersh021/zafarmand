package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
)

// Repository construction and write errors stay credential- and
// visitor-data-safe at the boundary returned to handlers.
var (
	// errInquiryRepositoryDatabaseRequired prevents an application from starting
	// with a PostgreSQL repository that has no shared database pool.
	errInquiryRepositoryDatabaseRequired = errors.New(
		"create inquiry repository: database is required",
	)
	// errInquiryCreateFailed deliberately does not wrap a driver error. A
	// PostgreSQL constraint error can repeat the rejected row in its detail,
	// which could otherwise expose a visitor's contact information to callers or
	// logs that print the returned error.
	errInquiryCreateFailed = errors.New(
		"create inquiry: database operation failed",
	)
	// errInquirySubmissionConflict reports either that an existing live key belongs
	// to different normalized fields or that a retained purge tombstone blocks the
	// key. It contains no stored or submitted value and lets the handler rotate the
	// key instead of recommending an impossible same-key retry.
	errInquirySubmissionConflict = errors.New(
		"create inquiry: submission key conflicts with different data",
	)
)

// createInquirySQL inserts one validated Contact submission while using its
// opaque submission key as the idempotency boundary.
//
// Every visitor-controlled value is supplied through a PostgreSQL parameter;
// none is interpolated into SQL text. A repeated live key or the digest of a
// deliberately purged key suppresses the insert. A live exact replay can then
// succeed without creating a second inquiry, while a tombstone safely forces a
// fresh submission key.
const createInquirySQL = `INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message
)
SELECT $1, $2, $3, $4, $5
WHERE NOT EXISTS (
    SELECT 1
    FROM public.inquiry_submission_tombstones
    WHERE submission_key_hash = $6
)
ON CONFLICT (submission_key) DO NOTHING`

// exactInquiryReplaySQL distinguishes an exact live-row replay from either a
// changed-payload collision or a deliberately retained purge tombstone.
//
// A hidden form field is untrusted even when the server originally generated
// it. Treating every key collision as a successful replay could therefore tell
// a visitor that changed name or email data was saved when PostgreSQL retained
// a different row. The parameterized comparison recognizes a replay only when
// all persisted visitor fields are identical. The tombstone branch returns
// false because its original personal fields no longer exist to prove an exact
// replay. A tombstone-only match therefore makes the handler rotate the stale
// submission key; a live row retains precedence if defensive drift ever leaves
// both records present.
const exactInquiryReplaySQL = `SELECT exact_replay
FROM (
    SELECT
        0 AS precedence,
        name = $2 AND
        email = $3 AND
        discipline = $4 AND
        message = $5 AS exact_replay
    FROM public.inquiries
    WHERE submission_key = $1

    UNION ALL

    SELECT
        1 AS precedence,
        FALSE AS exact_replay
    FROM public.inquiry_submission_tombstones
    WHERE submission_key_hash = $6
) AS replay_candidates
ORDER BY precedence
LIMIT 1`

// inquirySubmission is the persistence input produced only after the Contact
// handler has normalized and validated every visitor-editable value.
//
// SubmissionKey is an opaque hidden-field value normally issued by the server
// but treated as untrusted after it returns in a POST. It is stored as bytes
// rather than displayed or treated as visitor content, while the four
// remaining fields map directly to the current inquiries table contract.
type inquirySubmission struct {
	// SubmissionKey makes a repeated POST distinguishable from a new inquiry.
	SubmissionKey []byte
	// Name is the normalized visitor name accepted by server-side validation.
	Name string
	// Email is the normalized reply address accepted by server-side validation.
	Email string
	// Discipline is one exact machine value from the Contact form whitelist.
	Discipline string
	// Message is the normalized project summary supplied by the visitor.
	Message string
}

// inquiryCreateResult describes the two successful outcomes of an idempotent
// inquiry insert.
//
// The constants begin above zero so a caller that accidentally inspects the
// result accompanying an error does not confuse its zero value with success.
type inquiryCreateResult uint8

const (
	// inquiryCreateResultCreated means PostgreSQL inserted exactly one new row.
	inquiryCreateResultCreated inquiryCreateResult = iota + 1
	// inquiryCreateResultReplay means the submission key and all normalized
	// fields matched an existing row, so no second row was inserted.
	inquiryCreateResultReplay
)

// inquiryRepository is the narrow persistence behavior needed by the public
// Contact handler.
//
// Accepting the request context allows client disconnects, forced connection
// closure, and any handler-owned deadline to cancel the database operation.
// Graceful HTTP shutdown waits for the handler and its deadline. Future read or
// admin operations can receive separate interfaces instead of expanding this
// one.
type inquiryRepository interface {
	Create(
		context.Context,
		inquirySubmission,
	) (inquiryCreateResult, error)
}

// inquiryTransaction is the exact database/sql transaction surface required
// to serialize a Contact insert against destructive retention and then verify
// a possible replay before committing.
type inquiryTransaction interface {
	ExecContext(
		context.Context,
		string,
		...any,
	) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) inquiryRowScanner
	Commit() error
	Rollback() error
}

// inquiryRowScanner is the result behavior needed to inspect one possible
// idempotency-key collision. *sql.Row satisfies it in production.
type inquiryRowScanner interface {
	Scan(...any) error
}

// sqlInquiryTransaction adapts database/sql's concrete row return to the
// narrow transaction seam used by deterministic repository tests.
type sqlInquiryTransaction struct {
	transaction *sql.Tx
}

// ExecContext delegates the shared lock and INSERT to the owned transaction.
func (transaction *sqlInquiryTransaction) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	return transaction.transaction.ExecContext(ctx, query, arguments...)
}

// QueryRowContext adapts *sql.Row to the narrow replay-scanner interface.
func (transaction *sqlInquiryTransaction) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) inquiryRowScanner {
	return transaction.transaction.QueryRowContext(ctx, query, arguments...)
}

// Commit makes either the new inquiry or confirmed replay transaction durable.
func (transaction *sqlInquiryTransaction) Commit() error {
	return transaction.transaction.Commit()
}

// Rollback releases the shared advisory lock after any incomplete path.
func (transaction *sqlInquiryTransaction) Rollback() error {
	return transaction.transaction.Rollback()
}

// inquiryBeginTransaction keeps pool ownership with the application while
// allowing unit tests to supply a deterministic transaction implementation.
type inquiryBeginTransaction func(
	context.Context,
	*sql.TxOptions,
) (inquiryTransaction, error)

// postgresInquiryRepository writes validated inquiries through the shared
// database pool owned by the long-running application process.
type postgresInquiryRepository struct {
	// begin opens the transaction that owns the shared retention lock, insert,
	// optional replay comparison, and final commit.
	begin inquiryBeginTransaction
}

// Compile-time interface verification protects the application dependency
// contract if the concrete repository's method signature changes later.
var _ inquiryRepository = (*postgresInquiryRepository)(nil)

// newPostgresInquiryRepository borrows the application's shared PostgreSQL
// pool and returns a repository ready for concurrent handler use.
//
// The repository never closes the pool because the process that opened it owns
// its lifecycle. Rejecting nil here converts a missing dependency into a clear
// startup error rather than a panic during the first Contact submission.
func newPostgresInquiryRepository(
	database *sql.DB,
) (*postgresInquiryRepository, error) {
	if database == nil {
		return nil, errInquiryRepositoryDatabaseRequired
	}

	return &postgresInquiryRepository{
		begin: func(
			ctx context.Context,
			options *sql.TxOptions,
		) (inquiryTransaction, error) {
			transaction, err := database.BeginTx(ctx, options)
			if err != nil {
				return nil, err
			}

			return &sqlInquiryTransaction{transaction: transaction}, nil
		},
	}, nil
}

// Create inserts one inquiry or recognizes a replay of an already-stored
// submission key.
//
// The method deliberately maps all executor and result-inspection failures to
// one safe sentinel. Driver details are not wrapped because they may contain a
// failing row and therefore personal information. Callers must ignore the zero
// result whenever the returned error is non-nil.
func (repository *postgresInquiryRepository) Create(
	ctx context.Context,
	submission inquirySubmission,
) (inquiryCreateResult, error) {
	if repository == nil || repository.begin == nil || ctx == nil {
		return 0, errInquiryCreateFailed
	}

	// READ COMMITTED is explicit because the shared-lock statement must finish
	// before PostgreSQL takes the separate INSERT statement's snapshot. If an
	// exclusive purge was already running, that fresh snapshot observes its
	// committed tombstone instead of recreating the deleted personal row.
	transaction, err := repository.begin(
		ctx,
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil || transaction == nil {
		return 0, errInquiryCreateFailed
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(
		ctx,
		acquireInquiryRetentionSharedLockSQL,
		inquiryRetentionAdvisoryLockID,
	); err != nil {
		return 0, errInquiryCreateFailed
	}

	// A retention tombstone stores only this one-way digest. Supplying it to both
	// statements prevents a stale browser POST from recreating personal data
	// after an operator has deliberately purged the original archived inquiry.
	submissionKeyHash := sha256.Sum256(submission.SubmissionKey)

	// The argument order mirrors the numbered placeholders in createInquirySQL.
	// Passing the byte slices and strings as parameters keeps every submitted
	// value separate from the trusted SQL statement.
	result, err := transaction.ExecContext(
		ctx,
		createInquirySQL,
		submission.SubmissionKey,
		submission.Name,
		submission.Email,
		submission.Discipline,
		submission.Message,
		submissionKeyHash[:],
	)
	if err != nil {
		return 0, errInquiryCreateFailed
	}
	if result == nil {
		return 0, errInquiryCreateFailed
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, errInquiryCreateFailed
	}

	// PostgreSQL reports one affected row for a new insert and zero when either
	// the live-key conflict path or the tombstone guard suppresses it. Any other
	// count contradicts this single-row statement and is treated as a safe
	// failure rather than guessed at.
	switch rowsAffected {
	case 1:
		if err := transaction.Commit(); err != nil {
			return 0, errInquiryCreateFailed
		}
		return inquiryCreateResultCreated, nil
	case 0:
		// Only the exact normalized payload of a live row may become replay
		// success. Changed visitor data and tombstoned keys become the same safe,
		// non-retryable conflict so the handler can issue a fresh key.
		row := transaction.QueryRowContext(
			ctx,
			exactInquiryReplaySQL,
			submission.SubmissionKey,
			submission.Name,
			submission.Email,
			submission.Discipline,
			submission.Message,
			submissionKeyHash[:],
		)
		if row == nil {
			return 0, errInquiryCreateFailed
		}

		var exactReplay bool
		err := row.Scan(&exactReplay)
		if err != nil {
			return 0, errInquiryCreateFailed
		}
		if !exactReplay {
			return 0, errInquirySubmissionConflict
		}
		if err := transaction.Commit(); err != nil {
			return 0, errInquiryCreateFailed
		}

		return inquiryCreateResultReplay, nil
	default:
		return 0, errInquiryCreateFailed
	}
}
