package main

import (
	"context"
	"database/sql"
	"errors"
)

// Password-reset persistence errors are intentionally independent from SQL,
// account identifiers, hashes, and driver diagnostics.
var (
	// errAdminPasswordResetRepositoryDatabaseRequired prevents construction
	// without the short-lived operator-owned database pool.
	errAdminPasswordResetRepositoryDatabaseRequired = errors.New(
		"create admin password reset repository: database is required",
	)
	// errAdminPasswordResetRepositoryInvalid rejects malformed command inputs
	// before PostgreSQL receives them.
	errAdminPasswordResetRepositoryInvalid = errors.New(
		"admin password reset input is invalid",
	)
	// errAdminPasswordResetRepositoryFailed collapses absent/inactive accounts and
	// all PostgreSQL failures into one credential-free outcome.
	errAdminPasswordResetRepositoryFailed = errors.New(
		"admin password reset database operation failed",
	)
)

// resetAdminPasswordAndRevokeSessionsSQL performs both security-sensitive
// writes in one PostgreSQL statement. The row lock serializes competing reset
// commands, while the data-modifying CTE makes a hash update and revocation
// sweep commit or roll back together.
const resetAdminPasswordAndRevokeSessionsSQL = `WITH target AS (
    SELECT id
    FROM public.admin_users
    WHERE email = $1
      AND active = TRUE
    FOR UPDATE
),
updated_user AS (
    UPDATE public.admin_users AS users
    SET password_hash = $2,
        updated_at = CURRENT_TIMESTAMP
    FROM target
    WHERE users.id = target.id
    RETURNING users.id
),
revoked_sessions AS (
    UPDATE public.admin_sessions AS sessions
    SET revoked_at = CURRENT_TIMESTAMP
    FROM updated_user
    WHERE sessions.user_id = updated_user.id
      AND sessions.revoked_at IS NULL
    RETURNING 1
)
SELECT updated_user.id,
       (SELECT COUNT(*) FROM revoked_sessions)
FROM updated_user`

// postgresAdminPasswordResetRepository exists only for the offline operator
// command. The HTTP application's repository interface intentionally has no
// password-changing method.
type postgresAdminPasswordResetRepository struct {
	// queryRow executes the one statement and exposes only its bounded result.
	queryRow adminQueryRow
}

// Compile-time verification protects the command-only capability contract.
var _ adminPasswordResetter = (*postgresAdminPasswordResetRepository)(nil)

// newPostgresAdminPasswordResetRepository borrows a short-lived database pool
// owned and closed by executeAdminResetPasswordCommand.
func newPostgresAdminPasswordResetRepository(
	database *sql.DB,
) (*postgresAdminPasswordResetRepository, error) {
	if database == nil {
		return nil, errAdminPasswordResetRepositoryDatabaseRequired
	}

	return &postgresAdminPasswordResetRepository{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ResetPassword replaces one active administrator's encoded verifier and
// revokes every session present when the offline statement executes. It never
// reads or returns the previous verifier.
func (repository *postgresAdminPasswordResetRepository) ResetPassword(
	ctx context.Context,
	email string,
	passwordHash string,
) error {
	if ctx == nil || repository == nil || repository.queryRow == nil {
		return errAdminPasswordResetRepositoryFailed
	}

	normalizedEmail, err := normalizeAdminEmail(email)
	if err != nil || !isValidAdminPasswordHash(passwordHash) {
		return errAdminPasswordResetRepositoryInvalid
	}

	row := repository.queryRow(
		ctx,
		resetAdminPasswordAndRevokeSessionsSQL,
		normalizedEmail,
		passwordHash,
	)
	if row == nil {
		return errAdminPasswordResetRepositoryFailed
	}

	var userID int64
	var revokedSessionCount int64
	if err := row.Scan(&userID, &revokedSessionCount); err != nil {
		return errAdminPasswordResetRepositoryFailed
	}
	if userID <= 0 || revokedSessionCount < 0 {
		return errAdminPasswordResetRepositoryFailed
	}

	return nil
}
