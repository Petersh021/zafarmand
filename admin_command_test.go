package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// adminPasswordHasherStub records every plaintext value passed across the
// hashing boundary and returns a controlled hash or error.
type adminPasswordHasherStub struct {
	// calls preserves invocation order so tests can prove exactly one hash attempt.
	calls []string
	// hash is returned when err is nil.
	hash string
	// err simulates a password-manager failure, including deliberately unsafe
	// detail used by redaction tests.
	err error
}

// Hash implements adminPasswordHasher for isolated command tests.
func (hasher *adminPasswordHasherStub) Hash(
	password string,
) (string, error) {
	hasher.calls = append(hasher.calls, password)

	return hasher.hash, hasher.err
}

// adminUserCreatorStub records users passed to persistence without connecting
// to PostgreSQL.
type adminUserCreatorStub struct {
	// contexts records the exact request context supplied to CreateUser.
	contexts []context.Context
	// users records complete persistence inputs in call order.
	users []adminUser
	// err simulates a repository failure, including unsafe detail for redaction
	// assertions.
	err error
}

// CreateUser implements adminUserCreator for isolated command tests.
func (creator *adminUserCreatorStub) CreateUser(
	ctx context.Context,
	user adminUser,
) error {
	creator.contexts = append(creator.contexts, ctx)
	creator.users = append(creator.users, user)

	return creator.err
}

// TestParseAdminCreateUserCommand verifies both accepted flag orders and the
// shared canonical email representation used by login and PostgreSQL.
func TestParseAdminCreateUserCommand(t *testing.T) {
	tests := []struct {
		// name labels one accepted command-line shape.
		name string
		// args are the values following `admin create-user`.
		args []string
		// expected is the complete validated command.
		expected adminCreateUserCommand
	}{
		{
			name: "email then owner role",
			args: []string{
				"--email",
				" Owner@Example.COM ",
				"--role",
				"owner",
			},
			expected: adminCreateUserCommand{
				Email: "owner@example.com",
				Role:  adminRoleOwner,
			},
		},
		{
			name: "editor role then email",
			args: []string{
				"--role",
				"editor",
				"--email",
				"editor@example.com",
			},
			expected: adminCreateUserCommand{
				Email: "editor@example.com",
				Role:  adminRoleEditor,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := parseAdminCreateUserCommand(test.args)
			if err != nil {
				t.Fatalf("parse command: %v", err)
			}
			if actual != test.expected {
				t.Errorf(
					"command: got %#v, want %#v",
					actual,
					test.expected,
				)
			}
		})
	}
}

// TestParseAdminCreateUserCommandRejectsMalformedArguments exercises the
// command's closed grammar. Every failure remains generic and never repeats a
// supplied email address or plaintext password.
func TestParseAdminCreateUserCommandRejectsMalformedArguments(
	t *testing.T,
) {
	privateEmail := "private.person@example.com"
	privatePassword := "Do-Not-Echo-This-Password"
	tests := []struct {
		// name labels one rejected grammar or value boundary.
		name string
		// args are the untrusted command-line values.
		args []string
	}{
		{name: "no flags"},
		{
			name: "email only",
			args: []string{"--email", privateEmail},
		},
		{
			name: "role only",
			args: []string{"--role", "owner"},
		},
		{
			name: "missing email value",
			args: []string{"--email", "--role", "owner"},
		},
		{
			name: "extra positional argument",
			args: []string{
				"--email", privateEmail,
				"--role", "owner",
				"extra",
			},
		},
		{
			name: "duplicate email",
			args: []string{
				"--email", privateEmail,
				"--email", "second@example.com",
			},
		},
		{
			name: "duplicate role",
			args: []string{
				"--role", "owner",
				"--role", "editor",
			},
		},
		{
			name: "unknown flag",
			args: []string{
				"--email", privateEmail,
				"--access", "owner",
			},
		},
		{
			name: "unsupported role",
			args: []string{
				"--email", privateEmail,
				"--role", "administrator",
			},
		},
		{
			name: "role is case sensitive",
			args: []string{
				"--email", privateEmail,
				"--role", "Owner",
			},
		},
		{
			name: "invalid mailbox",
			args: []string{
				"--email", privateEmail + " invalid",
				"--role", "owner",
			},
		},
		{
			name: "display-name mailbox",
			args: []string{
				"--email", "Private Person <" + privateEmail + ">",
				"--role", "owner",
			},
		},
		{
			name: "password flag is forbidden",
			args: []string{
				"--email", privateEmail,
				"--password", privatePassword,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := parseAdminCreateUserCommand(test.args)
			if !errors.Is(err, errAdminCreateUserArgumentsInvalid) {
				t.Fatalf(
					"error: got %v, want invalid-arguments sentinel",
					err,
				)
			}
			if command != (adminCreateUserCommand{}) {
				t.Errorf("command: got %#v, want zero value", command)
			}
			if !strings.Contains(err.Error(), adminCreateUserUsage) {
				t.Errorf("error does not contain usage: %v", err)
			}
			for _, secret := range []string{
				privateEmail,
				privatePassword,
			} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error exposes private value %q", secret)
				}
			}
		})
	}
}

// TestCreateAdminUserHashesOnceBeforePersistence verifies the complete local
// credential boundary without a live database.
func TestCreateAdminUserHashesOnceBeforePersistence(t *testing.T) {
	ctx := t.Context()
	password := "Correct-Horse-Battery-Staple-15"
	passwordHash := "pbkdf2_sha256$test-only-hash"
	hasher := &adminPasswordHasherStub{hash: passwordHash}
	repository := &adminUserCreatorStub{}
	command := adminCreateUserCommand{
		Email: "owner@example.com",
		Role:  adminRoleOwner,
	}

	err := createAdminUser(
		ctx,
		command,
		password,
		hasher,
		repository,
	)
	if err != nil {
		t.Fatalf("create administrator: %v", err)
	}

	if len(hasher.calls) != 1 || hasher.calls[0] != password {
		t.Fatalf(
			"hash calls: got %#v, want one plaintext input",
			hasher.calls,
		)
	}
	if len(repository.users) != 1 {
		t.Fatalf(
			"repository calls: got %d, want 1",
			len(repository.users),
		)
	}
	if len(repository.contexts) != 1 || repository.contexts[0] != ctx {
		t.Error("repository did not receive the original context exactly once")
	}

	expectedUser := adminUser{
		Email:        command.Email,
		PasswordHash: passwordHash,
		Role:         command.Role,
	}
	if repository.users[0] != expectedUser {
		t.Errorf(
			"repository user: got %#v, want %#v",
			repository.users[0],
			expectedUser,
		)
	}
	if strings.Contains(repository.users[0].PasswordHash, password) {
		t.Error("repository received a hash containing the plaintext password")
	}
}

// TestCreateAdminUserRedactsDependencyFailures proves that neither password
// manager errors nor repository errors can cross the CLI with credential or
// account details attached.
func TestCreateAdminUserRedactsDependencyFailures(t *testing.T) {
	password := "Never-Return-This-Password-15"
	email := "private.owner@example.com"
	passwordHash := "pbkdf2_sha256$private-test-hash"
	unsafeHashError := errors.New("could not hash " + password)
	unsafeRepositoryError := errors.New(
		"duplicate " + email + " with " + passwordHash,
	)

	tests := []struct {
		// name labels one dependency boundary.
		name string
		// hasher is nil only for the missing-hasher case.
		hasher adminPasswordHasher
		// repository is nil only for the missing-repository case.
		repository adminUserCreator
		// expectedError is the stable redacted category.
		expectedError error
	}{
		{
			name:          "missing hasher",
			repository:    &adminUserCreatorStub{},
			expectedError: errAdminCreateUserHashFailed,
		},
		{
			name:          "missing repository",
			hasher:        &adminPasswordHasherStub{hash: passwordHash},
			expectedError: errAdminCreateUserFailed,
		},
		{
			name: "hash failure",
			hasher: &adminPasswordHasherStub{
				err: unsafeHashError,
			},
			repository:    &adminUserCreatorStub{},
			expectedError: errAdminCreateUserHashFailed,
		},
		{
			name: "empty hash",
			hasher: &adminPasswordHasherStub{
				hash: "",
			},
			repository:    &adminUserCreatorStub{},
			expectedError: errAdminCreateUserHashFailed,
		},
		{
			name: "repository failure",
			hasher: &adminPasswordHasherStub{
				hash: passwordHash,
			},
			repository: &adminUserCreatorStub{
				err: unsafeRepositoryError,
			},
			expectedError: errAdminCreateUserFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := createAdminUser(
				t.Context(),
				adminCreateUserCommand{
					Email: email,
					Role:  adminRoleOwner,
				},
				password,
				test.hasher,
				test.repository,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			for _, secret := range []string{
				password,
				email,
				passwordHash,
			} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error exposes private value %q", secret)
				}
			}
		})
	}
}

// TestExecuteAdminCreateUserCommandRejectsLocalBoundaries verifies failures
// that occur before PostgreSQL can be opened. It also proves that a password is
// requested only through the one documented environment variable.
func TestExecuteAdminCreateUserCommandRejectsLocalBoundaries(
	t *testing.T,
) {
	command := adminCreateUserCommand{
		Email: "owner@example.com",
		Role:  adminRoleOwner,
	}
	password := "Environment-Only-Password-15"

	tests := []struct {
		// name labels one local execution boundary.
		name string
		// ctx is nil only for the context-ownership case.
		ctx context.Context
		// command may bypass the parser to exercise defensive validation.
		command adminCreateUserCommand
		// lookup supplies only explicitly controlled environment values.
		lookup environmentLookup
		// output is nil only for the writer-ownership case.
		output io.Writer
		// expectedError is the stable failure returned before networking.
		expectedError error
	}{
		{
			name:          "nil context",
			command:       command,
			output:        &strings.Builder{},
			expectedError: errAdminCreateUserContextRequired,
		},
		{
			name:          "nil output",
			ctx:           t.Context(),
			command:       command,
			expectedError: errAdminCreateUserOutputRequired,
		},
		{
			name: "invalid direct email",
			ctx:  t.Context(),
			command: adminCreateUserCommand{
				Email: "not-an-email",
				Role:  adminRoleOwner,
			},
			output:        &strings.Builder{},
			expectedError: errAdminCreateUserArgumentsInvalid,
		},
		{
			name: "invalid direct role",
			ctx:  t.Context(),
			command: adminCreateUserCommand{
				Email: command.Email,
				Role:  adminRole("administrator"),
			},
			output:        &strings.Builder{},
			expectedError: errAdminCreateUserArgumentsInvalid,
		},
		{
			name:          "nil environment lookup",
			ctx:           t.Context(),
			command:       command,
			output:        &strings.Builder{},
			expectedError: errAdminPasswordRequired,
		},
		{
			name:    "missing password",
			ctx:     t.Context(),
			command: command,
			lookup: func(string) (string, bool) {
				return "", false
			},
			output:        &strings.Builder{},
			expectedError: errAdminPasswordRequired,
		},
		{
			name:    "missing database URL",
			ctx:     t.Context(),
			command: command,
			lookup: func(name string) (string, bool) {
				if name == adminPasswordEnvironmentName {
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
			err := executeAdminCreateUserCommand(
				test.ctx,
				test.command,
				test.lookup,
				test.output,
			)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			if err != nil && strings.Contains(err.Error(), password) {
				t.Error("execution error exposes the environment password")
			}
			if output != nil && output.Len() != 0 {
				t.Errorf(
					"failure output: got %q, want empty",
					output.String(),
				)
			}
		})
	}
}
