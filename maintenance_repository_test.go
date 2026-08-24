package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// maintenanceRowStub copies fixed scalar values into Scan destinations or
// returns one injected safe/unsafe database error.
type maintenanceRowStub struct {
	values []any
	err    error
	calls  int
}

// Scan implements maintenanceRowScanner for deterministic repository tests.
func (row *maintenanceRowStub) Scan(destinations ...any) error {
	row.calls++
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("maintenance row destination count mismatch")
	}
	for index, value := range row.values {
		switch destination := destinations[index].(type) {
		case *string:
			stored, ok := value.(string)
			if !ok {
				return errors.New("maintenance row expected string")
			}
			*destination = stored
		case *int64:
			stored, ok := value.(int64)
			if !ok {
				return errors.New("maintenance row expected int64")
			}
			*destination = stored
		default:
			return errors.New("maintenance row destination type mismatch")
		}
	}

	return nil
}

// maintenanceQueryCall records one parameterized query invocation.
type maintenanceQueryCall struct {
	context   context.Context
	query     string
	arguments []any
}

// maintenanceExecCall records one advisory-lock execution.
type maintenanceExecCall struct {
	context   context.Context
	query     string
	arguments []any
}

// maintenanceTransactionStub records exact operation order and injects
// transaction, row, execution, commit, or rollback outcomes.
type maintenanceTransactionStub struct {
	rows          []*maintenanceRowStub
	queries       []maintenanceQueryCall
	executions    []maintenanceExecCall
	execError     error
	commitError   error
	rollbackError error
	commitCalls   int
	rollbackCalls int
	operations    []string
}

// ExecContext records the advisory-lock statement and optional failure.
func (transaction *maintenanceTransactionStub) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	transaction.executions = append(
		transaction.executions,
		maintenanceExecCall{
			context:   ctx,
			query:     query,
			arguments: append([]any(nil), arguments...),
		},
	)
	transaction.operations = append(transaction.operations, "exec:"+query)

	return nil, transaction.execError
}

// QueryRowContext records SQL/arguments and returns rows in fixture order.
func (transaction *maintenanceTransactionStub) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) maintenanceRowScanner {
	transaction.queries = append(transaction.queries, maintenanceQueryCall{
		context:   ctx,
		query:     query,
		arguments: append([]any(nil), arguments...),
	})
	transaction.operations = append(transaction.operations, "query:"+query)
	if len(transaction.rows) == 0 {
		return &maintenanceRowStub{err: errors.New("unexpected maintenance query")}
	}
	row := transaction.rows[0]
	transaction.rows = transaction.rows[1:]

	return row
}

// Commit records durability order and returns the injected outcome.
func (transaction *maintenanceTransactionStub) Commit() error {
	transaction.commitCalls++
	transaction.operations = append(transaction.operations, "commit")

	return transaction.commitError
}

// Rollback records cleanup on both success and failure paths.
func (transaction *maintenanceTransactionStub) Rollback() error {
	transaction.rollbackCalls++
	transaction.operations = append(transaction.operations, "rollback")

	return transaction.rollbackError
}

// maintenanceBeginStub captures context/options and returns one transaction.
type maintenanceBeginStub struct {
	transaction maintenanceTransaction
	err         error
	calls       int
	context     context.Context
	options     *sql.TxOptions
}

// Begin implements the repository's transaction-factory seam.
func (begin *maintenanceBeginStub) Begin(
	ctx context.Context,
	options *sql.TxOptions,
) (maintenanceTransaction, error) {
	begin.calls++
	begin.context = ctx
	begin.options = options

	return begin.transaction, begin.err
}

// TestNewPostgresMaintenanceRepository protects nil rejection and valid wiring.
func TestNewPostgresMaintenanceRepository(t *testing.T) {
	if repository, err := newPostgresMaintenanceRepository(nil); !errors.Is(err, errMaintenanceRepositoryDatabaseRequired) ||
		repository != nil {
		t.Fatalf("nil database: repository=%#v error=%v", repository, err)
	}
	repository, err := newPostgresMaintenanceRepository(new(sql.DB))
	if err != nil || repository == nil || repository.begin == nil {
		t.Fatalf("valid database: repository=%#v error=%v", repository, err)
	}
}

// TestMaintenanceRepositoryInspectRetention verifies read-only preview SQL,
// cutoff binding, aggregate mapping, and commit/cleanup behavior.
func TestMaintenanceRepositoryInspectRetention(t *testing.T) {
	cutoff := time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC)
	transaction := &maintenanceTransactionStub{
		rows: []*maintenanceRowStub{{values: []any{int64(2), int64(3)}}},
	}
	begin := &maintenanceBeginStub{transaction: transaction}
	repository := &postgresMaintenanceRepository{begin: begin.Begin}
	result, err := repository.InspectRetention(t.Context(), cutoff)
	if err != nil {
		t.Fatalf("inspect retention: %v", err)
	}
	if result != (maintenanceRetentionResult{2, 3}) {
		t.Errorf("result: got %#v", result)
	}
	if begin.options == nil || !begin.options.ReadOnly {
		t.Error("retention preview did not open a read-only transaction")
	}
	if len(transaction.queries) != 1 ||
		transaction.queries[0].query != inspectMaintenanceRetentionSQL ||
		!reflect.DeepEqual(transaction.queries[0].arguments, []any{cutoff}) {
		t.Errorf("preview queries: %#v", transaction.queries)
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 1 {
		t.Errorf(
			"preview commit/rollback calls: %d/%d",
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

// TestMaintenanceRepositoryApplyRetention verifies confirmation, exclusive lock,
// deletion order, cutoff binding, aggregate results, and atomic commit.
func TestMaintenanceRepositoryApplyRetention(t *testing.T) {
	cutoff := time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC)
	transaction := &maintenanceTransactionStub{
		rows: []*maintenanceRowStub{
			{values: []any{"zafarmand_test"}},
			{values: []any{int64(4)}},
			{values: []any{int64(5)}},
		},
	}
	begin := &maintenanceBeginStub{transaction: transaction}
	repository := &postgresMaintenanceRepository{begin: begin.Begin}
	result, err := repository.ApplyRetention(
		t.Context(), cutoff, "zafarmand_test",
	)
	if err != nil {
		t.Fatalf("apply retention: %v", err)
	}
	if result != (maintenanceRetentionResult{4, 5}) {
		t.Errorf("result: got %#v", result)
	}
	if begin.options == nil ||
		begin.options.Isolation != sql.LevelReadCommitted ||
		begin.options.ReadOnly {
		t.Errorf("apply transaction options: %#v", begin.options)
	}
	if len(transaction.queries) != 3 ||
		transaction.queries[0].query != maintenanceCurrentDatabaseSQL ||
		transaction.queries[1].query != deleteExpiredAdminSessionsSQL ||
		transaction.queries[2].query != purgeArchivedInquiriesBeforeSQL ||
		!reflect.DeepEqual(transaction.queries[2].arguments, []any{cutoff}) {
		t.Errorf("apply queries: %#v", transaction.queries)
	}
	if len(transaction.executions) != 1 ||
		transaction.executions[0].context != begin.context ||
		transaction.executions[0].query != acquireInquiryRetentionExclusiveLockSQL ||
		!reflect.DeepEqual(
			transaction.executions[0].arguments,
			[]any{inquiryRetentionAdvisoryLockID},
		) {
		t.Errorf("exclusive-lock executions: %#v", transaction.executions)
	}
	expectedOperationPrefix := []string{
		"query:" + maintenanceCurrentDatabaseSQL,
		"exec:" + acquireInquiryRetentionExclusiveLockSQL,
		"query:" + deleteExpiredAdminSessionsSQL,
		"query:" + purgeArchivedInquiriesBeforeSQL,
		"commit",
	}
	if len(transaction.operations) < len(expectedOperationPrefix) ||
		!reflect.DeepEqual(
			transaction.operations[:len(expectedOperationPrefix)],
			expectedOperationPrefix,
		) {
		t.Errorf("apply transaction order: %#v", transaction.operations)
	}
	for _, fragment := range []string{
		"sha256(submission_key)",
		"INSERT INTO public.inquiry_submission_tombstones",
		"ON CONFLICT (submission_key_hash) DO NOTHING",
		"FOR UPDATE",
		"DELETE FROM public.inquiries",
	} {
		if !strings.Contains(purgeArchivedInquiriesBeforeSQL, fragment) {
			t.Errorf("retention SQL does not contain %q", fragment)
		}
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 1 {
		t.Errorf(
			"apply commit/rollback calls: %d/%d",
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

// TestMaintenanceRepositoryRefusesWrongDatabase proves a mismatched identity
// rolls back before either lock acquisition or deletion.
func TestMaintenanceRepositoryRefusesWrongDatabase(t *testing.T) {
	transaction := &maintenanceTransactionStub{
		rows: []*maintenanceRowStub{{values: []any{"actual_database"}}},
	}
	begin := &maintenanceBeginStub{transaction: transaction}
	repository := &postgresMaintenanceRepository{begin: begin.Begin}
	_, err := repository.ApplyRetention(
		t.Context(),
		time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC),
		"confirmed_database",
	)
	if !errors.Is(err, errMaintenanceDatabaseConfirmationMismatch) {
		t.Fatalf("database mismatch: got %v", err)
	}
	if len(transaction.queries) != 1 || transaction.commitCalls != 0 ||
		transaction.rollbackCalls != 1 || len(transaction.executions) != 0 {
		t.Errorf(
			"mismatch queries/executions/commit/rollback: %d/%d/%d/%d",
			len(transaction.queries),
			len(transaction.executions),
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

// TestMaintenanceRepositoryApplyRollsBackFailures verifies atomic rollback and
// redaction when a later retention statement fails.
func TestMaintenanceRepositoryApplyRollsBackFailures(t *testing.T) {
	privateDetail := "visitor@example.com password=private"
	transaction := &maintenanceTransactionStub{
		rows: []*maintenanceRowStub{
			{values: []any{"zafarmand_test"}},
			{values: []any{int64(1)}},
			{err: errors.New(privateDetail)},
		},
	}
	begin := &maintenanceBeginStub{transaction: transaction}
	repository := &postgresMaintenanceRepository{begin: begin.Begin}
	_, err := repository.ApplyRetention(
		t.Context(),
		time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC),
		"zafarmand_test",
	)
	if !errors.Is(err, errMaintenanceRepositoryFailed) {
		t.Fatalf("apply failure: got %v", err)
	}
	if strings.Contains(err.Error(), "visitor") || strings.Contains(err.Error(), "private") {
		t.Error("maintenance error exposes driver or visitor detail")
	}
	if transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
		t.Errorf(
			"failed apply commit/rollback calls: %d/%d",
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

// TestMaintenanceRepositoryRedactsExclusiveLockFailure keeps driver and visitor
// details out of the command boundary while rolling back.
func TestMaintenanceRepositoryRedactsExclusiveLockFailure(t *testing.T) {
	transaction := &maintenanceTransactionStub{
		rows:      []*maintenanceRowStub{{values: []any{"zafarmand_test"}}},
		execError: errors.New("password=private visitor@example.com"),
	}
	begin := &maintenanceBeginStub{transaction: transaction}
	repository := &postgresMaintenanceRepository{begin: begin.Begin}
	_, err := repository.ApplyRetention(
		t.Context(),
		time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC),
		"zafarmand_test",
	)
	if !errors.Is(err, errMaintenanceRepositoryFailed) {
		t.Fatalf("exclusive lock failure: got %v", err)
	}
	if strings.Contains(err.Error(), "password") ||
		strings.Contains(err.Error(), "visitor") {
		t.Error("exclusive lock error exposes driver detail")
	}
	if len(transaction.executions) != 1 || len(transaction.queries) != 1 ||
		transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
		t.Errorf(
			"lock failure exec/query/commit/rollback: %d/%d/%d/%d",
			len(transaction.executions),
			len(transaction.queries),
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

// TestMaintenanceRepositoryPurgeInquiry covers neutral no-op and exact one-row
// targeted purge outcomes under the exclusive lock.
func TestMaintenanceRepositoryPurgeInquiry(t *testing.T) {
	for _, deleted := range []int64{0, 1} {
		t.Run(string(rune('0'+deleted))+" deleted", func(t *testing.T) {
			transaction := &maintenanceTransactionStub{
				rows: []*maintenanceRowStub{
					{values: []any{"zafarmand_test"}},
					{values: []any{deleted}},
				},
			}
			begin := &maintenanceBeginStub{transaction: transaction}
			repository := &postgresMaintenanceRepository{begin: begin.Begin}
			result, err := repository.PurgeInquiry(
				t.Context(), 42, "zafarmand_test",
			)
			if err != nil {
				t.Fatalf("purge inquiry: %v", err)
			}
			if result.Purged != (deleted == 1) {
				t.Errorf("purged: got %t", result.Purged)
			}
			if len(transaction.queries) != 2 ||
				transaction.queries[1].query != purgeArchivedInquiryByIDSQL ||
				!reflect.DeepEqual(transaction.queries[1].arguments, []any{int64(42)}) {
				t.Errorf("purge queries: %#v", transaction.queries)
			}
			if begin.options == nil ||
				begin.options.Isolation != sql.LevelReadCommitted ||
				len(transaction.executions) != 1 ||
				transaction.executions[0].query != acquireInquiryRetentionExclusiveLockSQL ||
				!reflect.DeepEqual(
					transaction.executions[0].arguments,
					[]any{inquiryRetentionAdvisoryLockID},
				) {
				t.Errorf(
					"purge transaction options/executions: %#v/%#v",
					begin.options,
					transaction.executions,
				)
			}
		})
	}
}

// TestMaintenanceRepositoryRejectsInvalidInputs prevents incomplete commands
// from reaching a transaction factory.
func TestMaintenanceRepositoryRejectsInvalidInputs(t *testing.T) {
	repository := &postgresMaintenanceRepository{}
	if _, err := repository.InspectRetention(nil, time.Now()); !errors.Is(err, errMaintenanceRepositoryInvalid) {
		t.Errorf("nil preview context: got %v", err)
	}
	if _, err := repository.ApplyRetention(
		t.Context(), time.Time{}, "database",
	); !errors.Is(err, errMaintenanceRepositoryInvalid) {
		t.Errorf("zero apply cutoff: got %v", err)
	}
	if _, err := repository.PurgeInquiry(
		t.Context(), 0, "database",
	); !errors.Is(err, errMaintenanceRepositoryInvalid) {
		t.Errorf("zero purge ID: got %v", err)
	}
}
