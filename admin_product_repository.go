package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

const (
	// productPublicationStatusDraft is the fail-closed state assigned by
	// migration 4 when a future writer omits an explicit lifecycle choice.
	productPublicationStatusDraft = "draft"
	// productPublicationStatusArchived keeps a Product stored while removing it
	// from the public catalogue.
	productPublicationStatusArchived = "archived"
)

// Administrator Product read errors are stable categories that never retain
// SQL, connection details, or stored catalogue text.
var (
	// errAdminProductReaderDatabaseRequired rejects construction without the
	// application-owned PostgreSQL pool.
	errAdminProductReaderDatabaseRequired = errors.New(
		"create admin product reader: database is required",
	)
	// errAdminProductInvalidQuery identifies a nil context or non-positive ID
	// before PostgreSQL is contacted.
	errAdminProductInvalidQuery = errors.New("admin product query is invalid")
	// errAdminProductNotFound maps only a genuine no-row detail result.
	errAdminProductNotFound = errors.New("admin product not found")
	// errAdminProductReadFailed collapses driver, scanning, ordering, and stored-
	// data failures into one credential-free dependency category.
	errAdminProductReadFailed = errors.New(
		"admin product database operation failed",
	)
)

// adminProductRecord is the complete migration-4 Product projection needed by
// protected list and detail pages. It contains no future descriptions, media,
// SEO fields, pricing, or mutation metadata that the schema does not own.
type adminProductRecord struct {
	// ID is PostgreSQL's positive internal Product identity.
	ID int64
	// Slug is the canonical public route segment, even when the row is not public.
	Slug string
	// Name is the stored catalogue heading.
	Name string
	// Category is the stored product-family label.
	Category string
	// SortOrder is the positive editorial position used before ID.
	SortOrder int
	// PublicationStatus is one value from the migration's closed lifecycle set.
	PublicationStatus string
	// CreatedAt is the stored database creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the stored update timestamp and cannot predate creation.
	UpdatedAt time.Time
}

// adminProductReader is the narrow, read-only persistence behavior required by
// Stage 19. It deliberately has no create, update, publish, archive, or delete
// method, keeping later mutation authority visible at the application boundary.
type adminProductReader interface {
	// List returns every Product state in deterministic editorial order.
	List(context.Context) ([]adminProductRecord, error)
	// FindByID returns one complete protected Product or a safe not-found category.
	FindByID(context.Context, int64) (adminProductRecord, error)
}

// listAdminProductsSQL selects only migration-4 fields. All lifecycle states
// are visible to authenticated administrators, while the public reader remains
// separately constrained to published rows.
const listAdminProductsSQL = `SELECT
    id,
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
FROM public.products
ORDER BY sort_order ASC, id ASC`

// findAdminProductByIDSQL uses the internal positive identity only inside the
// protected area. It does not accept a slug because draft and archived slugs
// must never become public-route lookups.
const findAdminProductByIDSQL = `SELECT
    id,
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
FROM public.products
WHERE id = $1`

// adminProductRows is the smallest database/sql iterator surface required by
// the protected Product list. Tests can implement it without a mocking library.
type adminProductRows interface {
	// Next advances to the next result row when one exists.
	Next() bool
	// Scan copies the current eight projected columns into Go destinations.
	Scan(...any) error
	// Err reports an iteration failure that happened after the last Scan.
	Err() error
	// Close releases the result and returns its connection to the shared pool.
	Close() error
}

// adminProductQuery adapts *sql.DB.QueryContext to the narrow list-test seam.
type adminProductQuery func(
	context.Context,
	string,
	...any,
) (adminProductRows, error)

// adminProductRowScanner is the single-row behavior required by detail reads.
type adminProductRowScanner interface {
	// Scan copies the projected row into the supplied destinations.
	Scan(...any) error
}

// adminProductQueryRow adapts *sql.DB.QueryRowContext without exposing the
// complete database pool to deterministic unit tests.
type adminProductQueryRow func(
	context.Context,
	string,
	...any,
) adminProductRowScanner

// postgresAdminProductReader borrows the process-owned PostgreSQL pool for
// concurrent protected reads. The process that opens the pool closes it.
type postgresAdminProductReader struct {
	// query runs the complete ordered Product list statement.
	query adminProductQuery
	// queryRow runs the exact positive-ID detail statement.
	queryRow adminProductQueryRow
}

// Compile-time verification catches accidental drift from the handler-facing
// read contract before application construction.
var _ adminProductReader = (*postgresAdminProductReader)(nil)

// newPostgresAdminProductReader adapts the shared database pool without opening
// a connection or issuing a query.
func newPostgresAdminProductReader(
	database *sql.DB,
) (*postgresAdminProductReader, error) {
	if database == nil {
		return nil, errAdminProductReaderDatabaseRequired
	}

	return &postgresAdminProductReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (adminProductRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminProductRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// List reads every Product state in editorial order and validates the complete
// result before returning it to a protected handler.
func (reader *postgresAdminProductReader) List(
	ctx context.Context,
) ([]adminProductRecord, error) {
	if ctx == nil {
		return nil, errAdminProductInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errAdminProductReadFailed
	}

	rows, err := reader.query(ctx, listAdminProductsSQL)
	if err != nil {
		if rows != nil {
			// A substituted implementation may return both rows and an error even
			// though database/sql normally does not. Close defensively in either case.
			_ = rows.Close()
		}

		return nil, errAdminProductReadFailed
	}
	if rows == nil {
		return nil, errAdminProductReadFailed
	}

	products := make([]adminProductRecord, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	var previous adminProductRecord
	for rows.Next() {
		var product adminProductRecord
		if err := rows.Scan(
			&product.ID,
			&product.Slug,
			&product.Name,
			&product.Category,
			&product.SortOrder,
			&product.PublicationStatus,
			&product.CreatedAt,
			&product.UpdatedAt,
		); err != nil {
			_ = rows.Close()

			return nil, errAdminProductReadFailed
		}

		if !isValidStoredAdminProduct(product) ||
			(len(products) > 0 && !adminProductFollows(product, previous)) {
			// A malformed value or ordering violation is a dependency-contract
			// failure, never trusted template data.
			_ = rows.Close()

			return nil, errAdminProductReadFailed
		}
		if _, exists := seenIDs[product.ID]; exists {
			_ = rows.Close()

			return nil, errAdminProductReadFailed
		}
		if _, exists := seenSlugs[product.Slug]; exists {
			_ = rows.Close()

			return nil, errAdminProductReadFailed
		}

		products = append(products, product)
		seenIDs[product.ID] = struct{}{}
		seenSlugs[product.Slug] = struct{}{}
		previous = product
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errAdminProductReadFailed
	}

	// Removing spare capacity makes a caller's next append allocate instead of
	// extending this method's backing array.
	return products[:len(products):len(products)], nil
}

// FindByID reads one complete protected Product. Only a positive identity is
// accepted, and a genuine missing row remains distinct from operational errors.
func (reader *postgresAdminProductReader) FindByID(
	ctx context.Context,
	id int64,
) (adminProductRecord, error) {
	if ctx == nil || id <= 0 {
		return adminProductRecord{}, errAdminProductInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminProductRecord{}, errAdminProductReadFailed
	}

	row := reader.queryRow(ctx, findAdminProductByIDSQL, id)
	if row == nil {
		return adminProductRecord{}, errAdminProductReadFailed
	}

	var product adminProductRecord
	if err := row.Scan(
		&product.ID,
		&product.Slug,
		&product.Name,
		&product.Category,
		&product.SortOrder,
		&product.PublicationStatus,
		&product.CreatedAt,
		&product.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return adminProductRecord{}, errAdminProductNotFound
		}

		return adminProductRecord{}, errAdminProductReadFailed
	}
	if product.ID != id || !isValidStoredAdminProduct(product) {
		return adminProductRecord{}, errAdminProductReadFailed
	}

	return product, nil
}

// isValidProductPublicationStatus recognizes only the lifecycle vocabulary
// protected by products_publication_status_supported.
func isValidProductPublicationStatus(status string) bool {
	return status == productPublicationStatusDraft ||
		status == publishedProductStatus ||
		status == productPublicationStatusArchived
}

// isValidStoredAdminProduct rechecks every migration-4 field before a stored
// row can influence protected HTML or navigation.
func isValidStoredAdminProduct(product adminProductRecord) bool {
	return product.ID > 0 &&
		isCanonicalProductSlug(product.Slug) &&
		isValidProductCatalogueText(product.Name, productNameMaximumLength) &&
		isValidProductCatalogueText(product.Category, productCategoryMaximumLength) &&
		product.SortOrder > 0 &&
		product.SortOrder <= math.MaxInt32 &&
		isValidProductPublicationStatus(product.PublicationStatus) &&
		!product.CreatedAt.IsZero() &&
		!product.UpdatedAt.IsZero() &&
		!product.UpdatedAt.Before(product.CreatedAt)
}

// adminProductFollows verifies strict `(sort_order, id)` ordering between two
// consecutive repository results.
func adminProductFollows(current adminProductRecord, previous adminProductRecord) bool {
	return current.SortOrder > previous.SortOrder ||
		(current.SortOrder == previous.SortOrder && current.ID > previous.ID)
}
