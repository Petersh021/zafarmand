package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	// inquirySuccessFlashKeyLength gives the process-local HMAC key 256 bits of
	// operating-system randomness.
	inquirySuccessFlashKeyLength = 32
	// inquirySuccessFlashSignatureLength is SHA-256's fixed output length.
	inquirySuccessFlashSignatureLength = sha256.Size
	// inquirySuccessFlashNonceLength gives each receipt an independent 256-bit
	// server-generated value that carries no form or database content.
	inquirySuccessFlashNonceLength = 32
	// inquirySuccessFlashCookieName identifies the short-lived Post/Redirect/Get
	// receipt without describing or containing a visitor identity.
	inquirySuccessFlashCookieName = "zafarmand_inquiry_saved"
	// inquirySuccessFlashMaxAge keeps the receipt only long enough for the
	// immediate redirect, while consume deletes it after the first GET.
	inquirySuccessFlashMaxAge = 120
	// inquirySuccessFlashSeparator divides the canonical base64url receipt nonce
	// from its signature. A dot cannot occur in either encoded component.
	inquirySuccessFlashSeparator = "."
	// inquirySuccessFlashPurpose binds signatures to this exact use so the same
	// key could never authenticate an unrelated future cookie format.
	inquirySuccessFlashPurpose = "zafarmand-inquiry-saved-v1:"
)

// Flash construction and issue errors are stable internal categories. They do
// not contain a submission key, visitor data, cookie value, or signing key.
var (
	// errInquirySuccessFlashSigningKey rejects a missing or incorrectly sized
	// process secret before it can produce unverifiable receipts.
	errInquirySuccessFlashSigningKey = errors.New(
		"create inquiry success flash: signing key must be 32 bytes",
	)
	// errInquirySuccessFlashRandomness reports that the process could not create
	// an independent receipt nonce. It exposes no operating-system detail.
	errInquirySuccessFlashRandomness = errors.New(
		"issue inquiry success flash: secure randomness unavailable",
	)
	// errInquirySuccessFlashResponse rejects missing HTTP dependencies without a
	// nil-pointer panic at an error boundary.
	errInquirySuccessFlashResponse = errors.New(
		"issue inquiry success flash: response and request are required",
	)
)

// inquirySuccessFlash signs and verifies the one-time browser receipt used by
// the Contact Post/Redirect/Get flow.
//
// The key is process-local: it is generated at startup, copied into a fixed
// array, never serialized, and never logged. Restarting the process invalidates
// an outstanding receipt, which safely withholds a success message rather than
// creating a false one.
type inquirySuccessFlash struct {
	signingKey [inquirySuccessFlashKeyLength]byte
}

// newInquirySuccessFlash creates a receipt signer from the operating system's
// cryptographically secure random source.
func newInquirySuccessFlash() (*inquirySuccessFlash, error) {
	key := make([]byte, inquirySuccessFlashKeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}

	return newInquirySuccessFlashWithSigningKey(key)
}

// newInquirySuccessFlashWithSigningKey validates and copies an explicit key.
//
// Production calls newInquirySuccessFlash. This constructor exists so unit
// tests can use deterministic signatures without replacing crypto/rand or
// exposing mutable key storage.
func newInquirySuccessFlashWithSigningKey(
	key []byte,
) (*inquirySuccessFlash, error) {
	if len(key) != inquirySuccessFlashKeyLength {
		return nil, errInquirySuccessFlashSigningKey
	}

	flash := &inquirySuccessFlash{}
	copy(flash.signingKey[:], key)

	return flash, nil
}

// issue writes the signed receipt only after the repository confirms a created
// row or idempotent replay.
//
// The value contains a new server-generated nonce and an HMAC signature. It
// never copies the untrusted hidden submission token, so even a crafted token
// containing reversible visitor data cannot enter the cookie. HttpOnly blocks
// page scripts, SameSite=Lax fits the same-site redirect, and direct TLS
// requests receive Secure.
func (flash *inquirySuccessFlash) issue(
	w http.ResponseWriter,
	r *http.Request,
) error {
	if w == nil || r == nil {
		return errInquirySuccessFlashResponse
	}
	if flash == nil {
		return errInquirySuccessFlashSigningKey
	}

	nonce := make([]byte, inquirySuccessFlashNonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return errInquirySuccessFlashRandomness
	}

	encodedNonce := base64.RawURLEncoding.EncodeToString(nonce)
	encodedSignature := base64.RawURLEncoding.EncodeToString(
		flash.signature(nonce),
	)

	http.SetCookie(
		w,
		&http.Cookie{
			Name: inquirySuccessFlashCookieName,
			Value: encodedNonce + inquirySuccessFlashSeparator +
				encodedSignature,
			Path:     "/contact",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   inquirySuccessFlashMaxAge,
			Expires: time.Now().UTC().Add(
				time.Duration(inquirySuccessFlashMaxAge) * time.Second,
			),
		},
	)

	return nil
}

// consume verifies and deletes a receipt from the redirected Contact GET.
//
// Any present cookie is deleted before validation finishes, so malformed or
// tampered values cannot be retried automatically. Returning only a boolean
// gives the template the minimum truth it needs and keeps the receipt nonce out
// of page data.
func (flash *inquirySuccessFlash) consume(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if flash == nil || w == nil || r == nil {
		return false
	}

	cookie, err := r.Cookie(inquirySuccessFlashCookieName)
	if err != nil {
		return false
	}

	deleteInquirySuccessFlashCookie(w, r)

	parts := strings.Split(cookie.Value, inquirySuccessFlashSeparator)
	if len(parts) != 2 {
		return false
	}

	nonce, validNonce := decodeCanonicalInquiryFlashPart(
		parts[0],
		inquirySuccessFlashNonceLength,
	)
	submittedSignature, validSignature := decodeCanonicalInquiryFlashPart(
		parts[1],
		inquirySuccessFlashSignatureLength,
	)
	if !validNonce || !validSignature {
		return false
	}

	expectedSignature := flash.signature(nonce)
	return subtle.ConstantTimeCompare(
		submittedSignature,
		expectedSignature,
	) == 1
}

// signature authenticates a purpose prefix and server-generated receipt nonce
// with HMAC-SHA256. The prefix prevents cross-protocol reuse if another signed
// value is introduced later.
func (flash *inquirySuccessFlash) signature(
	nonce []byte,
) []byte {
	digest := hmac.New(sha256.New, flash.signingKey[:])
	_, _ = digest.Write([]byte(inquirySuccessFlashPurpose))
	_, _ = digest.Write(nonce)

	return digest.Sum(nil)
}

// decodeCanonicalInquiryFlashPart accepts one strict, unpadded base64url part
// with the expected decoded length.
//
// Re-encoding rejects alternative textual representations, leaving one cookie
// value for each byte sequence before signature comparison.
func decodeCanonicalInquiryFlashPart(
	value string,
	expectedLength int,
) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != expectedLength {
		return nil, false
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, false
	}

	return decoded, true
}

// deleteInquirySuccessFlashCookie expires the receipt on the same path and with
// the same browser security attributes used when it was issued.
func deleteInquirySuccessFlashCookie(
	w http.ResponseWriter,
	r *http.Request,
) {
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     inquirySuccessFlashCookieName,
			Value:    "",
			Path:     "/contact",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
			Expires:  time.Unix(1, 0).UTC(),
		},
	)
}
