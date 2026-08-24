package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	// inquiryRequestBodyLimit bounds URL-encoded Contact requests before Go
	// allocates or parses visitor-controlled form values.
	inquiryRequestBodyLimit int64 = 64 << 10
	// inquiryNameMaxLength is measured in Unicode code points, matching what a
	// visitor understands as entered characters more closely than byte length.
	inquiryNameMaxLength = 100
	// inquiryEmailMaxLength provides a practical upper boundary before the
	// normalized address is checked by net/mail.
	inquiryEmailMaxLength = 254
	// inquiryMessageMaxLength keeps the inquiry concise and bounds
	// the amount of personal text reflected into a validation response.
	inquiryMessageMaxLength = 3000
	// inquiryCSRFTokenByteLength gives every token 256 bits of randomness.
	inquiryCSRFTokenByteLength = 32
	// inquirySubmissionTokenByteLength gives every per-form idempotency key 256
	// bits of randomness, independently from the reusable CSRF token.
	inquirySubmissionTokenByteLength = 32
	// inquiryCSRFCookieName is deliberately scoped to the Contact route and does
	// not represent an authenticated user session.
	inquiryCSRFCookieName = "zafarmand_inquiry_csrf"
	// inquiryCSRFFieldName is the hidden POST field paired with the protected
	// cookie by the double-submit comparison.
	inquiryCSRFFieldName = "csrf_token"
	// inquirySubmissionFieldName names the hidden per-form value that becomes the
	// database submission_key only after strict decoding and form validation.
	inquirySubmissionFieldName = "submission_token"
)

// inquirySubmissionState identifies the mutually exclusive response rendered
// with the Contact form.
//
// The zero value is the ordinary initial or validation state. Named non-zero
// values make it impossible for one call to request contradictory outcomes.
type inquirySubmissionState uint8

const (
	// inquirySubmissionStateForm renders no repository outcome message.
	inquirySubmissionStateForm inquirySubmissionState = iota
	// inquirySubmissionStateSucceeded renders the one-time persisted confirmation.
	inquirySubmissionStateSucceeded
	// inquirySubmissionStateFailed renders a safe, retryable persistence failure.
	inquirySubmissionStateFailed
	// inquirySubmissionStateConflict renders a safe permanent-key collision and
	// supplies a new token for a genuinely new submission attempt.
	inquirySubmissionStateConflict
)

// inquiryDisciplineOptions returns a new ordered option slice for each Contact
// page render.
//
// Returning a fresh value avoids package-level mutable state. The exact machine
// values also form the server-side whitelist used during validation.
func inquiryDisciplineOptions() []inquiryDisciplineOptionData {
	return []inquiryDisciplineOptionData{
		{
			Value: "interior-design",
			Label: "Interior Design",
		},
		{
			Value: "architecture-design",
			Label: "Architecture Design",
		},
		{
			Value: "products",
			Label: "Products",
		},
	}
}

// normalizeInquiryForm reads only POST-body values and trims surrounding
// whitespace from every visitor-editable field.
//
// Using r.PostForm at the call site, rather than the merged r.Form collection,
// prevents query-string values from overriding or supplementing the form body.
func normalizeInquiryForm(postForm url.Values) inquiryFormData {
	return inquiryFormData{
		Name: strings.TrimSpace(
			postForm.Get("name"),
		),
		Email: strings.TrimSpace(
			postForm.Get("email"),
		),
		Discipline: strings.TrimSpace(
			postForm.Get("discipline"),
		),
		Message: strings.TrimSpace(
			postForm.Get("message"),
		),
	}
}

// inquiryFormHasDuplicateValues reports whether any known field occurs more
// than once in the URL-encoded POST body.
//
// Duplicate security or identity values are rejected as malformed requests
// instead of silently choosing whichever value happens to appear first.
func inquiryFormHasDuplicateValues(postForm url.Values) bool {
	fieldNames := [...]string{
		inquiryCSRFFieldName,
		inquirySubmissionFieldName,
		"name",
		"email",
		"discipline",
		"message",
	}

	for _, fieldName := range fieldNames {
		if len(postForm[fieldName]) > 1 {
			return true
		}
	}

	return false
}

// validateInquiryForm applies the complete Stage 12 server-side validation
// contract to normalized form values.
//
// Browser attributes improve interaction but cannot be trusted as a security
// boundary, so every required value, length, address, and whitelist condition
// is checked again by Go.
func validateInquiryForm(form inquiryFormData) inquiryFormErrors {
	var formErrors inquiryFormErrors

	if !utf8.ValidString(form.Name) {
		formErrors.Name = "Name contains invalid text encoding."
	} else if strings.ContainsRune(form.Name, '\x00') {
		// PostgreSQL text values cannot contain U+0000. Reject it as visitor input
		// instead of allowing a predictable database error to become a false 503.
		formErrors.Name = "Name contains an unsupported character."
	} else if form.Name == "" {
		formErrors.Name = "Enter your name."
	} else if utf8.RuneCountInString(
		form.Name,
	) > inquiryNameMaxLength {
		formErrors.Name = "Name must be 100 characters or fewer."
	}

	if !utf8.ValidString(form.Email) {
		formErrors.Email = "Email contains invalid text encoding."
	} else if strings.ContainsRune(form.Email, '\x00') {
		// Keep the address boundary explicit even though net/mail also rejects NUL;
		// validation must stay aligned with PostgreSQL before repository access.
		formErrors.Email = "Email contains an unsupported character."
	} else if form.Email == "" {
		formErrors.Email = "Enter your email address."
	} else if utf8.RuneCountInString(
		form.Email,
	) > inquiryEmailMaxLength || !isExactInquiryEmail(form.Email) {
		formErrors.Email = "Enter one valid email address."
	}

	if _, exists := inquiryDisciplineLabel(
		form.Discipline,
	); !exists {
		formErrors.Discipline = "Choose a design discipline."
	}

	if !utf8.ValidString(form.Message) {
		formErrors.Message = "Message contains invalid text encoding."
	} else if strings.ContainsRune(form.Message, '\x00') {
		// PostgreSQL rejects NUL in text columns, so the form reports an actionable
		// field error without attempting a persistence call.
		formErrors.Message = "Message contains an unsupported character."
	} else if form.Message == "" {
		formErrors.Message = "Enter an inquiry message."
	} else if utf8.RuneCountInString(
		form.Message,
	) > inquiryMessageMaxLength {
		formErrors.Message = "Message must be 3000 characters or fewer."
	}

	return formErrors
}

// isExactInquiryEmail accepts one plain mailbox address and rejects display
// names or other syntax that net/mail might otherwise parse successfully.
func isExactInquiryEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	if err != nil {
		return false
	}

	return address.Address == value
}

// inquiryDisciplineLabel resolves one submitted machine value to server-owned
// visible copy. Unknown values never become trusted interface text.
func inquiryDisciplineLabel(value string) (string, bool) {
	for _, option := range inquiryDisciplineOptions() {
		if option.Value == value {
			return option.Label, true
		}
	}

	return "", false
}

// hasInquiryFormErrors reports whether at least one field failed validation.
// Keeping this decision in Go lets the template use one simple boolean when it
// decides whether to emit its accessible error summary.
func hasInquiryFormErrors(formErrors inquiryFormErrors) bool {
	return formErrors.Name != "" ||
		formErrors.Email != "" ||
		formErrors.Discipline != "" ||
		formErrors.Message != ""
}

// newContactPageData builds the complete view model shared by the initial form,
// validation response, persistence failure, key conflict, and redirected
// success states.
func newContactPageData(
	csrfToken string,
	submissionToken string,
	form inquiryFormData,
	formErrors inquiryFormErrors,
	submissionState inquirySubmissionState,
) *contactPageData {
	hasErrors := hasInquiryFormErrors(formErrors)
	// Field validation takes precedence over a repository outcome. This invariant
	// prevents a malformed future caller from rendering two mutually exclusive
	// regions with the same contact-form-response fragment ID.
	showSubmissionOutcome := !hasErrors

	return &contactPageData{
		// Eyebrow, Heading, Introduction, and optional direct information are
		// supplied from the mandatory managed Contact singleton immediately before
		// rendering. Keeping them out of this inquiry-state constructor prevents a
		// validation response from silently reverting to hard-coded public copy.
		AvailabilityNotice: "Submitting this form stores your inquiry for " +
			"studio review. It does not guarantee email delivery or a response " +
			"time.",
		CSRFToken:       csrfToken,
		SubmissionToken: submissionToken,
		Form:            form,
		Errors:          formErrors,
		HasErrors:       hasErrors,
		SubmissionSucceeded: submissionState ==
			inquirySubmissionStateSucceeded && showSubmissionOutcome,
		SubmissionFailed: submissionState ==
			inquirySubmissionStateFailed && showSubmissionOutcome,
		SubmissionConflict: submissionState ==
			inquirySubmissionStateConflict && showSubmissionOutcome,
		DisciplineOptions: inquiryDisciplineOptions(),
		NameMaxLength:     inquiryNameMaxLength,
		EmailMaxLength:    inquiryEmailMaxLength,
		MessageMaxLength:  inquiryMessageMaxLength,
	}
}

// ensureInquiryCSRFToken reuses a valid protected cookie or creates a new token
// for the Contact form.
//
// Reusing the same token keeps multiple open Contact tabs valid. An absent,
// malformed, or incorrectly sized cookie is replaced rather than reflected
// into HTML.
func ensureInquiryCSRFToken(
	w http.ResponseWriter,
	r *http.Request,
) (string, error) {
	cookie, err := r.Cookie(inquiryCSRFCookieName)
	if err == nil {
		if _, valid := decodeInquiryCSRFToken(cookie.Value); valid {
			// Cookie request headers do not carry their original Secure attribute.
			// Reissuing the same value on direct TLS or an operator-declared HTTPS
			// edge upgrades a development cookie without invalidating open forms.
			if requestUsesSecureCookies(r) {
				writeInquiryCSRFCookie(
					w,
					r,
					cookie.Value,
				)
			}

			return cookie.Value, nil
		}
	}

	token, err := newInquiryCSRFToken()
	if err != nil {
		return "", err
	}

	writeInquiryCSRFCookie(w, r, token)

	return token, nil
}

// writeInquiryCSRFCookie emits the protected half of the double-submit pair
// with one consistent scope and browser policy.
//
// Direct TLS and the explicit operational HTTPS marker receive Secure cookies.
// Untrusted forwarding headers never influence this browser security decision.
func writeInquiryCSRFCookie(
	w http.ResponseWriter,
	r *http.Request,
	token string,
) {
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     inquiryCSRFCookieName,
			Value:    token,
			Path:     "/contact",
			HttpOnly: true,
			Secure:   requestUsesSecureCookies(r),
			SameSite: http.SameSiteLaxMode,
		},
	)
}

// newInquiryCSRFToken creates a URL-safe representation of 32 bytes read from
// the operating system's cryptographically secure random source.
func newInquiryCSRFToken() (string, error) {
	randomBytes := make(
		[]byte,
		inquiryCSRFTokenByteLength,
	)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

// newInquirySubmissionToken creates the independent key for one rendered form.
//
// The Contact handler issues a new key for every fresh form render. Validation
// and database-failure responses preserve that key so a safe retry cannot
// create a duplicate row.
func newInquirySubmissionToken() (string, error) {
	randomBytes := make(
		[]byte,
		inquirySubmissionTokenByteLength,
	)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

// decodeInquiryCSRFToken verifies both base64url encoding and the exact token
// size before a value participates in a security comparison.
func decodeInquiryCSRFToken(value string) ([]byte, bool) {
	encoding := base64.RawURLEncoding.Strict()
	decoded, err := encoding.DecodeString(value)
	if err != nil || len(decoded) != inquiryCSRFTokenByteLength {
		return nil, false
	}
	if encoding.EncodeToString(decoded) != value {
		return nil, false
	}

	return decoded, true
}

// decodeInquirySubmissionToken accepts only an unpadded base64url value that
// represents the exact 32 bytes required by the database constraint.
//
// Returning decoded bytes keeps the repository input aligned with PostgreSQL's
// bytea column and prevents an alternate textual encoding from representing the
// same idempotency key.
func decodeInquirySubmissionToken(value string) ([]byte, bool) {
	encoding := base64.RawURLEncoding.Strict()
	decoded, err := encoding.DecodeString(value)
	if err != nil || len(decoded) != inquirySubmissionTokenByteLength {
		return nil, false
	}
	if encoding.EncodeToString(decoded) != value {
		return nil, false
	}

	return decoded, true
}

// validateInquiryCSRFToken performs the double-submit cookie check for a POST
// request and returns the trusted encoded token for a subsequent render.
//
// ConstantTimeCompare avoids content-dependent comparison timing once both
// values have passed the same exact encoding and length requirements.
func validateInquiryCSRFToken(
	r *http.Request,
	submittedToken string,
) (string, bool) {
	cookie, err := r.Cookie(inquiryCSRFCookieName)
	if err != nil {
		return "", false
	}

	cookieBytes, validCookie := decodeInquiryCSRFToken(cookie.Value)
	submittedBytes, validSubmitted := decodeInquiryCSRFToken(submittedToken)
	if !validCookie || !validSubmitted {
		return "", false
	}

	if subtle.ConstantTimeCompare(
		cookieBytes,
		submittedBytes,
	) != 1 {
		return "", false
	}

	return cookie.Value, true
}
