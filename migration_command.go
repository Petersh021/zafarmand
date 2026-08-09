package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Program, migration, and administrator command constants define the complete
// accepted CLI grammar in one auditable location.
const (
	// programCommandServe identifies the existing public HTTP-server mode.
	programCommandServe = "serve"
	// programCommandMigrate identifies the explicit PostgreSQL schema mode.
	programCommandMigrate = "migrate"
	// programCommandAdmin identifies one explicit administrator maintenance mode.
	programCommandAdmin = "admin"
	// migrationActionUp applies every validated pending migration.
	migrationActionUp = "up"
	// migrationActionStatus reports applied and pending catalog entries.
	migrationActionStatus = "status"
	// migrationActionDown rolls back only the latest applied migration.
	migrationActionDown = "down"
	// migrationDownConfirmation is required literally before destructive SQL can
	// reach the migration runner.
	migrationDownConfirmation = "--confirm"
	// programUsage is returned for every unsupported command shape.
	programUsage = "usage: go run . [migrate [up|status|down --confirm] | admin create-user --email <email> --role <owner|editor>]"
)

// Migration command errors identify confirmation and dependency boundaries
// that callers may test with errors.Is.
var (
	// errMigrationConfirmationRequired distinguishes a missing destructive-action
	// acknowledgement from a generally malformed command.
	errMigrationConfirmationRequired = errors.New(
		"migrate down requires the explicit --confirm argument",
	)
	// errMigrationOutputRequired protects command execution from a nil writer.
	errMigrationOutputRequired = errors.New(
		"migration command requires an output writer",
	)
)

// programCommand is the validated instruction selected from process arguments.
type programCommand struct {
	// Name selects the server, migration, or administrator maintenance mode.
	Name string
	// MigrationAction is populated only when Name is programCommandMigrate.
	MigrationAction string
	// AdminCreateUser is populated only for the strict admin create-user mode.
	AdminCreateUser adminCreateUserCommand
}

// parseProgramCommand converts process arguments into one strict, documented
// execution mode.
//
// No arguments preserves the existing `go run .` server behavior. `migrate`
// alone is a convenience alias for `migrate up`; rollback always requires both
// its action and the literal confirmation flag. The separate admin branch
// delegates its password-free flag grammar to parseAdminCreateUserCommand.
func parseProgramCommand(args []string) (programCommand, error) {
	if len(args) == 0 {
		return programCommand{Name: programCommandServe}, nil
	}
	if args[0] == programCommandAdmin {
		if len(args) < 2 || args[1] != "create-user" {
			return programCommand{}, fmt.Errorf(
				"unknown admin command; %s",
				programUsage,
			)
		}

		createUser, err := parseAdminCreateUserCommand(args[2:])
		if err != nil {
			return programCommand{}, err
		}

		return programCommand{
			Name:            programCommandAdmin,
			AdminCreateUser: createUser,
		}, nil
	}
	if args[0] != programCommandMigrate {
		return programCommand{}, fmt.Errorf(
			"unknown command %q; %s",
			args[0],
			programUsage,
		)
	}

	if len(args) == 1 {
		return programCommand{
			Name:            programCommandMigrate,
			MigrationAction: migrationActionUp,
		}, nil
	}

	switch args[1] {
	case migrationActionUp, migrationActionStatus:
		if len(args) != 2 {
			return programCommand{}, fmt.Errorf(
				"unexpected arguments after migrate %s; %s",
				args[1],
				programUsage,
			)
		}

		return programCommand{
			Name:            programCommandMigrate,
			MigrationAction: args[1],
		}, nil
	case migrationActionDown:
		if len(args) != 3 ||
			args[2] != migrationDownConfirmation {
			return programCommand{}, errMigrationConfirmationRequired
		}

		return programCommand{
			Name:            programCommandMigrate,
			MigrationAction: migrationActionDown,
		}, nil
	default:
		return programCommand{}, fmt.Errorf(
			"unknown migration action %q; %s",
			args[1],
			programUsage,
		)
	}
}

// executeMigrationCommand owns the database pool, embedded catalog, runner,
// and human-readable output for one validated migration action.
//
// The returned error is named so deferred database cleanup can be surfaced
// after successful work without replacing a more important migration failure.
func executeMigrationCommand(
	ctx context.Context,
	command programCommand,
	lookup environmentLookup,
	output io.Writer,
) (err error) {
	if output == nil {
		return errMigrationOutputRequired
	}
	if command.Name != programCommandMigrate {
		return errors.New(
			"execute migration command received a non-migration mode",
		)
	}
	switch command.MigrationAction {
	case migrationActionUp,
		migrationActionStatus,
		migrationActionDown:
		// Continue only after the action has passed the same closed set accepted
		// by parseProgramCommand.
	default:
		return fmt.Errorf(
			"unsupported migration action %q",
			command.MigrationAction,
		)
	}

	// Validate the compiled schema history before reading credentials or opening
	// a network connection. A broken build should fail entirely locally.
	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		return fmt.Errorf(
			"load embedded migration catalog: %w",
			err,
		)
	}

	config, err := loadDatabaseConfig(lookup)
	if err != nil {
		return err
	}

	database, err := openPostgresDatabase(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := database.Close(); err == nil && closeErr != nil {
			err = errors.New(
				"close PostgreSQL migration pool",
			)
		}
	}()

	runner, err := newMigrationRunner(database, catalog)
	if err != nil {
		return err
	}

	switch command.MigrationAction {
	case migrationActionUp:
		return writeMigrationUpResult(
			ctx,
			output,
			runner,
		)
	case migrationActionStatus:
		return writeMigrationStatus(
			ctx,
			output,
			runner,
		)
	case migrationActionDown:
		return writeMigrationDownResult(
			ctx,
			output,
			runner,
		)
	default:
		// The earlier validation makes this branch unreachable, but retaining a
		// defensive default keeps future action additions from silently succeeding.
		return errors.New("unreachable migration action")
	}
}

// writeMigrationUpResult applies pending migrations and reports each completed
// version without printing SQL or database configuration.
func writeMigrationUpResult(
	ctx context.Context,
	output io.Writer,
	runner *migrationRunner,
) error {
	applied, err := runner.Up(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		_, err = fmt.Fprintln(
			output,
			"No pending migrations.",
		)

		return err
	}

	for _, migration := range applied {
		if _, err := fmt.Fprintf(
			output,
			"Applied %06d_%s.\n",
			migration.Version,
			migration.Name,
		); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		output,
		"Applied %d migration(s).\n",
		len(applied),
	)

	return err
}

// writeMigrationStatus prints one stable line per embedded migration after the
// runner has validated that database history is an exact catalog prefix.
func writeMigrationStatus(
	ctx context.Context,
	output io.Writer,
	runner *migrationRunner,
) error {
	statuses, err := runner.Status(ctx)
	if err != nil {
		return err
	}

	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = "applied"
		}

		if _, err := fmt.Fprintf(
			output,
			"%06d %-8s %s\n",
			status.Migration.Version,
			state,
			status.Migration.Name,
		); err != nil {
			return err
		}
	}

	return nil
}

// writeMigrationDownResult rolls back one version or reports that the ledger is
// already empty.
func writeMigrationDownResult(
	ctx context.Context,
	output io.Writer,
	runner *migrationRunner,
) error {
	migration, err := runner.Down(ctx)
	if errors.Is(err, errNoAppliedMigrations) {
		_, writeErr := fmt.Fprintln(
			output,
			"No applied migrations to roll back.",
		)

		return writeErr
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		output,
		"Rolled back %06d_%s.\n",
		migration.Version,
		migration.Name,
	)

	return err
}
