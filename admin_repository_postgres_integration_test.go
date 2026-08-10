package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestPostgresAdminRepositoryIntegration exercises behavior that recording
// stubs cannot prove: migration compatibility, bytea mapping, generated user
// IDs, active-user filtering, database-clock expiry, revocation, and the named
// duplicate-email constraint.
//
// It reuses the separately confirmed disposable `_test` database guard. Normal
// `go test ./...` runs therefore skip it and can never fall back to DATABASE_URL.
func TestPostgresAdminRepositoryIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin integration database: %v", err)
	}
	// Cleanup functions execute last-in, first-out: register pool closure before
	// schema cleanup so cleanup can still issue statements over this connection.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	repository, err := newPostgresAdminRepository(database)
	if err != nil {
		t.Fatalf("create live admin repository: %v", err)
	}
	passwordManager, err := newAdminPasswordManagerWithParameters(
		2,
		bytes.NewReader(bytes.Repeat([]byte{0x81}, adminPasswordSaltBytes)),
	)
	if err != nil {
		t.Fatalf("create integration password manager: %v", err)
	}
	passwordHash, err := passwordManager.Hash(
		"stage fifteen test password",
	)
	if err != nil {
		t.Fatalf("hash integration password: %v", err)
	}

	createdUser := adminUser{
		Email:        "  STAGE15.OWNER@Example.COM  ",
		PasswordHash: passwordHash,
		Role:         adminRoleOwner,
	}
	if err := repository.CreateUser(t.Context(), createdUser); err != nil {
		t.Fatalf("create live admin user: %v", err)
	}

	storedUser, err := repository.FindActiveUserByEmail(
		t.Context(),
		"STAGE15.OWNER@EXAMPLE.COM",
	)
	if err != nil {
		t.Fatalf("find live active admin user: %v", err)
	}
	if storedUser.ID <= 0 ||
		storedUser.Email != "stage15.owner@example.com" ||
		storedUser.PasswordHash != passwordHash ||
		storedUser.Role != adminRoleOwner {
		t.Errorf("stored admin user does not match normalized input: %#v", storedUser)
	}

	// The same address in a different case reaches the named normalized unique
	// constraint and returns only the safe duplicate category.
	err = repository.CreateUser(
		t.Context(),
		adminUser{
			Email:        "Stage15.Owner@Example.com",
			PasswordHash: passwordHash,
			Role:         adminRoleEditor,
		},
	)
	if !errors.Is(err, errAdminEmailAlreadyExists) {
		t.Fatalf("duplicate email error: got %v, want duplicate sentinel", err)
	}
	if strings.Contains(err.Error(), "stage15.owner@example.com") ||
		strings.Contains(err.Error(), passwordHash) {
		t.Error("duplicate email error exposes stored credential data")
	}

	serverNow := readPostgresCurrentTime(t, database)
	firstSession := adminSession{
		TokenHash:     bytesOfLength(0x91, adminSessionHashBytes),
		UserID:        storedUser.ID,
		CSRFTokenHash: bytesOfLength(0x92, adminSessionHashBytes),
		ExpiresAt:     serverNow.Add(2 * time.Hour),
	}
	if err := repository.CreateSession(t.Context(), firstSession); err != nil {
		t.Fatalf("create live admin session: %v", err)
	}
	assertLiveAdminIdentity(t, repository, firstSession, storedUser)

	if err := repository.DeleteSession(
		t.Context(),
		firstSession.TokenHash,
	); err != nil {
		t.Fatalf("revoke live admin session: %v", err)
	}
	if _, err := repository.FindIdentityBySessionHash(
		t.Context(),
		firstSession.TokenHash,
	); !errors.Is(err, errAdminSessionNotFound) {
		t.Errorf("revoked session lookup: got %v, want not-found sentinel", err)
	}
	if err := repository.DeleteSession(
		t.Context(),
		firstSession.TokenHash,
	); !errors.Is(err, errAdminSessionNotFound) {
		t.Errorf("repeat revocation: got %v, want not-found sentinel", err)
	}

	secondSession := adminSession{
		TokenHash:     bytesOfLength(0xa1, adminSessionHashBytes),
		UserID:        storedUser.ID,
		CSRFTokenHash: bytesOfLength(0xa2, adminSessionHashBytes),
		ExpiresAt:     serverNow.Add(3 * time.Hour),
	}
	if err := repository.CreateSession(t.Context(), secondSession); err != nil {
		t.Fatalf("create second live admin session: %v", err)
	}

	// Directly deactivating the fixture proves both repository reads exclude the
	// user even while the otherwise-valid session remains persisted.
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.admin_users
SET active = FALSE
WHERE id = $1`,
		storedUser.ID,
	); err != nil {
		t.Fatal("deactivate integration admin user")
	}
	if _, err := repository.FindActiveUserByEmail(
		t.Context(),
		storedUser.Email,
	); !errors.Is(err, errAdminUserNotFound) {
		t.Errorf("inactive user lookup: got %v, want not-found sentinel", err)
	}
	if _, err := repository.FindIdentityBySessionHash(
		t.Context(),
		secondSession.TokenHash,
	); !errors.Is(err, errAdminSessionNotFound) {
		t.Errorf("inactive-user session lookup: got %v, want not-found sentinel", err)
	}

	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.admin_users
SET active = TRUE
WHERE id = $1`,
		storedUser.ID,
	); err != nil {
		t.Fatal("reactivate integration admin user")
	}
	assertLiveAdminIdentity(t, repository, secondSession, storedUser)

	// Moving both timestamps into a valid but historical interval proves lookup
	// uses the database clock's expiry condition rather than trusting Go input.
	if _, err := database.ExecContext(
		t.Context(),
		`UPDATE public.admin_sessions
SET created_at = CURRENT_TIMESTAMP - INTERVAL '2 hours',
    expires_at = CURRENT_TIMESTAMP - INTERVAL '1 hour'
WHERE token_hash = $1`,
		secondSession.TokenHash,
	); err != nil {
		t.Fatal("expire integration admin session")
	}
	if _, err := repository.FindIdentityBySessionHash(
		t.Context(),
		secondSession.TokenHash,
	); !errors.Is(err, errAdminSessionNotFound) {
		t.Errorf("expired session lookup: got %v, want not-found sentinel", err)
	}
}

// assertLiveAdminIdentity compares every non-password field returned by the
// live join while keeping bearer and CSRF token digests in byte form.
func assertLiveAdminIdentity(
	t *testing.T,
	repository adminRepository,
	session adminSession,
	user adminUser,
) {
	t.Helper()

	identity, err := repository.FindIdentityBySessionHash(
		t.Context(),
		session.TokenHash,
	)
	if err != nil {
		t.Fatalf("find live admin identity: %v", err)
	}
	if identity.UserID != user.ID ||
		identity.Email != user.Email ||
		identity.Role != user.Role ||
		!bytes.Equal(identity.SessionTokenHash, session.TokenHash) ||
		!bytes.Equal(identity.CSRFTokenHash, session.CSRFTokenHash) ||
		!identity.SessionExpiresAt.Equal(session.ExpiresAt) {
		t.Errorf("live admin identity does not match session and user: %#v", identity)
	}
}
