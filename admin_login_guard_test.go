package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// countingAdminLoginRepository observes only account lookup cardinality while
// delegating every repository result to the established in-memory fixture.
type countingAdminLoginRepository struct {
	adminRepository
	findUserCalls int
}

// FindActiveUserByEmail counts without retaining the submitted address.
func (repository *countingAdminLoginRepository) FindActiveUserByEmail(
	ctx context.Context,
	email string,
) (adminUser, error) {
	repository.findUserCalls++

	return repository.adminRepository.FindActiveUserByEmail(ctx, email)
}

// countingAdminPasswordManager records verifier calls while delegating every
// policy and encoding decision to the real inexpensive test manager.
type countingAdminPasswordManager struct {
	delegate    adminPasswordManager
	verifyCalls int
}

// Hash preserves the delegate's canonical test-only password encoding.
func (manager *countingAdminPasswordManager) Hash(password string) (string, error) {
	return manager.delegate.Hash(password)
}

// Verify records only a count; it never retains the password or encoded hash.
func (manager *countingAdminPasswordManager) Verify(
	password string,
	encoded string,
) (bool, error) {
	manager.verifyCalls++

	return manager.delegate.Verify(password, encoded)
}

// TestAdminLoginPasswordWorkGuardRejectsWithoutQueueing verifies saturation is
// nonblocking, account-neutral, and reached before repository or verifier work.
// It also proves a completed normal attempt releases its acquired permit.
func TestAdminLoginPasswordWorkGuardRejectsWithoutQueueing(t *testing.T) {
	storedRepository := newRecordingAdminRepository()
	repository := &countingAdminLoginRepository{
		adminRepository: storedRepository,
	}
	passwords := &countingAdminPasswordManager{
		delegate: newTestAdminPasswordManager(t),
	}
	app := newAdminHTTPTestApplication(t, repository, passwords)
	baselineVerifyCalls := passwords.verifyCalls
	if app.adminLoginPasswordWork == nil {
		t.Fatal("application did not initialize the login password-work guard")
	}
	if capacity := cap(app.adminLoginPasswordWork); capacity != adminLoginPasswordWorkLimit {
		t.Fatalf(
			"password-work capacity: got %d, want %d",
			capacity,
			adminLoginPasswordWorkLimit,
		)
	}

	// Occupy each permit exactly as concurrent in-flight login handlers would.
	for index := 0; index < adminLoginPasswordWorkLimit; index++ {
		if !app.beginAdminLoginPasswordWork() {
			t.Fatalf("could not reserve fixture permit %d", index+1)
		}
	}
	defer func() {
		for len(app.adminLoginPasswordWork) > 0 {
			app.endAdminLoginPasswordWork()
		}
	}()

	csrfToken, _ := adminHTTPToken(0x39)
	request := adminHTTPPostFormRequest(
		"/admin/login",
		url.Values{
			"csrf_token": {csrfToken},
			"email":      {adminHTTPTestEmail},
			"password":   {adminHTTPTestPassword},
		},
		false,
		&http.Cookie{
			Name:  adminLoginCSRFTokenCookieName,
			Value: csrfToken,
		},
	)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf(
			"status: got %d, want %d; body=%q",
			response.StatusCode,
			http.StatusTooManyRequests,
			recorder.Body.String(),
		)
	}
	if retryAfter := response.Header.Get("Retry-After"); retryAfter != "1" {
		t.Errorf("Retry-After: got %q, want 1", retryAfter)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
	if passwords.verifyCalls != baselineVerifyCalls {
		t.Error("saturated login performed password verification")
	}
	if repository.findUserCalls != 0 {
		t.Error("saturated login performed an account lookup")
	}
	if len(storedRepository.createdSessions) != 0 {
		t.Error("saturated login created a session")
	}
	for _, privateValue := range []string{
		adminHTTPTestEmail,
		adminHTTPTestPassword,
	} {
		if strings.Contains(recorder.Body.String(), privateValue) {
			t.Errorf("saturated response exposes %q", privateValue)
		}
	}
	if occupied := len(app.adminLoginPasswordWork); occupied != adminLoginPasswordWorkLimit {
		t.Errorf(
			"rejected login changed occupied permits: got %d, want %d",
			occupied,
			adminLoginPasswordWorkLimit,
		)
	}

	// Drain the fixture permits, then one ordinary unknown-account attempt must
	// perform its neutral verifier path and leave the guard empty afterward.
	for len(app.adminLoginPasswordWork) > 0 {
		app.endAdminLoginPasswordWork()
	}
	retryRequest := adminHTTPPostFormRequest(
		"/admin/login",
		url.Values{
			"csrf_token": {csrfToken},
			"email":      {adminHTTPTestEmail},
			"password":   {adminHTTPTestPassword},
		},
		false,
		&http.Cookie{
			Name:  adminLoginCSRFTokenCookieName,
			Value: csrfToken,
		},
	)
	retryRecorder := httptest.NewRecorder()
	app.routes().ServeHTTP(retryRecorder, retryRequest)
	if retryRecorder.Code != http.StatusUnauthorized {
		t.Errorf(
			"unsaturated status: got %d, want %d",
			retryRecorder.Code,
			http.StatusUnauthorized,
		)
	}
	if passwords.verifyCalls != baselineVerifyCalls+1 {
		t.Error("unsaturated login did not perform one neutral verification")
	}
	if repository.findUserCalls != 1 {
		t.Errorf(
			"unsaturated account lookups: got %d, want 1",
			repository.findUserCalls,
		)
	}
	if occupied := len(app.adminLoginPasswordWork); occupied != 0 {
		t.Errorf("completed login retained %d password-work permits", occupied)
	}
}
