package main

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/jackc/pgx/v5/pgconn"
)

// Protected Interior-project write errors are stable, response-safe categories.
// Driver diagnostics and rejected administrator text never cross this boundary.
var (
	// errAdminInteriorProjectWriterDatabaseRequired rejects construction without
	// the application-owned PostgreSQL pool.
	errAdminInteriorProjectWriterDatabaseRequired = errors.New(
		"create admin interior project writer: database is required",
	)
	// errAdminInteriorProjectWriteInvalid rejects an invalid context, identity,
	// revision, project input, or cover input before PostgreSQL is contacted.
	errAdminInteriorProjectWriteInvalid = errors.New(
		"admin interior project write is invalid",
	)
	// errAdminInteriorProjectWriteConflict identifies a valid edit based on an
	// obsolete project revision.
	errAdminInteriorProjectWriteConflict = errors.New(
		"admin interior project version conflict",
	)
	// errAdminInteriorProjectSlugConflict identifies the one named uniqueness
	// collision an administrator can correct safely.
	errAdminInteriorProjectSlugConflict = errors.New(
		"admin interior project slug already exists",
	)
	// errAdminInteriorProjectWriteFailed collapses every other execution, scan,
	// and result-contract failure without wrapping dependency detail.
	errAdminInteriorProjectWriteFailed = errors.New(
		"admin interior project database write failed",
	)
)

// adminInteriorProjectWriteInput contains exactly the administrator-controlled
// Interior facts. Identity, revisions, timestamps, and cover bytes have separate
// ownership and validation boundaries.
type adminInteriorProjectWriteInput struct {
	// Slug is the canonical lowercase public route segment.
	Slug string
	// Title is the required bounded public heading.
	Title string
	// Typology is the required bounded Interior category.
	Typology string
	// Location is optional reviewed public location text.
	Location string
	// ProjectYear is zero for SQL NULL or a supported four-digit year.
	ProjectYear int
	// ProjectStatus is a required public editorial fact such as Completed.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// SortOrder is the positive editorial position.
	SortOrder int
	// PublicationStatus is one exact value from the closed lifecycle vocabulary.
	PublicationStatus string
}

// adminInteriorProjectWriteResult returns only database-owned coordinates
// needed by canonical redirects and subsequent concurrency-aware editing.
type adminInteriorProjectWriteResult struct {
	// ID is the positive identity of the inserted or updated project.
	ID int64
	// Version is the positive revision after the successful write.
	Version int64
}

// adminInteriorProjectWriter is the narrow text and single-cover mutation
// authority. Deletion is deliberately absent because it needs an explicit
// retention and confirmation policy.
type adminInteriorProjectWriter interface {
	// Create inserts one validated project and returns its identity and revision.
	Create(
		context.Context,
		adminInteriorProjectWriteInput,
	) (adminInteriorProjectWriteResult, error)
	// Update stores one validated edit only while expectedVersion remains current.
	Update(
		context.Context,
		int64,
		int64,
		adminInteriorProjectWriteInput,
	) (adminInteriorProjectWriteResult, error)
	// UpsertCover atomically advances one project revision and inserts or replaces
	// its single reviewed cover.
	UpsertCover(
		context.Context,
		int64,
		int64,
		adminInteriorProjectCoverWriteInput,
	) (adminInteriorProjectCoverWriteResult, error)
}

// createAdminInteriorProjectSQL binds every editable value. PostgreSQL owns the
// identity, timestamps, initial version, and nullable representation of year.
const createAdminInteriorProjectSQL = `INSERT INTO public.interior_projects (
    slug,
    title,
    typology,
    location,
    project_year,
    project_status,
    description,
    sort_order,
    publication_status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, version`

// updateAdminInteriorProjectSQL observes existence and performs the
// version-guarded mutation in one PostgreSQL statement. A stale form changes no
// data while remaining distinguishable from a missing identity.
const updateAdminInteriorProjectSQL = `WITH current_project AS MATERIALIZED (
    SELECT id
    FROM public.interior_projects
    WHERE id = $1
),
updated_project AS (
    UPDATE public.interior_projects
    SET
        slug = $3,
        title = $4,
        typology = $5,
        location = $6,
        project_year = $7,
        project_status = $8,
        description = $9,
        sort_order = $10,
        publication_status = $11,
        updated_at = CURRENT_TIMESTAMP,
        version = version + 1
    WHERE id = $1 AND version = $2
    RETURNING id, version
)
SELECT
    COALESCE((SELECT id FROM updated_project), 0),
    COALESCE((SELECT version FROM updated_project), 0),
    EXISTS(SELECT 1 FROM current_project)`

// adminInteriorProjectWriteRowScanner is the fixed single-row behavior shared
// by create, update, and cover-upsert results.
type adminInteriorProjectWriteRowScanner interface {
	// Scan copies one statement result into supplied destinations.
	Scan(...any) error
}

// adminInteriorProjectWriteQueryRow is the narrow database/sql operation used
// by the writer and provides a dependency-free unit-test seam.
type adminInteriorProjectWriteQueryRow func(
	context.Context,
	string,
	...any,
) adminInteriorProjectWriteRowScanner

// postgresAdminInteriorProjectWriter borrows the process-owned pool for
// concurrent protected mutations. The application process closes that pool.
type postgresAdminInteriorProjectWriter struct {
	// queryRow executes one trusted parameterized mutation statement.
	queryRow adminInteriorProjectWriteQueryRow
}

// Compile-time verification prevents drift from the protected write contract.
var _ adminInteriorProjectWriter = (*postgresAdminInteriorProjectWriter)(nil)

// newPostgresAdminInteriorProjectWriter adapts the shared database pool without
// opening a connection or issuing SQL during construction.
func newPostgresAdminInteriorProjectWriter(
	database *sql.DB,
) (*postgresAdminInteriorProjectWriter, error) {
	if database == nil {
		return nil, errAdminInteriorProjectWriterDatabaseRequired
	}

	return &postgresAdminInteriorProjectWriter{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminInteriorProjectWriteRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// Create inserts one validated Interior project. Only the named slug uniqueness
// violation becomes a correctable conflict; all other diagnostics are redacted.
func (writer *postgresAdminInteriorProjectWriter) Create(
	ctx context.Context,
	input adminInteriorProjectWriteInput,
) (adminInteriorProjectWriteResult, error) {
	if ctx == nil || !isValidAdminInteriorProjectWriteInput(input) {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		createAdminInteriorProjectSQL,
		input.Slug,
		input.Title,
		input.Typology,
		input.Location,
		nullableAdminInteriorProjectYear(input.ProjectYear),
		input.ProjectStatus,
		input.Description,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	var result adminInteriorProjectWriteResult
	if err := row.Scan(&result.ID, &result.Version); err != nil {
		if isAdminInteriorProjectSlugUniqueViolation(err) {
			return adminInteriorProjectWriteResult{},
				errAdminInteriorProjectSlugConflict
		}

		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}
	if !isValidAdminInteriorProjectWriteResult(result) {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	return result, nil
}

// Update applies one edit only to the revision displayed by the form. Missing
// and stale rows stay distinct without returning stored project content.
func (writer *postgresAdminInteriorProjectWriter) Update(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminInteriorProjectWriteInput,
) (adminInteriorProjectWriteResult, error) {
	if ctx == nil || projectID <= 0 || expectedVersion <= 0 ||
		expectedVersion == math.MaxInt64 ||
		!isValidAdminInteriorProjectWriteInput(input) {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		updateAdminInteriorProjectSQL,
		projectID,
		expectedVersion,
		input.Slug,
		input.Title,
		input.Typology,
		input.Location,
		nullableAdminInteriorProjectYear(input.ProjectYear),
		input.ProjectStatus,
		input.Description,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	var result adminInteriorProjectWriteResult
	var projectExists bool
	if err := row.Scan(
		&result.ID,
		&result.Version,
		&projectExists,
	); err != nil {
		if isAdminInteriorProjectSlugUniqueViolation(err) {
			return adminInteriorProjectWriteResult{},
				errAdminInteriorProjectSlugConflict
		}

		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}
	if result == (adminInteriorProjectWriteResult{}) {
		if projectExists {
			return adminInteriorProjectWriteResult{},
				errAdminInteriorProjectWriteConflict
		}

		return adminInteriorProjectWriteResult{}, errAdminInteriorProjectNotFound
	}
	if !projectExists || result.ID != projectID ||
		result.Version != expectedVersion+1 ||
		!isValidAdminInteriorProjectWriteResult(result) {
		return adminInteriorProjectWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	return result, nil
}

// nullableAdminInteriorProjectYear maps the explicit zero-domain value to SQL
// NULL while preserving a supported four-digit year as a database argument.
func nullableAdminInteriorProjectYear(projectYear int) any {
	if projectYear == 0 {
		return nil
	}

	return projectYear
}

// isValidAdminInteriorProjectWriteInput mirrors every administrator-editable
// migration invariant before a database operation is attempted.
func isValidAdminInteriorProjectWriteInput(
	input adminInteriorProjectWriteInput,
) bool {
	return isCanonicalInteriorProjectSlug(input.Slug) &&
		isValidInteriorProjectCatalogueText(
			input.Title,
			interiorProjectTitleMaximumLength,
		) &&
		isValidInteriorProjectCatalogueText(
			input.Typology,
			interiorProjectTypologyMaximumLength,
		) &&
		(input.Location == "" || isValidInteriorProjectCatalogueText(
			input.Location,
			interiorProjectLocationMaximumLength,
		)) &&
		isValidInteriorProjectYear(input.ProjectYear) &&
		isValidInteriorProjectCatalogueText(
			input.ProjectStatus,
			interiorProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			input.Description,
			interiorProjectDescriptionMaximumLength,
		) &&
		input.SortOrder > 0 && input.SortOrder <= math.MaxInt32 &&
		isValidInteriorProjectPublicationStatus(input.PublicationStatus)
}

// isValidAdminInteriorProjectWriteResult accepts only database-owned positive
// identity and revision values.
func isValidAdminInteriorProjectWriteResult(
	result adminInteriorProjectWriteResult,
) bool {
	return result.ID > 0 && result.Version > 0
}

// isAdminInteriorProjectSlugUniqueViolation recognizes only migration 7's
// named slug constraint. Every other driver failure remains generic.
func isAdminInteriorProjectSlugUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "interior_projects_slug_unique"
}
