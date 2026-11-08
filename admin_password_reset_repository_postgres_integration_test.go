package main

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// TestPostgresAdminPasswordResetRepositoryIntegration proves the real
// data-modifying CTE changes only the password verifier and revokes sessions in
// the disposable, explicitly confirmed PostgreSQL integration database.
func TestPostgresAdminPasswordResetRepositoryIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)
	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed password-reset integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close password-reset integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	adminRepository, err := newPostgresAdminRepository(database)
	if err != nil {
		t.Fatalf("create integration admin repository: %v", err)
	}
	resetRepository, err := newPostgresAdminPasswordResetRepository(database)
	if err != nil {
		t.Fatalf("create integration reset repository: %v", err)
	}
	passwords, err := newAdminPasswordManagerWithParameters(
		2,
		bytes.NewReader(bytes.Repeat([]byte{0x71}, 2*adminPasswordSaltBytes)),
	)
	if err != nil {
		t.Fatalf("create integration password manager: %v", err)
	}

	oldPassword := "old integration administrator password"
	newPassword := "new integration administrator password"
	oldHash, err := passwords.Hash(oldPassword)
	if err != nil {
		t.Fatalf("hash old integration password: %v", err)
	}
	newHash, err := passwords.Hash(newPassword)
	if err != nil {
		t.Fatalf("hash new integration password: %v", err)
	}

	const email = "password.reset.owner@example.com"
	if err := adminRepository.CreateUser(
		t.Context(),
		adminUser{
			Email:        email,
			PasswordHash: oldHash,
			Role:         adminRoleOwner,
		},
	); err != nil {
		t.Fatalf("create integration reset user: %v", err)
	}
	userBefore, err := adminRepository.FindActiveUserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("read integration reset user: %v", err)
	}

	session := adminSession{
		TokenHash:     bytesOfLength(0xb1, adminSessionHashBytes),
		UserID:        userBefore.ID,
		CSRFTokenHash: bytesOfLength(0xb2, adminSessionHashBytes),
		ExpiresAt:     readPostgresCurrentTime(t, database).Add(2 * time.Hour),
	}
	if err := adminRepository.CreateSession(t.Context(), session); err != nil {
		t.Fatalf("create integration pre-reset session: %v", err)
	}

	if err := resetRepository.ResetPassword(
		t.Context(),
		email,
		newHash,
	); err != nil {
		t.Fatalf("reset integration administrator password: %v", err)
	}

	userAfter, err := adminRepository.FindActiveUserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("read reset integration user: %v", err)
	}
	if userAfter.ID != userBefore.ID || userAfter.Email != userBefore.Email ||
		userAfter.Role != userBefore.Role {
		t.Errorf("reset changed administrator identity: %#v", userAfter)
	}
	if userAfter.PasswordHash != newHash {
		t.Error("reset did not persist the replacement verifier")
	}
	oldMatches, err := passwords.Verify(oldPassword, userAfter.PasswordHash)
	if err != nil {
		t.Fatalf("verify old integration password: %v", err)
	}
	newMatches, err := passwords.Verify(newPassword, userAfter.PasswordHash)
	if err != nil {
		t.Fatalf("verify new integration password: %v", err)
	}
	if oldMatches || !newMatches {
		t.Errorf(
			"password matches after reset: old=%t new=%t",
			oldMatches,
			newMatches,
		)
	}
	if _, err := adminRepository.FindIdentityBySessionHash(
		t.Context(),
		session.TokenHash,
	); !errors.Is(err, errAdminSessionNotFound) {
		t.Errorf("pre-reset session lookup: got %v, want not found", err)
	}
}
