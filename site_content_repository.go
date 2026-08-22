package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
)

// readHomepageContentSQL reads one mandatory singleton, an enabled managed
// hero's binary-free metadata, and at most one fully public featured record per
// discipline. Each feature subquery requires both Published lifecycle and a
// current reviewed cover, so a Draft, Archived, missing, or coverless selection
// becomes the same all-NULL public projection.
const readHomepageContentSQL = `SELECT
    homepage.id,
    homepage.studio_name,
    homepage.descriptor,
    homepage.seo_title,
    homepage.seo_description,
    homepage.managed_hero_enabled,
    hero.version,
    hero.width,
    hero.height,
    hero.alt_text,
    featured_interior.id,
    featured_interior.slug,
    featured_interior.title,
    featured_interior.classification,
    featured_interior.cover_version,
    featured_interior.cover_width,
    featured_interior.cover_height,
    featured_interior.cover_alt_text,
    featured_architecture.id,
    featured_architecture.slug,
    featured_architecture.title,
    featured_architecture.classification,
    featured_architecture.cover_version,
    featured_architecture.cover_width,
    featured_architecture.cover_height,
    featured_architecture.cover_alt_text,
    featured_product.id,
    featured_product.slug,
    featured_product.title,
    featured_product.classification,
    featured_product.cover_version,
    featured_product.cover_width,
    featured_product.cover_height,
    featured_product.cover_alt_text
FROM public.homepage_content AS homepage
LEFT JOIN public.homepage_hero_images AS hero
    ON hero.homepage_content_id = homepage.id
   AND homepage.managed_hero_enabled = TRUE
LEFT JOIN (
    SELECT
        projects.id,
        projects.slug,
        projects.title,
        projects.typology AS classification,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text
    FROM public.interior_projects AS projects
    INNER JOIN public.interior_project_cover_images AS cover
        ON cover.interior_project_id = projects.id
    WHERE projects.publication_status = $1
) AS featured_interior
    ON featured_interior.id = homepage.featured_interior_project_id
LEFT JOIN (
    SELECT
        projects.id,
        projects.slug,
        projects.title,
        projects.typology AS classification,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text
    FROM public.architecture_projects AS projects
    INNER JOIN public.architecture_project_cover_images AS cover
        ON cover.architecture_project_id = projects.id
    WHERE projects.publication_status = $1
) AS featured_architecture
    ON featured_architecture.id = homepage.featured_architecture_project_id
LEFT JOIN (
    SELECT
        products.id,
        products.slug,
        products.name AS title,
        products.category AS classification,
        cover.version AS cover_version,
        cover.width AS cover_width,
        cover.height AS cover_height,
        cover.alt_text AS cover_alt_text
    FROM public.products AS products
    INNER JOIN public.product_cover_images AS cover
        ON cover.product_id = products.id
    WHERE products.publication_status = $1
) AS featured_product
    ON featured_product.id = homepage.featured_product_id
WHERE homepage.id = $2`

// readContactContentSQL reads only the public presentation fields from the one
// mandatory Contact singleton. Optimistic revision and timestamps remain at the
// protected administration boundary.
const readContactContentSQL = `SELECT
    id,
    eyebrow,
    heading,
    introduction,
    contact_email,
    phone_display,
    phone_e164,
    address,
    seo_title,
    seo_description
FROM public.contact_content
WHERE id = $1`

// findHomepageHeroSQL returns exact current bytes only while the owning
// Homepage explicitly enables managed media. Disabled, missing, and stale
// revisions therefore share one no-row result.
const findHomepageHeroSQL = `SELECT
    hero.homepage_content_id,
    hero.version,
    hero.content_type,
    hero.content,
    hero.byte_size,
    hero.width,
    hero.height,
    hero.sha256,
    hero.alt_text,
    hero.created_at,
    hero.updated_at
FROM public.homepage_hero_images AS hero
INNER JOIN public.homepage_content AS homepage
    ON homepage.id = hero.homepage_content_id
WHERE homepage.id = $1
  AND homepage.managed_hero_enabled = TRUE
  AND hero.version = $2`

// siteContentRowScanner is the one-method database/sql behavior shared by all
// singleton and exact-media reads.
type siteContentRowScanner interface {
	// Scan copies one fixed projection into the supplied destinations.
	Scan(...any) error
}

// siteContentQueryRow adapts sql.DB.QueryRowContext to a deterministic unit-test
// seam without granting the repository any additional database authority.
type siteContentQueryRow func(
	context.Context,
	string,
	...any,
) siteContentRowScanner

// postgresSiteContentReader borrows the process-owned PostgreSQL pool for
// concurrent public reads. The composition root remains responsible for close.
type postgresSiteContentReader struct {
	// queryRow executes mandatory singleton and exact hero statements.
	queryRow siteContentQueryRow
}

// Compile-time verification catches signature drift from the public handler
// contract before the server starts.
var _ siteContentReader = (*postgresSiteContentReader)(nil)

// newPostgresSiteContentReader adapts the shared database pool without opening
// a connection or issuing a query during construction.
func newPostgresSiteContentReader(
	database *sql.DB,
) (*postgresSiteContentReader, error) {
	if database == nil {
		return nil, errSiteContentReaderDatabaseRequired
	}

	return &postgresSiteContentReader{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) siteContentRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// ReadHomepage reads and validates the complete public Homepage projection. A
// missing seeded row is an operational failure, never an empty public page.
func (reader *postgresSiteContentReader) ReadHomepage(
	ctx context.Context,
) (publicHomepageContent, error) {
	if ctx == nil {
		return publicHomepageContent{}, errSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return publicHomepageContent{}, errSiteContentReadFailed
	}

	content, err := scanPublicHomepageContent(
		reader.queryRow(
			ctx,
			readHomepageContentSQL,
			publishedSiteContentStatus,
			siteContentSingletonID,
		),
	)
	if err != nil {
		return publicHomepageContent{}, errSiteContentReadFailed
	}

	return content, nil
}

// ReadContact reads and validates the public Contact singleton. It deliberately
// does not read inquiry rows, administrator metadata, or optimistic revision.
func (reader *postgresSiteContentReader) ReadContact(
	ctx context.Context,
) (publicContactContent, error) {
	if ctx == nil {
		return publicContactContent{}, errSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return publicContactContent{}, errSiteContentReadFailed
	}

	content, err := scanPublicContactContent(
		reader.queryRow(
			ctx,
			readContactContentSQL,
			siteContentSingletonID,
		),
	)
	if err != nil {
		return publicContactContent{}, errSiteContentReadFailed
	}

	return content, nil
}

// FindHomepageHero returns one isolated exact asset. The SQL publication join
// and safe no-row mapping prevent stale or disabled media from leaking.
func (reader *postgresSiteContentReader) FindHomepageHero(
	ctx context.Context,
	version int64,
) (homepageHeroAsset, error) {
	if ctx == nil || version <= 0 {
		return homepageHeroAsset{}, errSiteContentInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return homepageHeroAsset{}, errSiteContentReadFailed
	}

	asset, err := scanHomepageHeroAsset(
		reader.queryRow(
			ctx,
			findHomepageHeroSQL,
			siteContentSingletonID,
			version,
		),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return homepageHeroAsset{}, errHomepageHeroNotFound
	}
	if err != nil || asset.Version != version {
		return homepageHeroAsset{}, errSiteContentReadFailed
	}

	return asset, nil
}

// nullableHomepageFeature holds one LEFT JOIN projection until all-or-none
// nullability and discipline-specific public invariants have been verified.
type nullableHomepageFeature struct {
	// Base record columns must be entirely NULL or entirely present.
	id             sql.NullInt64
	slug           sql.NullString
	title          sql.NullString
	classification sql.NullString
	// Cover columns follow the same all-or-none rule and are required whenever
	// the feature record exists.
	coverVersion sql.NullInt64
	coverWidth   sql.NullInt64
	coverHeight  sql.NullInt64
	coverAltText sql.NullString
}

// destinations returns pointers in the exact order selected by each feature
// projection in readHomepageContentSQL.
func (feature *nullableHomepageFeature) destinations() []any {
	return []any{
		&feature.id,
		&feature.slug,
		&feature.title,
		&feature.classification,
		&feature.coverVersion,
		&feature.coverWidth,
		&feature.coverHeight,
		&feature.coverAltText,
	}
}

// publicFeature converts one nullable SQL group into either no public item or
// one complete cover-backed discipline feature.
func (feature nullableHomepageFeature) publicFeature(
	discipline homepageFeatureDiscipline,
) (*publicHomepageFeature, error) {
	baseValues := []bool{
		feature.id.Valid,
		feature.slug.Valid,
		feature.title.Valid,
		feature.classification.Valid,
	}
	coverValues := []bool{
		feature.coverVersion.Valid,
		feature.coverWidth.Valid,
		feature.coverHeight.Valid,
		feature.coverAltText.Valid,
	}
	baseCount := countTrueValues(baseValues)
	coverCount := countTrueValues(coverValues)
	if baseCount == 0 && coverCount == 0 {
		return nil, nil
	}
	if baseCount != len(baseValues) || coverCount != len(coverValues) ||
		feature.id.Int64 <= 0 ||
		feature.coverWidth.Int64 > int64(math.MaxInt) ||
		feature.coverWidth.Int64 < int64(math.MinInt) ||
		feature.coverHeight.Int64 > int64(math.MaxInt) ||
		feature.coverHeight.Int64 < int64(math.MinInt) {
		return nil, errSiteContentReadFailed
	}

	result := &publicHomepageFeature{
		Discipline:     discipline,
		Slug:           feature.slug.String,
		Title:          feature.title.String,
		Classification: feature.classification.String,
		Cover: &homepageFeatureCover{
			Version: feature.coverVersion.Int64,
			Width:   int(feature.coverWidth.Int64),
			Height:  int(feature.coverHeight.Int64),
			AltText: feature.coverAltText.String,
		},
	}
	if !isValidHomepageFeature(*result) {
		return nil, errSiteContentReadFailed
	}

	return result, nil
}

// countTrueValues counts present nullable columns without relying on a fragile
// chain of pairwise Boolean comparisons.
func countTrueValues(values []bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}

	return count
}

// scanPublicHomepageContent converts the one-row joined projection, rejects
// partial nullable groups, and assembles feature order in application code.
func scanPublicHomepageContent(
	scanner siteContentRowScanner,
) (publicHomepageContent, error) {
	if scanner == nil {
		return publicHomepageContent{}, errSiteContentReadFailed
	}

	var homepageID int64
	var content publicHomepageContent
	var managedHeroEnabled bool
	var heroVersion sql.NullInt64
	var heroWidth sql.NullInt64
	var heroHeight sql.NullInt64
	var heroAltText sql.NullString
	var interior nullableHomepageFeature
	var architecture nullableHomepageFeature
	var product nullableHomepageFeature
	destinations := []any{
		&homepageID,
		&content.StudioName,
		&content.Descriptor,
		&content.SEOTitle,
		&content.SEODescription,
		&managedHeroEnabled,
		&heroVersion,
		&heroWidth,
		&heroHeight,
		&heroAltText,
	}
	destinations = append(destinations, interior.destinations()...)
	destinations = append(destinations, architecture.destinations()...)
	destinations = append(destinations, product.destinations()...)
	if err := scanner.Scan(destinations...); err != nil {
		return publicHomepageContent{}, err
	}
	if homepageID != siteContentSingletonID {
		return publicHomepageContent{}, errSiteContentReadFailed
	}

	heroValues := []bool{
		heroVersion.Valid,
		heroWidth.Valid,
		heroHeight.Valid,
		heroAltText.Valid,
	}
	heroCount := countTrueValues(heroValues)
	if (managedHeroEnabled && heroCount != len(heroValues)) ||
		(!managedHeroEnabled && heroCount != 0) {
		return publicHomepageContent{}, errSiteContentReadFailed
	}
	if heroCount == len(heroValues) {
		if heroWidth.Int64 > int64(math.MaxInt) ||
			heroWidth.Int64 < int64(math.MinInt) ||
			heroHeight.Int64 > int64(math.MaxInt) ||
			heroHeight.Int64 < int64(math.MinInt) {
			return publicHomepageContent{}, errSiteContentReadFailed
		}
		content.Hero = &homepageHeroMetadata{
			Version: heroVersion.Int64,
			Width:   int(heroWidth.Int64),
			Height:  int(heroHeight.Int64),
			AltText: heroAltText.String,
		}
	}

	content.Features = make([]publicHomepageFeature, 0, 3)
	featureGroups := []struct {
		// discipline fixes presentation order independently of selected IDs.
		discipline homepageFeatureDiscipline
		// nullable contains one privacy-filtered SQL group.
		nullable nullableHomepageFeature
	}{
		{homepageFeatureInterior, interior},
		{homepageFeatureArchitecture, architecture},
		{homepageFeatureProduct, product},
	}
	for _, group := range featureGroups {
		feature, err := group.nullable.publicFeature(group.discipline)
		if err != nil {
			return publicHomepageContent{}, errSiteContentReadFailed
		}
		if feature != nil {
			content.Features = append(content.Features, *feature)
		}
	}
	if !isValidPublicHomepageContent(content) {
		return publicHomepageContent{}, errSiteContentReadFailed
	}

	return content, nil
}

// scanPublicContactContent reads the fixed singleton projection and rejects a
// wrong identity or malformed stored value before it can enter a template.
func scanPublicContactContent(
	scanner siteContentRowScanner,
) (publicContactContent, error) {
	if scanner == nil {
		return publicContactContent{}, errSiteContentReadFailed
	}

	var contactID int64
	var content publicContactContent
	if err := scanner.Scan(
		&contactID,
		&content.Eyebrow,
		&content.Heading,
		&content.Introduction,
		&content.Email,
		&content.PhoneDisplay,
		&content.PhoneE164,
		&content.Address,
		&content.SEOTitle,
		&content.SEODescription,
	); err != nil {
		return publicContactContent{}, err
	}
	if contactID != siteContentSingletonID ||
		!isValidPublicContactContent(content) {
		return publicContactContent{}, errSiteContentReadFailed
	}

	return content, nil
}

// scanHomepageHeroAsset converts the digest to fixed-size storage, validates
// the singleton owner and complete normalized image, and isolates its bytes.
func scanHomepageHeroAsset(
	scanner siteContentRowScanner,
) (homepageHeroAsset, error) {
	if scanner == nil {
		return homepageHeroAsset{}, errSiteContentReadFailed
	}

	var homepageID int64
	var asset homepageHeroAsset
	var digest []byte
	if err := scanner.Scan(
		&homepageID,
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
	); err != nil {
		return homepageHeroAsset{}, err
	}
	if homepageID != siteContentSingletonID || len(digest) != sha256.Size {
		return homepageHeroAsset{}, errSiteContentReadFailed
	}
	copy(asset.SHA256[:], digest)
	if !isValidHomepageHeroAsset(asset) {
		return homepageHeroAsset{}, errSiteContentReadFailed
	}

	return cloneHomepageHeroAsset(asset), nil
}
