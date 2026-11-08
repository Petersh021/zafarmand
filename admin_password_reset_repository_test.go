package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestNewPostgresAdminPasswordResetRepository verifies dependency rejection
// and confirms the valid constructor adapts the caller-owned pool.
func TestNewPostgresAdminPasswordResetRepository(t *testing.T) {
	repository, err := newPostgresAdminPasswordResetRepository(nil)
	if !errors.Is(err, errAdminPasswordResetRepositoryDatabaseRequired) {
		t.Fatalf("nil database error: got %v", err)
	}
	if repository != nil {
		t.Errorf("nil database repository: got %#v, want nil", repository)
	}

	database := new(sql.DB)
	repository, err = newPostgresAdminPasswordResetRepository(database)
	if err != nil {
		t.Fatalf("valid database: %v", err)
	}
	if repository.queryRow == nil {
		t.Error("constructor did not adapt the supplied pool")
	}
}

// TestPostgresAdminPasswordResetRepositoryResetPassword verifies the exact
// atomic SQL, normalized selector, encoded hash, bounded result, and context.
func TestPostgresAdminPasswordResetRepositoryResetPassword(t *testing.T) {
	row := &adminRowScannerRecordingStub{scan: func(destinations ...any) error {
		if len(destinations) != 2 {
			return errors.New("expected user ID and revoked-session count")
		}
		*destinations[0].(*int64) = 42
		*destinations[1].(*int64) = 3

		return nil
	}}
	queryRow := &adminQueryRowRecordingStub{row: row}
	repository := &postgresAdminPasswordResetRepository{
		queryRow: queryRow.QueryRow,
	}
	ctx := context.WithValue(
		context.Background(),
		adminRepositoryContextKey{},
		"reset-password",
	)
	passwordHash := "pbkdf2-sha256$v=1$i=600000$salt$key"

	if err := repository.ResetPassword(
		ctx,
		" OWNER@Example.COM ",
		passwordHash,
	); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if queryRow.calls != 1 || queryRow.context != ctx || row.scanCalls != 1 {
		t.Errorf(
			"query/scan/context: got %d/%d/%v",
			queryRow.calls,
			row.scanCalls,
			queryRow.context,
		)
	}
	if queryRow.query != resetAdminPasswordAndRevokeSessionsSQL {
		t.Errorf(
			"query:\n%s\nwant:\n%s",
			queryRow.query,
			resetAdminPasswordAndRevokeSessionsSQL,
		)
	}
	if !reflect.DeepEqual(
		queryRow.arguments,
		[]any{"owner@example.com", passwordHash},
	) {
		t.Errorf("arguments: got %#v", queryRow.arguments)
	}
}

// TestPostgresAdminPasswordResetRepositoryRejectsInvalidInput proves malformed
// email/hash values cannot reach PostgreSQL.
func TestPostgresAdminPasswordResetRepositoryRejectsInvalidInput(t *testing.T) {
	validHash := "pbkdf2-sha256$v=1$i=600000$salt$key"
	tests := []struct {
		name  string
		email string
		hash  string
	}{
		{name: "invalid email", email: "invalid", hash: validHash},
		{name: "empty hash", email: "owner@example.com"},
		{name: "padded hash", email: "owner@example.com", hash: " encoded "},
		{name: "NUL hash", email: "owner@example.com", hash: "encoded\x00hash"},
		{name: "oversized hash", email: "owner@example.com", hash: strings.Repeat("h", 256)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryRow := &adminQueryRowRecordingStub{}
			repository := &postgresAdminPasswordResetRepository{
				queryRow: queryRow.QueryRow,
			}
			err := repository.ResetPassword(
				context.Background(),
				test.email,
				test.hash,
			)
			if !errors.Is(err, errAdminPasswordResetRepositoryInvalid) {
				t.Fatalf("error: got %v, want invalid-input sentinel", err)
			}
			if queryRow.calls != 0 {
				t.Errorf("query calls: got %d, want 0", queryRow.calls)
			}
		})
	}
}

// TestPostgresAdminPasswordResetRepositoryFailures collapses missing accounts,
// malformed results, and driver detail into one credential-free error.
func TestPostgresAdminPasswordResetRepositoryFailures(t *testing.T) {
	const privateEmail = "private.owner@example.com"
	const privateHash = "pbkdf2-sha256$private-reset-hash"
	unsafeDetail := privateEmail + " " + privateHash
	tests := []struct {
		name       string
		ctx        context.Context
		repository *postgresAdminPasswordResetRepository
	}{
		{name: "nil repository", ctx: context.Background()},
		{name: "nil context", repository: &postgresAdminPasswordResetRepository{}},
		{name: "nil query dependency", ctx: context.Background(), repository: &postgresAdminPasswordResetRepository{}},
		{name: "nil row", ctx: context.Background(), repository: &postgresAdminPasswordResetRepository{queryRow: (&adminQueryRowRecordingStub{}).QueryRow}},
		{
			name: "account absent or inactive",
			ctx:  context.Background(),
			repository: &postgresAdminPasswordResetRepository{queryRow: (&adminQueryRowRecordingStub{row: &adminRowScannerRecordingStub{
				scan: func(...any) error { return sql.ErrNoRows },
			}}).QueryRow},
		},
		{
			name: "driver failure",
			ctx:  context.Background(),
			repository: &postgresAdminPasswordResetRepository{queryRow: (&adminQueryRowRecordingStub{row: &adminRowScannerRecordingStub{
				scan: func(...any) error { return errors.New(unsafeDetail) },
			}}).QueryRow},
		},
		{
			name: "invalid returned user ID",
			ctx:  context.Background(),
			repository: &postgresAdminPasswordResetRepository{queryRow: (&adminQueryRowRecordingStub{row: &adminRowScannerRecordingStub{
				scan: func(destinations ...any) error {
					*destinations[0].(*int64) = 0
					*destinations[1].(*int64) = 1
					return nil
				},
			}}).QueryRow},
		},
		{
			name: "invalid revoked count",
			ctx:  context.Background(),
			repository: &postgresAdminPasswordResetRepository{queryRow: (&adminQueryRowRecordingStub{row: &adminRowScannerRecordingStub{
				scan: func(destinations ...any) error {
					*destinations[0].(*int64) = 42
					*destinations[1].(*int64) = -1
					return nil
				},
			}}).QueryRow},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.repository.ResetPassword(
				test.ctx,
				privateEmail,
				privateHash,
			)
			if !errors.Is(err, errAdminPasswordResetRepositoryFailed) {
				t.Fatalf("error: got %v, want repository failure", err)
			}
			if strings.Contains(err.Error(), privateEmail) ||
				strings.Contains(err.Error(), privateHash) {
				t.Errorf("error exposes private detail: %q", err)
			}
		})
	}
}
