package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
