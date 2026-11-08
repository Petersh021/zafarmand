package main

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func validAdminOIDCIdentityFixture() adminOIDCIdentity {
	return adminOIDCIdentity{
		Issuer:  "https://accounts.google.com",
		Subject: "109876543210987654321",
		UserID:  42,
	}
}

// TestAdminOIDCIdentityRepositoryLinksOnlyExistingActiveUsers verifies the
// INSERT ... SELECT contract and its neutral inactive/missing result.
func TestAdminOIDCIdentityRepositoryLinksOnlyExistingActiveUsers(t *testing.T) {
	identity := validAdminOIDCIdentityFixture()
	tests := []struct {
		name         string
		rowsAffected int64
		wantError    error
	}{
		{name: "linked", rowsAffected: 1},
		{name: "missing or inactive user", wantError: errAdminUserNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &adminExecutorRecordingStub{
				result: &adminSQLResultRecordingStub{
					rowsAffected: test.rowsAffected,
				},
			}
			repository := &postgresAdminOIDCIdentityRepository{
				executor: executor,
			}

			err := repository.LinkIdentity(t.Context(), identity)
			if !errors.Is(err, test.wantError) {
				t.Errorf("link identity: got %v, want %v", err, test.wantError)
			}
			if executor.query != linkAdminOIDCIdentitySQL ||
				!reflect.DeepEqual(
					executor.arguments,
					[]any{identity.Issuer, identity.Subject, identity.UserID},
				) {
				t.Errorf("link query: %#v", executor)
			}
		})
	}
}

// TestAdminOIDCIdentityRepositoryClassifiesOnlyNamedConflicts prevents an
// unrelated unique constraint from being mistaken for an existing binding.
func TestAdminOIDCIdentityRepositoryClassifiesOnlyNamedConflicts(t *testing.T) {
	tests := []struct {
		constraint string
		wantError  error
	}{
		{
			constraint: "admin_oidc_identities_pkey",
			wantError:  errAdminOIDCIdentityConflict,
		},
		{
			constraint: "admin_oidc_identities_user_issuer_unique",
			wantError:  errAdminOIDCIdentityConflict,
		},
		{
			constraint: "unrelated_unique_constraint",
			wantError:  errAdminOIDCIdentityRepositoryDatabaseFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.constraint, func(t *testing.T) {
			executor := &adminExecutorRecordingStub{
				execError: &pgconn.PgError{
					Code:           "23505",
					ConstraintName: test.constraint,
				},
			}
			repository := &postgresAdminOIDCIdentityRepository{
				executor: executor,
			}

			err := repository.LinkIdentity(
				t.Context(),
				validAdminOIDCIdentityFixture(),
			)
			if !errors.Is(err, test.wantError) {
				t.Errorf("constraint classification: got %v, want %v", err, test.wantError)
			}
		})
	}
}

// TestAdminOIDCIdentityRepositoryFindActiveAccount locks lookup to immutable
// provider identity and a password-free active-account projection.
func TestAdminOIDCIdentityRepositoryFindActiveAccount(t *testing.T) {
	identity := validAdminOIDCIdentityFixture()
	row := &adminRowScannerRecordingStub{
		scan: func(destinations ...any) error {
			*destinations[0].(*int64) = 42
			*destinations[1].(*string) = "owner@example.com"
			*destinations[2].(*string) = string(adminRoleOwner)

			return nil
		},
	}
	query := &adminQueryRowRecordingStub{row: row}
	repository := &postgresAdminOIDCIdentityRepository{queryRow: query.QueryRow}

	account, err := repository.FindActiveAccountByIdentity(
		t.Context(),
		identity.Issuer,
		identity.Subject,
	)
	if err != nil {
		t.Fatalf("find active OIDC account: %v", err)
	}
	want := adminAccount{ID: 42, Email: "owner@example.com", Role: adminRoleOwner}
	if account != want {
		t.Errorf("account: got %#v, want %#v", account, want)
	}
	if query.query != findActiveAdminAccountByOIDCIdentitySQL ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{identity.Issuer, identity.Subject},
		) {
		t.Errorf("identity lookup: %#v", query)
	}
	if strings.Contains(strings.ToLower(query.query), "password") ||
		strings.Contains(strings.ToLower(query.query), "token") {
		t.Error("OIDC lookup projects a credential or provider token")
	}
}

func TestAdminOIDCIdentityRepositoryNeutralizesUnusableBinding(t *testing.T) {
	identity := validAdminOIDCIdentityFixture()
	query := &adminQueryRowRecordingStub{
		row: &adminRowScannerRecordingStub{
			scan: func(...any) error { return sql.ErrNoRows },
		},
	}
	repository := &postgresAdminOIDCIdentityRepository{queryRow: query.QueryRow}

	account, err := repository.FindActiveAccountByIdentity(
		t.Context(),
		identity.Issuer,
		identity.Subject,
	)
	if !errors.Is(err, errAdminOIDCIdentityNotFound) {
		t.Errorf("unusable binding: got %v", err)
	}
	if account != (adminAccount{}) {
		t.Errorf("unusable binding returned account %#v", account)
	}
}

// TestAdminOIDCIdentityRepositoryRecordsAuthentication covers the exact one-row
// update and the neutral result for missing, unlinked, or deactivated users.
func TestAdminOIDCIdentityRepositoryRecordsAuthentication(t *testing.T) {
	identity := validAdminOIDCIdentityFixture()
	for _, test := range []struct {
		name         string
		rowsAffected int64
		wantError    error
	}{
		{name: "updated", rowsAffected: 1},
		{name: "not active or linked", wantError: errAdminOIDCIdentityNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &adminExecutorRecordingStub{
				result: &adminSQLResultRecordingStub{
					rowsAffected: test.rowsAffected,
				},
			}
			repository := &postgresAdminOIDCIdentityRepository{
				executor: executor,
			}

			err := repository.RecordAuthentication(
				t.Context(),
				identity.Issuer,
				identity.Subject,
			)
			if !errors.Is(err, test.wantError) {
				t.Errorf("record authentication: got %v, want %v", err, test.wantError)
			}
			if executor.query != recordAdminOIDCAuthenticationSQL ||
				!reflect.DeepEqual(
					executor.arguments,
					[]any{identity.Issuer, identity.Subject},
				) {
				t.Errorf("authentication update: %#v", executor)
			}
		})
	}
}

func TestAdminOIDCIdentityRepositoryRejectsMalformedIdentity(t *testing.T) {
	executor := &adminExecutorRecordingStub{}
	query := &adminQueryRowRecordingStub{}
	repository := &postgresAdminOIDCIdentityRepository{
		executor: executor,
		queryRow: query.QueryRow,
	}
	identity := validAdminOIDCIdentityFixture()
	identity.Subject = " padded-subject "

	if err := repository.LinkIdentity(t.Context(), identity); !errors.Is(
		err,
		errAdminOIDCIdentityInvalid,
	) {
		t.Errorf("invalid link: got %v", err)
	}
	if _, err := repository.FindActiveAccountByIdentity(
		t.Context(),
		identity.Issuer,
		identity.Subject,
	); !errors.Is(err, errAdminOIDCIdentityInvalid) {
		t.Errorf("invalid lookup: got %v", err)
	}
	if executor.calls != 0 || query.calls != 0 {
		t.Error("invalid identity reached PostgreSQL")
	}

	identity = validAdminOIDCIdentityFixture()
	identity.Subject = "subject\ncontrol"
	if err := repository.LinkIdentity(t.Context(), identity); !errors.Is(
		err,
		errAdminOIDCIdentityInvalid,
	) {
		t.Errorf("control-bearing subject: got %v", err)
	}
}

func TestNewPostgresAdminOIDCIdentityRepositoryRequiresDatabase(t *testing.T) {
	if _, err := newPostgresAdminOIDCIdentityRepository(nil); !errors.Is(
		err,
		errAdminOIDCIdentityRepositoryDatabaseRequired,
	) {
		t.Errorf("nil database: got %v", err)
	}
}
