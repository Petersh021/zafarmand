package main

import (
	"errors"
	"strings"
	"time"
)

// Database configuration constants centralize the accepted environment names
// and the bounded connection-verification duration.
const (
	// databaseURLEnvironmentName supplies PostgreSQL connection information to
	// migration, administrator-bootstrap, and long-running server modes.
	databaseURLEnvironmentName = "DATABASE_URL"
	// databaseRequireTLSEnvironmentName lets an operator reject every PostgreSQL
	// connection plan that contains a plaintext fallback. Local development may
	// leave it absent; remote production databases should set it to exact `true`.
	databaseRequireTLSEnvironmentName = "ZAFARMAND_REQUIRE_DATABASE_TLS"
	// defaultDatabasePingTimeout bounds every database-consuming mode's initial
	// connectivity check so a stopped or unreachable server fails promptly.
	defaultDatabasePingTimeout = 5 * time.Second
)

// Database configuration errors are stable, credential-free categories that
// callers and tests can inspect without parsing driver text.
var (
	// errDatabaseURLRequired is returned without including a connection string,
	// ensuring configuration errors never echo database credentials.
	errDatabaseURLRequired = errors.New(
		"DATABASE_URL is required",
	)
	// errDatabaseRequireTLSInvalid rejects permissive boolean spellings so a
	// deployment typo cannot silently weaken database transport policy.
	errDatabaseRequireTLSInvalid = errors.New(
		"ZAFARMAND_REQUIRE_DATABASE_TLS must be exactly true or false",
	)
)

// environmentLookup describes the small part of os.LookupEnv used by
// configuration loading.
//
// Naming the function shape lets tests provide isolated environment values
// without mutating process-wide state or depending on a dotenv package.
type environmentLookup func(string) (string, bool)

// databaseConfig contains the private values needed to create and verify one
// PostgreSQL database/sql pool.
//
// Its fields stay unexported so other layers cannot casually print the
// connection string or treat the startup timeout as public application data.
type databaseConfig struct {
	// connectionString is the PostgreSQL URL or keyword/value string supplied by
	// the operator. Code must never include it in logs or returned errors.
	connectionString string
	// pingTimeout limits only the initial connectivity check, not migrations.
	pingTimeout time.Duration
	// requireTLS rejects a parsed pgx connection plan if its primary target or
	// any fallback could use plaintext PostgreSQL transport.
	requireTLS bool
}

// loadDatabaseConfig reads and validates the process's database environment
// configuration.
//
// Migration, administrator-bootstrap, and server modes use this same explicit
// configuration. Offline retention has a separate credential loader but shares
// the exact TLS declaration and pool-opening policy.
func loadDatabaseConfig(
	lookup environmentLookup,
) (databaseConfig, error) {
	if lookup == nil {
		return databaseConfig{}, errDatabaseURLRequired
	}

	connectionString, exists := lookup(
		databaseURLEnvironmentName,
	)
	connectionString = strings.TrimSpace(connectionString)
	if !exists || connectionString == "" {
		return databaseConfig{}, errDatabaseURLRequired
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
