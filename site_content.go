package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// siteContentSingletonID is the only identity accepted for each managed
	// Homepage and Contact row. A fixed identity keeps these records settings,
	// rather than allowing an accidental second public version to appear.
	siteContentSingletonID int64 = 1
	// publishedSiteContentStatus is the site-content repository's trusted shared
	// lifecycle filter for the three independently stored discipline records.
	publishedSiteContentStatus = "published"
	// homepageStudioNameMaximumLength mirrors the migration-owned character
	// bound for the primary homepage identity.
	homepageStudioNameMaximumLength = 120
	// homepageDescriptorMaximumLength bounds the short identity line beneath the
	// homepage heading.
	homepageDescriptorMaximumLength = 160
	// siteSEOTitleMaximumLength bounds one complete managed document title.
	siteSEOTitleMaximumLength = 160
	// siteSEODescriptionMaximumLength bounds one managed meta description.
	siteSEODescriptionMaximumLength = 320
	// contactEyebrowMaximumLength bounds the short Contact interface label.
	contactEyebrowMaximumLength = 80
	// contactHeadingMaximumLength bounds the Contact page's primary heading.
	contactHeadingMaximumLength = 160
	// contactIntroductionMaximumLength bounds the reviewed Contact introduction.
	contactIntroductionMaximumLength = 1200
	// contactEmailMaximumLength uses the conventional maximum mailbox length.
	contactEmailMaximumLength = 254
	// contactPhoneDisplayMaximumLength bounds the human-readable telephone label.
	contactPhoneDisplayMaximumLength = 60
	// contactPhoneE164MaximumLength includes E.164's leading plus character.
	contactPhoneE164MaximumLength = 16
	// contactAddressMaximumLength bounds optional public postal-address copy.
	contactAddressMaximumLength = 500
)

// contactPhoneE164Pattern accepts a plus-prefixed international number with a
// non-zero country-code digit. Formatting belongs in PhoneDisplay, never in the
// machine-readable tel destination.
var contactPhoneE164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// contactEmailPattern mirrors the migration's intentionally modest mailbox
// shape. The page mapper separately percent-escapes the mailto address
// component; this grammar prevents whitespace, multiple separators, and a
// domain without a suffix.
var contactEmailPattern = regexp.MustCompile(
	`^[^\s@]+@[^\s@]+\.[^\s@]+$`,
)

// Public site-content errors are stable, credential-free categories. Driver
// diagnostics are deliberately collapsed because they may expose SQL, stored
// editorial values, connection details, or private draft identifiers.
var (
	// errSiteContentReaderDatabaseRequired rejects repository construction
	// without the process-owned PostgreSQL pool.
	errSiteContentReaderDatabaseRequired = errors.New(
		"create site content reader: database is required",
	)
	// errSiteContentInvalidQuery identifies a nil context or a non-canonical
	// hero revision before PostgreSQL is contacted.
	errSiteContentInvalidQuery = errors.New("site content query is invalid")
	// errSiteContentReadFailed collapses missing singleton rows, driver failures,
	// and malformed stored public projections into one safe service category.
	errSiteContentReadFailed = errors.New("site content database operation failed")
	// errHomepageHeroNotFound hides disabled, absent, and stale managed hero
	// revisions behind one public media result.
	errHomepageHeroNotFound = errors.New("homepage hero not found")
)

// homepageFeatureDiscipline identifies one of the three fixed public feature
// slots without accepting an administrator-authored path or arbitrary label.
type homepageFeatureDiscipline uint8

const (
	// homepageFeatureInterior is rendered first, matching the established
	// homepage discipline order.
	homepageFeatureInterior homepageFeatureDiscipline = iota + 1
	// homepageFeatureArchitecture is the fixed second homepage feature slot.
	homepageFeatureArchitecture
	// homepageFeatureProduct is the fixed final homepage feature slot.
	homepageFeatureProduct
)

// homepageHeroMetadata is the binary-free current managed hero projection safe
// to pass through ordinary Homepage reads.
type homepageHeroMetadata struct {
	// Version identifies the exact revision in the public media URL.
	Version int64
	// Width is the decoded image width used to reserve layout space.
	Width int
	// Height is the decoded image height used to reserve layout space.
	Height int
	// AltText is the required meaningful alternative for the photograph.
	AltText string
}

// homepageFeatureCover is the common binary-free image projection used by the
// three fixed feature card types.
type homepageFeatureCover struct {
	// Version identifies the current discipline-owned public cover revision.
	Version int64
	// Width and Height are trusted decoded dimensions.
	Width  int
	Height int
	// AltText is the reviewed meaningful image alternative.
	AltText string
}

// publicHomepageFeature contains only fields already eligible for an ordinary
// published detail page. Selection IDs and private lifecycle values never cross
// this boundary.
type publicHomepageFeature struct {
	// Discipline selects an application-owned label and canonical path builder.
	Discipline homepageFeatureDiscipline
	// Slug is the canonical segment already used by the discipline detail route.
	Slug string
	// Title is the published Product name or project title.
	Title string
	// Classification is the Product category or project typology.
	Classification string
	// Cover contains required current published cover metadata. A nil value is a
	// malformed substituted result and never reaches the public Homepage.
	Cover *homepageFeatureCover
}

// publicHomepageContent is the complete public-only projection required by
// GET /. It contains no optimistic version, timestamps, selected private IDs,
// or publication state.
type publicHomepageContent struct {
	// StudioName is the homepage's primary identity.
	StudioName string
	// Descriptor is the short studio role displayed beneath the identity.
	Descriptor string
	// SEOTitle is the exact complete browser document title.
	SEOTitle string
	// SEODescription is the exact managed meta-description content.
	SEODescription string
	// Hero is the enabled current managed image metadata. Nil selects the
	// checked-in static fallback without inventing database content.
	Hero *homepageHeroMetadata
	// Features contains zero to three published records in fixed discipline order.
	Features []publicHomepageFeature
}

// publicContactContent contains reviewed studio information safe to expose at
// GET /contact. Inquiry protocol copy and visitor values remain outside it.
type publicContactContent struct {
	// Eyebrow is the short interface label above the Contact heading.
	Eyebrow string
	// Heading is the page's primary visible heading.
	Heading string
	// Introduction explains what the studio asks a visitor to share.
	Introduction string
	// Email is an optional exact public mailbox, without a display name.
	Email string
	// PhoneDisplay is optional human-readable telephone copy.
	PhoneDisplay string
	// PhoneE164 is the canonical machine-readable tel destination paired with
	// PhoneDisplay.
	PhoneE164 string
	// Address is optional reviewed multiline public address copy.
	Address string
	// SEOTitle is the exact managed Contact document title.
	SEOTitle string
	// SEODescription is the managed Contact meta description.
	SEODescription string
}

// homepageHeroAsset contains one complete normalized current managed hero. It
// is used only by binary media handlers and protected preview code, never by an
// ordinary HTML template.
type homepageHeroAsset struct {
	// Version is the exact revision encoded in the public path.
	Version int64
	// ContentType is the decoder-derived JPEG or PNG response type.
	ContentType string
	// Content contains one complete bounded normalized image.
	Content []byte
	// ByteSize duplicates len(Content) as a database integrity assertion.
	ByteSize int
	// Width and Height are the decoded image dimensions.
	Width  int
	Height int
	// SHA256 validates stored bytes and supplies the strong response ETag.
	SHA256 [sha256.Size]byte
	// AltText is carried for repository validation and HTML metadata reads; it is
	// never written into the binary response body.
	AltText string
	// CreatedAt and UpdatedAt prove revision timestamps remain ordered.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// siteContentReader is the narrow public read authority needed by Homepage,
// Contact, and exact managed-hero HTTP handlers.
type siteContentReader interface {
	// ReadHomepage returns the mandatory singleton and only currently published
	// featured records.
	ReadHomepage(context.Context) (publicHomepageContent, error)
	// ReadContact returns the mandatory public Contact singleton.
	ReadContact(context.Context) (publicContactContent, error)
	// FindHomepageHero returns one exact current revision only while managed hero
	// publication remains enabled.
	FindHomepageHero(context.Context, int64) (homepageHeroAsset, error)
}

// isValidSiteContentSingleLine accepts a required, trimmed, control-free UTF-8
// value within its Unicode-character bound.
func isValidSiteContentSingleLine(value string, maximumLength int) bool {
	return value != "" && isValidOptionalReviewedCoverText(value, maximumLength)
}

// isValidSiteContentMultiline accepts reviewed trimmed text while permitting
// line feeds and rejecting every other control or Unicode separator character.
func isValidSiteContentMultiline(
	value string,
	maximumLength int,
	required bool,
) bool {
	if maximumLength <= 0 || !utf8.ValidString(value) ||
		strings.ContainsRune(value, '\x00') ||
		strings.TrimSpace(value) != value ||
		utf8.RuneCountInString(value) > maximumLength ||
		(required && value == "") {
		return false
	}
	for _, character := range value {
		if character == '\n' {
			continue
		}
		if unicode.IsControl(character) ||
			unicode.In(character, unicode.Zl, unicode.Zp) {
			return false
		}
	}

	return true
}

// isValidPublicContactEmail accepts an empty value or one normalized lowercase
// mailbox matching the migration-owned shape without whitespace or controls.
func isValidPublicContactEmail(value string) bool {
	if value == "" {
		return true
	}
	if !isValidOptionalReviewedCoverText(value, contactEmailMaximumLength) {
		return false
	}
	for _, character := range value {
		if unicode.IsSpace(character) {
			return false
		}
	}
	return value == strings.ToLower(value) && contactEmailPattern.MatchString(value)
}

// isValidPublicContactPhone verifies the display and machine values are both
// absent or both present and canonical.
func isValidPublicContactPhone(display string, e164 string) bool {
	if display == "" || e164 == "" {
		return display == "" && e164 == ""
	}

	return isValidSiteContentSingleLine(
		display,
		contactPhoneDisplayMaximumLength,
	) && len(e164) <= contactPhoneE164MaximumLength &&
		contactPhoneE164Pattern.MatchString(e164)
}

// isValidHomepageHeroMetadata validates current managed image metadata before a
// handler constructs a public path or meaningful img element.
func isValidHomepageHeroMetadata(metadata homepageHeroMetadata) bool {
	return isValidReviewedCoverMetadata(
		metadata.Version,
		metadata.Width,
		metadata.Height,
		metadata.AltText,
		"",
	)
}

// isValidHomepageFeature validates one discipline-specific public projection
// and its required current cover metadata.
func isValidHomepageFeature(feature publicHomepageFeature) bool {
	validText := false
	switch feature.Discipline {
	case homepageFeatureInterior:
		validText = isCanonicalInteriorProjectSlug(feature.Slug) &&
			isValidInteriorProjectCatalogueText(
				feature.Title,
				interiorProjectTitleMaximumLength,
			) && isValidInteriorProjectCatalogueText(
			feature.Classification,
			interiorProjectTypologyMaximumLength,
		)
	case homepageFeatureArchitecture:
		validText = isCanonicalArchitectureProjectSlug(feature.Slug) &&
			isValidArchitectureProjectCatalogueText(
				feature.Title,
				architectureProjectTitleMaximumLength,
			) && isValidArchitectureProjectCatalogueText(
			feature.Classification,
			architectureProjectTypologyMaximumLength,
		)
	case homepageFeatureProduct:
		validText = isCanonicalProductSlug(feature.Slug) &&
			isValidProductCatalogueText(
				feature.Title,
				productNameMaximumLength,
			) && isValidProductCatalogueText(
			feature.Classification,
			productCategoryMaximumLength,
		)
	default:
		return false
	}
	if !validText {
		return false
	}
	if feature.Cover == nil {
		return false
	}

	return isValidReviewedCoverMetadata(
		feature.Cover.Version,
		feature.Cover.Width,
		feature.Cover.Height,
		feature.Cover.AltText,
		"",
	)
}

// isValidPublicHomepageContent checks managed copy, SEO, enabled hero metadata,
// feature validation, and strictly increasing fixed discipline order.
func isValidPublicHomepageContent(content publicHomepageContent) bool {
	if !isValidSiteContentSingleLine(
		content.StudioName,
		homepageStudioNameMaximumLength,
	) || !isValidSiteContentSingleLine(
		content.Descriptor,
		homepageDescriptorMaximumLength,
	) || !isValidSiteContentSingleLine(
		content.SEOTitle,
		siteSEOTitleMaximumLength,
	) || !isValidSiteContentSingleLine(
		content.SEODescription,
		siteSEODescriptionMaximumLength,
	) || (content.Hero != nil &&
		!isValidHomepageHeroMetadata(*content.Hero)) ||
		len(content.Features) > 3 {
		return false
	}

	previousDiscipline := homepageFeatureDiscipline(0)
	for _, feature := range content.Features {
		if feature.Discipline <= previousDiscipline ||
			!isValidHomepageFeature(feature) {
			return false
		}
		previousDiscipline = feature.Discipline
	}

	return true
}

// isValidPublicContactContent verifies every stored value before it can enter
// an HTML attribute, visible address, mailto link, tel link, title, or meta tag.
func isValidPublicContactContent(content publicContactContent) bool {
	return isValidSiteContentSingleLine(
		content.Eyebrow,
		contactEyebrowMaximumLength,
	) && isValidSiteContentSingleLine(
		content.Heading,
		contactHeadingMaximumLength,
	) && isValidSiteContentMultiline(
		content.Introduction,
		contactIntroductionMaximumLength,
		true,
	) && isValidPublicContactEmail(content.Email) &&
		isValidPublicContactPhone(content.PhoneDisplay, content.PhoneE164) &&
		isValidSiteContentMultiline(
			content.Address,
			contactAddressMaximumLength,
			false,
		) && isValidSiteContentSingleLine(
		content.SEOTitle,
		siteSEOTitleMaximumLength,
	) && isValidSiteContentSingleLine(
		content.SEODescription,
		siteSEODescriptionMaximumLength,
	)
}

// isValidHomepageHeroAsset applies the shared normalized-image security and
// integrity boundary to the singleton Homepage owner.
func isValidHomepageHeroAsset(asset homepageHeroAsset) bool {
	return isValidReviewedCoverAsset(
		siteContentSingletonID,
		asset.Version,
		asset.ContentType,
		asset.Content,
		asset.ByteSize,
		asset.Width,
		asset.Height,
		asset.SHA256,
		asset.AltText,
		"",
		asset.CreatedAt,
		asset.UpdatedAt,
	)
}

// cloneHomepageHeroAsset isolates mutable encoded bytes whenever an asset
// crosses a repository or test-double boundary.
func cloneHomepageHeroAsset(asset homepageHeroAsset) homepageHeroAsset {
	asset.Content = append([]byte(nil), asset.Content...)

	return asset
}
