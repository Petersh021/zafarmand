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

// Public Architecture lifecycle values and field bounds mirror migration 8.
// Rechecking them in Go keeps malformed stored content behind the repository.
const (
	// draftArchitectureProjectStatus keeps managed work private while its facts
	// and media are being prepared or reviewed.
	draftArchitectureProjectStatus = "draft"
	// publishedArchitectureProjectStatus is the only lifecycle state eligible
	// for public Architecture list, detail, and cover reads.
	publishedArchitectureProjectStatus = "published"
	// archivedArchitectureProjectStatus retains managed work while removing it
	// from every public Architecture query.
	archivedArchitectureProjectStatus = "archived"
	// architectureProjectSlugMaximumLength bounds a public route segment.
	architectureProjectSlugMaximumLength = 120
	// architectureProjectTitleMaximumLength bounds the primary public heading.
	architectureProjectTitleMaximumLength = 160
	// architectureProjectTypologyMaximumLength bounds the category label.
	architectureProjectTypologyMaximumLength = 80
	// architectureProjectLocationMaximumLength bounds optional geographic copy.
	architectureProjectLocationMaximumLength = 160
	// architectureProjectStatusMaximumLength bounds real-world status copy.
	architectureProjectStatusMaximumLength = 80
	// architectureProjectDescriptionMaximumLength bounds optional long-form copy.
	architectureProjectDescriptionMaximumLength = 6000
	// architectureProjectYearMinimum is the first accepted supplied year.
	architectureProjectYearMinimum = 1000
	// architectureProjectYearMaximum is the last accepted supplied year.
	architectureProjectYearMaximum = 9999
)

// architectureProjectSlugPattern accepts lowercase ASCII alphanumeric
// components separated by exactly one hyphen.
var architectureProjectSlugPattern = regexp.MustCompile(
	`^[a-z0-9]+(?:-[a-z0-9]+)*$`,
)

// Public Architecture repository errors are stable, credential-free
// categories. Driver diagnostics are deliberately collapsed because they may
// contain SQL, connection details, or malformed stored content.
var (
	// errArchitectureProjectCatalogueReaderDatabaseRequired rejects construction
	// without the application-owned PostgreSQL pool.
	errArchitectureProjectCatalogueReaderDatabaseRequired = errors.New(
		"create architecture project catalogue reader: database is required",
	)
	// errArchitectureProjectCatalogueInvalidQuery rejects invalid coordinates
	// before PostgreSQL is contacted.
	errArchitectureProjectCatalogueInvalidQuery = errors.New(
		"architecture project catalogue query is invalid",
	)
	// errArchitectureProjectCatalogueNotFound hides missing and private projects
	// behind the same public response category.
	errArchitectureProjectCatalogueNotFound = errors.New(
		"published architecture project not found",
	)
	// errArchitectureProjectCatalogueReadFailed collapses operational and stored-
	// contract failures without retaining database diagnostics.
	errArchitectureProjectCatalogueReadFailed = errors.New(
		"architecture project catalogue database operation failed",
	)
	// errArchitectureProjectCoverNotFound hides missing, private, and stale cover
	// coordinates behind the same public category.
	errArchitectureProjectCoverNotFound = errors.New(
		"architecture project cover not found",
	)
	// errArchitectureProjectCoverReadFailed collapses binary-read and validation
	// failures without retaining media or database content.
	errArchitectureProjectCoverReadFailed = errors.New(
		"architecture project cover read failed",
	)
)

// architectureProjectCoverMetadata is the binary-free cover projection safe
// to join into public HTML queries. A nil pointer means no reviewed cover exists.
type architectureProjectCoverMetadata struct {
	// Version changes whenever the image or reviewed metadata changes.
	Version int64
	// Width describes decoded horizontal pixels, not browser-supplied metadata.
	Width int
	// Height describes decoded vertical pixels, not browser-supplied metadata.
	Height int
	// AltText is required meaningful alternative copy.
	AltText string
	// Caption is optional reviewed visible copy.
	Caption string
}

// architectureProjectCoverAsset contains one complete binary response record.
// Ordinary list and detail reads deliberately use only metadata instead.
type architectureProjectCoverAsset struct {
	// ArchitectureProjectID is the positive identity of the owning project.
	ArchitectureProjectID int64
	// Version is the exact current revision encoded in the public URL.
	Version int64
	// ContentType is the decoder-derived normalized JPEG or PNG type.
	ContentType string
	// Content contains one complete normalized image file.
	Content []byte
	// ByteSize must equal the exact normalized Content length.
	ByteSize int
	// Width is the decoded horizontal pixel count.
	Width int
	// Height is the decoded vertical pixel count.
	Height int
	// SHA256 identifies the exact bytes and supplies a strong response ETag.
	SHA256 [sha256.Size]byte
	// Reviewed text is retained for validation even though binary handlers do
	// not write it into the image response body.
	AltText string
	Caption string
	// Timestamps prove the stored replacement cannot predate initial creation.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// catalogueArchitectureProject is the public projection needed by list and
// detail handlers. Private order and lifecycle values never cross this seam.
type catalogueArchitectureProject struct {
	// ID is PostgreSQL's positive internal identity used for validation only.
	ID int64
	// PortfolioNumber is the one-based position within published projects.
	PortfolioNumber int64
	// Slug is the canonical segment accepted after /architecture-design/.
	Slug string
	// Title is the required reviewed public heading.
	Title string
	// Typology is the required reviewed Architecture category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// ProjectYear is zero only when the nullable database value is absent.
	ProjectYear int
	// ProjectStatus describes real-world work and is not publication lifecycle.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// Cover holds reviewed metadata without loading binary bytes.
	Cover *architectureProjectCoverMetadata
}

// architectureProjectCatalogueReader is the narrow public read boundary. It
// exposes no private-state lookup, internal-ID lookup, or mutation authority.
type architectureProjectCatalogueReader interface {
	// ListPublished returns every public project in deterministic editorial order.
	ListPublished(context.Context) ([]catalogueArchitectureProject, error)
	// FindPublishedBySlug returns one public project and its published-only number.
	FindPublishedBySlug(
		context.Context,
		string,
	) (catalogueArchitectureProject, error)
	// FindPublishedCover returns one exact current cover only while its project is
	// Published.
	FindPublishedCover(
		context.Context,
		string,
		int64,
	) (architectureProjectCoverAsset, error)
}

// listPublishedArchitectureProjectsSQL numbers only public rows. The outer
// ORDER BY makes iteration order an explicit part of the repository contract.
const listPublishedArchitectureProjectsSQL = `SELECT
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
    FROM public.architecture_projects AS projects
    LEFT JOIN public.architecture_project_cover_images AS cover
        ON cover.architecture_project_id = projects.id
    WHERE projects.publication_status = $1
) AS published_architecture_projects
ORDER BY portfolio_number ASC`

// findPublishedArchitectureProjectBySlugSQL applies the slug outside the full
// public window, preserving the same portfolio number shown on the listing.
const findPublishedArchitectureProjectBySlugSQL = `SELECT
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
    FROM public.architecture_projects AS projects
    LEFT JOIN public.architecture_project_cover_images AS cover
        ON cover.architecture_project_id = projects.id
    WHERE projects.publication_status = $1
) AS published_architecture_projects
WHERE slug = $2`

// findPublishedArchitectureProjectCoverSQL returns bytes only for an exact
// current cover revision whose owning project remains published.
const findPublishedArchitectureProjectCoverSQL = `SELECT
    cover.architecture_project_id,
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
FROM public.architecture_project_cover_images AS cover
INNER JOIN public.architecture_projects AS projects
    ON projects.id = cover.architecture_project_id
WHERE projects.slug = $1
  AND projects.publication_status = $2
  AND cover.version = $3`

// Small interfaces expose only database/sql behavior required by this reader,
// allowing deterministic unit tests without opening a connection.
type architectureProjectCatalogueRows interface {
	// Next advances to the next published projection.
	Next() bool
	// Scan copies the current fixed projection into destinations.
	Scan(...any) error
	// Err reports a failure discovered after iteration.
	Err() error
	// Close returns the borrowed connection to its pool.
	Close() error
}

// architectureProjectCatalogueScanner is shared by list and detail row scans.
type architectureProjectCatalogueScanner interface {
	// Scan copies one fixed published project projection into destinations.
	Scan(...any) error
}

// architectureProjectCoverRowScanner is the exact-cover single-row scan seam.
type architectureProjectCoverRowScanner interface {
	// Scan copies one fixed binary cover projection into destinations.
	Scan(...any) error
}

// architectureProjectCatalogueQuery adapts database/sql's ordered-list method
// to a narrow injectable unit-test seam.
type architectureProjectCatalogueQuery func(
	context.Context,
	string,
	...any,
) (architectureProjectCatalogueRows, error)

// architectureProjectCatalogueRowScanner is the single-row behavior needed by
// published detail queries.
type architectureProjectCatalogueRowScanner interface {
	// Scan copies one fixed published detail projection into destinations.
	Scan(...any) error
}

// architectureProjectCatalogueQueryRow adapts database/sql's single-row method
// without exposing the complete pool to repository tests.
type architectureProjectCatalogueQueryRow func(
	context.Context,
	string,
	...any,
) architectureProjectCatalogueRowScanner

// postgresArchitectureProjectCatalogueReader borrows the process-owned pool;
// the application composition root remains responsible for closing that pool.
type postgresArchitectureProjectCatalogueReader struct {
	// query executes the deterministic published list statement.
	query architectureProjectCatalogueQuery
	// queryRow executes published detail and exact-cover statements.
	queryRow architectureProjectCatalogueQueryRow
}

// Compile-time verification keeps the PostgreSQL adapter aligned with the
// narrow public reader contract.
var _ architectureProjectCatalogueReader = (*postgresArchitectureProjectCatalogueReader)(nil)

// newPostgresArchitectureProjectCatalogueReader adapts a shared pool without
// opening a connection or issuing SQL during construction.
func newPostgresArchitectureProjectCatalogueReader(
	database *sql.DB,
) (*postgresArchitectureProjectCatalogueReader, error) {
	if database == nil {
		return nil, errArchitectureProjectCatalogueReaderDatabaseRequired
	}

	return &postgresArchitectureProjectCatalogueReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (architectureProjectCatalogueRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) architectureProjectCatalogueRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ListPublished reads every public record, validates consecutive order and
// uniqueness, and releases the row set on every success or failure path.
func (reader *postgresArchitectureProjectCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueArchitectureProject, error) {
	if ctx == nil {
		return nil, errArchitectureProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errArchitectureProjectCatalogueReadFailed
	}

	rows, err := reader.query(
		ctx,
		listPublishedArchitectureProjectsSQL,
		publishedArchitectureProjectStatus,
	)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}
		return nil, errArchitectureProjectCatalogueReadFailed
	}
	if rows == nil {
		return nil, errArchitectureProjectCatalogueReadFailed
	}

	projects := make([]catalogueArchitectureProject, 0)
	seenIDs := make(map[int64]struct{})
	seenSlugs := make(map[string]struct{})
	for rows.Next() {
		project, scanErr := scanCatalogueArchitectureProject(rows)
		if scanErr != nil ||
			!isValidCatalogueArchitectureProject(project) ||
			project.PortfolioNumber != int64(len(projects)+1) {
			_ = rows.Close()
			return nil, errArchitectureProjectCatalogueReadFailed
		}
		if _, exists := seenIDs[project.ID]; exists {
			_ = rows.Close()
			return nil, errArchitectureProjectCatalogueReadFailed
		}
		if _, exists := seenSlugs[project.Slug]; exists {
			_ = rows.Close()
			return nil, errArchitectureProjectCatalogueReadFailed
		}

		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
		projects = append(projects, project)
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errArchitectureProjectCatalogueReadFailed
	}

	return projects[:len(projects):len(projects)], nil
}

// FindPublishedBySlug returns a canonical published record at its list-wide
// number. Unknown, Draft, and Archived records share the not-found category.
func (reader *postgresArchitectureProjectCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueArchitectureProject, error) {
	if ctx == nil || !isCanonicalArchitectureProjectSlug(slug) {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedArchitectureProjectBySlugSQL,
		publishedArchitectureProjectStatus,
		slug,
	)
	if row == nil {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
	}

	project, err := scanCatalogueArchitectureProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueNotFound
	}
	if err != nil || !isValidCatalogueArchitectureProject(project) ||
		project.Slug != slug {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
	}

	return project, nil
}

// FindPublishedCover returns one exact current revision only while its owner
// remains public. Stale revisions and hidden owners are indistinguishable.
func (reader *postgresArchitectureProjectCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (architectureProjectCoverAsset, error) {
	if ctx == nil || !isCanonicalArchitectureProjectSlug(slug) || version <= 0 {
		return architectureProjectCoverAsset{}, errArchitectureProjectCatalogueInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
	}

	row := reader.queryRow(
		ctx,
		findPublishedArchitectureProjectCoverSQL,
		slug,
		publishedArchitectureProjectStatus,
		version,
	)
	if row == nil {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
	}

	asset, err := scanArchitectureProjectCoverAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverNotFound
	}
	if err != nil || asset.Version != version {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
	}

	return asset, nil
}

// scanCatalogueArchitectureProject converts nullable year and LEFT JOIN cover
// columns into explicit Go option representations. Partially-null cover data is
// rejected instead of being interpreted as absent or valid.
func scanCatalogueArchitectureProject(
	scanner architectureProjectCatalogueScanner,
) (catalogueArchitectureProject, error) {
	if scanner == nil {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
	}

	var project catalogueArchitectureProject
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
		return catalogueArchitectureProject{}, err
	}

	if projectYear.Valid {
		if projectYear.Int64 > int64(math.MaxInt) ||
			projectYear.Int64 < int64(math.MinInt) {
			return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
		}
		project.ProjectYear = int(projectYear.Int64)
	}

	hasCover := coverVersion.Valid || coverWidth.Valid || coverHeight.Valid ||
		coverAltText.Valid || coverCaption.Valid
	completeCover := coverVersion.Valid && coverWidth.Valid && coverHeight.Valid &&
		coverAltText.Valid && coverCaption.Valid
	if hasCover && !completeCover {
		return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
	}
	if completeCover {
		if coverWidth.Int64 > int64(math.MaxInt) ||
			coverWidth.Int64 < int64(math.MinInt) ||
			coverHeight.Int64 > int64(math.MaxInt) ||
			coverHeight.Int64 < int64(math.MinInt) {
			return catalogueArchitectureProject{}, errArchitectureProjectCatalogueReadFailed
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

// scanArchitectureProjectCoverAsset converts the digest to a fixed-size value,
// validates every stored invariant, and isolates the mutable content bytes.
func scanArchitectureProjectCoverAsset(
	scanner architectureProjectCoverRowScanner,
) (architectureProjectCoverAsset, error) {
	if scanner == nil {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
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
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
	}
	copy(asset.SHA256[:], digest)
	if !isValidArchitectureProjectCoverAsset(asset) {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverReadFailed
	}

	return cloneArchitectureProjectCoverAsset(asset), nil
}

// isCanonicalArchitectureProjectSlug mirrors migration 8's lowercase ASCII
// grammar. Byte and character lengths are equal for this restricted alphabet.
func isCanonicalArchitectureProjectSlug(slug string) bool {
	return slug != "" && len(slug) <= architectureProjectSlugMaximumLength &&
		architectureProjectSlugPattern.MatchString(slug)
}

// isValidArchitectureProjectCatalogueText adds required nonempty semantics to
// the shared whitespace and rune-count validator.
func isValidArchitectureProjectCatalogueText(
	value string,
	maximumLength int,
) bool {
	return value != "" && isValidOptionalEditorialText(value, maximumLength)
}

// isValidArchitectureProjectYear accepts SQL NULL's zero representation or a
// migration-supported four-digit year.
func isValidArchitectureProjectYear(year int) bool {
	return year == 0 ||
		(year >= architectureProjectYearMinimum && year <= architectureProjectYearMaximum)
}

// isValidArchitectureProjectCoverMetadata applies the shared reviewed-image
// dimensions and text contract to Architecture-specific metadata.
func isValidArchitectureProjectCoverMetadata(
	metadata architectureProjectCoverMetadata,
) bool {
	return isValidReviewedCoverMetadata(
		metadata.Version,
		metadata.Width,
		metadata.Height,
		metadata.AltText,
		metadata.Caption,
	)
}

// isValidArchitectureProjectCoverAsset delegates image security and integrity
// checks to the generic reviewed-cover boundary.
func isValidArchitectureProjectCoverAsset(
	asset architectureProjectCoverAsset,
) bool {
	return isValidReviewedCoverAsset(
		asset.ArchitectureProjectID,
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

// isValidCatalogueArchitectureProject rechecks every stored or derived value
// before it can influence HTML, a canonical link, or an image URL.
func isValidCatalogueArchitectureProject(
	project catalogueArchitectureProject,
) bool {
	return project.ID > 0 &&
		project.PortfolioNumber > 0 &&
		isCanonicalArchitectureProjectSlug(project.Slug) &&
		isValidArchitectureProjectCatalogueText(
			project.Title,
			architectureProjectTitleMaximumLength,
		) &&
		isValidArchitectureProjectCatalogueText(
			project.Typology,
			architectureProjectTypologyMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Location,
			architectureProjectLocationMaximumLength,
		) &&
		isValidArchitectureProjectYear(project.ProjectYear) &&
		isValidArchitectureProjectCatalogueText(
			project.ProjectStatus,
			architectureProjectStatusMaximumLength,
		) &&
		isValidOptionalEditorialText(
			project.Description,
			architectureProjectDescriptionMaximumLength,
		) &&
		(project.Cover == nil ||
			isValidArchitectureProjectCoverMetadata(*project.Cover))
}

// isValidPublishedArchitectureProjectCatalogue verifies the complete result
// from any injected public reader before a handler maps it into HTML. The SQL
// reader checks the same invariants, while this boundary keeps substitutes and
// future implementations fail-closed.
func isValidPublishedArchitectureProjectCatalogue(
	projects []catalogueArchitectureProject,
) bool {
	seenIDs := make(map[int64]struct{}, len(projects))
	seenSlugs := make(map[string]struct{}, len(projects))

	for index, project := range projects {
		if !isValidCatalogueArchitectureProject(project) ||
			project.PortfolioNumber != int64(index+1) {
			return false
		}
		if _, exists := seenIDs[project.ID]; exists {
			return false
		}
		if _, exists := seenSlugs[project.Slug]; exists {
			return false
		}

		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
	}

	return true
}

// cloneArchitectureProjectCoverAsset isolates mutable bytes whenever an asset
// crosses a repository or test-double interface boundary.
func cloneArchitectureProjectCoverAsset(
	asset architectureProjectCoverAsset,
) architectureProjectCoverAsset {
	asset.Content = append([]byte(nil), asset.Content...)

	return asset
}
