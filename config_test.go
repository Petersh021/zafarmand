package main

import (
	"errors"
	"strings"
	"testing"
)

// TestLoadDatabaseConfig verifies that the database configuration shared by
// migration commands and server startup accepts one trimmed connection string
// and rejects every missing-value representation.
func TestLoadDatabaseConfig(t *testing.T) {
	tests := []struct {
		// name identifies one environment-loading boundary.
		name string
		// lookup supplies the isolated result returned for DATABASE_URL.
		lookup environmentLookup
		// expectedConnectionString is nonempty only for the successful case.
		expectedConnectionString string
		// expectedRequireTLS is the strict transport declaration returned on success.
		expectedRequireTLS bool
		// expectedError identifies the stable, credential-safe failure.
		expectedError error
	}{
		{
			name: "trimmed value",
			lookup: func(name string) (string, bool) {
				if name == databaseURLEnvironmentName {
					return "  postgres://user:password@localhost/zafarmand  ", true
				}

				return "", false
			},
			expectedConnectionString: "postgres://user:password@localhost/zafarmand",
		},
		{
			name: "TLS required",
			lookup: func(name string) (string, bool) {
				switch name {
				case databaseURLEnvironmentName:
					return "postgres://user:password@database.example/zafarmand", true
				case databaseRequireTLSEnvironmentName:
					return "true", true
				default:
					return "", false
				}
			},
			expectedConnectionString: "postgres://user:password@database.example/zafarmand",
			expectedRequireTLS:       true,
		},
		{
			name:          "nil lookup",
			expectedError: errDatabaseURLRequired,
		},
		{
			name: "missing value",
			lookup: func(string) (string, bool) {
				return "", false
			},
			expectedError: errDatabaseURLRequired,
		},
		{
			name: "blank value",
			lookup: func(string) (string, bool) {
				return "  \t\r\n  ", true
			},
			expectedError: errDatabaseURLRequired,
		},
		{
			name: "invalid TLS declaration",
			lookup: func(name string) (string, bool) {
				if name == databaseURLEnvironmentName {
					return "postgres://localhost/zafarmand", true
				}
				return "TRUE", true
			},
			expectedError: errDatabaseRequireTLSInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadDatabaseConfig(test.lookup)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf(
					"error: got %v, want %v",
					err,
					test.expectedError,
				)
			}

			if config.connectionString != test.expectedConnectionString {
				t.Errorf(
					"connection string: got %q, want %q",
					config.connectionString,
					test.expectedConnectionString,
				)
			}

			if test.expectedError == nil &&
				config.pingTimeout != defaultDatabasePingTimeout {
				t.Errorf(
					"ping timeout: got %v, want %v",
					config.pingTimeout,
					defaultDatabasePingTimeout,
				)
			}
			if test.expectedError == nil &&
				config.requireTLS != test.expectedRequireTLS {
				t.Errorf(
					"require TLS: got %t, want %t",
					config.requireTLS,
					test.expectedRequireTLS,
				)
			}
		})
	}
}

// TestDatabaseConfigurationErrorDoesNotContainCredentials protects the
// operator's connection string from accidental inclusion in the stable error.
func TestDatabaseConfigurationErrorDoesNotContainCredentials(t *testing.T) {
	const secret = "stage13-secret-password"

	_, err := loadDatabaseConfig(
		func(string) (string, bool) {
			// LookupEnv normally returns an empty value when exists is false. Supplying
			// a credential marker anyway creates a meaningful regression seam: the
			// loader must reject the value without ever interpolating it into the error.
			return "postgres://user:" + secret + "@localhost/zafarmand", false
		},
	)
	if err == nil {
		t.Fatal("missing database configuration unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf(
			"configuration error exposes credentials: %q",
			err,
		)
	}
}
