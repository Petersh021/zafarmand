package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// adminExecutorRecordingStub records each write operation and supplies a
// controlled database/sql result or driver-like error.
type adminExecutorRecordingStub struct {
	result    sql.Result
	execError error
	calls     int
	context   context.Context
	query     string
	arguments []any
}

// ExecContext implements adminExecutor while preserving every call detail for
// SQL, parameter-order, and context assertions.
func (stub *adminExecutorRecordingStub) ExecContext(
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

// adminSQLResultRecordingStub controls affected-row inspection without a live
// PostgreSQL database.
type adminSQLResultRecordingStub struct {
	rowsAffected      int64
	rowsAffectedError error
	rowsAffectedCalls int
	lastInsertIDCalls int
}

// LastInsertId satisfies sql.Result and records an API that the repository
// must never use with PostgreSQL.
func (result *adminSQLResultRecordingStub) LastInsertId() (int64, error) {
	result.lastInsertIDCalls++

	return 0, errors.New("PostgreSQL does not expose LastInsertId")
}

// RowsAffected returns the configured single-write outcome.
func (result *adminSQLResultRecordingStub) RowsAffected() (int64, error) {
	result.rowsAffectedCalls++

	return result.rowsAffected, result.rowsAffectedError
}

// adminRowScannerRecordingStub delegates Scan to a test-specific function so
// user and joined-identity rows can share one narrow fake.
type adminRowScannerRecordingStub struct {
	scan      func(...any) error
	scanCalls int
}

// Scan implements adminRowScanner and records exactly one attempted read.
func (stub *adminRowScannerRecordingStub) Scan(destinations ...any) error {
	stub.scanCalls++
	if stub.scan == nil {
		return errors.New("admin row scanner has no result")
	}

	return stub.scan(destinations...)
}

// adminQueryRowRecordingStub records the complete read invocation and returns
// one controlled scanner.
type adminQueryRowRecordingStub struct {
	row       adminRowScanner
	calls     int
	context   context.Context
	query     string
	arguments []any
}

// QueryRow matches adminQueryRow and retains a copy of positional arguments.
func (stub *adminQueryRowRecordingStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) adminRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// adminRepositoryContextKey proves repository methods preserve their caller's
// exact cancellation and deadline context.
type adminRepositoryContextKey struct{}

// validAdminUserFixture returns a new-user input that satisfies all repository
// boundaries while intentionally requiring email normalization.
func validAdminUserFixture() adminUser {
	return adminUser{
		Email:        "  OWNER@Example.COM  ",
		PasswordHash: "pbkdf2-sha256$v=1$i=600000$salt$key",
		Role:         adminRoleOwner,
	}
}

// validAdminSessionFixture returns independent fixed-width hashes and a stable
// future expiry suitable for SQL argument assertions.
func validAdminSessionFixture() adminSession {
	return adminSession{
		TokenHash:     bytesOfLength(0x11, adminSessionHashBytes),
		UserID:        42,
		CSRFTokenHash: bytesOfLength(0x22, adminSessionHashBytes),
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

// bytesOfLength builds deterministic binary values without sharing mutable
// backing arrays between fixtures.
func bytesOfLength(value byte, length int) []byte {
	result := make([]byte, length)
	for index := range result {
		result[index] = value
	}

	return result
}

// TestNewPostgresAdminRepository verifies nil dependency rejection and confirms
// the valid constructor borrows the caller-owned pool.
func TestNewPostgresAdminRepository(t *testing.T) {
	repository, err := newPostgresAdminRepository(nil)
	if !errors.Is(err, errAdminRepositoryDatabaseRequired) {
		t.Fatalf("nil database error: got %v, want dependency sentinel", err)
	}
	if repository != nil {
		t.Errorf("nil database repository: got %#v, want nil", repository)
	}

	database := new(sql.DB)
	repository, err = newPostgresAdminRepository(database)
	if err != nil {
		t.Fatalf("valid database: %v", err)
	}
	if repository.executor != database || repository.queryRow == nil {
		t.Error("constructor did not borrow and adapt the supplied pool")
	}
}

// TestNormalizeAdminEmail verifies canonical storage, exact-mailbox syntax,
// rune limits, invalid UTF-8, and PostgreSQL's NUL boundary.
func TestNormalizeAdminEmail(t *testing.T) {
	invalidUTF8 := string([]byte{'a', '@', 0xff})
	tests := []struct {
		name     string
		input    string
		expected string
		valid    bool
	}{
		{name: "trim and lowercase", input: "  Owner@Example.COM\t", expected: "owner@example.com", valid: true},
		{name: "already normalized", input: "editor@example.com", expected: "editor@example.com", valid: true},
		{name: "empty"},
		{name: "too short", input: "a@"},
		{name: "too long", input: strings.Repeat("a", 250) + "@x.example"},
		{name: "display name", input: "Owner <owner@example.com>"},
		{name: "two addresses", input: "one@example.com, two@example.com"},
		{name: "missing domain", input: "owner@"},
		{name: "invalid UTF-8", input: invalidUTF8},
		{name: "NUL", input: "owner\x00@example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := normalizeAdminEmail(test.input)
			if test.valid {
				if err != nil {
					t.Fatalf("normalize valid address: %v", err)
				}
				if normalized != test.expected {
					t.Errorf("normalized: got %q, want %q", normalized, test.expected)
				}
				return
			}

			if !errors.Is(err, errAdminUserInvalid) {
				t.Fatalf("error: got %v, want user-invalid sentinel", err)
			}
			if normalized != "" {
				t.Errorf("normalized invalid address: got %q, want empty", normalized)
			}
		})
	}
}

// TestPostgresAdminRepositoryCreateUser verifies normalized exact email, role,
// hash, trusted SQL, positional parameters, and context propagation.
func TestPostgresAdminRepositoryCreateUser(t *testing.T) {
	result := &adminSQLResultRecordingStub{rowsAffected: 1}
	executor := &adminExecutorRecordingStub{result: result}
	repository := &postgresAdminRepository{executor: executor}
	user := validAdminUserFixture()
	ctx := context.WithValue(
		context.Background(),
		adminRepositoryContextKey{},
		"create-user-context",
	)

	if err := repository.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if executor.calls != 1 || executor.context != ctx {
		t.Errorf("executor calls/context: got %d/%v", executor.calls, executor.context)
	}
	const expectedSQL = `INSERT INTO public.admin_users (
    email,
    password_hash,
    role
)
VALUES ($1, $2, $3)`
	if executor.query != expectedSQL {
		t.Errorf("query:\n%s\nwant:\n%s", executor.query, expectedSQL)
	}
	expectedArguments := []any{
		"owner@example.com",
		user.PasswordHash,
		string(adminRoleOwner),
	}
	if !reflect.DeepEqual(executor.arguments, expectedArguments) {
		t.Errorf("arguments: got %#v, want %#v", executor.arguments, expectedArguments)
	}
	if result.rowsAffectedCalls != 1 || result.lastInsertIDCalls != 0 {
		t.Errorf(
			"result calls RowsAffected/LastInsertId: got %d/%d, want 1/0",
			result.rowsAffectedCalls,
			result.lastInsertIDCalls,
		)
	}
}

// TestPostgresAdminRepositoryCreateUserRejectsInvalidInput proves no SQL is
// attempted for malformed IDs, emails, stored hashes, or roles.
func TestPostgresAdminRepositoryCreateUserRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adminUser)
	}{
		{name: "preassigned ID", mutate: func(user *adminUser) { user.ID = 1 }},
		{name: "invalid email", mutate: func(user *adminUser) { user.Email = "not-an-email" }},
		{name: "unsupported role", mutate: func(user *adminUser) { user.Role = "administrator" }},
		{name: "empty hash", mutate: func(user *adminUser) { user.PasswordHash = "" }},
		{name: "padded hash", mutate: func(user *adminUser) { user.PasswordHash = " encoded " }},
		{name: "NUL hash", mutate: func(user *adminUser) { user.PasswordHash = "encoded\x00hash" }},
		{name: "oversized hash", mutate: func(user *adminUser) { user.PasswordHash = strings.Repeat("h", 256) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &adminExecutorRecordingStub{}
			repository := &postgresAdminRepository{executor: executor}
			user := validAdminUserFixture()
			test.mutate(&user)

			err := repository.CreateUser(context.Background(), user)
			if !errors.Is(err, errAdminUserInvalid) {
				t.Fatalf("error: got %v, want user-invalid sentinel", err)
			}
			if executor.calls != 0 {
				t.Errorf("executor calls: got %d, want 0", executor.calls)
			}
		})
	}
}

// TestPostgresAdminRepositoryCreateUserFailures verifies exact duplicate-email
// classification while redacting all other driver and result failures.
func TestPostgresAdminRepositoryCreateUserFailures(t *testing.T) {
	const sensitiveDetail = "owner@example.com password=database-secret"
	tests := []struct {
		name          string
		repository    *postgresAdminRepository
		expectedError error
	}{
		{name: "nil repository", expectedError: errAdminRepositoryDatabaseFailed},
		{name: "nil executor", repository: &postgresAdminRepository{}, expectedError: errAdminRepositoryDatabaseFailed},
		{
			name: "named duplicate email",
			repository: &postgresAdminRepository{executor: &adminExecutorRecordingStub{
				execError: &pgconn.PgError{Code: "23505", ConstraintName: adminUsersEmailUniqueConstraint, Detail: sensitiveDetail},
			}},
			expectedError: errAdminEmailAlreadyExists,
		},
		{
			name: "different unique constraint",
			repository: &postgresAdminRepository{executor: &adminExecutorRecordingStub{
				execError: &pgconn.PgError{Code: "23505", ConstraintName: "different_unique", Detail: sensitiveDetail},
			}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
		{
			name:          "driver failure",
			repository:    &postgresAdminRepository{executor: &adminExecutorRecordingStub{execError: errors.New(sensitiveDetail)}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
		{
			name:          "nil result",
			repository:    &postgresAdminRepository{executor: &adminExecutorRecordingStub{}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
		{
			name:          "RowsAffected failure",
			repository:    &postgresAdminRepository{executor: &adminExecutorRecordingStub{result: &adminSQLResultRecordingStub{rowsAffectedError: errors.New(sensitiveDetail)}}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
		{
			name:          "zero rows",
			repository:    &postgresAdminRepository{executor: &adminExecutorRecordingStub{result: &adminSQLResultRecordingStub{}}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
		{
			name:          "multiple rows",
			repository:    &postgresAdminRepository{executor: &adminExecutorRecordingStub{result: &adminSQLResultRecordingStub{rowsAffected: 2}}},
			expectedError: errAdminRepositoryDatabaseFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.repository.CreateUser(
				context.Background(),
				validAdminUserFixture(),
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if strings.Contains(err.Error(), "owner@example.com") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive detail: %q", err)
			}
		})
	}
}

// TestPostgresAdminRepositoryFindActiveUserByEmail verifies the exact active-
// user query, normalized lookup, scan ordering, and defensive record result.
func TestPostgresAdminRepositoryFindActiveUserByEmail(t *testing.T) {
	row := &adminRowScannerRecordingStub{scan: func(destinations ...any) error {
		if len(destinations) != 4 {
			return errors.New("expected four user destinations")
		}
		*destinations[0].(*int64) = 7
		*destinations[1].(*string) = "owner@example.com"
		*destinations[2].(*string) = "encoded-password-hash"
		*destinations[3].(*string) = "owner"

		return nil
	}}
	queryRow := &adminQueryRowRecordingStub{row: row}
	repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
	ctx := context.WithValue(context.Background(), adminRepositoryContextKey{}, "find-user")

	user, err := repository.FindActiveUserByEmail(ctx, " OWNER@Example.com ")
	if err != nil {
		t.Fatalf("find active user: %v", err)
	}
	expectedUser := adminUser{ID: 7, Email: "owner@example.com", PasswordHash: "encoded-password-hash", Role: adminRoleOwner}
	if !reflect.DeepEqual(user, expectedUser) {
		t.Errorf("user: got %#v, want %#v", user, expectedUser)
	}
	if queryRow.calls != 1 || queryRow.context != ctx || row.scanCalls != 1 {
		t.Errorf("query/scan/context not propagated: %d/%d/%v", queryRow.calls, row.scanCalls, queryRow.context)
	}
	const expectedSQL = `SELECT
    id,
    email,
    password_hash,
    role
FROM public.admin_users
WHERE email = $1
  AND active = TRUE`
	if queryRow.query != expectedSQL {
		t.Errorf("query:\n%s\nwant:\n%s", queryRow.query, expectedSQL)
	}
	if !reflect.DeepEqual(queryRow.arguments, []any{"owner@example.com"}) {
		t.Errorf("arguments: got %#v", queryRow.arguments)
	}
}

// TestPostgresAdminRepositoryFindActiveUserFailures verifies absent users,
// malformed stored records, driver redaction, and pre-query validation.
func TestPostgresAdminRepositoryFindActiveUserFailures(t *testing.T) {
	const sensitiveDetail = "owner@example.com database-secret"

	t.Run("invalid lookup email", func(t *testing.T) {
		queryRow := &adminQueryRowRecordingStub{}
		repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
		_, err := repository.FindActiveUserByEmail(context.Background(), "invalid")
		if !errors.Is(err, errAdminUserInvalid) || queryRow.calls != 0 {
			t.Errorf("invalid email: got error %v and %d queries", err, queryRow.calls)
		}
	})

	tests := []struct {
		name          string
		row           adminRowScanner
		expectedError error
	}{
		{name: "nil row", expectedError: errAdminRepositoryDatabaseFailed},
		{name: "not found or inactive", row: &adminRowScannerRecordingStub{scan: func(...any) error { return sql.ErrNoRows }}, expectedError: errAdminUserNotFound},
		{name: "driver failure", row: &adminRowScannerRecordingStub{scan: func(...any) error { return errors.New(sensitiveDetail) }}, expectedError: errAdminRepositoryDatabaseFailed},
		{name: "malformed stored record", row: &adminRowScannerRecordingStub{scan: func(destinations ...any) error {
			*destinations[0].(*int64) = 7
			*destinations[1].(*string) = "NOT-NORMALIZED@example.com"
			*destinations[2].(*string) = "encoded"
			*destinations[3].(*string) = "owner"
			return nil
		}}, expectedError: errAdminRepositoryDatabaseFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryRow := &adminQueryRowRecordingStub{row: test.row}
			repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
			user, err := repository.FindActiveUserByEmail(context.Background(), "owner@example.com")
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if user != (adminUser{}) {
				t.Errorf("user on failure: got %#v, want zero", user)
			}
			if strings.Contains(err.Error(), "owner@example.com") || strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive detail: %q", err)
			}
		})
	}
}

// TestPostgresAdminRepositoryCreateSession verifies fixed-width hashes remain
// binary SQL parameters in the documented order.
func TestPostgresAdminRepositoryCreateSession(t *testing.T) {
	result := &adminSQLResultRecordingStub{rowsAffected: 1}
	executor := &adminExecutorRecordingStub{result: result}
	repository := &postgresAdminRepository{executor: executor}
	session := validAdminSessionFixture()
	ctx := context.WithValue(context.Background(), adminRepositoryContextKey{}, "create-session")

	if err := repository.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	const expectedSQL = `INSERT INTO public.admin_sessions (
    token_hash,
    user_id,
    csrf_token_hash,
    expires_at
)
VALUES ($1, $2, $3, $4)`
	if executor.query != expectedSQL || executor.context != ctx {
		t.Errorf("query/context mismatch: %q / %v", executor.query, executor.context)
	}
	expectedArguments := []any{session.TokenHash, session.UserID, session.CSRFTokenHash, session.ExpiresAt}
	if !reflect.DeepEqual(executor.arguments, expectedArguments) {
		t.Errorf("arguments: got %#v, want %#v", executor.arguments, expectedArguments)
	}
}

// TestPostgresAdminRepositoryCreateSessionFailures verifies local session
// invariants and generic write-failure redaction.
func TestPostgresAdminRepositoryCreateSessionFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adminSession)
	}{
		{name: "missing user", mutate: func(session *adminSession) { session.UserID = 0 }},
		{name: "short token hash", mutate: func(session *adminSession) { session.TokenHash = session.TokenHash[:31] }},
		{name: "long token hash", mutate: func(session *adminSession) { session.TokenHash = append(session.TokenHash, 0) }},
		{name: "short CSRF hash", mutate: func(session *adminSession) { session.CSRFTokenHash = session.CSRFTokenHash[:31] }},
		{name: "missing expiry", mutate: func(session *adminSession) { session.ExpiresAt = time.Time{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &adminExecutorRecordingStub{}
			repository := &postgresAdminRepository{executor: executor}
			session := validAdminSessionFixture()
			test.mutate(&session)
			err := repository.CreateSession(context.Background(), session)
			if !errors.Is(err, errAdminSessionInvalid) || executor.calls != 0 {
				t.Errorf("invalid session: got error %v and %d calls", err, executor.calls)
			}
		})
	}

	for _, test := range []struct {
		name   string
		result sql.Result
		err    error
	}{
		{name: "driver", err: errors.New("token_hash=private database-secret")},
		{name: "nil result"},
		{name: "zero rows", result: &adminSQLResultRecordingStub{}},
		{name: "RowsAffected", result: &adminSQLResultRecordingStub{rowsAffectedError: errors.New("private")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &postgresAdminRepository{executor: &adminExecutorRecordingStub{result: test.result, execError: test.err}}
			err := repository.CreateSession(context.Background(), validAdminSessionFixture())
			if !errors.Is(err, errAdminRepositoryDatabaseFailed) {
				t.Fatalf("error: got %v, want database sentinel", err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes detail: %q", err)
			}
		})
	}
}

// TestPostgresAdminRepositoryFindIdentityBySessionHash verifies the complete
// active-user/unrevoked/unexpired join and result field ordering.
func TestPostgresAdminRepositoryFindIdentityBySessionHash(t *testing.T) {
	session := validAdminSessionFixture()
	row := &adminRowScannerRecordingStub{scan: func(destinations ...any) error {
		if len(destinations) != 6 {
			return errors.New("expected six identity destinations")
		}
		*destinations[0].(*int64) = session.UserID
		*destinations[1].(*string) = "editor@example.com"
		*destinations[2].(*string) = "editor"
		*destinations[3].(*[]byte) = append([]byte(nil), session.TokenHash...)
		*destinations[4].(*[]byte) = append([]byte(nil), session.CSRFTokenHash...)
		*destinations[5].(*time.Time) = session.ExpiresAt

		return nil
	}}
	queryRow := &adminQueryRowRecordingStub{row: row}
	repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
	ctx := context.WithValue(context.Background(), adminRepositoryContextKey{}, "find-session")

	identity, err := repository.FindIdentityBySessionHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("find identity: %v", err)
	}
	expectedIdentity := adminIdentity{
		UserID:           session.UserID,
		Email:            "editor@example.com",
		Role:             adminRoleEditor,
		SessionTokenHash: session.TokenHash,
		CSRFTokenHash:    session.CSRFTokenHash,
		SessionExpiresAt: session.ExpiresAt,
	}
	if !reflect.DeepEqual(identity, expectedIdentity) {
		t.Errorf("identity: got %#v, want %#v", identity, expectedIdentity)
	}
	const expectedSQL = `SELECT
    users.id,
    users.email,
    users.role,
    sessions.token_hash,
    sessions.csrf_token_hash,
    sessions.expires_at
FROM public.admin_sessions AS sessions
JOIN public.admin_users AS users
  ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
  AND sessions.revoked_at IS NULL
  AND sessions.expires_at > CURRENT_TIMESTAMP
  AND users.active = TRUE`
	if queryRow.query != expectedSQL || queryRow.context != ctx {
		t.Errorf("identity query/context mismatch:\n%s", queryRow.query)
	}
	if !reflect.DeepEqual(queryRow.arguments, []any{session.TokenHash}) {
		t.Errorf("arguments: got %#v", queryRow.arguments)
	}
}

// TestPostgresAdminRepositoryFindIdentityFailures verifies malformed hashes,
// missing sessions, driver redaction, and defensive joined-row checks.
func TestPostgresAdminRepositoryFindIdentityFailures(t *testing.T) {
	t.Run("invalid lookup hash", func(t *testing.T) {
		queryRow := &adminQueryRowRecordingStub{}
		repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
		_, err := repository.FindIdentityBySessionHash(context.Background(), make([]byte, 31))
		if !errors.Is(err, errAdminSessionInvalid) || queryRow.calls != 0 {
			t.Errorf("invalid hash: got error %v and %d queries", err, queryRow.calls)
		}
	})

	tests := []struct {
		name          string
		row           adminRowScanner
		expectedError error
	}{
		{name: "nil row", expectedError: errAdminRepositoryDatabaseFailed},
		{name: "missing expired revoked or inactive", row: &adminRowScannerRecordingStub{scan: func(...any) error { return sql.ErrNoRows }}, expectedError: errAdminSessionNotFound},
		{name: "driver failure", row: &adminRowScannerRecordingStub{scan: func(...any) error { return errors.New("editor@example.com token=private") }}, expectedError: errAdminRepositoryDatabaseFailed},
		{name: "malformed identity", row: &adminRowScannerRecordingStub{scan: func(destinations ...any) error {
			*destinations[0].(*int64) = 1
			*destinations[1].(*string) = "editor@example.com"
			*destinations[2].(*string) = "unsupported"
			*destinations[3].(*[]byte) = bytesOfLength(1, 32)
			*destinations[4].(*[]byte) = bytesOfLength(2, 32)
			*destinations[5].(*time.Time) = time.Now().Add(time.Hour)
			return nil
		}}, expectedError: errAdminRepositoryDatabaseFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryRow := &adminQueryRowRecordingStub{row: test.row}
			repository := &postgresAdminRepository{queryRow: queryRow.QueryRow}
			identity, err := repository.FindIdentityBySessionHash(context.Background(), bytesOfLength(3, 32))
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if !reflect.DeepEqual(identity, adminIdentity{}) {
				t.Errorf("identity on failure: got %#v, want zero", identity)
			}
			if strings.Contains(err.Error(), "editor@example.com") || strings.Contains(err.Error(), "private") {
				t.Errorf("error exposes sensitive detail: %q", err)
			}
		})
	}
}

// TestPostgresAdminRepositoryDeleteSession verifies logout revokes one row
// using only the fixed-width session digest.
func TestPostgresAdminRepositoryDeleteSession(t *testing.T) {
	hash := bytesOfLength(0x71, adminSessionHashBytes)
	executor := &adminExecutorRecordingStub{result: &adminSQLResultRecordingStub{rowsAffected: 1}}
	repository := &postgresAdminRepository{executor: executor}
	ctx := context.WithValue(context.Background(), adminRepositoryContextKey{}, "delete-session")

	if err := repository.DeleteSession(ctx, hash); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	const expectedSQL = `UPDATE public.admin_sessions
SET revoked_at = CURRENT_TIMESTAMP
WHERE token_hash = $1
  AND revoked_at IS NULL`
	if executor.query != expectedSQL || executor.context != ctx {
		t.Errorf("revoke query/context mismatch: %q / %v", executor.query, executor.context)
	}
	if !reflect.DeepEqual(executor.arguments, []any{hash}) {
		t.Errorf("arguments: got %#v", executor.arguments)
	}
}

// TestPostgresAdminRepositoryDeleteSessionFailures verifies local validation,
// not-found classification, impossible row counts, and driver redaction.
func TestPostgresAdminRepositoryDeleteSessionFailures(t *testing.T) {
	t.Run("invalid hash", func(t *testing.T) {
		executor := &adminExecutorRecordingStub{}
		repository := &postgresAdminRepository{executor: executor}
		err := repository.DeleteSession(context.Background(), make([]byte, 31))
		if !errors.Is(err, errAdminSessionInvalid) || executor.calls != 0 {
			t.Errorf("invalid hash: got error %v and %d calls", err, executor.calls)
		}
	})

	tests := []struct {
		name          string
		result        sql.Result
		execError     error
		expectedError error
	}{
		{name: "unknown or already revoked", result: &adminSQLResultRecordingStub{}, expectedError: errAdminSessionNotFound},
		{name: "driver", execError: errors.New("token=private database-secret"), expectedError: errAdminRepositoryDatabaseFailed},
		{name: "nil result", expectedError: errAdminRepositoryDatabaseFailed},
		{name: "RowsAffected", result: &adminSQLResultRecordingStub{rowsAffectedError: errors.New("private")}, expectedError: errAdminRepositoryDatabaseFailed},
		{name: "multiple rows", result: &adminSQLResultRecordingStub{rowsAffected: 2}, expectedError: errAdminRepositoryDatabaseFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &postgresAdminRepository{executor: &adminExecutorRecordingStub{result: test.result, execError: test.execError}}
			err := repository.DeleteSession(context.Background(), bytesOfLength(1, 32))
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive detail: %q", err)
			}
		})
	}
}

// TestNilPostgresAdminRepositoryMethods confirms every method returns a stable
// safe sentinel instead of panicking on a missing concrete dependency.
func TestNilPostgresAdminRepositoryMethods(t *testing.T) {
	var repository *postgresAdminRepository
	if err := repository.CreateUser(context.Background(), validAdminUserFixture()); !errors.Is(err, errAdminRepositoryDatabaseFailed) {
		t.Errorf("CreateUser nil receiver: %v", err)
	}
	if _, err := repository.FindActiveUserByEmail(context.Background(), "owner@example.com"); !errors.Is(err, errAdminRepositoryDatabaseFailed) {
		t.Errorf("FindActiveUserByEmail nil receiver: %v", err)
	}
	if err := repository.CreateSession(context.Background(), validAdminSessionFixture()); !errors.Is(err, errAdminRepositoryDatabaseFailed) {
		t.Errorf("CreateSession nil receiver: %v", err)
	}
	if _, err := repository.FindIdentityBySessionHash(context.Background(), bytesOfLength(1, 32)); !errors.Is(err, errAdminRepositoryDatabaseFailed) {
		t.Errorf("FindIdentityBySessionHash nil receiver: %v", err)
	}
	if err := repository.DeleteSession(context.Background(), bytesOfLength(1, 32)); !errors.Is(err, errAdminRepositoryDatabaseFailed) {
		t.Errorf("DeleteSession nil receiver: %v", err)
	}
}
