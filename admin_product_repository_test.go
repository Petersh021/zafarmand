package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// adminProductContextKey is a private comparable key used to prove that both
// repository methods preserve the caller's exact request context.
type adminProductContextKey struct{}

// adminProductQueryStub records one list query and returns controlled rows or
// a database-like error without needing a PostgreSQL server.
type adminProductQueryStub struct {
	// rows is the iterator returned to List and may be non-nil with queryError.
	rows adminProductRows
	// queryError simulates a database/sql or driver failure.
	queryError error
	// calls records how many list statements were attempted.
	calls int
	// context records the exact context forwarded by List.
	context context.Context
	// query records the complete fixed SQL statement.
	query string
	// arguments records any positional values; the list query should have none.
	arguments []any
}

// Query implements the narrow production query function and captures its full
// invocation before returning the configured result.
func (stub *adminProductQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (adminProductRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.rows, stub.queryError
}

// adminProductRowsStub exposes configured Products through the exact iterator
// used by List. Scan, iteration, and close failures are independently testable.
type adminProductRowsStub struct {
	// products are exposed in their supplied order.
	products []adminProductRecord
	// scanErrorAt is the zero-based failing row; -1 disables scan failure.
	scanErrorAt int
	// scanError is returned when Scan reaches scanErrorAt.
	scanError error
	// iterationError is returned after Next exhausts the configured rows.
	iterationError error
	// closeError simulates a result-finalization failure.
	closeError error
	// nextIndex identifies the next row that Next may expose.
	nextIndex int
	// currentIndex identifies the row currently available to Scan.
	currentIndex int
	// closeCalls proves every acquired iterator is finalized.
	closeCalls int
}

// newAdminProductRowsStub initializes the disabled scan index explicitly,
// because -1 rather than zero represents no failure.
func newAdminProductRowsStub(products []adminProductRecord) *adminProductRowsStub {
	return &adminProductRowsStub{
		products:     products,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances through the finite configured Product slice.
func (stub *adminProductRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.products) {
		return false
	}

	stub.currentIndex = stub.nextIndex
	stub.nextIndex++

	return true
}

// Scan copies one Product into the seventeen exact destinations selected by the
// protected list query or returns the configured failure.
func (stub *adminProductRowsStub) Scan(destinations ...any) error {
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.products) {
		return errors.New("admin product list scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}
	if len(destinations) != 17 {
		return errors.New("admin product list scan expected seventeen destinations")
	}

	product := stub.products[stub.currentIndex]
	id, idOK := destinations[0].(*int64)
	slug, slugOK := destinations[1].(*string)
	name, nameOK := destinations[2].(*string)
	category, categoryOK := destinations[3].(*string)
	sortOrder, sortOrderOK := destinations[4].(*int)
	status, statusOK := destinations[5].(*string)
	description, descriptionOK := destinations[6].(*string)
	material, materialOK := destinations[7].(*string)
	dimensions, dimensionsOK := destinations[8].(*string)
	version, versionOK := destinations[14].(*int64)
	createdAt, createdAtOK := destinations[15].(*time.Time)
	updatedAt, updatedAtOK := destinations[16].(*time.Time)
	if !idOK || !slugOK || !nameOK || !categoryOK || !sortOrderOK ||
		!statusOK || !descriptionOK || !materialOK || !dimensionsOK ||
		!versionOK || !createdAtOK || !updatedAtOK {
		return errors.New("admin product list scan received unexpected destinations")
	}

	*id = product.ID
	*slug = product.Slug
	*name = product.Name
	*category = product.Category
	*sortOrder = product.SortOrder
	*status = product.PublicationStatus
	*description = product.Description
	*material = product.Material
	*dimensions = product.Dimensions
	if err := setProductCoverMetadataDestinations(
		product.Cover,
		destinations[9:14],
	); err != nil {
		return err
	}
	*version = product.Version
	*createdAt = product.CreatedAt
	*updatedAt = product.UpdatedAt

	return nil
}

// Err returns the controlled post-iteration failure.
func (stub *adminProductRowsStub) Err() error {
	return stub.iterationError
}

// Close records finalization and returns the controlled close result.
func (stub *adminProductRowsStub) Close() error {
	stub.closeCalls++

	return stub.closeError
}

// adminProductQueryRowStub records one detail query and returns a controlled
// row scanner.
type adminProductQueryRowStub struct {
	// row is returned to FindByID.
	row adminProductRowScanner
	// calls records how many detail statements were attempted.
	calls int
	// context records the caller's exact context.
	context context.Context
	// query records the complete fixed SQL statement.
	query string
	// arguments records the bound positive Product ID.
	arguments []any
}

// QueryRow implements the production detail seam and captures its invocation.
func (stub *adminProductQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) adminProductRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// adminProductRowStub copies one configured detail or returns a database-like
// scanning failure.
type adminProductRowStub struct {
	// product contains every successful destination value.
	product adminProductRecord
	// scanError simulates no rows, decoding failure, or a driver error.
	scanError error
}

// adminProductScannerFunc adapts one focused closure to the protected Product
// scanner interface for malformed nullable-projection tests.
type adminProductScannerFunc func(...any) error

// Scan delegates to the configured focused scanner behavior.
func (scanner adminProductScannerFunc) Scan(destinations ...any) error {
	return scanner(destinations...)
}

// Scan implements the seventeen-column detail projection used by FindByID.
func (stub *adminProductRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 17 {
		return errors.New("admin product detail scan expected seventeen destinations")
	}

	id, idOK := destinations[0].(*int64)
	slug, slugOK := destinations[1].(*string)
	name, nameOK := destinations[2].(*string)
	category, categoryOK := destinations[3].(*string)
	sortOrder, sortOrderOK := destinations[4].(*int)
	status, statusOK := destinations[5].(*string)
	description, descriptionOK := destinations[6].(*string)
	material, materialOK := destinations[7].(*string)
	dimensions, dimensionsOK := destinations[8].(*string)
	version, versionOK := destinations[14].(*int64)
	createdAt, createdAtOK := destinations[15].(*time.Time)
	updatedAt, updatedAtOK := destinations[16].(*time.Time)
	if !idOK || !slugOK || !nameOK || !categoryOK || !sortOrderOK ||
		!statusOK || !descriptionOK || !materialOK || !dimensionsOK ||
		!versionOK || !createdAtOK || !updatedAtOK {
		return errors.New("admin product detail scan received unexpected destinations")
	}

	*id = stub.product.ID
	*slug = stub.product.Slug
	*name = stub.product.Name
	*category = stub.product.Category
	*sortOrder = stub.product.SortOrder
	*status = stub.product.PublicationStatus
	*description = stub.product.Description
	*material = stub.product.Material
	*dimensions = stub.product.Dimensions
	if err := setProductCoverMetadataDestinations(
		stub.product.Cover,
		destinations[9:14],
	); err != nil {
		return err
	}
	*version = stub.product.Version
	*createdAt = stub.product.CreatedAt
	*updatedAt = stub.product.UpdatedAt

	return nil
}

// validAdminProductRecord returns one deterministic migration-6 record. Tests
// vary one field at a time to isolate a defensive rule.
func validAdminProductRecord(
	id int64,
	slug string,
	sortOrder int,
	status string,
) adminProductRecord {
	createdAt := time.Date(2033, time.January, 2, 3, 4, 5, 0, time.UTC)

	return adminProductRecord{
		ID:                id,
		Slug:              slug,
		Name:              "Stage Nineteen Chair",
		Category:          "Furniture",
		SortOrder:         sortOrder,
		PublicationStatus: status,
		Version:           1,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt.Add(time.Hour),
	}
}

// TestNewPostgresAdminProductReader verifies dependency validation and proves
// that construction installs both adapters without opening a connection.
func TestNewPostgresAdminProductReader(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		reader, err := newPostgresAdminProductReader(nil)
		if !errors.Is(err, errAdminProductReaderDatabaseRequired) {
			t.Fatalf("error: got %v, want database-required sentinel", err)
		}
		if reader != nil {
			t.Errorf("reader: got %#v, want nil", reader)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		// The zero-value pool is never queried; construction only installs adapters.
		reader, err := newPostgresAdminProductReader(new(sql.DB))
		if err != nil {
			t.Fatalf("create admin product reader: %v", err)
		}
		if reader == nil || reader.query == nil || reader.queryRow == nil {
			t.Fatalf("reader adapters were not completely installed: %#v", reader)
		}
	})
}

// TestPostgresAdminProductReaderListsAllStates verifies fixed SQL, exact context
// propagation, deterministic mapping, and iterator cleanup.
func TestPostgresAdminProductReaderListsAllStates(t *testing.T) {
	expected := []adminProductRecord{
		validAdminProductRecord(2, "draft-chair", 1, productPublicationStatusDraft),
		validAdminProductRecord(1, "published-chair", 2, publishedProductStatus),
		validAdminProductRecord(3, "archived-chair", 2, productPublicationStatusArchived),
	}
	rows := newAdminProductRowsStub(expected)
	query := &adminProductQueryStub{rows: rows}
	reader := &postgresAdminProductReader{query: query.Query}
	ctx := context.WithValue(t.Context(), adminProductContextKey{}, "preserved")

	actual, err := reader.List(ctx)
	if err != nil {
		t.Fatalf("list admin products: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("products: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx || query.query != listAdminProductsSQL {
		t.Errorf("list invocation: got calls=%d context=%p query=%q", query.calls, query.context, query.query)
	}
	if len(query.arguments) != 0 {
		t.Errorf("list arguments: got %#v, want none", query.arguments)
	}
	if rows.closeCalls != 1 {
		t.Errorf("row close calls: got %d, want 1", rows.closeCalls)
	}
	if actual == nil || len(actual) != cap(actual) {
		t.Errorf("list result: got len=%d cap=%d, want allocated tight slice", len(actual), cap(actual))
	}
}

// TestPostgresAdminProductReaderRejectsUnsafeLists exercises result-lifecycle,
// ordering, uniqueness, and stored-value failures without allowing private
// diagnostics through.
func TestPostgresAdminProductReaderRejectsUnsafeLists(t *testing.T) {
	unsafeDetail := "private-product-driver-detail"
	valid := validAdminProductRecord(1, "valid-product", 1, publishedProductStatus)

	tests := []struct {
		// name identifies the isolated failure boundary.
		name string
		// reader is the deliberately malformed or failing dependency.
		reader *postgresAdminProductReader
	}{
		{name: "nil receiver", reader: nil},
		{name: "missing query function", reader: &postgresAdminProductReader{}},
		{name: "query error", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			return nil, errors.New(unsafeDetail)
		}}},
		{name: "nil rows", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			return nil, nil
		}}},
		{name: "scan error", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			rows := newAdminProductRowsStub([]adminProductRecord{valid})
			rows.scanErrorAt = 0
			rows.scanError = errors.New(unsafeDetail)
			return rows, nil
		}}},
		{name: "iteration error", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			rows := newAdminProductRowsStub([]adminProductRecord{valid})
			rows.iterationError = errors.New(unsafeDetail)
			return rows, nil
		}}},
		{name: "close error", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			rows := newAdminProductRowsStub([]adminProductRecord{valid})
			rows.closeError = errors.New(unsafeDetail)
			return rows, nil
		}}},
		{name: "invalid status", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			invalid := valid
			invalid.PublicationStatus = "future"
			return newAdminProductRowsStub([]adminProductRecord{invalid}), nil
		}}},
		{name: "invalid version", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			invalid := valid
			invalid.Version = 0
			return newAdminProductRowsStub([]adminProductRecord{invalid}), nil
		}}},
		{name: "invalid description", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			invalid := valid
			invalid.Description = strings.Repeat("d", productDescriptionMaximumLength+1)
			return newAdminProductRowsStub([]adminProductRecord{invalid}), nil
		}}},
		{name: "invalid cover metadata", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			invalid := valid
			invalid.Cover = &productCoverMetadata{Version: 1, Width: 4, Height: 3}
			return newAdminProductRowsStub([]adminProductRecord{invalid}), nil
		}}},
		{name: "unordered", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			later := validAdminProductRecord(2, "later-product", 2, publishedProductStatus)
			return newAdminProductRowsStub([]adminProductRecord{later, valid}), nil
		}}},
		{name: "duplicate identity", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			duplicate := validAdminProductRecord(1, "other-product", 2, publishedProductStatus)
			return newAdminProductRowsStub([]adminProductRecord{valid, duplicate}), nil
		}}},
		{name: "duplicate slug", reader: &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
			duplicate := validAdminProductRecord(2, valid.Slug, 2, publishedProductStatus)
			return newAdminProductRowsStub([]adminProductRecord{valid, duplicate}), nil
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			products, err := test.reader.List(t.Context())
			if !errors.Is(err, errAdminProductReadFailed) {
				t.Fatalf("error: got %v, want read-failed sentinel", err)
			}
			if products != nil {
				t.Errorf("products: got %#v, want nil", products)
			}
			if strings.Contains(err.Error(), unsafeDetail) {
				t.Error("safe error exposes private dependency detail")
			}
		})
	}

	reader := &postgresAdminProductReader{query: func(context.Context, string, ...any) (adminProductRows, error) {
		t.Fatal("nil context reached query")
		return nil, nil
	}}
	if _, err := reader.List(nil); !errors.Is(err, errAdminProductInvalidQuery) {
		t.Errorf("nil context error: got %v, want invalid-query sentinel", err)
	}
}

// TestPostgresAdminProductReaderFindsByID verifies exact parameterization,
// context propagation, and complete detail mapping.
func TestPostgresAdminProductReaderFindsByID(t *testing.T) {
	expected := validAdminProductRecord(19, "stage-nineteen-product", 4, productPublicationStatusDraft)
	query := &adminProductQueryRowStub{row: &adminProductRowStub{product: expected}}
	reader := &postgresAdminProductReader{queryRow: query.QueryRow}
	ctx := context.WithValue(t.Context(), adminProductContextKey{}, "detail")

	actual, err := reader.FindByID(ctx, expected.ID)
	if err != nil {
		t.Fatalf("find admin product: %v", err)
	}
	if actual != expected {
		t.Errorf("product: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx || query.query != findAdminProductByIDSQL {
		t.Errorf("detail invocation: got calls=%d context=%p query=%q", query.calls, query.context, query.query)
	}
	if !reflect.DeepEqual(query.arguments, []any{int64(19)}) {
		t.Errorf("detail arguments: got %#v, want [19]", query.arguments)
	}
}

// TestPostgresAdminProductReaderFindFailures distinguishes a missing row from
// invalid input and collapses all other dependency failures safely.
func TestPostgresAdminProductReaderFindFailures(t *testing.T) {
	unsafeDetail := "private-admin-product-scan-detail"

	t.Run("invalid input", func(t *testing.T) {
		reader := &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			t.Fatal("invalid input reached query")
			return nil
		}}
		for _, test := range []struct {
			// name identifies the rejected argument combination.
			name string
			// context is nil only for the nil-context boundary.
			context context.Context
			// id is the supplied internal identity.
			id int64
		}{
			{name: "nil context", context: nil, id: 1},
			{name: "zero identity", context: t.Context(), id: 0},
			{name: "negative identity", context: t.Context(), id: -1},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, err := reader.FindByID(test.context, test.id)
				if !errors.Is(err, errAdminProductInvalidQuery) {
					t.Errorf("error: got %v, want invalid-query sentinel", err)
				}
			})
		}
	})

	t.Run("not found", func(t *testing.T) {
		reader := &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			return &adminProductRowStub{scanError: sql.ErrNoRows}
		}}
		_, err := reader.FindByID(t.Context(), 1)
		if !errors.Is(err, errAdminProductNotFound) {
			t.Errorf("error: got %v, want not-found sentinel", err)
		}
	})

	for _, test := range []struct {
		// name identifies the malformed dependency result.
		name string
		// reader supplies that result.
		reader *postgresAdminProductReader
	}{
		{name: "nil receiver", reader: nil},
		{name: "missing query function", reader: &postgresAdminProductReader{}},
		{name: "nil row", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner { return nil }}},
		{name: "driver error", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			return &adminProductRowStub{scanError: errors.New(unsafeDetail)}
		}}},
		{name: "mismatched identity", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			return &adminProductRowStub{product: validAdminProductRecord(2, "other-product", 1, publishedProductStatus)}
		}}},
		{name: "malformed row", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			product := validAdminProductRecord(1, "valid-product", 1, "future")
			return &adminProductRowStub{product: product}
		}}},
		{name: "malformed rich content", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			product := validAdminProductRecord(1, "valid-product", 1, publishedProductStatus)
			product.Material = " untrimmed"
			return &adminProductRowStub{product: product}
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			product, err := test.reader.FindByID(t.Context(), 1)
			if !errors.Is(err, errAdminProductReadFailed) {
				t.Fatalf("error: got %v, want read-failed sentinel", err)
			}
			if product != (adminProductRecord{}) {
				t.Errorf("product: got %#v, want zero value", product)
			}
			if strings.Contains(err.Error(), unsafeDetail) {
				t.Error("safe error exposes private dependency detail")
			}
		})
	}
}

// TestScanAdminProductRejectsPartialCoverProjection proves a corrupt LEFT JOIN
// cannot turn one present nullable cover column into an absent or valid cover.
func TestScanAdminProductRejectsPartialCoverProjection(t *testing.T) {
	expected := validAdminProductRecord(1, "valid-product", 1, publishedProductStatus)
	base := &adminProductRowStub{product: expected}
	scanner := adminProductScannerFunc(func(destinations ...any) error {
		if err := base.Scan(destinations...); err != nil {
			return err
		}
		version := destinations[9].(*sql.NullInt64)
		*version = sql.NullInt64{Int64: 1, Valid: true}

		return nil
	})

	if product, err := scanAdminProduct(scanner); !errors.Is(
		err,
		errAdminProductReadFailed,
	) || product != (adminProductRecord{}) {
		t.Fatalf("partial protected cover projection: product=%#v err=%v", product, err)
	}
}

// TestAdminProductValidationVocabulary documents the exact stored lifecycle
// states and ordering relation trusted by protected views.
func TestAdminProductValidationVocabulary(t *testing.T) {
	for _, status := range []string{
		productPublicationStatusDraft,
		publishedProductStatus,
		productPublicationStatusArchived,
	} {
		if !isValidProductPublicationStatus(status) {
			t.Errorf("status %q should be valid", status)
		}
	}
	for _, status := range []string{"", "Published", "reviewed", " archived "} {
		if isValidProductPublicationStatus(status) {
			t.Errorf("status %q should be invalid", status)
		}
	}

	first := validAdminProductRecord(4, "first-product", 3, publishedProductStatus)
	byID := validAdminProductRecord(5, "second-product", 3, publishedProductStatus)
	byOrder := validAdminProductRecord(1, "third-product", 4, publishedProductStatus)
	if !adminProductFollows(byID, first) || !adminProductFollows(byOrder, byID) {
		t.Error("valid sort-order and ID successors were rejected")
	}
	if adminProductFollows(first, byID) || adminProductFollows(first, first) {
		t.Error("non-increasing Product order was accepted")
	}
}
