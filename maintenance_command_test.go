package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// recordingMaintenanceRepository captures only call coordinates and supplies
// fixed aggregate results for command-boundary tests.
type recordingMaintenanceRepository struct {
	inspectResult maintenanceRetentionResult
	applyResult   maintenanceRetentionResult
	purgeResult   maintenancePurgeInquiryResult
	err           error
	inspectCalls  int
	applyCalls    int
	purgeCalls    int
	context       context.Context
	cutoff        time.Time
	inquiryID     int64
	database      string
}

// InspectRetention records one preview invocation without connecting to a DB.
func (repository *recordingMaintenanceRepository) InspectRetention(
	ctx context.Context,
	cutoff time.Time,
) (maintenanceRetentionResult, error) {
	repository.inspectCalls++
	repository.context = ctx
	repository.cutoff = cutoff

	return repository.inspectResult, repository.err
}

// ApplyRetention records one bulk mutation invocation and its confirmation.
func (repository *recordingMaintenanceRepository) ApplyRetention(
	ctx context.Context,
	cutoff time.Time,
	database string,
) (maintenanceRetentionResult, error) {
	repository.applyCalls++
	repository.context = ctx
	repository.cutoff = cutoff
	repository.database = database

	return repository.applyResult, repository.err
}

// PurgeInquiry records one targeted mutation invocation and its confirmation.
func (repository *recordingMaintenanceRepository) PurgeInquiry(
	ctx context.Context,
	inquiryID int64,
	database string,
) (maintenancePurgeInquiryResult, error) {
	repository.purgeCalls++
	repository.context = ctx
	repository.inquiryID = inquiryID
	repository.database = database

	return repository.purgeResult, repository.err
}

// TestParseMaintenanceCommand covers the exact grammar and canonical cutoff,
// identifier, and flag-order rejection rules.
func TestParseMaintenanceCommand(t *testing.T) {
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		args    []string
		want    maintenanceCommand
		wantErr bool
	}{
		{
			name: "retention status",
			args: []string{
				"retention", "status",
				"--inquiries-before", "2025-08-22T00:00:00Z",
			},
			want: maintenanceCommand{
				Action:        maintenanceActionRetentionStatus,
				InquiryCutoff: cutoff,
			},
		},
		{
			name: "retention apply",
			args: []string{
				"retention", "apply",
				"--inquiries-before", "2025-08-22T00:00:00Z",
				"--confirm-database", "zafarmand_operations",
			},
			want: maintenanceCommand{
				Action:            maintenanceActionRetentionApply,
				InquiryCutoff:     cutoff,
				ConfirmedDatabase: "zafarmand_operations",
			},
		},
		{
			name: "targeted purge",
			args: []string{
				"purge-inquiry", "--id", "42",
				"--confirm-database", "zafarmand_operations",
			},
			want: maintenanceCommand{
				Action:            maintenanceActionPurgeInquiry,
				InquiryID:         42,
				ConfirmedDatabase: "zafarmand_operations",
			},
		},
		{name: "empty", wantErr: true},
		{name: "unknown action", args: []string{"erase"}, wantErr: true},
		{
			name: "reordered flags",
			args: []string{
				"retention", "apply", "--confirm-database", "database",
				"--inquiries-before", "2025-08-22T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "duplicate flag",
			args: []string{
				"retention", "status", "--inquiries-before",
				"2025-08-22T00:00:00Z", "--inquiries-before",
				"2025-08-22T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "offset cutoff",
			args: []string{
				"retention", "status", "--inquiries-before",
				"2025-08-22T03:30:00+03:30",
			},
			wantErr: true,
		},
		{
			name: "fractional cutoff",
			args: []string{
				"retention", "status", "--inquiries-before",
				"2025-08-22T00:00:00.1Z",
			},
			wantErr: true,
		},
		{
			name: "future cutoff",
			args: []string{
				"retention", "status", "--inquiries-before",
				"2027-08-22T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "noncanonical ID",
			args: []string{
				"purge-inquiry", "--id", "042",
				"--confirm-database", "database",
			},
			wantErr: true,
		},
		{
			name: "control in database",
			args: []string{
				"purge-inquiry", "--id", "42",
				"--confirm-database", "database\nunsafe",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseMaintenanceCommandAt(test.args, now)
			if test.wantErr {
				if !errors.Is(err, errMaintenanceCommandInvalid) {
					t.Fatalf("error: got %v, want invalid command", err)
				}
				if got != (maintenanceCommand{}) {
					t.Errorf("invalid command returned data: %#v", got)
				}

				return
			}
			if err != nil {
				t.Fatalf("parse maintenance command: %v", err)
			}
			if got != test.want {
				t.Errorf("command: got %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestProgramCommandIncludesMaintenance protects top-level dispatch wiring.
func TestProgramCommandIncludesMaintenance(t *testing.T) {
	command, err := parseProgramCommand([]string{
		"maintenance", "purge-inquiry", "--id", "7",
		"--confirm-database", "zafarmand_test",
	})
	if err != nil {
		t.Fatalf("parse top-level maintenance command: %v", err)
	}
	if command.Name != programCommandMaintenance ||
		command.Maintenance.Action != maintenanceActionPurgeInquiry ||
		command.Maintenance.InquiryID != 7 {
		t.Errorf("top-level maintenance command: %#v", command)
	}
}

// TestLoadMaintenanceDatabaseConfigIsIsolated proves the destructive command
// never falls back to the runtime URL and retains the shared TLS policy.
func TestLoadMaintenanceDatabaseConfigIsIsolated(t *testing.T) {
	const secret = "maintenance-secret"
	lookedUp := make([]string, 0, 1)
	config, err := loadMaintenanceDatabaseConfig(
		func(name string) (string, bool) {
			lookedUp = append(lookedUp, name)
			if name == maintenanceDatabaseURLEnvironmentName {
				return "  postgres://maintenance:" + secret + "@localhost/database  ", true
			}
			if name == databaseRequireTLSEnvironmentName {
				return "true", true
			}

			return "postgres://runtime:must-not-be-read@localhost/database", true
		},
	)
	if err != nil {
		t.Fatalf("load maintenance config: %v", err)
	}
	if len(lookedUp) != 2 ||
		lookedUp[0] != maintenanceDatabaseURLEnvironmentName ||
		lookedUp[1] != databaseRequireTLSEnvironmentName {
		t.Errorf("environment lookups: got %#v", lookedUp)
	}
	if config.connectionString !=
		"postgres://maintenance:"+secret+"@localhost/database" {
		t.Error("maintenance connection string was not trimmed exactly")
	}
	if !config.requireTLS {
		t.Error("maintenance config did not preserve required TLS policy")
	}

	_, err = loadMaintenanceDatabaseConfig(func(name string) (string, bool) {
		if name == databaseURLEnvironmentName {
			return "postgres://runtime:unsafe-fallback@localhost/database", true
		}

		return "", false
	})
	if !errors.Is(err, errMaintenanceDatabaseURLRequired) {
		t.Fatalf("missing maintenance URL: got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "unsafe") {
		t.Error("maintenance configuration error exposes a credential")
	}

	_, err = loadMaintenanceDatabaseConfig(func(name string) (string, bool) {
		switch name {
		case maintenanceDatabaseURLEnvironmentName:
			return "postgres://maintenance:secret@localhost/database", true
		case databaseRequireTLSEnvironmentName:
			return "TRUE", true
		default:
			return "", false
		}
	})
	if !errors.Is(err, errDatabaseRequireTLSInvalid) {
		t.Fatalf("invalid maintenance TLS policy: got %v", err)
	}
}

// TestRunMaintenanceCommand verifies bounded contexts and privacy-safe output
// for preview, bulk retention, targeted purge, and repository failure.
func TestRunMaintenanceCommand(t *testing.T) {
	cutoff := time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		command    maintenanceCommand
		repository *recordingMaintenanceRepository
		wantOutput string
		wantError  error
	}{
		{
			name: "status",
			command: maintenanceCommand{
				Action:        maintenanceActionRetentionStatus,
				InquiryCutoff: cutoff,
			},
			repository: &recordingMaintenanceRepository{
				inspectResult: maintenanceRetentionResult{2, 3},
			},
			wantOutput: "Retention status: 2 expired session(s); 3 archived inquiry/inquiries eligible before 2025-08-22T00:00:00Z.\n",
		},
		{
			name: "apply",
			command: maintenanceCommand{
				Action:            maintenanceActionRetentionApply,
				InquiryCutoff:     cutoff,
				ConfirmedDatabase: "private_database",
			},
			repository: &recordingMaintenanceRepository{
				applyResult: maintenanceRetentionResult{4, 5},
			},
			wantOutput: "Retention applied: 4 expired session(s) removed; 5 archived inquiry/inquiries removed before 2025-08-22T00:00:00Z.\n",
		},
		{
			name: "targeted purge",
			command: maintenanceCommand{
				Action:            maintenanceActionPurgeInquiry,
				InquiryID:         99,
				ConfirmedDatabase: "private_database",
			},
			repository: &recordingMaintenanceRepository{
				purgeResult: maintenancePurgeInquiryResult{Purged: true},
			},
			wantOutput: "One archived inquiry was removed.\n",
		},
		{
			name: "safe failure",
			command: maintenanceCommand{
				Action:        maintenanceActionRetentionStatus,
				InquiryCutoff: cutoff,
			},
			repository: &recordingMaintenanceRepository{
				err: errMaintenanceRepositoryFailed,
			},
			wantError: errMaintenanceRepositoryFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			err := runMaintenanceCommand(
				t.Context(),
				test.command,
				test.repository,
				&output,
			)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error: got %v, want %v", err, test.wantError)
			}
			if output.String() != test.wantOutput {
				t.Errorf("output: got %q, want %q", output.String(), test.wantOutput)
			}
			if strings.Contains(output.String(), "private_database") ||
				strings.Contains(output.String(), "99") {
				t.Error("maintenance output exposes database confirmation or inquiry ID")
			}
			if test.repository.context != nil {
				if _, hasDeadline := test.repository.context.Deadline(); !hasDeadline {
					t.Error("maintenance repository context has no deadline")
				}
			}
		})
	}
}

// TestExecuteMaintenanceCommandRejectsLocalBoundaries fails invalid dependency,
// command, credential, and URL states before any useful database work.
func TestExecuteMaintenanceCommandRejectsLocalBoundaries(t *testing.T) {
	cutoff := time.Date(2025, time.August, 22, 0, 0, 0, 0, time.UTC)
	valid := maintenanceCommand{
		Action:        maintenanceActionRetentionStatus,
		InquiryCutoff: cutoff,
	}
	if err := executeMaintenanceCommand(
		t.Context(), valid, nil, nil,
	); !errors.Is(err, errMaintenanceOutputRequired) {
		t.Errorf("nil output: got %v", err)
	}
	if err := executeMaintenanceCommand(
		t.Context(), maintenanceCommand{}, nil, &strings.Builder{},
	); !errors.Is(err, errMaintenanceCommandInvalid) {
		t.Errorf("invalid command: got %v", err)
	}
	if err := executeMaintenanceCommand(
		t.Context(),
		valid,
		func(string) (string, bool) { return "", false },
		&strings.Builder{},
	); !errors.Is(err, errMaintenanceDatabaseURLRequired) {
		t.Errorf("missing maintenance URL: got %v", err)
	}
	if err := executeMaintenanceCommand(
		t.Context(),
		valid,
		func(name string) (string, bool) {
			if name == maintenanceDatabaseURLEnvironmentName {
				return "not a PostgreSQL URL", true
			}

			return "", false
		},
		&strings.Builder{},
	); !errors.Is(err, errMaintenanceDatabaseConfigurationInvalid) {
		t.Errorf("invalid maintenance URL: got %v", err)
	}
}
