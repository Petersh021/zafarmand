package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// repeatingAdminEntropy is an infinite deterministic byte source used only by
// fast password and token tests. Production never receives this implementation.
type repeatingAdminEntropy byte

// Read fills every requested byte and therefore cannot run out between tests.
func (value repeatingAdminEntropy) Read(target []byte) (int, error) {
	for index := range target {
		target[index] = byte(value)
	}

	return len(target), nil
}

// recordingAdminRepository is a concurrency-safe in-memory implementation of
// the Stage 15 repository contract. It lets route tests exercise complete
// login/session/logout behavior without connecting to PostgreSQL.
type recordingAdminRepository struct {
	mu               sync.Mutex
	nextUserID       int64
	users            map[string]adminUser
	sessions         map[string]adminSession
	createdSessions  []adminSession
	revokedHashes    [][]byte
	findUserError    error
	createSessionErr error
	findSessionError error
	deleteSessionErr error
	now              func() time.Time
}

// newRecordingAdminRepository initializes all maps and a real-time clock so
// methods remain safe even when a caller does not configure any special case.
func newRecordingAdminRepository() *recordingAdminRepository {
	return &recordingAdminRepository{
		nextUserID: 1,
		users:      make(map[string]adminUser),
		sessions:   make(map[string]adminSession),
		now:        time.Now,
	}
}

// CreateUser records one valid normalized user and assigns a test-only ID when
// its input represents a new account.
func (repository *recordingAdminRepository) CreateUser(
	_ context.Context,
	user adminUser,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	normalized, err := normalizeAdminEmail(user.Email)
	if err != nil || !user.Role.valid() ||
		!isValidAdminPasswordHash(user.PasswordHash) {
		return errAdminUserInvalid
	}
	if _, exists := repository.users[normalized]; exists {
		return errAdminEmailAlreadyExists
	}
	if user.ID == 0 {
		user.ID = repository.nextUserID
		repository.nextUserID++
	}
	user.Email = normalized
	repository.users[normalized] = user

	return nil
}

// FindActiveUserByEmail returns one copied test user or its configured safe
// failure category.
func (repository *recordingAdminRepository) FindActiveUserByEmail(
	_ context.Context,
	email string,
) (adminUser, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if repository.findUserError != nil {
		return adminUser{}, repository.findUserError
	}
	normalized, err := normalizeAdminEmail(email)
	if err != nil {
		return adminUser{}, errAdminUserInvalid
	}
	user, exists := repository.users[normalized]
	if !exists {
		return adminUser{}, errAdminUserNotFound
	}

	return user, nil
}

// CreateSession copies both digest slices before recording the session, which
// prevents later caller mutation from changing repository state.
func (repository *recordingAdminRepository) CreateSession(
	_ context.Context,
	session adminSession,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if repository.createSessionErr != nil {
		return repository.createSessionErr
	}
	if !isValidAdminSession(session) {
		return errAdminSessionInvalid
	}
	if _, exists := repository.userByID(session.UserID); !exists {
		return errAdminRepositoryDatabaseFailed
	}

	session.TokenHash = append([]byte(nil), session.TokenHash...)
	session.CSRFTokenHash = append([]byte(nil), session.CSRFTokenHash...)
	repository.sessions[string(session.TokenHash)] = session
	repository.createdSessions = append(
		repository.createdSessions,
		copyAdminSession(session),
	)

	return nil
}

// FindIdentityBySessionHash reconstructs the same validated join shape returned
// by PostgreSQL and rejects missing or expired test sessions.
func (repository *recordingAdminRepository) FindIdentityBySessionHash(
	_ context.Context,
	hash []byte,
) (adminIdentity, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if repository.findSessionError != nil {
		return adminIdentity{}, repository.findSessionError
	}
	session, exists := repository.sessions[string(hash)]
	if !exists || !repository.now().Before(session.ExpiresAt) {
		return adminIdentity{}, errAdminSessionNotFound
	}
	user, exists := repository.userByID(session.UserID)
	if !exists {
		return adminIdentity{}, errAdminSessionNotFound
	}

	return adminIdentity{
		UserID:           user.ID,
		Email:            user.Email,
		Role:             user.Role,
		SessionTokenHash: append([]byte(nil), session.TokenHash...),
		CSRFTokenHash:    append([]byte(nil), session.CSRFTokenHash...),
		SessionExpiresAt: session.ExpiresAt,
	}, nil
}

// DeleteSession records the digest and removes its active in-memory row,
// modeling the externally observable behavior of PostgreSQL revocation.
func (repository *recordingAdminRepository) DeleteSession(
	_ context.Context,
	hash []byte,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if repository.deleteSessionErr != nil {
		return repository.deleteSessionErr
	}
	key := string(hash)
	if _, exists := repository.sessions[key]; !exists {
		return errAdminSessionNotFound
	}
	repository.revokedHashes = append(
		repository.revokedHashes,
		append([]byte(nil), hash...),
	)
	delete(repository.sessions, key)

	return nil
}

// addUser hashes and records one login fixture through the same manager used by
// the application under test.
func (repository *recordingAdminRepository) addUser(
	t *testing.T,
	passwords adminPasswordManager,
	email string,
	password string,
	role adminRole,
) adminUser {
	t.Helper()

	hash, err := passwords.Hash(password)
	if err != nil {
		t.Fatalf("hash admin fixture password: %v", err)
	}
	user := adminUser{
		Email:        email,
		PasswordHash: hash,
		Role:         role,
	}
	if err := repository.CreateUser(t.Context(), user); err != nil {
		t.Fatalf("create admin fixture: %v", err)
	}

	stored, err := repository.FindActiveUserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("find admin fixture: %v", err)
	}

	return stored
}

// userByID finds a repository-owned user while the caller already holds mu.
func (repository *recordingAdminRepository) userByID(
	userID int64,
) (adminUser, bool) {
	for _, user := range repository.users {
		if user.ID == userID {
			return user, true
		}
	}

	return adminUser{}, false
}

// copyAdminSession returns independent digest slices for assertions.
func copyAdminSession(session adminSession) adminSession {
	session.TokenHash = append([]byte(nil), session.TokenHash...)
	session.CSRFTokenHash = append([]byte(nil), session.CSRFTokenHash...)

	return session
}

// newTestAdminPasswordManager supplies a low work factor and deterministic salt
// while retaining the production parser and password-policy behavior.
func newTestAdminPasswordManager(t *testing.T) adminPasswordManager {
	t.Helper()

	manager, err := newAdminPasswordManagerWithParameters(
		2,
		repeatingAdminEntropy(0x5a),
	)
	if err != nil {
		t.Fatalf("create test admin password manager: %v", err)
	}

	return manager
}

// requireErrorIs is a small helper used by construction tests to keep their
// nil-dependency assertions readable.
func requireErrorIs(t *testing.T, actual error, expected error) {
	t.Helper()

	if !errors.Is(actual, expected) {
		t.Fatalf("error: got %v, want %v", actual, expected)
	}
}
