package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Password-policy and encoding constants keep every administrator credential
// on one explicit, reviewable contract. The production iteration count follows
// the project's Stage 15 decision; tests inject a much smaller count so the
// suite remains fast without weakening hashes created by the real server.
const (
	// adminPasswordMinimumRunes prevents short passwords while counting Unicode
	// code points rather than UTF-8 bytes shown invisibly to an administrator.
	adminPasswordMinimumRunes = 15
	// adminPasswordMaximumRunes bounds request work and accepts long passphrases
	// without silently truncating them.
	adminPasswordMaximumRunes = 128
	// adminPasswordProductionIterations is the only work factor accepted by the
	// production password manager for this encoding version.
	adminPasswordProductionIterations = 600_000
	// adminPasswordSaltBytes gives every hash an independently random 128-bit
	// salt, preventing equal passwords from producing equal stored strings.
	adminPasswordSaltBytes = 16
	// adminPasswordDerivedKeyBytes stores the full 256-bit PBKDF2 result.
	adminPasswordDerivedKeyBytes = sha256.Size
	// adminPasswordAlgorithm identifies both PBKDF2 and its SHA-256 HMAC.
	adminPasswordAlgorithm = "pbkdf2-sha256"
	// adminPasswordEncodingVersion allows a future password scheme to coexist
	// with this format without guessing how an older value should be parsed.
	adminPasswordEncodingVersion = "v=1"
)

// Password errors are stable categories that never include the supplied
// password, random bytes, or stored credential value.
var (
	// errAdminPasswordInvalid reports input outside the documented policy.
	errAdminPasswordInvalid = errors.New(
		"admin password does not meet the password policy",
	)
	// errAdminPasswordEncodingInvalid reports an unsupported or non-canonical
	// stored value without repeating that database value to its caller.
	errAdminPasswordEncodingInvalid = errors.New(
		"admin password hash has an invalid encoding",
	)
	// errAdminPasswordHashFailed hides entropy-reader and derivation details that
	// are not useful at an HTTP or CLI boundary.
	errAdminPasswordHashFailed = errors.New(
		"admin password hashing failed",
	)
	// errAdminPasswordManagerParametersInvalid protects the injectable
	// constructor from unusable work factors or entropy sources.
	errAdminPasswordManagerParametersInvalid = errors.New(
		"admin password manager parameters are invalid",
	)
)

// adminPasswordManager is the complete credential behavior required by user
// creation and login code. Callers do not need to know the encoded hash layout
// or import a password library.
type adminPasswordManager interface {
	// Hash validates a plaintext password and returns a salted encoded value.
	Hash(string) (string, error)
	// Verify compares one plaintext password with one encoded stored value.
	Verify(string, string) (bool, error)
}

// pbkdf2AdminPasswordManager derives PBKDF2-HMAC-SHA256 hashes with one fixed
// iteration count and an injected cryptographic entropy source.
type pbkdf2AdminPasswordManager struct {
	// iterations is embedded in every encoded result and checked during parsing.
	iterations int
	// entropy supplies a fresh salt for every successful Hash call.
	entropy io.Reader
}

// Compile-time interface verification catches accidental method-signature
// changes before the HTTP or CLI authentication layers are built.
var _ adminPasswordManager = (*pbkdf2AdminPasswordManager)(nil)

// newAdminPasswordManager constructs the production credential manager with
// the operating system's cryptographically secure random source and the full
// Stage 15 work factor.
func newAdminPasswordManager() adminPasswordManager {
	return &pbkdf2AdminPasswordManager{
		iterations: adminPasswordProductionIterations,
		entropy:    rand.Reader,
	}
}

// newAdminPasswordManagerWithParameters constructs an equivalent manager with
// an explicit work factor and entropy source.
//
// This seam exists for deterministic, low-cost unit tests. Production startup
// must call newAdminPasswordManager so an inexpensive test parameter cannot be
// selected through runtime configuration.
func newAdminPasswordManagerWithParameters(
	iterations int,
	entropy io.Reader,
) (adminPasswordManager, error) {
	if iterations <= 0 || entropy == nil {
		return nil, errAdminPasswordManagerParametersInvalid
	}

	return &pbkdf2AdminPasswordManager{
		iterations: iterations,
		entropy:    entropy,
	}, nil
}

// Hash validates the complete plaintext, generates a unique random salt, and
// derives a 32-byte PBKDF2-HMAC-SHA256 value.
//
// The returned string is deliberately self-describing but strict:
//
//	pbkdf2-sha256$v=1$i=<iterations>$<raw-url-salt>$<raw-url-key>
//
// Raw URL-safe Base64 avoids padding and delimiter ambiguity. Verify accepts
// only the exact canonical spelling emitted here.
func (manager *pbkdf2AdminPasswordManager) Hash(
	password string,
) (string, error) {
	if manager == nil || manager.iterations <= 0 || manager.entropy == nil {
		return "", errAdminPasswordHashFailed
	}
	if !isValidAdminPassword(password) {
		return "", errAdminPasswordInvalid
	}

	salt := make([]byte, adminPasswordSaltBytes)
	if _, err := io.ReadFull(manager.entropy, salt); err != nil {
		return "", errAdminPasswordHashFailed
	}

	derivedKey, err := pbkdf2.Key(
		sha256.New,
		password,
		salt,
		manager.iterations,
		adminPasswordDerivedKeyBytes,
	)
	if err != nil {
		return "", errAdminPasswordHashFailed
	}

	return strings.Join(
		[]string{
			adminPasswordAlgorithm,
			adminPasswordEncodingVersion,
			"i=" + strconv.Itoa(manager.iterations),
			base64.RawURLEncoding.EncodeToString(salt),
			base64.RawURLEncoding.EncodeToString(derivedKey),
		},
		"$",
	), nil
}

// Verify validates the plaintext policy, strictly parses the stored encoding,
// derives the candidate key, and compares equal-length byte slices in constant
// time.
//
// A malformed database value is distinguishable from a wrong password so the
// caller can report an operational problem internally while still presenting
// one generic login failure to an unauthenticated visitor.
func (manager *pbkdf2AdminPasswordManager) Verify(
	password string,
	encoded string,
) (bool, error) {
	if manager == nil || manager.iterations <= 0 {
		return false, errAdminPasswordHashFailed
	}
	if !isValidAdminPassword(password) {
		return false, errAdminPasswordInvalid
	}

	salt, expectedKey, err := manager.parse(encoded)
	if err != nil {
		return false, err
	}

	actualKey, err := pbkdf2.Key(
		sha256.New,
		password,
		salt,
		manager.iterations,
		adminPasswordDerivedKeyBytes,
	)
	if err != nil {
		return false, errAdminPasswordHashFailed
	}

	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

// parse decodes one credential only when every algorithm, version, parameter,
// and Base64 component exactly matches this manager's canonical format.
func (manager *pbkdf2AdminPasswordManager) parse(
	encoded string,
) ([]byte, []byte, error) {
	if !utf8.ValidString(encoded) || strings.ContainsRune(encoded, '\x00') {
		return nil, nil, errAdminPasswordEncodingInvalid
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 5 ||
		parts[0] != adminPasswordAlgorithm ||
		parts[1] != adminPasswordEncodingVersion ||
		!strings.HasPrefix(parts[2], "i=") {
		return nil, nil, errAdminPasswordEncodingInvalid
	}

	iterationText := strings.TrimPrefix(parts[2], "i=")
	iterations, err := strconv.Atoi(iterationText)
	if err != nil ||
		iterations <= 0 ||
		strconv.Itoa(iterations) != iterationText ||
		iterations != manager.iterations {
		return nil, nil, errAdminPasswordEncodingInvalid
	}

	salt, err := decodeCanonicalAdminPasswordComponent(
		parts[3],
		adminPasswordSaltBytes,
	)
	if err != nil {
		return nil, nil, err
	}
	derivedKey, err := decodeCanonicalAdminPasswordComponent(
		parts[4],
		adminPasswordDerivedKeyBytes,
	)
	if err != nil {
		return nil, nil, err
	}

	return salt, derivedKey, nil
}

// decodeCanonicalAdminPasswordComponent accepts one unpadded URL-safe Base64
// value with the exact decoded size required by the password format.
func decodeCanonicalAdminPasswordComponent(
	encoded string,
	expectedLength int,
) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != expectedLength {
		return nil, errAdminPasswordEncodingInvalid
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return nil, errAdminPasswordEncodingInvalid
	}

	return decoded, nil
}

// isValidAdminPassword enforces the plaintext boundary without trimming or
// otherwise normalizing passwords. Every accepted code point, including a
// deliberate leading or trailing space, remains part of the derived secret.
func isValidAdminPassword(password string) bool {
	if !utf8.ValidString(password) || strings.ContainsRune(password, '\x00') {
		return false
	}

	length := utf8.RuneCountInString(password)

	return length >= adminPasswordMinimumRunes &&
		length <= adminPasswordMaximumRunes
}
