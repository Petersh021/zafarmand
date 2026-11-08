package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// adminOIDCSequenceEntropy provides deterministic but nonrepeating bytes to
// protocol tests. It is never used by production construction.
type adminOIDCSequenceEntropy struct {
	next byte
}

// Read advances for every byte so independently generated state, nonce, PKCE,
// and GCM nonce fixtures cannot accidentally compare equal.
func (entropy *adminOIDCSequenceEntropy) Read(target []byte) (int, error) {
	for index := range target {
		entropy.next++
		target[index] = entropy.next
	}

	return len(target), nil
}

// adminOIDCTestConfig returns one syntactically valid confidential-web-client
// configuration with an obvious test-only encryption key.
func adminOIDCTestConfig() adminOIDCConfig {
	config := adminOIDCConfig{
		clientID:     "test-client.apps.googleusercontent.com",
		clientSecret: "test-only-client-secret",
		redirectURL: "https://admin.example.test" +
			adminOIDCCallbackPath,
	}
	for index := range config.cookieKey {
		config.cookieKey[index] = byte(index + 1)
	}

	return config
}

// adminOIDCTestEnvironment converts a valid config into the exact environment
// spellings accepted by loadAdminOIDCConfig.
func adminOIDCTestEnvironment(config adminOIDCConfig) map[string]string {
	return map[string]string{
		adminOIDCClientIDEnvironmentName:     config.clientID,
		adminOIDCClientSecretEnvironmentName: config.clientSecret,
		adminOIDCRedirectURLenvironmentName:  config.redirectURL,
		adminOIDCCookieKeyEnvironmentName: base64.RawURLEncoding.EncodeToString(
			config.cookieKey[:],
		),
	}
}

// adminOIDCTestLookup adapts an isolated map to environmentLookup without
// mutating process-wide environment variables.
func adminOIDCTestLookup(values map[string]string) environmentLookup {
	return func(name string) (string, bool) {
		value, exists := values[name]

		return value, exists
	}
}

// TestLoadAdminOIDCConfig validates complete loading, exact secret handling,
// canonical key decoding, HTTPS production redirects, and loopback-only HTTP.
func TestLoadAdminOIDCConfig(t *testing.T) {
	validConfig := adminOIDCTestConfig()
	t.Run("valid HTTPS", func(t *testing.T) {
		loaded, err := loadAdminOIDCConfig(
			adminOIDCTestLookup(adminOIDCTestEnvironment(validConfig)),
		)
		if err != nil {
			t.Fatalf("load valid config: %v", err)
		}
		if loaded.clientID != validConfig.clientID ||
			loaded.clientSecret != validConfig.clientSecret ||
			loaded.redirectURL != validConfig.redirectURL ||
			loaded.cookieKey != validConfig.cookieKey {
			t.Error("loaded OIDC config does not match exact input")
		}
	})

	t.Run("valid loopback HTTP", func(t *testing.T) {
		environment := adminOIDCTestEnvironment(validConfig)
		environment[adminOIDCRedirectURLenvironmentName] =
			"http://127.0.0.1:8080" + adminOIDCCallbackPath
		if _, err := loadAdminOIDCConfig(
			adminOIDCTestLookup(environment),
		); err != nil {
			t.Fatalf("load loopback config: %v", err)
		}
	})

	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   error
	}{
		{
			name: "missing client ID",
			mutate: func(values map[string]string) {
				delete(values, adminOIDCClientIDEnvironmentName)
			},
			want: errAdminOIDCConfigurationRequired,
		},
		{
			name: "padded client secret",
			mutate: func(values map[string]string) {
				values[adminOIDCClientSecretEnvironmentName] += " "
			},
			want: errAdminOIDCConfigurationInvalid,
		},
		{
			name: "public HTTP redirect",
			mutate: func(values map[string]string) {
				values[adminOIDCRedirectURLenvironmentName] =
					"http://admin.example.test" + adminOIDCCallbackPath
			},
			want: errAdminOIDCConfigurationInvalid,
		},
		{
			name: "redirect query",
			mutate: func(values map[string]string) {
				values[adminOIDCRedirectURLenvironmentName] += "?next=/admin"
			},
			want: errAdminOIDCConfigurationInvalid,
		},
		{
			name: "wrong callback path",
			mutate: func(values map[string]string) {
				values[adminOIDCRedirectURLenvironmentName] =
					"https://admin.example.test/admin/callback"
			},
			want: errAdminOIDCConfigurationInvalid,
		},
		{
			name: "short cookie key",
			mutate: func(values map[string]string) {
				values[adminOIDCCookieKeyEnvironmentName] =
					base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))
			},
			want: errAdminOIDCConfigurationInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := adminOIDCTestEnvironment(validConfig)
			test.mutate(environment)
			_, err := loadAdminOIDCConfig(adminOIDCTestLookup(environment))
			if !errors.Is(err, test.want) {
				t.Fatalf("error: got %v, want %v", err, test.want)
			}
			for _, privateValue := range environment {
				if err != nil && strings.Contains(err.Error(), privateValue) {
					t.Error("configuration error exposes an environment value")
				}
			}
		})
	}

	if _, err := loadAdminOIDCConfig(nil); !errors.Is(
		err,
		errAdminOIDCConfigurationRequired,
	) {
		t.Fatalf("nil lookup: got %v", err)
	}
}

// adminOIDCTestAuthorization constructs one deterministic authorization and
// returns its opened transaction for callback tests.
func adminOIDCTestAuthorization(
	t *testing.T,
	now time.Time,
) (
	adminOIDCConfig,
	*adminOIDCTransactionSealer,
	adminOIDCAuthorization,
	adminOIDCTransaction,
) {
	t.Helper()
	config := adminOIDCTestConfig()
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&adminOIDCSequenceEntropy{next: 0xa0},
	)
	if err != nil {
		t.Fatalf("create transaction sealer: %v", err)
	}
	authorization, err := newAdminOIDCAuthorization(
		config,
		sealer,
		&adminOIDCSequenceEntropy{},
		now,
	)
	if err != nil {
		t.Fatalf("create authorization: %v", err)
	}
	transaction, err := sealer.open(authorization.CookieValue, now)
	if err != nil {
		t.Fatalf("open authorization transaction: %v", err)
	}

	return config, sealer, authorization, transaction
}

// TestNewAdminOIDCAuthorization verifies state, nonce, S256 PKCE, minimal OIDC
// scopes, provider pinning, and the absence of client secrets from the URL.
func TestNewAdminOIDCAuthorization(t *testing.T) {
	now := time.Date(2034, 5, 6, 7, 8, 9, 0, time.UTC)
	config, _, authorization, transaction := adminOIDCTestAuthorization(t, now)

	parsed, err := url.Parse(authorization.URL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path !=
		adminOIDCGoogleAuthorizationEndpoint {
		t.Errorf("authorization endpoint: %q", parsed.String())
	}
	parameters := parsed.Query()
	expected := map[string]string{
		"client_id":             config.clientID,
		"code_challenge_method": "S256",
		"nonce":                 transaction.Nonce,
		"prompt":                "select_account",
		"redirect_uri":          config.redirectURL,
		"response_type":         "code",
		"scope":                 "openid email",
		"state":                 transaction.State,
	}
	for name, value := range expected {
		if got := parameters.Get(name); got != value || len(parameters[name]) != 1 {
			t.Errorf("parameter %s: got %q, want %q", name, got, value)
		}
	}
	challenge := sha256Text(transaction.CodeVerifier)
	if got := parameters.Get("code_challenge"); got != challenge {
		t.Errorf("PKCE challenge: got %q, want %q", got, challenge)
	}
	if strings.Contains(authorization.URL, config.clientSecret) ||
		parameters.Get("access_type") != "" {
		t.Error("authorization URL exposes secret or requests offline access")
	}
	if transaction.State == transaction.Nonce ||
		transaction.State == transaction.CodeVerifier ||
		transaction.Nonce == transaction.CodeVerifier {
		t.Error("authorization random values are not independent")
	}
	if !authorization.ExpiresAt.Equal(now.Add(adminOIDCTransactionLifetime)) {
		t.Errorf("authorization expiry: %v", authorization.ExpiresAt)
	}
}

// sha256Text returns the canonical S256 PKCE challenge used in assertions.
func sha256Text(value string) string {
	digest := sha256.Sum256([]byte(value))

	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// TestAdminOIDCTransactionSealerRejectsInvalidValues proves tampering, expiry,
// key mismatch, future issuance, and noncanonical input all fail closed.
func TestAdminOIDCTransactionSealerRejectsInvalidValues(t *testing.T) {
	now := time.Date(2034, 5, 6, 7, 8, 9, 0, time.UTC)
	config, sealer, authorization, transaction := adminOIDCTestAuthorization(t, now)

	tampered := []byte(authorization.CookieValue)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	tests := []struct {
		name  string
		value string
		now   time.Time
	}{
		{name: "tampered", value: string(tampered), now: now},
		{name: "expired", value: authorization.CookieValue, now: authorization.ExpiresAt},
		{name: "noncanonical", value: authorization.CookieValue + "=", now: now},
		{name: "empty", now: now},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := sealer.open(test.value, test.now); !errors.Is(
				err,
				errAdminOIDCTransactionInvalid,
			) {
				t.Fatalf("error: got %v", err)
			}
		})
	}

	wrongConfig := config
	wrongConfig.cookieKey[0] ^= 0xff
	wrongSealer, err := newAdminOIDCTransactionSealer(
		wrongConfig.cookieKey,
		&adminOIDCSequenceEntropy{},
	)
	if err != nil {
		t.Fatalf("create wrong-key sealer: %v", err)
	}
	if _, err := wrongSealer.open(authorization.CookieValue, now); !errors.Is(
		err,
		errAdminOIDCTransactionInvalid,
	) {
		t.Fatalf("wrong key: got %v", err)
	}

	transaction.IssuedAt = now.Add(2 * adminOIDCClockSkew).Unix()
	transaction.ExpiresAt = now.Add(adminOIDCTransactionLifetime).Unix()
	future, err := sealer.seal(transaction)
	if err != nil {
		t.Fatalf("seal future transaction: %v", err)
	}
	if _, err := sealer.open(future, now); !errors.Is(
		err,
		errAdminOIDCTransactionInvalid,
	) {
		t.Fatalf("future transaction: got %v", err)
	}
}

// TestAdminOIDCTransactionCookieAttributes verifies the callback-compatible
// SameSite mode, narrow path, trusted Secure decision, and matching deletion.
func TestAdminOIDCTransactionCookieAttributes(t *testing.T) {
	now := time.Date(2034, 5, 6, 7, 8, 9, 0, time.UTC)
	_, _, authorization, _ := adminOIDCTestAuthorization(t, now)

	for _, scenario := range []struct {
		name   string
		secure bool
	}{
		{name: "local HTTP"},
		{name: "direct TLS", secure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			target := "http://example.test" + adminOIDCCallbackPath
			if scenario.secure {
				target = "https://example.test" + adminOIDCCallbackPath
			}
			request := httptest.NewRequest(http.MethodGet, target, nil)
			recorder := httptest.NewRecorder()
			setAdminOIDCTransactionCookie(recorder, request, authorization)
			cookies := recorder.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("set cookies: got %d", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != adminOIDCTransactionCookieName ||
				cookie.Value != authorization.CookieValue ||
				cookie.Path != adminOIDCCallbackPath || !cookie.HttpOnly ||
				cookie.SameSite != http.SameSiteLaxMode ||
				cookie.Secure != scenario.secure {
				t.Errorf("set cookie attributes: %#v", cookie)
			}

			clearRecorder := httptest.NewRecorder()
			clearAdminOIDCTransactionCookie(clearRecorder, request)
			cleared := clearRecorder.Result().Cookies()
			if len(cleared) != 1 || cleared[0].MaxAge != -1 ||
				cleared[0].Path != adminOIDCCallbackPath ||
				cleared[0].SameSite != http.SameSiteLaxMode ||
				cleared[0].Secure != scenario.secure {
				t.Errorf("clear cookie attributes: %#v", cleared)
			}
		})
	}
}

// adminOIDCExchangeRecorder provides a network-free OAuth2 exchange seam.
type adminOIDCExchangeRecorder struct {
	request adminOIDCCodeExchangeRequest
	calls   int
	result  adminOIDCTokenSet
	err     error
}

// Exchange records no context values or secrets beyond the explicit test
// request and returns a controlled token result.
func (exchange *adminOIDCExchangeRecorder) Exchange(
	_ context.Context,
	request adminOIDCCodeExchangeRequest,
) (adminOIDCTokenSet, error) {
	exchange.calls++
	exchange.request = request

	return exchange.result, exchange.err
}

// adminOIDCVerifierRecorder provides deterministic signed-token claims without
// parsing or fetching provider metadata in tests.
type adminOIDCVerifierRecorder struct {
	rawToken string
	calls    int
	claims   adminOIDCIDTokenClaims
	err      error
}

// Verify records the opaque fixture and supplies controlled verified claims.
func (verifier *adminOIDCVerifierRecorder) Verify(
	_ context.Context,
	rawToken string,
) (adminOIDCIDTokenClaims, error) {
	verifier.calls++
	verifier.rawToken = rawToken

	return verifier.claims, verifier.err
}

// adminOIDCResolverRecorder proves orchestration reaches the existing-active-
// administrator authorization boundary with only a verified identity.
type adminOIDCResolverRecorder struct {
	identity adminOIDCVerifiedIdentity
	calls    int
	admin    adminOIDCAdmin
	err      error
}

// adminOIDCAccountReaderStub supplies one password-free existing account.
type adminOIDCAccountReaderStub struct {
	calls   int
	email   string
	account adminAccount
	err     error
}

// FindActiveAccountByEmail records the normalized verified address only.
func (reader *adminOIDCAccountReaderStub) FindActiveAccountByEmail(
	_ context.Context,
	email string,
) (adminAccount, error) {
	reader.calls++
	reader.email = email

	return reader.account, reader.err
}

// adminOIDCIdentityRepositoryStub models established links, first-link races,
// and last-authentication recording without opening PostgreSQL.
type adminOIDCIdentityRepositoryStub struct {
	findCalls    int
	findResults  []adminAccount
	findErrors   []error
	linked       []adminOIDCIdentity
	linkError    error
	recordCalls  int
	recordIssuer string
	recordSub    string
	recordError  error
}

// LinkIdentity records the exact immutable provider key and existing user ID.
func (repository *adminOIDCIdentityRepositoryStub) LinkIdentity(
	_ context.Context,
	identity adminOIDCIdentity,
) error {
	repository.linked = append(repository.linked, identity)

	return repository.linkError
}

// FindActiveAccountByIdentity returns results in call order for race tests.
func (repository *adminOIDCIdentityRepositoryStub) FindActiveAccountByIdentity(
	_ context.Context,
	_ string,
	_ string,
) (adminAccount, error) {
	index := repository.findCalls
	repository.findCalls++
	if index < len(repository.findErrors) && repository.findErrors[index] != nil {
		return adminAccount{}, repository.findErrors[index]
	}
	if index < len(repository.findResults) {
		return repository.findResults[index], nil
	}

	return adminAccount{}, errAdminOIDCIdentityNotFound
}

// RecordAuthentication observes only the immutable key and returns a safe test
// outcome without storing a token or mutable provider claim.
func (repository *adminOIDCIdentityRepositoryStub) RecordAuthentication(
	_ context.Context,
	issuer string,
	subject string,
) error {
	repository.recordCalls++
	repository.recordIssuer = issuer
	repository.recordSub = subject

	return repository.recordError
}

// TestAdminOIDCExistingAdminResolverRestrictsAccounts verifies established and
// first-use identities can resolve only pre-existing active local accounts.
func TestAdminOIDCExistingAdminResolverRestrictsAccounts(t *testing.T) {
	verified := adminOIDCVerifiedIdentity{
		Issuer:  adminOIDCGoogleIssuer,
		Subject: "google-subject-123",
		Email:   adminHTTPTestEmail,
	}
	account := adminAccount{
		ID:    41,
		Email: adminHTTPTestEmail,
		Role:  adminRoleOwner,
	}

	t.Run("established subject", func(t *testing.T) {
		accounts := &adminOIDCAccountReaderStub{}
		identities := &adminOIDCIdentityRepositoryStub{
			findResults: []adminAccount{{
				ID:    account.ID,
				Email: "renamed-owner@example.test",
				Role:  account.Role,
			}},
		}
		resolver, err := newAdminOIDCExistingAdminResolver(accounts, identities)
		if err != nil {
			t.Fatal("create established-link resolver")
		}
		admin, err := resolver.ResolveExistingActiveAdmin(t.Context(), verified)
		if err != nil {
			t.Fatalf("resolve established identity: %v", err)
		}
		if admin.UserID != account.ID || admin.Email != "renamed-owner@example.test" ||
			admin.Role != account.Role {
			t.Errorf("resolved established admin: %#v", admin)
		}
		if accounts.calls != 0 || len(identities.linked) != 0 ||
			identities.recordCalls != 1 {
			t.Errorf("established-link operations: accounts=%d links=%d records=%d", accounts.calls, len(identities.linked), identities.recordCalls)
		}
	})

	t.Run("first verified link", func(t *testing.T) {
		accounts := &adminOIDCAccountReaderStub{account: account}
		identities := &adminOIDCIdentityRepositoryStub{
			findErrors: []error{errAdminOIDCIdentityNotFound},
		}
		resolver, err := newAdminOIDCExistingAdminResolver(accounts, identities)
		if err != nil {
			t.Fatal("create first-link resolver")
		}
		admin, err := resolver.ResolveExistingActiveAdmin(t.Context(), verified)
		if err != nil || admin.UserID != account.ID {
			t.Fatalf("first-link admin=%#v error=%v", admin, err)
		}
		if accounts.calls != 1 || accounts.email != verified.Email ||
			len(identities.linked) != 1 ||
			identities.linked[0] != (adminOIDCIdentity{
				Issuer: verified.Issuer, Subject: verified.Subject, UserID: account.ID,
			}) || identities.recordCalls != 1 {
			t.Errorf("first-link operations: accounts=%#v identities=%#v", accounts, identities)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		accounts := &adminOIDCAccountReaderStub{err: errAdminUserNotFound}
		identities := &adminOIDCIdentityRepositoryStub{
			findErrors: []error{errAdminOIDCIdentityNotFound},
		}
		resolver, err := newAdminOIDCExistingAdminResolver(accounts, identities)
		if err != nil {
			t.Fatal("create unknown-account resolver")
		}
		_, err = resolver.ResolveExistingActiveAdmin(t.Context(), verified)
		if !errors.Is(err, errAdminOIDCAdminNotAuthorized) ||
			len(identities.linked) != 0 || identities.recordCalls != 0 {
			t.Fatalf("unknown account: error=%v identities=%#v", err, identities)
		}
	})

	t.Run("concurrent exact link", func(t *testing.T) {
		accounts := &adminOIDCAccountReaderStub{account: account}
		identities := &adminOIDCIdentityRepositoryStub{
			findResults: []adminAccount{{}, account},
			findErrors:  []error{errAdminOIDCIdentityNotFound, nil},
			linkError:   errAdminOIDCIdentityConflict,
		}
		resolver, err := newAdminOIDCExistingAdminResolver(accounts, identities)
		if err != nil {
			t.Fatal("create race resolver")
		}
		admin, err := resolver.ResolveExistingActiveAdmin(t.Context(), verified)
		if err != nil || admin.UserID != account.ID || identities.findCalls != 2 ||
			identities.recordCalls != 1 {
			t.Fatalf("concurrent link: admin=%#v error=%v identities=%#v", admin, err, identities)
		}
	})
}

// ResolveExistingActiveAdmin returns only its prearranged local account.
func (resolver *adminOIDCResolverRecorder) ResolveExistingActiveAdmin(
	_ context.Context,
	identity adminOIDCVerifiedIdentity,
) (adminOIDCAdmin, error) {
	resolver.calls++
	resolver.identity = identity

	return resolver.admin, resolver.err
}

// adminOIDCTestCallbackDependencies builds valid claims and recording
// dependencies for one opened authorization transaction.
func adminOIDCTestCallbackDependencies(
	config adminOIDCConfig,
	transaction adminOIDCTransaction,
	now time.Time,
) (
	*adminOIDCExchangeRecorder,
	*adminOIDCVerifierRecorder,
	*adminOIDCResolverRecorder,
) {
	exchange := &adminOIDCExchangeRecorder{
		result: adminOIDCTokenSet{RawIDToken: "signed-test-id-token"},
	}
	verifier := &adminOIDCVerifierRecorder{
		claims: adminOIDCIDTokenClaims{
			Issuer:        adminOIDCGoogleIssuer,
			Subject:       "google-subject-123",
			Audience:      []string{config.clientID},
			Nonce:         transaction.Nonce,
			Email:         adminHTTPTestEmail,
			EmailVerified: true,
			IssuedAt:      now.Add(-time.Minute),
			ExpiresAt:     now.Add(time.Hour),
		},
	}
	resolver := &adminOIDCResolverRecorder{
		admin: adminOIDCAdmin{
			UserID: 41,
			Email:  adminHTTPTestEmail,
			Role:   adminRoleOwner,
		},
	}

	return exchange, verifier, resolver
}

// TestAdminOIDCCallbackServiceAuthenticate verifies the complete network-free
// callback orchestration and its exact PKCE and existing-admin handoffs.
func TestAdminOIDCCallbackServiceAuthenticate(t *testing.T) {
	now := time.Date(2034, 5, 6, 7, 8, 9, 0, time.UTC)
	config, sealer, authorization, transaction := adminOIDCTestAuthorization(t, now)
	exchange, verifier, resolver := adminOIDCTestCallbackDependencies(
		config,
		transaction,
		now,
	)
	service, err := newAdminOIDCCallbackService(
		config,
		sealer,
		exchange,
		verifier,
		resolver,
	)
	if err != nil {
		t.Fatalf("create callback service: %v", err)
	}

	admin, err := service.Authenticate(
		t.Context(),
		adminOIDCCallbackInput{
			Code:              "provider-authorization-code",
			State:             transaction.State,
			TransactionCookie: authorization.CookieValue,
		},
		now,
	)
	if err != nil {
		t.Fatalf("authenticate callback: %v", err)
	}
	if admin != resolver.admin {
		t.Errorf("resolved admin: got %#v, want %#v", admin, resolver.admin)
	}
	if exchange.calls != 1 ||
		exchange.request.Code != "provider-authorization-code" ||
		exchange.request.CodeVerifier != transaction.CodeVerifier ||
		exchange.request.RedirectURL != config.redirectURL {
		t.Errorf("exchange request: %#v", exchange)
	}
	if verifier.calls != 1 || verifier.rawToken != "signed-test-id-token" {
		t.Errorf("verifier calls: %#v", verifier)
	}
	expectedIdentity := adminOIDCVerifiedIdentity{
		Issuer:  adminOIDCGoogleIssuer,
		Subject: "google-subject-123",
		Email:   adminHTTPTestEmail,
	}
	if resolver.calls != 1 || resolver.identity != expectedIdentity {
		t.Errorf("resolver handoff: %#v", resolver)
	}
}

// TestAdminOIDCCallbackServiceRejectsInvalidAuthentication proves every trust
// decision fails before local authorization or session creation can occur.
func TestAdminOIDCCallbackServiceRejectsInvalidAuthentication(t *testing.T) {
	now := time.Date(2034, 5, 6, 7, 8, 9, 0, time.UTC)
	config, sealer, authorization, transaction := adminOIDCTestAuthorization(t, now)
	otherState, err := generateAdminOIDCRandomValue(
		&adminOIDCSequenceEntropy{next: 0xd0},
	)
	if err != nil {
		t.Fatal("create mismatched state")
	}

	tests := []struct {
		name   string
		input  adminOIDCCallbackInput
		mutate func(
			*adminOIDCExchangeRecorder,
			*adminOIDCVerifierRecorder,
			*adminOIDCResolverRecorder,
		)
		want             error
		wantExchange     int
		wantVerification int
		wantResolution   int
	}{
		{
			name: "state mismatch",
			input: adminOIDCCallbackInput{
				Code:              "authorization-code",
				State:             otherState,
				TransactionCookie: authorization.CookieValue,
			},
			want: errAdminOIDCCallbackInvalid,
		},
		{
			name: "invalid code",
			input: adminOIDCCallbackInput{
				Code:              " authorization-code ",
				State:             transaction.State,
				TransactionCookie: authorization.CookieValue,
			},
			want: errAdminOIDCCallbackInvalid,
		},
		{
			name:  "exchange failure",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				exchange *adminOIDCExchangeRecorder,
				_ *adminOIDCVerifierRecorder,
				_ *adminOIDCResolverRecorder,
			) {
				exchange.err = errors.New("private provider response")
			},
			want:         errAdminOIDCCallbackInvalid,
			wantExchange: 1,
		},
		{
			name:  "provider unavailable",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				exchange *adminOIDCExchangeRecorder,
				_ *adminOIDCVerifierRecorder,
				_ *adminOIDCResolverRecorder,
			) {
				exchange.err = errAdminOIDCProviderUnavailable
			},
			want:         errAdminOIDCProviderUnavailable,
			wantExchange: 1,
		},
		{
			name:  "nonce mismatch",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				_ *adminOIDCExchangeRecorder,
				verifier *adminOIDCVerifierRecorder,
				_ *adminOIDCResolverRecorder,
			) {
				verifier.claims.Nonce = otherState
			},
			want:             errAdminOIDCCallbackInvalid,
			wantExchange:     1,
			wantVerification: 1,
		},
		{
			name:  "unverified email",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				_ *adminOIDCExchangeRecorder,
				verifier *adminOIDCVerifierRecorder,
				_ *adminOIDCResolverRecorder,
			) {
				verifier.claims.EmailVerified = false
			},
			want:             errAdminOIDCCallbackInvalid,
			wantExchange:     1,
			wantVerification: 1,
		},
		{
			name:  "wrong audience",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				_ *adminOIDCExchangeRecorder,
				verifier *adminOIDCVerifierRecorder,
				_ *adminOIDCResolverRecorder,
			) {
				verifier.claims.Audience = []string{"other-client"}
			},
			want:             errAdminOIDCCallbackInvalid,
			wantExchange:     1,
			wantVerification: 1,
		},
		{
			name:  "existing administrator required",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				_ *adminOIDCExchangeRecorder,
				_ *adminOIDCVerifierRecorder,
				resolver *adminOIDCResolverRecorder,
			) {
				resolver.err = errAdminOIDCAdminNotAuthorized
			},
			want:             errAdminOIDCAdminNotAuthorized,
			wantExchange:     1,
			wantVerification: 1,
			wantResolution:   1,
		},
		{
			name:  "repository failure redacted",
			input: validAdminOIDCCallbackInput(authorization, transaction),
			mutate: func(
				_ *adminOIDCExchangeRecorder,
				_ *adminOIDCVerifierRecorder,
				resolver *adminOIDCResolverRecorder,
			) {
				resolver.err = errors.New("private SQL detail")
			},
			want:             errAdminOIDCAdminLookupFailed,
			wantExchange:     1,
			wantVerification: 1,
			wantResolution:   1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exchange, verifier, resolver := adminOIDCTestCallbackDependencies(
				config,
				transaction,
				now,
			)
			if test.mutate != nil {
				test.mutate(exchange, verifier, resolver)
			}
			service, err := newAdminOIDCCallbackService(
				config,
				sealer,
				exchange,
				verifier,
				resolver,
			)
			if err != nil {
				t.Fatalf("create service: %v", err)
			}
			_, err = service.Authenticate(t.Context(), test.input, now)
			if !errors.Is(err, test.want) {
				t.Fatalf("error: got %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), "private") {
				t.Error("callback error exposes dependency detail")
			}
			if exchange.calls != test.wantExchange ||
				verifier.calls != test.wantVerification ||
				resolver.calls != test.wantResolution {
				t.Errorf(
					"calls exchange/verifier/resolver: got %d/%d/%d, want %d/%d/%d",
					exchange.calls,
					verifier.calls,
					resolver.calls,
					test.wantExchange,
					test.wantVerification,
					test.wantResolution,
				)
			}
		})
	}
}

// validAdminOIDCCallbackInput returns the exact successful callback shape used
// by table-driven negative tests.
func validAdminOIDCCallbackInput(
	authorization adminOIDCAuthorization,
	transaction adminOIDCTransaction,
) adminOIDCCallbackInput {
	return adminOIDCCallbackInput{
		Code:              "authorization-code",
		State:             transaction.State,
		TransactionCookie: authorization.CookieValue,
	}
}

// TestAdminOIDCAudienceAllowsClient covers OIDC's multi-audience azp rule.
func TestAdminOIDCAudienceAllowsClient(t *testing.T) {
	tests := []struct {
		name      string
		audience  []string
		party     string
		permitted bool
	}{
		{name: "single", audience: []string{"client"}, permitted: true},
		{name: "single matching azp", audience: []string{"client"}, party: "client", permitted: true},
		{name: "single wrong azp", audience: []string{"client"}, party: "other"},
		{name: "multiple matching azp", audience: []string{"client", "other"}, party: "client", permitted: true},
		{name: "multiple missing azp", audience: []string{"client", "other"}},
		{name: "missing client", audience: []string{"other"}},
		{name: "duplicate client", audience: []string{"client", "client"}, party: "client"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adminOIDCAudienceAllowsClient(
				test.audience,
				test.party,
				"client",
			); got != test.permitted {
				t.Errorf("permitted: got %t, want %t", got, test.permitted)
			}
		})
	}
}

// TestNewAdminOIDCCallbackServiceRequiresDependencies protects future wiring
// from accidentally serving callbacks without one trust boundary.
func TestNewAdminOIDCCallbackServiceRequiresDependencies(t *testing.T) {
	config := adminOIDCTestConfig()
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&adminOIDCSequenceEntropy{},
	)
	if err != nil {
		t.Fatal("create callback service fixture sealer")
	}
	exchange := &adminOIDCExchangeRecorder{}
	verifier := &adminOIDCVerifierRecorder{}
	resolver := &adminOIDCResolverRecorder{}

	tests := []struct {
		name       string
		config     adminOIDCConfig
		sealer     *adminOIDCTransactionSealer
		exchanger  adminOIDCTokenExchanger
		verifier   adminOIDCIDTokenVerifier
		adminUsers adminOIDCAdminResolver
	}{
		{name: "invalid config", sealer: sealer, exchanger: exchange, verifier: verifier, adminUsers: resolver},
		{name: "missing sealer", config: config, exchanger: exchange, verifier: verifier, adminUsers: resolver},
		{name: "missing exchanger", config: config, sealer: sealer, verifier: verifier, adminUsers: resolver},
		{name: "missing verifier", config: config, sealer: sealer, exchanger: exchange, adminUsers: resolver},
		{name: "missing resolver", config: config, sealer: sealer, exchanger: exchange, verifier: verifier},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := newAdminOIDCCallbackService(
				test.config,
				test.sealer,
				test.exchanger,
				test.verifier,
				test.adminUsers,
			)
			if service != nil || !errors.Is(err, errAdminOIDCDependencyRequired) {
				t.Fatalf("service=%#v error=%v", service, err)
			}
		})
	}
}
