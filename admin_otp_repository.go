package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// Both values mirror migration 000011 and keep raw selectors and codes out
	// of persistence. The HTTP layer supplies independently keyed HMAC output.
	adminEmailOTPChallengeHashBytes = 32
	adminEmailOTPCodeMACBytes       = 32
	adminEmailOTPMaximumAttempts    = 5
	// The durable rate row survives challenge replacement. Together these two
	// ceilings keep a four-digit code usable without making challenge rotation a
	// brute-force reset or an unbounded email trigger.
	adminEmailOTPMaximumFailedAttemptsPerWindow = 10
	adminEmailOTPMaximumRequestsPerWindow       = 6
)

var (
	errAdminEmailOTPRepositoryDatabaseRequired = errors.New(
		"create admin email OTP repository: database is required",
	)
	errAdminEmailOTPRepositoryDatabaseFailed = errors.New(
		"admin email OTP repository database operation failed",
	)
	errAdminEmailOTPChallengeInvalid = errors.New(
		"admin email OTP challenge is invalid",
	)
	// Unknown, expired, consumed, exhausted, and incorrect challenges deliberately
	// share one result so callers cannot turn verification into an account oracle.
	errAdminEmailOTPAuthenticationFailed = errors.New(
		"admin email OTP authentication failed",
	)
	// errAdminEmailOTPRateLimited is kept inside the service boundary. HTTP
	// callers still receive the same account-neutral request response.
	errAdminEmailOTPRateLimited = errors.New(
		"admin email OTP request is rate limited",
	)
)

// adminEmailOTPChallenge contains only derived secrets. The raw browser
// selector and numeric code remain outside this persistence boundary.
type adminEmailOTPChallenge struct {
	ChallengeHash []byte
	UserID        int64
	CodeMAC       []byte
	ExpiresAt     time.Time
}

// adminEmailOTPRepository replaces outstanding challenges and atomically
// increments/consumes one candidate under a PostgreSQL row lock.
type adminEmailOTPRepository interface {
	CreateChallenge(context.Context, adminEmailOTPChallenge) error
	CancelChallenge(context.Context, []byte) error
	VerifyAndConsume(
		context.Context,
		[]byte,
		[]byte,
	) (adminAccount, error)
}

const (
	ensureAdminEmailOTPRateLimitSQL = `INSERT INTO public.admin_email_otp_rate_limits (
    user_id
)
SELECT users.id
FROM public.admin_users AS users
WHERE users.id = $1
  AND users.active = TRUE
ON CONFLICT (user_id) DO NOTHING`

	lockAdminEmailOTPRateLimitSQL = `SELECT limits.user_id
FROM public.admin_email_otp_rate_limits AS limits
JOIN public.admin_users AS users
  ON users.id = limits.user_id
WHERE limits.user_id = $1
  AND users.active = TRUE
FOR UPDATE OF limits`

	resetExpiredAdminEmailOTPRateLimitSQL = `UPDATE public.admin_email_otp_rate_limits
SET window_started_at = CURRENT_TIMESTAMP,
    failed_attempt_count = 0,
    request_count = 0,
    last_requested_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND window_started_at <= CURRENT_TIMESTAMP - INTERVAL '24 hours'`

	reserveAdminEmailOTPRequestSQL = `UPDATE public.admin_email_otp_rate_limits
SET request_count = request_count + 1,
    last_requested_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND failed_attempt_count < 10
  AND request_count < 6
  AND (
      last_requested_at IS NULL OR
      last_requested_at <= CURRENT_TIMESTAMP - INTERVAL '24 seconds'
  )`

	invalidateOutstandingAdminEmailOTPChallengesSQL = `UPDATE public.admin_email_otp_challenges
SET consumed_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND consumed_at IS NULL`

	createAdminEmailOTPChallengeSQL = `INSERT INTO public.admin_email_otp_challenges (
    challenge_hash,
    user_id,
    code_mac,
    expires_at
)
SELECT $1, users.id, $3, $4
FROM public.admin_users AS users
WHERE users.id = $2
  AND users.active = TRUE`

	findAdminEmailOTPChallengeUserSQL = `SELECT user_id
FROM public.admin_email_otp_challenges
WHERE challenge_hash = $1`

	lockUsableAdminEmailOTPChallengeSQL = `SELECT
    users.id,
    users.email,
    users.role,
    challenges.code_mac
FROM public.admin_email_otp_challenges AS challenges
JOIN public.admin_users AS users
  ON users.id = challenges.user_id
JOIN public.admin_email_otp_rate_limits AS limits
  ON limits.user_id = challenges.user_id
WHERE challenges.challenge_hash = $1
  AND challenges.user_id = $2
  AND challenges.consumed_at IS NULL
  AND challenges.expires_at > CURRENT_TIMESTAMP
  AND challenges.attempt_count < 5
  AND limits.failed_attempt_count < 10
  AND users.active = TRUE
FOR UPDATE OF challenges`

	incrementFailedAdminEmailOTPChallengeSQL = `UPDATE public.admin_email_otp_challenges
SET attempt_count = attempt_count + 1
WHERE challenge_hash = $1
  AND consumed_at IS NULL
  AND expires_at > CURRENT_TIMESTAMP
  AND attempt_count < 5`

	incrementAdminEmailOTPFailedAttemptBudgetSQL = `UPDATE public.admin_email_otp_rate_limits
SET failed_attempt_count = failed_attempt_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND failed_attempt_count < 10`

	accountNeutralAdminEmailOTPChallengeWriteSQL = `UPDATE public.admin_email_otp_challenges
SET attempt_count = attempt_count
WHERE challenge_hash = $1
  AND FALSE`

	accountNeutralAdminEmailOTPRateWriteSQL = `UPDATE public.admin_email_otp_rate_limits
SET updated_at = updated_at
WHERE user_id = $1
  AND FALSE`

	consumeAdminEmailOTPChallengeSQL = `UPDATE public.admin_email_otp_challenges
SET attempt_count = attempt_count + 1,
    consumed_at = CURRENT_TIMESTAMP
WHERE challenge_hash = $1
  AND consumed_at IS NULL
  AND expires_at > CURRENT_TIMESTAMP
  AND attempt_count < 5`

	cancelAdminEmailOTPChallengeSQL = `WITH cancelled AS (
    UPDATE public.admin_email_otp_challenges
    SET consumed_at = CURRENT_TIMESTAMP
    WHERE challenge_hash = $1
      AND consumed_at IS NULL
    RETURNING user_id
)
UPDATE public.admin_email_otp_rate_limits AS limits
SET request_count = limits.request_count - 1,
    last_requested_at = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM cancelled
WHERE limits.user_id = cancelled.user_id
  AND limits.request_count > 0`
)

// adminEmailOTPTransaction is the exact transaction surface needed to keep
// replacement, attempt accounting, and one-time consumption indivisible.
type adminEmailOTPTransaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) adminRowScanner
	Commit() error
	Rollback() error
}

// CancelChallenge makes a code unusable when the bounded delivery queue cannot
// accept it. This compensating update lets a visitor retry immediately instead
// of retaining a valid-but-undelivered challenge for the resend window.
func (repository *postgresAdminEmailOTPRepository) CancelChallenge(
	ctx context.Context,
	challengeHash []byte,
) error {
	if repository == nil || repository.begin == nil || ctx == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if len(challengeHash) != adminEmailOTPChallengeHashBytes {
		return errAdminEmailOTPChallengeInvalid
	}
	transaction, err := repository.begin(ctx, nil)
	if err != nil || transaction == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(
		ctx,
		cancelAdminEmailOTPChallengeSQL,
		challengeHash,
	)
	if err != nil || !adminResultAffectedExactlyOne(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if err := transaction.Commit(); err != nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	return nil
}

type adminEmailOTPBeginTransaction func(
	context.Context,
	*sql.TxOptions,
) (adminEmailOTPTransaction, error)

// sqlAdminEmailOTPTransaction adapts *sql.Tx without exposing it to unit tests.
type sqlAdminEmailOTPTransaction struct {
	transaction *sql.Tx
}

func (transaction *sqlAdminEmailOTPTransaction) ExecContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (sql.Result, error) {
	return transaction.transaction.ExecContext(ctx, query, arguments...)
}

func (transaction *sqlAdminEmailOTPTransaction) QueryRowContext(
	ctx context.Context,
	query string,
	arguments ...any,
) adminRowScanner {
	return transaction.transaction.QueryRowContext(ctx, query, arguments...)
}

func (transaction *sqlAdminEmailOTPTransaction) Commit() error {
	return transaction.transaction.Commit()
}

func (transaction *sqlAdminEmailOTPTransaction) Rollback() error {
	return transaction.transaction.Rollback()
}

// postgresAdminEmailOTPRepository borrows the shared pool and owns no key: it
// receives only fixed-width challenge digests and precomputed keyed MACs.
type postgresAdminEmailOTPRepository struct {
	begin adminEmailOTPBeginTransaction
}

var _ adminEmailOTPRepository = (*postgresAdminEmailOTPRepository)(nil)

func newPostgresAdminEmailOTPRepository(
	database *sql.DB,
) (*postgresAdminEmailOTPRepository, error) {
	if database == nil {
		return nil, errAdminEmailOTPRepositoryDatabaseRequired
	}

	return &postgresAdminEmailOTPRepository{
		begin: func(
			ctx context.Context,
			options *sql.TxOptions,
		) (adminEmailOTPTransaction, error) {
			transaction, err := database.BeginTx(ctx, options)
			if err != nil {
				return nil, err
			}

			return &sqlAdminEmailOTPTransaction{transaction: transaction}, nil
		},
	}, nil
}

// CreateChallenge reserves one durable per-user issuance slot, consumes every
// prior outstanding row, and inserts the new challenge in one transaction. A
// failed insert rolls the rate counter and invalidation back together.
func (repository *postgresAdminEmailOTPRepository) CreateChallenge(
	ctx context.Context,
	challenge adminEmailOTPChallenge,
) error {
	if repository == nil || repository.begin == nil || ctx == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if !isValidAdminEmailOTPChallenge(challenge) {
		return errAdminEmailOTPChallengeInvalid
	}

	transaction, err := repository.begin(ctx, nil)
	if err != nil || transaction == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(
		ctx,
		ensureAdminEmailOTPRateLimitSQL,
		challenge.UserID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	row := transaction.QueryRowContext(
		ctx,
		lockAdminEmailOTPRateLimitSQL,
		challenge.UserID,
	)
	if row == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	var lockedUserID int64
	if err := row.Scan(&lockedUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errAdminUserNotFound
		}

		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if lockedUserID != challenge.UserID {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	result, err = transaction.ExecContext(
		ctx,
		resetExpiredAdminEmailOTPRateLimitSQL,
		challenge.UserID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	result, err = transaction.ExecContext(
		ctx,
		reserveAdminEmailOTPRequestSQL,
		challenge.UserID,
	)
	if err != nil || result == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	reserved, err := result.RowsAffected()
	if err != nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if reserved == 0 {
		return errAdminEmailOTPRateLimited
	}
	if reserved != 1 {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	result, err = transaction.ExecContext(
		ctx,
		invalidateOutstandingAdminEmailOTPChallengesSQL,
		challenge.UserID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	result, err = transaction.ExecContext(
		ctx,
		createAdminEmailOTPChallengeSQL,
		challenge.ChallengeHash,
		challenge.UserID,
		challenge.CodeMAC,
		challenge.ExpiresAt,
	)
	if err != nil {
		if isAdminEmailOTPOutstandingChallengeConflict(err) {
			return errAdminEmailOTPRateLimited
		}

		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if result == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	switch rowsAffected {
	case 0:
		// The transaction rolls the preceding invalidation back. Missing and
		// deactivated users share the established account-neutral category.
		return errAdminUserNotFound
	case 1:
		// Continue to the commit below.
	default:
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if err := transaction.Commit(); err != nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	return nil
}

// A named partial-index conflict can still occur under an unexpected direct
// writer or transaction race. It converges on the same neutral rate-limit
// category rather than exposing a database constraint.
func isAdminEmailOTPOutstandingChallengeConflict(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName ==
			"admin_email_otp_challenges_outstanding_user_idx"
}

// VerifyAndConsume locks one still-usable challenge, compares equal-length MACs
// in constant time, and durably records every real attempt before returning.
func (repository *postgresAdminEmailOTPRepository) VerifyAndConsume(
	ctx context.Context,
	challengeHash []byte,
	candidateCodeMAC []byte,
) (adminAccount, error) {
	if repository == nil || repository.begin == nil || ctx == nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if len(challengeHash) != adminEmailOTPChallengeHashBytes ||
		len(candidateCodeMAC) != adminEmailOTPCodeMACBytes {
		return adminAccount{}, errAdminEmailOTPChallengeInvalid
	}

	transaction, err := repository.begin(ctx, nil)
	if err != nil || transaction == nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	defer func() { _ = transaction.Rollback() }()

	userRow := transaction.QueryRowContext(
		ctx,
		findAdminEmailOTPChallengeUserSQL,
		challengeHash,
	)
	if userRow == nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	var userID int64
	if err := userRow.Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminAccount{}, completeAccountNeutralAdminEmailOTPVerification(
				ctx,
				transaction,
				challengeHash,
			)
		}

		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if userID <= 0 {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}

	result, err := transaction.ExecContext(
		ctx,
		ensureAdminEmailOTPRateLimitSQL,
		userID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	rateRow := transaction.QueryRowContext(
		ctx,
		lockAdminEmailOTPRateLimitSQL,
		userID,
	)
	if rateRow == nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	var lockedUserID int64
	if err := rateRow.Scan(&lockedUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminAccount{}, commitAccountNeutralAdminEmailOTPVerification(
				ctx,
				transaction,
				challengeHash,
				userID,
			)
		}

		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if lockedUserID != userID {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}

	result, err = transaction.ExecContext(
		ctx,
		resetExpiredAdminEmailOTPRateLimitSQL,
		userID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}

	row := transaction.QueryRowContext(
		ctx,
		lockUsableAdminEmailOTPChallengeSQL,
		challengeHash,
		userID,
	)
	if row == nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}

	var account adminAccount
	var role string
	var storedCodeMAC []byte
	if err := row.Scan(
		&account.ID,
		&account.Email,
		&role,
		&storedCodeMAC,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminAccount{}, commitAccountNeutralAdminEmailOTPVerification(
				ctx,
				transaction,
				challengeHash,
				userID,
			)
		}

		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	account.Role = adminRole(role)
	if !isValidAdminAccount(account) ||
		len(storedCodeMAC) != adminEmailOTPCodeMACBytes {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}

	matched := subtle.ConstantTimeCompare(storedCodeMAC, candidateCodeMAC) == 1
	query := incrementFailedAdminEmailOTPChallengeSQL
	if matched {
		query = consumeAdminEmailOTPChallengeSQL
	}
	result, err = transaction.ExecContext(ctx, query, challengeHash)
	if err != nil || !adminResultAffectedExactlyOne(result) {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	rateQuery := accountNeutralAdminEmailOTPRateWriteSQL
	if !matched {
		rateQuery = incrementAdminEmailOTPFailedAttemptBudgetSQL
	}
	result, err = transaction.ExecContext(ctx, rateQuery, userID)
	if err != nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if !matched && !adminResultAffectedExactlyOne(result) {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if matched && !adminResultAffectedNonnegative(result) {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if err := transaction.Commit(); err != nil {
		return adminAccount{}, errAdminEmailOTPRepositoryDatabaseFailed
	}
	if !matched {
		return adminAccount{}, errAdminEmailOTPAuthenticationFailed
	}

	return account, nil
}

// completeAccountNeutralAdminEmailOTPVerification mirrors the database shape
// of a real failed attempt for a random browser selector. The reserved user ID
// zero can never satisfy the positive admin foreign key, so no row can change.
func completeAccountNeutralAdminEmailOTPVerification(
	ctx context.Context,
	transaction adminEmailOTPTransaction,
	challengeHash []byte,
) error {
	result, err := transaction.ExecContext(
		ctx,
		ensureAdminEmailOTPRateLimitSQL,
		int64(0),
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	rateRow := transaction.QueryRowContext(
		ctx,
		lockAdminEmailOTPRateLimitSQL,
		int64(0),
	)
	if rateRow == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	var ignoredUserID int64
	if err := rateRow.Scan(&ignoredUserID); !errors.Is(err, sql.ErrNoRows) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	result, err = transaction.ExecContext(
		ctx,
		resetExpiredAdminEmailOTPRateLimitSQL,
		int64(0),
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	challengeRow := transaction.QueryRowContext(
		ctx,
		lockUsableAdminEmailOTPChallengeSQL,
		challengeHash,
		int64(0),
	)
	if challengeRow == nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	var ignoredID int64
	if err := challengeRow.Scan(&ignoredID); !errors.Is(err, sql.ErrNoRows) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	return commitAccountNeutralAdminEmailOTPVerification(
		ctx,
		transaction,
		challengeHash,
		0,
	)
}

// commitAccountNeutralAdminEmailOTPVerification completes two harmless writes
// and commits, matching the round trips of a real wrong-code result without
// exposing whether the selector belonged to an active administrator.
func commitAccountNeutralAdminEmailOTPVerification(
	ctx context.Context,
	transaction adminEmailOTPTransaction,
	challengeHash []byte,
	userID int64,
) error {
	result, err := transaction.ExecContext(
		ctx,
		accountNeutralAdminEmailOTPChallengeWriteSQL,
		challengeHash,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	result, err = transaction.ExecContext(
		ctx,
		accountNeutralAdminEmailOTPRateWriteSQL,
		userID,
	)
	if err != nil || !adminResultAffectedNonnegative(result) {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}
	if err := transaction.Commit(); err != nil {
		return errAdminEmailOTPRepositoryDatabaseFailed
	}

	return errAdminEmailOTPAuthenticationFailed
}

func isValidAdminEmailOTPChallenge(challenge adminEmailOTPChallenge) bool {
	return challenge.UserID > 0 &&
		len(challenge.ChallengeHash) == adminEmailOTPChallengeHashBytes &&
		len(challenge.CodeMAC) == adminEmailOTPCodeMACBytes &&
		!challenge.ExpiresAt.IsZero()
}

// RowsAffected may legitimately be zero while replacing a user's first OTP.
func adminResultAffectedNonnegative(result sql.Result) bool {
	if result == nil {
		return false
	}

	rowsAffected, err := result.RowsAffected()

	return err == nil && rowsAffected >= 0
}
