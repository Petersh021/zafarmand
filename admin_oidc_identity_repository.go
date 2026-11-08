package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

const adminOIDCIdentityComponentMaximumRunes = 255

var (
	errAdminOIDCIdentityRepositoryDatabaseRequired = errors.New(
		"create admin OIDC identity repository: database is required",
	)
	errAdminOIDCIdentityRepositoryDatabaseFailed = errors.New(
		"admin OIDC identity repository database operation failed",
	)
	errAdminOIDCIdentityInvalid = errors.New(
		"admin OIDC identity is invalid",
	)
	errAdminOIDCIdentityNotFound = errors.New(
		"active admin OIDC identity not found",
	)
	errAdminOIDCIdentityConflict = errors.New(
		"admin OIDC identity is already linked",
	)
)

// adminOIDCIdentity stores only the immutable verified provider key and its
// existing local administrator. Provider tokens and mutable claims stay out.
type adminOIDCIdentity struct {
	Issuer  string
	Subject string
	UserID  int64
}

// adminOIDCIdentityRepository binds, resolves, and timestamps existing active
// administrators without gaining user-creation or role-assignment authority.
type adminOIDCIdentityRepository interface {
	LinkIdentity(context.Context, adminOIDCIdentity) error
	FindActiveAccountByIdentity(
		context.Context,
		string,
		string,
	) (adminAccount, error)
	RecordAuthentication(context.Context, string, string) error
}

const (
	linkAdminOIDCIdentitySQL = `INSERT INTO public.admin_oidc_identities (
    issuer,
    subject,
    user_id
)
SELECT $1, $2, users.id
FROM public.admin_users AS users
WHERE users.id = $3
  AND users.active = TRUE`

	findActiveAdminAccountByOIDCIdentitySQL = `SELECT
    users.id,
    users.email,
    users.role
FROM public.admin_oidc_identities AS identities
JOIN public.admin_users AS users
  ON users.id = identities.user_id
WHERE identities.issuer = $1
  AND identities.subject = $2
  AND users.active = TRUE`

	recordAdminOIDCAuthenticationSQL = `UPDATE public.admin_oidc_identities AS identities
SET last_authenticated_at = CURRENT_TIMESTAMP
WHERE identities.issuer = $1
  AND identities.subject = $2
  AND EXISTS (
      SELECT 1
      FROM public.admin_users AS users
      WHERE users.id = identities.user_id
        AND users.active = TRUE
  )`
)

type postgresAdminOIDCIdentityRepository struct {
	executor adminExecutor
	queryRow adminQueryRow
}

var _ adminOIDCIdentityRepository = (*postgresAdminOIDCIdentityRepository)(nil)

func newPostgresAdminOIDCIdentityRepository(
	database *sql.DB,
) (*postgresAdminOIDCIdentityRepository, error) {
	if database == nil {
		return nil, errAdminOIDCIdentityRepositoryDatabaseRequired
	}

	return &postgresAdminOIDCIdentityRepository{
		executor: database,
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// LinkIdentity can bind only an already-active admin_users row. Named unique
// constraints collapse either provider-key or per-user issuer reuse safely.
func (repository *postgresAdminOIDCIdentityRepository) LinkIdentity(
	ctx context.Context,
	identity adminOIDCIdentity,
) error {
	if repository == nil || repository.executor == nil || ctx == nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	if !isValidAdminOIDCIdentity(identity) {
		return errAdminOIDCIdentityInvalid
	}

	result, err := repository.executor.ExecContext(
		ctx,
		linkAdminOIDCIdentitySQL,
		identity.Issuer,
		identity.Subject,
		identity.UserID,
	)
	if err != nil {
		if isAdminOIDCIdentityUniqueViolation(err) {
			return errAdminOIDCIdentityConflict
		}

		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	if result == nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	switch rowsAffected {
	case 1:
		return nil
	case 0:
		return errAdminUserNotFound
	default:
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
}

// FindActiveAccountByIdentity trusts only the previously linked issuer/subject
// pair and returns no provider token, email claim, or password verifier.
func (repository *postgresAdminOIDCIdentityRepository) FindActiveAccountByIdentity(
	ctx context.Context,
	issuer string,
	subject string,
) (adminAccount, error) {
	if repository == nil || repository.queryRow == nil || ctx == nil {
		return adminAccount{}, errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	if !isValidAdminOIDCIdentityComponents(issuer, subject) {
		return adminAccount{}, errAdminOIDCIdentityInvalid
	}

	row := repository.queryRow(
		ctx,
		findActiveAdminAccountByOIDCIdentitySQL,
		issuer,
		subject,
	)
	if row == nil {
		return adminAccount{}, errAdminOIDCIdentityRepositoryDatabaseFailed
	}

	var account adminAccount
	var role string
	if err := row.Scan(&account.ID, &account.Email, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminAccount{}, errAdminOIDCIdentityNotFound
		}

		return adminAccount{}, errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	account.Role = adminRole(role)
	if !isValidAdminAccount(account) {
		return adminAccount{}, errAdminOIDCIdentityRepositoryDatabaseFailed
	}

	return account, nil
}

// RecordAuthentication updates only an active linked account and reports an
// absent, unlinked, or deactivated identity through one neutral category.
func (repository *postgresAdminOIDCIdentityRepository) RecordAuthentication(
	ctx context.Context,
	issuer string,
	subject string,
) error {
	if repository == nil || repository.executor == nil || ctx == nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	if !isValidAdminOIDCIdentityComponents(issuer, subject) {
		return errAdminOIDCIdentityInvalid
	}

	result, err := repository.executor.ExecContext(
		ctx,
		recordAdminOIDCAuthenticationSQL,
		issuer,
		subject,
	)
	if err != nil || result == nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
	switch rowsAffected {
	case 1:
		return nil
	case 0:
		return errAdminOIDCIdentityNotFound
	default:
		return errAdminOIDCIdentityRepositoryDatabaseFailed
	}
}

func isValidAdminOIDCIdentity(identity adminOIDCIdentity) bool {
	return identity.UserID > 0 &&
		isValidAdminOIDCIdentityComponents(identity.Issuer, identity.Subject)
}

func isValidAdminOIDCIdentityComponents(issuer string, subject string) bool {
	return isValidAdminOIDCIdentityComponent(issuer) &&
		isValidAdminOIDCIdentityComponent(subject)
}

func isValidAdminOIDCIdentityComponent(value string) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		value != strings.TrimSpace(value) {
		return false
	}

	length := utf8.RuneCountInString(value)
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}

	return length >= 1 && length <= adminOIDCIdentityComponentMaximumRunes
}

func isAdminOIDCIdentityUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return false
	}

	return postgresError.ConstraintName == "admin_oidc_identities_pkey" ||
		postgresError.ConstraintName == "admin_oidc_identities_user_issuer_unique"
}
