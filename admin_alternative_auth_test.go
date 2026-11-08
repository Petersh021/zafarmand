package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// alternativeAuthEnvironmentLookup keeps configuration tests isolated from the
// process environment, including credentials that may exist on a developer's
// workstation.
func alternativeAuthEnvironmentLookup(
	values map[string]string,
) environmentLookup {
	return func(name string) (string, bool) {
		value, exists := values[name]

		return value, exists
	}
}

func alternativeAuthOTPEnvironment() map[string]string {
	return map[string]string{
		adminOTPKeyEnvironmentName: base64.RawURLEncoding.EncodeToString(
			bytes.Repeat([]byte{0x31}, adminOTPKeyBytes),
		),
		adminSMTPAddressEnvironmentName:  "smtp.example.test:587",
		adminSMTPHostEnvironmentName:     "smtp.example.test",
		adminSMTPUsernameEnvironmentName: "test-user",
		adminSMTPPasswordEnvironmentName: "test-password",
		adminSMTPFromEnvironmentName:     "Zafarmand <admin@example.test>",
	}
}

// TestLoadOptionalAdminOTPConfig protects the all-or-nothing deployment
// contract: absence disables the method, while partial or malformed secrets
// fail startup and a complete set is copied into the private runtime config.
func TestLoadOptionalAdminOTPConfig(t *testing.T) {
	t.Run("all absent", func(t *testing.T) {
		config, enabled, err := loadOptionalAdminOTPConfig(
			alternativeAuthEnvironmentLookup(map[string]string{}),
		)
		if err != nil || enabled || config != (adminOTPConfig{}) {
			t.Fatalf("config=%#v enabled=%t error=%v", config, enabled, err)
		}
	})

	t.Run("partial", func(t *testing.T) {
		values := alternativeAuthOTPEnvironment()
		delete(values, adminSMTPPasswordEnvironmentName)
		_, enabled, err := loadOptionalAdminOTPConfig(
			alternativeAuthEnvironmentLookup(values),
		)
		if enabled || !errors.Is(err, errAdminOTPConfigurationRequired) {
			t.Fatalf("enabled=%t error=%v", enabled, err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		values := alternativeAuthOTPEnvironment()
		config, enabled, err := loadOptionalAdminOTPConfig(
			alternativeAuthEnvironmentLookup(values),
		)
		if err != nil || !enabled {
			t.Fatalf("enabled=%t error=%v", enabled, err)
		}
		if got := base64.RawURLEncoding.EncodeToString(config.key[:]); got != values[adminOTPKeyEnvironmentName] {
			t.Error("loaded OTP key differs from the configured key")
		}
		if config.smtp.Address != values[adminSMTPAddressEnvironmentName] ||
			config.smtp.Host != values[adminSMTPHostEnvironmentName] ||
			config.smtp.Username != values[adminSMTPUsernameEnvironmentName] ||
			config.smtp.Password != values[adminSMTPPasswordEnvironmentName] ||
			config.smtp.From != values[adminSMTPFromEnvironmentName] ||
			config.smtp.Timeout != adminOTPMailTimeout {
			t.Errorf("loaded SMTP configuration differs from its input: %#v", config.smtp)
		}
	})

	t.Run("malformed key", func(t *testing.T) {
		values := alternativeAuthOTPEnvironment()
		values[adminOTPKeyEnvironmentName] = "not-canonical-base64!"
		_, enabled, err := loadOptionalAdminOTPConfig(
			alternativeAuthEnvironmentLookup(values),
		)
		if enabled || !errors.Is(err, errAdminOTPConfigurationInvalid) {
			t.Fatalf("enabled=%t error=%v", enabled, err)
		}
	})

	if _, enabled, err := loadOptionalAdminOTPConfig(nil); enabled ||
		!errors.Is(err, errAdminOTPConfigurationRequired) {
		t.Fatalf("nil lookup: enabled=%t error=%v", enabled, err)
	}
}

func alternativeAuthOIDCEnvironment() map[string]string {
	key := make([]byte, adminOIDCCookieKeyBytes)
	for index := range key {
		key[index] = byte(index + 1)
	}

	return map[string]string{
		adminOIDCClientIDEnvironmentName:     "test-client.apps.googleusercontent.com",
		adminOIDCClientSecretEnvironmentName: "test-only-client-secret",
		adminOIDCRedirectURLenvironmentName: "https://admin.example.test" +
			adminOIDCCallbackPath,
		adminOIDCCookieKeyEnvironmentName: base64.RawURLEncoding.EncodeToString(key),
	}
}

// TestLoadOptionalAdminOIDCConfig verifies that Google sign-in follows the
// same explicit optional-feature contract as email OTP.
func TestLoadOptionalAdminOIDCConfig(t *testing.T) {
	t.Run("all absent", func(t *testing.T) {
		config, enabled, err := loadOptionalAdminOIDCConfig(
			alternativeAuthEnvironmentLookup(map[string]string{}),
		)
		if err != nil || enabled || config != (adminOIDCConfig{}) {
			t.Fatalf("config=%#v enabled=%t error=%v", config, enabled, err)
		}
	})

	t.Run("partial", func(t *testing.T) {
		values := alternativeAuthOIDCEnvironment()
		delete(values, adminOIDCClientSecretEnvironmentName)
		_, enabled, err := loadOptionalAdminOIDCConfig(
			alternativeAuthEnvironmentLookup(values),
		)
		if enabled || !errors.Is(err, errAdminOIDCConfigurationRequired) {
			t.Fatalf("enabled=%t error=%v", enabled, err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		values := alternativeAuthOIDCEnvironment()
		config, enabled, err := loadOptionalAdminOIDCConfig(
			alternativeAuthEnvironmentLookup(values),
		)
		if err != nil || !enabled {
			t.Fatalf("enabled=%t error=%v", enabled, err)
		}
		if config.clientID != values[adminOIDCClientIDEnvironmentName] ||
			config.clientSecret != values[adminOIDCClientSecretEnvironmentName] ||
			config.redirectURL != values[adminOIDCRedirectURLenvironmentName] ||
			!config.valid() {
			t.Errorf("loaded OIDC configuration differs from its input")
		}
	})

	if _, enabled, err := loadOptionalAdminOIDCConfig(nil); enabled ||
		!errors.Is(err, errAdminOIDCConfigurationRequired) {
		t.Fatalf("nil lookup: enabled=%t error=%v", enabled, err)
	}
}

type alternativeAuthAccountReaderStub struct {
	account adminAccount
	err     error
	emails  []string
}

func (reader *alternativeAuthAccountReaderStub) FindActiveAccountByEmail(
	_ context.Context,
	email string,
) (adminAccount, error) {
	reader.emails = append(reader.emails, email)

	return reader.account, reader.err
}

type alternativeAuthOTPRepositoryStub struct {
	created          []adminEmailOTPChallenge
	createErr        error
	cancelled        [][]byte
	cancelErr        error
	verifiedHashes   [][]byte
	verifiedCodeMACs [][]byte
	verifyAccount    adminAccount
	verifyErr        error
}

func (repository *alternativeAuthOTPRepositoryStub) CreateChallenge(
	_ context.Context,
	challenge adminEmailOTPChallenge,
) error {
	challenge.ChallengeHash = append([]byte(nil), challenge.ChallengeHash...)
	challenge.CodeMAC = append([]byte(nil), challenge.CodeMAC...)
	repository.created = append(repository.created, challenge)

	return repository.createErr
}

func (repository *alternativeAuthOTPRepositoryStub) CancelChallenge(
	_ context.Context,
	challengeHash []byte,
) error {
	repository.cancelled = append(
		repository.cancelled,
		append([]byte(nil), challengeHash...),
	)

	return repository.cancelErr
}

func (repository *alternativeAuthOTPRepositoryStub) VerifyAndConsume(
	_ context.Context,
	challengeHash []byte,
	codeMAC []byte,
) (adminAccount, error) {
	repository.verifiedHashes = append(
		repository.verifiedHashes,
		append([]byte(nil), challengeHash...),
	)
	repository.verifiedCodeMACs = append(
		repository.verifiedCodeMACs,
		append([]byte(nil), codeMAC...),
	)

	return repository.verifyAccount, repository.verifyErr
}

type alternativeAuthOTPDispatcherStub struct {
	deliveries []adminOTPDelivery
	err        error
}

func (dispatcher *alternativeAuthOTPDispatcherStub) Dispatch(
	_ context.Context,
	delivery adminOTPDelivery,
) error {
	dispatcher.deliveries = append(dispatcher.deliveries, delivery)

	return dispatcher.err
}

func alternativeAuthOTPEntropy(tokenByte byte, code uint16) *bytes.Reader {
	data := bytes.Repeat([]byte{tokenByte}, adminTokenBytes)
	data = append(data, byte(code>>8), byte(code))
	// AES-GCM wraps the selector after challenge creation. Supplying its nonce
	// here keeps every service test deterministic without weakening production.
	data = append(data, bytes.Repeat([]byte{tokenByte ^ 0xff}, 12)...)

	return bytes.NewReader(data)
}

func alternativeAuthOTPService(
	t *testing.T,
	accounts adminAccountReader,
	repository adminEmailOTPRepository,
	dispatcher adminOTPDispatcher,
	entropy *bytes.Reader,
	now time.Time,
) *adminOTPService {
	t.Helper()
	service, err := newAdminOTPService(
		accounts,
		repository,
		dispatcher,
		bytes.Repeat([]byte{0x73}, adminOTPKeyBytes),
		entropy,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("create OTP service: %v", err)
	}

	return service
}

// TestAdminOTPServiceIssueKnownAccount checks that only a normalized active
// account receives a persisted challenge and a real delivery item.
func TestAdminOTPServiceIssueKnownAccount(t *testing.T) {
	now := time.Date(2035, 1, 2, 3, 4, 5, 0, time.FixedZone("fixture", 90*60))
	account := adminAccount{ID: 23, Email: adminHTTPTestEmail, Role: adminRoleOwner}
	accounts := &alternativeAuthAccountReaderStub{account: account}
	repository := &alternativeAuthOTPRepositoryStub{}
	dispatcher := &alternativeAuthOTPDispatcherStub{}
	service := alternativeAuthOTPService(
		t,
		accounts,
		repository,
		dispatcher,
		alternativeAuthOTPEntropy(0x45, 4719),
		now,
	)

	result, err := service.Issue(t.Context(), " OWNER@EXAMPLE.TEST ", "")
	if err != nil {
		t.Fatalf("issue OTP: %v", err)
	}
	if !result.Issued || result.ChallengeToken == "" ||
		!result.ExpiresAt.Equal(now.UTC().Add(adminOTPChallengeLifetime)) {
		t.Errorf("issue result: %#v", result)
	}
	if len(accounts.emails) != 1 || accounts.emails[0] != adminHTTPTestEmail {
		t.Errorf("account lookup emails: %#v", accounts.emails)
	}
	if len(repository.created) != 1 {
		t.Fatalf("created challenges: got %d, want 1", len(repository.created))
	}
	rawToken := bytes.Repeat([]byte{0x45}, adminTokenBytes)
	expectedHash := sha256.Sum256(rawToken)
	expectedMAC, err := adminOTPCodeMAC(service.key, expectedHash[:], "4719")
	if err != nil {
		t.Fatalf("derive expected OTP MAC: %v", err)
	}
	challenge := repository.created[0]
	if challenge.UserID != account.ID ||
		!bytes.Equal(challenge.ChallengeHash, expectedHash[:]) ||
		!bytes.Equal(challenge.CodeMAC, expectedMAC) ||
		!challenge.ExpiresAt.Equal(result.ExpiresAt) {
		t.Errorf("created challenge: %#v", challenge)
	}
	if len(dispatcher.deliveries) != 1 ||
		!dispatcher.deliveries[0].Deliver ||
		dispatcher.deliveries[0].Recipient != account.Email ||
		dispatcher.deliveries[0].Code != "4719" {
		t.Errorf("delivery: %#v", dispatcher.deliveries)
	}
}

// TestAdminOTPServiceIssueUnknownAccount verifies the account-neutral path:
// it returns an indistinguishable selector and expiry while persisting nothing
// and placing only dummy work on the bounded dispatcher.
func TestAdminOTPServiceIssueUnknownAccount(t *testing.T) {
	now := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	accounts := &alternativeAuthAccountReaderStub{err: errAdminUserNotFound}
	repository := &alternativeAuthOTPRepositoryStub{}
	dispatcher := &alternativeAuthOTPDispatcherStub{}
	service := alternativeAuthOTPService(
		t,
		accounts,
		repository,
		dispatcher,
		alternativeAuthOTPEntropy(0x46, 4720),
		now,
	)

	result, err := service.Issue(t.Context(), "unknown@example.test", "")
	if err != nil {
		t.Fatalf("issue account-neutral OTP: %v", err)
	}
	if result.Issued || result.ChallengeToken == "" ||
		!result.ExpiresAt.Equal(now.Add(adminOTPChallengeLifetime)) {
		t.Errorf("unknown-account result: %#v", result)
	}
	if len(repository.created) != 0 {
		t.Errorf("unknown account persisted %d challenges", len(repository.created))
	}
	if len(dispatcher.deliveries) != 1 || dispatcher.deliveries[0].Deliver ||
		dispatcher.deliveries[0].Recipient != "" ||
		dispatcher.deliveries[0].Code != "4720" {
		t.Errorf("dummy delivery: %#v", dispatcher.deliveries)
	}
}

// TestAdminOTPServiceNeutralIssuePreservesBrowserChallenge proves an early
// resend rotates the opaque envelope without discarding the valid selector the
// same browser already holds. The public cookie shape remains fresh either way.
func TestAdminOTPServiceNeutralIssuePreservesBrowserChallenge(t *testing.T) {
	now := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	account := adminAccount{ID: 24, Email: adminHTTPTestEmail, Role: adminRoleOwner}
	repository := &alternativeAuthOTPRepositoryStub{
		createErr: errAdminEmailOTPRateLimited,
	}
	service := alternativeAuthOTPService(
		t,
		&alternativeAuthAccountReaderStub{account: account},
		repository,
		&alternativeAuthOTPDispatcherStub{},
		alternativeAuthOTPEntropy(0x47, 4721),
		now,
	)
	existingRawBytes := bytes.Repeat([]byte{0x86}, adminTokenBytes)
	existingRaw := base64.RawURLEncoding.EncodeToString(existingRawBytes)
	existingWrapped, err := sealAdminOTPChallengeToken(
		service.key,
		existingRaw,
		bytes.NewReader(bytes.Repeat([]byte{0x87}, 12)),
	)
	if err != nil {
		t.Fatalf("seal existing selector: %v", err)
	}

	result, err := service.Issue(t.Context(), account.Email, existingWrapped)
	if err != nil {
		t.Fatalf("issue neutral replacement: %v", err)
	}
	if result.Issued || result.ChallengeToken == existingWrapped {
		t.Errorf("neutral result did not rotate only its envelope: %#v", result)
	}
	retainedRaw, valid := openAdminOTPChallengeToken(
		service.key,
		result.ChallengeToken,
	)
	if !valid || retainedRaw != existingRaw {
		t.Error("neutral request discarded the browser's existing selector")
	}
}

// TestAdminOTPServiceVerify covers the selector/code derivation handoff and
// proves malformed values fail before reaching persistence.
func TestAdminOTPServiceVerify(t *testing.T) {
	account := adminAccount{ID: 31, Email: adminHTTPTestEmail, Role: adminRoleEditor}
	repository := &alternativeAuthOTPRepositoryStub{verifyAccount: account}
	service := alternativeAuthOTPService(
		t,
		&alternativeAuthAccountReaderStub{},
		repository,
		&alternativeAuthOTPDispatcherStub{},
		alternativeAuthOTPEntropy(0x47, 4721),
		time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC),
	)
	tokenBytes := bytes.Repeat([]byte{0x81}, adminTokenBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	wrappedToken, err := sealAdminOTPChallengeToken(
		service.key,
		token,
		bytes.NewReader(bytes.Repeat([]byte{0x82}, 12)),
	)
	if err != nil {
		t.Fatalf("seal verification selector: %v", err)
	}

	got, err := service.Verify(t.Context(), wrappedToken, "0042")
	if err != nil || got != account {
		t.Fatalf("verify account=%#v error=%v", got, err)
	}
	if len(repository.verifiedHashes) != 1 ||
		len(repository.verifiedCodeMACs) != 1 {
		t.Fatalf("repository verification calls: %#v", repository)
	}
	expectedHash := sha256.Sum256(tokenBytes)
	expectedMAC, err := adminOTPCodeMAC(service.key, expectedHash[:], "0042")
	if err != nil {
		t.Fatalf("derive expected verification MAC: %v", err)
	}
	if !bytes.Equal(repository.verifiedHashes[0], expectedHash[:]) ||
		!bytes.Equal(repository.verifiedCodeMACs[0], expectedMAC) {
		t.Error("verification repository received an unexpected hash or MAC")
	}

	for _, input := range []struct {
		name  string
		token string
		code  string
	}{
		{name: "malformed selector", token: "not-base64!", code: "0042"},
		{name: "short code", token: wrappedToken, code: "042"},
		{name: "non-decimal code", token: wrappedToken, code: "00a2"},
	} {
		t.Run(input.name, func(t *testing.T) {
			before := len(repository.verifiedHashes)
			_, err := service.Verify(t.Context(), input.token, input.code)
			if !errors.Is(err, errAdminEmailOTPAuthenticationFailed) {
				t.Fatalf("error: got %v", err)
			}
			if len(repository.verifiedHashes) != before {
				t.Error("malformed verification reached persistence")
			}
		})
	}
}

// TestAdminOTPServiceCancelsUndeliverableChallenge ensures queue saturation
// cannot leave a valid code blocking the administrator's immediate retry.
func TestAdminOTPServiceCancelsUndeliverableChallenge(t *testing.T) {
	account := adminAccount{ID: 44, Email: adminHTTPTestEmail, Role: adminRoleOwner}
	repository := &alternativeAuthOTPRepositoryStub{}
	dispatcher := &alternativeAuthOTPDispatcherStub{
		err: errAdminOTPDeliveryUnavailable,
	}
	service := alternativeAuthOTPService(
		t,
		&alternativeAuthAccountReaderStub{account: account},
		repository,
		dispatcher,
		alternativeAuthOTPEntropy(0x48, 4722),
		time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC),
	)

	_, err := service.Issue(t.Context(), account.Email, "")
	if !errors.Is(err, errAdminOTPRequestUnavailable) {
		t.Fatalf("error: got %v", err)
	}
	if len(repository.created) != 1 || len(repository.cancelled) != 1 ||
		!bytes.Equal(
			repository.cancelled[0],
			repository.created[0].ChallengeHash,
		) {
		t.Errorf("create/cancel operations: %#v", repository)
	}
}
