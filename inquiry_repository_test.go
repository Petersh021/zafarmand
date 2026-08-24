package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// inquiryExecResponse lets one transaction double return independent outcomes
// for the shared-lock statement and the later INSERT statement.
type inquiryExecResponse struct {
	result sql.Result
	err    error
}

// inquiryExecCall records one lock or insert execution and its bound arguments.
type inquiryExecCall struct {
	context   context.Context
	query     string
	arguments []any
}

// inquiryQueryCall records one exact-replay lookup and its bound arguments.
type inquiryQueryCall struct {
	context   context.Context
	query     string
	arguments []any
}

// inquiryTransactionStub records transaction order without requiring a live
// database or weakening the production transaction interface.
type inquiryTransactionStub struct {
	execResponses []inquiryExecResponse
	execCalls     []inquiryExecCall
	queryCalls    []inquiryQueryCall
	row           inquiryRowScanner
	commitError   error
	commitCalls   int
	rollbackCalls int
	operations    []string
}

// ExecContext records ordered statements and consumes one configured result.
func (transaction *inquiryTransactionStub) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	transaction.execCalls = append(transaction.execCalls, inquiryExecCall{
		context:   ctx,
		query:     query,
		arguments: append([]any(nil), arguments...),
	})
	transaction.operations = append(transaction.operations, "exec:"+query)
	if len(transaction.execResponses) == 0 {
		return nil, errors.New("unexpected inquiry execution")
	}
	response := transaction.execResponses[0]
	transaction.execResponses = transaction.execResponses[1:]

	return response.result, response.err
}

// QueryRowContext records replay inspection and returns the configured scanner.
func (transaction *inquiryTransactionStub) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) inquiryRowScanner {
	transaction.queryCalls = append(transaction.queryCalls, inquiryQueryCall{
		context:   ctx,
		query:     query,
		arguments: append([]any(nil), arguments...),
	})
	transaction.operations = append(transaction.operations, "query:"+query)

	return transaction.row
}

// Commit records durability order and returns the injected commit outcome.
func (transaction *inquiryTransactionStub) Commit() error {
	transaction.commitCalls++
	transaction.operations = append(transaction.operations, "commit")

	return transaction.commitError
}

// Rollback records deferred cleanup after both success and failure.
func (transaction *inquiryTransactionStub) Rollback() error {
	transaction.rollbackCalls++
	transaction.operations = append(transaction.operations, "rollback")

	return nil
}

// inquiryBeginStub captures transaction options and returns one configured seam.
type inquiryBeginStub struct {
	transaction inquiryTransaction
	err         error
	calls       int
	context     context.Context
	options     *sql.TxOptions
}

// Begin implements the repository's transaction factory for unit tests.
func (begin *inquiryBeginStub) Begin(
	ctx context.Context,
	options *sql.TxOptions,
) (inquiryTransaction, error) {
	begin.calls++
	begin.context = ctx
	begin.options = options

	return begin.transaction, begin.err
}

// inquiryRowScannerStub supplies one replay comparison result or a controlled
// private driver-like failure.
type inquiryRowScannerStub struct {
	exactReplay bool
	scanError   error
	calls       int
}

// Scan maps one exact-replay Boolean or returns an injected private failure.
func (row *inquiryRowScannerStub) Scan(destinations ...any) error {
	row.calls++
	if row.scanError != nil {
		return row.scanError
	}
	if len(destinations) != 1 {
		return errors.New("replay scanner expected one destination")
	}
	exactReplay, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("replay scanner expected a boolean destination")
	}
	*exactReplay = row.exactReplay

	return nil
}

// inquirySQLResultStub controls RowsAffected without relying on the generated-
// identifier API that PostgreSQL does not support through database/sql.
type inquirySQLResultStub struct {
	rowsAffected      int64
	rowsAffectedError error
	rowsAffectedCalls int
	lastInsertIDCalls int
}

// LastInsertId records accidental use of an API unsupported by PostgreSQL.
func (result *inquirySQLResultStub) LastInsertId() (int64, error) {
	result.lastInsertIDCalls++

	return 0, errors.New("LastInsertId is unsupported")
}

// RowsAffected supplies the configured INSERT outcome.
func (result *inquirySQLResultStub) RowsAffected() (int64, error) {
	result.rowsAffectedCalls++

	return result.rowsAffected, result.rowsAffectedError
}

// inquiryRepositoryContextKey proves request context identity survives the seam.
type inquiryRepositoryContextKey struct{}

// TestInquiryRetentionAdvisoryLockNamespace protects stable, distinct shared and
// exclusive lock definitions from colliding with the migration runner.
func TestInquiryRetentionAdvisoryLockNamespace(t *testing.T) {
	if inquiryRetentionAdvisoryLockID <= 0 {
		t.Fatal("inquiry retention advisory lock must use a stable positive ID")
	}
	if inquiryRetentionAdvisoryLockID == migrationAdvisoryLockID {
		t.Fatal("inquiry retention and migration operations share one lock ID")
	}
	if acquireInquiryRetentionSharedLockSQL == acquireInquiryRetentionExclusiveLockSQL {
		t.Fatal("Contact and maintenance unexpectedly use the same lock mode")
	}
}

// TestInquiryRepositorySQLContract freezes parameterized lock, insert, tombstone,
// and exact-replay SQL at the repository boundary.
func TestInquiryRepositorySQLContract(t *testing.T) {
	const expectedSharedLockSQL = `SELECT pg_catalog.pg_advisory_xact_lock_shared($1::bigint)`
	if acquireInquiryRetentionSharedLockSQL != expectedSharedLockSQL {
		t.Errorf(
			"shared lock SQL:\n%s\nwant:\n%s",
			acquireInquiryRetentionSharedLockSQL,
			expectedSharedLockSQL,
		)
	}
	const expectedExclusiveLockSQL = `SELECT pg_catalog.pg_advisory_xact_lock($1::bigint)`
	if acquireInquiryRetentionExclusiveLockSQL != expectedExclusiveLockSQL {
		t.Errorf(
			"exclusive lock SQL:\n%s\nwant:\n%s",
			acquireInquiryRetentionExclusiveLockSQL,
			expectedExclusiveLockSQL,
		)
	}

	const expectedCreateSQL = `INSERT INTO public.inquiries (
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
	if createInquirySQL != expectedCreateSQL {
		t.Errorf("create SQL:\n%s\nwant:\n%s", createInquirySQL, expectedCreateSQL)
	}

	const expectedReplaySQL = `SELECT exact_replay
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
	if exactInquiryReplaySQL != expectedReplaySQL {
		t.Errorf(
			"replay SQL:\n%s\nwant:\n%s",
			exactInquiryReplaySQL,
			expectedReplaySQL,
		)
	}
}

// TestNewPostgresInquiryRepository covers nil rejection and valid pool wiring.
func TestNewPostgresInquiryRepository(t *testing.T) {
	repository, err := newPostgresInquiryRepository(nil)
	if !errors.Is(err, errInquiryRepositoryDatabaseRequired) || repository != nil {
		t.Fatalf("nil database: repository=%#v error=%v", repository, err)
	}

	repository, err = newPostgresInquiryRepository(new(sql.DB))
	if err != nil || repository == nil || repository.begin == nil {
		t.Fatalf("valid database: repository=%#v error=%v", repository, err)
	}
}

// TestPostgresInquiryRepositoryCreateTransaction verifies explicit isolation,
// shared-lock ordering, digest arguments, replay behavior, and commit cleanup.
func TestPostgresInquiryRepositoryCreateTransaction(t *testing.T) {
	for _, test := range []struct {
		name           string
		rowsAffected   int64
		expectedResult inquiryCreateResult
		verifyReplay   bool
	}{
		{"created", 1, inquiryCreateResultCreated, false},
		{"exact replay", 0, inquiryCreateResultReplay, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &inquirySQLResultStub{rowsAffected: test.rowsAffected}
			replayRow := &inquiryRowScannerStub{exactReplay: true}
			transaction := &inquiryTransactionStub{
				execResponses: []inquiryExecResponse{
					{},
					{result: result},
				},
				row: replayRow,
			}
			begin := &inquiryBeginStub{transaction: transaction}
			repository := &postgresInquiryRepository{begin: begin.Begin}
			submission := inquirySubmission{
				SubmissionKey: []byte("0123456789abcdef0123456789abcdef"),
				Name:          "Test Visitor",
				Email:         "visitor@example.com",
				Discipline:    "architecture-design",
				Message:       "A carefully normalized project inquiry.",
			}
			ctx := context.WithValue(
				context.Background(),
				inquiryRepositoryContextKey{},
				"request-context-marker",
			)

			created, err := repository.Create(ctx, submission)
			if err != nil || created != test.expectedResult {
				t.Fatalf("create inquiry: result=%d error=%v", created, err)
			}
			if begin.calls != 1 || begin.context != ctx || begin.options == nil ||
				begin.options.Isolation != sql.LevelReadCommitted || begin.options.ReadOnly {
				t.Errorf("begin call: %#v", begin)
			}
			if len(transaction.execCalls) != 2 {
				t.Fatalf("execution count: got %d, want 2", len(transaction.execCalls))
			}
			lockCall := transaction.execCalls[0]
			if lockCall.context != ctx ||
				lockCall.query != acquireInquiryRetentionSharedLockSQL ||
				!reflect.DeepEqual(
					lockCall.arguments,
					[]any{inquiryRetentionAdvisoryLockID},
				) {
				t.Errorf("shared-lock call: %#v", lockCall)
			}
			insertCall := transaction.execCalls[1]
			if insertCall.context != ctx || insertCall.query != createInquirySQL {
				t.Errorf("insert call: %#v", insertCall)
			}
			submissionKeyHash := sha256.Sum256(submission.SubmissionKey)
			expectedArguments := []any{
				submission.SubmissionKey,
				submission.Name,
				submission.Email,
				submission.Discipline,
				submission.Message,
				submissionKeyHash[:],
			}
			if !reflect.DeepEqual(insertCall.arguments, expectedArguments) {
				t.Errorf("insert arguments: got %#v", insertCall.arguments)
			}
			if result.rowsAffectedCalls != 1 || result.lastInsertIDCalls != 0 {
				t.Errorf(
					"result inspection calls: rows=%d ID=%d",
					result.rowsAffectedCalls,
					result.lastInsertIDCalls,
				)
			}
			if transaction.commitCalls != 1 || transaction.rollbackCalls != 1 {
				t.Errorf(
					"commit/rollback calls: %d/%d",
					transaction.commitCalls,
					transaction.rollbackCalls,
				)
			}

			expectedOperations := []string{
				"exec:" + acquireInquiryRetentionSharedLockSQL,
				"exec:" + createInquirySQL,
			}
			if test.verifyReplay {
				expectedOperations = append(
					expectedOperations,
					"query:"+exactInquiryReplaySQL,
				)
				if len(transaction.queryCalls) != 1 || replayRow.calls != 1 {
					t.Fatalf(
						"replay query/scan calls: %d/%d",
						len(transaction.queryCalls),
						replayRow.calls,
					)
				}
				queryCall := transaction.queryCalls[0]
				if queryCall.context != ctx ||
					queryCall.query != exactInquiryReplaySQL ||
					!reflect.DeepEqual(queryCall.arguments, expectedArguments) {
					t.Errorf("replay comparison call: %#v", queryCall)
				}
			} else if len(transaction.queryCalls) != 0 || replayRow.calls != 0 {
				t.Error("new insert unnecessarily queried for a replay")
			}
			expectedOperations = append(expectedOperations, "commit", "rollback")
			if !reflect.DeepEqual(transaction.operations, expectedOperations) {
				t.Errorf(
					"transaction order:\n got %#v\nwant %#v",
					transaction.operations,
					expectedOperations,
				)
			}
		})
	}
}

// TestPostgresInquiryRepositoryCreateFailures maps every transaction failure to
// one redacted sentinel and rolls back incomplete work.
func TestPostgresInquiryRepositoryCreateFailures(t *testing.T) {
	const sensitiveDetail = "password=database-secret visitor@example.com"
	assertSafeFailure := func(
		t *testing.T,
		repository *postgresInquiryRepository,
		ctx context.Context,
	) {
		t.Helper()
		result, err := repository.Create(ctx, inquirySubmission{})
		if !errors.Is(err, errInquiryCreateFailed) || result != 0 {
			t.Fatalf("failure: result=%d error=%v", result, err)
		}
		if strings.Contains(err.Error(), "database-secret") ||
			strings.Contains(err.Error(), "visitor@example.com") {
			t.Errorf("error exposes sensitive detail: %q", err)
		}
	}

	t.Run("invalid repository or context", func(t *testing.T) {
		assertSafeFailure(t, nil, context.Background())
		assertSafeFailure(t, &postgresInquiryRepository{}, context.Background())
		assertSafeFailure(
			t,
			&postgresInquiryRepository{begin: (&inquiryBeginStub{}).Begin},
			nil,
		)
	})

	for _, test := range []struct {
		name        string
		transaction inquiryTransaction
		beginError  error
	}{
		{"begin failure", nil, errors.New(sensitiveDetail)},
		{"nil transaction", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			begin := &inquiryBeginStub{
				transaction: test.transaction,
				err:         test.beginError,
			}
			assertSafeFailure(
				t,
				&postgresInquiryRepository{begin: begin.Begin},
				context.Background(),
			)
		})
	}

	tests := []struct {
		name            string
		execResponses   []inquiryExecResponse
		row             inquiryRowScanner
		commitError     error
		expectedExecs   int
		expectedQueries int
	}{
		{
			name:          "shared lock failure",
			execResponses: []inquiryExecResponse{{err: errors.New(sensitiveDetail)}},
			expectedExecs: 1,
		},
		{
			name: "insert failure",
			execResponses: []inquiryExecResponse{
				{}, {err: errors.New(sensitiveDetail)},
			},
			expectedExecs: 2,
		},
		{
			name:          "nil insert result",
			execResponses: []inquiryExecResponse{{}, {}},
			expectedExecs: 2,
		},
		{
			name: "rows affected failure",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffectedError: errors.New(sensitiveDetail)}},
			},
			expectedExecs: 2,
		},
		{
			name: "impossible affected count",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffected: 2}},
			},
			expectedExecs: 2,
		},
		{
			name: "missing replay row",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffected: 0}},
			},
			expectedExecs:   2,
			expectedQueries: 1,
		},
		{
			name: "replay scan failure",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffected: 0}},
			},
			row:             &inquiryRowScannerStub{scanError: errors.New(sensitiveDetail)},
			expectedExecs:   2,
			expectedQueries: 1,
		},
		{
			name: "created commit failure",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffected: 1}},
			},
			commitError:   errors.New(sensitiveDetail),
			expectedExecs: 2,
		},
		{
			name: "replay commit failure",
			execResponses: []inquiryExecResponse{
				{}, {result: &inquirySQLResultStub{rowsAffected: 0}},
			},
			row:             &inquiryRowScannerStub{exactReplay: true},
			commitError:     errors.New(sensitiveDetail),
			expectedExecs:   2,
			expectedQueries: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &inquiryTransactionStub{
				execResponses: test.execResponses,
				row:           test.row,
				commitError:   test.commitError,
			}
			begin := &inquiryBeginStub{transaction: transaction}
			assertSafeFailure(
				t,
				&postgresInquiryRepository{begin: begin.Begin},
				context.Background(),
			)
			if len(transaction.execCalls) != test.expectedExecs ||
				len(transaction.queryCalls) != test.expectedQueries ||
				transaction.rollbackCalls != 1 {
				t.Errorf(
					"exec/query/rollback calls: %d/%d/%d",
					len(transaction.execCalls),
					len(transaction.queryCalls),
					transaction.rollbackCalls,
				)
			}
		})
	}
}

// TestPostgresInquiryRepositoryRejectsKeyCollisions distinguishes a changed
// payload or tombstoned key from a successful exact replay.
func TestPostgresInquiryRepositoryRejectsKeyCollisions(t *testing.T) {
	transaction := &inquiryTransactionStub{
		execResponses: []inquiryExecResponse{
			{}, {result: &inquirySQLResultStub{rowsAffected: 0}},
		},
		row: &inquiryRowScannerStub{exactReplay: false},
	}
	begin := &inquiryBeginStub{transaction: transaction}
	repository := &postgresInquiryRepository{begin: begin.Begin}
	result, err := repository.Create(
		context.Background(),
		inquirySubmission{
			SubmissionKey: []byte("0123456789abcdef0123456789abcdef"),
			Name:          "Expected Visitor",
			Email:         "expected@example.com",
			Discipline:    "products",
			Message:       "The complete expected payload.",
		},
	)
	if !errors.Is(err, errInquirySubmissionConflict) || result != 0 {
		t.Fatalf("collision: result=%d error=%v", result, err)
	}
	if transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
		t.Errorf(
			"collision commit/rollback calls: %d/%d",
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}
