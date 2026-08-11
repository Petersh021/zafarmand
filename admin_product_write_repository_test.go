package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// recordingAdminProductWriteQuery captures one fixed statement and returns a
// controlled scanner without requiring PostgreSQL in ordinary unit tests.
type recordingAdminProductWriteQuery struct {
	// calls records how many database operations were attempted.
	calls int
	// context is the exact context forwarded by the repository.
	context context.Context
	// query is the trusted SQL supplied by the repository.
	query string
	// arguments are the positional values bound to the statement.
	arguments []any
	// row is returned to the repository for result scanning.
	row adminProductWriteRowScanner
}

// QueryRow records one invocation and implements adminProductWriteQueryRow.
func (query *recordingAdminProductWriteQuery) QueryRow(
	ctx context.Context,
	statement string,
	arguments ...any,
) adminProductWriteRowScanner {
	query.calls++
	query.context = ctx
	query.query = statement
	query.arguments = append([]any(nil), arguments...)

	return query.row
}

// adminProductWriteRowStub supplies CREATE or UPDATE result columns and can
// simulate driver and scanning failures.
type adminProductWriteRowStub struct {
	// result contains the ID and version copied on success.
	result adminProductWriteResult
	// productExists is the third UPDATE result column.
	productExists bool
	// coverResult contains the three cover-write revision columns.
	coverResult adminProductCoverWriteResult
	// scanError is returned before any destination is changed.
	scanError error
}

// Scan implements the two-column CREATE and three-column UPDATE projections.
func (row *adminProductWriteRowStub) Scan(destinations ...any) error {
	if row.scanError != nil {
		return row.scanError
	}

	switch len(destinations) {
	case 2:
		id, idOK := destinations[0].(*int64)
		version, versionOK := destinations[1].(*int64)
		if !idOK || !versionOK {
			return errors.New("create scan received unexpected destinations")
		}
		*id = row.result.ID
		*version = row.result.Version
	case 3:
		id, idOK := destinations[0].(*int64)
		version, versionOK := destinations[1].(*int64)
		exists, existsOK := destinations[2].(*bool)
		if !idOK || !versionOK || !existsOK {
			return errors.New("update scan received unexpected destinations")
		}
		*id = row.result.ID
		*version = row.result.Version
		*exists = row.productExists
	case 4:
		productID, productIDOK := destinations[0].(*int64)
		productVersion, productVersionOK := destinations[1].(*int64)
		coverVersion, coverVersionOK := destinations[2].(*int64)
		exists, existsOK := destinations[3].(*bool)
		if !productIDOK || !productVersionOK || !coverVersionOK || !existsOK {
			return errors.New("cover scan received unexpected destinations")
		}
		*productID = row.coverResult.ProductID
		*productVersion = row.coverResult.ProductVersion
		*coverVersion = row.coverResult.CoverVersion
		*exists = row.productExists
	default:
		return errors.New("write scan received unexpected column count")
	}

	return nil
}

// validAdminProductWriteInput returns one deterministic input satisfying every
// migration-owned editable constraint.
func validAdminProductWriteInput() adminProductWriteInput {
	return adminProductWriteInput{
		Slug:              "stage-20-chair",
		Name:              "Stage 20 Chair",
		Category:          "Furniture",
		SortOrder:         4,
		PublicationStatus: productPublicationStatusDraft,
	}
}

// TestNewPostgresAdminProductWriter verifies startup dependency validation and
// confirms construction installs the query adapter without performing a write.
func TestNewPostgresAdminProductWriter(t *testing.T) {
	writer, err := newPostgresAdminProductWriter(nil)
	if !errors.Is(err, errAdminProductWriterDatabaseRequired) || writer != nil {
		t.Fatalf("nil database result: writer=%#v err=%v", writer, err)
	}

	database := &sql.DB{}
	writer, err = newPostgresAdminProductWriter(database)
	if err != nil {
		t.Fatalf("construct writer: %v", err)
	}
	if writer == nil || writer.queryRow == nil {
		t.Fatal("constructed writer did not install its query adapter")
	}
}

// TestPostgresAdminProductWriterCreate verifies exact parameter binding and the
// returned database-owned identity/version pair.
func TestPostgresAdminProductWriterCreate(t *testing.T) {
	ctx := context.Background()
	input := validAdminProductWriteInput()
	query := &recordingAdminProductWriteQuery{
		row: &adminProductWriteRowStub{
			result: adminProductWriteResult{ID: 41, Version: 1},
		},
	}
	writer := &postgresAdminProductWriter{queryRow: query.QueryRow}

	result, err := writer.Create(ctx, input)
	if err != nil {
		t.Fatalf("create Product: %v", err)
	}
	if result != (adminProductWriteResult{ID: 41, Version: 1}) {
		t.Errorf("create result: got %#v", result)
	}
	if query.calls != 1 || query.context != ctx || query.query != createAdminProductSQL {
		t.Errorf("create invocation: calls=%d query=%q", query.calls, query.query)
	}
	wantArguments := []any{
		input.Slug,
		input.Name,
		input.Category,
		input.SortOrder,
		input.PublicationStatus,
		input.Description,
		input.Material,
		input.Dimensions,
	}
	if !reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("create arguments: got %#v, want %#v", query.arguments, wantArguments)
	}
}

// TestPostgresAdminProductWriterCreateRejectsInvalidInput proves validation
// happens before the database seam for every editable field and nil context.
func TestPostgresAdminProductWriterCreateRejectsInvalidInput(t *testing.T) {
	valid := validAdminProductWriteInput()
	tests := []struct {
		name  string
		ctx   context.Context
		input adminProductWriteInput
	}{
		{name: "nil context", input: valid},
		{name: "noncanonical slug", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Slug = "Bad Slug"; return value }()},
		{name: "untrimmed name", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Name = " Chair "; return value }()},
		{name: "empty category", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Category = ""; return value }()},
		{name: "zero order", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.SortOrder = 0; return value }()},
		{name: "large order", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.SortOrder = math.MaxInt32 + 1; return value }()},
		{name: "unsupported state", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.PublicationStatus = "deleted"; return value }()},
		{name: "untrimmed description", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Description = " description "; return value }()},
		{name: "description contains nul", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Description = "description\x00tail"; return value }()},
		{name: "long description", ctx: context.Background(), input: func() adminProductWriteInput {
			value := valid
			value.Description = strings.Repeat("d", productDescriptionMaximumLength+1)
			return value
		}()},
		{name: "untrimmed material", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Material = " oak "; return value }()},
		{name: "long material", ctx: context.Background(), input: func() adminProductWriteInput {
			value := valid
			value.Material = strings.Repeat("m", productMaterialMaximumLength+1)
			return value
		}()},
		{name: "untrimmed dimensions", ctx: context.Background(), input: func() adminProductWriteInput { value := valid; value.Dimensions = " 800 mm "; return value }()},
		{name: "long dimensions", ctx: context.Background(), input: func() adminProductWriteInput {
			value := valid
			value.Dimensions = strings.Repeat("x", productDimensionsMaximumLength+1)
			return value
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminProductWriteQuery{}
			writer := &postgresAdminProductWriter{queryRow: query.QueryRow}
			result, err := writer.Create(test.ctx, test.input)
			if !errors.Is(err, errAdminProductWriteInvalid) {
				t.Fatalf("error: got %v, want invalid", err)
			}
			if result != (adminProductWriteResult{}) || query.calls != 0 {
				t.Errorf("invalid create reached dependency: result=%#v calls=%d", result, query.calls)
			}
		})
	}
}

// TestPostgresAdminProductWriterCreateClassifiesFailures verifies the one safe
// slug conflict and redacts every other result or driver failure.
func TestPostgresAdminProductWriterCreateClassifiesFailures(t *testing.T) {
	tests := []struct {
		name    string
		row     adminProductWriteRowScanner
		wantErr error
	}{
		{name: "nil row", wantErr: errAdminProductWriteFailed},
		{name: "slug conflict", row: &adminProductWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "products_slug_unique"}}, wantErr: errAdminProductSlugConflict},
		{name: "other unique constraint", row: &adminProductWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "future_unique"}}, wantErr: errAdminProductWriteFailed},
		{name: "unsafe driver text", row: &adminProductWriteRowStub{scanError: errors.New("password=secret rejected-value")}, wantErr: errAdminProductWriteFailed},
		{name: "zero result", row: &adminProductWriteRowStub{}, wantErr: errAdminProductWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminProductWriteQuery{row: test.row}
			writer := &postgresAdminProductWriter{queryRow: query.QueryRow}
			result, err := writer.Create(context.Background(), validAdminProductWriteInput())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error: got %v, want %v", err, test.wantErr)
			}
			if result != (adminProductWriteResult{}) {
				t.Errorf("failed create returned %#v", result)
			}
			if err != test.wantErr {
				t.Errorf("failure wrapped unsafe detail: %#v", err)
			}
		})
	}
}

// TestPostgresAdminProductWriterUpdate verifies the exact optimistic-lock SQL,
// arguments, and revision increment accepted by the repository.
func TestPostgresAdminProductWriterUpdate(t *testing.T) {
	ctx := context.Background()
	input := validAdminProductWriteInput()
	query := &recordingAdminProductWriteQuery{
		row: &adminProductWriteRowStub{
			result:        adminProductWriteResult{ID: 17, Version: 6},
			productExists: true,
		},
	}
	writer := &postgresAdminProductWriter{queryRow: query.QueryRow}

	result, err := writer.Update(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("update Product: %v", err)
	}
	if result != (adminProductWriteResult{ID: 17, Version: 6}) {
		t.Errorf("update result: got %#v", result)
	}
	wantArguments := []any{
		int64(17),
		int64(5),
		input.Slug,
		input.Name,
		input.Category,
		input.SortOrder,
		input.PublicationStatus,
		input.Description,
		input.Material,
		input.Dimensions,
	}
	if query.calls != 1 || query.context != ctx || query.query != updateAdminProductSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("update invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}
}

// TestPostgresAdminProductWriterUpdateClassifiesOutcomes keeps missing rows,
// stale versions, slug collisions, and operational failures distinct and safe.
func TestPostgresAdminProductWriterUpdateClassifiesOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		row     adminProductWriteRowScanner
		wantErr error
	}{
		{name: "missing", row: &adminProductWriteRowStub{}, wantErr: errAdminProductNotFound},
		{name: "stale", row: &adminProductWriteRowStub{productExists: true}, wantErr: errAdminProductWriteConflict},
		{name: "slug conflict", row: &adminProductWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "products_slug_unique"}}, wantErr: errAdminProductSlugConflict},
		{name: "driver failure", row: &adminProductWriteRowStub{scanError: errors.New("unsafe SQL detail")}, wantErr: errAdminProductWriteFailed},
		{name: "wrong revision", row: &adminProductWriteRowStub{result: adminProductWriteResult{ID: 7, Version: 9}, productExists: true}, wantErr: errAdminProductWriteFailed},
		{name: "wrong identity", row: &adminProductWriteRowStub{result: adminProductWriteResult{ID: 8, Version: 4}, productExists: true}, wantErr: errAdminProductWriteFailed},
		{name: "result without existence", row: &adminProductWriteRowStub{result: adminProductWriteResult{ID: 7, Version: 4}}, wantErr: errAdminProductWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminProductWriteQuery{row: test.row}
			writer := &postgresAdminProductWriter{queryRow: query.QueryRow}
			result, err := writer.Update(
				context.Background(),
				7,
				3,
				validAdminProductWriteInput(),
			)
			if !errors.Is(err, test.wantErr) || result != (adminProductWriteResult{}) {
				t.Fatalf("outcome: result=%#v err=%v, want %v", result, err, test.wantErr)
			}
			if err != test.wantErr {
				t.Errorf("failure wrapped dependency detail: %#v", err)
			}
		})
	}
}

// TestPostgresAdminProductWriterUpdateRejectsInvalidBoundary verifies identity,
// revision, input, context, and receiver guards before any SQL call.
func TestPostgresAdminProductWriterUpdateRejectsInvalidBoundary(t *testing.T) {
	input := validAdminProductWriteInput()
	tests := []struct {
		name    string
		writer  *postgresAdminProductWriter
		ctx     context.Context
		id      int64
		version int64
		wantErr error
	}{
		{name: "nil context", writer: &postgresAdminProductWriter{}, id: 1, version: 1, wantErr: errAdminProductWriteInvalid},
		{name: "zero identity", writer: &postgresAdminProductWriter{}, ctx: context.Background(), version: 1, wantErr: errAdminProductWriteInvalid},
		{name: "zero version", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, wantErr: errAdminProductWriteInvalid},
		{name: "nil receiver", ctx: context.Background(), id: 1, version: 1, wantErr: errAdminProductWriteFailed},
		{name: "missing query", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, version: 1, wantErr: errAdminProductWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.writer.Update(test.ctx, test.id, test.version, input)
			if !errors.Is(err, test.wantErr) || result != (adminProductWriteResult{}) {
				t.Fatalf("boundary outcome: result=%#v err=%v, want %v", result, err, test.wantErr)
			}
		})
	}
}
