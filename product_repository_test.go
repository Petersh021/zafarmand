package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// productCatalogueContextKey is a private comparable marker used to prove that
// both repository methods forward the caller's exact request context.
type productCatalogueContextKey struct{}

// productCatalogueQueryStub records one list invocation and returns controlled
// rows or a database-like error without a live PostgreSQL server.
type productCatalogueQueryStub struct {
	// rows is returned with queryError and may deliberately be non-nil on error.
	rows productCatalogueRows
	// queryError simulates a QueryContext or driver failure.
	queryError error
	// calls records how many list statements were attempted.
	calls int
	// context records the exact context received from ListPublished.
	context context.Context
	// query records the complete fixed SQL statement.
	query string
	// arguments records the bound publication state.
	arguments []any
}

// Query matches productCatalogueQuery and captures the invocation before
// returning its configured result.
func (stub *productCatalogueQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (productCatalogueRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.rows, stub.queryError
}

// productCatalogueRowsStub exposes configured products through the exact
// iterator contract used by ListPublished. Scan, iteration, and close failures
// can be controlled independently.
type productCatalogueRowsStub struct {
	// products are exposed in their supplied order.
	products []catalogueProduct
	// scanErrorAt is the zero-based failing row; -1 disables scan failure.
	scanErrorAt int
	// scanError is returned at scanErrorAt.
	scanError error
	// iterationError is returned by Err after iteration stops.
	iterationError error
	// closeError is returned whenever Close is called.
	closeError error
	// nextIndex identifies the next configured row.
	nextIndex int
	// currentIndex identifies the row currently available to Scan.
	currentIndex int
	// scanCalls proves rows are scanned only after Next succeeds.
	scanCalls int
	// closeCalls proves every acquired result is finalized.
	closeCalls int
}

// newProductCatalogueRowsStub explicitly initializes the disabled scan index,
// whose sentinel value differs from the integer zero value.
func newProductCatalogueRowsStub(
	products []catalogueProduct,
) *productCatalogueRowsStub {
	return &productCatalogueRowsStub{
		products:     products,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances through the finite configured slice.
func (stub *productCatalogueRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.products) {
		return false
	}

	stub.currentIndex = stub.nextIndex
	stub.nextIndex++

	return true
}

// Scan copies one product into the five destination types used by the list
// query or returns the configured failure.
func (stub *productCatalogueRowsStub) Scan(destinations ...any) error {
	stub.scanCalls++
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.products) {
		return errors.New("product list scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}
	if len(destinations) != 5 {
		return errors.New("product list scan expected five destinations")
	}

	product := stub.products[stub.currentIndex]
	id, idOK := destinations[0].(*int64)
	catalogueNumber, catalogueNumberOK := destinations[1].(*int64)
	slug, slugOK := destinations[2].(*string)
	name, nameOK := destinations[3].(*string)
	category, categoryOK := destinations[4].(*string)
	if !idOK || !catalogueNumberOK || !slugOK || !nameOK || !categoryOK {
		return errors.New("product list scan received unexpected destinations")
	}

	*id = product.ID
	*catalogueNumber = product.CatalogueNumber
	*slug = product.Slug
	*name = product.Name
	*category = product.Category

	return nil
}

// Err returns the controlled post-iteration failure.
func (stub *productCatalogueRowsStub) Err() error {
	return stub.iterationError
}

// Close records finalization and returns the controlled close result.
func (stub *productCatalogueRowsStub) Close() error {
	stub.closeCalls++

	return stub.closeError
}

// productCatalogueQueryRowStub records one detail query and returns a
// controlled scanner.
type productCatalogueQueryRowStub struct {
	// row is returned to FindPublishedBySlug.
	row productCatalogueRowScanner
	// calls records how many detail statements were attempted.
	calls int
	// context records the caller's exact context.
	context context.Context
	// query records the complete fixed detail SQL.
	query string
	// arguments records the bound publication state and canonical slug.
	arguments []any
}

// QueryRow matches productCatalogueQueryRow and captures its invocation.
func (stub *productCatalogueQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) productCatalogueRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// productCatalogueRowStub copies one configured detail record or returns a
// database-like scanning failure.
type productCatalogueRowStub struct {
	// product contains every successful detail destination value.
	product catalogueProduct
	// scanError simulates no rows, decoding failure, or a driver error.
	scanError error
	// calls proves the row is scanned exactly once.
	calls int
}

// Scan implements productCatalogueRowScanner for the five-column detail query.
func (stub *productCatalogueRowStub) Scan(destinations ...any) error {
	stub.calls++
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 5 {
		return errors.New("product detail scan expected five destinations")
	}

	id, idOK := destinations[0].(*int64)
	catalogueNumber, catalogueNumberOK := destinations[1].(*int64)
	slug, slugOK := destinations[2].(*string)
	name, nameOK := destinations[3].(*string)
	category, categoryOK := destinations[4].(*string)
	if !idOK || !catalogueNumberOK || !slugOK || !nameOK || !categoryOK {
		return errors.New("product detail scan received unexpected destinations")
	}

	*id = stub.product.ID
	*catalogueNumber = stub.product.CatalogueNumber
	*slug = stub.product.Slug
	*name = stub.product.Name
	*category = stub.product.Category

	return nil
}

// validCatalogueProduct returns one deterministic valid record. Tests can
// change exactly one field to isolate each defensive rule.
func validCatalogueProduct(
	id int64,
	catalogueNumber int64,
	slug string,
) catalogueProduct {
	return catalogueProduct{
		ID:              id,
		CatalogueNumber: catalogueNumber,
		Slug:            slug,
		Name:            "Stage Eighteen Chair",
		Category:        "Furniture",
	}
}

// TestNewPostgresProductCatalogueReader verifies dependency validation and
// confirms construction adapts a pool without opening a connection.
func TestNewPostgresProductCatalogueReader(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		reader, err := newPostgresProductCatalogueReader(nil)
		if !errors.Is(err, errProductCatalogueReaderDatabaseRequired) {
			t.Fatalf("error: got %v, want database-required sentinel", err)
		}
		if reader != nil {
			t.Errorf("reader: got %#v, want nil", reader)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		// A zero sql.DB is sufficient because construction installs adapters but
		// deliberately performs no network operation.
		database := new(sql.DB)
		reader, err := newPostgresProductCatalogueReader(database)
		if err != nil {
			t.Fatalf("create reader: %v", err)
		}
		if reader == nil || reader.query == nil || reader.queryRow == nil {
			t.Fatalf("reader adapters were not completely installed: %#v", reader)
		}
	})
}

// TestPostgresProductCatalogueReaderListsPublished verifies the complete list
// statement, bound state, context propagation, ordered mapping, and row cleanup.
func TestPostgresProductCatalogueReaderListsPublished(t *testing.T) {
	expected := []catalogueProduct{
		validCatalogueProduct(8, 1, "chair-study"),
		validCatalogueProduct(3, 2, "lamp-study"),
		validCatalogueProduct(14, 3, "vessel-study"),
	}
	expected[1].Name = "Stage Eighteen Lamp"
	expected[1].Category = "Lighting"
	expected[2].Name = "Stage Eighteen Vessel"
	expected[2].Category = "Objects"

	rows := newProductCatalogueRowsStub(expected)
	query := &productCatalogueQueryStub{rows: rows}
	reader := &postgresProductCatalogueReader{query: query.Query}
	ctx := context.WithValue(
		context.Background(),
		productCatalogueContextKey{},
		"list-context",
	)

	actual, err := reader.ListPublished(ctx)
	if err != nil {
		t.Fatalf("list published products: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("products: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx {
		t.Errorf(
			"query invocation: got calls=%d context=%p, want 1/%p",
			query.calls,
			query.context,
			ctx,
		)
	}
	if query.query != listPublishedProductsSQL {
		t.Errorf("query text: got %q, want fixed list statement", query.query)
	}
	if !reflect.DeepEqual(query.arguments, []any{publishedProductStatus}) {
		t.Errorf(
			"query arguments: got %#v, want published state only",
			query.arguments,
		)
	}
	if rows.scanCalls != len(expected) || rows.closeCalls != 1 {
		t.Errorf(
			"row lifecycle: got scans=%d closes=%d, want %d/1",
			rows.scanCalls,
			rows.closeCalls,
			len(expected),
		)
	}
}

// TestPostgresProductCatalogueReaderListsEmpty verifies that a legitimate
// zero-row catalogue is successful and returns an allocated empty slice rather
// than confusing absence with a repository failure.
func TestPostgresProductCatalogueReaderListsEmpty(t *testing.T) {
	rows := newProductCatalogueRowsStub(nil)
	query := &productCatalogueQueryStub{rows: rows}
	reader := &postgresProductCatalogueReader{query: query.Query}

	products, err := reader.ListPublished(context.Background())
	if err != nil {
		t.Fatalf("list empty catalogue: %v", err)
	}
	if products == nil || len(products) != 0 {
		t.Errorf("empty products: got %#v, want allocated empty slice", products)
	}
	if rows.scanCalls != 0 || rows.closeCalls != 1 {
		t.Errorf(
			"empty row lifecycle: got scans=%d closes=%d, want 0/1",
			rows.scanCalls,
			rows.closeCalls,
		)
	}
}

// TestPostgresProductCatalogueReaderRejectsInvalidListResults proves that no
// malformed, duplicated, or out-of-order stored value can cross the repository
// boundary, even from a substituted query implementation.
func TestPostgresProductCatalogueReaderRejectsInvalidListResults(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	tests := []struct {
		// name identifies the isolated violated invariant.
		name string
		// products is the complete controlled query result.
		products []catalogueProduct
	}{
		{
			name: "non-positive id",
			products: []catalogueProduct{
				validCatalogueProduct(0, 1, "chair-study"),
			},
		},
		{
			name: "number does not start at one",
			products: []catalogueProduct{
				validCatalogueProduct(1, 2, "chair-study"),
			},
		},
		{
			name: "number contains gap",
			products: []catalogueProduct{
				validCatalogueProduct(1, 1, "chair-study"),
				validCatalogueProduct(2, 3, "lamp-study"),
			},
		},
		{
			name: "duplicate id",
			products: []catalogueProduct{
				validCatalogueProduct(1, 1, "chair-study"),
				validCatalogueProduct(1, 2, "lamp-study"),
			},
		},
		{
			name: "duplicate slug",
			products: []catalogueProduct{
				validCatalogueProduct(1, 1, "chair-study"),
				validCatalogueProduct(2, 2, "chair-study"),
			},
		},
		{
			name: "uppercase slug",
			products: []catalogueProduct{
				validCatalogueProduct(1, 1, "Chair-Study"),
			},
		},
		{
			name: "slug exceeds bound",
			products: []catalogueProduct{
				validCatalogueProduct(
					1,
					1,
					strings.Repeat("a", productSlugMaximumLength+1),
				),
			},
		},
		{
			name: "empty name",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Name = ""
					return product
				}(),
			},
		},
		{
			name: "name has surrounding whitespace",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Name = " Stage Eighteen Chair "
					return product
				}(),
			},
		},
		{
			name: "name exceeds rune bound",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Name = strings.Repeat(
						"é",
						productNameMaximumLength+1,
					)
					return product
				}(),
			},
		},
		{
			name: "name contains invalid utf8",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Name = invalidUTF8
					return product
				}(),
			},
		},
		{
			name: "empty category",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Category = ""
					return product
				}(),
			},
		},
		{
			name: "category exceeds rune bound",
			products: []catalogueProduct{
				func() catalogueProduct {
					product := validCatalogueProduct(1, 1, "chair-study")
					product.Category = strings.Repeat(
						"C",
						productCategoryMaximumLength+1,
					)
					return product
				}(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := newProductCatalogueRowsStub(test.products)
			query := &productCatalogueQueryStub{rows: rows}
			reader := &postgresProductCatalogueReader{query: query.Query}

			products, err := reader.ListPublished(context.Background())
			if !errors.Is(err, errProductCatalogueReadFailed) {
				t.Fatalf("error: got %v, want redacted read sentinel", err)
			}
			if products != nil {
				t.Errorf("invalid result exposed products: %#v", products)
			}
			if rows.closeCalls != 1 {
				t.Errorf("close calls: got %d, want 1", rows.closeCalls)
			}
		})
	}
}

// TestPostgresProductCatalogueReaderListFailures verifies request validation,
// unavailable implementations, and every database/result lifecycle failure.
func TestPostgresProductCatalogueReaderListFailures(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		query := &productCatalogueQueryStub{}
		reader := &postgresProductCatalogueReader{query: query.Query}

		products, err := reader.ListPublished(nil)
		if !errors.Is(err, errProductCatalogueInvalidQuery) {
			t.Fatalf("error: got %v, want invalid-query sentinel", err)
		}
		if products != nil || query.calls != 0 {
			t.Errorf(
				"nil context result: got products=%#v calls=%d, want nil/0",
				products,
				query.calls,
			)
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var reader *postgresProductCatalogueReader
		products, err := reader.ListPublished(context.Background())
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if products != nil {
			t.Errorf("nil receiver exposed products: %#v", products)
		}
	})

	t.Run("nil query", func(t *testing.T) {
		reader := &postgresProductCatalogueReader{}
		products, err := reader.ListPublished(context.Background())
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if products != nil {
			t.Errorf("nil query exposed products: %#v", products)
		}
	})

	controlledError := errors.New("driver secret must be redacted")
	tests := []struct {
		// name identifies the failing database lifecycle step.
		name string
		// configure mutates the controlled query and rows.
		configure func(*productCatalogueQueryStub, *productCatalogueRowsStub)
	}{
		{
			name: "query error with rows",
			configure: func(
				query *productCatalogueQueryStub,
				_ *productCatalogueRowsStub,
			) {
				query.queryError = controlledError
			},
		},
		{
			name: "scan error",
			configure: func(
				_ *productCatalogueQueryStub,
				rows *productCatalogueRowsStub,
			) {
				rows.scanErrorAt = 0
				rows.scanError = controlledError
			},
		},
		{
			name: "iteration error",
			configure: func(
				_ *productCatalogueQueryStub,
				rows *productCatalogueRowsStub,
			) {
				rows.iterationError = controlledError
			},
		},
		{
			name: "close error",
			configure: func(
				_ *productCatalogueQueryStub,
				rows *productCatalogueRowsStub,
			) {
				rows.closeError = controlledError
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := newProductCatalogueRowsStub([]catalogueProduct{
				validCatalogueProduct(1, 1, "chair-study"),
			})
			query := &productCatalogueQueryStub{rows: rows}
			test.configure(query, rows)
			reader := &postgresProductCatalogueReader{query: query.Query}

			products, err := reader.ListPublished(context.Background())
			if !errors.Is(err, errProductCatalogueReadFailed) {
				t.Fatalf("error: got %v, want read-failure sentinel", err)
			}
			if err.Error() != errProductCatalogueReadFailed.Error() ||
				strings.Contains(err.Error(), "driver secret") {
				t.Errorf("error exposed controlled detail: %q", err)
			}
			if products != nil {
				t.Errorf("failure exposed products: %#v", products)
			}
			if rows.closeCalls != 1 {
				t.Errorf("close calls: got %d, want 1", rows.closeCalls)
			}
		})
	}

	t.Run("query error with nil rows", func(t *testing.T) {
		query := &productCatalogueQueryStub{queryError: controlledError}
		reader := &postgresProductCatalogueReader{query: query.Query}

		products, err := reader.ListPublished(context.Background())
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if products != nil {
			t.Errorf("query failure exposed products: %#v", products)
		}
	})

	t.Run("nil rows without query error", func(t *testing.T) {
		query := &productCatalogueQueryStub{}
		reader := &postgresProductCatalogueReader{query: query.Query}

		products, err := reader.ListPublished(context.Background())
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if products != nil {
			t.Errorf("nil rows exposed products: %#v", products)
		}
	})
}

// TestPostgresProductCatalogueReaderFindsPublishedBySlug verifies exact query
// text, parameter order, context propagation, and complete field mapping.
func TestPostgresProductCatalogueReaderFindsPublishedBySlug(t *testing.T) {
	expected := validCatalogueProduct(23, 4, "stone-side-table")
	expected.Name = "Stone Side Table"
	expected.Category = "Furniture"
	row := &productCatalogueRowStub{product: expected}
	queryRow := &productCatalogueQueryRowStub{row: row}
	reader := &postgresProductCatalogueReader{queryRow: queryRow.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		productCatalogueContextKey{},
		"detail-context",
	)

	actual, err := reader.FindPublishedBySlug(ctx, expected.Slug)
	if err != nil {
		t.Fatalf("find published product: %v", err)
	}
	if actual != expected {
		t.Errorf("product: got %#v, want %#v", actual, expected)
	}
	if queryRow.calls != 1 || queryRow.context != ctx || row.calls != 1 {
		t.Errorf(
			"query invocation: got calls=%d row scans=%d context=%p, want 1/1/%p",
			queryRow.calls,
			row.calls,
			queryRow.context,
			ctx,
		)
	}
	if queryRow.query != findPublishedProductBySlugSQL {
		t.Errorf("query text: got %q, want fixed detail statement", queryRow.query)
	}
	if !reflect.DeepEqual(
		queryRow.arguments,
		[]any{publishedProductStatus, expected.Slug},
	) {
		t.Errorf(
			"query arguments: got %#v, want published state and slug",
			queryRow.arguments,
		)
	}
}

// TestPostgresProductCatalogueReaderRejectsInvalidDetailQueries proves malformed
// visitor path values and a nil context are rejected before SQL is attempted.
func TestPostgresProductCatalogueReaderRejectsInvalidDetailQueries(t *testing.T) {
	tests := []struct {
		// name explains the rejected request shape.
		name string
		// context is nil only in the dedicated invalid-context case.
		context context.Context
		// slug is the complete untrusted path value.
		slug string
	}{
		{name: "nil context", context: nil, slug: "chair-study"},
		{name: "empty slug", context: context.Background(), slug: ""},
		{name: "uppercase", context: context.Background(), slug: "Chair-Study"},
		{name: "leading hyphen", context: context.Background(), slug: "-chair"},
		{name: "trailing hyphen", context: context.Background(), slug: "chair-"},
		{name: "double hyphen", context: context.Background(), slug: "chair--study"},
		{name: "slash", context: context.Background(), slug: "chair/study"},
		{name: "unicode", context: context.Background(), slug: "chäir"},
		{
			name:    "too long",
			context: context.Background(),
			slug:    strings.Repeat("a", productSlugMaximumLength+1),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryRow := &productCatalogueQueryRowStub{}
			reader := &postgresProductCatalogueReader{
				queryRow: queryRow.QueryRow,
			}

			product, err := reader.FindPublishedBySlug(test.context, test.slug)
			if !errors.Is(err, errProductCatalogueInvalidQuery) {
				t.Fatalf("error: got %v, want invalid-query sentinel", err)
			}
			if product != (catalogueProduct{}) || queryRow.calls != 0 {
				t.Errorf(
					"invalid query result: got product=%#v calls=%d, want zero/0",
					product,
					queryRow.calls,
				)
			}
		})
	}
}

// TestPostgresProductCatalogueReaderDetailFailures verifies not-found mapping,
// safe driver redaction, unavailable implementations, and stored-row validation.
func TestPostgresProductCatalogueReaderDetailFailures(t *testing.T) {
	const slug = "chair-study"

	t.Run("no rows", func(t *testing.T) {
		row := &productCatalogueRowStub{scanError: sql.ErrNoRows}
		queryRow := &productCatalogueQueryRowStub{row: row}
		reader := &postgresProductCatalogueReader{queryRow: queryRow.QueryRow}

		product, err := reader.FindPublishedBySlug(
			context.Background(),
			slug,
		)
		if !errors.Is(err, errProductCatalogueNotFound) {
			t.Fatalf("error: got %v, want not-found sentinel", err)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("missing lookup exposed product: %#v", product)
		}
	})

	t.Run("driver error is redacted", func(t *testing.T) {
		row := &productCatalogueRowStub{
			scanError: errors.New("driver secret must be redacted"),
		}
		queryRow := &productCatalogueQueryRowStub{row: row}
		reader := &postgresProductCatalogueReader{queryRow: queryRow.QueryRow}

		product, err := reader.FindPublishedBySlug(
			context.Background(),
			slug,
		)
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if err.Error() != errProductCatalogueReadFailed.Error() ||
			strings.Contains(err.Error(), "driver secret") {
			t.Errorf("error exposed driver detail: %q", err)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("driver failure exposed product: %#v", product)
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var reader *postgresProductCatalogueReader
		product, err := reader.FindPublishedBySlug(
			context.Background(),
			slug,
		)
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("nil receiver exposed product: %#v", product)
		}
	})

	t.Run("nil query row", func(t *testing.T) {
		reader := &postgresProductCatalogueReader{}
		product, err := reader.FindPublishedBySlug(
			context.Background(),
			slug,
		)
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("nil query function exposed product: %#v", product)
		}
	})

	t.Run("query returns nil row", func(t *testing.T) {
		queryRow := &productCatalogueQueryRowStub{}
		reader := &postgresProductCatalogueReader{queryRow: queryRow.QueryRow}
		product, err := reader.FindPublishedBySlug(
			context.Background(),
			slug,
		)
		if !errors.Is(err, errProductCatalogueReadFailed) {
			t.Fatalf("error: got %v, want read-failure sentinel", err)
		}
		if product != (catalogueProduct{}) {
			t.Errorf("nil row exposed product: %#v", product)
		}
	})

	invalidProducts := []struct {
		// name identifies the violated result contract.
		name string
		// product is returned by the controlled row scanner.
		product catalogueProduct
	}{
		{name: "non-positive id", product: validCatalogueProduct(0, 1, slug)},
		{name: "non-positive number", product: validCatalogueProduct(1, 0, slug)},
		{name: "mismatched slug", product: validCatalogueProduct(1, 1, "lamp-study")},
		{
			name: "invalid name",
			product: func() catalogueProduct {
				product := validCatalogueProduct(1, 1, slug)
				product.Name = " Stage Eighteen Chair"
				return product
			}(),
		},
		{
			name: "invalid category",
			product: func() catalogueProduct {
				product := validCatalogueProduct(1, 1, slug)
				product.Category = ""
				return product
			}(),
		},
	}

	for _, test := range invalidProducts {
		t.Run(test.name, func(t *testing.T) {
			row := &productCatalogueRowStub{product: test.product}
			queryRow := &productCatalogueQueryRowStub{row: row}
			reader := &postgresProductCatalogueReader{queryRow: queryRow.QueryRow}

			product, err := reader.FindPublishedBySlug(
				context.Background(),
				slug,
			)
			if !errors.Is(err, errProductCatalogueReadFailed) {
				t.Fatalf("error: got %v, want read-failure sentinel", err)
			}
			if product != (catalogueProduct{}) {
				t.Errorf("invalid result exposed product: %#v", product)
			}
		})
	}
}

// TestProductCatalogueValidationBoundaries locks the exact migration-aligned
// string limits while proving multibyte names are counted as Unicode code points.
func TestProductCatalogueValidationBoundaries(t *testing.T) {
	if !isCanonicalProductSlug(strings.Repeat("a", productSlugMaximumLength)) {
		t.Error("maximum-length canonical slug was rejected")
	}
	if isCanonicalProductSlug(strings.Repeat("a", productSlugMaximumLength+1)) {
		t.Error("overlong canonical slug was accepted")
	}
	if !isValidProductCatalogueText(
		strings.Repeat("é", productNameMaximumLength),
		productNameMaximumLength,
	) {
		t.Error("maximum-length multibyte name was rejected")
	}
	if isValidProductCatalogueText(
		strings.Repeat("é", productNameMaximumLength+1),
		productNameMaximumLength,
	) {
		t.Error("overlong multibyte name was accepted")
	}
}
