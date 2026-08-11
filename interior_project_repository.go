package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"time"
)

const (
	// draftInteriorProjectStatus keeps a managed record private while its facts
	// and media are being prepared or reviewed.
	draftInteriorProjectStatus = "draft"
	// publishedInteriorProjectStatus is the only lifecycle state eligible for
	// public Interior list, detail, and cover reads.
	publishedInteriorProjectStatus = "published"
	// archivedInteriorProjectStatus retains a managed record while removing it
	// from every public Interior query.
	archivedInteriorProjectStatus = "archived"
	// interiorProjectSlugMaximumLength mirrors the migration-owned character
	// bound for one canonical Interior project path segment.
	interiorProjectSlugMaximumLength = 120
	// interiorProjectTitleMaximumLength bounds the required public heading.
	interiorProjectTitleMaximumLength = 160
	// interiorProjectTypologyMaximumLength bounds the required project category.
	interiorProjectTypologyMaximumLength = 80
	// interiorProjectLocationMaximumLength bounds optional reviewed location copy.
	interiorProjectLocationMaximumLength = 160
	// interiorProjectStatusMaximumLength bounds required real-world project state.
	interiorProjectStatusMaximumLength = 80
	// interiorProjectDescriptionMaximumLength bounds optional long-form public copy.
	interiorProjectDescriptionMaximumLength = 6000
	// interiorProjectYearMinimum and Maximum describe the supported non-null year
	// range. Go uses zero only to represent SQL NULL across the repository seam.
	interiorProjectYearMinimum = 1000
	interiorProjectYearMaximum = 9999
)

// interiorProjectSlugPattern accepts lowercase ASCII alphanumeric components
// separated by one hyphen. Anchors prevent a valid substring from making a
// malformed visitor path appear canonical.
var interiorProjectSlugPattern = regexp.MustCompile(
	`^[a-z0-9]+(?:-[a-z0-9]+)*$`,
)

// Public Interior repository errors are stable, credential-free categories.
// Driver diagnostics are deliberately collapsed because they may contain SQL,
// connection details, or malformed stored content.
var (
	// errInteriorProjectCatalogueReaderDatabaseRequired rejects construction
	// without the application-owned PostgreSQL pool.
	errInteriorProjectCatalogueReaderDatabaseRequired = errors.New(
		"create interior project catalogue reader: database is required",
	)
	// errInteriorProjectCatalogueInvalidQuery identifies a nil context,
	// malformed slug, or non-positive cover revision before SQL is attempted.
	errInteriorProjectCatalogueInvalidQuery = errors.New(
		"interior project catalogue query is invalid",
	)
	// errInteriorProjectCatalogueNotFound represents an unknown or non-public
	// canonical slug without revealing which condition occurred.
	errInteriorProjectCatalogueNotFound = errors.New(
		"published interior project not found",
	)
	// errInteriorProjectCatalogueReadFailed collapses query, scan, validation,
	// ordering, iteration, and cleanup failures into one safe service category.
	errInteriorProjectCatalogueReadFailed = errors.New(
		"interior project catalogue database operation failed",
	)
	// errInteriorProjectCoverNotFound represents an absent or stale exact cover
	// revision. Public lookups also collapse a hidden owner's lifecycle state.
	errInteriorProjectCoverNotFound = errors.New(
		"interior project cover not found",
	)
	// errInteriorProjectCoverReadFailed collapses binary-media database and
	// stored-contract failures into one safe public/protected category.
	errInteriorProjectCoverReadFailed = errors.New(
		"interior project cover read failed",
	)
)

// interiorProjectCoverMetadata is the binary-free cover projection safe to
// join into public or protected HTML queries. A nil pointer means that no
// reviewed cover currently exists.
type interiorProjectCoverMetadata struct {
	// Version changes whenever the stored image or reviewed metadata changes.
	Version int64
	// Width is the decoded image width in pixels.
	Width int
	// Height is the decoded image height in pixels.
	Height int
	// AltText is the required meaningful image alternative.
	AltText string
	// Caption is optional visible editorial copy.
	Caption string
}

// interiorProjectCoverAsset contains one complete cover response record. It is
// kept outside HTML view models so ordinary list and detail queries never load
// binary image content.
type interiorProjectCoverAsset struct {
	// InteriorProjectID is the positive database identity of the owning project.
	InteriorProjectID int64
	// Version is the exact revision present in public and protected media paths.
	Version int64
	// ContentType is the decoder-derived JPEG or PNG response type.
	ContentType string
	// Content contains the complete bounded normalized encoded image.
	Content []byte
	// ByteSize duplicates len(Content) as a database integrity assertion.
	ByteSize int
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 verifies the exact stored bytes and supplies a strong ETag.
	SHA256 [sha256.Size]byte
	// AltText is retained for repository validation, not binary responses.
	AltText string
	// Caption is optional reviewed cover copy.
	Caption string
	// CreatedAt records when the first cover revision was inserted.
	CreatedAt time.Time
	// UpdatedAt records the current replacement time and cannot predate creation.
	UpdatedAt time.Time
}

// catalogueInteriorProject is the derived projection needed by both public
// Interior HTML handlers. Internal ordering and lifecycle fields do not cross
// this boundary: SQL converts them to eligibility and a consecutive number.
type catalogueInteriorProject struct {
	// ID is PostgreSQL's positive internal identity used for validation only.
	ID int64
	// PortfolioNumber is the one-based position among published projects ordered
	// by sort_order and then ID.
	PortfolioNumber int64
	// Slug is the canonical path segment accepted after /interior-design/.
	Slug string
	// Title is the required public project heading.
	Title string
	// Typology is the required broad Interior project category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// ProjectYear is a reviewed four-digit year, or zero when SQL stored NULL.
	ProjectYear int
	// ProjectStatus is required real-world project-state copy and is distinct
	// from the private draft/published/archived lifecycle value.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// Cover contains binary-free reviewed metadata, or nil when no cover exists.
	Cover *interiorProjectCoverMetadata
}

// interiorProjectCatalogueReader is the narrow public Interior read boundary.
// It exposes no draft lookup, internal-ID lookup, or mutation authority.
type interiorProjectCatalogueReader interface {
	// ListPublished returns every currently published project in portfolio order.
	ListPublished(context.Context) ([]catalogueInteriorProject, error)
	// FindPublishedBySlug returns one published record at its list-consistent
	// portfolio position, or a safe not-found category.
	FindPublishedBySlug(
		context.Context,
		string,
	) (catalogueInteriorProject, error)
	// FindPublishedCover returns one exact current revision only while its owner
	// remains published.
	FindPublishedCover(
		context.Context,
		string,
		int64,
	) (interiorProjectCoverAsset, error)
}

// listPublishedInteriorProjectsSQL numbers rows only after excluding Draft and
// Archived projects. The outer ORDER BY makes iterator order part of the
// repository contract instead of relying on window-function processing order.
const listPublishedInteriorProjectsSQL = `SELECT
    id,
    portfolio_number,
    slug,
    title,
    typology,
    location,
    project_year,
    project_status,
    description,
    cover_version,
    cover_width,
    cover_height,
    cover_alt_text,
    cover_caption
FROM (
    SELECT
        projects.id,
        ROW_NUMBER() OVER (ORDER BY projects.sort_order ASC, projects.id ASC)
            AS portfolio_number,
        projects.slug,
        projects.title,
        projects.typology,
        projects.location,
        projects.project_year,
        projects.project_status,
        projects.description,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text,
        cover.caption AS cover_caption
    FROM public.interior_projects AS projects
    LEFT JOIN public.interior_project_cover_images AS cover
        ON cover.interior_project_id = projects.id
    WHERE projects.publication_status = $1
) AS published_interior_projects
ORDER BY portfolio_number ASC`

// findPublishedInteriorProjectBySlugSQL applies the slug predicate outside the
// complete published window. Filtering inside would renumber every matching
// detail record to one and make it disagree with the portfolio listing.
const findPublishedInteriorProjectBySlugSQL = `SELECT
    id,
    portfolio_number,
    slug,
    title,
    typology,
    location,
    project_year,
    project_status,
    description,
    cover_version,
    cover_width,
    cover_height,
    cover_alt_text,
    cover_caption
FROM (
    SELECT
        projects.id,
        ROW_NUMBER() OVER (ORDER BY projects.sort_order ASC, projects.id ASC)
            AS portfolio_number,
        projects.slug,
        projects.title,
        projects.typology,
        projects.location,
        projects.project_year,
        projects.project_status,
        projects.description,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text,
        cover.caption AS cover_caption
    FROM public.interior_projects AS projects
    LEFT JOIN public.interior_project_cover_images AS cover
        ON cover.interior_project_id = projects.id
    WHERE projects.publication_status = $1
) AS published_interior_projects
WHERE slug = $2`

// findPublishedInteriorProjectCoverSQL loads binary content only for an exact
// canonical slug, published owner, and current revision encoded in the URL.
const findPublishedInteriorProjectCoverSQL = `SELECT
    cover.interior_project_id,
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
FROM public.interior_project_cover_images AS cover
INNER JOIN public.interior_projects AS projects
    ON projects.id = cover.interior_project_id
WHERE projects.slug = $1
  AND projects.publication_status = $2
  AND cover.version = $3`

// interiorProjectCatalogueRows is the minimal database/sql iterator surface
// required by the ordered public list.
type interiorProjectCatalogueRows interface {
	// Next advances to the next result when one is available.
	Next() bool
	// Scan copies the current fixed projection into supplied destinations.
	Scan(...any) error
	// Err reports an iteration failure discovered after the last Scan.
	Err() error
	// Close releases the borrowed database connection.
	Close() error
}

// interiorProjectCatalogueScanner is shared by list and detail projections.
type interiorProjectCatalogueScanner interface {
	// Scan copies one fixed public project projection.
	Scan(...any) error
}

// interiorProjectCoverRowScanner is the binary cover projection seam.
type interiorProjectCoverRowScanner interface {
	// Scan copies one fixed twelve-column media projection.
	Scan(...any) error
}

// interiorProjectCatalogueQuery adapts QueryContext for deterministic tests.
type interiorProjectCatalogueQuery func(
	context.Context,
	string,
	...any,
) (interiorProjectCatalogueRows, error)

// interiorProjectCatalogueRowScanner is the single-row project-or-cover seam.
type interiorProjectCatalogueRowScanner interface {
	// Scan copies one fixed project or cover projection, or returns an error.
	Scan(...any) error
}

// interiorProjectCatalogueQueryRow adapts QueryRowContext without exposing the
// complete connection pool to repository unit tests.
type interiorProjectCatalogueQueryRow func(
	context.Context,
	string,
	...any,
) interiorProjectCatalogueRowScanner

// postgresInteriorProjectCatalogueReader borrows the process-owned pool for
// concurrent public reads. The composition root remains responsible for close.
type postgresInteriorProjectCatalogueReader struct {
	// query executes the ordered published-list statement.
	query interiorProjectCatalogueQuery
	// queryRow executes detail and exact-cover statements.
	queryRow interiorProjectCatalogueQueryRow
}

// Compile-time verification catches signature drift from the handler contract.
var _ interiorProjectCatalogueReader = (*postgresInteriorProjectCatalogueReader)(nil)

// newPostgresInteriorProjectCatalogueReader adapts a shared pool without
// opening a connection or issuing SQL during construction.
func newPostgresInteriorProjectCatalogueReader(
	database *sql.DB,
) (*postgresInteriorProjectCatalogueReader, error) {
	if database == nil {
		return nil, errInteriorProjectCatalogueReaderDatabaseRequired
	}

	return &postgresInteriorProjectCatalogueReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (interiorProjectCatalogueRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) interiorProjectCatalogueRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ListPublished reads and validates every public project while preserving
// deterministic SQL order and closing every acquired row set on all paths.
func (reader *postgresInteriorProjectCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueInteriorProject, error) {
	if ctx == nil {
		return nil, errInteriorProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errInteriorProjectCatalogueReadFailed
	}

	rows, err := reader.query(
		ctx,
		listPublishedInteriorProjectsSQL,
		publishedInteriorProjectStatus,
	)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}

		return nil, errInteriorProjectCatalogueReadFailed
	}
	if rows == nil {
		return nil, errInteriorProjectCatalogueReadFailed
	}

	projects := make([]catalogueInteriorProject, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	for rows.Next() {
		project, scanErr := scanCatalogueInteriorProject(rows)
		if scanErr != nil {
			_ = rows.Close()

			return nil, errInteriorProjectCatalogueReadFailed
		}

		expectedNumber := int64(len(projects) + 1)
		if !isValidCatalogueInteriorProject(project) ||
			project.PortfolioNumber != expectedNumber {
			_ = rows.Close()

			return nil, errInteriorProjectCatalogueReadFailed
		}
		if _, exists := seenIDs[project.ID]; exists {
			_ = rows.Close()

			return nil, errInteriorProjectCatalogueReadFailed
		}
		if _, exists := seenSlugs[project.Slug]; exists {
			_ = rows.Close()

			return nil, errInteriorProjectCatalogueReadFailed
		}

		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
		projects = append(projects, project)
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errInteriorProjectCatalogueReadFailed
	}

	return projects[:len(projects):len(projects)], nil
}

// FindPublishedBySlug reads one canonical published project while preserving
// its number in the complete public list. Unknown and non-public rows share the
// same not-found category.
func (reader *postgresInteriorProjectCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueInteriorProject, error) {
	if ctx == nil || !isCanonicalInteriorProjectSlug(slug) {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedInteriorProjectBySlugSQL,
		publishedInteriorProjectStatus,
		slug,
	)
	if row == nil {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}

	project, err := scanCatalogueInteriorProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueNotFound
	}
	if err != nil {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}
	if !isValidCatalogueInteriorProject(project) || project.Slug != slug {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}

	return project, nil
}

// FindPublishedCover reads one complete image only while its owner is public
// and the requested revision exactly matches the current stored revision.
func (reader *postgresInteriorProjectCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (interiorProjectCoverAsset, error) {
	if ctx == nil || !isCanonicalInteriorProjectSlug(slug) || version <= 0 {
		return interiorProjectCoverAsset{}, errInteriorProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedInteriorProjectCoverSQL,
		slug,
		publishedInteriorProjectStatus,
		version,
	)
	if row == nil {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	asset, err := scanInteriorProjectCoverAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverNotFound
	}
	if err != nil || asset.Version != version {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	return asset, nil
}

// scanCatalogueInteriorProject turns nullable year and LEFT JOIN cover columns
// into explicit Go option representations. Any partially-null cover projection
// fails closed rather than masquerading as a missing or valid image.
func scanCatalogueInteriorProject(
	scanner interiorProjectCatalogueScanner,
) (catalogueInteriorProject, error) {
	if scanner == nil {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}

	var project catalogueInteriorProject
	var projectYear sql.NullInt64
	var coverVersion sql.NullInt64
	var coverWidth sql.NullInt64
	var coverHeight sql.NullInt64
	var coverAltText sql.NullString
	var coverCaption sql.NullString
	err := scanner.Scan(
		&project.ID,
		&project.PortfolioNumber,
		&project.Slug,
		&project.Title,
		&project.Typology,
		&project.Location,
		&projectYear,
		&project.ProjectStatus,
		&project.Description,
		&coverVersion,
		&coverWidth,
		&coverHeight,
		&coverAltText,
		&coverCaption,
	)
	if err != nil {
		return catalogueInteriorProject{}, err
	}

	if projectYear.Valid {
		if projectYear.Int64 > int64(math.MaxInt) ||
			projectYear.Int64 < int64(math.MinInt) {
			return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
		}
		project.ProjectYear = int(projectYear.Int64)
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(math.MaxInt) ||
			coverWidth.Int64 < int64(math.MinInt) ||
			coverHeight.Int64 > int64(math.MaxInt) ||
			coverHeight.Int64 < int64(math.MinInt) {
			return catalogueInteriorProject{}, errInteriorProjectCatalogueReadFailed
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

// scanInteriorProjectCoverAsset reads one binary projection, converts its
// digest to a fixed-size value, validates every stored invariant, and returns
// isolated mutable byte storage.
func scanInteriorProjectCoverAsset(
	scanner interiorProjectCoverRowScanner,
) (interiorProjectCoverAsset, error) {
	if scanner == nil {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	var asset interiorProjectCoverAsset
	var digest []byte
	err := scanner.Scan(
		&asset.InteriorProjectID,
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
		return interiorProjectCoverAsset{}, err
	}
	if len(digest) != sha256.Size {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}
	copy(asset.SHA256[:], digest)
	if !isValidInteriorProjectCoverAsset(asset) {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverReadFailed
	}

	return cloneInteriorProjectCoverAsset(asset), nil
}

// isCanonicalInteriorProjectSlug mirrors the migration's lowercase ASCII slug
// grammar. Byte and character lengths are equal for this restricted alphabet.
func isCanonicalInteriorProjectSlug(slug string) bool {
	return slug != "" && len(slug) <= interiorProjectSlugMaximumLength &&
		interiorProjectSlugPattern.MatchString(slug)
}

// isValidInteriorProjectCatalogueText accepts one required reviewed string by
// adding nonempty semantics to the shared optional editorial validator.
func isValidInteriorProjectCatalogueText(value string, maximumLength int) bool {
	return value != "" && isValidOptionalEditorialText(value, maximumLength)
}

// isValidInteriorProjectYear accepts SQL NULL's zero representation or a
// migration-supported four-digit year.
func isValidInteriorProjectYear(year int) bool {
	return year == 0 ||
		(year >= interiorProjectYearMinimum && year <= interiorProjectYearMaximum)
}

// isValidInteriorProjectCoverMetadata applies the shared reviewed-image
// dimensions and text contract to Interior-specific metadata.
func isValidInteriorProjectCoverMetadata(
	metadata interiorProjectCoverMetadata,
) bool {
	return isValidReviewedCoverMetadata(
		metadata.Version,
		metadata.Width,
		metadata.Height,
		metadata.AltText,
		metadata.Caption,
	)
}

// isValidInteriorProjectCoverAsset validates one Interior-owned record through
// the generic reviewed-image security and integrity boundary.
func isValidInteriorProjectCoverAsset(asset interiorProjectCoverAsset) bool {
	return isValidReviewedCoverAsset(
		asset.InteriorProjectID,
		asset.Version,
		asset.ContentType,
		asset.Content,
		asset.ByteSize,
		asset.Width,
		asset.Height,
		asset.SHA256,
		asset.AltText,
		asset.Caption,
		asset.CreatedAt,
		asset.UpdatedAt,
	)
}

// isValidCatalogueInteriorProject validates every stored or SQL-derived value
// before it can influence HTML, a canonical link, or an image URL.
func isValidCatalogueInteriorProject(project catalogueInteriorProject) bool {
	return project.ID > 0 &&
		project.PortfolioNumber > 0 &&
		isCanonicalInteriorProjectSlug(project.Slug) &&
		isValidInteriorProjectCatalogueText(
			project.Title,
			interiorProjectTitleMaximumLength,
		) &&
		isValidInteriorProjectCatalogueText(
			project.Typology,
			interiorProjectTypologyMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Location,
			interiorProjectLocationMaximumLength,
		) &&
		isValidInteriorProjectYear(project.ProjectYear) &&
		isValidInteriorProjectCatalogueText(
			project.ProjectStatus,
			interiorProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Description,
			interiorProjectDescriptionMaximumLength,
		) &&
		(project.Cover == nil ||
			isValidInteriorProjectCoverMetadata(*project.Cover))
}

// cloneInteriorProjectCoverAsset isolates the mutable content slice whenever a
// repository or test double returns an asset across an interface boundary.
func cloneInteriorProjectCoverAsset(
	asset interiorProjectCoverAsset,
) interiorProjectCoverAsset {
	asset.Content = append([]byte(nil), asset.Content...)

	return asset
}
