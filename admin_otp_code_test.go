package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
)

// TestGenerateAdminOTPCode covers zero padding and rejection of biased samples.
func TestGenerateAdminOTPCode(t *testing.T) {
	code, err := generateAdminOTPCode(bytes.NewReader([]byte{
		0xea, 0x60, // 60000 is rejected.
		0x12, 0x6f, // 4719 is accepted.
	}))
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if code != "4719" {
		t.Errorf("code: got %q, want 4719", code)
	}

	code, err = generateAdminOTPCode(bytes.NewReader([]byte{0x00, 0x07}))
	if err != nil {
		t.Fatalf("generate padded code: %v", err)
	}
	if code != "0007" {
		t.Errorf("padded code: got %q, want 0007", code)
	}
	if _, err := generateAdminOTPCode(bytes.NewReader(nil)); !errors.Is(err, errAdminOTPEntropyFailed) {
		t.Errorf("empty entropy: got %v", err)
	}
}

// TestAdminOTPCodeMAC verifies challenge binding and constant-time comparison.
func TestAdminOTPCodeMAC(t *testing.T) {
	key := bytes.Repeat([]byte{0x41}, adminOTPKeyBytes)
	challengeA := bytes.Repeat([]byte{0x11}, adminSessionHashBytes)
	challengeB := bytes.Repeat([]byte{0x12}, adminSessionHashBytes)
	macA, err := adminOTPCodeMAC(key, challengeA, "4719")
	if err != nil {
		t.Fatalf("MAC A: %v", err)
	}
	macB, err := adminOTPCodeMAC(key, challengeB, "4719")
	if err != nil {
		t.Fatalf("MAC B: %v", err)
	}
	if bytes.Equal(macA, macB) {
		t.Error("different challenges produced the same MAC")
	}
	if !adminOTPCodeMACMatches(macA, append([]byte(nil), macA...)) {
		t.Error("equal MACs did not match")
	}
	if adminOTPCodeMACMatches(macA, macB) {
		t.Error("different MACs matched")
	}
	if adminOTPCodeMACMatches(macA, make([]byte, sha256.Size-1)) {
		t.Error("malformed MAC matched")
	}

	for _, test := range []struct {
		key       []byte
		challenge []byte
		code      string
	}{
		{key: key[:adminOTPKeyBytes-1], challenge: challengeA, code: "4719"},
		{key: key, challenge: challengeA[:adminSessionHashBytes-1], code: "4719"},
		{key: key, challenge: challengeA, code: "47x9"},
	} {
		if _, err := adminOTPCodeMAC(test.key, test.challenge, test.code); !errors.Is(err, errAdminOTPKeyInvalid) {
			t.Errorf("invalid MAC input: got %v", err)
		}
	}
}
