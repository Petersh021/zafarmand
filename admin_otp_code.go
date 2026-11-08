package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// adminOTPCodeSpace is the complete four-cell decimal space approved for the
	// optical verification UI. Attempt and lifetime limits compensate for its
	// intentionally short human-entered representation.
	adminOTPCodeSpace = 10_000
	// adminOTPCodeSampleLimit is the largest multiple of the code space that fits
	// in uint16. Rejecting larger samples removes modulo bias.
	adminOTPCodeSampleLimit = 60_000
	// adminOTPKeyBytes is the minimum independent deployment secret used to MAC
	// low-entropy codes before they enter PostgreSQL.
	adminOTPKeyBytes = 32
)

var (
	// errAdminOTPEntropyFailed reports only the local random-source category.
	errAdminOTPEntropyFailed = errors.New("generate administrator OTP failed")
	// errAdminOTPKeyInvalid prevents plaintext or unkeyed code storage when the
	// deployment secret is absent or too short.
	errAdminOTPKeyInvalid = errors.New("administrator OTP key is invalid")
)

// generateAdminOTPCode returns four zero-padded ASCII digits using rejection
// sampling, so all 10,000 visual combinations have exactly equal probability.
func generateAdminOTPCode(entropy io.Reader) (string, error) {
	if entropy == nil {
		return "", errAdminOTPEntropyFailed
	}

	var sampleBytes [2]byte
	for {
		if _, err := io.ReadFull(entropy, sampleBytes[:]); err != nil {
			return "", errAdminOTPEntropyFailed
		}
		sample := int(binary.BigEndian.Uint16(sampleBytes[:]))
		if sample >= adminOTPCodeSampleLimit {
			continue
		}

		return fmt.Sprintf("%04d", sample%adminOTPCodeSpace), nil
	}
}

// adminOTPCodeMAC binds the code to one opaque challenge. A database disclosure
// cannot test the 10,000 possibilities without the separately managed key.
func adminOTPCodeMAC(
	key []byte,
	challengeHash []byte,
	code string,
) ([]byte, error) {
	if len(key) < adminOTPKeyBytes ||
		len(challengeHash) != adminSessionHashBytes ||
		!validAdminOTPCode(code) {
		return nil, errAdminOTPKeyInvalid
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(challengeHash)
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))

	return mac.Sum(nil), nil
}

// adminOTPCodeMACMatches performs a constant-time comparison for two canonical
// 32-byte MAC values and fails closed for every malformed input.
func adminOTPCodeMACMatches(expected []byte, actual []byte) bool {
	return len(expected) == sha256.Size &&
		len(actual) == sha256.Size &&
		subtle.ConstantTimeCompare(expected, actual) == 1
}
