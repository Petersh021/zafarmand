package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Maintenance command constants define the isolated credential, closed action
// vocabulary, exact flags, bounded execution window, and credential-free help.
const (
	maintenanceDatabaseURLEnvironmentName = "ZAFARMAND_MAINTENANCE_DATABASE_URL"
	maintenanceActionRetentionStatus      = "retention-status"
	maintenanceActionRetentionApply       = "retention-apply"
	maintenanceActionPurgeInquiry         = "purge-inquiry"
	maintenanceCutoffFlag                 = "--inquiries-before"
	maintenanceInquiryIDFlag              = "--id"
	maintenanceDatabaseConfirmationFlag   = "--confirm-database"
	maintenanceOperationTimeout           = 2 * time.Minute
	maintenanceUsage                      = "usage: go run . maintenance [retention status --inquiries-before <UTC-RFC3339> | retention apply --inquiries-before <UTC-RFC3339> --confirm-database <database> | purge-inquiry --id <positive-id> --confirm-database <database>]"
)

// Maintenance command errors are stable categories that never copy a supplied
// URL, database name, inquiry ID, or malformed argument into logs.
var (
	errMaintenanceCommandInvalid = errors.New(
		"maintenance command is invalid",
	)
	errMaintenanceDatabaseURLRequired = errors.New(
		"ZAFARMAND_MAINTENANCE_DATABASE_URL is required",
	)
	errMaintenanceOutputRequired = errors.New(
		"maintenance command requires an output writer",
	)
	errMaintenanceDatabaseConfigurationInvalid = errors.New(
		"maintenance PostgreSQL configuration is invalid",
	)
)

// maintenanceCommand contains only canonical, locally validated values. The
// confirmed database is compared with PostgreSQL's server-reported identity in
// the same transaction as every destructive operation.
type maintenanceCommand struct {
	// Action is one closed operation selected by the strict CLI grammar.
	Action string
	// InquiryCutoff is populated only for retention preview or application.
	InquiryCutoff time.Time
	// InquiryID is populated only for one reviewed targeted purge.
	InquiryID int64
	// ConfirmedDatabase must match current_database() before any deletion.
	ConfirmedDatabase string
}

// parseMaintenanceCommand accepts one exact grammar and rejects reordered,
// duplicate, unknown, or alternate-spelling flags before credentials are read.
func parseMaintenanceCommand(args []string) (maintenanceCommand, error) {
	return parseMaintenanceCommandAt(args, time.Now().UTC())
}

// parseMaintenanceCommandAt is the deterministic parser seam used by tests to
// evaluate past/future cutoff rules against one supplied UTC instant.
func parseMaintenanceCommandAt(
	args []string,
	now time.Time,
) (maintenanceCommand, error) {
	invalid := func() (maintenanceCommand, error) {
		return maintenanceCommand{}, fmt.Errorf(
			"%w; %s",
			errMaintenanceCommandInvalid,
			maintenanceUsage,
		)
	}

	if len(args) >= 2 && args[0] == "retention" {
		switch args[1] {
		case "status":
			if len(args) != 4 || args[2] != maintenanceCutoffFlag {
				return invalid()
			}
			cutoff, valid := parseCanonicalPastMaintenanceCutoff(
				args[3],
				now,
			)
			if !valid {
				return invalid()
			}

			return maintenanceCommand{
				Action:        maintenanceActionRetentionStatus,
				InquiryCutoff: cutoff,
			}, nil
		case "apply":
			if len(args) != 6 ||
				args[2] != maintenanceCutoffFlag ||
				args[4] != maintenanceDatabaseConfirmationFlag {
				return invalid()
			}
			cutoff, cutoffValid := parseCanonicalPastMaintenanceCutoff(
				args[3],
				now,
			)
			if !cutoffValid || !validMaintenanceDatabaseName(args[5]) {
				return invalid()
			}

			return maintenanceCommand{
				Action:            maintenanceActionRetentionApply,
				InquiryCutoff:     cutoff,
				ConfirmedDatabase: args[5],
			}, nil
		default:
			return invalid()
		}
	}

	if len(args) == 5 &&
		args[0] == "purge-inquiry" &&
		args[1] == maintenanceInquiryIDFlag &&
		args[3] == maintenanceDatabaseConfirmationFlag {
		inquiryID, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || inquiryID <= 0 ||
			strconv.FormatInt(inquiryID, 10) != args[2] ||
			!validMaintenanceDatabaseName(args[4]) {
			return invalid()
		}

		return maintenanceCommand{
			Action:            maintenanceActionPurgeInquiry,
			InquiryID:         inquiryID,
			ConfirmedDatabase: args[4],
		}, nil
	}

	return invalid()
}

// Canonical maintenance cutoffs use whole UTC seconds. Rejecting offsets,
// fractions, future instants, and alternate spellings keeps a destructive
// command reproducible in shell history and operational review.
func parseCanonicalPastMaintenanceCutoff(
	value string,
	now time.Time,
) (time.Time, bool) {
	cutoff, err := time.Parse(time.RFC3339, value)
	if err != nil || cutoff.IsZero() || !cutoff.Before(now.UTC()) {
		return time.Time{}, false
	}
	canonical := cutoff.UTC().Format(time.RFC3339)
	if canonical != value {
		return time.Time{}, false
	}

	return cutoff.UTC(), true
}

// validMaintenanceDatabaseName accepts a server identity that can be compared
// as data and rejects oversized, padded, NUL, or control-bearing input.
func validMaintenanceDatabaseName(value string) bool {
	if value == "" || len(value) > 63 || !utf8.ValidString(value) ||
		value != strings.TrimSpace(value) || strings.ContainsRune(value, '\x00') {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

// loadMaintenanceDatabaseConfig reads only the isolated maintenance URL plus
// the shared TLS policy. It never falls back to the runtime or migration URL.
func loadMaintenanceDatabaseConfig(
	lookup environmentLookup,
) (databaseConfig, error) {
	if lookup == nil {
		return databaseConfig{}, errMaintenanceDatabaseURLRequired
	}
	connectionString, exists := lookup(
		maintenanceDatabaseURLEnvironmentName,
	)
	connectionString = strings.TrimSpace(connectionString)
	if !exists || connectionString == "" {
		return databaseConfig{}, errMaintenanceDatabaseURLRequired
	}
	requireTLS := false
	if value, exists := lookup(databaseRequireTLSEnvironmentName); exists {
		switch value {
		case "true":
			requireTLS = true
		case "false":
			requireTLS = false
		default:
			return databaseConfig{}, errDatabaseRequireTLSInvalid
		}
	}

	return databaseConfig{
		connectionString: connectionString,
		pingTimeout:      defaultDatabasePingTimeout,
		requireTLS:       requireTLS,
	}, nil
}

// executeMaintenanceCommand owns the dedicated pool and exposes the complete
// operator boundary for main's command dispatch. No server repository receives
// the resulting deletion authority.
func executeMaintenanceCommand(
	ctx context.Context,
	command maintenanceCommand,
	lookup environmentLookup,
	output io.Writer,
) (err error) {
	if ctx == nil {
		return errMaintenanceCommandInvalid
	}
	if output == nil {
		return errMaintenanceOutputRequired
	}
	if !validMaintenanceCommand(command, time.Now().UTC()) {
		return errMaintenanceCommandInvalid
	}
	config, err := loadMaintenanceDatabaseConfig(lookup)
	if err != nil {
		return err
	}
	database, err := openPostgresDatabase(ctx, config)
	if err != nil {
		if errors.Is(err, errDatabaseConfigurationInvalid) {
			return errMaintenanceDatabaseConfigurationInvalid
		}

		return err
	}
	defer func() {
		if closeErr := database.Close(); err == nil && closeErr != nil {
			err = errors.New("close PostgreSQL maintenance pool")
		}
	}()

	repository, err := newPostgresMaintenanceRepository(database)
	if err != nil {
		return err
	}

	return runMaintenanceCommand(
		ctx,
		command,
		repository,
		output,
	)
}

// validMaintenanceCommand rechecks constructed commands at the execution
// boundary so callers cannot bypass the strict CLI parser with a partial value.
func validMaintenanceCommand(command maintenanceCommand, now time.Time) bool {
	switch command.Action {
	case maintenanceActionRetentionStatus:
		return command.InquiryID == 0 && command.ConfirmedDatabase == "" &&
			validMaintenanceCutoff(command.InquiryCutoff, now)
	case maintenanceActionRetentionApply:
		return command.InquiryID == 0 &&
			validMaintenanceCutoff(command.InquiryCutoff, now) &&
			validMaintenanceDatabaseName(command.ConfirmedDatabase)
	case maintenanceActionPurgeInquiry:
		return command.InquiryCutoff.IsZero() && command.InquiryID > 0 &&
			validMaintenanceDatabaseName(command.ConfirmedDatabase)
	default:
		return false
	}
}

// validMaintenanceCutoff requires an exact whole-second UTC instant in the past.
func validMaintenanceCutoff(cutoff time.Time, now time.Time) bool {
	return !cutoff.IsZero() && cutoff.Location() == time.UTC &&
		cutoff.Nanosecond() == 0 && cutoff.Before(now.UTC())
}

// runMaintenanceCommand gives repository work one bounded context and writes
// only aggregate or neutral results to the operator-provided output.
func runMaintenanceCommand(
	parent context.Context,
	command maintenanceCommand,
	repository maintenanceRepository,
	output io.Writer,
) error {
	if parent == nil || repository == nil || output == nil {
		return errMaintenanceCommandInvalid
	}
	ctx, cancel := context.WithTimeout(parent, maintenanceOperationTimeout)
	defer cancel()

	switch command.Action {
	case maintenanceActionRetentionStatus:
		result, err := repository.InspectRetention(
			ctx,
			command.InquiryCutoff,
		)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			output,
			"Retention status: %d expired session(s); %d archived inquiry/inquiries eligible before %s.\n",
			result.ExpiredSessions,
			result.ArchivedInquiries,
			command.InquiryCutoff.Format(time.RFC3339),
		)

		return err
	case maintenanceActionRetentionApply:
		result, err := repository.ApplyRetention(
			ctx,
			command.InquiryCutoff,
			command.ConfirmedDatabase,
		)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			output,
			"Retention applied: %d expired session(s) removed; %d archived inquiry/inquiries removed before %s.\n",
			result.ExpiredSessions,
			result.ArchivedInquiries,
			command.InquiryCutoff.Format(time.RFC3339),
		)

		return err
	case maintenanceActionPurgeInquiry:
		result, err := repository.PurgeInquiry(
			ctx,
			command.InquiryID,
			command.ConfirmedDatabase,
		)
		if err != nil {
			return err
		}
		message := "No eligible archived inquiry was removed.\n"
		if result.Purged {
			message = "One archived inquiry was removed.\n"
		}
		_, err = io.WriteString(output, message)

		return err
	default:
		return errMaintenanceCommandInvalid
	}
}
