package main

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

// Repository validation constants mirror the named PostgreSQL constraints so
// invalid CLI or HTTP data is rejected before it reaches the driver.
const (
	// adminEmailMinimumRunes mirrors admin_users_email_length.
	adminEmailMinimumRunes = 3
	// adminEmailMaximumRunes mirrors admin_users_email_length.
	adminEmailMaximumRunes = 254
	// adminPasswordHashMaximumRunes mirrors admin_users_password_hash_length.
	adminPasswordHashMaximumRunes = 255
	// adminSessionHashBytes mirrors both admin_sessions bytea length checks.
	adminSessionHashBytes = 32
	// adminUsersEmailUniqueConstraint is the only database violation that maps
	// to the public duplicate-email category.
	adminUsersEmailUniqueConstraint = "admin_users_email_unique"
)

// Stable repository errors deliberately contain no email address, credential,
// session hash, SQL statement, or driver detail.
var (
	// errAdminRepositoryDatabaseRequired prevents construction without the
	// application-owned PostgreSQL pool.
	errAdminRepositoryDatabaseRequired = errors.New(
		"create admin repository: database is required",
	)
	// errAdminRepositoryDatabaseFailed collapses unexpected PostgreSQL failures
	// into one safe category suitable for an HTTP or CLI boundary.
	errAdminRepositoryDatabaseFailed = errors.New(
		"admin repository database operation failed",
	)
	// errAdminUserNotFound reports that no active user has the requested email.
	errAdminUserNotFound = errors.New("active admin user not found")
	// errAdminSessionNotFound reports an unknown, expired, revoked, or otherwise
	// unusable session without revealing which condition occurred.
	errAdminSessionNotFound = errors.New("active admin session not found")
	// errAdminEmailAlreadyExists is the one intentionally classified PostgreSQL
	// constraint failure needed by the initial-user CLI.
	errAdminEmailAlreadyExists = errors.New("admin email already exists")
	// errAdminUserInvalid identifies an invalid role, email, ID, or stored hash
	// without echoing the rejected value.
	errAdminUserInvalid = errors.New("admin user is invalid")
	// errAdminSessionInvalid identifies a malformed user ID, hash, or expiry.
	errAdminSessionInvalid = errors.New("admin session is invalid")
)

// adminRole is a closed application-level vocabulary mirrored by the database
// check constraint and future authorization decisions.
type adminRole string

const (
	// adminRoleOwner may eventually manage users and all studio content.
	adminRoleOwner adminRole = "owner"
	// adminRoleEditor may eventually manage content without owner-only actions.
	adminRoleEditor adminRole = "editor"
)

// adminUser contains the credential and authorization record used during user
// creation and login. FindActiveUserByEmail returns only active records.
type adminUser struct {
	// ID is PostgreSQL's generated stable identity; creation inputs leave it zero.
	ID int64
	// Email is stored trimmed and lowercase for exact, unique lookup.
	Email string
	// PasswordHash is the versioned value produced by adminPasswordManager.
	PasswordHash string
	// Role is one of the two explicitly supported authorization roles.
	Role adminRole
}

// adminSession contains only derived token hashes; raw cookie and CSRF tokens
// must never cross the repository boundary.
type adminSession struct {
	// TokenHash is the SHA-256 digest used as the session's primary key.
	TokenHash []byte
	// UserID associates the session with one existing administrator.
	UserID int64
	// CSRFTokenHash is the digest used to verify state-changing admin requests.
	CSRFTokenHash []byte
	// ExpiresAt is the absolute instant after which lookup must reject the session.
	ExpiresAt time.Time
}

// adminIdentity is the authenticated result of joining one usable session to
// one active user. It intentionally excludes the user's password hash.
type adminIdentity struct {
	// UserID is the stable database identity used by authorization and auditing.
	UserID int64
	// Email is the normalized administrator address displayed by trusted pages.
	Email string
	// Role drives owner/editor authorization decisions.
	Role adminRole
	// SessionTokenHash identifies the persisted session without exposing its raw
	// bearer token.
	SessionTokenHash []byte
	// CSRFTokenHash verifies a separately supplied raw CSRF token.
	CSRFTokenHash []byte
	// SessionExpiresAt lets application code reason about the authenticated
	// session without extending its database-controlled lifetime.
	SessionExpiresAt time.Time
}

// adminRepository is the complete persistence contract needed by the Stage 15
// user-creation, login, authentication, and logout boundaries.
type adminRepository interface {
	CreateUser(context.Context, adminUser) error
	FindActiveUserByEmail(context.Context, string) (adminUser, error)
	CreateSession(context.Context, adminSession) error
	FindIdentityBySessionHash(
		context.Context,
		[]byte,
	) (adminIdentity, error)
	DeleteSession(context.Context, []byte) error
}

// Trusted statements remain constants while all administrator-controlled or
// randomly generated values travel as positional PostgreSQL parameters.
const (
	createAdminUserSQL = `INSERT INTO public.admin_users (
    email,
    password_hash,
    role
)
VALUES ($1, $2, $3)`

	findActiveAdminUserByEmailSQL = `SELECT
    id,
    email,
    password_hash,
    role
FROM public.admin_users
WHERE email = $1
  AND active = TRUE`

	createAdminSessionSQL = `INSERT INTO public.admin_sessions (
    token_hash,
    user_id,
    csrf_token_hash,
    expires_at
)
VALUES ($1, $2, $3, $4)`

	findAdminIdentityBySessionHashSQL = `SELECT
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

	revokeAdminSessionSQL = `UPDATE public.admin_sessions
SET revoked_at = CURRENT_TIMESTAMP
WHERE token_hash = $1
  AND revoked_at IS NULL`
)

// adminExecutor is the smallest database/sql write surface used by the
// PostgreSQL repository and by deterministic recording tests.
type adminExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// adminRowScanner is implemented by *sql.Row and by small unit-test stubs.
type adminRowScanner interface {
	Scan(...any) error
}

// adminQueryRow adapts database/sql's concrete *sql.Row result to the narrow
// scanner seam without introducing a mocking dependency.
type adminQueryRow func(context.Context, string, ...any) adminRowScanner

// postgresAdminRepository borrows the process-wide database pool. It neither
// opens nor closes that pool and is safe for concurrent handler use.
type postgresAdminRepository struct {
	// executor is the shared *sql.DB in production and a write recorder in tests.
	executor adminExecutor
	// queryRow adapts the shared pool's single-row reads to a mockable scanner.
	queryRow adminQueryRow
}

// Compile-time interface verification protects the HTTP/CLI dependency
// contract from accidental concrete method changes.
var _ adminRepository = (*postgresAdminRepository)(nil)

// newPostgresAdminRepository adapts the application-owned PostgreSQL pool to
// the narrow Stage 15 authentication repository.
func newPostgresAdminRepository(
	database *sql.DB,
) (*postgresAdminRepository, error) {
	if database == nil {
		return nil, errAdminRepositoryDatabaseRequired
	}

	return &postgresAdminRepository{
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

// CreateUser validates and normalizes a new administrator before inserting it.
// PostgreSQL generates the ID and applies active=true by default.
func (repository *postgresAdminRepository) CreateUser(
	ctx context.Context,
	user adminUser,
) error {
	if repository == nil || repository.executor == nil {
		return errAdminRepositoryDatabaseFailed
	}
	if user.ID != 0 || !user.Role.valid() ||
		!isValidAdminPasswordHash(user.PasswordHash) {
		return errAdminUserInvalid
	}

	normalizedEmail, err := normalizeAdminEmail(user.Email)
	if err != nil {
		return err
	}

	result, err := repository.executor.ExecContext(
		ctx,
		createAdminUserSQL,
		normalizedEmail,
		user.PasswordHash,
		string(user.Role),
	)
	if err != nil {
		if isAdminEmailUniqueViolation(err) {
			return errAdminEmailAlreadyExists
		}

		return errAdminRepositoryDatabaseFailed
	}
	if !adminResultAffectedExactlyOne(result) {
		return errAdminRepositoryDatabaseFailed
	}

	return nil
}

// FindActiveUserByEmail normalizes one plain mailbox address and returns its
// active credential record. Missing and inactive users share one safe result.
func (repository *postgresAdminRepository) FindActiveUserByEmail(
	ctx context.Context,
	email string,
) (adminUser, error) {
	if repository == nil || repository.queryRow == nil {
		return adminUser{}, errAdminRepositoryDatabaseFailed
	}

	normalizedEmail, err := normalizeAdminEmail(email)
	if err != nil {
		return adminUser{}, err
	}

	row := repository.queryRow(
		ctx,
		findActiveAdminUserByEmailSQL,
		normalizedEmail,
	)
	if row == nil {
		return adminUser{}, errAdminRepositoryDatabaseFailed
	}

	var user adminUser
	var role string
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&role,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminUser{}, errAdminUserNotFound
		}

		return adminUser{}, errAdminRepositoryDatabaseFailed
	}
	user.Role = adminRole(role)

	if !isValidStoredAdminUser(user) {
		return adminUser{}, errAdminRepositoryDatabaseFailed
	}

	return user, nil
}

// CreateSession persists only token digests and an absolute expiry. A raw
// bearer or CSRF token must remain in the calling security boundary.
func (repository *postgresAdminRepository) CreateSession(
	ctx context.Context,
	session adminSession,
) error {
	if repository == nil || repository.executor == nil {
		return errAdminRepositoryDatabaseFailed
	}
	if !isValidAdminSession(session) {
		return errAdminSessionInvalid
	}

	result, err := repository.executor.ExecContext(
		ctx,
		createAdminSessionSQL,
		session.TokenHash,
		session.UserID,
		session.CSRFTokenHash,
		session.ExpiresAt,
	)
	if err != nil {
		return errAdminRepositoryDatabaseFailed
	}
	if !adminResultAffectedExactlyOne(result) {
		return errAdminRepositoryDatabaseFailed
	}

	return nil
}

// FindIdentityBySessionHash authenticates only a fixed-width hash joined to an
// active user and a session that PostgreSQL considers unrevoked and unexpired.
func (repository *postgresAdminRepository) FindIdentityBySessionHash(
	ctx context.Context,
	hash []byte,
) (adminIdentity, error) {
	if repository == nil || repository.queryRow == nil {
		return adminIdentity{}, errAdminRepositoryDatabaseFailed
	}
	if len(hash) != adminSessionHashBytes {
		return adminIdentity{}, errAdminSessionInvalid
	}

	row := repository.queryRow(
		ctx,
		findAdminIdentityBySessionHashSQL,
		hash,
	)
	if row == nil {
		return adminIdentity{}, errAdminRepositoryDatabaseFailed
	}

	var identity adminIdentity
	var role string
	if err := row.Scan(
		&identity.UserID,
		&identity.Email,
		&role,
		&identity.SessionTokenHash,
		&identity.CSRFTokenHash,
		&identity.SessionExpiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminIdentity{}, errAdminSessionNotFound
		}

		return adminIdentity{}, errAdminRepositoryDatabaseFailed
	}
	identity.Role = adminRole(role)

	if !isValidAdminIdentity(identity) {
		return adminIdentity{}, errAdminRepositoryDatabaseFailed
	}

	return identity, nil
}

// DeleteSession preserves the audit row while revoking an active session at
// the database clock's current instant. Already revoked and unknown hashes are
// intentionally indistinguishable.
func (repository *postgresAdminRepository) DeleteSession(
	ctx context.Context,
	hash []byte,
) error {
	if repository == nil || repository.executor == nil {
		return errAdminRepositoryDatabaseFailed
	}
	if len(hash) != adminSessionHashBytes {
		return errAdminSessionInvalid
	}

	result, err := repository.executor.ExecContext(
		ctx,
		revokeAdminSessionSQL,
		hash,
	)
	if err != nil {
		return errAdminRepositoryDatabaseFailed
	}
	if result == nil {
		return errAdminRepositoryDatabaseFailed
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errAdminRepositoryDatabaseFailed
	}
	switch rowsAffected {
	case 1:
		return nil
	case 0:
		return errAdminSessionNotFound
	default:
		return errAdminRepositoryDatabaseFailed
	}
}

// valid reports whether a role belongs to the closed owner/editor vocabulary.
func (role adminRole) valid() bool {
	return role == adminRoleOwner || role == adminRoleEditor
}

// normalizeAdminEmail trims surrounding whitespace, applies the database's
// lowercase representation, and accepts exactly one plain mailbox address.
// Display-name syntax remains invalid even when net/mail can parse it.
func normalizeAdminEmail(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", errAdminUserInvalid
	}

	normalized := strings.ToLower(strings.TrimSpace(value))
	length := utf8.RuneCountInString(normalized)
	if length < adminEmailMinimumRunes || length > adminEmailMaximumRunes {
		return "", errAdminUserInvalid
	}

	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", errAdminUserInvalid
	}

	return normalized, nil
}

// isValidAdminPasswordHash checks storage-level invariants without coupling
// the repository to one password-manager implementation.
func isValidAdminPasswordHash(value string) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		value == strings.TrimSpace(value) &&
		value != "" &&
		utf8.RuneCountInString(value) <= adminPasswordHashMaximumRunes
}

// isValidStoredAdminUser defensively verifies data scanned from PostgreSQL
// before it becomes an authentication credential.
func isValidStoredAdminUser(user adminUser) bool {
	if user.ID <= 0 || !user.Role.valid() ||
		!isValidAdminPasswordHash(user.PasswordHash) {
		return false
	}

	normalized, err := normalizeAdminEmail(user.Email)

	return err == nil && normalized == user.Email
}

// isValidAdminSession verifies the invariants known before PostgreSQL applies
// its foreign-key and timestamp-order constraints.
func isValidAdminSession(session adminSession) bool {
	return session.UserID > 0 &&
		len(session.TokenHash) == adminSessionHashBytes &&
		len(session.CSRFTokenHash) == adminSessionHashBytes &&
		!session.ExpiresAt.IsZero()
}

// isValidAdminIdentity defensively verifies the complete joined authentication
// record before a handler trusts it.
func isValidAdminIdentity(identity adminIdentity) bool {
	if identity.UserID <= 0 || !identity.Role.valid() ||
		len(identity.SessionTokenHash) != adminSessionHashBytes ||
		len(identity.CSRFTokenHash) != adminSessionHashBytes ||
		identity.SessionExpiresAt.IsZero() {
		return false
	}

	normalized, err := normalizeAdminEmail(identity.Email)

	return err == nil && normalized == identity.Email
}

// adminResultAffectedExactlyOne safely inspects a single-row write result.
func adminResultAffectedExactlyOne(result sql.Result) bool {
	if result == nil {
		return false
	}

	rowsAffected, err := result.RowsAffected()

	return err == nil && rowsAffected == 1
}

// isAdminEmailUniqueViolation classifies only the named duplicate-email
// constraint. All other pgx and database/sql details remain redacted.
func isAdminEmailUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}

	return postgresError.Code == "23505" &&
		postgresError.ConstraintName == adminUsersEmailUniqueConstraint
}
