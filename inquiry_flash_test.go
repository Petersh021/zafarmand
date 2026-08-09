package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deterministicInquirySuccessFlash creates a signer whose exact key remains
// stable across flash unit tests while each issued receipt keeps production
// nonce generation.
func deterministicInquirySuccessFlash(
	t *testing.T,
) *inquirySuccessFlash {
	t.Helper()

	flash, err := newInquirySuccessFlashWithSigningKey(
		bytes.Repeat([]byte{0x41}, inquirySuccessFlashKeyLength),
	)
	if err != nil {
		t.Fatalf("create deterministic inquiry flash: %v", err)
	}

	return flash
}

// inquirySuccessCookieFromRecorder locates the flash cookie produced by issue
// or consume and fails at the caller if the expected Set-Cookie header is absent.
func inquirySuccessCookieFromRecorder(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) *http.Cookie {
	t.Helper()

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == inquirySuccessFlashCookieName {
			return cookie
		}
	}

	t.Fatalf("response did not set cookie %q", inquirySuccessFlashCookieName)
	return nil
}

// TestInquirySuccessFlashConstruction verifies production randomness, strict
// key sizing, and defensive copying of a caller-provided key.
func TestInquirySuccessFlashConstruction(t *testing.T) {
	first, err := newInquirySuccessFlash()
	if err != nil {
		t.Fatalf("create first inquiry success flash: %v", err)
	}
	second, err := newInquirySuccessFlash()
	if err != nil {
		t.Fatalf("create second inquiry success flash: %v", err)
	}
	if first == nil || second == nil {
		t.Fatal("random flash construction returned nil")
	}
	if first.signingKey == second.signingKey {
		t.Error("separate flash signers unexpectedly share one random key")
	}

	for _, keyLength := range []int{0, inquirySuccessFlashKeyLength - 1,
		inquirySuccessFlashKeyLength + 1} {
		_, err := newInquirySuccessFlashWithSigningKey(
			make([]byte, keyLength),
		)
		if !errors.Is(err, errInquirySuccessFlashSigningKey) {
			t.Errorf(
				"key length %d error: got %v, want signing-key sentinel",
				keyLength,
				err,
			)
		}
	}

	sourceKey := bytes.Repeat(
		[]byte{0x33},
		inquirySuccessFlashKeyLength,
	)
	flash, err := newInquirySuccessFlashWithSigningKey(sourceKey)
	if err != nil {
		t.Fatalf("create copied-key flash: %v", err)
	}
	sourceKey[0] = 0
	if flash.signingKey[0] != 0x33 {
		t.Error("flash retains mutable caller-owned signing-key storage")
	}
}

// TestInquirySuccessFlashIssueAndConsume verifies the complete plain-HTTP
// receipt lifecycle, including cookie policy, signed shape, success, deletion,
// and absence of visitor-identifying text.
func TestInquirySuccessFlashIssueAndConsume(t *testing.T) {
	flash := deterministicInquirySuccessFlash(t)
	issueRecorder := httptest.NewRecorder()
	issueRequest := httptest.NewRequest(http.MethodPost, "/contact", nil)

	if err := flash.issue(
		issueRecorder,
		issueRequest,
	); err != nil {
		t.Fatalf("issue success flash: %v", err)
	}
	cookie := inquirySuccessCookieFromRecorder(t, issueRecorder)
	if cookie.Path != "/contact" || !cookie.HttpOnly || cookie.Secure ||
		cookie.SameSite != http.SameSiteLaxMode ||
		cookie.MaxAge != inquirySuccessFlashMaxAge {
		t.Errorf("issued cookie policy: got %#v", cookie)
	}
	if cookie.Expires.IsZero() {
		t.Error("issued cookie lacks a bounded expiry")
	}
	if strings.Contains(cookie.Value, "visitor") ||
		strings.Contains(cookie.Value, "example.com") {
		t.Error("issued receipt contains visitor-identifying text")
	}

	parts := strings.Split(cookie.Value, inquirySuccessFlashSeparator)
	if len(parts) != 2 {
		t.Fatalf("issued cookie part count: got %d, want 2", len(parts))
	}
	decodedNonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(decodedNonce) != inquirySuccessFlashNonceLength {
		t.Error("issued cookie does not carry one server-generated receipt nonce")
	}
	decodedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil ||
		len(decodedSignature) != inquirySuccessFlashSignatureLength {
		t.Error("issued cookie does not carry one SHA-256 signature")
	}

	consumeRecorder := httptest.NewRecorder()
	consumeRequest := httptest.NewRequest(http.MethodGet, "/contact", nil)
	consumeRequest.AddCookie(cookie)
	if !flash.consume(consumeRecorder, consumeRequest) {
		t.Error("valid signed receipt was not accepted")
	}
	deletedCookie := inquirySuccessCookieFromRecorder(t, consumeRecorder)
	if deletedCookie.MaxAge != -1 || deletedCookie.Value != "" ||
		deletedCookie.Path != "/contact" || !deletedCookie.HttpOnly {
		t.Errorf("consumed cookie deletion policy: got %#v", deletedCookie)
	}

	// A browser applies the deletion before a refresh, so the next request has no
	// receipt and cannot repeat the one-time confirmation.
	refreshRecorder := httptest.NewRecorder()
	refreshRequest := httptest.NewRequest(http.MethodGet, "/contact", nil)
	if flash.consume(refreshRecorder, refreshRequest) {
		t.Error("request without a receipt reports success")
	}
	if len(refreshRecorder.Result().Cookies()) != 0 {
		t.Error("absent receipt emits an unnecessary deletion cookie")
	}
}

// TestInquirySuccessFlashRejectsInvalidReceipts proves malformed, tampered, and
// differently signed values never create a saved claim and are always deleted.
func TestInquirySuccessFlashRejectsInvalidReceipts(t *testing.T) {
	flash := deterministicInquirySuccessFlash(t)
	validRecorder := httptest.NewRecorder()
	validRequest := httptest.NewRequest(http.MethodPost, "/contact", nil)
	if err := flash.issue(validRecorder, validRequest); err != nil {
		t.Fatalf("issue valid fixture: %v", err)
	}
	validCookie := inquirySuccessCookieFromRecorder(t, validRecorder)

	otherFlash, err := newInquirySuccessFlashWithSigningKey(
		bytes.Repeat([]byte{0x72}, inquirySuccessFlashKeyLength),
	)
	if err != nil {
		t.Fatalf("create alternate signer: %v", err)
	}
	otherRecorder := httptest.NewRecorder()
	if err := otherFlash.issue(
		otherRecorder,
		validRequest,
	); err != nil {
		t.Fatalf("issue alternate receipt: %v", err)
	}
	otherCookie := inquirySuccessCookieFromRecorder(t, otherRecorder)

	parts := strings.Split(validCookie.Value, inquirySuccessFlashSeparator)
	tamperedSignature := parts[1]
	if strings.HasSuffix(tamperedSignature, "A") {
		tamperedSignature = tamperedSignature[:len(tamperedSignature)-1] + "B"
	} else {
		tamperedSignature = tamperedSignature[:len(tamperedSignature)-1] + "A"
	}

	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "one part", value: parts[0]},
		{name: "extra part", value: validCookie.Value + ".extra"},
		{name: "padded nonce", value: parts[0] + "=." + parts[1]},
		{name: "short nonce", value: "QQ." + parts[1]},
		{name: "tampered signature", value: parts[0] + "." + tamperedSignature},
		{name: "different signer", value: otherCookie.Value},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/contact", nil)
			request.AddCookie(&http.Cookie{
				Name:  inquirySuccessFlashCookieName,
				Value: test.value,
			})

			if flash.consume(recorder, request) {
				t.Error("invalid receipt reports success")
			}
			if deletion := inquirySuccessCookieFromRecorder(
				t,
				recorder,
			); deletion.MaxAge != -1 {
				t.Error("invalid receipt was not deleted")
			}
		})
	}
}

// TestInquirySuccessFlashTLSAndIssueBoundaries verifies Secure follows the
// direct request transport and invalid dependencies fail with safe sentinels.
func TestInquirySuccessFlashTLSAndIssueBoundaries(t *testing.T) {
	flash := deterministicInquirySuccessFlash(t)
	secureRecorder := httptest.NewRecorder()
	secureRequest := httptest.NewRequest(http.MethodPost, "/contact", nil)
	secureRequest.TLS = &tls.ConnectionState{}
	if err := flash.issue(
		secureRecorder,
		secureRequest,
	); err != nil {
		t.Fatalf("issue TLS receipt: %v", err)
	}
	if cookie := inquirySuccessCookieFromRecorder(
		t,
		secureRecorder,
	); !cookie.Secure {
		t.Error("direct TLS request does not set Secure")
	}

	if err := (*inquirySuccessFlash)(nil).issue(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/contact", nil),
	); !errors.Is(err, errInquirySuccessFlashSigningKey) {
		t.Errorf("nil flash issue error: got %v", err)
	}
	if err := flash.issue(nil, nil); !errors.Is(
		err,
		errInquirySuccessFlashResponse,
	) {
		t.Errorf("nil HTTP dependencies error: got %v", err)
	}
	if (*inquirySuccessFlash)(nil).consume(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/contact", nil),
	) {
		t.Error("nil flash consumes a receipt")
	}
}
