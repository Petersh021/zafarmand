package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	adminOTPKeyEnvironmentName         = "ZAFARMAND_ADMIN_OTP_HMAC_KEY"
	adminSMTPAddressEnvironmentName    = "ZAFARMAND_SMTP_ADDRESS"
	adminSMTPHostEnvironmentName       = "ZAFARMAND_SMTP_HOST"
	adminSMTPUsernameEnvironmentName   = "ZAFARMAND_SMTP_USERNAME"
	adminSMTPPasswordEnvironmentName   = "ZAFARMAND_SMTP_PASSWORD"
	adminSMTPFromEnvironmentName       = "ZAFARMAND_SMTP_FROM"
	adminAlternativeAuthNetworkTimeout = 10 * time.Second
	adminOTPChallengeLifetime          = 5 * time.Minute
	adminOTPChallengeCookieName        = "zafarmand_admin_email_otp"
	adminOTPChallengeCookiePath        = "/admin/login/otp"
	adminOTPResendDelaySeconds         = 24
	adminOTPChallengeCookieVersion     = byte(1)
	adminGoogleOIDCDiscoveryIssuer     = "https://accounts.google.com"
)

var adminOTPChallengeCookiePurpose = []byte(
	"zafarmand administrator OTP browser selector v1",
)

var (
	errAdminOTPConfigurationRequired = errors.New(
		"administrator OTP configuration is required",
	)
	errAdminOTPConfigurationInvalid = errors.New(
		"administrator OTP configuration is invalid",
	)
	errAdminOTPServiceRequired = errors.New(
		"administrator OTP service dependency is required",
	)
	errAdminOTPRequestUnavailable = errors.New(
		"administrator OTP request is unavailable",
	)
)

// adminOTPConfig is populated only when every mail and HMAC setting is present.
// Keeping its fields private prevents a template or generic formatter from
// accidentally receiving the SMTP password or code-authentication key.
type adminOTPConfig struct {
	key  [adminOTPKeyBytes]byte
	smtp adminSMTPConfig
}

// loadOptionalAdminOTPConfig makes email-code authentication an explicit
// all-or-nothing feature. A completely absent configuration leaves the
// existing password login usable; a partial configuration is a startup error.
func loadOptionalAdminOTPConfig(
	lookup environmentLookup,
) (adminOTPConfig, bool, error) {
	if lookup == nil {
		return adminOTPConfig{}, false, errAdminOTPConfigurationRequired
	}

	names := []string{
		adminOTPKeyEnvironmentName,
		adminSMTPAddressEnvironmentName,
		adminSMTPHostEnvironmentName,
		adminSMTPUsernameEnvironmentName,
		adminSMTPPasswordEnvironmentName,
		adminSMTPFromEnvironmentName,
	}
	values := make(map[string]string, len(names))
	present := 0
	for _, name := range names {
		value, exists := lookup(name)
		if exists && value != "" {
			present++
		}
		values[name] = value
	}
	if present == 0 {
		return adminOTPConfig{}, false, nil
	}
	if present != len(names) {
		return adminOTPConfig{}, false, errAdminOTPConfigurationRequired
	}

	keyBytes, err := base64.RawURLEncoding.Strict().DecodeString(
		values[adminOTPKeyEnvironmentName],
	)
	if err != nil || len(keyBytes) != adminOTPKeyBytes ||
		base64.RawURLEncoding.EncodeToString(keyBytes) !=
			values[adminOTPKeyEnvironmentName] {
		return adminOTPConfig{}, false, errAdminOTPConfigurationInvalid
	}

	config := adminOTPConfig{
		smtp: adminSMTPConfig{
			Address:  values[adminSMTPAddressEnvironmentName],
			Host:     values[adminSMTPHostEnvironmentName],
			Username: values[adminSMTPUsernameEnvironmentName],
			Password: values[adminSMTPPasswordEnvironmentName],
			From:     values[adminSMTPFromEnvironmentName],
			Timeout:  adminOTPMailTimeout,
		},
	}
	copy(config.key[:], keyBytes)
	if _, err := newSMTPAdminOTPMailer(config.smtp); err != nil {
		return adminOTPConfig{}, false, errAdminOTPConfigurationInvalid
	}

	return config, true, nil
}

// loadOptionalAdminOIDCConfig applies the same deployment rule to Google:
// zero settings disable the method, while any partial or malformed set fails
// startup rather than presenting a button whose trust boundary is incomplete.
func loadOptionalAdminOIDCConfig(
	lookup environmentLookup,
) (adminOIDCConfig, bool, error) {
	if lookup == nil {
		return adminOIDCConfig{}, false, errAdminOIDCConfigurationRequired
	}

	names := []string{
		adminOIDCClientIDEnvironmentName,
		adminOIDCClientSecretEnvironmentName,
		adminOIDCRedirectURLenvironmentName,
		adminOIDCCookieKeyEnvironmentName,
	}
	present := 0
	for _, name := range names {
		if value, exists := lookup(name); exists && value != "" {
			present++
		}
	}
	if present == 0 {
		return adminOIDCConfig{}, false, nil
	}
	if present != len(names) {
		return adminOIDCConfig{}, false, errAdminOIDCConfigurationRequired
	}

	config, err := loadAdminOIDCConfig(lookup)
	if err != nil {
		return adminOIDCConfig{}, false, err
	}

	return config, true, nil
}

// adminOTPService joins the password-free account projection, challenge store,
// keyed code derivation, and bounded delivery queue. HTTP handlers remain
// responsible only for CSRF, cookies, and response representation.
type adminOTPService struct {
	accounts   adminAccountReader
	challenges adminEmailOTPRepository
	dispatcher adminOTPDispatcher
	key        []byte
	entropy    io.Reader
	now        func() time.Time
}

type adminOTPIssueResult struct {
	ChallengeToken string
	ExpiresAt      time.Time
	Issued         bool
}

func newAdminOTPService(
	accounts adminAccountReader,
	challenges adminEmailOTPRepository,
	dispatcher adminOTPDispatcher,
	key []byte,
	entropy io.Reader,
	now func() time.Time,
) (*adminOTPService, error) {
	if accounts == nil || challenges == nil || dispatcher == nil ||
		len(key) != adminOTPKeyBytes || entropy == nil || now == nil {
		return nil, errAdminOTPServiceRequired
	}

	return &adminOTPService{
		accounts:   accounts,
		challenges: challenges,
		dispatcher: dispatcher,
		key:        append([]byte(nil), key...),
		entropy:    entropy,
		now:        now,
	}, nil
}

// Issue creates equal-shape random work before account lookup and returns the
// same successful result for an unknown address. Only an active local account
// gets a durable challenge and a real delivery item.
func (service *adminOTPService) Issue(
	ctx context.Context,
	email string,
	existingChallengeToken string,
) (adminOTPIssueResult, error) {
	if service == nil || ctx == nil || service.accounts == nil ||
		service.challenges == nil || service.dispatcher == nil ||
		service.entropy == nil || service.now == nil {
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}

	challengeToken, challengeHash, err := generateAdminToken(service.entropy)
	if err != nil {
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}
	code, err := generateAdminOTPCode(service.entropy)
	if err != nil {
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}
	codeMAC, err := adminOTPCodeMAC(service.key, challengeHash, code)
	if err != nil {
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}
	expiresAt := service.now().UTC().Add(adminOTPChallengeLifetime)
	// Every address receives a fresh indistinguishable browser selector. Only
	// Issued reveals to this internal service whether its digest was persisted;
	// the HTTP layer still writes a dummy selector for unknown accounts.
	result := adminOTPIssueResult{
		ChallengeToken: challengeToken,
		ExpiresAt:      expiresAt,
	}
	delivery := adminOTPDelivery{
		Code:      code,
		ExpiresAt: expiresAt,
	}

	normalizedEmail, normalizeErr := normalizeAdminEmail(email)
	if normalizeErr == nil {
		account, accountErr := service.accounts.FindActiveAccountByEmail(
			ctx,
			normalizedEmail,
		)
		switch {
		case accountErr == nil:
			err = service.challenges.CreateChallenge(
				ctx,
				adminEmailOTPChallenge{
					ChallengeHash: challengeHash,
					UserID:        account.ID,
					CodeMAC:       codeMAC,
					ExpiresAt:     expiresAt,
				},
			)
			if errors.Is(err, errAdminEmailOTPRateLimited) {
				// Keep the prior browser challenge usable and perform one dummy queue
				// operation so the public response remains account-neutral.
				if dispatchErr := service.dispatcher.Dispatch(ctx, delivery); dispatchErr != nil {
					return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
				}
				return service.wrapAdminOTPIssueResult(
					result,
					existingChallengeToken,
				)
			}
			if err != nil {
				return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
			}
			result.Issued = true
			delivery.Recipient = account.Email
			delivery.Deliver = true
		case errors.Is(accountErr, errAdminUserNotFound),
			errors.Is(accountErr, errAdminUserInvalid):
			// Continue with a dummy queue item and the same browser response.
		default:
			return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
		}
	}

	if err := service.dispatcher.Dispatch(ctx, delivery); err != nil {
		if result.Issued {
			// Best-effort compensation prevents a valid-but-undelivered code from
			// blocking an immediate retry after queue saturation.
			_ = service.challenges.CancelChallenge(ctx, challengeHash)
		}
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}

	return service.wrapAdminOTPIssueResult(result, existingChallengeToken)
}

// wrapAdminOTPIssueResult gives every neutral response a fresh opaque cookie
// while retaining the selector already held by that browser. Rotating the
// encrypted envelope hides whether a new database challenge was issued, and
// retaining its authenticated payload prevents an early resend from destroying
// a still-valid code.
func (service *adminOTPService) wrapAdminOTPIssueResult(
	result adminOTPIssueResult,
	existingChallengeToken string,
) (adminOTPIssueResult, error) {
	selector := result.ChallengeToken
	if !result.Issued {
		if existingSelector, valid := openAdminOTPChallengeToken(
			service.key,
			existingChallengeToken,
		); valid {
			selector = existingSelector
		}
	}

	wrappedSelector, err := sealAdminOTPChallengeToken(
		service.key,
		selector,
		service.entropy,
	)
	if err != nil {
		return adminOTPIssueResult{}, errAdminOTPRequestUnavailable
	}
	result.ChallengeToken = wrappedSelector

	return result, nil
}

// Verify derives the candidate MAC from the browser selector and consumes a
// match transactionally. Every malformed, missing, expired, and wrong value
// converges on the repository's account-neutral authentication failure.
func (service *adminOTPService) Verify(
	ctx context.Context,
	challengeToken string,
	code string,
) (adminAccount, error) {
	if service == nil || ctx == nil || service.challenges == nil {
		return adminAccount{}, errAdminEmailOTPAuthenticationFailed
	}
	rawChallengeToken, valid := openAdminOTPChallengeToken(
		service.key,
		challengeToken,
	)
	if !valid {
		return adminAccount{}, errAdminEmailOTPAuthenticationFailed
	}
	_, challengeHash, valid := decodeAndHashAdminToken(rawChallengeToken)
	if !valid || !validAdminOTPCode(code) {
		return adminAccount{}, errAdminEmailOTPAuthenticationFailed
	}
	codeMAC, err := adminOTPCodeMAC(service.key, challengeHash, code)
	if err != nil {
		return adminAccount{}, errAdminEmailOTPAuthenticationFailed
	}

	return service.challenges.VerifyAndConsume(ctx, challengeHash, codeMAC)
}

// sealAdminOTPChallengeToken uses a domain-separated AES-GCM key so the short
// browser selector is both confidential and authenticated. A fresh nonce makes
// repeated neutral responses unlinkable by their Set-Cookie value.
func sealAdminOTPChallengeToken(
	key []byte,
	challengeToken string,
	entropy io.Reader,
) (string, error) {
	if len(key) != adminOTPKeyBytes || entropy == nil {
		return "", errAdminOTPRequestUnavailable
	}
	if _, _, valid := decodeAndHashAdminToken(challengeToken); !valid {
		return "", errAdminOTPRequestUnavailable
	}
	block, err := aes.NewCipher(deriveAdminOTPCookieKey(key))
	if err != nil {
		return "", errAdminOTPRequestUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", errAdminOTPRequestUnavailable
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(entropy, nonce); err != nil {
		return "", errAdminOTPRequestUnavailable
	}
	sealed := aead.Seal(
		nil,
		nonce,
		[]byte(challengeToken),
		adminOTPChallengeCookiePurpose,
	)
	payload := make([]byte, 1, 1+len(nonce)+len(sealed))
	payload[0] = adminOTPChallengeCookieVersion
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func openAdminOTPChallengeToken(
	key []byte,
	wrappedToken string,
) (string, bool) {
	if len(key) != adminOTPKeyBytes || wrappedToken == "" {
		return "", false
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(wrappedToken)
	if err != nil || len(payload) < 2 ||
		payload[0] != adminOTPChallengeCookieVersion {
		return "", false
	}
	block, err := aes.NewCipher(deriveAdminOTPCookieKey(key))
	if err != nil {
		return "", false
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(payload) <= 1+aead.NonceSize()+aead.Overhead() {
		return "", false
	}
	nonceEnd := 1 + aead.NonceSize()
	plaintext, err := aead.Open(
		nil,
		payload[1:nonceEnd],
		payload[nonceEnd:],
		adminOTPChallengeCookiePurpose,
	)
	if err != nil {
		return "", false
	}
	challengeToken := string(plaintext)
	if _, _, valid := decodeAndHashAdminToken(challengeToken); !valid {
		return "", false
	}

	return challengeToken, true
}

func deriveAdminOTPCookieKey(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(adminOTPChallengeCookiePurpose)

	return mac.Sum(nil)
}

// setAdminOTPChallengeCookie stores only an authenticated opaque envelope.
// PostgreSQL keeps the selector digest only for a real issued challenge; every
// request rotates the browser value so Set-Cookie behavior is account-neutral.
func setAdminOTPChallengeCookie(
	w http.ResponseWriter,
	r *http.Request,
	result adminOTPIssueResult,
) {
	if w == nil || r == nil || result.ChallengeToken == "" ||
		result.ExpiresAt.IsZero() {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminOTPChallengeCookieName,
		Value:    result.ChallengeToken,
		Path:     adminOTPChallengeCookiePath,
		Expires:  result.ExpiresAt,
		MaxAge:   int(adminOTPChallengeLifetime / time.Second),
		HttpOnly: true,
		Secure:   requestUsesSecureCookies(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAdminOTPChallengeCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil || r == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminOTPChallengeCookieName,
		Value:    "",
		Path:     adminOTPChallengeCookiePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesSecureCookies(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// adminOIDCRuntime contains the production authorization and callback seams as
// one optional application dependency. Secrets remain confined to its private
// config and provider adapters.
type adminOIDCRuntime struct {
	config   adminOIDCConfig
	sealer   *adminOIDCTransactionSealer
	callback *adminOIDCCallbackService
}

type googleAdminOIDCAdapter struct {
	config          adminOIDCConfig
	providerContext context.Context
	mu              sync.RWMutex
	oauthConfig     *oauth2.Config
	verifier        *oidc.IDTokenVerifier
}

// newGoogleAdminOIDCAdapter validates local composition without contacting the
// provider. Discovery is lazy and cached after its first success, so a transient
// Google or DNS outage cannot prevent password login or the public site from
// starting. Google recommends a vetted OIDC library because signature and claim
// verification are security critical and easy to implement incorrectly by hand.
func newGoogleAdminOIDCAdapter(
	ctx context.Context,
	config adminOIDCConfig,
) (*googleAdminOIDCAdapter, error) {
	if ctx == nil || !config.valid() {
		return nil, errAdminOIDCDependencyRequired
	}
	httpClient := &http.Client{Timeout: adminAlternativeAuthNetworkTimeout}

	return &googleAdminOIDCAdapter{
		config:          config,
		providerContext: oidc.ClientContext(ctx, httpClient),
	}, nil
}

// providerDependencies serializes only the first discovery attempt. Failures
// are not cached, allowing a later callback to recover when Google or DNS is
// available again; a successful verifier and remote-key cache are reused.
func (adapter *googleAdminOIDCAdapter) providerDependencies() (
	*oauth2.Config,
	*oidc.IDTokenVerifier,
	error,
) {
	if adapter == nil || adapter.providerContext == nil || !adapter.config.valid() {
		return nil, nil, errAdminOIDCDependencyRequired
	}
	adapter.mu.RLock()
	if adapter.oauthConfig != nil && adapter.verifier != nil {
		oauthConfig := adapter.oauthConfig
		verifier := adapter.verifier
		adapter.mu.RUnlock()
		return oauthConfig, verifier, nil
	}
	adapter.mu.RUnlock()

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.oauthConfig != nil && adapter.verifier != nil {
		return adapter.oauthConfig, adapter.verifier, nil
	}
	provider, err := oidc.NewProvider(
		adapter.providerContext,
		adminGoogleOIDCDiscoveryIssuer,
	)
	if err != nil {
		return nil, nil, errAdminOIDCProviderUnavailable
	}
	oauthConfig := &oauth2.Config{
		ClientID:     adapter.config.clientID,
		ClientSecret: adapter.config.clientSecret,
		RedirectURL:  adapter.config.redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	verifier := provider.VerifierContext(
		adapter.providerContext,
		&oidc.Config{ClientID: adapter.config.clientID},
	)
	adapter.oauthConfig = oauthConfig
	adapter.verifier = verifier

	return oauthConfig, verifier, nil
}

func (adapter *googleAdminOIDCAdapter) Exchange(
	ctx context.Context,
	request adminOIDCCodeExchangeRequest,
) (adminOIDCTokenSet, error) {
	if adapter == nil || ctx == nil ||
		request.RedirectURL != adapter.config.redirectURL ||
		!validAdminOIDCAuthorizationCode(request.Code) ||
		!validAdminOIDCRandomValue(request.CodeVerifier) {
		return adminOIDCTokenSet{}, errAdminOIDCCallbackInvalid
	}
	oauthConfig, _, err := adapter.providerDependencies()
	if err != nil {
		return adminOIDCTokenSet{}, err
	}
	token, err := oauthConfig.Exchange(
		ctx,
		request.Code,
		oauth2.VerifierOption(request.CodeVerifier),
	)
	if err != nil {
		if adminOIDCNetworkFailure(err) {
			return adminOIDCTokenSet{}, errAdminOIDCProviderUnavailable
		}
		return adminOIDCTokenSet{}, errAdminOIDCCallbackInvalid
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return adminOIDCTokenSet{}, errAdminOIDCCallbackInvalid
	}

	return adminOIDCTokenSet{RawIDToken: rawIDToken}, nil
}

func (adapter *googleAdminOIDCAdapter) Verify(
	ctx context.Context,
	rawIDToken string,
) (adminOIDCIDTokenClaims, error) {
	if adapter == nil || ctx == nil ||
		strings.TrimSpace(rawIDToken) == "" {
		return adminOIDCIDTokenClaims{}, errAdminOIDCCallbackInvalid
	}
	_, verifier, err := adapter.providerDependencies()
	if err != nil {
		return adminOIDCIDTokenClaims{}, err
	}
	token, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		if adminOIDCNetworkFailure(err) {
			return adminOIDCIDTokenClaims{}, errAdminOIDCProviderUnavailable
		}
		return adminOIDCIDTokenClaims{}, errAdminOIDCCallbackInvalid
	}
	var extra struct {
		AuthorizedParty string `json:"azp"`
		Email           string `json:"email"`
		EmailVerified   bool   `json:"email_verified"`
	}
	if err := token.Claims(&extra); err != nil {
		return adminOIDCIDTokenClaims{}, errAdminOIDCCallbackInvalid
	}

	return adminOIDCIDTokenClaims{
		Issuer:          token.Issuer,
		Subject:         token.Subject,
		Audience:        append([]string(nil), token.Audience...),
		AuthorizedParty: extra.AuthorizedParty,
		Nonce:           token.Nonce,
		Email:           extra.Email,
		EmailVerified:   extra.EmailVerified,
		IssuedAt:        token.IssuedAt,
		ExpiresAt:       token.Expiry,
	}, nil
}

func adminOIDCNetworkFailure(err error) bool {
	if err == nil {
		return false
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

// compile-time interface checks keep the two provider responsibilities visible.
var _ adminOIDCTokenExchanger = (*googleAdminOIDCAdapter)(nil)
var _ adminOIDCIDTokenVerifier = (*googleAdminOIDCAdapter)(nil)
