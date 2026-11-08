package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// adminPasswordResetterStub records the encoded credential passed to the
// command-only persistence boundary.
type adminPasswordResetterStub struct {
	// contexts preserves the caller context for exact propagation assertions.
	contexts []context.Context
	// emails records canonical account selectors in call order.
	emails []string
	// passwordHashes records only encoded verifier inputs.
	passwordHashes []string
	// err simulates a persistence failure with potentially unsafe detail.
	err error
}

// ResetPassword implements adminPasswordResetter for isolated command tests.
func (resetter *adminPasswordResetterStub) ResetPassword(
	ctx context.Context,
	email string,
	passwordHash string,
) error {
	resetter.contexts = append(resetter.contexts, ctx)
	resetter.emails = append(resetter.emails, email)
	resetter.passwordHashes = append(resetter.passwordHashes, passwordHash)

	return resetter.err
}

// TestParseAdminResetPasswordCommand verifies the one accepted flag pair and
// the canonical mailbox shared by login and persistence.
func TestParseAdminResetPasswordCommand(t *testing.T) {
	command, err := parseAdminResetPasswordCommand([]string{
		"--email",
		" Owner@Example.COM ",
	})
	if err != nil {
		t.Fatalf("parse reset command: %v", err)
	}
	if command != (adminResetPasswordCommand{Email: "owner@example.com"}) {
		t.Errorf("command: got %#v", command)
	}
}

// TestParseAdminResetPasswordCommandRejectsMalformedArguments exercises the
// closed grammar and proves errors never repeat supplied private values.
func TestParseAdminResetPasswordCommandRejectsMalformedArguments(t *testing.T) {
	privateEmail := "private.owner@example.com"
	privatePassword := "Never-Echo-This-Reset-Password"
	tests := []struct {
		name string
		args []string
	}{
		{name: "no flags"},
		{name: "missing value", args: []string{"--email"}},
		{name: "extra positional", args: []string{"--email", privateEmail, "extra"}},
		{name: "duplicate email", args: []string{"--email", privateEmail, "--email", privateEmail}},
		{name: "unknown flag", args: []string{"--account", privateEmail}},
		{name: "role forbidden", args: []string{"--role", "owner"}},
		{name: "password flag forbidden", args: []string{"--password", privatePassword}},
		{name: "invalid mailbox", args: []string{"--email", "not-an-email"}},
		{name: "display name forbidden", args: []string{"--email", "Owner <" + privateEmail + ">"}},
		{name: "markdown escaped mailbox forbidden", args: []string{"--email", `private.owner\@example.com`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := parseAdminResetPasswordCommand(test.args)
			if !errors.Is(err, errAdminResetPasswordArgumentsInvalid) {
				t.Fatalf("error: got %v, want invalid-arguments sentinel", err)
			}
			if command != (adminResetPasswordCommand{}) {
				t.Errorf("command: got %#v, want zero", command)
			}
			if !strings.Contains(err.Error(), adminResetPasswordUsage) {
				t.Errorf("error does not contain reset usage: %v", err)
			}
			for _, privateValue := range []string{privateEmail, privatePassword} {
				if strings.Contains(err.Error(), privateValue) {
					t.Errorf("error exposes private value %q", privateValue)
				}
			}
		})
	}
}

// TestResetAdminPasswordHashesOnceBeforePersistence verifies that plaintext
// stops at the password manager and only the encoded verifier reaches storage.
func TestResetAdminPasswordHashesOnceBeforePersistence(t *testing.T) {
	ctx := t.Context()
	password := "Replacement-Correct-Horse-15"
	passwordHash := "pbkdf2-sha256$reset-test-hash"
	hasher := &adminPasswordHasherStub{hash: passwordHash}
	resetter := &adminPasswordResetterStub{}
	command := adminResetPasswordCommand{Email: "owner@example.com"}

	if err := resetAdminPassword(
		ctx,
		command,
		password,
		hasher,
		resetter,
	); err != nil {
		t.Fatalf("reset administrator password: %v", err)
	}

	if len(hasher.calls) != 1 || hasher.calls[0] != password {
		t.Fatalf("hash calls: got %#v, want one plaintext input", hasher.calls)
	}
	if len(resetter.contexts) != 1 || resetter.contexts[0] != ctx ||
		len(resetter.emails) != 1 || resetter.emails[0] != command.Email ||
		len(resetter.passwordHashes) != 1 ||
		resetter.passwordHashes[0] != passwordHash {
		t.Errorf(
			"resetter calls: contexts=%d emails=%#v hashes=%#v",
			len(resetter.contexts),
			resetter.emails,
			resetter.passwordHashes,
		)
	}
	if strings.Contains(resetter.passwordHashes[0], password) {
		t.Error("resetter received a hash containing the plaintext password")
	}
}

// TestResetAdminPasswordRedactsDependencyFailures proves credential and
// account material cannot cross either dependency error boundary.
func TestResetAdminPasswordRedactsDependencyFailures(t *testing.T) {
	password := "Never-Return-Reset-Password-15"
	email := "private.owner@example.com"
	passwordHash := "pbkdf2-sha256$private-reset-hash"
	tests := []struct {
		name          string
		hasher        adminPasswordHasher
		resetter      adminPasswordResetter
		expectedError error
	}{
		{name: "missing hasher", resetter: &adminPasswordResetterStub{}, expectedError: errAdminResetPasswordHashFailed},
		{name: "missing resetter", hasher: &adminPasswordHasherStub{hash: passwordHash}, expectedError: errAdminResetPasswordFailed},
		{name: "hash failure", hasher: &adminPasswordHasherStub{err: errors.New("hash " + password)}, resetter: &adminPasswordResetterStub{}, expectedError: errAdminResetPasswordHashFailed},
		{name: "empty hash", hasher: &adminPasswordHasherStub{}, resetter: &adminPasswordResetterStub{}, expectedError: errAdminResetPasswordHashFailed},
		{name: "repository failure", hasher: &adminPasswordHasherStub{hash: passwordHash}, resetter: &adminPasswordResetterStub{err: errors.New(email + passwordHash)}, expectedError: errAdminResetPasswordFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := resetAdminPassword(
				t.Context(),
				adminResetPasswordCommand{Email: email},
				password,
				test.hasher,
				test.resetter,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			for _, privateValue := range []string{password, email, passwordHash} {
				if strings.Contains(err.Error(), privateValue) {
					t.Errorf("error exposes private value %q", privateValue)
				}
			}
		})
	}
}

// TestExecuteAdminResetPasswordCommandRejectsLocalBoundaries checks every
// pre-connection failure and the dedicated replacement-password variable.
func TestExecuteAdminResetPasswordCommandRejectsLocalBoundaries(t *testing.T) {
	command := adminResetPasswordCommand{Email: "owner@example.com"}
	password := "Environment-Only-Replacement-15"
	tests := []struct {
		name          string
		ctx           context.Context
		command       adminResetPasswordCommand
		lookup        environmentLookup
		output        io.Writer
		expectedError error
	}{
		{name: "nil context", command: command, output: &strings.Builder{}, expectedError: errAdminResetPasswordContextRequired},
		{name: "nil output", ctx: t.Context(), command: command, expectedError: errAdminResetPasswordOutputRequired},
		{name: "invalid direct email", ctx: t.Context(), command: adminResetPasswordCommand{Email: "invalid"}, output: &strings.Builder{}, expectedError: errAdminResetPasswordArgumentsInvalid},
		{name: "nil lookup", ctx: t.Context(), command: command, output: &strings.Builder{}, expectedError: errAdminNewPasswordRequired},
		{name: "missing new password", ctx: t.Context(), command: command, lookup: func(string) (string, bool) { return "", false }, output: &strings.Builder{}, expectedError: errAdminNewPasswordRequired},
		{name: "empty new password", ctx: t.Context(), command: command, lookup: func(name string) (string, bool) { return "", name == adminNewPasswordEnvironmentName }, output: &strings.Builder{}, expectedError: errAdminNewPasswordRequired},
		{
			name:    "missing database URL",
			ctx:     t.Context(),
			command: command,
			lookup: func(name string) (string, bool) {
				if name == adminNewPasswordEnvironmentName {
					return password, true
				}
				return "", false
			},
			output:        &strings.Builder{},
			expectedError: errDatabaseURLRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, _ := test.output.(*strings.Builder)
			err := executeAdminResetPasswordCommand(
				test.ctx,
				test.command,
				test.lookup,
				test.output,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if strings.Contains(err.Error(), password) {
				t.Error("execution error exposes the replacement password")
			}
			if output != nil && output.Len() != 0 {
				t.Errorf("failure output: got %q, want empty", output.String())
			}
		})
	}
}
