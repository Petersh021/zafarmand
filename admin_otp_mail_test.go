package main

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// TestNewSMTPAdminOTPMailer validates configuration without opening a network
// connection or exposing the supplied test-only credential in errors.
func TestNewSMTPAdminOTPMailer(t *testing.T) {
	valid := adminSMTPConfig{
		Address:  "smtp.example.test:587",
		Host:     "smtp.example.test",
		Username: "mailer",
		Password: "test-only-password",
		From:     "Zafarmand <admin@example.test>",
	}
	mailer, err := newSMTPAdminOTPMailer(valid)
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if mailer.timeout != adminOTPMailTimeout {
		t.Errorf("default timeout: got %s", mailer.timeout)
	}

	invalid := []adminSMTPConfig{
		{},
		{Address: "smtp.example.test", Host: "smtp.example.test", From: "a@example.test"},
		{Address: "other.example.test:587", Host: "smtp.example.test", From: "a@example.test"},
		{Address: "smtp.example.test:587", Host: "smtp.example.test", From: "not an address"},
		{Address: "smtp.example.test:587", Host: "smtp.example.test", From: "a@example.test", Username: "only-user"},
	}
	for index, config := range invalid {
		if _, err := newSMTPAdminOTPMailer(config); !errors.Is(err, errAdminOTPMailerConfigInvalid) {
			t.Errorf("invalid config %d: got %v", index, err)
		}
	}
}

// TestAdminOTPMailMessage checks both branded alternatives, the fixed security
// guidance, and the canonical MIME boundary without parsing user-owned markup.
func TestAdminOTPMailMessage(t *testing.T) {
	from, _ := mail.ParseAddress("Zafarmand <admin@example.test>")
	to, _ := mail.ParseAddress("Owner <owner@example.test>")
	expiresAt := time.Date(2026, time.November, 7, 15, 30, 0, 0, time.UTC)
	message := adminOTPMailMessage(from, to, "4719", expiresAt)
	for _, expected := range []string{
		"From: \"Zafarmand\" <admin@example.test>\r\n",
		"To: \"Owner\" <owner@example.test>\r\n",
		"Subject: Your Zafarmand administrator verification code\r\n",
		"Content-Type: multipart/alternative; boundary=\"zafarmand-admin-verification\"\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"Your four-digit verification code is:\r\n\r\n4719",
		"Content-Type: text/html; charset=UTF-8\r\n",
		">4719</div>",
		"Zafarmand will never ask you to share this code.",
		"2026-11-07 15:30:00 UTC",
		"--zafarmand-admin-verification--\r\n",
	} {
		if !strings.Contains(message, expected) {
			t.Errorf("message does not contain %q", expected)
		}
	}
	if count := strings.Count(message, "4719"); count != 2 {
		t.Errorf("verification code occurrence count: got %d, want 2", count)
	}

	parsedMessage, err := mail.ReadMessage(strings.NewReader(message))
	if err != nil {
		t.Fatalf("parse generated message: %v", err)
	}
	mediaType, parameters, err := mime.ParseMediaType(
		parsedMessage.Header.Get("Content-Type"),
	)
	if err != nil {
		t.Fatalf("parse generated content type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("generated media type: got %q", mediaType)
	}

	reader := multipart.NewReader(parsedMessage.Body, parameters["boundary"])
	partTypes := []string{"text/plain", "text/html"}
	for index, expectedType := range partTypes {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read generated MIME part %d: %v", index, err)
		}
		partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse generated MIME part %d type: %v", index, err)
		}
		if partType != expectedType {
			t.Errorf("generated MIME part %d: got %q, want %q", index, partType, expectedType)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read generated MIME part %d body: %v", index, err)
		}
		if strings.Count(string(body), "4719") != 1 {
			t.Errorf("generated MIME part %d does not contain one code", index)
		}
	}
	if extraPart, err := reader.NextPart(); err != io.EOF || extraPart != nil {
		t.Fatalf("unexpected generated MIME part: part=%v err=%v", extraPart, err)
	}
}

// TestValidAdminOTPCode rejects ambiguous, overlong, and non-ASCII values.
func TestValidAdminOTPCode(t *testing.T) {
	for _, code := range []string{"0000", "4719", "9999"} {
		if !validAdminOTPCode(code) {
			t.Errorf("validAdminOTPCode(%q) = false", code)
		}
	}
	for _, code := range []string{"", "123", "12345", "12a4", "１２３４"} {
		if validAdminOTPCode(code) {
			t.Errorf("validAdminOTPCode(%q) = true", code)
		}
	}
}

// TestAdminOTPMailDeliveryErrorsAreRedactedAndClassified ensures operational
// diagnostics expose only reviewed stage labels while errors remain generic.
func TestAdminOTPMailDeliveryErrorsAreRedactedAndClassified(t *testing.T) {
	tests := []struct {
		stage adminOTPMailDeliveryStage
		label string
	}{
		{adminOTPMailDeliveryStageConnect, "connect"},
		{adminOTPMailDeliveryStageTLS, "tls"},
		{adminOTPMailDeliveryStageAuthenticate, "authenticate"},
		{adminOTPMailDeliveryStageSender, "sender"},
		{adminOTPMailDeliveryStageRecipient, "recipient"},
		{adminOTPMailDeliveryStageMessage, "message"},
	}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			err := newAdminOTPMailDeliveryError(test.stage)
			if !errors.Is(err, errAdminOTPMailDeliveryFailed) {
				t.Fatalf("error does not wrap generic delivery failure: %v", err)
			}
			if err.Error() != errAdminOTPMailDeliveryFailed.Error() {
				t.Errorf("error text: got %q", err.Error())
			}
			if label := adminOTPMailDeliveryStageLabel(err); label != test.label {
				t.Errorf("stage label: got %q, want %q", label, test.label)
			}
		})
	}

	if label := adminOTPMailDeliveryStageLabel(errors.New("provider canary")); label != "unknown" {
		t.Errorf("unknown stage label: got %q", label)
	}
	if label := adminOTPMailDeliveryStageLabel(
		newAdminOTPMailDeliveryError(adminOTPMailDeliveryStage(255)),
	); label != "unknown" {
		t.Errorf("unsupported stage label: got %q", label)
	}
}
