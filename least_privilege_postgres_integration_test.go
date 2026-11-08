package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

// These fixed NOLOGIN role names are cluster-global and may exist only on the
// independently confirmed disposable integration cluster. The test refuses to
// reuse an existing role and removes only names it created.
const (
	stage25RuntimeSmokeRole     = "zafarmand_stage25_runtime_smoke"
	stage25MaintenanceSmokeRole = "zafarmand_stage25_maintenance_smoke"
	// The database-only integration acknowledgement cannot authorize CREATE ROLE,
	// because PostgreSQL roles are cluster-global. This independent exact value is
	// set only for an isolated disposable cluster such as the CI service.
	stage25ClusterConfirmationEnvironmentName = "ZAFARMAND_TEST_CLUSTER_CONFIRM"
	stage25ClusterConfirmationValue           = "stage25-disposable-postgresql-cluster"
)

// TestPostgresStage25LeastPrivilegeIntegration is a CI deployment-permission
// gate. It executes the real readiness, Contact, alternative-authentication,
// and destructive-retention SQL after SET LOCAL ROLE, proving that the narrow
// column and function grants are sufficient without giving the HTTP role
// deletion power or the maintenance role access to visitor/authentication data.
func TestPostgresStage25LeastPrivilegeIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	requireStage25DisposableClusterConfirmation(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed least-privilege integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close least-privilege integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	requireStage25RoleManagementAuthority(t, database)
	createStage25SmokeRoles(t, database)
	t.Cleanup(func() {
		dropStage25SmokeRoles(t, database)
	})
	grantStage25SmokePrivileges(t, database)

	assertStage25RuntimeSmoke(t, database)
	assertStage25MaintenanceSmoke(t, database)
	assertStage25ForbiddenSmokePrivileges(t, database)
}

// requireStage25DisposableClusterConfirmation keeps cluster-global role DDL
// behind an opt-in independent from the disposable-database guard. Absence is a
// safe skip for ordinary local PostgreSQL tests; a present typo is a hard error.
func requireStage25DisposableClusterConfirmation(t *testing.T) {
	t.Helper()
	confirmation, exists := os.LookupEnv(
		stage25ClusterConfirmationEnvironmentName,
	)
	enabled, valid := classifyStage25DisposableClusterConfirmation(
		confirmation,
		exists,
	)
	if !enabled {
		t.Skip(
			"least-privilege role test requires " +
				stage25ClusterConfirmationEnvironmentName,
		)
	}
	if !valid {
		t.Fatalf(
			"set %s to the documented disposable-cluster confirmation value",
			stage25ClusterConfirmationEnvironmentName,
		)
	}
}

// classifyStage25DisposableClusterConfirmation keeps exact-value policy
// deterministic and unit-testable without mutating process-wide state.
func classifyStage25DisposableClusterConfirmation(
	value string,
	exists bool,
) (enabled bool, valid bool) {
	if !exists || strings.TrimSpace(value) == "" {
		return false, true
	}

	return true, value == stage25ClusterConfirmationValue
}

// TestClassifyStage25DisposableClusterConfirmation proves that only the exact
// independent acknowledgement can enable cluster-global role mutation.
func TestClassifyStage25DisposableClusterConfirmation(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		exists      bool
		wantEnabled bool
		wantValid   bool
	}{
		{name: "absent", wantValid: true},
		{name: "empty", exists: true, wantValid: true},
		{name: "whitespace", value: " \t", exists: true, wantValid: true},
		{
			name:        "exact",
			value:       stage25ClusterConfirmationValue,
			exists:      true,
			wantEnabled: true,
			wantValid:   true,
		},
		{name: "wrong", value: "stage25-disposable-database", exists: true, wantEnabled: true},
		{name: "wrong case", value: "STAGE25-DISPOSABLE-POSTGRESQL-CLUSTER", exists: true, wantEnabled: true},
		{name: "trailing space", value: stage25ClusterConfirmationValue + " ", exists: true, wantEnabled: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			enabled, valid := classifyStage25DisposableClusterConfirmation(
				test.value,
				test.exists,
			)
			if enabled != test.wantEnabled || valid != test.wantValid {
				t.Errorf(
					"classification: got enabled=%t valid=%t, want enabled=%t valid=%t",
					enabled,
					valid,
					test.wantEnabled,
					test.wantValid,
				)
			}
		})
	}
}

// requireStage25RoleManagementAuthority makes missing CI role-management
// authority a failure instead of silently skipping the permission gate.
func requireStage25RoleManagementAuthority(t *testing.T, database *sql.DB) {
	t.Helper()
	var isSuperuser bool
	if err := database.QueryRowContext(
		t.Context(),
		`SELECT rolsuper
FROM pg_catalog.pg_roles
WHERE rolname = current_user`,
	).Scan(&isSuperuser); err != nil || !isSuperuser {
		t.Fatal("least-privilege integration requires disposable role-management authority")
	}
}

// createStage25SmokeRoles refuses collisions before atomically creating only
// the two fixed NOLOGIN roles authorized by the disposable-cluster guard.
func createStage25SmokeRoles(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, role := range []string{
		stage25RuntimeSmokeRole,
		stage25MaintenanceSmokeRole,
	} {
		var exists bool
		if err := database.QueryRowContext(
			t.Context(),
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)",
			role,
		).Scan(&exists); err != nil {
			t.Fatal("inspect least-privilege integration role")
		}
		if exists {
			t.Fatalf("refuse to reuse existing least-privilege test role %q", role)
		}
	}

	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal("begin least-privilege role creation")
	}
	defer func() { _ = transaction.Rollback() }()
	for _, statement := range []string{
		"CREATE ROLE " + stage25RuntimeSmokeRole + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT",
		"CREATE ROLE " + stage25MaintenanceSmokeRole + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT",
	} {
		if _, err := transaction.ExecContext(t.Context(), statement); err != nil {
			t.Fatal("create least-privilege integration role")
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal("commit least-privilege role creation")
	}
}

// dropStage25SmokeRoles removes grants before roles with a fresh bounded cleanup
// context; failure favors leaving an inspectable role over broadening deletion.
func dropStage25SmokeRoles(t *testing.T, database *sql.DB) {
	t.Helper()
	// testing.T cancels its context before Cleanup callbacks run. A fresh bounded
	// context keeps exact role cleanup available after either success or failure
	// without allowing it to block the test process indefinitely.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		migrationIntegrationCleanupTimeout,
	)
	defer cancel()
	for _, role := range []string{
		stage25MaintenanceSmokeRole,
		stage25RuntimeSmokeRole,
	} {
		if _, err := database.ExecContext(
			ctx,
			"DROP OWNED BY "+role,
		); err != nil {
			t.Errorf("remove privileges owned by least-privilege test role %q", role)
			continue
		}
		if _, err := database.ExecContext(
			ctx,
			"DROP ROLE "+role,
		); err != nil {
			t.Errorf("drop least-privilege test role %q", role)
		}
	}
}

// grantStage25SmokePrivileges mirrors only the base capabilities needed by the
// exercised paths plus the exact ledger, alternative-authentication, tombstone,
// and advisory-lock additions. It is intentionally not a broad all-feature
// runtime grant list.
func grantStage25SmokePrivileges(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		"GRANT USAGE ON SCHEMA public TO " + stage25RuntimeSmokeRole + ", " + stage25MaintenanceSmokeRole,
		"GRANT INSERT (submission_key, name, email, discipline, message), SELECT (submission_key, name, email, discipline, message) ON TABLE public.inquiries TO " + stage25RuntimeSmokeRole,
		"GRANT USAGE, SELECT ON SEQUENCE public.inquiries_id_seq TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (version, name, checksum) ON TABLE public.schema_migrations TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (submission_key_hash) ON TABLE public.inquiry_submission_tombstones TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (id, email, role, active) ON TABLE public.admin_users TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (user_id, window_started_at, failed_attempt_count, request_count, last_requested_at, updated_at), INSERT (user_id), UPDATE (window_started_at, failed_attempt_count, request_count, last_requested_at, updated_at) ON TABLE public.admin_email_otp_rate_limits TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (challenge_hash, user_id, code_mac, attempt_count, created_at, expires_at, consumed_at), INSERT (challenge_hash, user_id, code_mac, expires_at), UPDATE (attempt_count, consumed_at) ON TABLE public.admin_email_otp_challenges TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (issuer, subject, user_id), INSERT (issuer, subject, user_id), UPDATE (last_authenticated_at) ON TABLE public.admin_oidc_identities TO " + stage25RuntimeSmokeRole,
		"GRANT EXECUTE ON FUNCTION pg_catalog.pg_advisory_xact_lock_shared(bigint) TO " + stage25RuntimeSmokeRole,
		"GRANT SELECT (expires_at), DELETE ON TABLE public.admin_sessions TO " + stage25MaintenanceSmokeRole,
		"GRANT SELECT (id, submission_key, status, updated_at), DELETE ON TABLE public.inquiries TO " + stage25MaintenanceSmokeRole,
		"GRANT UPDATE (status) ON TABLE public.inquiries TO " + stage25MaintenanceSmokeRole,
		"GRANT SELECT (submission_key_hash), INSERT (submission_key_hash) ON TABLE public.inquiry_submission_tombstones TO " + stage25MaintenanceSmokeRole,
		"GRANT EXECUTE ON FUNCTION pg_catalog.pg_advisory_xact_lock(bigint), pg_catalog.sha256(bytea) TO " + stage25MaintenanceSmokeRole,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal("grant least-privilege integration capability")
		}
	}
}

// assertStage25RuntimeSmoke assumes the restricted runtime role and executes
// the actual readiness, shared-lock, tombstone-check, Contact INSERT, account
// lookup, email-OTP, and OIDC repository SQL.
func assertStage25RuntimeSmoke(t *testing.T, database *sql.DB) {
	t.Helper()
	adminUserID := insertMigrationIntegrationAdminAccess(
		t,
		database,
		"alternative-auth-runtime@example.test",
		"owner",
		0xa2,
		0xa3,
	)
	otpExpiresAt := readPostgresCurrentTime(t, database).Add(5 * time.Minute)
	transaction, err := database.BeginTx(
		t.Context(),
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		t.Fatal("begin least-privilege runtime smoke transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(
		t.Context(),
		"SET LOCAL ROLE "+stage25RuntimeSmokeRole,
	); err != nil {
		t.Fatal("assume least-privilege runtime role")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		acquireInquiryRetentionSharedLockSQL,
		inquiryRetentionAdvisoryLockID,
	); err != nil {
		t.Fatal("runtime role cannot acquire the Contact retention lock")
	}

	rows, err := transaction.QueryContext(
		t.Context(),
		operationalReadinessLedgerSQL,
	)
	if err != nil {
		t.Fatal("runtime role cannot read the migration ledger")
	}
	ledgerRows := 0
	for rows.Next() {
		var version int64
		var name string
		var checksum []byte
		if err := rows.Scan(&version, &name, &checksum); err != nil ||
			version <= 0 || name == "" || len(checksum) != sha256.Size {
			_ = rows.Close()
			t.Fatal("runtime role read an invalid migration-ledger row")
		}
		ledgerRows++
	}
	if err := rows.Close(); err != nil || ledgerRows != 11 {
		t.Fatal("runtime role did not read the complete migration ledger")
	}

	var accountID int64
	var accountEmail string
	var accountRole string
	if err := transaction.QueryRowContext(
		t.Context(),
		findActiveAdminAccountByEmailSQL,
		"alternative-auth-runtime@example.test",
	).Scan(&accountID, &accountEmail, &accountRole); err != nil ||
		accountID != adminUserID ||
		accountEmail != "alternative-auth-runtime@example.test" ||
		accountRole != "owner" {
		t.Fatal("runtime role cannot read an active administrator projection")
	}

	result, err := transaction.ExecContext(
		t.Context(),
		ensureAdminEmailOTPRateLimitSQL,
		adminUserID,
	)
	if err != nil {
		t.Fatal("runtime role cannot ensure an email OTP rate-limit row")
	}
	ensured, err := result.RowsAffected()
	if err != nil || ensured != 1 {
		t.Fatal("runtime email OTP smoke did not ensure exactly one rate-limit row")
	}
	var rateLimitUserID int64
	if err := transaction.QueryRowContext(
		t.Context(),
		lockAdminEmailOTPRateLimitSQL,
		adminUserID,
	).Scan(&rateLimitUserID); err != nil || rateLimitUserID != adminUserID {
		t.Fatal("runtime role cannot lock an email OTP rate-limit row")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		resetExpiredAdminEmailOTPRateLimitSQL,
		adminUserID,
	); err != nil {
		t.Fatal("runtime role cannot reset an expired email OTP rate-limit window")
	}
	result, err = transaction.ExecContext(
		t.Context(),
		reserveAdminEmailOTPRequestSQL,
		adminUserID,
	)
	if err != nil {
		t.Fatal("runtime role cannot reserve an email OTP issuance slot")
	}
	reserved, err := result.RowsAffected()
	if err != nil || reserved != 1 {
		t.Fatal("runtime email OTP smoke did not reserve exactly one issuance slot")
	}

	challengeHash := bytes.Repeat([]byte{0xa4}, adminEmailOTPChallengeHashBytes)
	codeMAC := bytes.Repeat([]byte{0xa5}, adminEmailOTPCodeMACBytes)
	if _, err := transaction.ExecContext(
		t.Context(),
		invalidateOutstandingAdminEmailOTPChallengesSQL,
		adminUserID,
	); err != nil {
		t.Fatal("runtime role cannot invalidate an outstanding email OTP challenge")
	}
	result, err = transaction.ExecContext(
		t.Context(),
		createAdminEmailOTPChallengeSQL,
		challengeHash,
		adminUserID,
		codeMAC,
		otpExpiresAt,
	)
	if err != nil {
		t.Fatal("runtime role cannot create an email OTP challenge")
	}
	created, err := result.RowsAffected()
	if err != nil || created != 1 {
		t.Fatal("runtime email OTP smoke did not create exactly one challenge")
	}
	var challengeUserID int64
	if err := transaction.QueryRowContext(
		t.Context(),
		findAdminEmailOTPChallengeUserSQL,
		challengeHash,
	).Scan(&challengeUserID); err != nil || challengeUserID != adminUserID {
		t.Fatal("runtime role cannot resolve an email OTP challenge owner")
	}

	var challengeAccountID int64
	var challengeEmail string
	var challengeRole string
	var storedCodeMAC []byte
	if err := transaction.QueryRowContext(
		t.Context(),
		lockUsableAdminEmailOTPChallengeSQL,
		challengeHash,
		adminUserID,
	).Scan(
		&challengeAccountID,
		&challengeEmail,
		&challengeRole,
		&storedCodeMAC,
	); err != nil ||
		challengeAccountID != adminUserID ||
		challengeEmail != accountEmail ||
		challengeRole != accountRole ||
		!bytes.Equal(storedCodeMAC, codeMAC) {
		t.Fatal("runtime role cannot lock and read a usable email OTP challenge")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		incrementAdminEmailOTPFailedAttemptBudgetSQL,
		adminUserID,
	); err != nil {
		t.Fatal("runtime role cannot record a failed email OTP attempt budget")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		consumeAdminEmailOTPChallengeSQL,
		challengeHash,
	); err != nil {
		t.Fatal("runtime role cannot consume an email OTP challenge")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		accountNeutralAdminEmailOTPRateWriteSQL,
		adminUserID,
	); err != nil {
		t.Fatal("runtime role cannot execute the account-neutral OTP rate write")
	}

	const oidcSubject = "least-privilege-runtime-subject"
	result, err = transaction.ExecContext(
		t.Context(),
		linkAdminOIDCIdentitySQL,
		migrationIntegrationOIDCIssuer,
		oidcSubject,
		adminUserID,
	)
	if err != nil {
		t.Fatal("runtime role cannot link an OIDC identity")
	}
	linked, err := result.RowsAffected()
	if err != nil || linked != 1 {
		t.Fatal("runtime OIDC smoke did not link exactly one identity")
	}
	if err := transaction.QueryRowContext(
		t.Context(),
		findActiveAdminAccountByOIDCIdentitySQL,
		migrationIntegrationOIDCIssuer,
		oidcSubject,
	).Scan(&accountID, &accountEmail, &accountRole); err != nil ||
		accountID != adminUserID ||
		accountEmail != "alternative-auth-runtime@example.test" ||
		accountRole != "owner" {
		t.Fatal("runtime role cannot resolve an active OIDC identity")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		recordAdminOIDCAuthenticationSQL,
		migrationIntegrationOIDCIssuer,
		oidcSubject,
	); err != nil {
		t.Fatal("runtime role cannot timestamp an OIDC authentication")
	}

	submissionKey := bytes.Repeat([]byte{0xa1}, inquirySubmissionTokenByteLength)
	submissionHash := sha256.Sum256(submissionKey)
	result, err = transaction.ExecContext(
		t.Context(),
		createInquirySQL,
		submissionKey,
		"Synthetic Least Privilege Visitor",
		"least-privilege-runtime@example.test",
		"products",
		"Synthetic permission smoke data only.",
		submissionHash[:],
	)
	if err != nil {
		t.Fatal("runtime role cannot execute the Contact insert")
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted != 1 {
		t.Fatal("runtime Contact smoke did not insert exactly one row")
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal("commit least-privilege runtime smoke transaction")
	}
}

// assertStage25MaintenanceSmoke proves the narrow role can take the exclusive
// lock, expire sessions, and tombstone/delete one archived synthetic inquiry.
func assertStage25MaintenanceSmoke(t *testing.T, database *sql.DB) {
	t.Helper()
	userID := insertMigrationIntegrationAdminAccess(
		t,
		database,
		"stage25-least-privilege@example.test",
		"owner",
		0xb1,
		0xb2,
	)
	insertMaintenanceIntegrationSession(
		t,
		database,
		userID,
		0xb3,
		0xb4,
		"CURRENT_TIMESTAMP - INTERVAL '3 hours'",
		"CURRENT_TIMESTAMP - INTERVAL '2 hours'",
		"NULL",
	)
	cutoff := readPostgresCurrentTime(t, database).Add(-24 * time.Hour)
	archived := insertMaintenanceIntegrationInquiry(
		t,
		database,
		0xb5,
		inquiryStatusArchived,
		cutoff.Add(-time.Hour),
	)

	transaction, err := database.BeginTx(
		t.Context(),
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		t.Fatal("begin least-privilege maintenance smoke transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(
		t.Context(),
		"SET LOCAL ROLE "+stage25MaintenanceSmokeRole,
	); err != nil {
		t.Fatal("assume least-privilege maintenance role")
	}
	if _, err := transaction.ExecContext(
		t.Context(),
		acquireInquiryRetentionExclusiveLockSQL,
		inquiryRetentionAdvisoryLockID,
	); err != nil {
		t.Fatal("maintenance role cannot acquire the retention lock")
	}

	var expiredSessions int64
	if err := transaction.QueryRowContext(
		t.Context(),
		deleteExpiredAdminSessionsSQL,
	).Scan(&expiredSessions); err != nil || expiredSessions != 1 {
		t.Fatal("maintenance role cannot delete one expired session")
	}
	var purgedInquiries int64
	if err := transaction.QueryRowContext(
		t.Context(),
		purgeArchivedInquiriesBeforeSQL,
		cutoff,
	).Scan(&purgedInquiries); err != nil || purgedInquiries != 1 {
		t.Fatal("maintenance role cannot tombstone and purge one archived inquiry")
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal("commit least-privilege maintenance smoke transaction")
	}
	assertMaintenanceIntegrationTombstone(t, database, archived.submissionKey)
}

// assertStage25ForbiddenSmokePrivileges guards the negative side of the ACL
// contract, including runtime deletion and maintenance access to personal or
// authentication data.
func assertStage25ForbiddenSmokePrivileges(t *testing.T, database *sql.DB) {
	t.Helper()
	checks := []struct {
		role      string
		relation  string
		privilege string
	}{
		{stage25RuntimeSmokeRole, "public.inquiries", "DELETE"},
		{stage25RuntimeSmokeRole, "public.admin_sessions", "DELETE"},
		{stage25RuntimeSmokeRole, "public.admin_email_otp_rate_limits", "DELETE"},
		{stage25RuntimeSmokeRole, "public.admin_email_otp_challenges", "DELETE"},
		{stage25RuntimeSmokeRole, "public.admin_oidc_identities", "DELETE"},
		{stage25RuntimeSmokeRole, "public.inquiry_submission_tombstones", "INSERT"},
		{stage25RuntimeSmokeRole, "public.schema_migrations", "UPDATE"},
		{stage25MaintenanceSmokeRole, "public.schema_migrations", "SELECT"},
		{stage25MaintenanceSmokeRole, "public.admin_users", "SELECT"},
		{stage25MaintenanceSmokeRole, "public.admin_email_otp_rate_limits", "SELECT"},
		{stage25MaintenanceSmokeRole, "public.admin_email_otp_rate_limits", "INSERT"},
		{stage25MaintenanceSmokeRole, "public.admin_email_otp_rate_limits", "UPDATE"},
		{stage25MaintenanceSmokeRole, "public.admin_email_otp_challenges", "INSERT"},
		{stage25MaintenanceSmokeRole, "public.admin_email_otp_challenges", "UPDATE"},
		{stage25MaintenanceSmokeRole, "public.admin_oidc_identities", "SELECT"},
	}
	for _, check := range checks {
		var granted bool
		if err := database.QueryRowContext(
			t.Context(),
			"SELECT pg_catalog.has_table_privilege($1, $2, $3)",
			check.role,
			check.relation,
			check.privilege,
		).Scan(&granted); err != nil {
			t.Fatal("inspect forbidden least-privilege table grant")
		}
		if granted {
			t.Errorf(
				"role %q unexpectedly has %s on %s",
				check.role,
				check.privilege,
				check.relation,
			)
		}
	}

	for _, column := range []string{"name", "email", "discipline", "message"} {
		var granted bool
		if err := database.QueryRowContext(
			t.Context(),
			"SELECT pg_catalog.has_column_privilege($1, $2, $3, 'SELECT')",
			stage25MaintenanceSmokeRole,
			"public.inquiries",
			column,
		).Scan(&granted); err != nil {
			t.Fatal("inspect forbidden maintenance inquiry-column grant")
		}
		if granted {
			t.Errorf("maintenance role unexpectedly reads inquiry column %q", column)
		}
	}

	columnChecks := []struct {
		role        string
		relation    string
		column      string
		privilege   string
		description string
	}{
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_users",
			column: "password_hash", privilege: "SELECT",
			description: "runtime password verifier read",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "challenge_hash", privilege: "UPDATE",
			description: "runtime OTP selector mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "user_id", privilege: "UPDATE",
			description: "runtime OTP owner mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "code_mac", privilege: "UPDATE",
			description: "runtime OTP MAC mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "expires_at", privilege: "UPDATE",
			description: "runtime OTP expiry mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_email_otp_rate_limits",
			column: "user_id", privilege: "UPDATE",
			description: "runtime OTP rate-limit owner mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_oidc_identities",
			column: "issuer", privilege: "UPDATE",
			description: "runtime OIDC issuer mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_oidc_identities",
			column: "subject", privilege: "UPDATE",
			description: "runtime OIDC subject mutation",
		},
		{
			role: stage25RuntimeSmokeRole, relation: "public.admin_oidc_identities",
			column: "user_id", privilege: "UPDATE",
			description: "runtime OIDC owner mutation",
		},
		{
			role: stage25MaintenanceSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "challenge_hash", privilege: "SELECT",
			description: "maintenance OTP selector read",
		},
		{
			role: stage25MaintenanceSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "user_id", privilege: "SELECT",
			description: "maintenance OTP owner read",
		},
		{
			role: stage25MaintenanceSmokeRole, relation: "public.admin_email_otp_challenges",
			column: "code_mac", privilege: "SELECT",
			description: "maintenance OTP MAC read",
		},
	}
	for _, check := range columnChecks {
		var granted bool
		if err := database.QueryRowContext(
			t.Context(),
			"SELECT pg_catalog.has_column_privilege($1, $2, $3, $4)",
			check.role,
			check.relation,
			check.column,
			check.privilege,
		).Scan(&granted); err != nil {
			t.Fatal("inspect forbidden least-privilege column grant")
		}
		if granted {
			t.Errorf("unexpectedly granted %s", check.description)
		}
	}

	for _, function := range []string{
		"pg_catalog.pg_advisory_xact_lock_shared(bigint)",
		"pg_catalog.pg_advisory_xact_lock(bigint)",
		"pg_catalog.sha256(bytea)",
	} {
		role := stage25MaintenanceSmokeRole
		if function == "pg_catalog.pg_advisory_xact_lock_shared(bigint)" {
			role = stage25RuntimeSmokeRole
		}
		var explicitlyGranted bool
		if err := database.QueryRowContext(
			t.Context(),
			`SELECT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_proc AS procedure
    CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) AS privilege
    JOIN pg_catalog.pg_roles AS grantee
      ON grantee.oid = privilege.grantee
    WHERE procedure.oid = $1::regprocedure
      AND grantee.rolname = $2
      AND privilege.privilege_type = 'EXECUTE'
)`,
			function,
			role,
		).Scan(&explicitlyGranted); err != nil {
			t.Fatal("inspect explicit least-privilege function grant")
		}
		if !explicitlyGranted {
			t.Errorf("role %q lacks explicit EXECUTE on %s", role, function)
		}
	}
}
