package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	// adminNewPasswordEnvironmentName is the only accepted source for a reset
	// credential. Keeping it distinct prevents an old bootstrap value from being
	// reused accidentally.
	adminNewPasswordEnvironmentName = "ZAFARMAND_ADMIN_NEW_PASSWORD"
	// adminResetPasswordUsage documents the complete credential-rotation grammar.
	// The plaintext password remains environment-only and is never a process
	// argument.
	adminResetPasswordUsage = "usage: go run . admin reset-password --email <email>"
)

// Stable reset errors keep email addresses, plaintext passwords, hashes, SQL,
// and PostgreSQL driver diagnostics outside the process log.
var (
	// errAdminResetPasswordArgumentsInvalid covers every malformed flag shape or
	// mailbox without repeating the rejected value.
	errAdminResetPasswordArgumentsInvalid = errors.New(
		"admin reset-password arguments are invalid",
	)
	// errAdminResetPasswordOutputRequired prevents a successful rotation when the
	// operator cannot receive its generic confirmation.
	errAdminResetPasswordOutputRequired = errors.New(
		"admin reset-password requires an output writer",
	)
	// errAdminNewPasswordRequired identifies a missing environment-only reset
	// credential without including its value.
	errAdminNewPasswordRequired = errors.New(
		"ZAFARMAND_ADMIN_NEW_PASSWORD is required",
	)
	// errAdminResetPasswordHashFailed replaces password-manager diagnostics that
	// could otherwise retain plaintext credential material.
	errAdminResetPasswordHashFailed = errors.New(
		"prepare replacement administrator credential: password hashing failed",
	)
	// errAdminResetPasswordFailed deliberately makes an absent/inactive account
	// indistinguishable from any other persistence failure.
	errAdminResetPasswordFailed = errors.New(
		"reset administrator password: database operation failed",
	)
	// errAdminResetPasswordContextRequired rejects direct callers that provide no
	// cancellation or deadline boundary.
	errAdminResetPasswordContextRequired = errors.New(
		"admin reset-password requires a context",
	)
)

// adminResetPasswordCommand contains only the canonical account selector. The
// replacement password enters later through ZAFARMAND_ADMIN_NEW_PASSWORD.
type adminResetPasswordCommand struct {
	// Email is the lowercase, trimmed mailbox returned by normalizeAdminEmail.
	Email string
}

// adminPasswordResetter is the narrow persistence capability needed by the
// offline operator command. The production repository also revokes every live
// session in the same PostgreSQL statement as the hash update.
type adminPasswordResetter interface {
	ResetPassword(context.Context, string, string) error
}

// parseAdminResetPasswordCommand accepts exactly one --email pair. Unknown,
// duplicate, positional, password-bearing, or malformed arguments share one
// redacted usage failure.
func parseAdminResetPasswordCommand(
	args []string,
) (adminResetPasswordCommand, error) {
	if len(args) != 2 || args[0] != "--email" {
		return adminResetPasswordCommand{}, adminResetPasswordUsageError()
	}

	normalizedEmail, err := normalizeAdminEmail(args[1])
	if err != nil {
		return adminResetPasswordCommand{}, adminResetPasswordUsageError()
	}

	return adminResetPasswordCommand{Email: normalizedEmail}, nil
}

// adminResetPasswordUsageError adds actionable syntax while preserving a
// stable errors.Is category for tests and callers.
func adminResetPasswordUsageError() error {
	return fmt.Errorf(
		"%w; %s",
		errAdminResetPasswordArgumentsInvalid,
		adminResetPasswordUsage,
	)
}

// executeAdminResetPasswordCommand owns the complete offline rotation: local
// validation, environment loading, database lifecycle, password hashing, the
// atomic hash/session write, and credential-free confirmation output.
func executeAdminResetPasswordCommand(
	ctx context.Context,
	command adminResetPasswordCommand,
	lookup environmentLookup,
	output io.Writer,
) (err error) {
	if ctx == nil {
		return errAdminResetPasswordContextRequired
	}
	if output == nil {
		return errAdminResetPasswordOutputRequired
	}

	normalizedEmail, normalizeErr := normalizeAdminEmail(command.Email)
	if normalizeErr != nil {
		return adminResetPasswordUsageError()
	}
	command.Email = normalizedEmail

	if lookup == nil {
		return errAdminNewPasswordRequired
	}
	replacementPassword, exists := lookup(adminNewPasswordEnvironmentName)
	if !exists || replacementPassword == "" {
		return errAdminNewPasswordRequired
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
			err = errors.New("close PostgreSQL administrator reset pool")
		}
	}()

	repository, err := newPostgresAdminPasswordResetRepository(database)
	if err != nil {
		return errAdminResetPasswordFailed
	}

	if err := resetAdminPassword(
		ctx,
		command,
		replacementPassword,
		newAdminPasswordManager(),
		repository,
	); err != nil {
		return err
	}

	_, err = fmt.Fprintln(
		output,
		"Administrator password reset; existing sessions revoked.",
	)

	return err
}

// resetAdminPassword converts plaintext to a one-way verifier exactly once
// before allowing only the encoded value to cross the repository boundary.
func resetAdminPassword(
	ctx context.Context,
	command adminResetPasswordCommand,
	replacementPassword string,
	hasher adminPasswordHasher,
	repository adminPasswordResetter,
) error {
	if hasher == nil {
		return errAdminResetPasswordHashFailed
	}
	if repository == nil {
		return errAdminResetPasswordFailed
	}

	passwordHash, err := hasher.Hash(replacementPassword)
	if err != nil || passwordHash == "" {
		return errAdminResetPasswordHashFailed
	}

	if err := repository.ResetPassword(
		ctx,
		command.Email,
		passwordHash,
	); err != nil {
		return errAdminResetPasswordFailed
	}

	return nil
}
