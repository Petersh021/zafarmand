package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// inquiryExecutorStub records one repository call and returns a controlled SQL
// result or driver-like error.
//
// Its fields are intentionally explicit so each test can prove that Create
// forwards the original context, trusted statement, and parameters exactly
// once without relying on a third-party database mocking package.
type inquiryExecutorStub struct {
	// result is returned when execError is nil.
	result sql.Result
	// execError simulates a failure from database/sql or the pgx driver.
	execError error
	// calls records how many statements the repository attempted.
	calls int
	// context records the exact context received by ExecContext.
	context context.Context
	// query records the complete SQL statement received by ExecContext.
	query string
	// arguments records the positional values supplied to the statement.
	arguments []any
}

// inquiryRowScannerStub supplies one boolean comparison result or a controlled
// database-like error to the replay verification path.
type inquiryRowScannerStub struct {
	// exactReplay is copied into the repository's boolean destination.
	exactReplay bool
	// scanError simulates a missing row, canceled query, or driver failure.
	scanError error
	// calls proves the comparison row is inspected exactly once.
	calls int
}

// Scan implements inquiryRowScanner without exposing any simulated driver
// detail through the repository boundary.
func (stub *inquiryRowScannerStub) Scan(destinations ...any) error {
	stub.calls++
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 1 {
		return errors.New("replay scanner expected one destination")
	}

	exactReplay, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("replay scanner expected a boolean destination")
	}
	*exactReplay = stub.exactReplay

	return nil
}

// inquiryQueryRowStub records the read-only replay comparison separately from
// the INSERT executor so tests can verify both trusted SQL statements and
// their parameter order.
type inquiryQueryRowStub struct {
	// row is returned to the repository for Scan.
	row inquiryRowScanner
	// calls records how many comparison queries were attempted.
	calls int
	// context records the exact context received by the comparison.
	context context.Context
	// query records the trusted comparison SQL.
	query string
	// arguments records its positional values.
	arguments []any
}

// QueryRow matches inquiryQueryRow and records the complete invocation before
// returning the configured scanner.
func (stub *inquiryQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) inquiryRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// ExecContext implements inquiryExecutor and records its complete invocation
// before returning the configured result.
func (stub *inquiryExecutorStub) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.result, stub.execError
}

// inquirySQLResultStub controls RowsAffected and records whether the repository
// ever requests an unsupported generated identifier.
type inquirySQLResultStub struct {
	// rowsAffected is the count returned by RowsAffected when its error is nil.
	rowsAffected int64
	// rowsAffectedError simulates result-inspection failure.
	rowsAffectedError error
	// lastInsertIDCalls protects the INSERT contract from relying on an API that
	// PostgreSQL does not support through database/sql.
	lastInsertIDCalls int
	// rowsAffectedCalls proves the successful Exec result is inspected once.
	rowsAffectedCalls int
}

// LastInsertId satisfies sql.Result while recording an unexpected call.
func (result *inquirySQLResultStub) LastInsertId() (int64, error) {
	result.lastInsertIDCalls++

	return 0, errors.New("LastInsertId is unsupported")
}

// RowsAffected satisfies sql.Result and returns the test's controlled count or
// error.
func (result *inquirySQLResultStub) RowsAffected() (int64, error) {
	result.rowsAffectedCalls++

	return result.rowsAffected, result.rowsAffectedError
}

// inquiryRepositoryContextKey is a private comparable type used to prove that
// Create forwards the caller's exact context instead of replacing it.
type inquiryRepositoryContextKey struct{}

// TestNewPostgresInquiryRepository verifies constructor dependency validation
// and confirms a valid pool is borrowed rather than replaced.
func TestNewPostgresInquiryRepository(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		repository, err := newPostgresInquiryRepository(nil)
		if !errors.Is(err, errInquiryRepositoryDatabaseRequired) {
			t.Fatalf(
				"error: got %v, want database-required sentinel",
				err,
			)
		}
		if repository != nil {
			t.Errorf(
				"repository: got %#v, want nil",
				repository,
			)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		// A zero database/sql value is sufficient for constructor identity testing;
		// no method is called and no connection is opened by the constructor.
		database := new(sql.DB)
		repository, err := newPostgresInquiryRepository(database)
		if err != nil {
			t.Fatalf("create repository: %v", err)
		}
		if repository.executor != database {
			t.Error("repository did not borrow the supplied database pool")
		}
	})
}

// TestPostgresInquiryRepositoryCreate verifies the complete successful insert
// contract, including SQL text, positional argument order, context propagation,
// and the additional exact-payload check required before replay success.
func TestPostgresInquiryRepositoryCreate(t *testing.T) {
	tests := []struct {
		// name labels the affected-row outcome in verbose test output.
		name string
		// rowsAffected is the exact count reported by the SQL result.
		rowsAffected int64
		// expectedResult is the repository's semantic outcome.
		expectedResult inquiryCreateResult
		// verifyReplay says the zero-row INSERT must run the comparison query.
		verifyReplay bool
	}{
		{
			name:           "created",
			rowsAffected:   1,
			expectedResult: inquiryCreateResultCreated,
		},
		{
			name:           "exact replay",
			rowsAffected:   0,
			expectedResult: inquiryCreateResultReplay,
			verifyReplay:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sqlResult := &inquirySQLResultStub{
				rowsAffected: test.rowsAffected,
			}
			executor := &inquiryExecutorStub{
				result: sqlResult,
			}
			replayRow := &inquiryRowScannerStub{
				exactReplay: true,
			}
			queryRow := &inquiryQueryRowStub{
				row: replayRow,
			}
			repository := &postgresInquiryRepository{
				executor: executor,
				queryRow: queryRow.QueryRow,
			}
			submission := inquirySubmission{
				// The migration requires the same 32-byte key width produced by the
				// submission-token helper at the HTTP boundary.
				SubmissionKey: []byte(
					"0123456789abcdef0123456789abcdef",
				),
				Name:       "Test Visitor",
				Email:      "visitor@example.com",
				Discipline: "architecture-design",
				Message:    "A carefully normalized project inquiry.",
			}
			ctx := context.WithValue(
				context.Background(),
				inquiryRepositoryContextKey{},
				"request-context-marker",
			)

			createResult, err := repository.Create(ctx, submission)
			if err != nil {
				t.Fatalf("create inquiry: %v", err)
			}
			if createResult != test.expectedResult {
				t.Errorf(
					"result: got %d, want %d",
					createResult,
					test.expectedResult,
				)
			}
			if executor.calls != 1 {
				t.Errorf(
					"ExecContext calls: got %d, want 1",
					executor.calls,
				)
			}
			if executor.context != ctx {
				t.Error("ExecContext did not receive the caller's exact context")
			}

			// Keep a test-owned copy of the statement so an accidental change to the
			// production constant cannot make the assertion pass automatically.
			const expectedSQL = `INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (submission_key) DO NOTHING`
			if executor.query != expectedSQL {
				t.Errorf(
					"query:\n%s\nwant:\n%s",
					executor.query,
					expectedSQL,
				)
			}

			expectedArguments := []any{
				submission.SubmissionKey,
				submission.Name,
				submission.Email,
				submission.Discipline,
				submission.Message,
			}
			if !reflect.DeepEqual(
				executor.arguments,
				expectedArguments,
			) {
				t.Errorf(
					"arguments: got %#v, want %#v",
					executor.arguments,
					expectedArguments,
				)
			}
			if sqlResult.rowsAffectedCalls != 1 {
				t.Errorf(
					"RowsAffected calls: got %d, want 1",
					sqlResult.rowsAffectedCalls,
				)
			}
			if sqlResult.lastInsertIDCalls != 0 {
				t.Errorf(
					"LastInsertId calls: got %d, want 0",
					sqlResult.lastInsertIDCalls,
				)
			}

			if !test.verifyReplay {
				if queryRow.calls != 0 || replayRow.calls != 0 {
					t.Error("new insert unnecessarily queried for a replay")
				}
				return
			}

			if queryRow.calls != 1 || replayRow.calls != 1 {
				t.Errorf(
					"replay query/scan calls: got %d/%d, want 1/1",
					queryRow.calls,
					replayRow.calls,
				)
			}
			if queryRow.context != ctx {
				t.Error("replay comparison did not receive the caller's context")
			}
			const expectedReplaySQL = `SELECT
    name = $2 AND
    email = $3 AND
    discipline = $4 AND
    message = $5
FROM public.inquiries
WHERE submission_key = $1`
			if queryRow.query != expectedReplaySQL {
				t.Errorf(
					"replay query:\n%s\nwant:\n%s",
					queryRow.query,
					expectedReplaySQL,
				)
			}
			if !reflect.DeepEqual(queryRow.arguments, expectedArguments) {
				t.Errorf(
					"replay arguments: got %#v, want %#v",
					queryRow.arguments,
					expectedArguments,
				)
			}
		})
	}
}

// TestPostgresInquiryRepositoryCreateFailures verifies every lower-level
// failure becomes one generic sentinel that contains no credential or visitor
// data from a simulated driver error.
func TestPostgresInquiryRepositoryCreateFailures(t *testing.T) {
	const sensitiveDetail = "password=database-secret visitor@example.com"

	tests := []struct {
		// name labels the failing repository boundary.
		name string
		// repository is the concrete value exercised by the test.
		repository *postgresInquiryRepository
		// expectedExecCalls proves whether SQL should have been attempted.
		expectedExecCalls int
		// expectedRowsAffectedCalls proves result inspection stops at the error.
		expectedRowsAffectedCalls int
	}{
		{
			name:       "nil repository",
			repository: nil,
		},
		{
			name:       "nil executor",
			repository: &postgresInquiryRepository{},
		},
		{
			name: "execute failure",
			repository: &postgresInquiryRepository{
				executor: &inquiryExecutorStub{
					execError: errors.New(sensitiveDetail),
				},
			},
			expectedExecCalls: 1,
		},
		{
			name: "nil SQL result",
			repository: &postgresInquiryRepository{
				executor: &inquiryExecutorStub{},
			},
			expectedExecCalls: 1,
		},
		{
			name: "RowsAffected failure",
			repository: &postgresInquiryRepository{
				executor: &inquiryExecutorStub{
					result: &inquirySQLResultStub{
						rowsAffectedError: errors.New(sensitiveDetail),
					},
				},
			},
			expectedExecCalls:         1,
			expectedRowsAffectedCalls: 1,
		},
		{
			name: "impossible affected-row count",
			repository: &postgresInquiryRepository{
				executor: &inquiryExecutorStub{
					result: &inquirySQLResultStub{
						rowsAffected: 2,
					},
				},
			},
			expectedExecCalls:         1,
			expectedRowsAffectedCalls: 1,
		},
		{
			name: "replay comparison unavailable",
			repository: &postgresInquiryRepository{
				executor: &inquiryExecutorStub{
					result: &inquirySQLResultStub{
						rowsAffected: 0,
					},
				},
			},
			expectedExecCalls:         1,
			expectedRowsAffectedCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.repository.Create(
				context.Background(),
				inquirySubmission{},
			)
			if !errors.Is(err, errInquiryCreateFailed) {
				t.Fatalf(
					"error: got %v, want generic create sentinel",
					err,
				)
			}
			if result != 0 {
				t.Errorf("result: got %d, want zero on failure", result)
			}
			if strings.Contains(err.Error(), sensitiveDetail) ||
				strings.Contains(err.Error(), "visitor@example.com") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive driver detail: %q", err)
			}

			if test.repository == nil || test.repository.executor == nil {
				return
			}
			executor, ok := test.repository.executor.(*inquiryExecutorStub)
			if !ok {
				t.Fatal("test repository does not contain its recording executor")
			}
			if executor.calls != test.expectedExecCalls {
				t.Errorf(
					"ExecContext calls: got %d, want %d",
					executor.calls,
					test.expectedExecCalls,
				)
			}
			if sqlResult, ok := executor.result.(*inquirySQLResultStub); ok &&
				sqlResult.rowsAffectedCalls != test.expectedRowsAffectedCalls {
				t.Errorf(
					"RowsAffected calls: got %d, want %d",
					sqlResult.rowsAffectedCalls,
					test.expectedRowsAffectedCalls,
				)
			}
		})
	}
}

// TestPostgresInquiryRepositoryRejectsKeyCollisions verifies a conflicting
// key is successful only when PostgreSQL confirms the stored name, email,
// discipline, and message all match the attempted normalized payload.
func TestPostgresInquiryRepositoryRejectsKeyCollisions(t *testing.T) {
	const sensitiveDetail = "visitor@example.com password=database-secret"

	tests := []struct {
		// name labels the unsafe or unreadable collision outcome.
		name string
		// row is returned by the comparison query; nil tests a defensive boundary.
		row inquiryRowScanner
		// expectedError distinguishes a permanent payload conflict from an
		// unreadable comparison that may be transient.
		expectedError error
	}{
		{
			name: "different persisted payload",
			row: &inquiryRowScannerStub{
				exactReplay: false,
			},
			expectedError: errInquirySubmissionConflict,
		},
		{
			name: "comparison query failure",
			row: &inquiryRowScannerStub{
				scanError: errors.New(sensitiveDetail),
			},
			expectedError: errInquiryCreateFailed,
		},
		{
			name:          "missing comparison row",
			expectedError: errInquiryCreateFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &inquiryExecutorStub{
				result: &inquirySQLResultStub{
					rowsAffected: 0,
				},
			}
			queryRow := &inquiryQueryRowStub{
				row: test.row,
			}
			repository := &postgresInquiryRepository{
				executor: executor,
				queryRow: queryRow.QueryRow,
			}
			submission := inquirySubmission{
				SubmissionKey: []byte(
					"0123456789abcdef0123456789abcdef",
				),
				Name:       "Expected Visitor",
				Email:      "expected@example.com",
				Discipline: "products",
				Message:    "The complete expected payload.",
			}

			result, err := repository.Create(
				context.Background(),
				submission,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			if result != 0 {
				t.Errorf("result: got %d, want zero on collision", result)
			}
			if strings.Contains(err.Error(), "visitor@example.com") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("collision error exposes driver detail: %q", err)
			}
			if executor.calls != 1 || queryRow.calls != 1 {
				t.Errorf(
					"insert/comparison calls: got %d/%d, want 1/1",
					executor.calls,
					queryRow.calls,
				)
			}
		})
	}
}
