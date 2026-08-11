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

// adminProductRecord is the complete migration-6 Product projection needed by
// protected list, detail, and edit pages. Binary cover bytes remain on a
// separate media-read path so ordinary HTML queries stay small.
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
	// Description is optional reviewed long-form public copy.
	Description string
	// Material is an optional reviewed material fact.
	Material string
	// Dimensions is an optional reviewed dimensions fact.
	Dimensions string
	// Cover contains binary-free image metadata, or nil when no cover exists.
	Cover *productCoverMetadata
	// Version is the positive revision used to reject stale administrator edits.
	Version int64
	// CreatedAt is the stored database creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the stored update timestamp and cannot predate creation.
	UpdatedAt time.Time
}

// adminProductReader is the protected all-state Product and cover-read contract.
// It deliberately has no create, update, publish, archive, or delete method,
// keeping mutation authority visible at the application boundary.
type adminProductReader interface {
	// List returns every Product state in deterministic editorial order.
	List(context.Context) ([]adminProductRecord, error)
	// FindByID returns one complete protected Product or a safe not-found category.
	FindByID(context.Context, int64) (adminProductRecord, error)
	// FindCoverByProductID returns one exact cover revision for an authenticated
	// preview regardless of the Product publication state.
	FindCoverByProductID(
		context.Context,
		int64,
		int64,
	) (productCoverAsset, error)
}

// listAdminProductsSQL selects only migration-6 fields. All lifecycle states
// are visible to authenticated administrators, while the public reader remains
// separately constrained to published rows.
const listAdminProductsSQL = `SELECT
    products.id,
    products.slug,
    products.name,
    products.category,
    products.sort_order,
    products.publication_status,
    products.description,
    products.material,
    products.dimensions,
    cover.version,
    cover.width,
    cover.height,
    cover.alt_text,
    cover.caption,
    products.version,
    products.created_at,
    products.updated_at
FROM public.products AS products
LEFT JOIN public.product_cover_images AS cover
    ON cover.product_id = products.id
ORDER BY products.sort_order ASC, products.id ASC`

// findAdminProductByIDSQL uses the internal positive identity only inside the
// protected area. It does not accept a slug because draft and archived slugs
// must never become public-route lookups.
const findAdminProductByIDSQL = `SELECT
    products.id,
    products.slug,
    products.name,
    products.category,
    products.sort_order,
    products.publication_status,
    products.description,
    products.material,
    products.dimensions,
    cover.version,
    cover.width,
    cover.height,
    cover.alt_text,
    cover.caption,
    products.version,
    products.created_at,
    products.updated_at
FROM public.products AS products
LEFT JOIN public.product_cover_images AS cover
    ON cover.product_id = products.id
WHERE products.id = $1`

// findAdminProductCoverSQL supplies binary media only to an authenticated exact
// Product-and-revision request. Publication state is intentionally irrelevant
// inside the protected preview boundary.
const findAdminProductCoverSQL = `SELECT
    product_id,
    version,
    content_type,
    content,
    byte_size,
    width,
    height,
    sha256,
    alt_text,
    caption,
    created_at,
    updated_at
FROM public.product_cover_images
WHERE product_id = $1 AND version = $2`

// adminProductRows is the smallest database/sql iterator surface required by
// the protected Product list. Tests can implement it without a mocking library.
type adminProductRows interface {
	// Next advances to the next result row when one exists.
	Next() bool
	// Scan copies the current Product and optional cover projection into Go
	// destinations.
	Scan(...any) error
	// Err reports an iteration failure that happened after the last Scan.
	Err() error
	// Close releases the result and returns its connection to the shared pool.
	Close() error
}

// adminProductScanner is shared by all-status list and detail result scans.
type adminProductScanner interface {
	// Scan copies the fixed protected Product projection into destinations.
	Scan(...any) error
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
		product, err := scanAdminProduct(rows)
		if err != nil {
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

	product, err := scanAdminProduct(row)
	if err != nil {
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

// FindCoverByProductID returns one exact protected cover revision. A missing
// Product, absent cover, and stale media path share one not-found category.
func (reader *postgresAdminProductReader) FindCoverByProductID(
	ctx context.Context,
	productID int64,
	version int64,
) (productCoverAsset, error) {
	if ctx == nil || productID <= 0 || version <= 0 {
		return productCoverAsset{}, errAdminProductInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findAdminProductCoverSQL,
		productID,
		version,
	)
	if row == nil {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	asset, err := scanProductCoverAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return productCoverAsset{}, errProductCoverNotFound
	}
	if err != nil || asset.ProductID != productID || asset.Version != version {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	return asset, nil
}

// scanAdminProduct converts nullable cover columns into an all-or-none metadata
// pointer and preserves sql.ErrNoRows for the caller's safe 404 mapping.
func scanAdminProduct(scanner adminProductScanner) (adminProductRecord, error) {
	if scanner == nil {
		return adminProductRecord{}, errAdminProductReadFailed
	}

	var product adminProductRecord
	var coverVersion sql.NullInt64
	var coverWidth sql.NullInt64
	var coverHeight sql.NullInt64
	var coverAltText sql.NullString
	var coverCaption sql.NullString
	err := scanner.Scan(
		&product.ID,
		&product.Slug,
		&product.Name,
		&product.Category,
		&product.SortOrder,
		&product.PublicationStatus,
		&product.Description,
		&product.Material,
		&product.Dimensions,
		&coverVersion,
		&coverWidth,
		&coverHeight,
		&coverAltText,
		&coverCaption,
		&product.Version,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if err != nil {
		return adminProductRecord{}, err
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return adminProductRecord{}, errAdminProductReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(math.MaxInt) ||
			coverHeight.Int64 > int64(math.MaxInt) {
			return adminProductRecord{}, errAdminProductReadFailed
		}
		product.Cover = &productCoverMetadata{
			Version: coverVersion.Int64,
			Width:   int(coverWidth.Int64),
			Height:  int(coverHeight.Int64),
			AltText: coverAltText.String,
			Caption: coverCaption.String,
		}
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

// isValidStoredAdminProduct rechecks every migration-6 field before a stored
// row can influence protected HTML or navigation.
func isValidStoredAdminProduct(product adminProductRecord) bool {
	return product.ID > 0 &&
		isCanonicalProductSlug(product.Slug) &&
		isValidProductCatalogueText(product.Name, productNameMaximumLength) &&
		isValidProductCatalogueText(product.Category, productCategoryMaximumLength) &&
		isValidOptionalProductText(
			product.Description,
			productDescriptionMaximumLength,
		) &&
		isValidOptionalProductText(
			product.Material,
			productMaterialMaximumLength,
		) &&
		isValidOptionalProductText(
			product.Dimensions,
			productDimensionsMaximumLength,
		) &&
		(product.Cover == nil || isValidProductCoverMetadata(*product.Cover)) &&
		product.SortOrder > 0 &&
		product.SortOrder <= math.MaxInt32 &&
		isValidProductPublicationStatus(product.PublicationStatus) &&
		product.Version > 0 &&
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
