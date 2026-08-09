package main

import (
	"errors"
	"strings"
	"testing"
)

// adminConstructionPasswordManager controls both startup operations used to
// prove the missing-account verifier is valid without exposing dependency text.
type adminConstructionPasswordManager struct {
	// hash and hashError are returned by Hash.
	hash      string
	hashError error
	// matches and verifyError are returned by Verify.
	matches     bool
	verifyError error
}

// Hash implements adminPasswordManager for application-construction tests.
func (manager adminConstructionPasswordManager) Hash(
	_ string,
) (string, error) {
	return manager.hash, manager.hashError
}

// Verify implements adminPasswordManager for application-construction tests.
func (manager adminConstructionPasswordManager) Verify(
	_ string,
	_ string,
) (bool, error) {
	return manager.matches, manager.verifyError
}

// TestNewApplicationRejectsInvalidDummyVerifier protects the account-neutral
// login path from a failing or inconsistent future password implementation.
// Every construction failure is reduced to one safe sentinel rather than
// retaining implementation text that could include its plaintext input.
func TestNewApplicationRejectsInvalidDummyVerifier(t *testing.T) {
	privateDetail := "private password implementation detail"
	tests := []struct {
		// name identifies one broken manager contract.
		name string
		// manager supplies that behavior to newApplication.
		manager adminPasswordManager
	}{
		{
			name: "hash error",
			manager: adminConstructionPasswordManager{
				hashError: errors.New(privateDetail),
			},
		},
		{
			name: "empty hash",
			manager: adminConstructionPasswordManager{
				matches: true,
			},
		},
		{
			name: "verification error",
			manager: adminConstructionPasswordManager{
				hash:        "syntactically-valid-test-hash",
				verifyError: errors.New(privateDetail),
			},
		},
		{
			name: "verification mismatch",
			manager: adminConstructionPasswordManager{
				hash: "syntactically-valid-test-hash",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, err := newApplication(
				&recordingInquiryRepository{
					result: inquiryCreateResultCreated,
				},
				newRecordingAdminRepository(),
				newRecordingAdminInquiryReader(),
				test.manager,
			)

			requireErrorIs(t, err, errAdminDummyPasswordHashFailed)
			if app != nil {
				t.Error("invalid dummy verifier returned a usable application")
			}
			if strings.Contains(err.Error(), privateDetail) {
				t.Error("construction error exposes password-manager detail")
			}
		})
	}
}
