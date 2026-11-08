package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// adminLoginPresentation centralizes the configuration flags and trusted panel
// name used by every anonymous authentication response.
func (app *application) adminLoginPresentation(method string) adminPageData {
	otpEnabled := app != nil && app.adminOTP != nil
	googleEnabled := app != nil && app.adminOIDC != nil
	switch method {
	case "otp", "google", "password":
	default:
		// The reference OTP instrument is the visual entrance in every
		// environment. Provider configuration controls whether its network
		// action can run; it must never silently replace the requested view.
		method = "otp"
	}

	return adminPageData{
		Title:         "Sign in",
		LoginMethod:   method,
		OTPEnabled:    otpEnabled,
		GoogleEnabled: googleEnabled,
	}
}

// adminOTPRequestHandler accepts an address but never reveals whether it maps
// to an active administrator. JavaScript receives a small fixed JSON shape;
// the ordinary form path renders the same optical-code panel directly.
func (app *application) adminOTPRequestHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	form, ok := parseStrictAdminForm(
		w,
		r,
		[]string{"csrf_token", "email"},
	)
	if !ok {
		return
	}
	if !adminLoginCSRFTokenIsValid(r, form.Get("csrf_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if app == nil || app.adminOTP == nil {
		http.Error(w, "sign in method is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !app.beginAdminLoginAlternativeWork() {
		rejectAdminLoginAlternativeWork(w)
		return
	}
	defer app.endAdminLoginAlternativeWork()

	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	existingChallengeToken := ""
	if cookie, cookieErr := r.Cookie(adminOTPChallengeCookieName); cookieErr == nil {
		existingChallengeToken = cookie.Value
	}
	result, err := app.adminOTP.Issue(
		ctx,
		form.Get("email"),
		existingChallengeToken,
	)
	cancel()
	if err != nil {
		log.Print("administrator OTP request failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	setAdminOTPChallengeCookie(w, r, result)

	if adminLoginWantsJSON(r) {
		writeAdminLoginJSON(w, http.StatusAccepted, map[string]any{
			"ok":             true,
			"expires_in":     int(adminOTPChallengeLifetime.Seconds()),
			"resend_after":   adminOTPResendDelaySeconds,
			"status_message": "If that address is authorized, a code has been sent.",
		})
		return
	}

	data := app.adminLoginPresentation("otp")
	data.LoginCSRFToken = form.Get("csrf_token")
	data.OTPRequested = true
	app.renderAdmin(w, http.StatusAccepted, "login.html", data)
}

// adminOTPVerifyHandler lets the repository distinguish operational failure
// internally while keeping all invalid, expired, exhausted, and unknown codes
// in one browser-visible authentication result.
func (app *application) adminOTPVerifyHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	form, ok := parseStrictAdminForm(
		w,
		r,
		[]string{"csrf_token", "otp_code"},
	)
	if !ok {
		return
	}
	if !adminLoginCSRFTokenIsValid(r, form.Get("csrf_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if app == nil || app.adminOTP == nil {
		http.Error(w, "sign in method is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !app.beginAdminLoginAlternativeWork() {
		rejectAdminLoginAlternativeWork(w)
		return
	}
	defer app.endAdminLoginAlternativeWork()

	challengeToken := ""
	if cookie, err := r.Cookie(adminOTPChallengeCookieName); err == nil {
		challengeToken = cookie.Value
	}
	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	account, err := app.adminOTP.Verify(
		ctx,
		challengeToken,
		form.Get("otp_code"),
	)
	cancel()
	if err != nil {
		if !errors.Is(err, errAdminEmailOTPAuthenticationFailed) &&
			!errors.Is(err, errAdminEmailOTPChallengeInvalid) {
			log.Print("administrator OTP verification repository failed")
			http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		if adminLoginWantsJSON(r) {
			writeAdminLoginJSON(w, http.StatusUnauthorized, map[string]any{
				"ok":             false,
				"status_message": "Verification code is invalid or expired.",
			})
			return
		}
		data := app.adminLoginPresentation("otp")
		data.LoginCSRFToken = form.Get("csrf_token")
		data.OTPRequested = true
		data.OTPAuthenticationFailed = true
		app.renderAdmin(w, http.StatusUnauthorized, "login.html", data)
		return
	}

	if err := app.issueAdminSession(
		w,
		r,
		account.ID,
		http.SameSiteStrictMode,
	); err != nil {
		log.Print("administrator OTP session creation failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	clearAdminOTPChallengeCookie(w, r)
	if adminLoginWantsJSON(r) {
		writeAdminLoginJSON(w, http.StatusOK, map[string]any{
			"ok":             true,
			"redirect":       "/admin",
			"status_message": "Access authorized.",
		})
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// adminGoogleLoginHandler starts the confidential authorization-code flow only
// after the same anonymous double-submit CSRF check used by the local methods.
func (app *application) adminGoogleLoginHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	form, ok := parseStrictAdminForm(w, r, []string{"csrf_token"})
	if !ok {
		return
	}
	if !adminLoginCSRFTokenIsValid(r, form.Get("csrf_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if app == nil || app.adminOIDC == nil || app.adminOIDC.sealer == nil ||
		app.now == nil {
		http.Error(w, "sign in method is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !app.beginAdminLoginAlternativeWork() {
		rejectAdminLoginAlternativeWork(w)
		return
	}
	defer app.endAdminLoginAlternativeWork()

	authorization, err := newAdminOIDCAuthorization(
		app.adminOIDC.config,
		app.adminOIDC.sealer,
		rand.Reader,
		app.now().UTC(),
	)
	if err != nil {
		log.Print("Google administrator authorization creation failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	setAdminOIDCTransactionCookie(w, r, authorization)
	http.Redirect(w, r, authorization.URL, http.StatusSeeOther)
}

// adminGoogleCallbackHandler clears the one-use browser transaction on every
// outcome, verifies the provider response, and creates only the existing local
// session type. Provider text is never reflected or logged.
func (app *application) adminGoogleCallbackHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	clearAdminOIDCTransactionCookie(w, r)
	if app == nil || app.adminOIDC == nil || app.adminOIDC.callback == nil ||
		app.now == nil {
		http.Error(w, "sign in method is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !app.beginAdminLoginAlternativeWork() {
		rejectAdminLoginAlternativeWork(w)
		return
	}
	defer app.endAdminLoginAlternativeWork()

	query := r.URL.Query()
	if query.Get("error") != "" || len(query["code"]) != 1 ||
		len(query["state"]) != 1 {
		app.renderAdminGoogleLoginFailure(w, r, http.StatusUnauthorized)
		return
	}
	transactionCookie, err := r.Cookie(adminOIDCTransactionCookieName)
	if err != nil {
		app.renderAdminGoogleLoginFailure(w, r, http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		adminAlternativeAuthNetworkTimeout,
	)
	admin, err := app.adminOIDC.callback.Authenticate(
		ctx,
		adminOIDCCallbackInput{
			Code:              query.Get("code"),
			State:             query.Get("state"),
			TransactionCookie: transactionCookie.Value,
		},
		app.now().UTC(),
	)
	cancel()
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, errAdminOIDCProviderUnavailable) ||
			errors.Is(err, errAdminOIDCAdminLookupFailed) {
			status = http.StatusServiceUnavailable
			log.Print("Google administrator callback dependency failed")
		}
		app.renderAdminGoogleLoginFailure(w, r, status)
		return
	}
	if err := app.issueAdminSession(
		w,
		r,
		admin.UserID,
		http.SameSiteLaxMode,
	); err != nil {
		log.Print("Google administrator session creation failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (app *application) renderAdminGoogleLoginFailure(
	w http.ResponseWriter,
	r *http.Request,
	status int,
) {
	csrfToken, err := app.issueAdminLoginCSRFToken(w, r)
	if err != nil {
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	data := app.adminLoginPresentation("google")
	data.LoginCSRFToken = csrfToken
	data.GoogleAuthenticationFailed = true
	app.renderAdmin(w, status, "login.html", data)
}

func adminLoginWantsJSON(r *http.Request) bool {
	return r != nil && strings.Contains(
		strings.ToLower(r.Header.Get("Accept")),
		"application/json",
	)
}

func writeAdminLoginJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Print("administrator login JSON response failed")
	}
}

func (app *application) beginAdminLoginAlternativeWork() bool {
	if app == nil || app.adminLoginAlternativeWork == nil {
		return false
	}
	select {
	case app.adminLoginAlternativeWork <- struct{}{}:
		return true
	default:
		return false
	}
}

func (app *application) endAdminLoginAlternativeWork() {
	<-app.adminLoginAlternativeWork
}

func rejectAdminLoginAlternativeWork(w http.ResponseWriter) {
	w.Header().Set(
		"Retry-After",
		strconv.Itoa(adminLoginRetryAfterSeconds),
	)
	http.Error(
		w,
		"sign in temporarily unavailable",
		http.StatusTooManyRequests,
	)
}
