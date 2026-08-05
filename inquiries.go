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
	// inquiryMessageMaxLength keeps the structural preview concise and bounds
	// the amount of personal text reflected into a validation response.
	inquiryMessageMaxLength = 3000
	// inquiryCSRFTokenByteLength gives every token 256 bits of randomness.
	inquiryCSRFTokenByteLength = 32
	// inquiryCSRFCookieName is deliberately scoped to the Contact route and does
	// not represent an authenticated user session.
	inquiryCSRFCookieName = "zafarmand_inquiry_csrf"
	// inquiryCSRFFieldName is the hidden POST field paired with the protected
	// cookie by the double-submit comparison.
	inquiryCSRFFieldName = "csrf_token"
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
	} else if form.Name == "" {
		formErrors.Name = "Enter your name."
	} else if utf8.RuneCountInString(
		form.Name,
	) > inquiryNameMaxLength {
		formErrors.Name = "Name must be 100 characters or fewer."
	}

	if !utf8.ValidString(form.Email) {
		formErrors.Email = "Email contains invalid text encoding."
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
// visible copy. Unknown values never become labels in the preview.
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

// newInquiryPreview maps a validated form to the narrow display-only shape used
// by the confirmation preview.
//
// The returned value contains no delivery status, identifier, timestamp, or
// persistence metadata because Stage 12 performs none of those actions.
func newInquiryPreview(form inquiryFormData) inquiryPreviewData {
	disciplineLabel, _ := inquiryDisciplineLabel(form.Discipline)

	return inquiryPreviewData{
		Name:            form.Name,
		Email:           form.Email,
		DisciplineLabel: disciplineLabel,
		Message:         form.Message,
	}
}

// newContactPageData builds the complete view model shared by the initial form,
// validation response, and valid non-persistent preview states.
func newContactPageData(
	csrfToken string,
	form inquiryFormData,
	formErrors inquiryFormErrors,
	preview *inquiryPreviewData,
) *contactPageData {
	return &contactPageData{
		Eyebrow:      "Contact",
		Heading:      "Prepare your inquiry",
		Introduction: "Choose a discipline and prepare the context for a future conversation with Zafarmand.",
		AvailabilityNotice: "The server processes this information only " +
			"to create the preview response. It is not delivered to the " +
			"studio or saved.",
		CSRFToken:         csrfToken,
		Form:              form,
		Errors:            formErrors,
		HasErrors:         hasInquiryFormErrors(formErrors),
		Preview:           preview,
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
			// Reissuing the same value on a direct TLS request upgrades an HTTP
			// development cookie without invalidating forms open in other tabs.
			if r.TLS != nil {
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
// Direct TLS requests receive Secure cookies. Reverse-proxy header trust is a
// deployment concern and remains intentionally outside this local HTTP stage.
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
			Secure:   r.TLS != nil,
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

// decodeInquiryCSRFToken verifies both base64url encoding and the exact token
// size before a value participates in a security comparison.
func decodeInquiryCSRFToken(value string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != inquiryCSRFTokenByteLength {
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
