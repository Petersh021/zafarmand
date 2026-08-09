package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	// adminPasswordEnvironmentName is the only accepted source for the initial
	// administrator's plaintext password. Keeping it out of process arguments
	// prevents ordinary command-history and process-list inspection from exposing
	// the credential.
	adminPasswordEnvironmentName = "ZAFARMAND_ADMIN_PASSWORD"
	// adminCreateUserUsage documents the complete, closed bootstrap grammar. The
	// two flag-value pairs may appear in either order, but neither may be omitted
	// or repeated.
	adminCreateUserUsage = "usage: go run . admin create-user --email <email> --role <owner|editor>"
)

// Admin bootstrap errors provide stable, credential-free categories for the
// process boundary and its tests.
var (
	// errAdminCreateUserArgumentsInvalid covers every malformed flag shape and
	// invalid value without echoing a potentially private email address.
	errAdminCreateUserArgumentsInvalid = errors.New(
		"admin create-user arguments are invalid",
	)
	// errAdminCreateUserOutputRequired prevents a successful database write when
	// the command has no destination for its operator confirmation.
	errAdminCreateUserOutputRequired = errors.New(
		"admin create-user requires an output writer",
	)
	// errAdminPasswordRequired identifies a missing or empty environment-only
	// password without ever returning its value.
	errAdminPasswordRequired = errors.New(
		"ZAFARMAND_ADMIN_PASSWORD is required",
	)
	// errAdminCreateUserHashFailed deliberately replaces a password manager error.
	// A future implementation error must not be allowed to repeat its plaintext
	// input through the CLI boundary.
	errAdminCreateUserHashFailed = errors.New(
		"prepare administrator credential: password hashing failed",
	)
	// errAdminCreateUserFailed replaces repository errors because PostgreSQL
	// diagnostics can include the conflicting administrator email or hash.
	errAdminCreateUserFailed = errors.New(
		"create administrator: database operation failed",
	)
	// errAdminCreateUserContextRequired protects database and hashing operations
	// from a nil context supplied by a direct caller.
	errAdminCreateUserContextRequired = errors.New(
		"admin create-user requires a context",
	)
)

// adminCreateUserCommand is the validated, normalized instruction for creating
// the first database-backed administrator.
//
// It deliberately contains no password field. The plaintext credential enters
// only at execution through environmentLookup and is converted to a hash before
// the repository receives an adminUser.
type adminCreateUserCommand struct {
	// Email is the canonical lowercase mailbox returned by normalizeAdminEmail.
	Email string
	// Role is one exact value from the closed owner/editor authorization set.
	Role adminRole
}

// adminPasswordHasher is the smallest password-manager behavior required by
// the bootstrap command. adminPasswordManager satisfies it, while tests can
// inject a recorder that proves the plaintext is hashed exactly once.
type adminPasswordHasher interface {
	Hash(string) (string, error)
}

// adminUserCreator is the narrow write behavior needed after password hashing.
// postgresAdminRepository satisfies it without coupling command tests to a live
// PostgreSQL server.
type adminUserCreator interface {
	CreateUser(context.Context, adminUser) error
}

// parseAdminCreateUserCommand validates arguments following `admin create-user`.
//
// Exactly one --email and one --role pair are accepted in either order. Unknown
// flags, positional values, duplicates, missing values, unsupported roles, and
// invalid mailboxes all return the same redacted usage error. The password is
// intentionally outside this grammar.
func parseAdminCreateUserCommand(
	args []string,
) (adminCreateUserCommand, error) {
	if len(args) != 4 {
		return adminCreateUserCommand{}, adminCreateUserUsageError()
	}

	var (
		email    string
		role     adminRole
		hasEmail bool
		hasRole  bool
	)

	for index := 0; index < len(args); index += 2 {
		flag := args[index]
		value := args[index+1]

		switch flag {
		case "--email":
			if hasEmail {
				return adminCreateUserCommand{}, adminCreateUserUsageError()
			}

			normalizedEmail, err := normalizeAdminEmail(value)
			if err != nil {
				return adminCreateUserCommand{}, adminCreateUserUsageError()
			}

			email = normalizedEmail
			hasEmail = true
		case "--role":
			if hasRole {
				return adminCreateUserCommand{}, adminCreateUserUsageError()
			}

			switch adminRole(value) {
			case adminRoleOwner:
				role = adminRoleOwner
			case adminRoleEditor:
				role = adminRoleEditor
			default:
				return adminCreateUserCommand{}, adminCreateUserUsageError()
			}

			hasRole = true
		default:
			return adminCreateUserCommand{}, adminCreateUserUsageError()
		}
	}

	if !hasEmail || !hasRole {
		return adminCreateUserCommand{}, adminCreateUserUsageError()
	}

	return adminCreateUserCommand{
		Email: email,
		Role:  role,
	}, nil
}

// adminCreateUserUsageError adds actionable syntax to a stable error without
// including any supplied flag value.
func adminCreateUserUsageError() error {
	return fmt.Errorf(
		"%w; %s",
		errAdminCreateUserArgumentsInvalid,
		adminCreateUserUsage,
	)
}

// executeAdminCreateUserCommand owns the complete one-user bootstrap lifecycle:
// local validation, environment loading, PostgreSQL pool creation, password
// hashing, repository insertion, confirmation output, and pool cleanup.
//
// The named error lets a close failure be surfaced only after otherwise
// successful work. Neither success output nor returned errors include the
// administrator email, plaintext password, password hash, or database URL.
func executeAdminCreateUserCommand(
	ctx context.Context,
	command adminCreateUserCommand,
	lookup environmentLookup,
	output io.Writer,
) (err error) {
	if ctx == nil {
		return errAdminCreateUserContextRequired
	}
	if output == nil {
		return errAdminCreateUserOutputRequired
	}

	// Revalidate direct callers rather than assuming every command came through
	// the parser. Canonicalizing here also ensures the repository sees one login
	// identity even if a test or future dispatcher constructs the value itself.
	normalizedEmail, normalizeErr := normalizeAdminEmail(command.Email)
	if normalizeErr != nil {
		return adminCreateUserUsageError()
	}
	command.Email = normalizedEmail

	switch command.Role {
	case adminRoleOwner, adminRoleEditor:
		// Continue only for the same closed role set accepted by the parser.
	default:
		return adminCreateUserUsageError()
	}

	if lookup == nil {
		return errAdminPasswordRequired
	}
	password, exists := lookup(adminPasswordEnvironmentName)
	if !exists || password == "" {
		return errAdminPasswordRequired
	}

	databaseConfig, err := loadDatabaseConfig(lookup)
	if err != nil {
		return err
	}

	database, err := openPostgresDatabase(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := database.Close(); err == nil && closeErr != nil {
			// database/sql close errors contain no outcome needed by later cleanup.
			// Returning a generic boundary still avoids leaking driver configuration.
			err = errors.New("close PostgreSQL administrator pool")
		}
	}()

	repository, err := newPostgresAdminRepository(database)
	if err != nil {
		// Construction errors are configuration defects, but the command keeps its
		// external error independent of repository or credential details.
		return errAdminCreateUserFailed
	}

	if err := createAdminUser(
		ctx,
		command,
		password,
		newAdminPasswordManager(),
		repository,
	); err != nil {
		return err
	}

	// The confirmation intentionally omits the account identifier and role. The
	// operator already supplied them, and repeating them would place personal
	// information into captured deployment logs.
	_, err = fmt.Fprintln(output, "Administrator account created.")

	return err
}

// createAdminUser performs the credential transformation and the single
// repository call after execution has acquired production dependencies.
//
// Separating this local boundary lets ordinary unit tests prove that plaintext
// is hashed exactly once, only the hash reaches persistence, and dependency
// errors are redacted without opening PostgreSQL.
func createAdminUser(
	ctx context.Context,
	command adminCreateUserCommand,
	password string,
	hasher adminPasswordHasher,
	repository adminUserCreator,
) error {
	if hasher == nil {
		return errAdminCreateUserHashFailed
	}
	if repository == nil {
		return errAdminCreateUserFailed
	}

	passwordHash, err := hasher.Hash(password)
	if err != nil {
		return errAdminCreateUserHashFailed
	}
	if passwordHash == "" {
		return errAdminCreateUserHashFailed
	}

	user := adminUser{
		Email:        command.Email,
		PasswordHash: passwordHash,
		Role:         command.Role,
	}
	if err := repository.CreateUser(ctx, user); err != nil {
		return errAdminCreateUserFailed
	}

	return nil
}
