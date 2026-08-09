package main

import (
	"errors"
	"strings"
	"time"
)

// Database configuration constants centralize the only accepted variable name
// and the bounded connection-verification duration.
const (
	// databaseURLEnvironmentName is the one environment variable from which the
	// migration commands and the long-running server accept PostgreSQL connection
	// information.
	databaseURLEnvironmentName = "DATABASE_URL"
	// defaultDatabasePingTimeout bounds either operating mode's initial
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
}

// loadDatabaseConfig reads and validates the process's database environment
// configuration.
//
// Stage 14 uses the same explicit configuration for migrations and server
// startup. A single loader prevents subtly different parsing or secret-handling
// rules between those two database consumers.
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

	return databaseConfig{
		connectionString: connectionString,
		pingTimeout:      defaultDatabasePingTimeout,
	}, nil
}
