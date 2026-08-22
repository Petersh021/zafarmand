package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Protected site-content read errors are deliberately small, stable categories.
// They never retain SQL, driver diagnostics, connection details, or managed copy.
var (
	// errAdminSiteContentReaderDatabaseRequired rejects construction without the
	// process-owned PostgreSQL pool.
	errAdminSiteContentReaderDatabaseRequired = errors.New(
		"create admin site content reader: database is required",
	)
	// errAdminSiteContentInvalidQuery rejects nil contexts and invalid exact hero
	// revisions before PostgreSQL is contacted.
	errAdminSiteContentInvalidQuery = errors.New(
		"admin site content query is invalid",
	)
	// errAdminSiteContentReadFailed collapses missing singleton rows, operational
	// errors, and malformed stored projections into one response-safe category.
	errAdminSiteContentReadFailed = errors.New(
		"admin site content database operation failed",
	)
	// errAdminHomepageHeroNotFound identifies an absent or stale protected hero
	// revision without disclosing any binary metadata.
	errAdminHomepageHeroNotFound = errors.New(
		"admin homepage hero not found",
	)
)

// adminHomepageFeatureSelection is one currently stored homepage foreign-key
// selection. Unlike the candidate list, it remains present when the referenced
// record later becomes private or loses its cover so an editor can clear it.
type adminHomepageFeatureSelection struct {
	// Discipline is one fixed application-owned homepage slot.
	Discipline homepageFeatureDiscipline
	// ID is the positive internal identity held by the singleton foreign key.
	ID int64
	// Slug is shown as route context but never becomes an admin path segment.
	Slug string
	// Title is the Product name or project title displayed in the form.
	Title string
	// Classification is the Product category or project typology.
	Classification string
	// PublicationStatus records why a selection may no longer be eligible.
	PublicationStatus string
	// CoverVersion is zero when the selected record has no current cover.
	CoverVersion int64
	// Eligible is derived from Published state plus a current reviewed cover.
	Eligible bool
}

// adminHomepageFeatureCandidate is one record that may be selected now. The
// reader returns only Published records with a current reviewed cover.
type adminHomepageFeatureCandidate struct {
	// Discipline assigns the candidate to exactly one fixed selector.
	Discipline homepageFeatureDiscipline
	// ID is the positive value posted by the corresponding native select.
	ID int64
	// Slug gives administrators useful public-route context.
	Slug string
	// Title is the Product name or project title.
	Title string
	// Classification is the Product category or project typology.
	Classification string
	// SortOrder and ID define deterministic editorial ordering within a slot.
	SortOrder int
	// CoverVersion proves the eligibility query found current cover metadata.
	CoverVersion int64
}

// adminHomepageContentRecord is the complete protected singleton projection.
// It includes revision and availability facts that are intentionally absent from
// the public homepage reader.
type adminHomepageContentRecord struct {
	// ID must equal the fixed singleton identity.
	ID int64
	// StudioName and Descriptor are the visible homepage identity lines.
	StudioName string
	Descriptor string
	// ManagedHeroEnabled chooses the database hero instead of the static fallback.
	ManagedHeroEnabled bool
	// Featured selections are nil when the corresponding nullable key is clear.
	FeaturedInterior     *adminHomepageFeatureSelection
	FeaturedArchitecture *adminHomepageFeatureSelection
	FeaturedProduct      *adminHomepageFeatureSelection
	// SEOTitle and SEODescription are complete managed document metadata.
	SEOTitle       string
	SEODescription string
	// Hero contains current binary-free metadata even while fallback is selected.
	Hero *homepageHeroMetadata
	// Version is the optimistic revision submitted by Homepage and hero forms.
	Version int64
	// CreatedAt and UpdatedAt are database-owned audit timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// adminContactContentRecord is the complete protected Contact singleton.
type adminContactContentRecord struct {
	// ID must equal the fixed singleton identity.
	ID int64
	// Eyebrow, Heading, and Introduction form the public Contact introduction.
	Eyebrow      string
	Heading      string
	Introduction string
	// ContactEmail is an optional normalized public mailbox.
	ContactEmail string
	// PhoneDisplay and PhoneE164 are both absent or both present.
	PhoneDisplay string
	PhoneE164    string
	// Address is optional reviewed multiline public copy.
	Address string
	// SEOTitle and SEODescription are complete managed document metadata.
	SEOTitle       string
	SEODescription string
	// Version is the optimistic revision submitted by the Contact form.
	Version int64
	// CreatedAt and UpdatedAt are database-owned audit timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// adminSiteContentReader is the protected read authority for both singleton
// settings records, eligible feature choices, and exact private hero previews.
type adminSiteContentReader interface {
	// ReadHomepage returns managed fields, all stored selections, and current hero
	// metadata without loading image bytes.
	ReadHomepage(context.Context) (adminHomepageContentRecord, error)
	// ReadContact returns the complete managed Contact singleton.
	ReadContact(context.Context) (adminContactContentRecord, error)
	// ListFeatureCandidates returns only currently eligible records in fixed slot
	// and editorial order.
	ListFeatureCandidates(context.Context) ([]adminHomepageFeatureCandidate, error)
	// FindHomepageHero returns one exact protected current hero revision whether
	// or not the public homepage currently enables it.
	FindHomepageHero(context.Context, int64) (homepageHeroAsset, error)
}

// readAdminHomepageContentSQL selects the singleton, all three referenced rows,
// nullable current covers, and optional hero metadata without loading image bytes.
const readAdminHomepageContentSQL = `SELECT
    homepage.id,
    homepage.studio_name,
    homepage.descriptor,
    homepage.managed_hero_enabled,
    homepage.featured_interior_project_id,
    interior.slug,
    interior.title,
    interior.typology,
    interior.publication_status,
    interior_cover.version,
    homepage.featured_architecture_project_id,
    architecture.slug,
    architecture.title,
    architecture.typology,
    architecture.publication_status,
    architecture_cover.version,
    homepage.featured_product_id,
    product.slug,
    product.name,
    product.category,
    product.publication_status,
    product_cover.version,
    homepage.seo_title,
    homepage.seo_description,
    hero.version,
    hero.width,
    hero.height,
    hero.alt_text,
    homepage.version,
    homepage.created_at,
    homepage.updated_at
FROM public.homepage_content AS homepage
LEFT JOIN public.interior_projects AS interior
    ON interior.id = homepage.featured_interior_project_id
LEFT JOIN public.interior_project_cover_images AS interior_cover
    ON interior_cover.interior_project_id = interior.id
LEFT JOIN public.architecture_projects AS architecture
    ON architecture.id = homepage.featured_architecture_project_id
LEFT JOIN public.architecture_project_cover_images AS architecture_cover
    ON architecture_cover.architecture_project_id = architecture.id
LEFT JOIN public.products AS product
    ON product.id = homepage.featured_product_id
LEFT JOIN public.product_cover_images AS product_cover
    ON product_cover.product_id = product.id
LEFT JOIN public.homepage_hero_images AS hero
    ON hero.homepage_content_id = homepage.id
WHERE homepage.id = 1`

// readAdminContactContentSQL loads one complete protected Contact singleton.
const readAdminContactContentSQL = `SELECT
    id,
    eyebrow,
    heading,
    introduction,
    contact_email,
    phone_display,
    phone_e164,
    address,
    seo_title,
    seo_description,
    version,
    created_at,
    updated_at
FROM public.contact_content
WHERE id = 1`

// listAdminHomepageFeatureCandidatesSQL uses fixed numeric slot labels in a
// UNION so callers receive one deterministic, type-safe selector data set.
const listAdminHomepageFeatureCandidatesSQL = `SELECT
    candidate.discipline,
    candidate.id,
    candidate.slug,
    candidate.title,
    candidate.classification,
    candidate.sort_order,
    candidate.cover_version
FROM (
    SELECT
        1 AS discipline,
        project.id,
        project.slug,
        project.title,
        project.typology AS classification,
        project.sort_order,
        cover.version AS cover_version
    FROM public.interior_projects AS project
    INNER JOIN public.interior_project_cover_images AS cover
        ON cover.interior_project_id = project.id
    WHERE project.publication_status = 'published'

    UNION ALL

    SELECT
        2 AS discipline,
        project.id,
        project.slug,
        project.title,
        project.typology AS classification,
        project.sort_order,
        cover.version AS cover_version
    FROM public.architecture_projects AS project
    INNER JOIN public.architecture_project_cover_images AS cover
        ON cover.architecture_project_id = project.id
    WHERE project.publication_status = 'published'

    UNION ALL

    SELECT
        3 AS discipline,
        product.id,
        product.slug,
        product.name AS title,
        product.category AS classification,
        product.sort_order,
        cover.version AS cover_version
    FROM public.products AS product
    INNER JOIN public.product_cover_images AS cover
        ON cover.product_id = product.id
    WHERE product.publication_status = 'published'
) AS candidate
ORDER BY candidate.discipline ASC, candidate.sort_order ASC, candidate.id ASC`

// findAdminHomepageHeroSQL reads one exact private current hero revision without
// requiring managed publication to be enabled.
const findAdminHomepageHeroSQL = `SELECT
    version,
    content_type,
    content,
    byte_size,
    width,
    height,
    sha256,
    alt_text,
    created_at,
    updated_at
FROM public.homepage_hero_images
WHERE homepage_content_id = 1 AND version = $1`

// adminSiteContentRows is the smallest iterator required by candidate reads.
type adminSiteContentRows interface {
	// Next advances to the next fixed candidate projection.
	Next() bool
	// Scan copies the current projection into supplied destinations.
	Scan(...any) error
	// Err reports a failure discovered after iteration.
	Err() error
	// Close returns the borrowed connection to its pool.
	Close() error
}

// adminSiteContentRowScanner is the single-row behavior shared by singleton and
// exact-media reads.
type adminSiteContentRowScanner interface {
	// Scan copies one fixed query result into supplied destinations.
	Scan(...any) error
}

// adminSiteContentQuery adapts database/sql's list operation to a narrow unit-
// test seam.
type adminSiteContentQuery func(
	context.Context,
	string,
	...any,
) (adminSiteContentRows, error)

// adminSiteContentQueryRow adapts database/sql's single-row operation without
// exposing the full connection pool to reader tests.
type adminSiteContentQueryRow func(
	context.Context,
	string,
	...any,
) adminSiteContentRowScanner

// postgresAdminSiteContentReader borrows the process-owned pool for concurrent
// protected reads. The process that opens the pool remains responsible for it.
type postgresAdminSiteContentReader struct {
	// query executes the ordered candidate statement.
	query adminSiteContentQuery
	// queryRow executes singleton and exact-hero statements.
	queryRow adminSiteContentQueryRow
}

// Compile-time verification keeps the PostgreSQL adapter aligned with the
// protected reader contract.
var _ adminSiteContentReader = (*postgresAdminSiteContentReader)(nil)

// newPostgresAdminSiteContentReader adapts a shared pool without opening a new
// connection or issuing SQL during construction.
func newPostgresAdminSiteContentReader(
	database *sql.DB,
) (*postgresAdminSiteContentReader, error) {
	if database == nil {
		return nil, errAdminSiteContentReaderDatabaseRequired
	}

	return &postgresAdminSiteContentReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (adminSiteContentRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminSiteContentRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ReadHomepage loads and validates the one protected Homepage document before
// managed copy or identifiers can influence an administrator template.
func (reader *postgresAdminSiteContentReader) ReadHomepage(
	ctx context.Context,
) (adminHomepageContentRecord, error) {
	if ctx == nil {
		return adminHomepageContentRecord{}, errAdminSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}

	row := reader.queryRow(ctx, readAdminHomepageContentSQL)
	if row == nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}
	record, err := scanAdminHomepageContent(row)
	if err != nil || !isValidStoredAdminHomepageContent(record) {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}

	return record, nil
}

// ReadContact loads and validates the one protected Contact document.
func (reader *postgresAdminSiteContentReader) ReadContact(
	ctx context.Context,
) (adminContactContentRecord, error) {
	if ctx == nil {
		return adminContactContentRecord{}, errAdminSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminContactContentRecord{}, errAdminSiteContentReadFailed
	}

	row := reader.queryRow(ctx, readAdminContactContentSQL)
	if row == nil {
		return adminContactContentRecord{}, errAdminSiteContentReadFailed
	}
	var record adminContactContentRecord
	err := row.Scan(
		&record.ID,
		&record.Eyebrow,
		&record.Heading,
		&record.Introduction,
		&record.ContactEmail,
		&record.PhoneDisplay,
		&record.PhoneE164,
		&record.Address,
		&record.SEOTitle,
		&record.SEODescription,
		&record.Version,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil || !isValidStoredAdminContactContent(record) {
		return adminContactContentRecord{}, errAdminSiteContentReadFailed
	}

	return record, nil
}

// ListFeatureCandidates reads only current eligible choices and independently
// verifies fixed slot order, identity uniqueness, and cover presence.
func (reader *postgresAdminSiteContentReader) ListFeatureCandidates(
	ctx context.Context,
) ([]adminHomepageFeatureCandidate, error) {
	if ctx == nil {
		return nil, errAdminSiteContentInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return nil, errAdminSiteContentReadFailed
	}

	rows, err := reader.query(ctx, listAdminHomepageFeatureCandidatesSQL)
	if err != nil {
		if rows != nil {
			_ = rows.Close()
		}
		return nil, errAdminSiteContentReadFailed
	}
	if rows == nil {
		return nil, errAdminSiteContentReadFailed
	}

	candidates := make([]adminHomepageFeatureCandidate, 0)
	seen := make(map[[2]int64]struct{})
	var previous adminHomepageFeatureCandidate
	for rows.Next() {
		var candidate adminHomepageFeatureCandidate
		var discipline int64
		err := rows.Scan(
			&discipline,
			&candidate.ID,
			&candidate.Slug,
			&candidate.Title,
			&candidate.Classification,
			&candidate.SortOrder,
			&candidate.CoverVersion,
		)
		candidate.Discipline = homepageFeatureDiscipline(discipline)
		if err != nil || !isValidAdminHomepageFeatureCandidate(candidate) ||
			(len(candidates) > 0 &&
				!adminHomepageFeatureCandidateFollows(candidate, previous)) {
			_ = rows.Close()
			return nil, errAdminSiteContentReadFailed
		}
		key := [2]int64{discipline, candidate.ID}
		if _, duplicate := seen[key]; duplicate {
			_ = rows.Close()
			return nil, errAdminSiteContentReadFailed
		}

		candidates = append(candidates, candidate)
		seen[key] = struct{}{}
		previous = candidate
	}
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		return nil, errAdminSiteContentReadFailed
	}

	return candidates[:len(candidates):len(candidates)], nil
}

// FindHomepageHero loads and validates one exact private hero revision.
func (reader *postgresAdminSiteContentReader) FindHomepageHero(
	ctx context.Context,
	version int64,
) (homepageHeroAsset, error) {
	if ctx == nil || version <= 0 {
		return homepageHeroAsset{}, errAdminSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return homepageHeroAsset{}, errAdminSiteContentReadFailed
	}

	row := reader.queryRow(ctx, findAdminHomepageHeroSQL, version)
	if row == nil {
		return homepageHeroAsset{}, errAdminSiteContentReadFailed
	}
	asset, err := scanAdminHomepageHeroAsset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return homepageHeroAsset{}, errAdminHomepageHeroNotFound
	}
	if err != nil || asset.Version != version || !isValidHomepageHeroAsset(asset) {
		return homepageHeroAsset{}, errAdminSiteContentReadFailed
	}

	return asset, nil
}

// scanAdminHomepageContent converts nullable joined groups into explicit
// selection and hero pointers while rejecting partial projections.
func scanAdminHomepageContent(
	scanner adminSiteContentRowScanner,
) (adminHomepageContentRecord, error) {
	if scanner == nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}

	var record adminHomepageContentRecord
	interior := adminHomepageFeatureNullableScan{}
	architecture := adminHomepageFeatureNullableScan{}
	product := adminHomepageFeatureNullableScan{}
	var heroVersion sql.NullInt64
	var heroWidth sql.NullInt64
	var heroHeight sql.NullInt64
	var heroAltText sql.NullString
	err := scanner.Scan(
		&record.ID,
		&record.StudioName,
		&record.Descriptor,
		&record.ManagedHeroEnabled,
		&interior.ID,
		&interior.Slug,
		&interior.Title,
		&interior.Classification,
		&interior.PublicationStatus,
		&interior.CoverVersion,
		&architecture.ID,
		&architecture.Slug,
		&architecture.Title,
		&architecture.Classification,
		&architecture.PublicationStatus,
		&architecture.CoverVersion,
		&product.ID,
		&product.Slug,
		&product.Title,
		&product.Classification,
		&product.PublicationStatus,
		&product.CoverVersion,
		&record.SEOTitle,
		&record.SEODescription,
		&heroVersion,
		&heroWidth,
		&heroHeight,
		&heroAltText,
		&record.Version,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return adminHomepageContentRecord{}, err
	}

	selection, err := interior.selection(homepageFeatureInterior)
	if err != nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}
	record.FeaturedInterior = selection
	selection, err = architecture.selection(homepageFeatureArchitecture)
	if err != nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}
	record.FeaturedArchitecture = selection
	selection, err = product.selection(homepageFeatureProduct)
	if err != nil {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}
	record.FeaturedProduct = selection

	hasHero := heroVersion.Valid || heroWidth.Valid || heroHeight.Valid ||
		heroAltText.Valid
	completeHero := heroVersion.Valid && heroWidth.Valid && heroHeight.Valid &&
		heroAltText.Valid
	if hasHero && !completeHero {
		return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
	}
	if completeHero {
		if heroWidth.Int64 > int64(math.MaxInt) ||
			heroHeight.Int64 > int64(math.MaxInt) {
			return adminHomepageContentRecord{}, errAdminSiteContentReadFailed
		}
		record.Hero = &homepageHeroMetadata{
			Version: heroVersion.Int64,
			Width:   int(heroWidth.Int64),
			Height:  int(heroHeight.Int64),
			AltText: heroAltText.String,
		}
	}

	return record, nil
}

// adminHomepageFeatureNullableScan groups the nullable columns belonging to one
// selected foreign-key join so partial groups cannot be accepted accidentally.
type adminHomepageFeatureNullableScan struct {
	// ID, text fields, publication state, and cover revision mirror one join.
	ID                sql.NullInt64
	Slug              sql.NullString
	Title             sql.NullString
	Classification    sql.NullString
	PublicationStatus sql.NullString
	CoverVersion      sql.NullInt64
}

// selection maps an entirely absent group to nil and requires every referenced
// row field when a foreign key is present. CoverVersion alone may remain NULL.
func (scan adminHomepageFeatureNullableScan) selection(
	discipline homepageFeatureDiscipline,
) (*adminHomepageFeatureSelection, error) {
	hasRecordValue := scan.ID.Valid || scan.Slug.Valid || scan.Title.Valid ||
		scan.Classification.Valid || scan.PublicationStatus.Valid
	completeRecord := scan.ID.Valid && scan.Slug.Valid && scan.Title.Valid &&
		scan.Classification.Valid && scan.PublicationStatus.Valid
	if !hasRecordValue && !scan.CoverVersion.Valid {
		return nil, nil
	}
	if !completeRecord {
		return nil, errAdminSiteContentReadFailed
	}

	selection := adminHomepageFeatureSelection{
		Discipline:        discipline,
		ID:                scan.ID.Int64,
		Slug:              scan.Slug.String,
		Title:             scan.Title.String,
		Classification:    scan.Classification.String,
		PublicationStatus: scan.PublicationStatus.String,
	}
	if scan.CoverVersion.Valid {
		selection.CoverVersion = scan.CoverVersion.Int64
	}
	selection.Eligible = selection.PublicationStatus == "published" &&
		selection.CoverVersion > 0

	return &selection, nil
}

// scanAdminHomepageHeroAsset copies one binary row and converts the variable-
// length database digest into the fixed-size shared asset representation.
func scanAdminHomepageHeroAsset(
	scanner adminSiteContentRowScanner,
) (homepageHeroAsset, error) {
	if scanner == nil {
		return homepageHeroAsset{}, errAdminSiteContentReadFailed
	}
	var asset homepageHeroAsset
	var digest []byte
	err := scanner.Scan(
		&asset.Version,
		&asset.ContentType,
		&asset.Content,
		&asset.ByteSize,
		&asset.Width,
		&asset.Height,
		&digest,
		&asset.AltText,
		&asset.CreatedAt,
		&asset.UpdatedAt,
	)
	if err != nil {
		return homepageHeroAsset{}, err
	}
	if len(digest) != sha256.Size {
		return homepageHeroAsset{}, errAdminSiteContentReadFailed
	}
	copy(asset.SHA256[:], digest)

	return asset, nil
}

// isValidStoredAdminHomepageContent rechecks every singleton, field, selection,
// hero, revision, and timestamp invariant at the protected repository boundary.
func isValidStoredAdminHomepageContent(record adminHomepageContentRecord) bool {
	return record.ID == siteContentSingletonID &&
		isValidSiteContentSingleLine(
			record.StudioName,
			homepageStudioNameMaximumLength,
		) && isValidSiteContentSingleLine(
		record.Descriptor,
		homepageDescriptorMaximumLength,
	) && isValidSiteContentSingleLine(
		record.SEOTitle,
		siteSEOTitleMaximumLength,
	) && isValidSiteContentSingleLine(
		record.SEODescription,
		siteSEODescriptionMaximumLength,
	) && (record.FeaturedInterior == nil ||
		isValidStoredAdminHomepageFeatureSelection(
			*record.FeaturedInterior,
			homepageFeatureInterior,
		)) && (record.FeaturedArchitecture == nil ||
		isValidStoredAdminHomepageFeatureSelection(
			*record.FeaturedArchitecture,
			homepageFeatureArchitecture,
		)) && (record.FeaturedProduct == nil ||
		isValidStoredAdminHomepageFeatureSelection(
			*record.FeaturedProduct,
			homepageFeatureProduct,
		)) && (record.Hero == nil || isValidHomepageHeroMetadata(*record.Hero)) &&
		(!record.ManagedHeroEnabled || record.Hero != nil) &&
		record.Version > 0 && !record.CreatedAt.IsZero() &&
		!record.UpdatedAt.IsZero() &&
		!record.UpdatedAt.Before(record.CreatedAt)
}

// isValidStoredAdminContactContent mirrors every migration-owned Contact field,
// concurrency, and timestamp invariant.
func isValidStoredAdminContactContent(record adminContactContentRecord) bool {
	return record.ID == siteContentSingletonID &&
		isValidSiteContentSingleLine(
			record.Eyebrow,
			contactEyebrowMaximumLength,
		) && isValidSiteContentSingleLine(
		record.Heading,
		contactHeadingMaximumLength,
	) && isValidSiteContentMultiline(
		record.Introduction,
		contactIntroductionMaximumLength,
		true,
	) && isValidPublicContactEmail(record.ContactEmail) &&
		isValidPublicContactPhone(record.PhoneDisplay, record.PhoneE164) &&
		isValidSiteContentMultiline(
			record.Address,
			contactAddressMaximumLength,
			false,
		) && isValidSiteContentSingleLine(
		record.SEOTitle,
		siteSEOTitleMaximumLength,
	) && isValidSiteContentSingleLine(
		record.SEODescription,
		siteSEODescriptionMaximumLength,
	) && record.Version > 0 && !record.CreatedAt.IsZero() &&
		!record.UpdatedAt.IsZero() &&
		!record.UpdatedAt.Before(record.CreatedAt)
}

// isValidStoredAdminHomepageFeatureSelection validates one stored reference
// without requiring it to remain eligible.
func isValidStoredAdminHomepageFeatureSelection(
	selection adminHomepageFeatureSelection,
	want homepageFeatureDiscipline,
) bool {
	if selection.Discipline != want || selection.ID <= 0 ||
		selection.CoverVersion < 0 ||
		(selection.Eligible != (selection.PublicationStatus == "published" &&
			selection.CoverVersion > 0)) ||
		!isValidManagedPublicationStatus(selection.PublicationStatus) {
		return false
	}

	return isValidAdminHomepageFeatureText(
		selection.Discipline,
		selection.Slug,
		selection.Title,
		selection.Classification,
	)
}

// isValidAdminHomepageFeatureCandidate validates one currently eligible choice.
func isValidAdminHomepageFeatureCandidate(
	candidate adminHomepageFeatureCandidate,
) bool {
	return candidate.ID > 0 && candidate.SortOrder > 0 &&
		candidate.SortOrder <= math.MaxInt32 && candidate.CoverVersion > 0 &&
		isValidAdminHomepageFeatureText(
			candidate.Discipline,
			candidate.Slug,
			candidate.Title,
			candidate.Classification,
		)
}

// isValidAdminHomepageFeatureText delegates discipline-specific slug and copy
// boundaries to the established public record validators.
func isValidAdminHomepageFeatureText(
	discipline homepageFeatureDiscipline,
	slug string,
	title string,
	classification string,
) bool {
	switch discipline {
	case homepageFeatureInterior:
		return isCanonicalInteriorProjectSlug(slug) &&
			isValidInteriorProjectCatalogueText(
				title,
				interiorProjectTitleMaximumLength,
			) && isValidInteriorProjectCatalogueText(
			classification,
			interiorProjectTypologyMaximumLength,
		)
	case homepageFeatureArchitecture:
		return isCanonicalArchitectureProjectSlug(slug) &&
			isValidArchitectureProjectCatalogueText(
				title,
				architectureProjectTitleMaximumLength,
			) && isValidArchitectureProjectCatalogueText(
			classification,
			architectureProjectTypologyMaximumLength,
		)
	case homepageFeatureProduct:
		return isCanonicalProductSlug(slug) &&
			isValidProductCatalogueText(
				title,
				productNameMaximumLength,
			) && isValidProductCatalogueText(
			classification,
			productCategoryMaximumLength,
		)
	default:
		return false
	}
}

// isValidManagedPublicationStatus accepts only the three migration-owned record
// lifecycle values shared across all current disciplines.
func isValidManagedPublicationStatus(status string) bool {
	return status == "draft" || status == "published" || status == "archived"
}

// adminHomepageFeatureCandidateFollows verifies strict fixed-slot then
// `(sort_order, id)` ordering for consecutive candidate rows.
func adminHomepageFeatureCandidateFollows(
	current adminHomepageFeatureCandidate,
	previous adminHomepageFeatureCandidate,
) bool {
	return current.Discipline > previous.Discipline ||
		(current.Discipline == previous.Discipline &&
			(current.SortOrder > previous.SortOrder ||
				(current.SortOrder == previous.SortOrder &&
					current.ID > previous.ID)))
}
