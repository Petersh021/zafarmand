package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Google OIDC configuration and protocol constants keep the external trust
// boundary explicit. The authorization endpoint and issuer are fixed rather
// than accepted from a browser or environment variable.
const (
	adminOIDCClientIDEnvironmentName     = "ZAFARMAND_GOOGLE_OIDC_CLIENT_ID"
	adminOIDCClientSecretEnvironmentName = "ZAFARMAND_GOOGLE_OIDC_CLIENT_SECRET"
	adminOIDCRedirectURLenvironmentName  = "ZAFARMAND_GOOGLE_OIDC_REDIRECT_URL"
	adminOIDCCookieKeyEnvironmentName    = "ZAFARMAND_GOOGLE_OIDC_COOKIE_KEY"

	adminOIDCGoogleAuthorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
	adminOIDCGoogleIssuer                = "https://accounts.google.com"
	adminOIDCCallbackPath                = "/admin/login/google/callback"

	adminOIDCTransactionCookieName = "zafarmand_admin_google_oidc"
	adminOIDCTransactionVersion    = 1
	adminOIDCTransactionLifetime   = 10 * time.Minute
	adminOIDCClockSkew             = time.Minute
	adminOIDCRandomBytes           = 32
	adminOIDCCookieKeyBytes        = 32
	adminOIDCMaximumCookieBytes    = 4096
	adminOIDCMaximumCodeBytes      = 4096
	adminOIDCMaximumSubjectRunes   = 255
	adminOIDCMaximumClientIDRunes  = 512
	adminOIDCMaximumSecretRunes    = 1024

	adminOIDCTransactionAAD = "zafarmand/admin/google-oidc-transaction/v1"
)

// Stable OIDC errors never include a client secret, authorization code, token,
// administrator address, provider response, or injected dependency error.
var (
	errAdminOIDCConfigurationRequired = errors.New(
		"Google administrator sign-in configuration is required",
	)
	errAdminOIDCConfigurationInvalid = errors.New(
		"Google administrator sign-in configuration is invalid",
	)
	errAdminOIDCDependencyRequired = errors.New(
		"Google administrator sign-in dependency is required",
	)
	errAdminOIDCTransactionInvalid = errors.New(
		"Google administrator sign-in transaction is invalid",
	)
	errAdminOIDCCallbackInvalid = errors.New(
		"Google administrator sign-in callback is invalid",
	)
	errAdminOIDCProviderUnavailable = errors.New(
		"Google administrator sign-in provider is unavailable",
	)
	errAdminOIDCAdminNotAuthorized = errors.New(
		"Google account is not an authorized administrator",
	)
	errAdminOIDCAdminLookupFailed = errors.New(
		"Google administrator lookup failed",
	)
)

// adminOIDCConfig contains the confidential and public settings needed by a
// server-side Google authorization-code flow. Its unexported fields reduce the
// chance that a generic formatter or template receives the client secret or
// cookie key.
type adminOIDCConfig struct {
	clientID     string
	clientSecret string
	redirectURL  string
	cookieKey    [adminOIDCCookieKeyBytes]byte
}

// loadAdminOIDCConfig accepts one exact, complete environment configuration.
// Missing and partially configured deployments fail closed without identifying
// which secret was absent in a public-facing error.
func loadAdminOIDCConfig(lookup environmentLookup) (adminOIDCConfig, error) {
	if lookup == nil {
		return adminOIDCConfig{}, errAdminOIDCConfigurationRequired
	}

	clientID, clientIDExists := lookup(adminOIDCClientIDEnvironmentName)
	clientSecret, clientSecretExists := lookup(
		adminOIDCClientSecretEnvironmentName,
	)
	redirectURL, redirectURLExists := lookup(
		adminOIDCRedirectURLenvironmentName,
	)
	cookieKeyText, cookieKeyExists := lookup(
		adminOIDCCookieKeyEnvironmentName,
	)
	if !clientIDExists || !clientSecretExists || !redirectURLExists ||
		!cookieKeyExists || clientID == "" || clientSecret == "" ||
		redirectURL == "" || cookieKeyText == "" {
		return adminOIDCConfig{}, errAdminOIDCConfigurationRequired
	}

	if !validAdminOIDCText(clientID, 1, adminOIDCMaximumClientIDRunes) ||
		!validAdminOIDCText(clientSecret, 8, adminOIDCMaximumSecretRunes) ||
		!validAdminOIDCRedirectURL(redirectURL) {
		return adminOIDCConfig{}, errAdminOIDCConfigurationInvalid
	}

	decodedKey, err := base64.RawURLEncoding.Strict().DecodeString(cookieKeyText)
	if err != nil || len(decodedKey) != adminOIDCCookieKeyBytes ||
		base64.RawURLEncoding.EncodeToString(decodedKey) != cookieKeyText {
		return adminOIDCConfig{}, errAdminOIDCConfigurationInvalid
	}

	config := adminOIDCConfig{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
	}
	copy(config.cookieKey[:], decodedKey)

	return config, nil
}

// validAdminOIDCText rejects padded, malformed, or control-bearing environment
// values before they can enter a URL, HTTP request, or provider-library config.
func validAdminOIDCText(value string, minimum int, maximum int) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		value != strings.TrimSpace(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}

	return true
}

// validAdminOIDCRedirectURL allows production HTTPS and deliberate loopback
// HTTP development only. Query, fragment, userinfo, and a different callback
// path are rejected so deployment configuration cannot widen the callback.
func validAdminOIDCRedirectURL(value string) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.String() != value || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != adminOIDCCallbackPath || parsed.Host == "" {
		return false
	}

	hostname := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "https" {
		return hostname != ""
	}
	if parsed.Scheme != "http" {
		return false
	}

	return hostname == "localhost" ||
		net.ParseIP(hostname) != nil && net.ParseIP(hostname).IsLoopback()
}

// adminOIDCTransaction is the confidential, short-lived browser transaction.
// It is encrypted and authenticated before it enters the cookie; the browser
// never receives the client secret or cookie key.
type adminOIDCTransaction struct {
	Version      int    `json:"v"`
	State        string `json:"s"`
	Nonce        string `json:"n"`
	CodeVerifier string `json:"p"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
}

// adminOIDCTransactionSealer owns AES-256-GCM and its nonce source. A fresh GCM
// nonce is mandatory for every cookie, even when a test or caller creates more
// than one authorization transaction in the same instant.
type adminOIDCTransactionSealer struct {
	aead    cipher.AEAD
	entropy io.Reader
}

// newAdminOIDCTransactionSealer constructs the authenticated-encryption
// boundary from a validated deployment key and a cryptographic random source.
func newAdminOIDCTransactionSealer(
	key [adminOIDCCookieKeyBytes]byte,
	entropy io.Reader,
) (*adminOIDCTransactionSealer, error) {
	if entropy == nil {
		return nil, errAdminOIDCDependencyRequired
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, errAdminOIDCConfigurationInvalid
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errAdminOIDCConfigurationInvalid
	}

	return &adminOIDCTransactionSealer{
		aead:    aead,
		entropy: entropy,
	}, nil
}

// seal serializes only the closed transaction shape and authenticates its
// purpose as additional data. A ciphertext copied to another cookie family or
// future protocol version therefore cannot be opened successfully.
func (sealer *adminOIDCTransactionSealer) seal(
	transaction adminOIDCTransaction,
) (string, error) {
	if sealer == nil || sealer.aead == nil || sealer.entropy == nil ||
		!validAdminOIDCTransactionShape(transaction) {
		return "", errAdminOIDCTransactionInvalid
	}
	plaintext, err := json.Marshal(transaction)
	if err != nil {
		return "", errAdminOIDCTransactionInvalid
	}

	nonce := make([]byte, sealer.aead.NonceSize())
	if _, err := io.ReadFull(sealer.entropy, nonce); err != nil {
		return "", errAdminOIDCTransactionInvalid
	}
	sealed := sealer.aead.Seal(
		append([]byte(nil), nonce...),
		nonce,
		plaintext,
		[]byte(adminOIDCTransactionAAD),
	)

	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// open authenticates, strictly decodes, and time-validates one transaction.
// All malformed, tampered, expired, or future-issued values share one error.
func (sealer *adminOIDCTransactionSealer) open(
	encoded string,
	now time.Time,
) (adminOIDCTransaction, error) {
	if sealer == nil || sealer.aead == nil || encoded == "" ||
		len(encoded) > adminOIDCMaximumCookieBytes || now.IsZero() {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(sealed) != encoded ||
		len(sealed) <= sealer.aead.NonceSize()+sealer.aead.Overhead() {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}
	nonce := sealed[:sealer.aead.NonceSize()]
	ciphertext := sealed[sealer.aead.NonceSize():]
	plaintext, err := sealer.aead.Open(
		nil,
		nonce,
		ciphertext,
		[]byte(adminOIDCTransactionAAD),
	)
	if err != nil {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}

	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var transaction adminOIDCTransaction
	if err := decoder.Decode(&transaction); err != nil {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}
	if !validAdminOIDCTransactionShape(transaction) {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}

	issuedAt := time.Unix(transaction.IssuedAt, 0).UTC()
	expiresAt := time.Unix(transaction.ExpiresAt, 0).UTC()
	now = now.UTC()
	if issuedAt.After(now.Add(adminOIDCClockSkew)) || !expiresAt.After(now) ||
		!expiresAt.After(issuedAt) ||
		expiresAt.Sub(issuedAt) > adminOIDCTransactionLifetime {
		return adminOIDCTransaction{}, errAdminOIDCTransactionInvalid
	}

	return transaction, nil
}

// validAdminOIDCTransactionShape verifies the exact random-value spellings and
// fixed protocol version independently from authenticated cookie transport.
func validAdminOIDCTransactionShape(transaction adminOIDCTransaction) bool {
	return transaction.Version == adminOIDCTransactionVersion &&
		validAdminOIDCRandomValue(transaction.State) &&
		validAdminOIDCRandomValue(transaction.Nonce) &&
		validAdminOIDCRandomValue(transaction.CodeVerifier) &&
		transaction.IssuedAt > 0 && transaction.ExpiresAt > 0
}

// validAdminOIDCRandomValue accepts only the canonical unpadded base64url form
// emitted from 256 random bits. This also satisfies PKCE's verifier grammar.
func validAdminOIDCRandomValue(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)

	return err == nil && len(decoded) == adminOIDCRandomBytes &&
		base64.RawURLEncoding.EncodeToString(decoded) == value
}

// generateAdminOIDCRandomValue creates one independent state, nonce, or PKCE
// verifier without retaining the raw random bytes after encoding.
func generateAdminOIDCRandomValue(entropy io.Reader) (string, error) {
	if entropy == nil {
		return "", errAdminOIDCDependencyRequired
	}
	raw := make([]byte, adminOIDCRandomBytes)
	if _, err := io.ReadFull(entropy, raw); err != nil {
		return "", errAdminOIDCTransactionInvalid
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// adminOIDCAuthorization contains exactly what an HTTP integration needs to
// redirect the browser and write the short-lived transaction cookie.
type adminOIDCAuthorization struct {
	URL         string
	CookieValue string
	ExpiresAt   time.Time
}

// newAdminOIDCAuthorization creates independent state, nonce, and PKCE values,
// seals them for the initiating browser, and builds Google's authorization URL.
func newAdminOIDCAuthorization(
	config adminOIDCConfig,
	sealer *adminOIDCTransactionSealer,
	entropy io.Reader,
	now time.Time,
) (adminOIDCAuthorization, error) {
	if !config.valid() || sealer == nil || entropy == nil || now.IsZero() {
		return adminOIDCAuthorization{}, errAdminOIDCDependencyRequired
	}
	state, err := generateAdminOIDCRandomValue(entropy)
	if err != nil {
		return adminOIDCAuthorization{}, err
	}
	nonce, err := generateAdminOIDCRandomValue(entropy)
	if err != nil {
		return adminOIDCAuthorization{}, err
	}
	verifier, err := generateAdminOIDCRandomValue(entropy)
	if err != nil {
		return adminOIDCAuthorization{}, err
	}

	issuedAt := now.UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(adminOIDCTransactionLifetime)
	cookieValue, err := sealer.seal(adminOIDCTransaction{
		Version:      adminOIDCTransactionVersion,
		State:        state,
		Nonce:        nonce,
		CodeVerifier: verifier,
		IssuedAt:     issuedAt.Unix(),
		ExpiresAt:    expiresAt.Unix(),
	})
	if err != nil {
		return adminOIDCAuthorization{}, err
	}

	challengeBytes := sha256.Sum256([]byte(verifier))
	parameters := url.Values{
		"client_id":             {config.clientID},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challengeBytes[:])},
		"code_challenge_method": {"S256"},
		"nonce":                 {nonce},
		"prompt":                {"select_account"},
		"redirect_uri":          {config.redirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid email"},
		"state":                 {state},
	}

	return adminOIDCAuthorization{
		URL: adminOIDCGoogleAuthorizationEndpoint + "?" +
			parameters.Encode(),
		CookieValue: cookieValue,
		ExpiresAt:   expiresAt,
	}, nil
}

// setAdminOIDCTransactionCookie binds the sealed transaction to the initiating
// browser. SameSite=Lax is required because Google's callback is a cross-site
// top-level navigation; HttpOnly and the callback-only path reduce exposure.
func setAdminOIDCTransactionCookie(
	w http.ResponseWriter,
	r *http.Request,
	authorization adminOIDCAuthorization,
) {
	if w == nil || r == nil || authorization.CookieValue == "" ||
		authorization.ExpiresAt.IsZero() {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminOIDCTransactionCookieName,
		Value:    authorization.CookieValue,
		Path:     adminOIDCCallbackPath,
		Expires:  authorization.ExpiresAt,
		MaxAge:   int(adminOIDCTransactionLifetime / time.Second),
		HttpOnly: true,
		Secure:   requestUsesSecureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// clearAdminOIDCTransactionCookie removes the one-use browser transaction with
// the same path, transport policy, and SameSite mode used when it was issued.
func clearAdminOIDCTransactionCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil || r == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminOIDCTransactionCookieName,
		Value:    "",
		Path:     adminOIDCCallbackPath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesSecureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// valid reports whether a directly constructed config still satisfies the
// same non-secret invariants enforced by loadAdminOIDCConfig.
func (config adminOIDCConfig) valid() bool {
	if !validAdminOIDCText(
		config.clientID,
		1,
		adminOIDCMaximumClientIDRunes,
	) || !validAdminOIDCText(
		config.clientSecret,
		8,
		adminOIDCMaximumSecretRunes,
	) || !validAdminOIDCRedirectURL(config.redirectURL) {
		return false
	}
	var zero [adminOIDCCookieKeyBytes]byte

	return subtle.ConstantTimeCompare(config.cookieKey[:], zero[:]) == 0
}

// adminOIDCCodeExchangeRequest gives an adapter only the callback values it
// needs. The adapter should be initialized from adminOIDCConfig so the client
// secret does not travel through every call or error path.
type adminOIDCCodeExchangeRequest struct {
	Code         string
	CodeVerifier string
	RedirectURL  string
}

// adminOIDCTokenSet deliberately retains only the raw ID token. Login-only OIDC
// must not persist or expose Google access or refresh tokens.
type adminOIDCTokenSet struct {
	RawIDToken string
}

// adminOIDCTokenExchanger is the network seam for an OAuth2 implementation.
// Tests replace it with an in-memory recorder and never contact Google.
type adminOIDCTokenExchanger interface {
	Exchange(
		context.Context,
		adminOIDCCodeExchangeRequest,
	) (adminOIDCTokenSet, error)
}

// adminOIDCIDTokenClaims are the claims independently checked by orchestration
// after a cryptographic verifier has validated the signed ID token.
type adminOIDCIDTokenClaims struct {
	Issuer          string
	Subject         string
	Audience        []string
	AuthorizedParty string
	Nonce           string
	Email           string
	EmailVerified   bool
	IssuedAt        time.Time
	ExpiresAt       time.Time
}

// adminOIDCIDTokenVerifier abstracts signature/JWKS and standard token parsing.
// An adapter should pin Google's issuer and client ID and may return
// errAdminOIDCProviderUnavailable for a transient discovery/JWKS failure.
type adminOIDCIDTokenVerifier interface {
	Verify(context.Context, string) (adminOIDCIDTokenClaims, error)
}

// adminOIDCVerifiedIdentity contains only the stable external key and verified
// email assertion needed to resolve an existing local administrator.
type adminOIDCVerifiedIdentity struct {
	Issuer  string
	Subject string
	Email   string
}

// adminOIDCAdmin is the password-free local projection returned only after a
// repository has found an existing active administrator.
type adminOIDCAdmin struct {
	UserID int64
	Email  string
	Role   adminRole
}

// adminOIDCAdminResolver is the authorization boundary. Implementations must
// never create a user or role here: they may resolve/link only an already
// existing active administrator and must key established links by issuer+sub.
type adminOIDCAdminResolver interface {
	ResolveExistingActiveAdmin(
		context.Context,
		adminOIDCVerifiedIdentity,
	) (adminOIDCAdmin, error)
}

// adminOIDCExistingAdminResolver adapts the password-free account reader and
// durable issuer/subject links added by the alternative-authentication schema.
// A first successful Google assertion may link only an already existing active
// administrator with the same verified email; it can never create a user or
// choose a role. Later sign-ins trust the immutable linked subject rather than
// requiring Google's mutable email claim to remain equal to the local address.
type adminOIDCExistingAdminResolver struct {
	accounts   adminAccountReader
	identities adminOIDCIdentityRepository
}

// newAdminOIDCExistingAdminResolver fails closed when either password-free
// persistence boundary is absent.
func newAdminOIDCExistingAdminResolver(
	accounts adminAccountReader,
	identities adminOIDCIdentityRepository,
) (*adminOIDCExistingAdminResolver, error) {
	if accounts == nil || identities == nil {
		return nil, errAdminOIDCDependencyRequired
	}

	return &adminOIDCExistingAdminResolver{
		accounts:   accounts,
		identities: identities,
	}, nil
}

// ResolveExistingActiveAdmin first honors an established issuer/subject link.
// On first use it links by verified normalized email, resolving unique-key races
// by rereading the requested immutable identity instead of replacing a link.
func (resolver *adminOIDCExistingAdminResolver) ResolveExistingActiveAdmin(
	ctx context.Context,
	identity adminOIDCVerifiedIdentity,
) (adminOIDCAdmin, error) {
	if resolver == nil || resolver.accounts == nil || resolver.identities == nil ||
		ctx == nil || identity.Issuer != adminOIDCGoogleIssuer ||
		!validAdminOIDCSubject(identity.Subject) {
		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}
	normalizedEmail, err := normalizeAdminEmail(identity.Email)
	if err != nil || normalizedEmail != identity.Email {
		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}

	account, err := resolver.identities.FindActiveAccountByIdentity(
		ctx,
		identity.Issuer,
		identity.Subject,
	)
	if err == nil {
		return resolver.recordAuthentication(ctx, identity, account)
	}
	if !errors.Is(err, errAdminOIDCIdentityNotFound) {
		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}

	account, err = resolver.accounts.FindActiveAccountByEmail(ctx, identity.Email)
	if err != nil {
		if errors.Is(err, errAdminUserNotFound) ||
			errors.Is(err, errAdminUserInvalid) {
			return adminOIDCAdmin{}, errAdminOIDCAdminNotAuthorized
		}

		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}
	err = resolver.identities.LinkIdentity(ctx, adminOIDCIdentity{
		Issuer:  identity.Issuer,
		Subject: identity.Subject,
		UserID:  account.ID,
	})
	if err != nil {
		if !errors.Is(err, errAdminOIDCIdentityConflict) {
			if errors.Is(err, errAdminUserNotFound) {
				return adminOIDCAdmin{}, errAdminOIDCAdminNotAuthorized
			}

			return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
		}

		// A concurrent first login may have completed the same link. Only the
		// exact requested issuer/subject is accepted after the conflict.
		linkedAccount, findErr := resolver.identities.FindActiveAccountByIdentity(
			ctx,
			identity.Issuer,
			identity.Subject,
		)
		if findErr != nil || linkedAccount.ID != account.ID {
			return adminOIDCAdmin{}, errAdminOIDCAdminNotAuthorized
		}
		account = linkedAccount
	}

	return resolver.recordAuthentication(ctx, identity, account)
}

// recordAuthentication updates only the non-secret last-used timestamp and
// converts the established local account to the callback service projection.
func (resolver *adminOIDCExistingAdminResolver) recordAuthentication(
	ctx context.Context,
	identity adminOIDCVerifiedIdentity,
	account adminAccount,
) (adminOIDCAdmin, error) {
	if !isValidAdminAccount(account) {
		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}
	if err := resolver.identities.RecordAuthentication(
		ctx,
		identity.Issuer,
		identity.Subject,
	); err != nil {
		if errors.Is(err, errAdminOIDCIdentityNotFound) {
			return adminOIDCAdmin{}, errAdminOIDCAdminNotAuthorized
		}

		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}

	return adminOIDCAdmin{
		UserID: account.ID,
		Email:  account.Email,
		Role:   account.Role,
	}, nil
}

// adminOIDCCallbackInput is assembled by the future HTTP handler from exactly
// one code, state, and callback-path cookie.
type adminOIDCCallbackInput struct {
	Code              string
	State             string
	TransactionCookie string
}

// adminOIDCCallbackService coordinates browser binding, PKCE exchange, signed
// token verification, claim validation, and existing-admin resolution. Session
// creation intentionally remains in the established admin session boundary.
type adminOIDCCallbackService struct {
	config     adminOIDCConfig
	sealer     *adminOIDCTransactionSealer
	exchanger  adminOIDCTokenExchanger
	verifier   adminOIDCIDTokenVerifier
	adminUsers adminOIDCAdminResolver
}

// newAdminOIDCCallbackService rejects an incomplete integration before an HTTP
// route can accept provider callbacks.
func newAdminOIDCCallbackService(
	config adminOIDCConfig,
	sealer *adminOIDCTransactionSealer,
	exchanger adminOIDCTokenExchanger,
	verifier adminOIDCIDTokenVerifier,
	adminUsers adminOIDCAdminResolver,
) (*adminOIDCCallbackService, error) {
	if !config.valid() || sealer == nil || exchanger == nil || verifier == nil ||
		adminUsers == nil {
		return nil, errAdminOIDCDependencyRequired
	}

	return &adminOIDCCallbackService{
		config:     config,
		sealer:     sealer,
		exchanger:  exchanger,
		verifier:   verifier,
		adminUsers: adminUsers,
	}, nil
}

// Authenticate returns a password-free local identity only after every OIDC
// and local-authorization check succeeds. Dependency text is never wrapped.
func (service *adminOIDCCallbackService) Authenticate(
	ctx context.Context,
	input adminOIDCCallbackInput,
	now time.Time,
) (adminOIDCAdmin, error) {
	if service == nil || ctx == nil || now.IsZero() ||
		!validAdminOIDCAuthorizationCode(input.Code) ||
		!validAdminOIDCRandomValue(input.State) ||
		input.TransactionCookie == "" {
		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}
	transaction, err := service.sealer.open(input.TransactionCookie, now)
	if err != nil || !constantTimeAdminOIDCValueEqual(
		transaction.State,
		input.State,
	) {
		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}

	tokenSet, err := service.exchanger.Exchange(
		ctx,
		adminOIDCCodeExchangeRequest{
			Code:         input.Code,
			CodeVerifier: transaction.CodeVerifier,
			RedirectURL:  service.config.redirectURL,
		},
	)
	if err != nil {
		if errors.Is(err, errAdminOIDCProviderUnavailable) {
			return adminOIDCAdmin{}, errAdminOIDCProviderUnavailable
		}

		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}
	if tokenSet.RawIDToken == "" {
		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}

	claims, err := service.verifier.Verify(ctx, tokenSet.RawIDToken)
	if err != nil {
		if errors.Is(err, errAdminOIDCProviderUnavailable) {
			return adminOIDCAdmin{}, errAdminOIDCProviderUnavailable
		}

		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}
	identity, valid := service.validIdentity(claims, transaction.Nonce, now)
	if !valid {
		return adminOIDCAdmin{}, errAdminOIDCCallbackInvalid
	}

	admin, err := service.adminUsers.ResolveExistingActiveAdmin(ctx, identity)
	if err != nil {
		if errors.Is(err, errAdminOIDCAdminNotAuthorized) {
			return adminOIDCAdmin{}, errAdminOIDCAdminNotAuthorized
		}

		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}
	if !validAdminOIDCAdmin(admin) {
		return adminOIDCAdmin{}, errAdminOIDCAdminLookupFailed
	}

	return admin, nil
}

// validIdentity applies defense-in-depth checks even when the verifier already
// validates standard claims. In particular, it requires Google's exact issuer,
// this deployment's audience, browser nonce, and a verified normalized email.
func (service *adminOIDCCallbackService) validIdentity(
	claims adminOIDCIDTokenClaims,
	expectedNonce string,
	now time.Time,
) (adminOIDCVerifiedIdentity, bool) {
	if claims.Issuer != adminOIDCGoogleIssuer ||
		!validAdminOIDCSubject(claims.Subject) ||
		!claims.EmailVerified ||
		!validAdminOIDCRandomValue(claims.Nonce) ||
		!constantTimeAdminOIDCValueEqual(claims.Nonce, expectedNonce) ||
		claims.IssuedAt.IsZero() || claims.ExpiresAt.IsZero() ||
		claims.IssuedAt.After(now.UTC().Add(adminOIDCClockSkew)) ||
		!claims.ExpiresAt.After(now.UTC()) ||
		!adminOIDCAudienceAllowsClient(
			claims.Audience,
			claims.AuthorizedParty,
			service.config.clientID,
		) {
		return adminOIDCVerifiedIdentity{}, false
	}

	normalizedEmail, err := normalizeAdminEmail(claims.Email)
	if err != nil {
		return adminOIDCVerifiedIdentity{}, false
	}

	return adminOIDCVerifiedIdentity{
		Issuer:  claims.Issuer,
		Subject: claims.Subject,
		Email:   normalizedEmail,
	}, true
}

// adminOIDCAudienceAllowsClient implements OIDC's azp rule: a multi-audience
// token must identify this client as the authorized party; any supplied azp on
// a single-audience token must also match exactly.
func adminOIDCAudienceAllowsClient(
	audience []string,
	authorizedParty string,
	clientID string,
) bool {
	if len(audience) == 0 || clientID == "" {
		return false
	}
	matches := 0
	for _, candidate := range audience {
		if candidate == clientID {
			matches++
		}
	}
	if matches != 1 {
		return false
	}
	if len(audience) > 1 {
		return authorizedParty == clientID
	}

	return authorizedParty == "" || authorizedParty == clientID
}

// validAdminOIDCSubject accepts Google's opaque, case-sensitive stable subject
// without assuming that it is numeric or treating it as a display value.
func validAdminOIDCSubject(subject string) bool {
	if !utf8.ValidString(subject) || subject == "" ||
		subject != strings.TrimSpace(subject) ||
		utf8.RuneCountInString(subject) > adminOIDCMaximumSubjectRunes {
		return false
	}
	for _, character := range subject {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

// validAdminOIDCAuthorizationCode rejects empty, padded, oversized, malformed,
// or control-bearing callback values before they reach a network adapter.
func validAdminOIDCAuthorizationCode(code string) bool {
	if code == "" || len(code) > adminOIDCMaximumCodeBytes ||
		!utf8.ValidString(code) || code != strings.TrimSpace(code) {
		return false
	}
	for _, character := range code {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

// constantTimeAdminOIDCValueEqual compares equal-length encoded random values
// without branching on attacker-controlled content.
func constantTimeAdminOIDCValueEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// validAdminOIDCAdmin validates the narrow local projection without accepting a
// malformed role, ID, or noncanonical email from a repository implementation.
func validAdminOIDCAdmin(admin adminOIDCAdmin) bool {
	if admin.UserID <= 0 || !admin.Role.valid() {
		return false
	}
	normalized, err := normalizeAdminEmail(admin.Email)

	return err == nil && normalized == admin.Email
}
