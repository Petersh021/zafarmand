package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Protected Interior-project read errors are fixed categories that never
// retain SQL, database credentials, driver diagnostics, or managed project
// content.
var (
	// errAdminInteriorProjectReaderDatabaseRequired rejects construction without
	// the application-owned PostgreSQL pool.
	errAdminInteriorProjectReaderDatabaseRequired = errors.New(
		"create admin interior project reader: database is required",
	)
	// errAdminInteriorProjectInvalidQuery rejects a nil context or non-positive
	// project/media coordinate before PostgreSQL is contacted.
	errAdminInteriorProjectInvalidQuery = errors.New(
		"admin interior project query is invalid",
	)
	// errAdminInteriorProjectNotFound identifies only a genuine missing protected
	// project row.
	errAdminInteriorProjectNotFound = errors.New(
		"admin interior project not found",
	)
	// errAdminInteriorProjectReadFailed collapses every other query, scan,
	// ordering, and stored-data failure into one safe dependency category.
	errAdminInteriorProjectReadFailed = errors.New(
		"admin interior project database operation failed",
	)
)

// adminInteriorProjectRecord is the complete protected project projection.
// Cover bytes remain on a separate exact-revision path so ordinary list and
// detail reads carry only small reviewed metadata.
type adminInteriorProjectRecord struct {
	// ID is PostgreSQL's positive internal project identity.
	ID int64
	// Slug is the canonical public route segment in every lifecycle state.
	Slug string
	// Title is the reviewed project heading.
	Title string
	// Typology is the reviewed Interior-project category.
	Typology string
	// Location is optional reviewed public location text.
	Location string
	// ProjectYear is zero when the nullable database year is absent.
	ProjectYear int
	// ProjectStatus is a required editorial fact distinct from publication state.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// SortOrder is the positive editorial position used before ID.
	SortOrder int
	// PublicationStatus is one lowercase value from the closed lifecycle set.
	PublicationStatus string
	// Cover contains binary-free reviewed metadata, or nil when absent.
	Cover *interiorProjectCoverMetadata
	// Version is the positive revision used to reject stale mutations.
	Version int64
	// CreatedAt is the database-owned creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the database-owned last mutation timestamp.
	UpdatedAt time.Time
}

// adminInteriorProjectReader is the protected all-state read authority. It has
// no mutation methods, making the read/write permission split explicit at the
// application boundary.
type adminInteriorProjectReader interface {
	// List returns every lifecycle state in deterministic editorial order.
	List(context.Context) ([]adminInteriorProjectRecord, error)
	// FindByID returns one protected project selected by positive internal ID.
	FindByID(context.Context, int64) (adminInteriorProjectRecord, error)
	// FindCoverByProjectID returns one exact protected cover revision regardless
	// of the owning project's publication state.
	FindCoverByProjectID(
		context.Context,
		int64,
		int64,
	) (interiorProjectCoverAsset, error)
}

// listAdminInteriorProjectsSQL selects all managed project facts plus nullable
// cover metadata, never binary image content. All lifecycle states remain
// visible inside the authenticated workspace.
const listAdminInteriorProjectsSQL = `SELECT
    projects.id,
    projects.slug,
    projects.title,
    projects.typology,
    projects.location,
    projects.project_year,
    projects.project_status,
    projects.description,
    projects.sort_order,
    projects.publication_status,
    cover.version,
    cover.width,
    cover.height,
    cover.alt_text,
    cover.caption,
    projects.version,
    projects.created_at,
    projects.updated_at
FROM public.interior_projects AS projects
LEFT JOIN public.interior_project_cover_images AS cover
    ON cover.interior_project_id = projects.id
ORDER BY projects.sort_order ASC, projects.id ASC`

// findAdminInteriorProjectByIDSQL uses an internal positive identity only in
// the protected area. Draft and Archived slugs never become public lookups.
const findAdminInteriorProjectByIDSQL = `SELECT
    projects.id,
    projects.slug,
    projects.title,
    projects.typology,
    projects.location,
    projects.project_year,
    projects.project_status,
    projects.description,
    projects.sort_order,
    projects.publication_status,
    cover.version,
    cover.width,
    cover.height,
    cover.alt_text,
    cover.caption,
    projects.version,
    projects.created_at,
    projects.updated_at
FROM public.interior_projects AS projects
LEFT JOIN public.interior_project_cover_images AS cover
    ON cover.interior_project_id = projects.id
WHERE projects.id = $1`

// findAdminInteriorProjectCoverSQL carries binary media only for one protected
// positive project identity and exact positive cover revision.
const findAdminInteriorProjectCoverSQL = `SELECT
    interior_project_id,
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
FROM public.interior_project_cover_images
WHERE interior_project_id = $1 AND version = $2`

// adminInteriorProjectRows is the smallest iterator required by the protected
// list operation and keeps ordinary tests independent from a mocking library.
type adminInteriorProjectRows interface {
	// Next advances to the next available projection.
	Next() bool
	// Scan copies the current fixed projection into supplied destinations.
	Scan(...any) error
	// Err reports a failure discovered after iteration.
	Err() error
	// Close releases the result and returns its connection to the shared pool.
	Close() error
}

// adminInteriorProjectScanner is shared by list and detail projection scans.
type adminInteriorProjectScanner interface {
	// Scan copies one fixed protected projection into supplied destinations.
	Scan(...any) error
}

// adminInteriorProjectQuery adapts database/sql's ordered list operation to a
// narrow injectable seam.
type adminInteriorProjectQuery func(
	context.Context,
	string,
	...any,
) (adminInteriorProjectRows, error)

// adminInteriorProjectRowScanner is the single-row behavior needed by project
// detail and exact-cover reads.
type adminInteriorProjectRowScanner interface {
	// Scan copies one query result into supplied destinations.
	Scan(...any) error
}

// adminInteriorProjectQueryRow adapts database/sql's single-row operation
// without exposing the complete pool to deterministic unit tests.
type adminInteriorProjectQueryRow func(
	context.Context,
	string,
	...any,
) adminInteriorProjectRowScanner

// postgresAdminInteriorProjectReader borrows the process-owned pool for
// concurrent protected reads. The process that opens the pool closes it.
type postgresAdminInteriorProjectReader struct {
	// query executes the all-state ordered list statement.
	query adminInteriorProjectQuery
	// queryRow executes positive-ID project and exact-cover statements.
	queryRow adminInteriorProjectQueryRow
}

// Compile-time verification keeps the PostgreSQL adapter aligned with the
// handler-facing read contract.
var _ adminInteriorProjectReader = (*postgresAdminInteriorProjectReader)(nil)

// newPostgresAdminInteriorProjectReader adapts the shared database pool without
// opening a connection or issuing a query during application construction.
func newPostgresAdminInteriorProjectReader(
	database *sql.DB,
) (*postgresAdminInteriorProjectReader, error) {
	if database == nil {
		return nil, errAdminInteriorProjectReaderDatabaseRequired
	}

	return &postgresAdminInteriorProjectReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (adminInteriorProjectRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminInteriorProjectRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// List reads and validates every protected Interior project before returning
// any managed value to a handler.
func (reader *postgresAdminInteriorProjectReader) List(
	ctx context.Context,
) ([]adminInteriorProjectRecord, error) {
	if ctx == nil {
		return nil, errAdminInteriorProjectInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errAdminInteriorProjectReadFailed
	}

	rows, err := reader.query(ctx, listAdminInteriorProjectsSQL)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}

		return nil, errAdminInteriorProjectReadFailed
	}
	if rows == nil {
		return nil, errAdminInteriorProjectReadFailed
	}

	projects := make([]adminInteriorProjectRecord, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	var previous adminInteriorProjectRecord
	for rows.Next() {
		project, scanErr := scanAdminInteriorProject(rows)
		if scanErr != nil || !isValidStoredAdminInteriorProject(project) ||
			(len(projects) > 0 &&
				!adminInteriorProjectFollows(project, previous)) {
			_ = rows.Close()

			return nil, errAdminInteriorProjectReadFailed
		}
		if _, duplicate := seenIDs[project.ID]; duplicate {
			_ = rows.Close()

			return nil, errAdminInteriorProjectReadFailed
		}
		if _, duplicate := seenSlugs[project.Slug]; duplicate {
			_ = rows.Close()

			return nil, errAdminInteriorProjectReadFailed
		}

		projects = append(projects, project)
		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
		previous = project
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errAdminInteriorProjectReadFailed
	}

	return projects[:len(projects):len(projects)], nil
}

// FindByID reads one protected Interior project. Only a positive identity is
// accepted, and a genuine missing row remains distinct from operational errors.
func (reader *postgresAdminInteriorProjectReader) FindByID(
	ctx context.Context,
	projectID int64,
) (adminInteriorProjectRecord, error) {
	if ctx == nil || projectID <= 0 {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
	}

	row := reader.queryRow(ctx, findAdminInteriorProjectByIDSQL, projectID)
	if row == nil {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
	}

	project, err := scanAdminInteriorProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectNotFound
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminInteriorProject(project) {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
	}

	return project, nil
}

// FindCoverByProjectID returns one exact protected cover revision. Missing
// projects, absent covers, and stale revisions share the common safe
// cover-not-found category without exposing the underlying condition.
func (reader *postgresAdminInteriorProjectReader) FindCoverByProjectID(
	ctx context.Context,
	projectID int64,
	coverVersion int64,
) (interiorProjectCoverAsset, error) {
	if ctx == nil || projectID <= 0 || coverVersion <= 0 {
		return interiorProjectCoverAsset{}, errAdminInteriorProjectInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findAdminInteriorProjectCoverSQL,
		projectID,
		coverVersion,
	)
	if row == nil {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	asset, err := scanInteriorProjectCoverAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverNotFound
	}
	if err != nil || asset.InteriorProjectID != projectID ||
		asset.Version != coverVersion {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	return asset, nil
}

// scanAdminInteriorProject converts nullable year and cover columns into their
// explicit Go representations. A partially NULL cover projection is rejected.
func scanAdminInteriorProject(
	scanner adminInteriorProjectScanner,
) (adminInteriorProjectRecord, error) {
	if scanner == nil {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
	}

	var project adminInteriorProjectRecord
	var projectYear sql.NullInt64
	var coverVersion sql.NullInt64
	var coverWidth sql.NullInt64
	var coverHeight sql.NullInt64
	var coverAltText sql.NullString
	var coverCaption sql.NullString
	err := scanner.Scan(
		&project.ID,
		&project.Slug,
		&project.Title,
		&project.Typology,
		&project.Location,
		&projectYear,
		&project.ProjectStatus,
		&project.Description,
		&project.SortOrder,
		&project.PublicationStatus,
		&coverVersion,
		&coverWidth,
		&coverHeight,
		&coverAltText,
		&coverCaption,
		&project.Version,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if err != nil {
		return adminInteriorProjectRecord{}, err
	}

	if projectYear.Valid {
		if projectYear.Int64 > int64(math.MaxInt) ||
			projectYear.Int64 < int64(math.MinInt) {
			return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
		}
		project.ProjectYear = int(projectYear.Int64)
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(math.MaxInt) ||
			coverWidth.Int64 < int64(math.MinInt) ||
			coverHeight.Int64 > int64(math.MaxInt) ||
			coverHeight.Int64 < int64(math.MinInt) {
			return adminInteriorProjectRecord{}, errAdminInteriorProjectReadFailed
		}
		project.Cover = &interiorProjectCoverMetadata{
			Version: coverVersion.Int64,
			Width:   int(coverWidth.Int64),
			Height:  int(coverHeight.Int64),
			AltText: coverAltText.String,
			Caption: coverCaption.String,
		}
	}

	return project, nil
}

// isValidInteriorProjectPublicationStatus recognizes only the migration-owned
// fail-closed lifecycle vocabulary.
func isValidInteriorProjectPublicationStatus(status string) bool {
	return status == draftInteriorProjectStatus ||
		status == publishedInteriorProjectStatus ||
		status == archivedInteriorProjectStatus
}

// isValidStoredAdminInteriorProject rechecks every selected schema invariant
// before stored content can influence protected HTML or navigation.
func isValidStoredAdminInteriorProject(
	project adminInteriorProjectRecord,
) bool {
	return project.ID > 0 &&
		isCanonicalInteriorProjectSlug(project.Slug) &&
		isValidInteriorProjectCatalogueText(
			project.Title,
			interiorProjectTitleMaximumLength,
		) &&
		isValidInteriorProjectCatalogueText(
			project.Typology,
			interiorProjectTypologyMaximumLength,
		) &&
		(project.Location == "" || isValidInteriorProjectCatalogueText(
			project.Location,
			interiorProjectLocationMaximumLength,
		)) &&
		isValidInteriorProjectYear(project.ProjectYear) &&
		isValidInteriorProjectCatalogueText(
			project.ProjectStatus,
			interiorProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Description,
			interiorProjectDescriptionMaximumLength,
		) &&
		project.SortOrder > 0 && project.SortOrder <= math.MaxInt32 &&
		isValidInteriorProjectPublicationStatus(project.PublicationStatus) &&
		(project.Cover == nil ||
			isValidInteriorProjectCoverMetadata(*project.Cover)) &&
		project.Version > 0 &&
		!project.CreatedAt.IsZero() &&
		!project.UpdatedAt.IsZero() &&
		!project.UpdatedAt.Before(project.CreatedAt)
}

// adminInteriorProjectFollows verifies strict `(sort_order, id)` ordering for
// consecutive all-state repository results.
func adminInteriorProjectFollows(
	current adminInteriorProjectRecord,
	previous adminInteriorProjectRecord,
) bool {
	return current.SortOrder > previous.SortOrder ||
		(current.SortOrder == previous.SortOrder && current.ID > previous.ID)
}
