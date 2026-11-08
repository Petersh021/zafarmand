package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestAdminAccountRepositoryFindActiveAccount verifies that alternative login
// methods receive authorization data without retrieving a password verifier.
func TestAdminAccountRepositoryFindActiveAccount(t *testing.T) {
	ctx := context.WithValue(
		context.Background(),
		adminRepositoryContextKey{},
		"account-context",
	)
	row := &adminRowScannerRecordingStub{
		scan: func(destinations ...any) error {
			if len(destinations) != 3 {
				return errors.New("unexpected account destination count")
			}
			*destinations[0].(*int64) = 42
			*destinations[1].(*string) = "owner@example.com"
			*destinations[2].(*string) = string(adminRoleOwner)

			return nil
		},
	}
	query := &adminQueryRowRecordingStub{row: row}
	repository := &postgresAdminAccountRepository{queryRow: query.QueryRow}

	account, err := repository.FindActiveAccountByEmail(
		ctx,
		"  OWNER@Example.COM  ",
	)
	if err != nil {
		t.Fatalf("find active account: %v", err)
	}
	want := adminAccount{ID: 42, Email: "owner@example.com", Role: adminRoleOwner}
	if account != want {
		t.Errorf("account: got %#v, want %#v", account, want)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findActiveAdminAccountByEmailSQL ||
		!reflect.DeepEqual(query.arguments, []any{"owner@example.com"}) {
		t.Errorf("account query: %#v", query)
	}
	if strings.Contains(strings.ToLower(query.query), "password") {
		t.Error("password-free account query projects a password field")
	}
}

// TestAdminAccountRepositoryRejectsInvalidAndUnusableRows locks the neutral
// not-found result and fail-closed treatment of malformed stored data.
func TestAdminAccountRepositoryRejectsInvalidAndUnusableRows(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		scan      func(...any) error
		wantError error
		wantCalls int
	}{
		{
			name:      "invalid email",
			email:     "not-an-address",
			wantError: errAdminUserInvalid,
		},
		{
			name:  "missing or inactive",
			email: "owner@example.com",
			scan: func(...any) error {
				return sql.ErrNoRows
			},
			wantError: errAdminUserNotFound,
			wantCalls: 1,
		},
		{
			name:  "driver failure",
			email: "owner@example.com",
			scan: func(...any) error {
				return errors.New("private driver detail")
			},
			wantError: errAdminAccountRepositoryDatabaseFailed,
			wantCalls: 1,
		},
		{
			name:  "corrupt role",
			email: "owner@example.com",
			scan: func(destinations ...any) error {
				*destinations[0].(*int64) = 42
				*destinations[1].(*string) = "owner@example.com"
				*destinations[2].(*string) = "administrator"

				return nil
			},
			wantError: errAdminAccountRepositoryDatabaseFailed,
			wantCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &adminQueryRowRecordingStub{
				row: &adminRowScannerRecordingStub{scan: test.scan},
			}
			repository := &postgresAdminAccountRepository{
				queryRow: query.QueryRow,
			}

			account, err := repository.FindActiveAccountByEmail(
				t.Context(),
				test.email,
			)
			if !errors.Is(err, test.wantError) {
				t.Errorf("error: got %v, want %v", err, test.wantError)
			}
			if account != (adminAccount{}) {
				t.Errorf("failed account lookup returned %#v", account)
			}
			if query.calls != test.wantCalls {
				t.Errorf("query calls: got %d, want %d", query.calls, test.wantCalls)
			}
		})
	}
}

func TestNewPostgresAdminAccountRepositoryRequiresDatabase(t *testing.T) {
	if _, err := newPostgresAdminAccountRepository(nil); !errors.Is(
		err,
		errAdminAccountRepositoryDatabaseRequired,
	) {
		t.Errorf("nil database: got %v", err)
	}
}
