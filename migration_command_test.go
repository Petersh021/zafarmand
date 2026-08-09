package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestParseProgramCommand verifies every supported server, migration, and
// administrator-bootstrap shape, including the rollback confirmation boundary.
func TestParseProgramCommand(t *testing.T) {
	tests := []struct {
		// name labels one command-line shape.
		name string
		// args are the values that follow the executable name.
		args []string
		// expected is the complete parsed command for a successful case.
		expected programCommand
		// expectedError identifies a stable failure when one exists.
		expectedError error
		// expectedErrorText checks useful context for non-sentinel failures.
		expectedErrorText string
	}{
		{
			name:     "public server",
			expected: programCommand{Name: programCommandServe},
		},
		{
			name: "migrate defaults to up",
			args: []string{"migrate"},
			expected: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionUp,
			},
		},
		{
			name: "explicit up",
			args: []string{"migrate", "up"},
			expected: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionUp,
			},
		},
		{
			name: "status",
			args: []string{"migrate", "status"},
			expected: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionStatus,
			},
		},
		{
			name: "confirmed down",
			args: []string{
				"migrate",
				"down",
				"--confirm",
			},
			expected: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionDown,
			},
		},
		{
			name: "create owner administrator",
			args: []string{
				"admin",
				"create-user",
				"--email",
				" Owner@Example.COM ",
				"--role",
				"owner",
			},
			expected: programCommand{
				Name: programCommandAdmin,
				AdminCreateUser: adminCreateUserCommand{
					Email: "owner@example.com",
					Role:  adminRoleOwner,
				},
			},
		},
		{
			name: "create editor administrator with reversed flags",
			args: []string{
				"admin",
				"create-user",
				"--role",
				"editor",
				"--email",
				"editor@example.com",
			},
			expected: programCommand{
				Name: programCommandAdmin,
				AdminCreateUser: adminCreateUserCommand{
					Email: "editor@example.com",
					Role:  adminRoleEditor,
				},
			},
		},
		{
			name:              "unknown top-level command",
			args:              []string{"database"},
			expectedErrorText: "unknown command",
		},
		{
			name:              "unknown migration action",
			args:              []string{"migrate", "reset"},
			expectedErrorText: "unknown migration action",
		},
		{
			name:              "unknown admin action",
			args:              []string{"admin", "delete-user"},
			expectedErrorText: "unknown admin command",
		},
		{
			name:              "extra up argument",
			args:              []string{"migrate", "up", "extra"},
			expectedErrorText: "unexpected arguments",
		},
		{
			name:          "unconfirmed down",
			args:          []string{"migrate", "down"},
			expectedError: errMigrationConfirmationRequired,
		},
		{
			name: "incorrect down confirmation",
			args: []string{
				"migrate",
				"down",
				"--yes",
			},
			expectedError: errMigrationConfirmationRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := parseProgramCommand(test.args)
			if test.expectedErrorText == "" &&
				!errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			if test.expectedErrorText != "" &&
				(err == nil || !strings.Contains(
					err.Error(),
					test.expectedErrorText,
				)) {
				t.Fatalf(
					"error: got %v, want text %q",
					err,
					test.expectedErrorText,
				)
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

// TestExecuteMigrationCommandRejectsLocalBoundaries verifies errors that occur
// before any PostgreSQL connection can be opened.
func TestExecuteMigrationCommandRejectsLocalBoundaries(t *testing.T) {
	tests := []struct {
		// name labels one pre-connection command boundary.
		name string
		// command is passed directly to the execution layer.
		command programCommand
		// lookup supplies DATABASE_URL when the boundary needs it.
		lookup environmentLookup
		// output is nil only for the writer-ownership case.
		output io.Writer
		// expectedError is the stable error returned before networking.
		expectedError error
	}{
		{
			name: "nil output",
			command: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionStatus,
			},
			expectedError: errMigrationOutputRequired,
		},
		{
			name:    "non-migration mode",
			command: programCommand{Name: programCommandServe},
			output:  &strings.Builder{},
		},
		{
			name: "missing database URL",
			command: programCommand{
				Name:            programCommandMigrate,
				MigrationAction: migrationActionStatus,
			},
			lookup: func(string) (string, bool) {
				return "", false
			},
			output:        &strings.Builder{},
			expectedError: errDatabaseURLRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := executeMigrationCommand(
				t.Context(),
				test.command,
				test.lookup,
				test.output,
			)
			if test.name == "non-migration mode" {
				if err == nil || !strings.Contains(
					err.Error(),
					"non-migration",
				) {
					t.Fatalf(
						"error: got %v, want non-migration context",
						err,
					)
				}

				return
			}

			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
		})
	}
}
