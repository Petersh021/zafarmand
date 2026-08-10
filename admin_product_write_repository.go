package main

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/jackc/pgx/v5/pgconn"
)

// Product-write errors are stable categories safe to cross the repository
// boundary. Driver diagnostics and rejected catalogue text are never wrapped.
var (
	// errAdminProductWriterDatabaseRequired rejects construction without the
	// application-owned PostgreSQL pool.
	errAdminProductWriterDatabaseRequired = errors.New(
		"create admin product writer: database is required",
	)
	// errAdminProductWriteInvalid rejects an unusable context, identity, version,
	// or Product input before PostgreSQL is contacted.
	errAdminProductWriteInvalid = errors.New("admin product write is invalid")
	// errAdminProductWriteConflict identifies a valid edit based on an obsolete
	// version. The caller must fetch the latest row before trying again.
	errAdminProductWriteConflict = errors.New("admin product version conflict")
	// errAdminProductSlugConflict identifies the one safe, expected uniqueness
	// collision that a form may explain without exposing PostgreSQL diagnostics.
	errAdminProductSlugConflict = errors.New("admin product slug already exists")
	// errAdminProductWriteFailed collapses every other execution, scan, and
	// dependency-contract failure into one credential-free category.
	errAdminProductWriteFailed = errors.New("admin product database write failed")
)

// adminProductWriteInput contains exactly the five administrator-controlled
// values supported by the current Product schema. It deliberately excludes ID,
// version, and timestamps because PostgreSQL owns those concurrency fields.
type adminProductWriteInput struct {
	// Slug is the canonical lowercase public route segment.
	Slug string
	// Name is the bounded catalogue heading.
	Name string
	// Category is the bounded Product-family label.
	Category string
	// SortOrder is the positive editorial position.
	SortOrder int
	// PublicationStatus is one exact value from the closed lifecycle vocabulary.
	PublicationStatus string
}

// adminProductWriteResult returns only the database-owned values required by a
// canonical redirect and by later concurrency-aware editing.
type adminProductWriteResult struct {
	// ID is the positive identity of the inserted or updated Product.
	ID int64
	// Version is the positive revision after the successful write.
	Version int64
}

// adminProductWriter is the narrow Product mutation authority used by Stage 20.
// Read handlers retain their separate read-only dependency, and deletion is not
// included because it needs its own retention and confirmation policy.
type adminProductWriter interface {
	// Create inserts one validated Product and returns its database identity.
	Create(context.Context, adminProductWriteInput) (adminProductWriteResult, error)
	// Update stores one validated edit only when expectedVersion is still current.
	Update(
		context.Context,
		int64,
		int64,
		adminProductWriteInput,
	) (adminProductWriteResult, error)
}

// createAdminProductSQL binds every editable value and lets PostgreSQL assign
// identity, timestamps, and the initial version. RETURNING avoids a second read
// and supplies only data needed for the Post/Redirect/Get destination.
const createAdminProductSQL = `INSERT INTO public.products (
    slug,
    name,
    category,
    sort_order,
    publication_status
) VALUES ($1, $2, $3, $4, $5)
RETURNING id, version`

// updateAdminProductSQL performs the existence check and version-guarded edit
// in one PostgreSQL statement. MATERIALIZED fixes the first CTE as the pre-edit
// identity observation; the update itself still succeeds only for the expected
// revision and increments that revision exactly once.
const updateAdminProductSQL = `WITH current_product AS MATERIALIZED (
    SELECT id
    FROM public.products
    WHERE id = $1
),
updated_product AS (
    UPDATE public.products
    SET
        slug = $3,
        name = $4,
        category = $5,
        sort_order = $6,
        publication_status = $7,
        updated_at = CURRENT_TIMESTAMP,
        version = version + 1
    WHERE id = $1 AND version = $2
    RETURNING id, version
)
SELECT
    COALESCE((SELECT id FROM updated_product), 0),
    COALESCE((SELECT version FROM updated_product), 0),
    EXISTS(SELECT 1 FROM current_product)`

// adminProductWriteRowScanner is the single-row behavior shared by INSERT and
// UPDATE results. *sql.Row satisfies it without exposing the whole pool.
type adminProductWriteRowScanner interface {
	// Scan copies the fixed statement result into supplied destinations.
	Scan(...any) error
}

// adminProductWriteQueryRow is the narrow database/sql operation needed by the
// writer and gives unit tests a dependency-free recording seam.
type adminProductWriteQueryRow func(
	context.Context,
	string,
	...any,
) adminProductWriteRowScanner

// postgresAdminProductWriter borrows the process-owned connection pool for
// concurrent protected mutations. The process remains responsible for closing
// that pool after HTTP shutdown.
type postgresAdminProductWriter struct {
	// queryRow executes one trusted, parameterized write statement.
	queryRow adminProductWriteQueryRow
}

// Compile-time verification prevents accidental drift from the application
// mutation contract.
var _ adminProductWriter = (*postgresAdminProductWriter)(nil)

// newPostgresAdminProductWriter adapts the shared pool without opening a new
// connection or issuing SQL during application construction.
func newPostgresAdminProductWriter(
	database *sql.DB,
) (*postgresAdminProductWriter, error) {
	if database == nil {
		return nil, errAdminProductWriterDatabaseRequired
	}

	return &postgresAdminProductWriter{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminProductWriteRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// Create inserts one validated Product. A duplicate canonical slug is the only
// driver failure translated into a form-facing conflict; every other diagnostic
// remains behind the generic safe failure category.
func (writer *postgresAdminProductWriter) Create(
	ctx context.Context,
	input adminProductWriteInput,
) (adminProductWriteResult, error) {
	if ctx == nil || !isValidAdminProductWriteInput(input) {
		return adminProductWriteResult{}, errAdminProductWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	row := writer.queryRow(
		ctx,
		createAdminProductSQL,
		input.Slug,
		input.Name,
		input.Category,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	var result adminProductWriteResult
	if err := row.Scan(&result.ID, &result.Version); err != nil {
		if isAdminProductSlugUniqueViolation(err) {
			return adminProductWriteResult{}, errAdminProductSlugConflict
		}

		return adminProductWriteResult{}, errAdminProductWriteFailed
	}
	if !isValidAdminProductWriteResult(result) {
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	return result, nil
}

// Update applies an edit only to the revision displayed by the form. A missing
// row and a stale row remain distinct without returning stored Product data.
func (writer *postgresAdminProductWriter) Update(
	ctx context.Context,
	productID int64,
	expectedVersion int64,
	input adminProductWriteInput,
) (adminProductWriteResult, error) {
	if ctx == nil || productID <= 0 || expectedVersion <= 0 ||
		expectedVersion == math.MaxInt64 ||
		!isValidAdminProductWriteInput(input) {
		return adminProductWriteResult{}, errAdminProductWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	row := writer.queryRow(
		ctx,
		updateAdminProductSQL,
		productID,
		expectedVersion,
		input.Slug,
		input.Name,
		input.Category,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	var result adminProductWriteResult
	var productExists bool
	if err := row.Scan(&result.ID, &result.Version, &productExists); err != nil {
		if isAdminProductSlugUniqueViolation(err) {
			return adminProductWriteResult{}, errAdminProductSlugConflict
		}

		return adminProductWriteResult{}, errAdminProductWriteFailed
	}
	if result == (adminProductWriteResult{}) {
		if productExists {
			return adminProductWriteResult{}, errAdminProductWriteConflict
		}

		return adminProductWriteResult{}, errAdminProductNotFound
	}
	if !productExists || result.ID != productID ||
		result.Version != expectedVersion+1 ||
		!isValidAdminProductWriteResult(result) {
		// Impossible combinations indicate a broken dependency contract; guessing
		// success here could redirect after an unverified mutation.
		return adminProductWriteResult{}, errAdminProductWriteFailed
	}

	return result, nil
}

// isValidAdminProductWriteInput mirrors every administrator-editable schema
// invariant before a query is attempted.
func isValidAdminProductWriteInput(input adminProductWriteInput) bool {
	return isCanonicalProductSlug(input.Slug) &&
		isValidProductCatalogueText(input.Name, productNameMaximumLength) &&
		isValidProductCatalogueText(input.Category, productCategoryMaximumLength) &&
		input.SortOrder > 0 && input.SortOrder <= math.MaxInt32 &&
		isValidProductPublicationStatus(input.PublicationStatus)
}

// isValidAdminProductWriteResult accepts only database-owned positive values.
func isValidAdminProductWriteResult(result adminProductWriteResult) bool {
	return result.ID > 0 && result.Version > 0
}

// isAdminProductSlugUniqueViolation recognizes only the named migration-owned
// slug constraint. Other uniqueness failures remain generic dependency errors.
func isAdminProductSlugUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "products_slug_unique"
}
