package main

import (
	"context"
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
	// errInquirySubmissionConflict reports that an existing key belongs to
	// different normalized fields. It contains no stored or submitted value and
	// lets the handler avoid recommending an impossible same-key retry.
	errInquirySubmissionConflict = errors.New(
		"create inquiry: submission key conflicts with different data",
	)
)

// createInquirySQL inserts one validated Contact submission while using its
// opaque submission key as the idempotency boundary.
//
// Every visitor-controlled value is supplied through a PostgreSQL parameter;
// none is interpolated into SQL text. ON CONFLICT turns a repeated key into a
// successful zero-row result, allowing the handler to use Post/Redirect/Get
// without creating a second inquiry when the same submission is replayed.
const createInquirySQL = `INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (submission_key) DO NOTHING`

// exactInquiryReplaySQL compares a conflicting row with the complete
// normalized submission that attempted to reuse its key.
//
// A hidden form field is untrusted even when the server originally generated
// it. Treating every key collision as a successful replay could therefore tell
// a visitor that changed name or email data was saved when PostgreSQL retained
// a different row. The parameterized comparison recognizes a replay only when
// all persisted visitor fields are identical.
const exactInquiryReplaySQL = `SELECT
    name = $2 AND
    email = $3 AND
    discipline = $4 AND
    message = $5
FROM public.inquiries
WHERE submission_key = $1`

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

// inquiryExecutor is the small part of database/sql used by the PostgreSQL
// repository.
//
// Keeping this seam narrower than *sql.DB lets unit tests prove the SQL,
// parameter order, context propagation, and result handling without a third-
// party mocking package or a live database.
type inquiryExecutor interface {
	ExecContext(
		context.Context,
		string,
		...any,
	) (sql.Result, error)
}

// inquiryRowScanner is the result behavior needed to inspect one possible
// idempotency-key collision.
//
// *sql.Row satisfies this interface in production. A narrow interface lets
// unit tests return deterministic values without a database mocking package.
type inquiryRowScanner interface {
	Scan(...any) error
}

// inquiryQueryRow performs the read-only comparison used after an INSERT is
// suppressed by ON CONFLICT.
//
// A function type is used because database/sql returns the concrete *sql.Row;
// the constructor adapts that concrete result to inquiryRowScanner while tests
// can inject a small recording implementation.
type inquiryQueryRow func(
	context.Context,
	string,
	...any,
) inquiryRowScanner

// postgresInquiryRepository writes validated inquiries through the shared
// database pool owned by the long-running application process.
type postgresInquiryRepository struct {
	// executor is *sql.DB in production and a narrow recording stub in tests.
	executor inquiryExecutor
	// queryRow verifies that a zero-row insert was an exact replay rather than a
	// key collision carrying different visitor data.
	queryRow inquiryQueryRow
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
		executor: database,
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) inquiryRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
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
	if repository == nil || repository.executor == nil {
		return 0, errInquiryCreateFailed
	}

	// The argument order mirrors the numbered placeholders in createInquirySQL.
	// Passing the byte slice and strings as parameters keeps every submitted
	// value separate from the trusted SQL statement.
	result, err := repository.executor.ExecContext(
		ctx,
		createInquirySQL,
		submission.SubmissionKey,
		submission.Name,
		submission.Email,
		submission.Discipline,
		submission.Message,
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

	// PostgreSQL reports one affected row for a new insert and zero when ON
	// CONFLICT suppresses a key collision. Any other count contradicts this
	// single-row statement and is treated as a safe failure rather than guessed
	// at.
	switch rowsAffected {
	case 1:
		return inquiryCreateResultCreated, nil
	case 0:
		if repository.queryRow == nil {
			return 0, errInquiryCreateFailed
		}

		// Only the exact normalized payload may turn the collision into replay
		// success. A different name, email, discipline, or message leaves the
		// original row untouched and becomes a distinct non-retryable conflict.
		row := repository.queryRow(
			ctx,
			exactInquiryReplaySQL,
			submission.SubmissionKey,
			submission.Name,
			submission.Email,
			submission.Discipline,
			submission.Message,
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

		return inquiryCreateResultReplay, nil
	default:
		return 0, errInquiryCreateFailed
	}
}
