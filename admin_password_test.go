package main

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// adminFailingEntropyReader simulates an operating-system entropy failure
// while carrying a detail that must never cross the password-manager boundary.
type adminFailingEntropyReader struct{}

// Read implements io.Reader and always returns a credential-like error.
func (adminFailingEntropyReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy failed near password=private-value")
}

// TestNewAdminPasswordManager verifies production parameters remain isolated
// from the low-cost constructor intended only for deterministic tests.
func TestNewAdminPasswordManager(t *testing.T) {
	manager, ok := newAdminPasswordManager().(*pbkdf2AdminPasswordManager)
	if !ok {
		t.Fatal("production constructor returned an unexpected implementation")
	}
	if manager.iterations != adminPasswordProductionIterations {
		t.Errorf(
			"production iterations: got %d, want %d",
			manager.iterations,
			adminPasswordProductionIterations,
		)
	}
	if manager.entropy == nil {
		t.Error("production manager has no cryptographic entropy source")
	}
}

// TestNewAdminPasswordManagerWithParameters verifies that invalid test seams
// fail clearly instead of creating a manager that panics during Hash.
func TestNewAdminPasswordManagerWithParameters(t *testing.T) {
	tests := []struct {
		name       string
		iterations int
		entropy    io.Reader
	}{
		{name: "zero iterations", entropy: bytes.NewReader(nil)},
		{name: "negative iterations", iterations: -1, entropy: bytes.NewReader(nil)},
		{name: "nil entropy", iterations: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := newAdminPasswordManagerWithParameters(
				test.iterations,
				test.entropy,
			)
			if !errors.Is(err, errAdminPasswordManagerParametersInvalid) {
				t.Fatalf("error: got %v, want parameter sentinel", err)
			}
			if manager != nil {
				t.Errorf("manager: got %#v, want nil", manager)
			}
		})
	}
}

// TestAdminPasswordHashAndVerify exercises the complete canonical encoding,
// randomized salt, correct-password, and wrong-password behavior at low cost.
func TestAdminPasswordHashAndVerify(t *testing.T) {
	const iterations = 3
	const password = "correct horse battery staple"

	// Two distinct 16-byte chunks let consecutive Hash calls prove that equal
	// passwords receive different salts and therefore different encoded values.
	entropy := append(
		bytes.Repeat([]byte{0x11}, adminPasswordSaltBytes),
		bytes.Repeat([]byte{0x22}, adminPasswordSaltBytes)...,
	)
	managerInterface, err := newAdminPasswordManagerWithParameters(
		iterations,
		bytes.NewReader(entropy),
	)
	if err != nil {
		t.Fatalf("create test manager: %v", err)
	}
	manager := managerInterface.(*pbkdf2AdminPasswordManager)

	first, err := manager.Hash(password)
	if err != nil {
		t.Fatalf("hash first password: %v", err)
	}
	second, err := manager.Hash(password)
	if err != nil {
		t.Fatalf("hash second password: %v", err)
	}
	if first == second {
		t.Error("equal passwords produced equal salted encodings")
	}

	firstSalt := bytes.Repeat([]byte{0x11}, adminPasswordSaltBytes)
	firstKey, err := pbkdf2.Key(
		sha256.New,
		password,
		firstSalt,
		iterations,
		adminPasswordDerivedKeyBytes,
	)
	if err != nil {
		t.Fatalf("derive independently expected key: %v", err)
	}
	expectedFirst := strings.Join(
		[]string{
			adminPasswordAlgorithm,
			adminPasswordEncodingVersion,
			"i=" + strconv.Itoa(iterations),
			base64.RawURLEncoding.EncodeToString(firstSalt),
			base64.RawURLEncoding.EncodeToString(firstKey),
		},
		"$",
	)
	if first != expectedFirst {
		t.Errorf("canonical encoding: got %q, want %q", first, expectedFirst)
	}

	verified, err := manager.Verify(password, first)
	if err != nil {
		t.Fatalf("verify correct password: %v", err)
	}
	if !verified {
		t.Error("correct password did not verify")
	}

	verified, err = manager.Verify("wrong password long enough", first)
	if err != nil {
		t.Fatalf("verify wrong password: %v", err)
	}
	if verified {
		t.Error("wrong password unexpectedly verified")
	}
}

// TestAdminPasswordPolicy verifies rune-based boundaries, invalid UTF-8, NUL
// rejection, and the deliberate preservation of surrounding whitespace.
func TestAdminPasswordPolicy(t *testing.T) {
	invalidUTF8 := string([]byte{0xff, 'a'})
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "fourteen runes", password: strings.Repeat("a", 14)},
		{name: "fifteen runes", password: strings.Repeat("a", 15), valid: true},
		{
			name:     "fifteen multibyte runes",
			password: strings.Repeat("界", 15),
			valid:    true,
		},
		{name: "one hundred twenty eight", password: strings.Repeat("a", 128), valid: true},
		{name: "one hundred twenty nine", password: strings.Repeat("a", 129)},
		{name: "invalid UTF-8", password: invalidUTF8},
		{name: "embedded NUL", password: "long-enough\x00password"},
		{
			name:     "surrounding spaces are significant",
			password: "  spaced password  ",
			valid:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isValidAdminPassword(test.password); got != test.valid {
				t.Errorf("valid: got %t, want %t", got, test.valid)
			}

			manager, err := newAdminPasswordManagerWithParameters(
				1,
				bytes.NewReader(bytes.Repeat(
					[]byte{0x31},
					adminPasswordSaltBytes,
				)),
			)
			if err != nil {
				t.Fatalf("create test manager: %v", err)
			}
			encoded, hashErr := manager.Hash(test.password)
			if !test.valid {
				if !errors.Is(hashErr, errAdminPasswordInvalid) {
					t.Fatalf("Hash error: got %v, want policy sentinel", hashErr)
				}
				if encoded != "" {
					t.Errorf("Hash result: got %q, want empty", encoded)
				}
				return
			}

			if hashErr != nil {
				t.Fatalf("Hash accepted policy value: %v", hashErr)
			}
			if utf8.RuneCountInString(test.password) < adminPasswordMinimumRunes {
				t.Fatal("test fixture does not satisfy the documented minimum")
			}
		})
	}
}

// TestAdminPasswordVerifyRejectsInvalidPlaintext confirms Verify applies the
// same complete-password policy as Hash before attempting derivation.
func TestAdminPasswordVerifyRejectsInvalidPlaintext(t *testing.T) {
	manager, err := newAdminPasswordManagerWithParameters(
		1,
		bytes.NewReader(bytes.Repeat([]byte{0x41}, adminPasswordSaltBytes)),
	)
	if err != nil {
		t.Fatalf("create test manager: %v", err)
	}

	verified, err := manager.Verify(
		"too short",
		"pbkdf2-sha256$v=1$i=1$ignored$ignored",
	)
	if !errors.Is(err, errAdminPasswordInvalid) {
		t.Fatalf("error: got %v, want policy sentinel", err)
	}
	if verified {
		t.Error("invalid plaintext unexpectedly verified")
	}
}

// TestAdminPasswordVerifyRejectsNonCanonicalEncodings proves stored hashes are
// never interpreted leniently or with a manager-selected work-factor change.
func TestAdminPasswordVerifyRejectsNonCanonicalEncodings(t *testing.T) {
	const password = "a sufficiently long password"
	const iterations = 2

	salt := bytes.Repeat([]byte{0x51}, adminPasswordSaltBytes)
	key := bytes.Repeat([]byte{0x61}, adminPasswordDerivedKeyBytes)
	saltText := base64.RawURLEncoding.EncodeToString(salt)
	keyText := base64.RawURLEncoding.EncodeToString(key)
	// Sixteen bytes leave four unused bits in the final Base64 character. R has
	// the same decoded data bits as canonical Q but non-zero padding bits, so the
	// strict decoder must reject this otherwise-decodable alternate spelling.
	if !strings.HasSuffix(saltText, "Q") {
		t.Fatalf("test salt does not provide the expected strict-Base64 fixture")
	}
	nonCanonicalSaltText := strings.TrimSuffix(saltText, "Q") + "R"
	canonical := strings.Join(
		[]string{
			adminPasswordAlgorithm,
			adminPasswordEncodingVersion,
			"i=2",
			saltText,
			keyText,
		},
		"$",
	)
	managerInterface, err := newAdminPasswordManagerWithParameters(
		iterations,
		bytes.NewReader(nil),
	)
	if err != nil {
		t.Fatalf("create test manager: %v", err)
	}

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "algorithm", encoded: strings.Replace(canonical, adminPasswordAlgorithm, "pbkdf2-sha1", 1)},
		{name: "version", encoded: strings.Replace(canonical, "v=1", "v=2", 1)},
		{name: "iteration label", encoded: strings.Replace(canonical, "i=2", "rounds=2", 1)},
		{name: "leading-zero iterations", encoded: strings.Replace(canonical, "i=2", "i=02", 1)},
		{name: "different iterations", encoded: strings.Replace(canonical, "i=2", "i=3", 1)},
		{name: "signed iterations", encoded: strings.Replace(canonical, "i=2", "i=+2", 1)},
		{name: "non-numeric iterations", encoded: strings.Replace(canonical, "i=2", "i=two", 1)},
		{name: "missing component", encoded: strings.TrimSuffix(canonical, "$"+keyText)},
		{name: "extra component", encoded: canonical + "$extra"},
		{name: "padded salt", encoded: strings.Replace(canonical, saltText, base64.URLEncoding.EncodeToString(salt), 1)},
		{name: "non-zero Base64 padding bits", encoded: strings.Replace(canonical, saltText, nonCanonicalSaltText, 1)},
		{name: "short salt", encoded: strings.Replace(canonical, saltText, base64.RawURLEncoding.EncodeToString(salt[:15]), 1)},
		{name: "short key", encoded: strings.Replace(canonical, keyText, base64.RawURLEncoding.EncodeToString(key[:31]), 1)},
		{name: "invalid salt alphabet", encoded: strings.Replace(canonical, saltText, "not+url/base64", 1)},
		{name: "NUL", encoded: canonical + "\x00"},
		{name: "invalid UTF-8", encoded: canonical + string([]byte{0xff})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verified, err := managerInterface.Verify(password, test.encoded)
			if !errors.Is(err, errAdminPasswordEncodingInvalid) {
				t.Fatalf("error: got %v, want encoding sentinel", err)
			}
			if verified {
				t.Error("non-canonical encoding unexpectedly verified")
			}
		})
	}
}

// TestAdminPasswordHashRedactsEntropyFailure verifies low-level error text and
// the plaintext password do not escape from a failed salt read.
func TestAdminPasswordHashRedactsEntropyFailure(t *testing.T) {
	const password = "private password value"
	manager, err := newAdminPasswordManagerWithParameters(
		1,
		adminFailingEntropyReader{},
	)
	if err != nil {
		t.Fatalf("create test manager: %v", err)
	}

	encoded, err := manager.Hash(password)
	if !errors.Is(err, errAdminPasswordHashFailed) {
		t.Fatalf("error: got %v, want generic hashing sentinel", err)
	}
	if encoded != "" {
		t.Errorf("encoded: got %q, want empty", encoded)
	}
	if strings.Contains(err.Error(), password) ||
		strings.Contains(err.Error(), "private-value") {
		t.Errorf("error exposes credential detail: %q", err)
	}
}

// TestNilAdminPasswordManager verifies a defensive nil receiver produces one
// safe error rather than panicking.
func TestNilAdminPasswordManager(t *testing.T) {
	var manager *pbkdf2AdminPasswordManager

	encoded, err := manager.Hash("a sufficiently long password")
	if !errors.Is(err, errAdminPasswordHashFailed) || encoded != "" {
		t.Errorf("Hash nil receiver: got %q, %v", encoded, err)
	}
	verified, err := manager.Verify(
		"a sufficiently long password",
		"ignored",
	)
	if !errors.Is(err, errAdminPasswordHashFailed) || verified {
		t.Errorf("Verify nil receiver: got %t, %v", verified, err)
	}
}
