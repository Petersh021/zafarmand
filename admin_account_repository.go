package main

import (
	"context"
	"database/sql"
	"errors"
)

// Alternative authentication resolves this password-free projection before a
// method-specific repository creates the same local session used by passwords.
type adminAccount struct {
	// ID is the stable admin_users identity used by sessions and provider links.
	ID int64
	// Email is the normalized address used for account-neutral OTP initiation.
	Email string
	// Role preserves the existing owner/editor authorization vocabulary.
	Role adminRole
}

// adminAccountReader exposes no password verifier to OTP or OIDC handlers.
type adminAccountReader interface {
	FindActiveAccountByEmail(context.Context, string) (adminAccount, error)
}

var (
	errAdminAccountRepositoryDatabaseRequired = errors.New(
		"create admin account repository: database is required",
	)
	errAdminAccountRepositoryDatabaseFailed = errors.New(
		"admin account repository database operation failed",
	)
)

const findActiveAdminAccountByEmailSQL = `SELECT
    id,
    email,
    role
FROM public.admin_users
WHERE email = $1
  AND active = TRUE`

// postgresAdminAccountRepository borrows the shared pool and returns only the
// authorization fields required after a non-password method succeeds.
type postgresAdminAccountRepository struct {
	queryRow adminQueryRow
}

var _ adminAccountReader = (*postgresAdminAccountRepository)(nil)

// newPostgresAdminAccountRepository binds password-free account lookup to the
// application-owned database without opening or closing another pool.
func newPostgresAdminAccountRepository(
	database *sql.DB,
) (*postgresAdminAccountRepository, error) {
	if database == nil {
		return nil, errAdminAccountRepositoryDatabaseRequired
	}

	return &postgresAdminAccountRepository{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// FindActiveAccountByEmail normalizes one mailbox and deliberately projects no
// password hash. Missing and inactive administrators share the existing safe
// not-found category used by password login.
func (repository *postgresAdminAccountRepository) FindActiveAccountByEmail(
	ctx context.Context,
	email string,
) (adminAccount, error) {
	if repository == nil || repository.queryRow == nil || ctx == nil {
		return adminAccount{}, errAdminAccountRepositoryDatabaseFailed
	}

	normalizedEmail, err := normalizeAdminEmail(email)
	if err != nil {
		return adminAccount{}, err
	}

	row := repository.queryRow(ctx, findActiveAdminAccountByEmailSQL, normalizedEmail)
	if row == nil {
		return adminAccount{}, errAdminAccountRepositoryDatabaseFailed
	}

	var account adminAccount
	var role string
	if err := row.Scan(&account.ID, &account.Email, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminAccount{}, errAdminUserNotFound
		}

		return adminAccount{}, errAdminAccountRepositoryDatabaseFailed
	}
	account.Role = adminRole(role)
	if !isValidAdminAccount(account) {
		return adminAccount{}, errAdminAccountRepositoryDatabaseFailed
	}

	return account, nil
}

// isValidAdminAccount prevents corrupt stored authorization data from crossing
// into a handler even when PostgreSQL constraints were bypassed externally.
func isValidAdminAccount(account adminAccount) bool {
	if account.ID <= 0 || !account.Role.valid() {
		return false
	}

	normalized, err := normalizeAdminEmail(account.Email)

	return err == nil && normalized == account.Email
}
