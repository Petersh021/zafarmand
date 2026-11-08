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

type adminEmailOTPExecCall struct {
	context   context.Context
	query     string
	arguments []any
}

// adminEmailOTPTransactionStub records ordered transactional reads and writes.
type adminEmailOTPTransactionStub struct {
	execResults []sql.Result
	execErrors  []error
	rows        []adminRowScanner
	execCalls   []adminEmailOTPExecCall
	queryCalls  []adminEmailOTPExecCall
	commits     int
	rollbacks   int
	commitError error
}

func (stub *adminEmailOTPTransactionStub) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	index := len(stub.execCalls)
	stub.execCalls = append(stub.execCalls, adminEmailOTPExecCall{
		context:   ctx,
		query:     query,
		arguments: append([]any(nil), arguments...),
	})
	if index >= len(stub.execResults) {
		return nil, errors.New("unexpected OTP transaction write")
	}
	var err error
	if index < len(stub.execErrors) {
		err = stub.execErrors[index]
	}

	return stub.execResults[index], err
}

func (stub *adminEmailOTPTransactionStub) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) adminRowScanner {
	index := len(stub.queryCalls)
	stub.queryCalls = append(stub.queryCalls, adminEmailOTPExecCall{
		context:   ctx,
		query:     query,
		arguments: append([]any(nil), arguments...),
	})
	if index >= len(stub.rows) {
		return nil
	}

	return stub.rows[index]
}

func (stub *adminEmailOTPTransactionStub) Commit() error {
	stub.commits++

	return stub.commitError
}

func (stub *adminEmailOTPTransactionStub) Rollback() error {
	stub.rollbacks++

	return nil
}

type adminEmailOTPBeginStub struct {
	transaction adminEmailOTPTransaction
	err         error
	calls       int
	context     context.Context
	options     *sql.TxOptions
}

func (stub *adminEmailOTPBeginStub) Begin(
	ctx context.Context,
	options *sql.TxOptions,
) (adminEmailOTPTransaction, error) {
	stub.calls++
	stub.context = ctx
	stub.options = options

	return stub.transaction, stub.err
}

func validAdminEmailOTPChallengeFixture() adminEmailOTPChallenge {
	return adminEmailOTPChallenge{
		ChallengeHash: bytesOfLength(0x31, adminEmailOTPChallengeHashBytes),
		UserID:        42,
		CodeMAC:       bytesOfLength(0x41, adminEmailOTPCodeMACBytes),
		ExpiresAt: time.Date(
			2035,
			time.January,
			2,
			3,
			4,
			5,
			0,
			time.UTC,
		),
	}
}

// TestAdminEmailOTPRepositoryCancelsUndeliveredChallenge proves delivery
// failure consumes the unusable code and releases its latest request slot in
// the same transaction, permitting an immediate honest retry.
func TestAdminEmailOTPRepositoryCancelsUndeliveredChallenge(t *testing.T) {
	transaction := &adminEmailOTPTransactionStub{
		execResults: []sql.Result{
			&adminSQLResultRecordingStub{rowsAffected: 1},
		},
	}
	begin := &adminEmailOTPBeginStub{transaction: transaction}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}
	challengeHash := bytesOfLength(0x30, adminEmailOTPChallengeHashBytes)

	if err := repository.CancelChallenge(t.Context(), challengeHash); err != nil {
		t.Fatalf("cancel undelivered challenge: %v", err)
	}
	if len(transaction.execCalls) != 1 ||
		transaction.execCalls[0].query != cancelAdminEmailOTPChallengeSQL ||
		!reflect.DeepEqual(transaction.execCalls[0].arguments, []any{challengeHash}) {
		t.Errorf("cancel call: %#v", transaction.execCalls)
	}
	if transaction.commits != 1 || transaction.rollbacks != 1 {
		t.Errorf(
			"cancel completion: commits=%d rollbacks=%d",
			transaction.commits,
			transaction.rollbacks,
		)
	}
}

// TestAdminEmailOTPRepositoryCreatesReplacementAtomically verifies that the
// durable request budget, replacement, and insert share one transaction.
func TestAdminEmailOTPRepositoryCreatesReplacementAtomically(t *testing.T) {
	transaction := &adminEmailOTPTransactionStub{
		execResults: []sql.Result{
			&adminSQLResultRecordingStub{rowsAffected: 1},
			&adminSQLResultRecordingStub{rowsAffected: 0},
			&adminSQLResultRecordingStub{rowsAffected: 1},
			&adminSQLResultRecordingStub{rowsAffected: 1},
			&adminSQLResultRecordingStub{rowsAffected: 1},
		},
		rows: []adminRowScanner{
			&adminRowScannerRecordingStub{scan: func(destinations ...any) error {
				*destinations[0].(*int64) = 42
				return nil
			}},
		},
	}
	begin := &adminEmailOTPBeginStub{transaction: transaction}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}
	challenge := validAdminEmailOTPChallengeFixture()

	if err := repository.CreateChallenge(t.Context(), challenge); err != nil {
		t.Fatalf("create OTP challenge: %v", err)
	}
	if begin.calls != 1 || begin.context != t.Context() || begin.options != nil {
		t.Errorf("begin transaction: %#v", begin)
	}
	if len(transaction.execCalls) != 5 {
		t.Fatalf("write count: got %d, want 5", len(transaction.execCalls))
	}
	if transaction.execCalls[0].query != ensureAdminEmailOTPRateLimitSQL ||
		transaction.execCalls[1].query != resetExpiredAdminEmailOTPRateLimitSQL ||
		transaction.execCalls[2].query != reserveAdminEmailOTPRequestSQL ||
		transaction.execCalls[3].query != invalidateOutstandingAdminEmailOTPChallengesSQL {
		t.Errorf("rate/replacement calls: %#v", transaction.execCalls[:4])
	}
	for _, call := range transaction.execCalls[:4] {
		if !reflect.DeepEqual(call.arguments, []any{int64(42)}) {
			t.Errorf("user-scoped write arguments: %#v", call)
		}
	}
	if transaction.execCalls[4].query != createAdminEmailOTPChallengeSQL ||
		!reflect.DeepEqual(
			transaction.execCalls[4].arguments,
			[]any{
				challenge.ChallengeHash,
				challenge.UserID,
				challenge.CodeMAC,
				challenge.ExpiresAt,
			},
		) {
		t.Errorf("insert call: %#v", transaction.execCalls[4])
	}
	if len(transaction.queryCalls) != 1 ||
		transaction.queryCalls[0].query != lockAdminEmailOTPRateLimitSQL {
		t.Errorf("rate lock call: %#v", transaction.queryCalls)
	}
	if transaction.commits != 1 || transaction.rollbacks != 1 {
		t.Errorf(
			"transaction completion: commits=%d rollbacks=%d",
			transaction.commits,
			transaction.rollbacks,
		)
	}
}

// TestAdminEmailOTPRepositoryRollsBackReplacementFailure proves that consuming
// a previous code is not committed when the replacement cannot be inserted.
func TestAdminEmailOTPRepositoryRollsBackReplacementFailure(t *testing.T) {
	transaction := &adminEmailOTPTransactionStub{
		execResults: []sql.Result{
			&adminSQLResultRecordingStub{rowsAffected: 1},
			&adminSQLResultRecordingStub{rowsAffected: 0},
			&adminSQLResultRecordingStub{rowsAffected: 1},
			&adminSQLResultRecordingStub{rowsAffected: 1},
			nil,
		},
		execErrors: []error{
			nil,
			nil,
			nil,
			nil,
			errors.New("private insert failure"),
		},
		rows: []adminRowScanner{
			&adminRowScannerRecordingStub{scan: func(destinations ...any) error {
				*destinations[0].(*int64) = 42
				return nil
			}},
		},
	}
	begin := &adminEmailOTPBeginStub{transaction: transaction}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}

	err := repository.CreateChallenge(
		t.Context(),
		validAdminEmailOTPChallengeFixture(),
	)
	if !errors.Is(err, errAdminEmailOTPRepositoryDatabaseFailed) {
		t.Fatalf("replacement failure: got %v", err)
	}
	if transaction.commits != 0 || transaction.rollbacks != 1 {
		t.Errorf(
			"transaction completion: commits=%d rollbacks=%d",
			transaction.commits,
			transaction.rollbacks,
		)
	}
	if err != nil && stringsContainAny(
		err.Error(),
		[]string{"private insert failure", "owner@example.com"},
	) {
		t.Error("OTP repository error exposes private detail")
	}
}

// TestAdminEmailOTPRepositoryRequiresActiveUser ensures a deactivation race
// cannot leave a fresh challenge for an account that is no longer authorized.
func TestAdminEmailOTPRepositoryRequiresActiveUser(t *testing.T) {
	transaction := &adminEmailOTPTransactionStub{
		execResults: []sql.Result{
			&adminSQLResultRecordingStub{rowsAffected: 1},
		},
		rows: []adminRowScanner{
			&adminRowScannerRecordingStub{scan: func(...any) error {
				return sql.ErrNoRows
			}},
		},
	}
	begin := &adminEmailOTPBeginStub{transaction: transaction}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}

	err := repository.CreateChallenge(
		t.Context(),
		validAdminEmailOTPChallengeFixture(),
	)
	if !errors.Is(err, errAdminUserNotFound) {
		t.Errorf("inactive user: got %v", err)
	}
	if transaction.commits != 0 || transaction.rollbacks != 1 {
		t.Errorf(
			"inactive-user transaction: commits=%d rollbacks=%d",
			transaction.commits,
			transaction.rollbacks,
		)
	}
}

// TestAdminEmailOTPRepositoryVerifyAndConsume covers successful one-time use
// and a wrong MAC whose failed attempt is committed without consuming the row.
func TestAdminEmailOTPRepositoryVerifyAndConsume(t *testing.T) {
	correctMAC := bytesOfLength(0x51, adminEmailOTPCodeMACBytes)
	tests := []struct {
		name         string
		candidateMAC []byte
		wantQuery    string
		wantError    error
		wantAccount  adminAccount
	}{
		{
			name:         "matching MAC consumes",
			candidateMAC: append([]byte(nil), correctMAC...),
			wantQuery:    consumeAdminEmailOTPChallengeSQL,
			wantAccount: adminAccount{
				ID:    42,
				Email: "owner@example.com",
				Role:  adminRoleOwner,
			},
		},
		{
			name:         "wrong MAC increments",
			candidateMAC: bytesOfLength(0x52, adminEmailOTPCodeMACBytes),
			wantQuery:    incrementFailedAdminEmailOTPChallengeSQL,
			wantError:    errAdminEmailOTPAuthenticationFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			challengeRow := &adminRowScannerRecordingStub{
				scan: func(destinations ...any) error {
					*destinations[0].(*int64) = 42
					*destinations[1].(*string) = "owner@example.com"
					*destinations[2].(*string) = string(adminRoleOwner)
					*destinations[3].(*[]byte) = append([]byte(nil), correctMAC...)

					return nil
				},
			}
			transaction := &adminEmailOTPTransactionStub{
				execResults: []sql.Result{
					&adminSQLResultRecordingStub{rowsAffected: 1},
					&adminSQLResultRecordingStub{rowsAffected: 0},
					&adminSQLResultRecordingStub{rowsAffected: 1},
					&adminSQLResultRecordingStub{rowsAffected: 1},
				},
				rows: []adminRowScanner{
					&adminRowScannerRecordingStub{scan: func(destinations ...any) error {
						*destinations[0].(*int64) = 42
						return nil
					}},
					&adminRowScannerRecordingStub{scan: func(destinations ...any) error {
						*destinations[0].(*int64) = 42
						return nil
					}},
					challengeRow,
				},
			}
			begin := &adminEmailOTPBeginStub{transaction: transaction}
			repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}
			challengeHash := bytesOfLength(0x61, adminEmailOTPChallengeHashBytes)

			account, err := repository.VerifyAndConsume(
				t.Context(),
				challengeHash,
				test.candidateMAC,
			)
			if !errors.Is(err, test.wantError) {
				t.Errorf("error: got %v, want %v", err, test.wantError)
			}
			if account != test.wantAccount {
				t.Errorf("account: got %#v, want %#v", account, test.wantAccount)
			}
			if len(transaction.queryCalls) != 3 ||
				transaction.queryCalls[0].query != findAdminEmailOTPChallengeUserSQL ||
				transaction.queryCalls[1].query != lockAdminEmailOTPRateLimitSQL ||
				transaction.queryCalls[2].query !=
					lockUsableAdminEmailOTPChallengeSQL ||
				!reflect.DeepEqual(
					transaction.queryCalls[2].arguments,
					[]any{challengeHash, int64(42)},
				) {
				t.Errorf("locked challenge query: %#v", transaction.queryCalls)
			}
			if len(transaction.execCalls) != 4 ||
				transaction.execCalls[2].query != test.wantQuery {
				t.Errorf("attempt write: %#v", transaction.execCalls)
			}
			wantRateQuery := accountNeutralAdminEmailOTPRateWriteSQL
			if test.wantError != nil {
				wantRateQuery = incrementAdminEmailOTPFailedAttemptBudgetSQL
			}
			if transaction.execCalls[3].query != wantRateQuery {
				t.Errorf("rate write: %#v", transaction.execCalls[3])
			}
			if transaction.commits != 1 || transaction.rollbacks != 1 {
				t.Errorf(
					"transaction completion: commits=%d rollbacks=%d",
					transaction.commits,
					transaction.rollbacks,
				)
			}
		})
	}
}

// TestAdminEmailOTPRepositoryNeutralizesUnusableChallenges ensures missing,
// expired, consumed, exhausted, and inactive-user rows have one external result.
func TestAdminEmailOTPRepositoryNeutralizesUnusableChallenges(t *testing.T) {
	transaction := &adminEmailOTPTransactionStub{
		execResults: []sql.Result{
			&adminSQLResultRecordingStub{rowsAffected: 0},
			&adminSQLResultRecordingStub{rowsAffected: 0},
			&adminSQLResultRecordingStub{rowsAffected: 0},
			&adminSQLResultRecordingStub{rowsAffected: 0},
		},
		rows: []adminRowScanner{
			&adminRowScannerRecordingStub{
				scan: func(...any) error { return sql.ErrNoRows },
			},
			&adminRowScannerRecordingStub{
				scan: func(...any) error { return sql.ErrNoRows },
			},
			&adminRowScannerRecordingStub{
				scan: func(...any) error { return sql.ErrNoRows },
			},
		},
	}
	begin := &adminEmailOTPBeginStub{transaction: transaction}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}

	account, err := repository.VerifyAndConsume(
		t.Context(),
		bytesOfLength(0x61, adminEmailOTPChallengeHashBytes),
		bytesOfLength(0x51, adminEmailOTPCodeMACBytes),
	)
	if !errors.Is(err, errAdminEmailOTPAuthenticationFailed) {
		t.Errorf("unusable challenge: got %v", err)
	}
	if account != (adminAccount{}) {
		t.Errorf("unusable challenge returned account %#v", account)
	}
	if len(transaction.execCalls) != 4 || transaction.commits != 1 ||
		transaction.rollbacks != 1 {
		t.Errorf("unusable challenge did not complete neutral shape: %#v", transaction)
	}
}

func TestAdminEmailOTPRepositoryRejectsMalformedDerivedSecrets(t *testing.T) {
	begin := &adminEmailOTPBeginStub{}
	repository := &postgresAdminEmailOTPRepository{begin: begin.Begin}
	challenge := validAdminEmailOTPChallengeFixture()
	challenge.CodeMAC = challenge.CodeMAC[:adminEmailOTPCodeMACBytes-1]

	if err := repository.CreateChallenge(t.Context(), challenge); !errors.Is(
		err,
		errAdminEmailOTPChallengeInvalid,
	) {
		t.Errorf("short stored MAC: got %v", err)
	}
	if _, err := repository.VerifyAndConsume(
		t.Context(),
		bytesOfLength(0x61, adminEmailOTPChallengeHashBytes-1),
		bytesOfLength(0x51, adminEmailOTPCodeMACBytes),
	); !errors.Is(err, errAdminEmailOTPChallengeInvalid) {
		t.Errorf("short challenge hash: got %v", err)
	}
	if begin.calls != 0 {
		t.Errorf("malformed derived secrets opened %d transaction(s)", begin.calls)
	}
}

func TestNewPostgresAdminEmailOTPRepositoryRequiresDatabase(t *testing.T) {
	if _, err := newPostgresAdminEmailOTPRepository(nil); !errors.Is(
		err,
		errAdminEmailOTPRepositoryDatabaseRequired,
	) {
		t.Errorf("nil database: got %v", err)
	}
}

// stringsContainAny is local to redaction assertions and avoids printing any
// candidate secret while reporting a failure.
func stringsContainAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}

	return false
}
