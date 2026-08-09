package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	// Register pgx's database/sql compatibility driver under the "pgx" name.
	// PostgreSQL is shared by migrations, Contact writes, and admin authentication.
	_ "github.com/jackc/pgx/v5/stdlib"
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
// sql.Open creates a concurrency-safe handle but may not establish a network
// connection immediately. PingContext is therefore the real startup boundary.
// The caller owns the returned pool and must close it exactly once.
func openPostgresDatabase(
	ctx context.Context,
	config databaseConfig,
) (*sql.DB, error) {
	if config.pingTimeout <= 0 {
		return nil, errDatabaseConfigurationInvalid
	}
	if _, err := pgx.ParseConfig(config.connectionString); err != nil {
		// Validate before sql.Open so malformed, credential-bearing input is
		// classified locally without returning the parser's detailed error.
		return nil, errDatabaseConfigurationInvalid
	}

	database, err := sql.Open(
		"pgx",
		config.connectionString,
	)
	if err != nil {
		return nil, errDatabaseConfigurationInvalid
	}

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
