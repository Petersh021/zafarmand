package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Protected Architecture-project read errors are stable categories. They do
// not retain SQL, database credentials, driver diagnostics, or managed copy.
var (
	// errAdminArchitectureProjectReaderDatabaseRequired rejects a constructor
	// call that does not supply the application-owned PostgreSQL pool.
	errAdminArchitectureProjectReaderDatabaseRequired = errors.New(
		"create admin architecture project reader: database is required",
	)
	// errAdminArchitectureProjectInvalidQuery rejects invalid read coordinates
	// before PostgreSQL is contacted.
	errAdminArchitectureProjectInvalidQuery = errors.New(
		"admin architecture project query is invalid",
	)
	// errAdminArchitectureProjectNotFound identifies a genuinely absent protected
	// project while revealing no administrator-controlled content.
	errAdminArchitectureProjectNotFound = errors.New(
		"admin architecture project not found",
	)
	// errAdminArchitectureProjectReadFailed collapses all operational and stored-
	// contract failures into one response-safe dependency category.
	errAdminArchitectureProjectReadFailed = errors.New(
		"admin architecture project database operation failed",
	)
)

// adminArchitectureProjectRecord is the complete protected project projection.
// Cover bytes remain on a separate exact-revision path so list and detail reads
// carry only small reviewed metadata.
type adminArchitectureProjectRecord struct {
	// ID is PostgreSQL's positive internal identity.
	ID int64
	// Slug is the canonical public route segment in every lifecycle state.
	Slug string
	// Title is the required reviewed public heading.
	Title string
	// Typology is the required reviewed Architecture category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// ProjectYear is zero when the nullable database year is absent.
	ProjectYear int
	// ProjectStatus is required editorial copy distinct from publication state.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// SortOrder is the positive editorial position used before ID.
	SortOrder int
	// PublicationStatus is one lowercase value from the closed lifecycle set.
	PublicationStatus string
	// Cover contains binary-free reviewed metadata, or nil when absent.
	Cover *architectureProjectCoverMetadata
	// Version is the positive revision used to reject stale mutations.
	Version int64
	// CreatedAt is the database-owned creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the database-owned last-mutation timestamp.
	UpdatedAt time.Time
}

// adminArchitectureProjectReader is the protected all-state read authority. It
// deliberately exposes no mutation methods.
type adminArchitectureProjectReader interface {
	// List returns every lifecycle state in deterministic editorial order.
	List(context.Context) ([]adminArchitectureProjectRecord, error)
	// FindByID returns one protected project selected by positive internal ID.
	FindByID(context.Context, int64) (adminArchitectureProjectRecord, error)
	// FindCoverByProjectID returns one exact protected cover revision regardless
	// of the owning project's publication state.
	FindCoverByProjectID(
		context.Context,
		int64,
		int64,
	) (architectureProjectCoverAsset, error)
}

// listAdminArchitectureProjectsSQL selects all managed project facts plus
// nullable cover metadata, but never binary image bytes.
const listAdminArchitectureProjectsSQL = `SELECT
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
FROM public.architecture_projects AS projects
LEFT JOIN public.architecture_project_cover_images AS cover
    ON cover.architecture_project_id = projects.id
ORDER BY projects.sort_order ASC, projects.id ASC`

// findAdminArchitectureProjectByIDSQL uses an internal positive identity only
// in the protected area; public lookups never receive Draft or Archived slugs.
const findAdminArchitectureProjectByIDSQL = `SELECT
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
FROM public.architecture_projects AS projects
LEFT JOIN public.architecture_project_cover_images AS cover
    ON cover.architecture_project_id = projects.id
WHERE projects.id = $1`

// findAdminArchitectureProjectCoverSQL loads binary media only for one exact
// protected owner and cover revision.
const findAdminArchitectureProjectCoverSQL = `SELECT
    architecture_project_id,
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
FROM public.architecture_project_cover_images
WHERE architecture_project_id = $1 AND version = $2`

// adminArchitectureProjectRows is the smallest iterator needed by List and
// keeps deterministic unit tests independent from a mocking package.
type adminArchitectureProjectRows interface {
	// Next advances to the next projection.
	Next() bool
	// Scan copies the current fixed projection into destinations.
	Scan(...any) error
	// Err reports a failure discovered after iteration.
	Err() error
	// Close returns the borrowed database connection to its pool.
	Close() error
}

// adminArchitectureProjectScanner is shared by list and detail row scans.
type adminArchitectureProjectScanner interface {
	// Scan copies one fixed protected projection into destinations.
	Scan(...any) error
}

// adminArchitectureProjectQuery adapts database/sql's ordered list operation
// to a narrow injectable test seam.
type adminArchitectureProjectQuery func(
	context.Context,
	string,
	...any,
) (adminArchitectureProjectRows, error)

// adminArchitectureProjectRowScanner is the single-row behavior needed by
// protected detail and exact-cover reads.
type adminArchitectureProjectRowScanner interface {
	// Scan copies one query result into destinations.
	Scan(...any) error
}

// adminArchitectureProjectQueryRow adapts database/sql's single-row operation
// without exposing the entire pool to repository unit tests.
type adminArchitectureProjectQueryRow func(
	context.Context,
	string,
	...any,
) adminArchitectureProjectRowScanner

// postgresAdminArchitectureProjectReader borrows the process-owned pool for
// concurrent protected reads. The process that opens the pool closes it.
type postgresAdminArchitectureProjectReader struct {
	// query executes the all-state ordered list statement.
	query adminArchitectureProjectQuery
	// queryRow executes positive-ID project and exact-cover statements.
	queryRow adminArchitectureProjectQueryRow
}

// Compile-time verification keeps the adapter aligned with the protected read
// contract as both evolve.
var _ adminArchitectureProjectReader = (*postgresAdminArchitectureProjectReader)(nil)

// newPostgresAdminArchitectureProjectReader adapts the shared pool without
// opening a connection or issuing a query during construction.
func newPostgresAdminArchitectureProjectReader(
	database *sql.DB,
) (*postgresAdminArchitectureProjectReader, error) {
	if database == nil {
		return nil, errAdminArchitectureProjectReaderDatabaseRequired
	}

	return &postgresAdminArchitectureProjectReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (adminArchitectureProjectRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminArchitectureProjectRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// List reads and validates every protected Architecture project before any
// managed value can reach an administrator handler.
func (reader *postgresAdminArchitectureProjectReader) List(
	ctx context.Context,
) ([]adminArchitectureProjectRecord, error) {
	if ctx == nil {
		return nil, errAdminArchitectureProjectInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errAdminArchitectureProjectReadFailed
	}

	rows, err := reader.query(ctx, listAdminArchitectureProjectsSQL)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}
		return nil, errAdminArchitectureProjectReadFailed
	}
	if rows == nil {
		return nil, errAdminArchitectureProjectReadFailed
	}

	projects := make([]adminArchitectureProjectRecord, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	var previous adminArchitectureProjectRecord
	for rows.Next() {
		project, scanErr := scanAdminArchitectureProject(rows)
		if scanErr != nil || !isValidStoredAdminArchitectureProject(project) ||
			(len(projects) > 0 &&
				!adminArchitectureProjectFollows(project, previous)) {
			_ = rows.Close()
			return nil, errAdminArchitectureProjectReadFailed
		}
		if _, duplicate := seenIDs[project.ID]; duplicate {
			_ = rows.Close()
			return nil, errAdminArchitectureProjectReadFailed
		}
		if _, duplicate := seenSlugs[project.Slug]; duplicate {
			_ = rows.Close()
			return nil, errAdminArchitectureProjectReadFailed
		}

		projects = append(projects, project)
		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
		previous = project
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errAdminArchitectureProjectReadFailed
	}

	return projects[:len(projects):len(projects)], nil
}

// FindByID reads one protected Architecture project and distinguishes only a
// genuine missing row from safe operational failure categories.
func (reader *postgresAdminArchitectureProjectReader) FindByID(
	ctx context.Context,
	projectID int64,
) (adminArchitectureProjectRecord, error) {
	if ctx == nil || projectID <= 0 {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectReadFailed
	}

	row := reader.queryRow(ctx, findAdminArchitectureProjectByIDSQL, projectID)
	if row == nil {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectReadFailed
	}

	project, err := scanAdminArchitectureProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return adminArchitectureProjectRecord{}, errAdminArchitectureProjectNotFound
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminArchitectureProject(project) {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectReadFailed
	}

	return project, nil
}

// FindCoverByProjectID returns one exact protected cover revision. Missing and
// stale cover coordinates share the Architecture cover-not-found category.
func (reader *postgresAdminArchitectureProjectReader) FindCoverByProjectID(
	ctx context.Context,
	projectID int64,
	coverVersion int64,
) (architectureProjectCoverAsset, error) {
	if ctx == nil || projectID <= 0 || coverVersion <= 0 {
		return architectureProjectCoverAsset{},
			errAdminArchitectureProjectInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return architectureProjectCoverAsset{},
			errArchitectureProjectCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findAdminArchitectureProjectCoverSQL,
		projectID,
		coverVersion,
	)
	if row == nil {
		return architectureProjectCoverAsset{},
			errArchitectureProjectCoverReadFailed
	}

	asset, err := scanAdminArchitectureProjectCover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverNotFound
	}
	if err != nil || asset.ArchitectureProjectID != projectID ||
		asset.Version != coverVersion ||
		!isValidArchitectureProjectCoverAsset(asset) {
		return architectureProjectCoverAsset{},
			errArchitectureProjectCoverReadFailed
	}

	return asset, nil
}

// scanAdminArchitectureProject converts nullable year and cover columns into
// explicit Go representations and rejects partially NULL cover projections.
func scanAdminArchitectureProject(
	scanner adminArchitectureProjectScanner,
) (adminArchitectureProjectRecord, error) {
	if scanner == nil {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectReadFailed
	}

	var project adminArchitectureProjectRecord
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
		return adminArchitectureProjectRecord{}, err
	}

	if projectYear.Valid {
		if projectYear.Int64 > int64(math.MaxInt) ||
			projectYear.Int64 < int64(math.MinInt) {
			return adminArchitectureProjectRecord{},
				errAdminArchitectureProjectReadFailed
		}
		project.ProjectYear = int(projectYear.Int64)
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return adminArchitectureProjectRecord{},
			errAdminArchitectureProjectReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(math.MaxInt) ||
			coverWidth.Int64 < int64(math.MinInt) ||
			coverHeight.Int64 > int64(math.MaxInt) ||
			coverHeight.Int64 < int64(math.MinInt) {
			return adminArchitectureProjectRecord{},
				errAdminArchitectureProjectReadFailed
		}
		project.Cover = &architectureProjectCoverMetadata{
			Version: coverVersion.Int64,
			Width:   int(coverWidth.Int64),
			Height:  int(coverHeight.Int64),
			AltText: coverAltText.String,
			Caption: coverCaption.String,
		}
	}

	return project, nil
}

// scanAdminArchitectureProjectCover copies one binary cover row and converts
// its variable-length database digest into the fixed-size Go representation.
func scanAdminArchitectureProjectCover(
	scanner adminArchitectureProjectRowScanner,
) (architectureProjectCoverAsset, error) {
	if scanner == nil {
		return architectureProjectCoverAsset{},
			errArchitectureProjectCoverReadFailed
	}

	var asset architectureProjectCoverAsset
	var digest []byte
	err := scanner.Scan(
		&asset.ArchitectureProjectID,
		&asset.Version,
		&asset.ContentType,
		&asset.Content,
		&asset.ByteSize,
		&asset.Width,
		&asset.Height,
		&digest,
		&asset.AltText,
		&asset.Caption,
		&asset.CreatedAt,
		&asset.UpdatedAt,
	)
	if err != nil {
		return architectureProjectCoverAsset{}, err
	}
	if len(digest) != sha256.Size {
		return architectureProjectCoverAsset{},
			errArchitectureProjectCoverReadFailed
	}
	copy(asset.SHA256[:], digest)

	return asset, nil
}

// isValidArchitectureProjectPublicationStatus recognizes only migration 8's
// fail-closed lifecycle vocabulary.
func isValidArchitectureProjectPublicationStatus(status string) bool {
	return status == draftArchitectureProjectStatus ||
		status == publishedArchitectureProjectStatus ||
		status == archivedArchitectureProjectStatus
}

// isValidStoredAdminArchitectureProject rechecks every selected schema
// invariant before managed content can influence protected HTML.
func isValidStoredAdminArchitectureProject(
	project adminArchitectureProjectRecord,
) bool {
	return project.ID > 0 &&
		isCanonicalArchitectureProjectSlug(project.Slug) &&
		isValidArchitectureProjectCatalogueText(
			project.Title,
			architectureProjectTitleMaximumLength,
		) &&
		isValidArchitectureProjectCatalogueText(
			project.Typology,
			architectureProjectTypologyMaximumLength,
		) &&
		(project.Location == "" || isValidArchitectureProjectCatalogueText(
			project.Location,
			architectureProjectLocationMaximumLength,
		)) &&
		isValidArchitectureProjectYear(project.ProjectYear) &&
		isValidArchitectureProjectCatalogueText(
			project.ProjectStatus,
			architectureProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Description,
			architectureProjectDescriptionMaximumLength,
		) &&
		project.SortOrder > 0 && project.SortOrder <= math.MaxInt32 &&
		isValidArchitectureProjectPublicationStatus(project.PublicationStatus) &&
		(project.Cover == nil ||
			isValidArchitectureProjectCoverMetadata(*project.Cover)) &&
		project.Version > 0 &&
		!project.CreatedAt.IsZero() &&
		!project.UpdatedAt.IsZero() &&
		!project.UpdatedAt.Before(project.CreatedAt)
}

// adminArchitectureProjectFollows verifies strict `(sort_order, id)` ordering
// for consecutive all-state repository results.
func adminArchitectureProjectFollows(
	current adminArchitectureProjectRecord,
	previous adminArchitectureProjectRecord,
) bool {
	return current.SortOrder > previous.SortOrder ||
		(current.SortOrder == previous.SortOrder && current.ID > previous.ID)
}
