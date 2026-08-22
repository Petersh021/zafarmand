package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

// Protected site-content write errors are stable, response-safe categories.
// Rejected copy, SQL, and driver diagnostics never cross this boundary.
var (
	// errAdminSiteContentWriterDatabaseRequired rejects construction without the
	// process-owned PostgreSQL pool.
	errAdminSiteContentWriterDatabaseRequired = errors.New(
		"create admin site content writer: database is required",
	)
	// errAdminSiteContentWriteInvalid rejects invalid coordinates or managed
	// values before PostgreSQL is contacted.
	errAdminSiteContentWriteInvalid = errors.New(
		"admin site content write is invalid",
	)
	// errAdminSiteContentWriteConflict identifies a valid form based on an
	// obsolete Homepage or Contact revision.
	errAdminSiteContentWriteConflict = errors.New(
		"admin site content version conflict",
	)
	// errAdminHomepageInteriorFeatureUnavailable reports a selected Interior
	// record that is no longer Published with a current cover.
	errAdminHomepageInteriorFeatureUnavailable = errors.New(
		"admin homepage interior feature is unavailable",
	)
	// errAdminHomepageArchitectureFeatureUnavailable reports the equivalent
	// invalid Architecture selection.
	errAdminHomepageArchitectureFeatureUnavailable = errors.New(
		"admin homepage architecture feature is unavailable",
	)
	// errAdminHomepageProductFeatureUnavailable reports the equivalent invalid
	// Product selection.
	errAdminHomepageProductFeatureUnavailable = errors.New(
		"admin homepage product feature is unavailable",
	)
	// errAdminHomepageHeroRequired prevents enabling managed mode before one
	// reviewed hero has been stored.
	errAdminHomepageHeroRequired = errors.New(
		"admin homepage managed hero is required",
	)
	// errAdminSiteContentWriteFailed collapses every other execution, scan, and
	// result-contract failure.
	errAdminSiteContentWriteFailed = errors.New(
		"admin site content database write failed",
	)
)

// adminHomepageContentWriteInput contains only administrator-owned Homepage
// values. Identity, revisions, timestamps, and hero bytes remain separate.
type adminHomepageContentWriteInput struct {
	// StudioName and Descriptor are the required visible identity lines.
	StudioName string
	Descriptor string
	// ManagedHeroEnabled chooses managed media rather than the static fallback.
	ManagedHeroEnabled bool
	// A zero feature ID clears that nullable selector; positive values request a
	// currently eligible record in its fixed discipline.
	FeaturedInteriorProjectID     int64
	FeaturedArchitectureProjectID int64
	FeaturedProductID             int64
	// SEOTitle and SEODescription are complete managed document metadata.
	SEOTitle       string
	SEODescription string
}

// adminContactContentWriteInput contains exactly the managed Contact fields.
type adminContactContentWriteInput struct {
	// Eyebrow, Heading, and Introduction form the visible page introduction.
	Eyebrow      string
	Heading      string
	Introduction string
	// ContactEmail is empty or one normalized public mailbox.
	ContactEmail string
	// PhoneDisplay and PhoneE164 are both absent or both present.
	PhoneDisplay string
	PhoneE164    string
	// Address is optional reviewed multiline copy.
	Address string
	// SEOTitle and SEODescription are complete managed document metadata.
	SEOTitle       string
	SEODescription string
}

// adminHomepageHeroWriteInput contains normalized image facts and reviewed alt
// text. Browser filenames and claimed MIME headers never enter persistence.
type adminHomepageHeroWriteInput struct {
	// ContentType is derived from the standard-library decoder.
	ContentType string
	// Content is one complete normalized JPEG or PNG.
	Content []byte
	// ByteSize exactly duplicates len(Content) as an integrity assertion.
	ByteSize int
	// Width and Height are decoder-derived pixel dimensions.
	Width  int
	Height int
	// SHA256 is the digest of the exact normalized bytes.
	SHA256 [32]byte
	// AltText is the required reviewed meaningful alternative.
	AltText string
}

// adminSiteContentWriteResult returns the database-owned optimistic revision
// after a successful Homepage or Contact update.
type adminSiteContentWriteResult struct {
	// Version is exactly one greater than the accepted expected revision.
	Version int64
}

// adminHomepageHeroWriteResult returns both revisions advanced by an atomic
// managed-hero upload or replacement.
type adminHomepageHeroWriteResult struct {
	// HomepageVersion is the new parent optimistic revision.
	HomepageVersion int64
	// HeroVersion is the inserted or incremented exact media revision.
	HeroVersion int64
}

// adminSiteContentWriter is the narrow mutation authority for both settings
// singletons and the current reviewed Homepage hero.
type adminSiteContentWriter interface {
	// UpdateHomepage applies managed text, source choice, SEO, and three fixed
	// eligible feature selections only while expectedVersion remains current.
	UpdateHomepage(
		context.Context,
		int64,
		adminHomepageContentWriteInput,
	) (adminSiteContentWriteResult, error)
	// UpdateContact applies one complete Contact edit optimistically.
	UpdateContact(
		context.Context,
		int64,
		adminContactContentWriteInput,
	) (adminSiteContentWriteResult, error)
	// UpsertHomepageHero atomically enables managed mode, advances the Homepage
	// revision, and inserts or replaces the normalized hero revision.
	UpsertHomepageHero(
		context.Context,
		int64,
		adminHomepageHeroWriteInput,
	) (adminHomepageHeroWriteResult, error)
}

// updateAdminHomepageContentSQL locks the Homepage singleton before checking
// related feature and hero rows, then performs the optimistic update in the
// same PostgreSQL statement transaction. The parent-first order matches hero
// replacement, so concurrent writes resolve through the Homepage revision
// instead of forming a parent/hero lock cycle. A stale form or unavailable
// selection gives the UPDATE no source row.
const updateAdminHomepageContentSQL = `WITH current_homepage AS MATERIALIZED (
    SELECT version
    FROM public.homepage_content
    WHERE id = 1
    FOR UPDATE
),
eligibility AS MATERIALIZED (
    SELECT
        $4::bigint IS NULL OR EXISTS (
            SELECT 1
            FROM public.interior_projects AS project
            INNER JOIN public.interior_project_cover_images AS cover
                ON cover.interior_project_id = project.id
            WHERE project.id = $4 AND project.publication_status = 'published'
            FOR SHARE OF project, cover
        ) AS interior_available,
        $5::bigint IS NULL OR EXISTS (
            SELECT 1
            FROM public.architecture_projects AS project
            INNER JOIN public.architecture_project_cover_images AS cover
                ON cover.architecture_project_id = project.id
            WHERE project.id = $5 AND project.publication_status = 'published'
            FOR SHARE OF project, cover
        ) AS architecture_available,
        $6::bigint IS NULL OR EXISTS (
            SELECT 1
            FROM public.products AS product
            INNER JOIN public.product_cover_images AS cover
                ON cover.product_id = product.id
            WHERE product.id = $6 AND product.publication_status = 'published'
            FOR SHARE OF product, cover
        ) AS product_available,
        EXISTS (
            SELECT 1
            FROM public.homepage_hero_images
            WHERE homepage_content_id = 1
            FOR SHARE
        ) AS hero_available
    FROM current_homepage
),
updated_homepage AS (
    UPDATE public.homepage_content
    SET
        studio_name = $2,
        descriptor = $3,
        managed_hero_enabled = $7,
        featured_interior_project_id = $4,
        featured_architecture_project_id = $5,
        featured_product_id = $6,
        seo_title = $8,
        seo_description = $9,
        version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    FROM eligibility
    WHERE id = 1
      AND version = $1
      AND eligibility.interior_available
      AND eligibility.architecture_available
      AND eligibility.product_available
      AND (NOT $7 OR eligibility.hero_available)
    RETURNING version
)
SELECT
    COALESCE((SELECT version FROM updated_homepage), 0),
    EXISTS(SELECT 1 FROM current_homepage),
    EXISTS(SELECT 1 FROM current_homepage WHERE version = $1),
    eligibility.interior_available,
    eligibility.architecture_available,
    eligibility.product_available,
    eligibility.hero_available
FROM eligibility`

// updateAdminContactContentSQL observes singleton existence and applies one
// optimistic Contact edit in a single statement.
const updateAdminContactContentSQL = `WITH current_contact AS MATERIALIZED (
    SELECT version
    FROM public.contact_content
    WHERE id = 1
),
updated_contact AS (
    UPDATE public.contact_content
    SET
        eyebrow = $2,
        heading = $3,
        introduction = $4,
        contact_email = $5,
        phone_display = $6,
        phone_e164 = $7,
        address = $8,
        seo_title = $9,
        seo_description = $10,
        version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = 1 AND version = $1
    RETURNING version
)
SELECT
    COALESCE((SELECT version FROM updated_contact), 0),
    EXISTS(SELECT 1 FROM current_contact)`

// upsertAdminHomepageHeroSQL advances the Homepage and its current managed hero
// atomically. A stale parent revision gives the media CTE no source row.
const upsertAdminHomepageHeroSQL = `WITH current_homepage AS MATERIALIZED (
    SELECT id
    FROM public.homepage_content
    WHERE id = 1
),
updated_homepage AS (
    UPDATE public.homepage_content
    SET
        managed_hero_enabled = true,
        version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = 1 AND version = $1
    RETURNING id, version
),
upserted_hero AS (
    INSERT INTO public.homepage_hero_images (
        homepage_content_id,
        version,
        content_type,
        content,
        byte_size,
        width,
        height,
        sha256,
        alt_text
    )
    SELECT
        id,
        1,
        $2,
        $3,
        $4,
        $5,
        $6,
        $7,
        $8
    FROM updated_homepage
    ON CONFLICT (homepage_content_id) DO UPDATE
    SET
        version = homepage_hero_images.version + 1,
        content_type = EXCLUDED.content_type,
        content = EXCLUDED.content,
        byte_size = EXCLUDED.byte_size,
        width = EXCLUDED.width,
        height = EXCLUDED.height,
        sha256 = EXCLUDED.sha256,
        alt_text = EXCLUDED.alt_text,
        updated_at = CURRENT_TIMESTAMP
    RETURNING version
)
SELECT
    COALESCE((SELECT version FROM updated_homepage), 0),
    COALESCE((SELECT version FROM upserted_hero), 0),
    EXISTS(SELECT 1 FROM current_homepage)`

// adminSiteContentWriteRowScanner is the fixed single-row behavior shared by
// all protected settings mutation results.
type adminSiteContentWriteRowScanner interface {
	// Scan copies one statement result into supplied destinations.
	Scan(...any) error
}

// adminSiteContentWriteQueryRow is the narrow database/sql operation used by
// the writer and supplies a dependency-free unit-test seam.
type adminSiteContentWriteQueryRow func(
	context.Context,
	string,
	...any,
) adminSiteContentWriteRowScanner

// postgresAdminSiteContentWriter borrows the process-owned pool for concurrent
// protected mutations. The application process closes the pool.
type postgresAdminSiteContentWriter struct {
	// queryRow executes one trusted parameterized atomic mutation statement.
	queryRow adminSiteContentWriteQueryRow
}

// Compile-time verification prevents drift from the protected write contract.
var _ adminSiteContentWriter = (*postgresAdminSiteContentWriter)(nil)

// newPostgresAdminSiteContentWriter adapts the shared pool without opening a
// connection or issuing SQL during construction.
func newPostgresAdminSiteContentWriter(
	database *sql.DB,
) (*postgresAdminSiteContentWriter, error) {
	if database == nil {
		return nil, errAdminSiteContentWriterDatabaseRequired
	}

	return &postgresAdminSiteContentWriter{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminSiteContentWriteRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// UpdateHomepage applies one complete edit only if the submitted revision,
// feature eligibility, and managed-hero dependency remain current together.
func (writer *postgresAdminSiteContentWriter) UpdateHomepage(
	ctx context.Context,
	expectedVersion int64,
	input adminHomepageContentWriteInput,
) (adminSiteContentWriteResult, error) {
	if ctx == nil || expectedVersion <= 0 || expectedVersion == math.MaxInt64 ||
		!isValidAdminHomepageContentWriteInput(input) {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	row := writer.queryRow(
		ctx,
		updateAdminHomepageContentSQL,
		expectedVersion,
		input.StudioName,
		input.Descriptor,
		nullableAdminHomepageFeatureID(input.FeaturedInteriorProjectID),
		nullableAdminHomepageFeatureID(input.FeaturedArchitectureProjectID),
		nullableAdminHomepageFeatureID(input.FeaturedProductID),
		input.ManagedHeroEnabled,
		input.SEOTitle,
		input.SEODescription,
	)
	if row == nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	var result adminSiteContentWriteResult
	var singletonExists bool
	var versionMatches bool
	var interiorAvailable bool
	var architectureAvailable bool
	var productAvailable bool
	var heroAvailable bool
	err := row.Scan(
		&result.Version,
		&singletonExists,
		&versionMatches,
		&interiorAvailable,
		&architectureAvailable,
		&productAvailable,
		&heroAvailable,
	)
	if err != nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}
	if result.Version == 0 {
		switch {
		case !singletonExists:
			return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
		case !versionMatches:
			return adminSiteContentWriteResult{}, errAdminSiteContentWriteConflict
		case !interiorAvailable:
			return adminSiteContentWriteResult{},
				errAdminHomepageInteriorFeatureUnavailable
		case !architectureAvailable:
			return adminSiteContentWriteResult{},
				errAdminHomepageArchitectureFeatureUnavailable
		case !productAvailable:
			return adminSiteContentWriteResult{},
				errAdminHomepageProductFeatureUnavailable
		case input.ManagedHeroEnabled && !heroAvailable:
			return adminSiteContentWriteResult{}, errAdminHomepageHeroRequired
		default:
			// A concurrent Homepage update can make PostgreSQL's rechecked UPDATE
			// affect zero rows even though the statement snapshot observed the old
			// version. With all dependency failures excluded, zero rows is therefore
			// the safe optimistic-conflict result rather than a false 503.
			return adminSiteContentWriteResult{}, errAdminSiteContentWriteConflict
		}
	}
	if !singletonExists || !versionMatches || !interiorAvailable ||
		!architectureAvailable || !productAvailable ||
		(input.ManagedHeroEnabled && !heroAvailable) ||
		result.Version != expectedVersion+1 {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	return result, nil
}

// UpdateContact applies one complete Contact edit only while expectedVersion
// remains current.
func (writer *postgresAdminSiteContentWriter) UpdateContact(
	ctx context.Context,
	expectedVersion int64,
	input adminContactContentWriteInput,
) (adminSiteContentWriteResult, error) {
	if ctx == nil || expectedVersion <= 0 || expectedVersion == math.MaxInt64 ||
		!isValidAdminContactContentWriteInput(input) {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	row := writer.queryRow(
		ctx,
		updateAdminContactContentSQL,
		expectedVersion,
		input.Eyebrow,
		input.Heading,
		input.Introduction,
		input.ContactEmail,
		input.PhoneDisplay,
		input.PhoneE164,
		input.Address,
		input.SEOTitle,
		input.SEODescription,
	)
	if row == nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	var result adminSiteContentWriteResult
	var singletonExists bool
	if err := row.Scan(&result.Version, &singletonExists); err != nil {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}
	if result.Version == 0 {
		if singletonExists {
			return adminSiteContentWriteResult{}, errAdminSiteContentWriteConflict
		}
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}
	if !singletonExists || result.Version != expectedVersion+1 {
		return adminSiteContentWriteResult{}, errAdminSiteContentWriteFailed
	}

	return result, nil
}

// UpsertHomepageHero inserts or replaces one normalized hero only while the
// parent Homepage revision remains current. Both revisions change atomically.
func (writer *postgresAdminSiteContentWriter) UpsertHomepageHero(
	ctx context.Context,
	expectedHomepageVersion int64,
	input adminHomepageHeroWriteInput,
) (adminHomepageHeroWriteResult, error) {
	if ctx == nil || expectedHomepageVersion <= 0 ||
		expectedHomepageVersion == math.MaxInt64 ||
		!isValidAdminHomepageHeroWriteInput(input) {
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteFailed
	}

	row := writer.queryRow(
		ctx,
		upsertAdminHomepageHeroSQL,
		expectedHomepageVersion,
		input.ContentType,
		input.Content,
		input.ByteSize,
		input.Width,
		input.Height,
		input.SHA256[:],
		input.AltText,
	)
	if row == nil {
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteFailed
	}

	var result adminHomepageHeroWriteResult
	var singletonExists bool
	if err := row.Scan(
		&result.HomepageVersion,
		&result.HeroVersion,
		&singletonExists,
	); err != nil {
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteFailed
	}
	if result == (adminHomepageHeroWriteResult{}) {
		if singletonExists {
			return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteConflict
		}
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteFailed
	}
	if !singletonExists ||
		result.HomepageVersion != expectedHomepageVersion+1 ||
		result.HeroVersion <= 0 {
		return adminHomepageHeroWriteResult{}, errAdminSiteContentWriteFailed
	}

	return result, nil
}

// nullableAdminHomepageFeatureID maps zero to SQL NULL and preserves one
// positive selected identity.
func nullableAdminHomepageFeatureID(id int64) any {
	if id == 0 {
		return nil
	}

	return id
}

// isValidAdminHomepageContentWriteInput mirrors every editable migration-9
// Homepage invariant except dependencies that must be rechecked transactionally.
func isValidAdminHomepageContentWriteInput(
	input adminHomepageContentWriteInput,
) bool {
	return isValidSiteContentSingleLine(
		input.StudioName,
		homepageStudioNameMaximumLength,
	) && isValidSiteContentSingleLine(
		input.Descriptor,
		homepageDescriptorMaximumLength,
	) && input.FeaturedInteriorProjectID >= 0 &&
		input.FeaturedArchitectureProjectID >= 0 &&
		input.FeaturedProductID >= 0 &&
		isValidSiteContentSingleLine(
			input.SEOTitle,
			siteSEOTitleMaximumLength,
		) && isValidSiteContentSingleLine(
		input.SEODescription,
		siteSEODescriptionMaximumLength,
	)
}

// isValidAdminContactContentWriteInput mirrors every administrator-editable
// Contact invariant before a database call is attempted.
func isValidAdminContactContentWriteInput(
	input adminContactContentWriteInput,
) bool {
	return isValidSiteContentSingleLine(
		input.Eyebrow,
		contactEyebrowMaximumLength,
	) && isValidSiteContentSingleLine(
		input.Heading,
		contactHeadingMaximumLength,
	) && isValidSiteContentMultiline(
		input.Introduction,
		contactIntroductionMaximumLength,
		true,
	) && isValidPublicContactEmail(input.ContactEmail) &&
		isValidPublicContactPhone(input.PhoneDisplay, input.PhoneE164) &&
		isValidSiteContentMultiline(
			input.Address,
			contactAddressMaximumLength,
			false,
		) && isValidSiteContentSingleLine(
		input.SEOTitle,
		siteSEOTitleMaximumLength,
	) && isValidSiteContentSingleLine(
		input.SEODescription,
		siteSEODescriptionMaximumLength,
	)
}

// isValidAdminHomepageHeroWriteInput proves every stored image fact matches its
// normalized bytes and that alt text satisfies migration 9.
func isValidAdminHomepageHeroWriteInput(
	input adminHomepageHeroWriteInput,
) bool {
	if input.ByteSize != len(input.Content) ||
		!isValidRequiredReviewedCoverText(
			input.AltText,
			reviewedCoverAltTextMaximumLength,
		) {
		return false
	}

	inspection, err := inspectReviewedCover(input.Content, false)
	if err != nil {
		return false
	}

	return input.ContentType == inspection.ContentType &&
		input.Width == inspection.Width &&
		input.Height == inspection.Height &&
		input.SHA256 == inspection.SHA256
}
