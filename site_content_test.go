package main

import (
	"strings"
	"testing"
)

// TestPublicContactEmailValidation verifies the shared public/protected mailbox
// contract: optional, normalized lowercase, bounded, one separator, no
// whitespace, and a dotted domain.
func TestPublicContactEmailValidation(t *testing.T) {
	tests := []struct {
		// value is the complete stored mailbox candidate.
		value string
		// valid is the expected normalized public result.
		valid bool
	}{
		{value: "", valid: true},
		{value: "studio@example.com", valid: true},
		{value: "projects+interior@example.co.uk", valid: true},
		{value: "Studio@example.com", valid: false},
		{value: "studio@example", valid: false},
		{value: "studio@@example.com", valid: false},
		{value: " studio@example.com", valid: false},
		{value: "studio @example.com", valid: false},
		{value: "studio@\u00a0example.com", valid: false},
		{value: strings.Repeat("a", contactEmailMaximumLength) + "@example.com", valid: false},
	}

	for _, test := range tests {
		if got := isValidPublicContactEmail(test.value); got != test.valid {
			t.Errorf("email %q: got valid=%t, want %t", test.value, got, test.valid)
		}
	}
}

// TestPublicContactPhoneValidation verifies pair atomicity, E.164 normalization,
// and the independent human-readable display boundary.
func TestPublicContactPhoneValidation(t *testing.T) {
	tests := []struct {
		// display is the visible telephone label.
		display string
		// e164 is the machine-readable destination.
		e164 string
		// valid is the expected pair result.
		valid bool
	}{
		{display: "", e164: "", valid: true},
		{display: "+98 21 5555 0101", e164: "+982155550101", valid: true},
		{display: "+98 21 5555 0101", e164: "", valid: false},
		{display: "", e164: "+982155550101", valid: false},
		{display: "Local", e164: "982155550101", valid: false},
		{display: "Local", e164: "+0123456789", valid: false},
		{display: "Local\nOffice", e164: "+982155550101", valid: false},
	}

	for _, test := range tests {
		if got := isValidPublicContactPhone(test.display, test.e164); got != test.valid {
			t.Errorf(
				"phone display=%q e164=%q: got valid=%t, want %t",
				test.display,
				test.e164,
				got,
				test.valid,
			)
		}
	}
}

// TestSiteContentMultilineValidation verifies authored line feeds are retained
// while padding and other control characters fail closed.
func TestSiteContentMultilineValidation(t *testing.T) {
	tests := []struct {
		// value is the reviewed multiline candidate.
		value string
		// required selects required or optional empty semantics.
		required bool
		// valid is the expected result.
		valid bool
	}{
		{value: "First reviewed line\nSecond reviewed line", required: true, valid: true},
		{value: "", required: false, valid: true},
		{value: "", required: true, valid: false},
		{value: " Padded", required: true, valid: false},
		{value: "Tabbed\ttext", required: true, valid: false},
		{value: "Carriage\rreturn", required: true, valid: false},
	}

	for _, test := range tests {
		if got := isValidSiteContentMultiline(test.value, 100, test.required); got != test.valid {
			t.Errorf(
				"multiline %q required=%t: got valid=%t, want %t",
				test.value,
				test.required,
				got,
				test.valid,
			)
		}
	}
}

// TestHomepageFeatureRequiresReviewedCover protects the settled public policy:
// a selected Published record without current reviewed media is omitted by SQL,
// never represented as a coverless feature.
func TestHomepageFeatureRequiresReviewedCover(t *testing.T) {
	feature := validRepositoryHomepageContent().Features[0]
	if !isValidHomepageFeature(feature) {
		t.Fatal("complete cover-backed Homepage feature is invalid")
	}
	feature.Cover = nil
	if isValidHomepageFeature(feature) {
		t.Error("coverless Homepage feature passed public validation")
	}
}
