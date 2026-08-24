package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// PostgreSQL pool limits provide one process-wide resource budget. Handler
// deadlines bound individual operations, while these limits prevent bursts or
// expensive authentication work from opening an unbounded number of server
// connections. The values are deliberately conservative for the current
// single-process deployment and can be reviewed with the Stage 26 platform.
const (
	postgresMaximumOpenConnections = 10
	postgresMaximumIdleConnections = 5
	postgresConnectionMaximumIdle  = 5 * time.Minute
	postgresConnectionMaximumAge   = 30 * time.Minute
)

// Database-opening errors intentionally classify local configuration and
// connectivity failures without retaining a credential-bearing driver cause.
var (
	// errDatabaseConfigurationInvalid avoids returning the driver's parsing
	// error because such errors may repeat parts of a credential-bearing URL.
	errDatabaseConfigurationInvalid = errors.New(
		"open PostgreSQL database: DATABASE_URL is invalid",
	)
	// errDatabaseUnavailable gives operators an actionable category without
	// exposing the configured host, username, password, or database name.
	errDatabaseUnavailable = errors.New(
		"verify PostgreSQL connection: database unavailable or credentials rejected",
	)
)

// openPostgresDatabase creates one database/sql pool and proves that it can
// reach PostgreSQL before returning it to the requesting process mode.
//
// stdlib.OpenDB creates a concurrency-safe handle from the already-validated
// pgx configuration but does not establish a network connection immediately.
// PingContext is therefore the real startup boundary. The caller owns the
// returned pool and must close it exactly once.
func openPostgresDatabase(
	ctx context.Context,
	config databaseConfig,
) (*sql.DB, error) {
	if config.pingTimeout <= 0 {
		return nil, errDatabaseConfigurationInvalid
	}
	parsedConfig, err := pgx.ParseConfig(config.connectionString)
	if err != nil {
		// Validate before pool construction so malformed, credential-bearing input is
		// classified locally without returning the parser's detailed error.
		return nil, errDatabaseConfigurationInvalid
	}
	if config.requireTLS && !postgresConfigRequiresTLS(parsedConfig) {
		// The stable category deliberately avoids saying which configured target or
		// fallback was plaintext because a connection plan may contain credentials.
		return nil, errDatabaseConfigurationInvalid
	}

	// Open the pool from the exact configuration already validated above. Passing
	// the original string to sql.Open would parse service files and environment
	// defaults a second time, creating a time-of-check/time-of-use gap in the TLS
	// decision.
	database := stdlib.OpenDB(*parsedConfig)

	configurePostgresPool(database)

	// The timeout bounds only connection verification. Migration statements and
	// request writes receive their own parent contexts after this startup check.
	pingContext, cancel := context.WithTimeout(
		ctx,
		config.pingTimeout,
	)
	defer cancel()

	if err := database.PingContext(pingContext); err != nil {
		// A failed pool never escapes this function, so close it here rather than
		// leaving its background resources for the caller to discover.
		_ = database.Close()
		if contextError := pingContext.Err(); contextError != nil {
			// Cancellation and timeout sentinels contain no connection information
			// and let callers distinguish operator interruption from bad credentials.
			return nil, fmt.Errorf(
				"verify PostgreSQL connection: %w",
				contextError,
			)
		}

		return nil, errDatabaseUnavailable
	}

	return database, nil
}

// configurePostgresPool applies the same bounded lifecycle to server,
// migration, bootstrap, and maintenance modes. Each command still owns and
// closes its pool; limiting every mode protects PostgreSQL even when operators
// accidentally run more than one process at once.
func configurePostgresPool(database *sql.DB) {
	if database == nil {
		return
	}

	database.SetMaxOpenConns(postgresMaximumOpenConnections)
	database.SetMaxIdleConns(postgresMaximumIdleConnections)
	database.SetConnMaxIdleTime(postgresConnectionMaximumIdle)
	database.SetConnMaxLifetime(postgresConnectionMaximumAge)
}

// postgresConfigRequiresTLS verifies the primary target and every pgx fallback
// before database/sql opens a socket. This never trusts URL text heuristics and
// therefore covers keyword/value DSNs and multi-host plans consistently.
func postgresConfigRequiresTLS(config *pgx.ConnConfig) bool {
	if config == nil || config.TLSConfig == nil {
		return false
	}

	for _, fallback := range config.Fallbacks {
		if fallback == nil || fallback.TLSConfig == nil {
			return false
		}
	}

	return true
}
