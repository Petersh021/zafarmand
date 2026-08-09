package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// errAdminInquiryStatusUpdaterRequired prevents the application from starting
// without the persistence boundary used by the private status mutation route.
// The message is safe for startup logs because it contains no database or
// visitor-provided value.
var errAdminInquiryStatusUpdaterRequired = errors.New(
	"create application: admin inquiry status updater is required",
)

// adminInquiryStatusUpdateHandler applies one administrator-selected review
// state and redirects to the canonical detail page. Route composition must put
// requireAdmin and the explicit owner/editor allowlist outside this handler;
// the context check below keeps the mutation fail-closed if that wiring ever
// regresses.
func (app *application) adminInquiryStatusUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	// The mutation has one fixed destination, so it accepts no query-carried
	// status, return URL, or alternate spelling. ForceQuery distinguishes a bare
	// trailing question mark from a request with no query at all.
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid inquiry status request", http.StatusBadRequest)

		return
	}

	inquiryID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	detailPath := adminInquiryNavigationPath + "/" +
		strconv.FormatInt(inquiryID, 10)
	statusPath := detailPath + "/status"
	if r.URL.EscapedPath() != statusPath {
		// ServeMux matches after unescaping path segments. Rechecking EscapedPath
		// prevents an encoded digit or another equivalent spelling from becoming
		// a second mutation address for the same protected record.
		http.NotFound(w, r)

		return
	}

	// Native browser forms do not apply a content coding. Rejecting every coded
	// body avoids treating compressed or otherwise transformed bytes as the
	// small URL-encoded control form expected by parseStrictAdminForm.
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(
			w,
			"content encoding is not supported",
			http.StatusUnsupportedMediaType,
		)

		return
	}

	form, parsed := parseStrictAdminForm(
		w,
		r,
		[]string{"csrf_token", "status"},
	)
	if !parsed {
		return
	}

	// CSRF is checked before interpreting the desired status or querying the
	// inquiry. A forged request therefore cannot learn whether an ID exists and
	// cannot cause any persistence call.
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	status := inquiryStatus(form.Get("status"))
	if !status.valid() {
		// Status values are machine identifiers: case folding or trimming would
		// silently accept a spelling the form and database do not define.
		http.Error(w, "invalid inquiry status", http.StatusBadRequest)

		return
	}

	if app == nil || app.adminInquiryStatuses == nil {
		// Construction rejects this state. Retaining a runtime guard avoids a nil
		// interface panic if a future test or route bypasses normal construction.
		log.Printf("admin inquiry status updater unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	err := app.adminInquiryStatuses.UpdateStatus(ctx, inquiryID, status)
	cancel()
	if err != nil {
		if errors.Is(err, errAdminInquiryNotFound) {
			http.NotFound(w, r)

			return
		}

		// Do not log the inquiry ID, desired state, administrator identity, driver
		// diagnostic, or visitor data. The fixed event remains useful without
		// widening private-data exposure through operational logs.
		log.Printf("admin inquiry status update failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	// See Other implements Post/Redirect/Get: refresh and ordinary Back/Forward
	// navigation operate on a GET detail page instead of resubmitting the POST.
	// The Location is derived only from the validated numeric path identity.
	http.Redirect(w, r, detailPath, http.StatusSeeOther)
}

// adminSessionCSRFTokenIsValid validates the session-bound hidden form token
// using the same canonical encoding and fixed decoded size as token issuance.
// Comparing decoded bytes in constant time avoids accepting alternate Base64
// spellings and centralizes the rule shared by authenticated mutations.
func adminSessionCSRFTokenIsValid(expected string, submitted string) bool {
	expectedBytes, _, expectedOK := decodeAndHashAdminToken(expected)
	submittedBytes, _, submittedOK := decodeAndHashAdminToken(submitted)

	return expectedOK && submittedOK && subtle.ConstantTimeCompare(
		expectedBytes,
		submittedBytes,
	) == 1
}
