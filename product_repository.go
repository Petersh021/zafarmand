package main

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// publishedProductStatus is the only stored state eligible for public reads.
	// It is bound as a query filter and never copied into the public result.
	publishedProductStatus = "published"
	// productSlugMaximumLength mirrors the migration-owned character bound for
	// canonical public product slugs.
	productSlugMaximumLength = 120
	// productNameMaximumLength mirrors the migration-owned character bound for a
	// product's public catalogue name.
	productNameMaximumLength = 160
	// productCategoryMaximumLength mirrors the migration-owned character bound
	// for the public category label.
	productCategoryMaximumLength = 80
)

// productSlugPattern accepts one lowercase ASCII word or multiple such words
// separated by single hyphens. The anchors reject partial matches, while the
// explicit length check below keeps this expression aligned with PostgreSQL.
var productSlugPattern = regexp.MustCompile(
	`^[a-z0-9]+(?:-[a-z0-9]+)*$`,
)

// Product catalogue construction and read errors are stable, credential-free
// categories. Driver diagnostics are never wrapped because they can expose SQL,
// connection information, or a malformed stored value.
var (
	// errProductCatalogueReaderDatabaseRequired rejects repository construction
	// without the application-owned PostgreSQL pool.
	errProductCatalogueReaderDatabaseRequired = errors.New(
		"create product catalogue reader: database is required",
	)
	// errProductCatalogueInvalidQuery identifies a nil context or malformed slug
	// before a database operation is attempted.
	errProductCatalogueInvalidQuery = errors.New(
		"product catalogue query is invalid",
	)
	// errProductCatalogueNotFound maps only a no-row result for a canonical
	// product slug. It does not distinguish missing, draft, and archived rows.
	errProductCatalogueNotFound = errors.New(
		"published product not found",
	)
	// errProductCatalogueReadFailed collapses every driver, scanning, ordering,
	// closing, and stored-data failure into one safe public-service category.
	errProductCatalogueReadFailed = errors.New(
		"product catalogue database operation failed",
	)
)

// catalogueProduct is the derived repository projection needed by both public
// Products handlers. Publication state and internal sort order deliberately do
// not cross this boundary: the query turns them into eligibility and a
// consecutive presentation number before Go receives a row.
type catalogueProduct struct {
	// ID is PostgreSQL's positive internal identity. It is used only to validate
	// deterministic results and is not required in a public URL.
	ID int64
	// CatalogueNumber is the one-based position calculated across all published
	// products ordered by sort_order and then ID.
	CatalogueNumber int64
	// Slug is the canonical path segment accepted after /products/.
	Slug string
	// Name is the public product heading.
	Name string
	// Category is the public product-family label.
	Category string
	// Description is optional reviewed long-form public copy.
	Description string
	// Material is an optional reviewed material fact.
	Material string
	// Dimensions is an optional reviewed dimensions fact.
	Dimensions string
	// Cover contains binary-free public image metadata, or nil when no reviewed
	// cover has been uploaded.
	Cover *productCoverMetadata
}

// productCatalogueReader is the narrow read behavior required by public Product
// HTML and exact-cover handlers. It provides no draft access or mutation
// authority and therefore cannot grow into an accidental administration API.
type productCatalogueReader interface {
	// ListPublished returns every currently published record in catalogue order.
	ListPublished(context.Context) ([]catalogueProduct, error)
	// FindPublishedBySlug returns one published record at its list-consistent
	// catalogue position, or a safe not-found category.
	FindPublishedBySlug(
		context.Context,
		string,
	) (catalogueProduct, error)
	// FindPublishedCover returns one exact cover revision only while its owning
	// Product remains published.
	FindPublishedCover(
		context.Context,
		string,
		int64,
	) (productCoverAsset, error)
}

// listPublishedProductsSQL computes numbering only after excluding non-public
// rows. Its outer ORDER BY makes the iterator contract explicit rather than
// relying on the window function's internal processing order.
//
// The publication state is a positional parameter even though it is a trusted
// application constant. Keeping it outside SQL makes the public-only boundary
// visible in unit tests and avoids constructing query text dynamically.
const listPublishedProductsSQL = `SELECT
    id,
    catalogue_number,
    slug,
    name,
    category,
    description,
    material,
    dimensions,
    cover_version,
    cover_width,
    cover_height,
    cover_alt_text,
    cover_caption
FROM (
    SELECT
        products.id,
        ROW_NUMBER() OVER (ORDER BY products.sort_order ASC, products.id ASC)
            AS catalogue_number,
        products.slug,
        products.name,
        products.category,
        products.description,
        products.material,
        products.dimensions,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text,
        cover.caption AS cover_caption
    FROM public.products AS products
    LEFT JOIN public.product_cover_images AS cover
        ON cover.product_id = products.id
    WHERE products.publication_status = $1
) AS published_products
ORDER BY catalogue_number ASC`

// findPublishedProductBySlugSQL deliberately applies the slug predicate outside
// the published window. Filtering inside it would renumber every matching row
// to 1 and make a detail page disagree with the same product in the listing.
// Draft and archived rows never enter the window and are indistinguishable from
// an unknown slug at the repository boundary.
const findPublishedProductBySlugSQL = `SELECT
    id,
    catalogue_number,
    slug,
    name,
    category,
    description,
    material,
    dimensions,
    cover_version,
    cover_width,
    cover_height,
    cover_alt_text,
    cover_caption
FROM (
    SELECT
        products.id,
        ROW_NUMBER() OVER (ORDER BY products.sort_order ASC, products.id ASC)
            AS catalogue_number,
        products.slug,
        products.name,
        products.category,
        products.description,
        products.material,
        products.dimensions,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text,
        cover.caption AS cover_caption
    FROM public.products AS products
    LEFT JOIN public.product_cover_images AS cover
        ON cover.product_id = products.id
    WHERE products.publication_status = $1
) AS published_products
WHERE slug = $2`

// findPublishedProductCoverSQL reads binary media only for the exact canonical
// Product slug, published state, and exact cover revision in the URL.
const findPublishedProductCoverSQL = `SELECT
    cover.product_id,
    cover.version,
    cover.content_type,
    cover.content,
    cover.byte_size,
    cover.width,
    cover.height,
    cover.sha256,
    cover.alt_text,
    cover.caption,
    cover.created_at,
    cover.updated_at
FROM public.product_cover_images AS cover
INNER JOIN public.products AS products
    ON products.id = cover.product_id
WHERE products.slug = $1
  AND products.publication_status = $2
  AND cover.version = $3`

// productCatalogueRows is the smallest database/sql iterator surface required
// by ListPublished. Tests can implement it without a SQL mocking dependency.
type productCatalogueRows interface {
	// Next advances to the next product when one is available.
	Next() bool
	// Scan copies the current Product and optional cover projection into Go
	// destinations.
	Scan(...any) error
	// Err reports an iteration failure that occurs after the last Scan.
	Err() error
	// Close releases the result and returns its borrowed connection to the pool.
	Close() error
}

// productCatalogueScanner is shared by multi-row and single-row Product scans.
// Both database/sql result types satisfy this one-method interface.
type productCatalogueScanner interface {
	// Scan copies one fixed Product projection into supplied destinations.
	Scan(...any) error
}

// productCatalogueQuery adapts database/sql's concrete QueryContext result to
// the narrow multi-row seam used by the repository and deterministic tests.
type productCatalogueQuery func(
	context.Context,
	string,
	...any,
) (productCatalogueRows, error)

// productCatalogueRowScanner is the single-row behavior required by a detail
// lookup. *sql.Row satisfies this interface in production.
type productCatalogueRowScanner interface {
	// Scan copies the projected product into the supplied destinations.
	Scan(...any) error
}

// productCatalogueQueryRow adapts *sql.DB.QueryRowContext without exposing the
// complete database pool to repository unit tests.
type productCatalogueQueryRow func(
	context.Context,
	string,
	...any,
) productCatalogueRowScanner

// postgresProductCatalogueReader borrows the process-owned PostgreSQL pool for
// concurrent public catalogue requests. The process that opens the pool remains
// solely responsible for closing it.
type postgresProductCatalogueReader struct {
	// query executes the ordered published-list statement.
	query productCatalogueQuery
	// queryRow executes the exact canonical-slug detail statement.
	queryRow productCatalogueQueryRow
}

// Compile-time verification catches accidental method-signature drift between
// the production repository and the handler-facing read contract.
var _ productCatalogueReader = (*postgresProductCatalogueReader)(nil)

// newPostgresProductCatalogueReader adapts the shared database pool without
// opening a connection or issuing a query. Rejecting nil converts a missing
// startup dependency into a clear error instead of a first-request panic.
func newPostgresProductCatalogueReader(
	database *sql.DB,
) (*postgresProductCatalogueReader, error) {
	if database == nil {
		return nil, errProductCatalogueReaderDatabaseRequired
	}

	return &postgresProductCatalogueReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (productCatalogueRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) productCatalogueRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ListPublished reads every public product in deterministic catalogue order.
// It validates the complete returned contract before exposing any record to a
// handler and closes every acquired row set in success and failure paths.
func (reader *postgresProductCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueProduct, error) {
	if ctx == nil {
		return nil, errProductCatalogueInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errProductCatalogueReadFailed
	}

	rows, err := reader.query(
		ctx,
		listPublishedProductsSQL,
		publishedProductStatus,
	)
	if err != nil {
		// database/sql normally returns nil rows with an error. Closing a non-nil
		// substituted result keeps this boundary leak-free without trusting that
		// implementation detail.
		if rows != nil {
			_ = rows.Close()
		}

		return nil, errProductCatalogueReadFailed
	}
	if rows == nil {
		return nil, errProductCatalogueReadFailed
	}

	products := make([]catalogueProduct, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	for rows.Next() {
		product, err := scanCatalogueProduct(rows)
		if err != nil {
			_ = rows.Close()

			return nil, errProductCatalogueReadFailed
		}

		expectedNumber := int64(len(products) + 1)
		if !isValidCatalogueProduct(product) ||
			product.CatalogueNumber != expectedNumber {
			// Consecutive numbers prove the row iterator preserved the SQL order and
			// that numbering started at one after non-public rows were excluded.
			_ = rows.Close()

			return nil, errProductCatalogueReadFailed
		}
		if _, exists := seenIDs[product.ID]; exists {
			_ = rows.Close()

			return nil, errProductCatalogueReadFailed
		}
		if _, exists := seenSlugs[product.Slug]; exists {
			_ = rows.Close()

			return nil, errProductCatalogueReadFailed
		}

		seenIDs[product.ID] = struct{}{}
		seenSlugs[product.Slug] = struct{}{}
		products = append(products, product)
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errProductCatalogueReadFailed
	}

	// Removing spare capacity makes a caller's next append allocate instead of
	// extending the slice into this method's backing array.
	return products[:len(products):len(products)], nil
}

// FindPublishedBySlug loads one public product while retaining the number it
// has in ListPublished. Canonical input is rejected before a query; unpublished
// and missing records share one safe not-found result.
func (reader *postgresProductCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueProduct, error) {
	if ctx == nil || !isCanonicalProductSlug(slug) {
		return catalogueProduct{}, errProductCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return catalogueProduct{}, errProductCatalogueReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedProductBySlugSQL,
		publishedProductStatus,
		slug,
	)
	if row == nil {
		return catalogueProduct{}, errProductCatalogueReadFailed
	}

	product, err := scanCatalogueProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return catalogueProduct{}, errProductCatalogueNotFound
	}
	if err != nil {
		return catalogueProduct{}, errProductCatalogueReadFailed
	}
	if !isValidCatalogueProduct(product) || product.Slug != slug {
		// A mismatched row indicates a broken substituted reader or violated query
		// contract. It must never create a canonical URL for the wrong record.
		return catalogueProduct{}, errProductCatalogueReadFailed
	}

	return product, nil
}

// FindPublishedCover loads one complete image only when its Product is public
// and the requested revision exactly matches the current cover revision.
func (reader *postgresProductCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (productCoverAsset, error) {
	if ctx == nil || !isCanonicalProductSlug(slug) || version <= 0 {
		return productCoverAsset{}, errProductCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedProductCoverSQL,
		slug,
		publishedProductStatus,
		version,
	)
	if row == nil {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	asset, err := scanProductCoverAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return productCoverAsset{}, errProductCoverNotFound
	}
	if err != nil || asset.Version != version {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	return asset, nil
}

// scanCatalogueProduct converts nullable LEFT JOIN columns into an all-or-none
// cover pointer so a partially malformed database projection fails closed.
func scanCatalogueProduct(
	scanner productCatalogueScanner,
) (catalogueProduct, error) {
	if scanner == nil {
		return catalogueProduct{}, errProductCatalogueReadFailed
	}

	var product catalogueProduct
	var coverVersion sql.NullInt64
	var coverWidth sql.NullInt64
	var coverHeight sql.NullInt64
	var coverAltText sql.NullString
	var coverCaption sql.NullString
	err := scanner.Scan(
		&product.ID,
		&product.CatalogueNumber,
		&product.Slug,
		&product.Name,
		&product.Category,
		&product.Description,
		&product.Material,
		&product.Dimensions,
		&coverVersion,
		&coverWidth,
		&coverHeight,
		&coverAltText,
		&coverCaption,
	)
	if err != nil {
		return catalogueProduct{}, err
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return catalogueProduct{}, errProductCatalogueReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(^uint(0)>>1) ||
			coverHeight.Int64 > int64(^uint(0)>>1) {
			return catalogueProduct{}, errProductCatalogueReadFailed
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

// isValidCatalogueProduct validates the complete repository projection before
// stored or SQL-derived values can influence HTML or a canonical route.
func isValidCatalogueProduct(product catalogueProduct) bool {
	return product.ID > 0 &&
		product.CatalogueNumber > 0 &&
		isCanonicalProductSlug(product.Slug) &&
		isValidProductCatalogueText(
			product.Name,
			productNameMaximumLength,
		) &&
		isValidProductCatalogueText(
			product.Category,
			productCategoryMaximumLength,
		) &&
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
		(product.Cover == nil || isValidProductCoverMetadata(*product.Cover))
}

// isCanonicalProductSlug mirrors the database slug grammar for both request
// validation and defensive stored-row validation. Since the grammar is ASCII,
// byte length and PostgreSQL character length are identical here.
func isCanonicalProductSlug(slug string) bool {
	return slug != "" &&
		len(slug) <= productSlugMaximumLength &&
		productSlugPattern.MatchString(slug)
}

// isValidProductCatalogueText accepts nonempty, valid UTF-8 text with no
// leading or trailing Unicode whitespace, no PostgreSQL-incompatible NUL, and
// a bounded code-point count.
// html/template remains responsible for contextual output escaping.
func isValidProductCatalogueText(value string, maximumLength int) bool {
	if maximumLength <= 0 ||
		!utf8.ValidString(value) ||
		value == "" ||
		strings.ContainsRune(value, '\x00') ||
		value != strings.TrimSpace(value) {
		return false
	}

	length := utf8.RuneCountInString(value)

	return length >= 1 && length <= maximumLength
}
