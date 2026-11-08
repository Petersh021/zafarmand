package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func alternativeAuthHTTPApplication(
	t *testing.T,
) (*application, *recordingAdminRepository, adminUser) {
	t.Helper()
	repository := newRecordingAdminRepository()
	passwords := newTestAdminPasswordManager(t)
	user := repository.addUser(
		t,
		passwords,
		adminHTTPTestEmail,
		adminHTTPTestPassword,
		adminRoleOwner,
	)
	app := newAdminHTTPTestApplication(t, repository, passwords)
	app.now = func() time.Time {
		return time.Date(2036, 2, 3, 4, 5, 6, 0, time.UTC)
	}

	return app, repository, user
}

func alternativeAuthHTTPOTPService(
	t *testing.T,
	accountReader adminAccountReader,
	otpRepository adminEmailOTPRepository,
	dispatcher adminOTPDispatcher,
	tokenByte byte,
	code uint16,
	now func() time.Time,
) *adminOTPService {
	t.Helper()
	service, err := newAdminOTPService(
		accountReader,
		otpRepository,
		dispatcher,
		bytes.Repeat([]byte{0x74}, adminOTPKeyBytes),
		alternativeAuthOTPEntropy(tokenByte, code),
		now,
	)
	if err != nil {
		t.Fatalf("create HTTP OTP service: %v", err)
	}

	return service
}

func alternativeAuthHTTPJSON(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON response %q: %v", recorder.Body.String(), err)
	}

	return payload
}

// TestAdminLoginPresentationDoesNotReplaceUnconfiguredViews protects the
// visual contract: provider availability controls actions, not which designed
// method the visitor is allowed to see.
func TestAdminLoginPresentationDoesNotReplaceUnconfiguredViews(t *testing.T) {
	app, _, _ := alternativeAuthHTTPApplication(t)

	if data := app.adminLoginPresentation(""); data.LoginMethod != "otp" ||
		data.OTPEnabled || data.GoogleEnabled {
		t.Errorf("default presentation: %#v", data)
	}
	if data := app.adminLoginPresentation("google"); data.LoginMethod != "google" {
		t.Errorf("unconfigured Google presentation: %#v", data)
	}
	if data := app.adminLoginPresentation("password"); data.LoginMethod != "password" {
		t.Errorf("password presentation: %#v", data)
	}
}

// TestAdminOTPRequestHTTPRemainsAccountNeutral compares a known and unknown
// administrator at the public HTTP boundary. Status, JSON, and cookie shape
// remain identical while only the known account is persisted and delivered.
func TestAdminOTPRequestHTTPRemainsAccountNeutral(t *testing.T) {
	csrfToken, _ := adminHTTPToken(0x51)
	csrfCookie := &http.Cookie{
		Name:  adminLoginCSRFTokenCookieName,
		Value: csrfToken,
	}
	type outcome struct {
		body       string
		cookie     *http.Cookie
		created    int
		deliveries []adminOTPDelivery
	}
	run := func(
		t *testing.T,
		email string,
		accountReader adminAccountReader,
		tokenByte byte,
	) outcome {
		t.Helper()
		app, repository, _ := alternativeAuthHTTPApplication(t)
		otpRepository := &alternativeAuthOTPRepositoryStub{}
		dispatcher := &alternativeAuthOTPDispatcherStub{}
		app.adminOTP = alternativeAuthHTTPOTPService(
			t,
			accountReader,
			otpRepository,
			dispatcher,
			tokenByte,
			4810,
			app.now,
		)
		request := adminHTTPPostFormRequest(
			"/admin/login/otp/request",
			url.Values{
				"csrf_token": {csrfToken},
				"email":      {email},
			},
			true,
			csrfCookie,
		)
		request.Header.Set("Accept", "application/json")
		recorder := httptest.NewRecorder()
		app.routes().ServeHTTP(recorder, request)
		response := recorder.Result()
		defer response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusAccepted, recorder.Body.String())
		}
		assertAdminHTTPSecurityHeaders(
			t,
			response.Header,
			adminLoginContentSecurityPolicy,
		)
		if response.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("Content-Type: got %q", response.Header.Get("Content-Type"))
		}
		payload := alternativeAuthHTTPJSON(t, recorder)
		if payload["ok"] != true ||
			payload["status_message"] != "If that address is authorized, a code has been sent." ||
			payload["expires_in"] != float64(adminOTPChallengeLifetime/time.Second) ||
			payload["resend_after"] != float64(adminOTPResendDelaySeconds) {
			t.Errorf("JSON payload: %#v", payload)
		}
		cookie := adminHTTPResponseCookie(t, response, adminOTPChallengeCookieName)
		assertAdminHTTPCookieSecurity(
			t,
			cookie,
			adminOTPChallengeCookiePath,
			true,
		)
		if cookie.Value == "" || cookie.MaxAge != int(adminOTPChallengeLifetime/time.Second) ||
			!cookie.Expires.Equal(app.now().Add(adminOTPChallengeLifetime)) {
			t.Errorf("OTP cookie: %#v", cookie)
		}
		if len(repository.createdSessions) != 0 {
			t.Error("requesting an OTP created an administrator session")
		}

		return outcome{
			body:       recorder.Body.String(),
			cookie:     cookie,
			created:    len(otpRepository.created),
			deliveries: append([]adminOTPDelivery(nil), dispatcher.deliveries...),
		}
	}

	knownApp, _, knownUser := alternativeAuthHTTPApplication(t)
	_ = knownApp
	knownAccount := adminAccount{
		ID: knownUser.ID, Email: knownUser.Email, Role: knownUser.Role,
	}
	known := run(
		t,
		adminHTTPTestEmail,
		&alternativeAuthAccountReaderStub{account: knownAccount},
		0x52,
	)
	unknown := run(
		t,
		"unknown@example.test",
		&alternativeAuthAccountReaderStub{err: errAdminUserNotFound},
		0x53,
	)

	if known.body != unknown.body {
		t.Errorf("known and unknown response bodies differ:\nknown: %s\nunknown: %s", known.body, unknown.body)
	}
	if len(known.cookie.Value) != len(unknown.cookie.Value) ||
		known.cookie.Path != unknown.cookie.Path ||
		known.cookie.MaxAge != unknown.cookie.MaxAge {
		t.Error("known and unknown OTP cookies have different observable shapes")
	}
	if known.created != 1 || unknown.created != 0 {
		t.Errorf("created challenges known/unknown: got %d/%d, want 1/0", known.created, unknown.created)
	}
	if len(known.deliveries) != 1 || !known.deliveries[0].Deliver ||
		len(unknown.deliveries) != 1 || unknown.deliveries[0].Deliver {
		t.Errorf("delivery behavior known=%#v unknown=%#v", known.deliveries, unknown.deliveries)
	}
}

// TestAdminAlternativeLoginRoutesRejectInvalidCSRF verifies that none of the
// three new POST boundaries performs account, challenge, provider, or session
// work without the anonymous double-submit pair.
func TestAdminAlternativeLoginRoutesRejectInvalidCSRF(t *testing.T) {
	validToken, _ := adminHTTPToken(0x54)
	otherToken, _ := adminHTTPToken(0x55)
	app, repository, user := alternativeAuthHTTPApplication(t)
	accounts := &alternativeAuthAccountReaderStub{account: adminAccount{
		ID: user.ID, Email: user.Email, Role: user.Role,
	}}
	otpRepository := &alternativeAuthOTPRepositoryStub{}
	dispatcher := &alternativeAuthOTPDispatcherStub{}
	app.adminOTP = alternativeAuthHTTPOTPService(
		t,
		accounts,
		otpRepository,
		dispatcher,
		0x56,
		4811,
		app.now,
	)
	config := alternativeAuthHTTPOIDCConfig(t)
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&alternativeAuthSequenceEntropy{next: 0x20},
	)
	if err != nil {
		t.Fatalf("create OIDC sealer: %v", err)
	}
	app.adminOIDC = &adminOIDCRuntime{config: config, sealer: sealer}

	tests := []struct {
		name   string
		path   string
		values url.Values
	}{
		{
			name: "OTP request",
			path: "/admin/login/otp/request",
			values: url.Values{
				"csrf_token": {validToken},
				"email":      {adminHTTPTestEmail},
			},
		},
		{
			name: "OTP verify",
			path: "/admin/login/otp/verify",
			values: url.Values{
				"csrf_token": {validToken},
				"otp_code":   {"4811"},
			},
		},
		{
			name: "Google start",
			path: "/admin/login/google",
			values: url.Values{
				"csrf_token": {validToken},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := adminHTTPPostFormRequest(
				test.path,
				test.values,
				false,
				&http.Cookie{
					Name:  adminLoginCSRFTokenCookieName,
					Value: otherToken,
				},
			)
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Errorf("status: got %d, want %d", response.StatusCode, http.StatusForbidden)
			}
			assertAdminHTTPSecurityHeaders(
				t,
				response.Header,
				adminLoginContentSecurityPolicy,
			)
		})
	}
	if len(accounts.emails) != 0 || len(otpRepository.created) != 0 ||
		len(otpRepository.verifiedHashes) != 0 || len(dispatcher.deliveries) != 0 ||
		len(repository.createdSessions) != 0 {
		t.Errorf(
			"CSRF failure reached dependencies: accounts=%#v otp=%#v dispatcher=%#v sessions=%#v",
			accounts,
			otpRepository,
			dispatcher,
			repository.createdSessions,
		)
	}
}

// TestAdminOTPVerifyHTTPSuccess verifies real selector/code derivation, local
// session issuance, JSON enhancement, and one-time challenge cleanup.
func TestAdminOTPVerifyHTTPSuccess(t *testing.T) {
	app, repository, user := alternativeAuthHTTPApplication(t)
	app.adminEntropy = adminHTTPEntropy(0x61, 0x62)
	account := adminAccount{ID: user.ID, Email: user.Email, Role: user.Role}
	otpRepository := &alternativeAuthOTPRepositoryStub{verifyAccount: account}
	app.adminOTP = alternativeAuthHTTPOTPService(
		t,
		&alternativeAuthAccountReaderStub{},
		otpRepository,
		&alternativeAuthOTPDispatcherStub{},
		0x63,
		4812,
		app.now,
	)
	csrfToken, _ := adminHTTPToken(0x64)
	challengeToken, _ := adminHTTPToken(0x65)
	wrappedChallengeToken, err := sealAdminOTPChallengeToken(
		app.adminOTP.key,
		challengeToken,
		bytes.NewReader(bytes.Repeat([]byte{0x66}, 12)),
	)
	if err != nil {
		t.Fatalf("seal HTTP challenge token: %v", err)
	}
	request := adminHTTPPostFormRequest(
		"/admin/login/otp/verify",
		url.Values{
			"csrf_token": {csrfToken},
			"otp_code":   {"0042"},
		},
		true,
		&http.Cookie{Name: adminLoginCSRFTokenCookieName, Value: csrfToken},
		&http.Cookie{Name: adminOTPChallengeCookieName, Value: wrappedChallengeToken},
	)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusOK, recorder.Body.String())
	}
	assertAdminHTTPSecurityHeaders(
		t,
		response.Header,
		adminLoginContentSecurityPolicy,
	)
	payload := alternativeAuthHTTPJSON(t, recorder)
	if payload["ok"] != true || payload["redirect"] != "/admin" ||
		payload["status_message"] != "Access authorized." {
		t.Errorf("JSON payload: %#v", payload)
	}
	if len(otpRepository.verifiedHashes) != 1 ||
		len(repository.createdSessions) != 1 ||
		repository.createdSessions[0].UserID != user.ID {
		t.Errorf("verification/session operations: otp=%#v sessions=%#v", otpRepository, repository.createdSessions)
	}
	for _, name := range []string{
		adminSessionCookieName,
		adminSessionCSRFTokenCookieName,
	} {
		cookie := adminHTTPResponseCookie(t, response, name)
		assertAdminHTTPCookieSecurity(t, cookie, "/admin", true)
		if cookie.Value == "" || cookie.MaxAge != int(adminSessionLifetime/time.Second) {
			t.Errorf("session cookie %q: %#v", name, cookie)
		}
	}
	clearedOTP := adminHTTPResponseCookie(t, response, adminOTPChallengeCookieName)
	if clearedOTP.MaxAge != -1 || clearedOTP.Path != adminOTPChallengeCookiePath ||
		!clearedOTP.HttpOnly || !clearedOTP.Secure {
		t.Errorf("cleared OTP cookie: %#v", clearedOTP)
	}
	clearedLoginCSRF := adminHTTPResponseCookie(
		t,
		response,
		adminLoginCSRFTokenCookieName,
	)
	if clearedLoginCSRF.MaxAge != -1 || clearedLoginCSRF.Path != "/admin/login" {
		t.Errorf("cleared login CSRF cookie: %#v", clearedLoginCSRF)
	}
}

// TestAdminOTPVerifyHTTPFailure remains generic for both a wrong persisted MAC
// and a malformed browser selector, and neither path creates a local session.
func TestAdminOTPVerifyHTTPFailure(t *testing.T) {
	csrfToken, _ := adminHTTPToken(0x66)
	challengeToken, _ := adminHTTPToken(0x67)
	var sharedBody string
	tests := []struct {
		name           string
		challengeToken string
		verifyErr      error
		wantCalls      int
	}{
		{
			name:           "wrong code",
			challengeToken: challengeToken,
			verifyErr:      errAdminEmailOTPAuthenticationFailed,
			wantCalls:      1,
		},
		{
			name:           "malformed selector",
			challengeToken: "not-base64!",
			wantCalls:      0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, repository, _ := alternativeAuthHTTPApplication(t)
			otpRepository := &alternativeAuthOTPRepositoryStub{
				verifyErr: test.verifyErr,
			}
			app.adminOTP = alternativeAuthHTTPOTPService(
				t,
				&alternativeAuthAccountReaderStub{},
				otpRepository,
				&alternativeAuthOTPDispatcherStub{},
				0x68,
				4813,
				app.now,
			)
			if test.name == "wrong code" {
				var err error
				test.challengeToken, err = sealAdminOTPChallengeToken(
					app.adminOTP.key,
					test.challengeToken,
					bytes.NewReader(bytes.Repeat([]byte{0x69}, 12)),
				)
				if err != nil {
					t.Fatalf("seal failure challenge token: %v", err)
				}
			}
			request := adminHTTPPostFormRequest(
				"/admin/login/otp/verify",
				url.Values{
					"csrf_token": {csrfToken},
					"otp_code":   {"9999"},
				},
				false,
				&http.Cookie{Name: adminLoginCSRFTokenCookieName, Value: csrfToken},
				&http.Cookie{Name: adminOTPChallengeCookieName, Value: test.challengeToken},
			)
			request.Header.Set("Accept", "application/json")
			recorder := httptest.NewRecorder()
			app.routes().ServeHTTP(recorder, request)
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d", response.StatusCode, http.StatusUnauthorized)
			}
			payload := alternativeAuthHTTPJSON(t, recorder)
			if payload["ok"] != false ||
				payload["status_message"] != "Verification code is invalid or expired." {
				t.Errorf("JSON payload: %#v", payload)
			}
			if len(otpRepository.verifiedHashes) != test.wantCalls ||
				len(repository.createdSessions) != 0 {
				t.Errorf("verification calls=%d sessions=%d", len(otpRepository.verifiedHashes), len(repository.createdSessions))
			}
			if sharedBody == "" {
				sharedBody = recorder.Body.String()
			} else if recorder.Body.String() != sharedBody {
				t.Error("OTP authentication failures produced different JSON bodies")
			}
		})
	}
}

type alternativeAuthSequenceEntropy struct {
	next byte
}

func (entropy *alternativeAuthSequenceEntropy) Read(target []byte) (int, error) {
	for index := range target {
		entropy.next++
		target[index] = entropy.next
	}

	return len(target), nil
}

func alternativeAuthHTTPOIDCConfig(t *testing.T) adminOIDCConfig {
	t.Helper()
	config, enabled, err := loadOptionalAdminOIDCConfig(
		alternativeAuthEnvironmentLookup(alternativeAuthOIDCEnvironment()),
	)
	if err != nil || !enabled {
		t.Fatalf("load HTTP OIDC config: enabled=%t error=%v", enabled, err)
	}

	return config
}

// TestAdminGoogleLoginHTTP verifies the CSRF-protected start of the provider
// flow, including a Lax callback-only transaction cookie and a pinned Google
// authorization destination.
func TestAdminGoogleLoginHTTP(t *testing.T) {
	app, repository, _ := alternativeAuthHTTPApplication(t)
	config := alternativeAuthHTTPOIDCConfig(t)
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&alternativeAuthSequenceEntropy{next: 0x30},
	)
	if err != nil {
		t.Fatalf("create OIDC sealer: %v", err)
	}
	app.adminOIDC = &adminOIDCRuntime{config: config, sealer: sealer}
	csrfToken, _ := adminHTTPToken(0x70)
	request := adminHTTPPostFormRequest(
		"/admin/login/google",
		url.Values{"csrf_token": {csrfToken}},
		true,
		&http.Cookie{Name: adminLoginCSRFTokenCookieName, Value: csrfToken},
	)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusSeeOther, recorder.Body.String())
	}
	assertAdminHTTPSecurityHeaders(
		t,
		response.Header,
		adminLoginContentSecurityPolicy,
	)
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization redirect: %v", err)
	}
	if location.Scheme != "https" || location.Host != "accounts.google.com" ||
		location.Path != "/o/oauth2/v2/auth" ||
		location.Query().Get("client_id") != config.clientID ||
		location.Query().Get("redirect_uri") != config.redirectURL ||
		location.Query().Get("state") == "" ||
		location.Query().Get("nonce") == "" ||
		location.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("authorization redirect: %s", location.String())
	}
	cookie := adminHTTPResponseCookie(t, response, adminOIDCTransactionCookieName)
	if cookie.Path != adminOIDCCallbackPath || !cookie.HttpOnly || !cookie.Secure ||
		cookie.SameSite != http.SameSiteLaxMode || cookie.Value == "" ||
		cookie.MaxAge != int(adminOIDCTransactionLifetime/time.Second) {
		t.Errorf("OIDC transaction cookie: %#v", cookie)
	}
	if len(repository.createdSessions) != 0 {
		t.Error("starting Google authorization created a local session")
	}
}

type alternativeAuthOIDCExchangeStub struct {
	calls   int
	request adminOIDCCodeExchangeRequest
	result  adminOIDCTokenSet
	err     error
}

func (stub *alternativeAuthOIDCExchangeStub) Exchange(
	_ context.Context,
	request adminOIDCCodeExchangeRequest,
) (adminOIDCTokenSet, error) {
	stub.calls++
	stub.request = request

	return stub.result, stub.err
}

type alternativeAuthOIDCVerifierStub struct {
	calls  int
	raw    string
	claims adminOIDCIDTokenClaims
	err    error
}

func (stub *alternativeAuthOIDCVerifierStub) Verify(
	_ context.Context,
	raw string,
) (adminOIDCIDTokenClaims, error) {
	stub.calls++
	stub.raw = raw

	return stub.claims, stub.err
}

type alternativeAuthOIDCResolverStub struct {
	calls    int
	identity adminOIDCVerifiedIdentity
	admin    adminOIDCAdmin
	err      error
}

func (stub *alternativeAuthOIDCResolverStub) ResolveExistingActiveAdmin(
	_ context.Context,
	identity adminOIDCVerifiedIdentity,
) (adminOIDCAdmin, error) {
	stub.calls++
	stub.identity = identity

	return stub.admin, stub.err
}

// TestAdminGoogleCallbackHTTPSuccess executes the network-free callback
// orchestration and proves it converges on the same local session boundary as
// password and OTP authentication.
func TestAdminGoogleCallbackHTTPSuccess(t *testing.T) {
	app, repository, user := alternativeAuthHTTPApplication(t)
	app.adminEntropy = adminHTTPEntropy(0x71, 0x72)
	now := app.now()
	config := alternativeAuthHTTPOIDCConfig(t)
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&alternativeAuthSequenceEntropy{next: 0x40},
	)
	if err != nil {
		t.Fatalf("create callback sealer: %v", err)
	}
	authorization, err := newAdminOIDCAuthorization(
		config,
		sealer,
		&alternativeAuthSequenceEntropy{next: 0x50},
		now,
	)
	if err != nil {
		t.Fatalf("create callback authorization: %v", err)
	}
	transaction, err := sealer.open(authorization.CookieValue, now)
	if err != nil {
		t.Fatalf("open callback transaction: %v", err)
	}
	exchange := &alternativeAuthOIDCExchangeStub{
		result: adminOIDCTokenSet{RawIDToken: "signed-test-id-token"},
	}
	verifier := &alternativeAuthOIDCVerifierStub{claims: adminOIDCIDTokenClaims{
		Issuer:        adminOIDCGoogleIssuer,
		Subject:       "google-test-subject",
		Audience:      []string{config.clientID},
		Nonce:         transaction.Nonce,
		Email:         user.Email,
		EmailVerified: true,
		IssuedAt:      now.Add(-time.Minute),
		ExpiresAt:     now.Add(time.Hour),
	}}
	resolver := &alternativeAuthOIDCResolverStub{admin: adminOIDCAdmin{
		UserID: user.ID, Email: user.Email, Role: user.Role,
	}}
	callback, err := newAdminOIDCCallbackService(
		config,
		sealer,
		exchange,
		verifier,
		resolver,
	)
	if err != nil {
		t.Fatalf("create callback service: %v", err)
	}
	app.adminOIDC = &adminOIDCRuntime{
		config: config, sealer: sealer, callback: callback,
	}
	query := url.Values{
		"code":  {"provider-authorization-code"},
		"state": {transaction.State},
	}
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/login/google/callback?"+query.Encode(),
		nil,
		true,
		&http.Cookie{
			Name: adminOIDCTransactionCookieName, Value: authorization.CookieValue,
		},
	)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther ||
		response.Header.Get("Location") != "/admin" {
		t.Fatalf("status/location: got %d/%q, want 303/\"/admin\"; body=%q", response.StatusCode, response.Header.Get("Location"), recorder.Body.String())
	}
	if exchange.calls != 1 || verifier.calls != 1 || resolver.calls != 1 ||
		exchange.request.CodeVerifier != transaction.CodeVerifier ||
		resolver.identity.Email != user.Email {
		t.Errorf("callback handoff exchange=%#v verifier=%#v resolver=%#v", exchange, verifier, resolver)
	}
	if len(repository.createdSessions) != 1 ||
		repository.createdSessions[0].UserID != user.ID {
		t.Errorf("created sessions: %#v", repository.createdSessions)
	}
	for _, name := range []string{
		adminSessionCookieName,
		adminSessionCSRFTokenCookieName,
	} {
		cookie := adminHTTPResponseCookie(t, response, name)
		assertAdminHTTPCookieSecurity(
			t,
			cookie,
			"/admin",
			true,
			http.SameSiteLaxMode,
		)
	}
	cleared := adminHTTPResponseCookie(t, response, adminOIDCTransactionCookieName)
	if cleared.MaxAge != -1 || cleared.Path != adminOIDCCallbackPath ||
		cleared.SameSite != http.SameSiteLaxMode || !cleared.Secure {
		t.Errorf("cleared OIDC transaction cookie: %#v", cleared)
	}
}

// TestAdminGoogleCallbackHTTPFailure clears the one-use transaction, rotates
// anonymous CSRF, and creates no session when the initiating cookie is absent.
func TestAdminGoogleCallbackHTTPFailure(t *testing.T) {
	app, repository, _ := alternativeAuthHTTPApplication(t)
	app.adminEntropy = adminHTTPEntropy(0x73)
	config := alternativeAuthHTTPOIDCConfig(t)
	sealer, err := newAdminOIDCTransactionSealer(
		config.cookieKey,
		&alternativeAuthSequenceEntropy{next: 0x60},
	)
	if err != nil {
		t.Fatalf("create failure sealer: %v", err)
	}
	// The callback dependency is intentionally non-nil; the missing transaction
	// cookie must fail before any provider or local-identity operation.
	callback, err := newAdminOIDCCallbackService(
		config,
		sealer,
		&alternativeAuthOIDCExchangeStub{},
		&alternativeAuthOIDCVerifierStub{},
		&alternativeAuthOIDCResolverStub{},
	)
	if err != nil {
		t.Fatalf("create failure callback service: %v", err)
	}
	app.adminOIDC = &adminOIDCRuntime{
		config: config, sealer: sealer, callback: callback,
	}
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/login/google/callback?code=provider-code&state=state",
		nil,
		false,
	)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d; body=%q", response.StatusCode, http.StatusUnauthorized, recorder.Body.String())
	}
	assertAdminHTTPSecurityHeaders(
		t,
		response.Header,
		adminLoginContentSecurityPolicy,
	)
	if len(repository.createdSessions) != 0 {
		t.Error("invalid Google callback created a local session")
	}
	csrfCookie := adminHTTPResponseCookie(
		t,
		response,
		adminLoginCSRFTokenCookieName,
	)
	if csrfCookie.Value == "" || csrfCookie.Path != "/admin/login" ||
		csrfCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("rotated login CSRF cookie: %#v", csrfCookie)
	}
	cleared := adminHTTPResponseCookie(t, response, adminOIDCTransactionCookieName)
	if cleared.MaxAge != -1 || cleared.Path != adminOIDCCallbackPath {
		t.Errorf("cleared transaction cookie: %#v", cleared)
	}
	if !strings.Contains(recorder.Body.String(), "Google sign-in could not be completed") {
		t.Error("Google callback failure lacks the generic browser message")
	}
}

// Compile-time assertions keep the test doubles aligned with their production
// boundaries as those interfaces evolve.
var (
	_ adminAccountReader       = (*alternativeAuthAccountReaderStub)(nil)
	_ adminEmailOTPRepository  = (*alternativeAuthOTPRepositoryStub)(nil)
	_ adminOTPDispatcher       = (*alternativeAuthOTPDispatcherStub)(nil)
	_ adminOIDCTokenExchanger  = (*alternativeAuthOIDCExchangeStub)(nil)
	_ adminOIDCIDTokenVerifier = (*alternativeAuthOIDCVerifierStub)(nil)
	_ adminOIDCAdminResolver   = (*alternativeAuthOIDCResolverStub)(nil)
)
