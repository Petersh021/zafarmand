package main

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/jackc/pgx/v5/pgconn"
)

// Protected Architecture-project write errors are stable, response-safe
// categories. Driver diagnostics and rejected administrator copy never cross
// this persistence boundary.
var (
	// errAdminArchitectureProjectWriterDatabaseRequired rejects construction
	// without the application-owned PostgreSQL pool.
	errAdminArchitectureProjectWriterDatabaseRequired = errors.New(
		"create admin architecture project writer: database is required",
	)
	// errAdminArchitectureProjectWriteInvalid rejects invalid coordinates or
	// content before PostgreSQL is contacted.
	errAdminArchitectureProjectWriteInvalid = errors.New(
		"admin architecture project write is invalid",
	)
	// errAdminArchitectureProjectWriteConflict identifies a valid edit based on
	// an obsolete protected project revision.
	errAdminArchitectureProjectWriteConflict = errors.New(
		"admin architecture project version conflict",
	)
	// errAdminArchitectureProjectSlugConflict is the one named uniqueness
	// collision that an administrator can correct safely.
	errAdminArchitectureProjectSlugConflict = errors.New(
		"admin architecture project slug already exists",
	)
	// errAdminArchitectureProjectWriteFailed collapses every other execution,
	// scan, and result-contract failure without wrapping dependency details.
	errAdminArchitectureProjectWriteFailed = errors.New(
		"admin architecture project database write failed",
	)
)

// adminArchitectureProjectWriteInput contains exactly the administrator-owned
// Architecture facts. Identity, revisions, timestamps, and media are separate.
type adminArchitectureProjectWriteInput struct {
	// Slug is the canonical lowercase public route segment.
	Slug string
	// Title is the required bounded public heading.
	Title string
	// Typology is the required bounded Architecture category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// ProjectYear is zero for SQL NULL or a supported four-digit year.
	ProjectYear int
	// ProjectStatus is a required public fact such as Completed.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// SortOrder is the positive editorial position.
	SortOrder int
	// PublicationStatus is one exact value from the closed lifecycle vocabulary.
	PublicationStatus string
}

// adminArchitectureProjectWriteResult returns only database-owned coordinates
// needed by canonical redirects and concurrency-aware editing.
type adminArchitectureProjectWriteResult struct {
	// ID is the positive identity of the inserted or updated project.
	ID int64
	// Version is the positive revision after the successful write.
	Version int64
}

// adminArchitectureProjectWriter is the narrow project and single-cover
// mutation authority. Deletion is intentionally absent pending a retention and
// confirmation policy.
type adminArchitectureProjectWriter interface {
	// Create inserts one validated project and returns its identity and revision.
	Create(
		context.Context,
		adminArchitectureProjectWriteInput,
	) (adminArchitectureProjectWriteResult, error)
	// Update stores one validated edit only while expectedVersion is current.
	Update(
		context.Context,
		int64,
		int64,
		adminArchitectureProjectWriteInput,
	) (adminArchitectureProjectWriteResult, error)
	// UpsertCover atomically advances a project revision and inserts or replaces
	// its one reviewed cover.
	UpsertCover(
		context.Context,
		int64,
		int64,
		adminArchitectureProjectCoverWriteInput,
	) (adminArchitectureProjectCoverWriteResult, error)
}

// createAdminArchitectureProjectSQL binds every editable value. PostgreSQL
// owns identity, timestamps, the initial revision, and nullable year storage.
const createAdminArchitectureProjectSQL = `INSERT INTO public.architecture_projects (
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

// updateAdminArchitectureProjectSQL observes existence and performs an
// optimistic mutation in one statement. A stale form cannot alter data.
const updateAdminArchitectureProjectSQL = `WITH current_project AS MATERIALIZED (
    SELECT id
    FROM public.architecture_projects
    WHERE id = $1
),
updated_project AS (
    UPDATE public.architecture_projects
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

// adminArchitectureProjectWriteRowScanner is the fixed single-row behavior
// shared by create, update, and cover-upsert results.
type adminArchitectureProjectWriteRowScanner interface {
	// Scan copies one statement result into supplied destinations.
	Scan(...any) error
}

// adminArchitectureProjectWriteQueryRow is the narrow database/sql operation
// used by the writer and supplies a dependency-free unit-test seam.
type adminArchitectureProjectWriteQueryRow func(
	context.Context,
	string,
	...any,
) adminArchitectureProjectWriteRowScanner

// postgresAdminArchitectureProjectWriter borrows the process-owned pool for
// concurrent protected mutations. The application process closes that pool.
type postgresAdminArchitectureProjectWriter struct {
	// queryRow executes one trusted parameterized mutation statement.
	queryRow adminArchitectureProjectWriteQueryRow
}

// Compile-time verification prevents drift from the protected write contract.
var _ adminArchitectureProjectWriter = (*postgresAdminArchitectureProjectWriter)(nil)

// newPostgresAdminArchitectureProjectWriter adapts the shared pool without
// opening a connection or issuing SQL during construction.
func newPostgresAdminArchitectureProjectWriter(
	database *sql.DB,
) (*postgresAdminArchitectureProjectWriter, error) {
	if database == nil {
		return nil, errAdminArchitectureProjectWriterDatabaseRequired
	}

	return &postgresAdminArchitectureProjectWriter{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminArchitectureProjectWriteRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// Create inserts one validated Architecture project. Only the named slug
// uniqueness violation becomes a correctable conflict.
func (writer *postgresAdminArchitectureProjectWriter) Create(
	ctx context.Context,
	input adminArchitectureProjectWriteInput,
) (adminArchitectureProjectWriteResult, error) {
	if ctx == nil || !isValidAdminArchitectureProjectWriteInput(input) {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		createAdminArchitectureProjectSQL,
		input.Slug,
		input.Title,
		input.Typology,
		input.Location,
		nullableAdminArchitectureProjectYear(input.ProjectYear),
		input.ProjectStatus,
		input.Description,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	var result adminArchitectureProjectWriteResult
	if err := row.Scan(&result.ID, &result.Version); err != nil {
		if isAdminArchitectureProjectSlugUniqueViolation(err) {
			return adminArchitectureProjectWriteResult{},
				errAdminArchitectureProjectSlugConflict
		}
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}
	if !isValidAdminArchitectureProjectWriteResult(result) {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	return result, nil
}

// Update applies one edit only to the revision displayed by the form. Missing
// and stale rows remain distinct without returning stored project content.
func (writer *postgresAdminArchitectureProjectWriter) Update(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminArchitectureProjectWriteInput,
) (adminArchitectureProjectWriteResult, error) {
	if ctx == nil || projectID <= 0 || expectedVersion <= 0 ||
		expectedVersion == math.MaxInt64 ||
		!isValidAdminArchitectureProjectWriteInput(input) {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		updateAdminArchitectureProjectSQL,
		projectID,
		expectedVersion,
		input.Slug,
		input.Title,
		input.Typology,
		input.Location,
		nullableAdminArchitectureProjectYear(input.ProjectYear),
		input.ProjectStatus,
		input.Description,
		input.SortOrder,
		input.PublicationStatus,
	)
	if row == nil {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	var result adminArchitectureProjectWriteResult
	var projectExists bool
	if err := row.Scan(&result.ID, &result.Version, &projectExists); err != nil {
		if isAdminArchitectureProjectSlugUniqueViolation(err) {
			return adminArchitectureProjectWriteResult{},
				errAdminArchitectureProjectSlugConflict
		}
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}
	if result == (adminArchitectureProjectWriteResult{}) {
		if projectExists {
			return adminArchitectureProjectWriteResult{},
				errAdminArchitectureProjectWriteConflict
		}
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectNotFound
	}
	if !projectExists || result.ID != projectID ||
		result.Version != expectedVersion+1 ||
		!isValidAdminArchitectureProjectWriteResult(result) {
		return adminArchitectureProjectWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	return result, nil
}

// nullableAdminArchitectureProjectYear maps the explicit zero-domain value to
// SQL NULL while preserving a supported four-digit year.
func nullableAdminArchitectureProjectYear(projectYear int) any {
	if projectYear == 0 {
		return nil
	}
	return projectYear
}

// isValidAdminArchitectureProjectWriteInput mirrors every administrator-
// editable migration-8 invariant before a database call is attempted.
func isValidAdminArchitectureProjectWriteInput(
	input adminArchitectureProjectWriteInput,
) bool {
	return isCanonicalArchitectureProjectSlug(input.Slug) &&
		isValidArchitectureProjectCatalogueText(
			input.Title,
			architectureProjectTitleMaximumLength,
		) &&
		isValidArchitectureProjectCatalogueText(
			input.Typology,
			architectureProjectTypologyMaximumLength,
		) &&
		(input.Location == "" || isValidArchitectureProjectCatalogueText(
			input.Location,
			architectureProjectLocationMaximumLength,
		)) &&
		isValidArchitectureProjectYear(input.ProjectYear) &&
		isValidArchitectureProjectCatalogueText(
			input.ProjectStatus,
			architectureProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			input.Description,
			architectureProjectDescriptionMaximumLength,
		) &&
		input.SortOrder > 0 && input.SortOrder <= math.MaxInt32 &&
		isValidArchitectureProjectPublicationStatus(input.PublicationStatus)
}

// isValidAdminArchitectureProjectWriteResult accepts only database-owned
// positive identity and revision values.
func isValidAdminArchitectureProjectWriteResult(
	result adminArchitectureProjectWriteResult,
) bool {
	return result.ID > 0 && result.Version > 0
}

// isAdminArchitectureProjectSlugUniqueViolation recognizes only migration 8's
// named slug constraint; every other driver failure remains generic.
func isAdminArchitectureProjectSlugUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "architecture_projects_slug_unique"
}
