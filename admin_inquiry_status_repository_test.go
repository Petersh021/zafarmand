package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// adminInquiryStatusContextKey is a private comparable marker used to prove
// that UpdateStatus forwards the caller's exact context to database/sql.
type adminInquiryStatusContextKey struct{}

// adminInquiryStatusExecutorStub records one status statement and returns a
// controlled sql.Result or lower-level failure without opening PostgreSQL.
type adminInquiryStatusExecutorStub struct {
	// result is returned when executionError is nil.
	result sql.Result
	// executionError simulates a driver or connection failure.
	executionError error
	// calls records how many SQL statements were attempted.
	calls int
	// context records the exact context supplied by the repository.
	context context.Context
	// query records the complete trusted SQL statement.
	query string
	// arguments records the positional inquiry identity and requested status.
	arguments []any
}

// ExecContext implements adminInquiryStatusExecutor and records the complete
// invocation before returning its configured result.
func (stub *adminInquiryStatusExecutorStub) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.result, stub.executionError
}

// adminInquiryStatusResultStub implements sql.Result while independently
// controlling RowsAffected. LastInsertId is irrelevant to UPDATE and records a
// call so a test can prove the repository never relies on it.
type adminInquiryStatusResultStub struct {
	// rowsAffected is the PostgreSQL-like command count returned on success.
	rowsAffected int64
	// rowsAffectedError simulates a result-inspection failure.
	rowsAffectedError error
	// rowsAffectedCalls proves the repository inspects the command count once.
	rowsAffectedCalls int
	// lastInsertIDCalls proves UPDATE handling never asks for an insert identity.
	lastInsertIDCalls int
}

// LastInsertId satisfies sql.Result but identifies accidental use because
// PostgreSQL UPDATE semantics do not produce an inserted identity.
func (result *adminInquiryStatusResultStub) LastInsertId() (int64, error) {
	result.lastInsertIDCalls++

	return 0, errors.New("status update has no insert identity")
}

// RowsAffected returns the configured command count or controlled inspection
// failure and records the invocation for exact behavior assertions.
func (result *adminInquiryStatusResultStub) RowsAffected() (int64, error) {
	result.rowsAffectedCalls++

	return result.rowsAffected, result.rowsAffectedError
}

// TestNewPostgresAdminInquiryStatusUpdater verifies dependency validation and
// confirms that adapting a valid pool performs no network operation.
func TestNewPostgresAdminInquiryStatusUpdater(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		updater, err := newPostgresAdminInquiryStatusUpdater(nil)
		if !errors.Is(err, errAdminInquiryStatusUpdaterDatabaseRequired) {
			t.Fatalf("error: got %v, want database-required sentinel", err)
		}
		if updater != nil {
			t.Errorf("updater: got %#v, want nil", updater)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		// A zero sql.DB must never be queried here. It is sufficient to prove that
		// construction installs the shared pool without contacting PostgreSQL.
		database := new(sql.DB)
		updater, err := newPostgresAdminInquiryStatusUpdater(database)
		if err != nil {
			t.Fatalf("create admin inquiry status updater: %v", err)
		}
		if updater == nil || updater.executor != database {
			t.Error("updater did not retain the supplied database executor")
		}
	})
}

// TestPostgresAdminInquiryStatusUpdaterSuccess verifies the complete trusted
// SQL, positional arguments, context propagation, and success semantics for
// every state allowed by the existing PostgreSQL constraint.
func TestPostgresAdminInquiryStatusUpdaterSuccess(t *testing.T) {
	const expectedSQL = `UPDATE public.inquiries
SET
    status = $2,
    updated_at = CASE
        WHEN status = $2 THEN updated_at
        ELSE CURRENT_TIMESTAMP
    END
WHERE id = $1`

	statuses := []inquiryStatus{
		inquiryStatusNew,
		inquiryStatusReviewed,
		inquiryStatusArchived,
	}
	for index, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			result := &adminInquiryStatusResultStub{rowsAffected: 1}
			executor := &adminInquiryStatusExecutorStub{result: result}
			updater := &postgresAdminInquiryStatusUpdater{executor: executor}
			ctx := context.WithValue(
				context.Background(),
				adminInquiryStatusContextKey{},
				"stage17-status-context",
			)
			inquiryID := int64(index + 41)

			err := updater.UpdateStatus(ctx, inquiryID, status)
			if err != nil {
				t.Fatalf("update inquiry status: %v", err)
			}
			if executor.calls != 1 || executor.context != ctx {
				t.Errorf(
					"executor calls/context: got %d/%v, want 1/caller context",
					executor.calls,
					executor.context,
				)
			}
			if executor.query != expectedSQL {
				t.Errorf("query:\n%s\nwant:\n%s", executor.query, expectedSQL)
			}
			expectedArguments := []any{inquiryID, string(status)}
			if !reflect.DeepEqual(executor.arguments, expectedArguments) {
				t.Errorf(
					"arguments: got %#v, want %#v",
					executor.arguments,
					expectedArguments,
				)
			}
			if result.rowsAffectedCalls != 1 {
				t.Errorf(
					"RowsAffected calls: got %d, want 1",
					result.rowsAffectedCalls,
				)
			}
			if result.lastInsertIDCalls != 0 {
				t.Errorf(
					"LastInsertId calls: got %d, want 0",
					result.lastInsertIDCalls,
				)
			}
		})
	}
}

// TestPostgresAdminInquiryStatusUpdaterRejectsInvalidInput proves every
// request-derived invalid value is rejected before the executor can be called.
func TestPostgresAdminInquiryStatusUpdaterRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		// name identifies the invalid boundary in verbose test output.
		name string
		// ctx is nil only for the explicit context validation case.
		ctx context.Context
		// inquiryID supplies the record identity under test.
		inquiryID int64
		// status supplies the closed-vocabulary value under test.
		status inquiryStatus
	}{
		{
			name:      "nil context",
			inquiryID: 1,
			status:    inquiryStatusNew,
		},
		{
			name:      "zero ID",
			ctx:       context.Background(),
			inquiryID: 0,
			status:    inquiryStatusNew,
		},
		{
			name:      "negative ID",
			ctx:       context.Background(),
			inquiryID: -1,
			status:    inquiryStatusNew,
		},
		{
			name:      "empty status",
			ctx:       context.Background(),
			inquiryID: 1,
		},
		{
			name:      "unsupported status",
			ctx:       context.Background(),
			inquiryID: 1,
			status:    "pending",
		},
		{
			name:      "wrong-case status",
			ctx:       context.Background(),
			inquiryID: 1,
			status:    "NEW",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &adminInquiryStatusExecutorStub{
				result: &adminInquiryStatusResultStub{rowsAffected: 1},
			}
			updater := &postgresAdminInquiryStatusUpdater{executor: executor}

			err := updater.UpdateStatus(test.ctx, test.inquiryID, test.status)
			if !errors.Is(err, errAdminInquiryStatusUpdateInvalid) {
				t.Fatalf("error: got %v, want invalid-update sentinel", err)
			}
			if executor.calls != 0 {
				t.Errorf("executor calls: got %d, want 0", executor.calls)
			}
		})
	}
}

// TestPostgresAdminInquiryStatusUpdaterFailures proves broken dependencies,
// driver failures, result failures, missing rows, and impossible row counts map
// to stable safe categories without exposing a lower-level sensitive message.
func TestPostgresAdminInquiryStatusUpdaterFailures(t *testing.T) {
	const sensitiveDetail = "visitor@example.test password=database-secret"

	tests := []struct {
		// name identifies the controlled repository failure.
		name string
		// updater is nil or carries one controlled executor.
		updater *postgresAdminInquiryStatusUpdater
		// expectedError distinguishes a missing record from operational failure.
		expectedError error
		// expectedCalls is the number of attempted SQL statements.
		expectedCalls int
		// expectedRowsAffectedCalls is zero when no usable result was acquired.
		expectedRowsAffectedCalls int
	}{
		{
			name:          "nil updater",
			expectedError: errAdminInquiryStatusUpdateFailed,
		},
		{
			name:          "nil executor",
			updater:       &postgresAdminInquiryStatusUpdater{},
			expectedError: errAdminInquiryStatusUpdateFailed,
		},
		{
			name: "execution failure",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{
					executionError: errors.New(sensitiveDetail),
				},
			},
			expectedError: errAdminInquiryStatusUpdateFailed,
			expectedCalls: 1,
		},
		{
			name: "nil result",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{},
			},
			expectedError: errAdminInquiryStatusUpdateFailed,
			expectedCalls: 1,
		},
		{
			name: "RowsAffected failure",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{
					result: &adminInquiryStatusResultStub{
						rowsAffectedError: errors.New(sensitiveDetail),
					},
				},
			},
			expectedError:             errAdminInquiryStatusUpdateFailed,
			expectedCalls:             1,
			expectedRowsAffectedCalls: 1,
		},
		{
			name: "missing inquiry",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{
					result: &adminInquiryStatusResultStub{rowsAffected: 0},
				},
			},
			expectedError:             errAdminInquiryNotFound,
			expectedCalls:             1,
			expectedRowsAffectedCalls: 1,
		},
		{
			name: "multiple rows",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{
					result: &adminInquiryStatusResultStub{rowsAffected: 2},
				},
			},
			expectedError:             errAdminInquiryStatusUpdateFailed,
			expectedCalls:             1,
			expectedRowsAffectedCalls: 1,
		},
		{
			name: "negative row count",
			updater: &postgresAdminInquiryStatusUpdater{
				executor: &adminInquiryStatusExecutorStub{
					result: &adminInquiryStatusResultStub{rowsAffected: -1},
				},
			},
			expectedError:             errAdminInquiryStatusUpdateFailed,
			expectedCalls:             1,
			expectedRowsAffectedCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.updater.UpdateStatus(
				context.Background(),
				17,
				inquiryStatusReviewed,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if strings.Contains(err.Error(), "visitor@example.test") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes lower-level detail: %q", err)
			}

			if test.updater == nil || test.updater.executor == nil {
				// Nil dependency cases have no recording seam to inspect.
				return
			}
			executor, ok := test.updater.executor.(*adminInquiryStatusExecutorStub)
			if !ok {
				t.Fatal("failure fixture did not use the recording executor")
			}
			if executor.calls != test.expectedCalls {
				t.Errorf(
					"executor calls: got %d, want %d",
					executor.calls,
					test.expectedCalls,
				)
			}
			result, ok := executor.result.(*adminInquiryStatusResultStub)
			if !ok {
				// Execution-error and nil-result cases cannot expose result evidence.
				return
			}
			if result.rowsAffectedCalls != test.expectedRowsAffectedCalls {
				t.Errorf(
					"RowsAffected calls: got %d, want %d",
					result.rowsAffectedCalls,
					test.expectedRowsAffectedCalls,
				)
			}
			if result.lastInsertIDCalls != 0 {
				t.Errorf(
					"LastInsertId calls: got %d, want 0",
					result.lastInsertIDCalls,
				)
			}
		})
	}
}
