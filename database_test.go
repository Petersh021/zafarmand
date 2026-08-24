package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestConfigurePostgresPoolBoundsOpenConnections protects the resource ceiling
// that prevents one process from exhausting PostgreSQL under request load.
func TestConfigurePostgresPoolBoundsOpenConnections(t *testing.T) {
	database, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatalf("create unopened PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close unopened PostgreSQL pool")
		}
	})

	configurePostgresPool(database)

	if got := database.Stats().MaxOpenConnections; got != postgresMaximumOpenConnections {
		t.Errorf(
			"maximum open connections: got %d, want %d",
			got,
			postgresMaximumOpenConnections,
		)
	}
	if postgresMaximumIdleConnections <= 0 ||
		postgresMaximumIdleConnections > postgresMaximumOpenConnections ||
		postgresConnectionMaximumIdle <= 0 ||
		postgresConnectionMaximumAge <= postgresConnectionMaximumIdle {
		t.Error("PostgreSQL pool lifecycle constants are internally inconsistent")
	}
}

// TestPostgresConfigRequiresTLS covers every supported sslmode shape and a
// multi-host plan. A single plaintext primary or fallback must reject the
// complete connection plan when transport security is required.
func TestPostgresConfigRequiresTLS(t *testing.T) {
	tests := []struct {
		name             string
		connectionString string
		want             bool
	}{
		{
			name:             "disable",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=disable",
		},
		{
			name:             "allow includes plaintext",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=allow",
		},
		{
			name:             "prefer includes plaintext fallback",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=prefer",
		},
		{
			name:             "require",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=require",
			want:             true,
		},
		{
			name:             "verify ca",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=verify-ca",
			want:             true,
		},
		{
			name:             "verify full",
			connectionString: "postgres://user:password@db.example.test/zafarmand?sslmode=verify-full",
			want:             true,
		},
		{
			name:             "multi host require",
			connectionString: "host=db-one.example.test,db-two.example.test port=5432,5433 user=user password=password dbname=zafarmand sslmode=require",
			want:             true,
		},
		{
			name:             "multi host prefer",
			connectionString: "host=db-one.example.test,db-two.example.test port=5432,5433 user=user password=password dbname=zafarmand sslmode=prefer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := pgx.ParseConfig(test.connectionString)
			if err != nil {
				t.Fatalf("parse test connection plan: %v", err)
			}
			if got := postgresConfigRequiresTLS(config); got != test.want {
				t.Errorf("TLS-only plan: got %t, want %t", got, test.want)
			}
		})
	}

	if postgresConfigRequiresTLS(nil) {
		t.Error("nil PostgreSQL configuration was treated as TLS-only")
	}
}

// TestOpenPostgresDatabaseRejectsUnsafeConfiguration verifies local validation
// and confirms returned errors never echo a credential-bearing connection URL.
func TestOpenPostgresDatabaseRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		// name labels one configuration failure.
		name string
		// config is passed directly to the production pool constructor.
		config databaseConfig
		// expectedError is the stable, redacted error category.
		expectedError error
	}{
		{
			name: "nonpositive timeout",
			config: databaseConfig{
				connectionString: "postgres://user:stage13-secret@localhost/zafarmand",
			},
			expectedError: errDatabaseConfigurationInvalid,
		},
		{
			name: "malformed connection string",
			config: databaseConfig{
				connectionString: "postgres://user:stage13-secret@[%invalid",
				pingTimeout:      time.Millisecond,
			},
			expectedError: errDatabaseConfigurationInvalid,
		},
		{
			name: "required TLS has plaintext fallback",
			config: databaseConfig{
				connectionString: "postgres://user:stage13-secret@localhost/zafarmand?sslmode=prefer",
				pingTimeout:      time.Millisecond,
				requireTLS:       true,
			},
			expectedError: errDatabaseConfigurationInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, err := openPostgresDatabase(
				context.Background(),
				test.config,
			)
			if database != nil {
				_ = database.Close()
				t.Fatal("invalid database configuration returned a pool")
			}
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}
			if strings.Contains(
				err.Error(),
				"stage13-secret",
			) {
				t.Errorf(
					"database error exposes credentials: %q",
					err,
				)
			}
		})
	}
}

// TestOpenPostgresDatabasePreservesCancellation verifies that an interrupted
// connection attempt remains distinguishable without exposing its URL.
func TestOpenPostgresDatabasePreservesCancellation(t *testing.T) {
	const secret = "stage13-cancelled-secret"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	database, err := openPostgresDatabase(
		ctx,
		databaseConfig{
			connectionString: "postgres://user:" + secret + "@localhost/zafarmand",
			pingTimeout:      time.Second,
		},
	)
	if database != nil {
		_ = database.Close()
		t.Fatal("cancelled connection attempt returned a pool")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want context cancellation", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("cancellation error exposes credentials: %q", err)
	}
}
