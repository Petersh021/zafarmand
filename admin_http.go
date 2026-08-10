package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
)

// Administrator HTTP constants keep cookie scope, token size, request limits,
// and session lifetime visible at one security boundary.
const (
	// adminLoginCSRFTokenCookieName protects the anonymous login form from a
	// cross-site request that could sign a browser into an attacker's account.
	adminLoginCSRFTokenCookieName = "zafarmand_admin_login_csrf"
	// adminSessionCookieName carries the raw bearer token. PostgreSQL receives
	// only its SHA-256 digest.
	adminSessionCookieName = "zafarmand_admin_session"
	// adminSessionCSRFTokenCookieName carries a second random secret bound to the
	// persisted session digest and copied into authenticated POST forms.
	adminSessionCSRFTokenCookieName = "zafarmand_admin_csrf"
	// adminTokenBytes gives every independently generated secret 256 bits of
	// cryptographic entropy before URL-safe encoding.
	adminTokenBytes = 32
	// adminMaximumFormBytes bounds parsing work for every small private form.
	adminMaximumFormBytes = 16 * 1024
	// adminLoginCSRFLifetime limits how long an unused anonymous form stays valid.
	adminLoginCSRFLifetime = 10 * time.Minute
	// adminSessionLifetime is an absolute lifetime; requests never slide it
	// forward, so every browser session eventually requires a fresh login.
	adminSessionLifetime = 8 * time.Hour
	// adminRepositoryTimeout prevents an unavailable database from holding a
	// private authentication or inquiry request indefinitely.
	adminRepositoryTimeout = 5 * time.Second
	// adminDummyPassword is not an account credential. It exists only to produce
	// one startup-validated verifier for account-neutral missing-user logins.
	adminDummyPassword = "zafarmand dummy administrator credential"
)

// Stable HTTP-layer errors describe only local dependency failures. Random
// tokens, passwords, email addresses, and database details never enter them.
var (
	// errAdminRepositoryRequired prevents routes from being built without the
	// persistence boundary needed to authenticate and revoke sessions.
	errAdminRepositoryRequired = errors.New(
		"create application: admin repository is required",
	)
	// errAdminPasswordManagerRequired prevents login from silently bypassing
	// password verification.
	errAdminPasswordManagerRequired = errors.New(
		"create application: admin password manager is required",
	)
	// errAdminDummyPasswordHashFailed reports startup failure without exposing
	// the fixed dummy input or any derived credential text.
	errAdminDummyPasswordHashFailed = errors.New(
		"create application: dummy admin password hash failed",
	)
)

// adminPageData is the typed presentation contract shared by every private
// template. Route-specific pointers remain optional so templates receive no
// repository record, password hash, or unrelated personal data.
type adminPageData struct {
	// Title supplies the page-specific part of the private document title.
	Title string
	// LoginCSRFToken is rendered only into the anonymous login form.
	LoginCSRFToken string
	// Email restores the normalized login value after a generic failure.
	Email string
	// AuthenticationFailed selects the single account-neutral error message.
	AuthenticationFailed bool
	// NavigationPath selects one trusted active item in the shared admin nav.
	NavigationPath string
	// Identity contains the minimal authenticated display fields, or nil on login.
	Identity *adminIdentityPageData
	// SessionCSRFToken is rendered only into authenticated mutation forms.
	SessionCSRFToken string
	// InquiryList contains only the read-only inbox presentation contract.
	InquiryList *adminInquiryListPageData
	// InquiryDetail contains one protected inquiry presentation contract.
	InquiryDetail *adminInquiryDetailPageData
	// ProductList contains the protected all-status Product catalogue contract.
	ProductList *adminProductListPageData
	// ProductDetail contains one protected read-only Product contract.
	ProductDetail *adminProductDetailPageData
}

// adminIdentityPageData keeps persistence and authorization records out of the
// template while retaining the two values needed by the shared private shell.
type adminIdentityPageData struct {
	// Email is the normalized address of the authenticated administrator.
	Email string
	// RoleLabel is the trusted human-readable owner/editor label.
	RoleLabel string
}

// authenticatedAdminRequest is stored in a request context only after both
// browser secrets match one active, unexpired database session.
type authenticatedAdminRequest struct {
	// Identity is the repository's validated user/session result.
	Identity adminIdentity
	// CSRFToken is the raw second secret needed by authenticated POST forms.
	CSRFToken string
}

// authenticatedAdminContextKey is an unexported distinct type, preventing an
// unrelated package value from colliding with the authentication context entry.
type authenticatedAdminContextKey struct{}

// adminLoginPageHandler creates a fresh anonymous CSRF pair and renders the
// login form. It never queries administrator identities or exposes whether an
// email exists.
func (app *application) adminLoginPageHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	csrfToken, err := app.issueAdminLoginCSRFToken(w, r)
	if err != nil {
		log.Printf("could not create admin login CSRF token")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	app.renderAdmin(
		w,
		http.StatusOK,
		"login.html",
		adminPageData{
			Title:          "Sign in",
			LoginCSRFToken: csrfToken,
		},
	)
}

// adminLoginHandler validates one strict form and always performs a password
// comparison for a syntactically valid request, even when no account exists.
// Successful authentication rotates to new independent session and CSRF tokens.
func (app *application) adminLoginHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	form, ok := parseStrictAdminForm(
		w,
		r,
		[]string{"csrf_token", "email", "password"},
	)
	if !ok {
		return
	}

	if !adminLoginCSRFTokenIsValid(r, form.Get("csrf_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	normalizedEmail, emailErr := normalizeAdminEmail(form.Get("email"))
	password := form.Get("password")

	// The dummy verifier makes a missing account follow the same intentionally
	// expensive password path as an existing account. The response remains one
	// generic failure for missing, inactive, malformed, or incorrect credentials.
	passwordHash := app.adminDummyPasswordHash
	var user adminUser
	userFound := false
	if emailErr == nil {
		ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
		user, emailErr = app.admins.FindActiveUserByEmail(ctx, normalizedEmail)
		cancel()

		switch {
		case emailErr == nil:
			passwordHash = user.PasswordHash
			userFound = true
		case errors.Is(emailErr, errAdminUserNotFound),
			errors.Is(emailErr, errAdminUserInvalid):
			// Continue with the dummy hash and the generic authentication result.
		default:
			// Repository implementations own detailed diagnostics. The HTTP layer
			// never logs an injected error that could repeat an address or query.
			log.Printf("admin login user lookup failed")
			http.Error(
				w,
				"service temporarily unavailable",
				http.StatusServiceUnavailable,
			)

			return
		}
	}

	passwordMatches, verifyErr := app.adminPasswords.Verify(
		password,
		passwordHash,
	)
	if verifyErr != nil {
		// Invalid visitor input and a corrupt stored encoding receive the same
		// external result. The safe sentinel is useful in local logs without an
		// address, password, or encoded verifier.
		log.Printf("admin login password verification failed")
	}

	if !userFound || verifyErr != nil || !passwordMatches {
		app.renderAdminLoginFailure(w, r, normalizedEmail)

		return
	}

	sessionToken, sessionHash, err := generateAdminToken(app.adminEntropy)
	if err != nil {
		log.Printf("could not create admin session token")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}
	csrfToken, csrfHash, err := generateAdminToken(app.adminEntropy)
	if err != nil {
		log.Printf("could not create admin session CSRF token")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	expiresAt := app.now().UTC().Add(adminSessionLifetime)
	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	err = app.admins.CreateSession(
		ctx,
		adminSession{
			TokenHash:     sessionHash,
			UserID:        user.ID,
			CSRFTokenHash: csrfHash,
			ExpiresAt:     expiresAt,
		},
	)
	cancel()
	if err != nil {
		log.Printf("admin login session creation failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	setAdminSessionCookies(
		w,
		r,
		sessionToken,
		csrfToken,
		expiresAt,
	)
	clearAdminLoginCSRFCookie(w, r)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// renderAdminLoginFailure rotates the anonymous CSRF token and renders the one
// generic 401 response. A failed value is restored only when normalization was
// safe; html/template still performs contextual escaping.
func (app *application) renderAdminLoginFailure(
	w http.ResponseWriter,
	r *http.Request,
	email string,
) {
	csrfToken, err := app.issueAdminLoginCSRFToken(w, r)
	if err != nil {
		log.Printf("could not rotate admin login CSRF token")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	app.renderAdmin(
		w,
		http.StatusUnauthorized,
		"login.html",
		adminPageData{
			Title:                "Sign in",
			LoginCSRFToken:       csrfToken,
			Email:                email,
			AuthenticationFailed: true,
		},
	)
}

// requireAdmin authenticates both browser-held tokens before calling a private
// handler. Unknown, expired, revoked, inactive, or incomplete sessions share a
// cookie-clearing redirect and disclose no account state.
func (app *application) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, sessionErr := r.Cookie(adminSessionCookieName)
		csrfCookie, csrfErr := r.Cookie(adminSessionCSRFTokenCookieName)
		if sessionErr != nil || csrfErr != nil {
			clearAdminSessionCookies(w, r)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)

			return
		}

		_, sessionHash, sessionOK := decodeAndHashAdminToken(
			sessionCookie.Value,
		)
		csrfTokenBytes, csrfHash, csrfOK := decodeAndHashAdminToken(
			csrfCookie.Value,
		)
		if !sessionOK || !csrfOK {
			clearAdminSessionCookies(w, r)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)

			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
		identity, err := app.admins.FindIdentityBySessionHash(ctx, sessionHash)
		cancel()
		if err != nil {
			if errors.Is(err, errAdminSessionNotFound) ||
				errors.Is(err, errAdminSessionInvalid) {
				clearAdminSessionCookies(w, r)
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)

				return
			}

			log.Printf("admin session lookup failed")
			http.Error(
				w,
				"service temporarily unavailable",
				http.StatusServiceUnavailable,
			)

			return
		}

		hashesMatch := subtle.ConstantTimeCompare(
			identity.SessionTokenHash,
			sessionHash,
		) == 1 && subtle.ConstantTimeCompare(
			identity.CSRFTokenHash,
			csrfHash,
		) == 1
		if !hashesMatch || !app.now().UTC().Before(identity.SessionExpiresAt) {
			app.revokeAdminSession(r.Context(), sessionHash)
			clearAdminSessionCookies(w, r)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)

			return
		}

		requestIdentity := authenticatedAdminRequest{
			Identity:  identity,
			CSRFToken: base64.RawURLEncoding.EncodeToString(csrfTokenBytes),
		}
		requestContext := context.WithValue(
			r.Context(),
			authenticatedAdminContextKey{},
			requestIdentity,
		)

		next.ServeHTTP(w, r.WithContext(requestContext))
	})
}

// requireAdminRoles adds an explicit authorization decision after session
// authentication. Its allowlist is built once when routes are composed; an
// empty or invalid set therefore denies every request rather than falling back
// to whichever roles happen to exist in the database.
func requireAdminRoles(
	roles ...adminRole,
) func(http.Handler) http.Handler {
	allowed := make(map[adminRole]struct{}, len(roles))
	for _, role := range roles {
		if role.valid() {
			allowed[role] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestIdentity, ok := authenticatedAdminFromContext(r.Context())
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)

				return
			}
			if _, permitted := allowed[requestIdentity.Identity.Role]; !permitted {
				http.Error(w, "forbidden", http.StatusForbidden)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// adminDashboardHandler renders the authenticated overview. It links to the
// Product and inquiry workflows without querying either table merely to show
// counts or other dashboard metrics.
func (app *application) adminDashboardHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Dashboard",
		"/admin",
		requestIdentity,
	)

	app.renderAdmin(
		w,
		http.StatusOK,
		"dashboard.html",
		data,
	)
}

// newAuthenticatedAdminPageData constructs the shared identity, navigation,
// and logout contract after requireAdmin has validated both browser secrets.
// Route handlers add only their own optional page payload afterward.
func newAuthenticatedAdminPageData(
	title string,
	navigationPath string,
	requestIdentity authenticatedAdminRequest,
) adminPageData {
	return adminPageData{
		Title:          title,
		NavigationPath: navigationPath,
		Identity: &adminIdentityPageData{
			Email:     requestIdentity.Identity.Email,
			RoleLabel: adminRoleLabel(requestIdentity.Identity.Role),
		},
		SessionCSRFToken: requestIdentity.CSRFToken,
	}
}

// adminLogoutHandler accepts only the session-bound hidden token, revokes the
// matching database row, clears both browser secrets, and redirects with 303.
func (app *application) adminLogoutHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	form, parsed := parseStrictAdminForm(
		w,
		r,
		[]string{"csrf_token"},
	)
	if !parsed {
		return
	}

	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	err := app.admins.DeleteSession(
		ctx,
		requestIdentity.Identity.SessionTokenHash,
	)
	cancel()
	if err != nil && !errors.Is(err, errAdminSessionNotFound) {
		log.Printf("admin logout session revocation failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	clearAdminSessionCookies(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// authenticatedAdminFromContext returns the value installed by requireAdmin.
// The boolean lets a handler fail closed if it is accidentally registered
// without the middleware.
func authenticatedAdminFromContext(
	ctx context.Context,
) (authenticatedAdminRequest, bool) {
	identity, ok := ctx.Value(
		authenticatedAdminContextKey{},
	).(authenticatedAdminRequest)

	return identity, ok
}

// issueAdminLoginCSRFToken generates the anonymous double-submit value and
// stores its browser copy with the narrowest path used by the login endpoint.
func (app *application) issueAdminLoginCSRFToken(
	w http.ResponseWriter,
	r *http.Request,
) (string, error) {
	token, _, err := generateAdminToken(app.adminEntropy)
	if err != nil {
		return "", err
	}

	expiresAt := app.now().UTC().Add(adminLoginCSRFLifetime)
	http.SetCookie(w, &http.Cookie{
		Name:     adminLoginCSRFTokenCookieName,
		Value:    token,
		Path:     "/admin/login",
		Expires:  expiresAt,
		MaxAge:   int(adminLoginCSRFLifetime / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	return token, nil
}

// adminLoginCSRFTokenIsValid compares canonical decoded bytes rather than
// attacker-controlled strings and requires the protected browser copy.
func adminLoginCSRFTokenIsValid(
	r *http.Request,
	submitted string,
) bool {
	cookie, err := r.Cookie(adminLoginCSRFTokenCookieName)
	if err != nil {
		return false
	}

	submittedBytes, _, submittedOK := decodeAndHashAdminToken(submitted)
	cookieBytes, _, cookieOK := decodeAndHashAdminToken(cookie.Value)

	return submittedOK && cookieOK && subtle.ConstantTimeCompare(
		submittedBytes,
		cookieBytes,
	) == 1
}

// generateAdminToken returns the raw URL-safe browser value and a separate
// SHA-256 slice suitable for persistence. The raw bytes remain local.
func generateAdminToken(
	entropy io.Reader,
) (string, []byte, error) {
	if entropy == nil {
		return "", nil, errors.New("admin token entropy is unavailable")
	}

	raw := make([]byte, adminTokenBytes)
	if _, err := io.ReadFull(entropy, raw); err != nil {
		return "", nil, errors.New("generate admin token")
	}

	digest := sha256.Sum256(raw)

	return base64.RawURLEncoding.EncodeToString(raw), digest[:], nil
}

// decodeAndHashAdminToken accepts only the exact unpadded Base64 spelling and
// fixed decoded length emitted by generateAdminToken.
func decodeAndHashAdminToken(
	encoded string,
) ([]byte, []byte, bool) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != adminTokenBytes ||
		base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, nil, false
	}

	digest := sha256.Sum256(raw)

	return raw, digest[:], true
}

// setAdminSessionCookies emits the bearer and CSRF secrets with identical
// absolute expiry and admin-only scope. HttpOnly prevents script access, while
// the server can still copy the CSRF value into a protected form after lookup.
func setAdminSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
	sessionToken string,
	csrfToken string,
	expiresAt time.Time,
) {
	// MaxAge is a relative browser lifetime, while Expires is the matching
	// absolute server decision. Avoid time.Until here so tests and handlers use
	// the application's injected clock consistently.
	maxAge := int(adminSessionLifetime / time.Second)

	for _, cookie := range []*http.Cookie{
		{
			Name:  adminSessionCookieName,
			Value: sessionToken,
		},
		{
			Name:  adminSessionCSRFTokenCookieName,
			Value: csrfToken,
		},
	} {
		cookie.Path = "/admin"
		cookie.Expires = expiresAt
		cookie.MaxAge = maxAge
		cookie.HttpOnly = true
		cookie.Secure = r.TLS != nil
		cookie.SameSite = http.SameSiteStrictMode
		http.SetCookie(w, cookie)
	}
}

// clearAdminLoginCSRFCookie expires only the anonymous login value after a
// successful transition to authenticated session cookies.
func clearAdminLoginCSRFCookie(
	w http.ResponseWriter,
	r *http.Request,
) {
	expireAdminCookie(
		w,
		r,
		adminLoginCSRFTokenCookieName,
		"/admin/login",
	)
}

// clearAdminSessionCookies expires both secrets so neither half of a stale or
// revoked authentication pair remains in the browser.
func clearAdminSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
) {
	expireAdminCookie(w, r, adminSessionCookieName, "/admin")
	expireAdminCookie(w, r, adminSessionCSRFTokenCookieName, "/admin")
}

// expireAdminCookie reproduces the security attributes and original path while
// setting a past expiry and negative MaxAge, as required for reliable deletion.
func expireAdminCookie(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	path string,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// parseStrictAdminForm accepts only URL-encoded browser forms containing one
// value for each expected field and no extra keys.
func parseStrictAdminForm(
	w http.ResponseWriter,
	r *http.Request,
	expectedFields []string,
) (mapFormValues, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		http.Error(
			w,
			"content type must be application/x-www-form-urlencoded",
			http.StatusUnsupportedMediaType,
		)

		return nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, adminMaximumFormBytes)
	if err := r.ParseForm(); err != nil {
		status := http.StatusBadRequest
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid form submission", status)

		return nil, false
	}

	expected := make(map[string]struct{}, len(expectedFields))
	for _, field := range expectedFields {
		expected[field] = struct{}{}
		if len(r.PostForm[field]) != 1 {
			http.Error(w, "invalid form submission", http.StatusBadRequest)

			return nil, false
		}
	}
	if len(r.PostForm) != len(expected) {
		http.Error(w, "invalid form submission", http.StatusBadRequest)

		return nil, false
	}
	for field := range r.PostForm {
		if _, ok := expected[field]; !ok {
			http.Error(w, "invalid form submission", http.StatusBadRequest)

			return nil, false
		}
	}

	return mapFormValues(r.PostForm), true
}

// mapFormValues names the Get behavior needed from url.Values without making
// handler signatures expose a mutable request-owned map type.
type mapFormValues map[string][]string

// Get returns the first value for a key after parseStrictAdminForm has already
// proved that every accepted field occurs exactly once.
func (values mapFormValues) Get(key string) string {
	if len(values[key]) == 0 {
		return ""
	}

	return values[key][0]
}

// revokeAdminSession is a best-effort cleanup used only when the two browser
// secrets do not match the persisted pair. The response still fails closed if
// PostgreSQL is unavailable.
func (app *application) revokeAdminSession(
	parent context.Context,
	sessionHash []byte,
) {
	ctx, cancel := context.WithTimeout(parent, adminRepositoryTimeout)
	defer cancel()

	if err := app.admins.DeleteSession(ctx, sessionHash); err != nil &&
		!errors.Is(err, errAdminSessionNotFound) {
		log.Printf("invalid admin session revocation failed")
	}
}

// adminRoleLabel converts the closed internal role vocabulary into trusted
// interface text. An invalid role fails to a neutral label rather than gaining
// owner wording.
func adminRoleLabel(role adminRole) string {
	switch role {
	case adminRoleOwner:
		return "Owner"
	case adminRoleEditor:
		return "Editor"
	default:
		return "Unknown"
	}
}

// adminSecurityHeaders applies private-cache and browser hardening policy to
// every /admin response, including ServeMux-generated 404 and 405 responses.
func adminSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			headers := w.Header()
			headers.Set("Cache-Control", "no-store")
			headers.Set(
				"Content-Security-Policy",
				"default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
			)
			headers.Set("Cross-Origin-Opener-Policy", "same-origin")
			headers.Set("Cross-Origin-Resource-Policy", "same-origin")
			headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			headers.Set("Referrer-Policy", "no-referrer")
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "DENY")
			headers.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		}

		next.ServeHTTP(w, r)
	})
}
