package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"
)

// application holds dependencies shared by all HTTP handlers.
//
// Keeping these values on an application instance avoids mutable global state
// and makes it straightforward for tests to create an isolated application.
type application struct {
	// templates maps public filenames such as "home.html" and namespaced private
	// keys such as "admin/login.html" to their isolated parsed template sets.
	templates map[string]*template.Template
	// products is the read-only published-catalogue and exact-cover boundary
	// shared by public Product HTML and media handlers. Production supplies
	// PostgreSQL, while tests inject records without opening a database.
	products productCatalogueReader
	// interiorProjects is the published-only Interior catalogue and exact-cover
	// boundary shared by the public listing, detail, and media handlers.
	// Keeping it separate from Architecture prevents Stage 22 publication rules
	// from accidentally exposing or constraining the later discipline.
	interiorProjects interiorProjectCatalogueReader
	// architectureProjects is the published-only Architecture catalogue and
	// exact-cover boundary shared by public listing, detail, and media handlers.
	// Its discipline-specific interface prevents Interior and Architecture
	// publication rules from becoming coupled through one generic repository.
	architectureProjects architectureProjectCatalogueReader
	// siteContent is the public-only Homepage, Contact, and exact managed-hero
	// boundary. Its projections exclude private feature selections and versions.
	siteContent siteContentReader
	// inquiries is the narrow write dependency used only by the public Contact
	// submission handler. Production supplies PostgreSQL, while tests can inject
	// a recording implementation without opening a database connection.
	inquiries inquiryRepository
	// admins owns the narrow PostgreSQL operations used by administrator
	// creation, login, session lookup, and revocation. HTTP handlers never issue
	// authentication SQL directly.
	admins adminRepository
	// adminProducts is the read-only all-status Product boundary used by protected
	// catalogue, detail, edit, and exact-cover preview handlers.
	adminProducts adminProductReader
	// adminProductWrites is the separate Product text and single-cover mutation
	// authority. Keeping it apart from readers makes permission explicit.
	adminProductWrites adminProductWriter
	// adminInteriorProjects provides protected all-state Interior project and
	// exact-cover reads without granting mutation authority.
	adminInteriorProjects adminInteriorProjectReader
	// adminInteriorProjectWrites owns Interior create, optimistic edit, and
	// single-cover mutations. It deliberately exposes no delete operation.
	adminInteriorProjectWrites adminInteriorProjectWriter
	// adminArchitectureProjects provides protected all-state Architecture
	// project and exact-cover reads without granting mutation authority.
	adminArchitectureProjects adminArchitectureProjectReader
	// adminArchitectureProjectWrites owns Architecture create, optimistic edit,
	// and single-cover mutations. It deliberately exposes no delete operation.
	adminArchitectureProjectWrites adminArchitectureProjectWriter
	// adminSiteContent provides protected reads for the Homepage and Contact
	// singletons, feature candidates, and exact managed-hero previews.
	adminSiteContent adminSiteContentReader
	// adminSiteContentWrites owns optimistic Homepage, Contact, and hero changes.
	// Its separation from the reader keeps private mutation authority explicit.
	adminSiteContentWrites adminSiteContentWriter
	// adminInquiries is the read-only personal-data boundary used by the private
	// inquiry inbox. It remains separate from the public Contact write interface.
	adminInquiries adminInquiryReader
	// adminInquiryStatuses is the narrow private mutation boundary used only for
	// explicit inquiry workflow changes. Keeping it separate from both inquiry
	// readers and the public Contact writer makes its database authority visible.
	adminInquiryStatuses adminInquiryStatusUpdater
	// adminPasswords owns the versioned password hashing and verification format.
	// Depending on its interface keeps an inexpensive deterministic manager
	// injectable in tests while production always selects the full work factor.
	adminPasswords adminPasswordManager
	// adminDummyPasswordHash is verified for unknown accounts so login requests
	// do not skip the intentionally expensive password work based on existence.
	adminDummyPasswordHash string
	// adminLoginPasswordWork is a bounded semaphore for account lookup and the
	// deliberately expensive verifier. A channel gives acquisition a nonblocking
	// select and requires no client identifier or unbounded per-account map.
	adminLoginPasswordWork chan struct{}
	// adminEntropy supplies independently random login, session, and CSRF tokens.
	// Production uses crypto/rand.Reader; the field is replaceable only in tests.
	adminEntropy io.Reader
	// now centralizes absolute session and cookie times and permits deterministic
	// expiry tests without changing the system clock.
	now func() time.Time
	// inquirySuccess signs and consumes the short-lived receipt carried across
	// the Post/Redirect/Get boundary. It contains no visitor data and never
	// exposes its process-local key.
	inquirySuccess *inquirySuccessFlash
}

// Application construction errors identify missing runtime dependencies before
// the HTTP server begins accepting requests.
var (
	// errSiteContentReaderRequired prevents public managed pages from starting
	// with hard-coded content after Stage 24 makes their singleton rows durable.
	errSiteContentReaderRequired = errors.New(
		"create application: site content reader is required",
	)
	// errInquiryRepositoryRequired prevents a valid-looking application from
	// silently accepting Contact submissions that cannot be persisted.
	errInquiryRepositoryRequired = errors.New(
		"create application: inquiry repository is required",
	)
)

// homeHeroData describes the content and media needed by the homepage hero.
//
// This view model is intentionally about template needs rather than database
// storage. The handler maps either managed metadata or the checked-in fallback
// into the same stable shape.
type homeHeroData struct {
	// StudioName is the primary identity displayed as the hero heading.
	StudioName string
	// Descriptor briefly states the studio's role beneath its name.
	Descriptor string
	// ImageURL is the browser-accessible path to the hero image.
	ImageURL string
	// ImageAlt describes meaningful image content for visitors who cannot see it.
	ImageAlt string
	// ImageWidth is the source image width used to reserve layout space.
	ImageWidth int
	// ImageHeight is the source image height used to reserve layout space.
	ImageHeight int
}

// seoPageData contains exact managed metadata for one public document. The
// canonical path is application-owned and never derived from a request Host or
// administrator-authored absolute URL.
type seoPageData struct {
	// Title is the complete document title emitted without another brand suffix.
	Title string
	// Description is the exact content of the page's description meta element.
	Description string
	// CanonicalPath is the registered relative route represented by the response.
	CanonicalPath string
}

// homeFeaturedCoverData is the binary-free meaningful image contract for one
// published Homepage feature card.
type homeFeaturedCoverData struct {
	// Path is the discipline-owned exact current cover URL.
	Path string
	// AltText is the reviewed meaningful alternative supplied with that cover.
	AltText string
	// Width and Height reserve the reviewed image's intrinsic aspect ratio.
	Width  int
	Height int
}

// homeFeaturedItemData is the complete public presentation shape for one of the
// three fixed discipline feature slots.
type homeFeaturedItemData struct {
	// Discipline is the application-owned visible category label.
	Discipline string
	// Title is the published Product name or project title.
	Title string
	// Classification is the Product category or project typology.
	Classification string
	// Path is the existing canonical server-rendered detail route.
	Path string
	// Cover is required by the repository-backed mapping. The template retains a
	// defensive nil treatment so an incomplete future caller cannot emit a broken
	// meaningful image.
	Cover *homeFeaturedCoverData
}

// disciplineEntranceData is the small view model needed by one homepage
// discipline link.
//
// It deliberately contains fixed presentation and route data only. Featured
// database records use a separate cover-backed view, so this structural index
// never couples itself to IDs, publication status, or slugs.
type disciplineEntranceData struct {
	// Number is a decorative editorial sequence displayed beside the link.
	Number string
	// Name is the visible discipline name and the link's accessible text.
	Name string
	// Path is the real server-rendered destination used by the link's href.
	Path string
}

// disciplinePageData is the minimal view model shared by the three discipline
// landing pages.
//
// It contains route-level interface information only. Catalogue records belong
// to the product-specific view models below, while project descriptions, images,
// and publication state will enter their own vertical slices when designed.
type disciplinePageData struct {
	// Number is the zero-padded position displayed in the editorial sequence.
	Number string
	// Name is the visible h1 and also supplies the document title in the handler.
	Name string
	// NextName labels the next real discipline destination.
	NextName string
	// NextPath is the registered server-rendered URL for that destination.
	NextPath string
}

// productListingData describes the Products-only catalogue section.
//
// The pointer to this value is optional on pageData because only /products uses
// it. Keeping section copy beside its ordered preview slice establishes the
// handler-to-template boundary while the repository remains responsible for
// PostgreSQL eligibility, ordering, and stored-value validation.
type productListingData struct {
	// Eyebrow is the short interface label displayed above the section heading.
	Eyebrow string
	// Heading names the catalogue section and labels its semantic section.
	Heading string
	// Introduction explains the current scope without inventing product claims.
	Introduction string
	// EmptyMessage is shown when Items is nil or empty.
	EmptyMessage string
	// Items contains published catalogue previews in repository order.
	Items []productPreviewData
}

// productPreviewData is the minimal presentation shape for one published
// catalogue entry.
//
// It receives a complete trusted Path rather than exposing the stored Slug and
// intentionally omits database identity, publication state, sort order, prices,
// and detail-only content that the catalogue card does not use.
type productPreviewData struct {
	// Number is the zero-padded editorial position visible in the media field.
	Number string
	// Name is the published product heading displayed inside the catalogue card.
	Name string
	// Category identifies the published product's broad family.
	Category string
	// Status is trusted interface copy derived from the published-only boundary.
	Status string
	// Path is the real server-rendered detail URL used by the card anchor.
	Path string
	// Cover contains reviewed image metadata, or nil for the structural fallback.
	Cover *productCoverPageData
}

// productCoverPageData is the binary-free HTML image contract shared by public
// Product pages and protected detail/cover forms.
type productCoverPageData struct {
	// Path is the revision-specific cover route.
	Path string
	// AltText is the required meaningful image alternative.
	AltText string
	// Caption is optional reviewed visible copy.
	Caption string
	// Width reserves horizontal layout space before the image loads.
	Width string
	// Height reserves vertical layout space before the image loads.
	Height string
}

// productDetailData is the complete view model needed by the current published
// product detail page.
//
// It contains only the trusted published repository projection and presentation
// mapping. Reviewed copy, material, dimensions, and one cover are supported;
// pricing, purchasing, and richer specifications remain deferred.
type productDetailData struct {
	// Number is the catalogue position displayed as editorial context.
	Number string
	// Name is the detail page's one primary heading.
	Name string
	// Category identifies the product family in the visible facts list.
	Category string
	// Status is trusted interface copy derived from the published-only boundary.
	Status string
	// Description is optional reviewed long-form Product copy.
	Description string
	// Material is an optional reviewed material fact.
	Material string
	// Dimensions is an optional reviewed dimensions fact.
	Dimensions string
	// Cover contains reviewed image metadata, or nil for the honest fallback.
	Cover *productCoverPageData
}

// interiorProjectListingData describes the Interior Design showcase section.
//
// The pointer is optional on pageData because only /interior-design uses this
// contract. Published records remain the primary source; ReferenceItems provide
// the four approved concept previews only while that public projection is empty.
type interiorProjectListingData struct {
	// Heading labels the reference-aligned project section.
	Heading string
	// Items contains published previews in their database portfolio order.
	Items []interiorProjectPreviewData
	// ReferenceItems contains non-interactive checked-in concept previews used
	// only until an administrator publishes database-backed portfolio records.
	ReferenceItems []interiorReferenceProjectPreviewData
}

// interiorReferenceProjectPreviewData describes one approved local concept
// preview shown when the public Interior catalogue has no published records.
//
// It intentionally has no Path: reference cards must not promise detail pages
// that do not yet exist in PostgreSQL.
type interiorReferenceProjectPreviewData struct {
	// Title is the project name supplied by the approved page concept.
	Title string
	// Typology is the short category displayed below Title.
	Typology string
	// ImagePath is an application-owned path under /static/images.
	ImagePath string
	// Width and Height reserve the optimized asset's intrinsic dimensions.
	Width  int
	Height int
	// AltText describes the interior photograph without repeating its title.
	AltText string
}

// interiorProjectPreviewData is the public presentation shape for one
// published Interior Design project.
//
// It receives complete trusted HTML and cover paths rather than exposing the
// database identity, slug, ordering key, or private publication state.
type interiorProjectPreviewData struct {
	// Number is the consecutive zero-padded published portfolio sequence.
	Number string
	// Title is the reviewed project heading displayed by the preview article.
	Title string
	// Typology identifies the broad Interior project category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// YearLabel is empty when the nullable project year is not supplied.
	YearLabel string
	// ProjectStatus is the required real-world project state, such as Completed.
	ProjectStatus string
	// Path is the real server-rendered project detail URL used by the card link.
	Path string
	// Cover contains reviewed responsive image data, or nil for the honest
	// decorative fallback.
	Cover *publicInteriorProjectCoverPageData
}

// publicInteriorProjectCoverPageData is the binary-free image contract shared
// by the public Interior listing and detail templates. Its public prefix keeps
// it distinct from the protected administrator preview model.
type publicInteriorProjectCoverPageData struct {
	// Path is the exact current public cover revision URL.
	Path string
	// Width reserves horizontal space from the reviewed pixel dimensions.
	Width int
	// Height reserves vertical space from the reviewed pixel dimensions.
	Height int
	// AltText is the required meaningful image alternative.
	AltText string
	// Caption is optional visible reviewed copy.
	Caption string
}

// interiorProjectDetailData is the complete public view model needed by one
// published Interior Design project detail page.
//
// Internal ID, slug, lifecycle, sort order, version, and timestamps stay behind
// the mapping boundary. Galleries and client data remain deferred.
type interiorProjectDetailData struct {
	// Number is the portfolio sequence displayed as editorial context.
	Number string
	// Title is the detail page's one primary heading.
	Title string
	// Typology identifies the broad interior category in the visible facts list.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// YearLabel is empty when no reviewed project year exists.
	YearLabel string
	// ProjectStatus is required real-world project-state copy.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// Cover contains one reviewed image, or nil for the honest fallback field.
	Cover *publicInteriorProjectCoverPageData
}

// architectureProjectListingData describes the Architecture Design portfolio
// section.
//
// Only /architecture-design receives this optional view model. Keeping section
// copy beside its ordered published projection prevents the template from
// depending on database identity, lifecycle, or ordering columns.
type architectureProjectListingData struct {
	// Eyebrow is the short interface label displayed above the section heading.
	Eyebrow string
	// Heading names the project index and labels the shared work section.
	Heading string
	// Introduction is fixed explanatory copy above the published project index.
	Introduction string
	// EmptyMessage is shown when Items is nil or empty.
	EmptyMessage string
	// Items contains published previews in their database portfolio order.
	Items []architectureProjectPreviewData
}

// architectureProjectPreviewData is the public presentation shape for one
// published Architecture Design project.
//
// It receives complete trusted HTML and cover paths rather than exposing the
// database identity, slug, ordering key, or private publication state.
type architectureProjectPreviewData struct {
	// Number is the consecutive zero-padded published portfolio sequence.
	Number string
	// Title is the reviewed project heading displayed by the preview article.
	Title string
	// Typology identifies the broad Architecture project category.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// YearLabel is empty when the nullable project year is not supplied.
	YearLabel string
	// ProjectStatus is the required real-world project state, such as Completed.
	ProjectStatus string
	// Path is the real server-rendered project detail URL used by the card link.
	Path string
	// Cover contains reviewed responsive image data, or nil for the honest
	// decorative architectural fallback.
	Cover *publicArchitectureProjectCoverPageData
}

// architectureProjectDetailData is the complete view model needed by one
// published Architecture Design project detail page.
//
// Internal ID, slug, lifecycle, sort order, revision, and timestamps stay
// behind the mapping boundary. Galleries and client data remain deferred.
type architectureProjectDetailData struct {
	// Number is the portfolio sequence displayed as editorial context.
	Number string
	// Title is the detail page's one primary heading.
	Title string
	// Typology identifies the broad architecture category in the visible facts.
	Typology string
	// Location is optional reviewed geographic copy.
	Location string
	// YearLabel is empty when no reviewed project year exists.
	YearLabel string
	// ProjectStatus is required real-world project-state copy.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// Cover contains one reviewed image, or nil for the honest fallback field.
	Cover *publicArchitectureProjectCoverPageData
}

// publicArchitectureProjectCoverPageData is the binary-free image contract
// shared by the public Architecture listing and detail templates. The public
// prefix distinguishes it from the protected administrator preview model.
type publicArchitectureProjectCoverPageData struct {
	// Path is the exact current public cover revision URL.
	Path string
	// Width reserves horizontal space from the reviewed pixel dimensions.
	Width int
	// Height reserves vertical space from the reviewed pixel dimensions.
	Height int
	// AltText is the required meaningful image alternative.
	AltText string
	// Caption is optional visible reviewed copy.
	Caption string
}

// contactPageData is the complete presentation contract for the public Contact
// page and its persisted-submission states.
//
// The page receives display-ready inquiry state and a separately mapped optional
// information view rather than either database row. Templates therefore cannot
// acquire PostgreSQL authority or expose revisions and internal identities.
type contactPageData struct {
	// Eyebrow is the short interface label displayed above the primary heading.
	Eyebrow string
	// Heading is the Contact page's one primary heading.
	Heading string
	// Introduction explains what information the structural form requests.
	Introduction string
	// Information contains optional direct studio details, or nil while the
	// inquiry form remains the only truthful contact channel.
	Information *contactInformationData
	// AvailabilityNotice explains exactly what submitting the form does and does
	// not promise email delivery or a response time.
	AvailabilityNotice string
	// CSRFToken is the unpredictable request token emitted in the hidden form
	// field and compared with the request's protected cookie on POST.
	CSRFToken string
	// SubmissionToken is a separate, single-form idempotency key. Reusing the
	// CSRF token would make different tabs share one database identity, so this
	// value has its own hidden field and lifecycle.
	SubmissionToken string
	// Form contains normalized values for the form controls. On a validation
	// response it lets visitors correct one field without retyping the others.
	Form inquiryFormData
	// Errors contains optional, field-specific validation messages.
	Errors inquiryFormErrors
	// HasErrors lets the template render one accessible summary only when at
	// least one validation message exists.
	HasErrors bool
	// SubmissionSucceeded is true only on the redirected GET reached after the
	// repository confirms either a new insert or an idempotent replay.
	SubmissionSucceeded bool
	// SubmissionFailed tells the template to render one generic, accessible
	// storage failure without exposing a driver error or database configuration.
	SubmissionFailed bool
	// SubmissionConflict tells the template that the submitted key belongs to
	// different data and that the rendered form now carries a fresh key.
	SubmissionConflict bool
	// DisciplineOptions is the trusted ordered set rendered by the radio group.
	DisciplineOptions []inquiryDisciplineOptionData
	// NameMaxLength supplies the browser's maxlength hint; Go remains the
	// authoritative boundary for normalized Unicode text.
	NameMaxLength int
	// EmailMaxLength supplies the browser's maxlength hint before Go validates
	// the normalized address syntax and length.
	EmailMaxLength int
	// MessageMaxLength supplies the browser's maxlength hint; Go remains the
	// authoritative boundary for normalized Unicode text.
	MessageMaxLength int
}

// contactInformationData contains display-ready optional studio details. The
// application derives the URI address component while the template owns the
// literal schemes, so no trusted-HTML or trusted-URL bypass is needed.
type contactInformationData struct {
	// Email is the normalized public mailbox used as visible text.
	Email string
	// EmailHref is the percent-escaped address component placed after the
	// template-owned mailto scheme. Keeping it separate prevents mailbox
	// punctuation from becoming a URI query, fragment, or escape sequence.
	EmailHref string
	// PhoneDisplay is the human-readable telephone label.
	PhoneDisplay string
	// PhoneE164 is the canonical value placed after the literal tel scheme.
	PhoneE164 string
	// AddressLines are reviewed visible lines rendered inside an address element.
	AddressLines []string
}

// inquiryFormData contains only the four visitor-editable values accepted by
// the public Contact form.
//
// These fields are request data, not a persistence model. html/template escapes
// them contextually when a validation response restores them to the form.
type inquiryFormData struct {
	// Name is the normalized visitor-provided name restored after a failed POST.
	Name string
	// Email is the visitor-provided reply address validated with net/mail.
	Email string
	// Discipline is one exact machine value from the trusted option list.
	Discipline string
	// Message is the visitor's project or conversation context.
	Message string
}

// inquiryFormErrors stores one human-readable validation message per form
// field. Empty strings mean the corresponding values passed validation.
type inquiryFormErrors struct {
	// Name explains why the submitted name could not be accepted.
	Name string
	// Email explains why the submitted reply address could not be accepted.
	Email string
	// Discipline explains why no supported discipline was selected.
	Discipline string
	// Message explains why the inquiry message could not be accepted.
	Message string
}

// inquiryDisciplineOptionData pairs one accepted POST value with its visible
// choice-group label.
type inquiryDisciplineOptionData struct {
	// Value is the exact machine value accepted by server-side validation.
	Value string
	// Label is the human-readable discipline name rendered in the interface.
	Label string
}

// pageData is the common top-level value passed to every page template.
//
// A pointer is used for HomeHero because only the homepage has hero data. A nil
// pointer lets other templates omit that optional section naturally. A slice
// is used for HomeDisciplines because templates can range over any number of
// entries, while a nil or empty slice renders no discipline section. The route-
// specific pointers below follow the same optional-data pattern.
type pageData struct {
	// Title becomes the page-specific portion of the fallback browser title.
	// SEO.Title replaces it verbatim on the two Stage 24 managed routes.
	Title string
	// CurrentPath is the real canonical URL represented by the response.
	CurrentPath string
	// NavigationPath optionally identifies a parent route for active navigation.
	NavigationPath string
	// SEO contains exact managed metadata for Home and Contact. Nil preserves the
	// existing fixed metadata behavior on routes outside Stage 24.
	SEO *seoPageData
	// HomeHero contains homepage-only content, or nil for other pages.
	HomeHero *homeHeroData
	// HomeDisciplines contains the ordered homepage route entrances.
	HomeDisciplines []disciplineEntranceData
	// HomeFeatured contains zero to three published, cover-backed items in fixed
	// discipline order.
	HomeFeatured []homeFeaturedItemData
	// DisciplinePage contains shared landing data, or nil for non-discipline pages.
	DisciplinePage *disciplinePageData
	// ProductListing contains catalogue data only for the Products route.
	ProductListing *productListingData
	// ProductDetail contains one Products detail view, or nil on other routes.
	ProductDetail *productDetailData
	// InteriorProjectListing contains portfolio data only for Interior Design.
	InteriorProjectListing *interiorProjectListingData
	// InteriorProjectDetail contains one Interior detail view, or nil elsewhere.
	InteriorProjectDetail *interiorProjectDetailData
	// ArchitectureProjectListing contains only the Architecture portfolio data.
	ArchitectureProjectListing *architectureProjectListingData
	// ArchitectureProjectDetail contains one Architecture detail view, or nil.
	ArchitectureProjectDetail *architectureProjectDetailData
	// Contact contains the Contact form and its current response state, or nil.
	Contact *contactPageData
}

// newApplication creates a ready-to-serve application and its shared template
// cache.
//
// It returns an error instead of terminating the process so the caller decides
// how initialization failures should be handled. This also makes construction
// reusable from both main and tests.
func newApplication(
	products productCatalogueReader,
	interiorProjects interiorProjectCatalogueReader,
	architectureProjects architectureProjectCatalogueReader,
	siteContent siteContentReader,
	inquiries inquiryRepository,
	admins adminRepository,
	adminProducts adminProductReader,
	adminProductWrites adminProductWriter,
	adminInteriorProjects adminInteriorProjectReader,
	adminInteriorProjectWrites adminInteriorProjectWriter,
	adminArchitectureProjects adminArchitectureProjectReader,
	adminArchitectureProjectWrites adminArchitectureProjectWriter,
	adminSiteContent adminSiteContentReader,
	adminSiteContentWrites adminSiteContentWriter,
	adminInquiries adminInquiryReader,
	adminInquiryStatuses adminInquiryStatusUpdater,
	passwords adminPasswordManager,
) (*application, error) {
	if products == nil {
		return nil, errProductCatalogueReaderRequired
	}
	if interiorProjects == nil {
		return nil, errInteriorProjectCatalogueReaderRequired
	}
	if architectureProjects == nil {
		return nil, errArchitectureProjectCatalogueReaderRequired
	}
	if siteContent == nil {
		return nil, errSiteContentReaderRequired
	}
	if inquiries == nil {
		return nil, errInquiryRepositoryRequired
	}
	if admins == nil {
		return nil, errAdminRepositoryRequired
	}
	if adminProducts == nil {
		return nil, errAdminProductReaderRequired
	}
	if adminProductWrites == nil {
		return nil, errAdminProductWriterRequired
	}
	if adminInteriorProjects == nil {
		return nil, errAdminInteriorProjectReaderRequired
	}
	if adminInteriorProjectWrites == nil {
		return nil, errAdminInteriorProjectWriterRequired
	}
	if adminArchitectureProjects == nil {
		return nil, errAdminArchitectureProjectReaderRequired
	}
	if adminArchitectureProjectWrites == nil {
		return nil, errAdminArchitectureProjectWriterRequired
	}
	if adminSiteContent == nil {
		return nil, errAdminSiteContentReaderRequired
	}
	if adminSiteContentWrites == nil {
		return nil, errAdminSiteContentWriterRequired
	}
	if adminInquiries == nil {
		return nil, errAdminInquiryReaderRequired
	}
	if adminInquiryStatuses == nil {
		return nil, errAdminInquiryStatusUpdaterRequired
	}
	if passwords == nil {
		return nil, errAdminPasswordManagerRequired
	}

	templateCache, err := newTemplateCache()
	if err != nil {
		return nil, err
	}

	// A fresh process-local signing key authenticates the short-lived success
	// cookie used by the Contact Post/Redirect/Get flow. A random-source failure
	// is a startup failure because rendering an unverifiable success claim would
	// be less truthful than refusing to start.
	inquirySuccess, err := newInquirySuccessFlash()
	if err != nil {
		return nil, fmt.Errorf(
			"create inquiry success key: %w",
			err,
		)
	}

	// Unknown addresses use a real encoded verifier so their login path performs
	// the same password derivation as a stored account. The fixed plaintext is
	// never an account credential, and the password manager still salts its hash.
	adminDummyPasswordHash, err := passwords.Hash(
		adminDummyPassword,
	)
	if err != nil || !isValidAdminPasswordHash(adminDummyPasswordHash) {
		// Do not wrap an injected implementation's text: a future dependency
		// could accidentally include its plaintext input in that error.
		return nil, errAdminDummyPasswordHashFailed
	}
	dummyPasswordMatches, err := passwords.Verify(
		adminDummyPassword,
		adminDummyPasswordHash,
	)
	if err != nil || !dummyPasswordMatches {
		// Verifying once at startup prevents an inconsistent injected or future
		// implementation from creating a fast malformed-hash path for missing users.
		return nil, errAdminDummyPasswordHashFailed
	}

	app := &application{
		templates:                      templateCache,
		products:                       products,
		interiorProjects:               interiorProjects,
		architectureProjects:           architectureProjects,
		siteContent:                    siteContent,
		inquiries:                      inquiries,
		inquirySuccess:                 inquirySuccess,
		admins:                         admins,
		adminProducts:                  adminProducts,
		adminProductWrites:             adminProductWrites,
		adminInteriorProjects:          adminInteriorProjects,
		adminInteriorProjectWrites:     adminInteriorProjectWrites,
		adminArchitectureProjects:      adminArchitectureProjects,
		adminArchitectureProjectWrites: adminArchitectureProjectWrites,
		adminSiteContent:               adminSiteContent,
		adminSiteContentWrites:         adminSiteContentWrites,
		adminInquiries:                 adminInquiries,
		adminInquiryStatuses:           adminInquiryStatuses,
		adminPasswords:                 passwords,
		adminDummyPasswordHash:         adminDummyPasswordHash,
		adminLoginPasswordWork: make(
			chan struct{},
			adminLoginPasswordWorkLimit,
		),
		adminEntropy: rand.Reader,
		now:          time.Now,
	}

	return app, nil
}

// newTemplateCache parses every public page with the shared public layout and
// partials, then parses each private page with the isolated admin layout.
//
// Parsing pages into separate sets prevents identically named blocks such as
// "content" from overwriting one another. The returned error wraps filesystem
// and parsing failures with context while preserving the original cause.
func newTemplateCache() (
	map[string]*template.Template,
	error,
) {
	cache := make(map[string]*template.Template)

	// Glob discovers page files without requiring a second hard-coded list that
	// could drift out of sync as pages are added.
	pages, err := filepath.Glob(
		"./templates/pages/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find page templates: %w",
			err,
		)
	}

	// An empty match is not an error for filepath.Glob, but it would leave the
	// application unable to render anything, so treat it as startup failure.
	if len(pages) == 0 {
		return nil, fmt.Errorf(
			"no page templates found",
		)
	}

	// Partials contain reusable named templates that page files may compose.
	// Glob returns paths in lexical order, giving every page a deterministic
	// parse order without maintaining another hard-coded file list.
	partials, err := filepath.Glob(
		"./templates/partials/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find partial templates: %w",
			err,
		)
	}

	// Stage 5 uses shared template definitions. An entirely missing partial
	// directory is always a startup configuration error; exact required names
	// are verified after each complete template set is parsed below.
	if len(partials) == 0 {
		return nil, fmt.Errorf(
			"no partial templates found",
		)
	}

	// These names form the contract between the three thin page wrappers and
	// the shared discipline partial. Lookup below protects that exact contract
	// even if another unrelated partial file still exists.
	requiredPartialTemplates := []string{
		"disciplinePageStyles",
		"disciplinePageContent",
		"siteReferenceMenu",
	}

	for _, page := range pages {
		// Base returns a platform-safe filename for use as the cache key.
		pageName := filepath.Base(page)

		// Build a fresh slice for each page so appending its path cannot reuse
		// and overwrite a backing array shared with the next iteration. ParseFiles
		// accepts the slice with "...", which expands it into variadic arguments.
		files := make(
			[]string,
			0,
			len(partials)+2,
		)
		files = append(
			files,
			"./templates/base.html",
		)
		files = append(files, partials...)
		files = append(files, page)

		templateSet, err := template.ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf(
				"parse template %s: %w",
				pageName,
				err,
			)
		}

		// ParseFiles can successfully parse a set that does not define a name
		// referenced only at execution time. Explicit Lookup checks convert that
		// delayed response failure into an actionable application-startup error.
		for _, templateName := range requiredPartialTemplates {
			if templateSet.Lookup(templateName) == nil {
				return nil, fmt.Errorf(
					"page template %s requires %s",
					pageName,
					templateName,
				)
			}
		}

		cache[pageName] = templateSet
	}

	// Admin pages use a separate document shell and directory so private tools
	// cannot inherit the public navigation, scripts, or same-named content blocks.
	adminPages, err := filepath.Glob(
		"./templates/admin/pages/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find admin page templates: %w",
			err,
		)
	}
	if len(adminPages) == 0 {
		return nil, fmt.Errorf(
			"no admin page templates found",
		)
	}

	for _, page := range adminPages {
		pageName := filepath.Base(page)
		templateSet, err := template.ParseFiles(
			"./templates/admin/base.html",
			page,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"parse admin template %s: %w",
				pageName,
				err,
			)
		}
		if templateSet.Lookup("admin-content") == nil {
			return nil, fmt.Errorf(
				"admin page template %s requires admin-content",
				pageName,
			)
		}

		// The namespace prevents a future public page with the same basename from
		// silently replacing an authenticated template in the shared cache map.
		cache["admin/"+pageName] = templateSet
	}

	return cache, nil
}

// render executes one cached page through the shared "base" template and writes
// the completed HTML response.
//
// status is the desired HTTP status code, pageName selects the cached template
// set, and data is the view model exposed to template actions. Rendering into a
// buffer first is important: if template execution fails, the handler can still
// send a clean 500 response instead of a partially written HTML document.
func (app *application) render(
	w http.ResponseWriter,
	status int,
	pageName string,
	data pageData,
) {
	app.renderTemplate(
		w,
		status,
		pageName,
		"base",
		data,
	)
}

// renderAdmin executes one cached private page through the isolated
// "admin-base" layout.
//
// Keeping a typed wrapper prevents public handlers from accidentally selecting
// the admin shell and prevents admin handlers from passing public pageData,
// while the shared buffered renderer retains identical error behavior.
func (app *application) renderAdmin(
	w http.ResponseWriter,
	status int,
	pageName string,
	data adminPageData,
) {
	app.renderTemplate(
		w,
		status,
		"admin/"+pageName,
		"admin-base",
		data,
	)
}

// renderTemplate performs the common buffered execution and HTTP write for a
// public or admin template set.
//
// data remains any only at this lowest shared boundary; render and renderAdmin
// preserve the two concrete view-model contracts for all handler call sites.
func (app *application) renderTemplate(
	w http.ResponseWriter,
	status int,
	pageName string,
	rootTemplateName string,
	data any,
) {
	templateSet, exists := app.templates[pageName]
	if !exists {
		// Missing templates are server configuration errors. Log the detailed
		// name for developers while returning a generic message to visitors.
		log.Printf(
			"template %q does not exist",
			pageName,
		)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	var buffer bytes.Buffer

	// ExecuteTemplate starts at the selected public or private base layout; the
	// page's definitions fill that shell without crossing cache namespaces.
	err := templateSet.ExecuteTemplate(
		&buffer,
		rootTemplateName,
		data,
	)
	if err != nil {
		// Template execution can inspect visitor or administrator-authored values.
		// Keep its concrete diagnostic out of logs and record only the trusted
		// template identity needed to locate the failing rendering path.
		log.Printf("could not execute template %q", pageName)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	// Set headers before WriteHeader because the first response write commits
	// the status and headers to the client.
	w.Header().Set(
		"Content-Type",
		"text/html; charset=utf-8",
	)

	w.WriteHeader(status)

	// At this point the status may already have reached the client, so a write
	// failure can only be logged; a second HTTP error response would be invalid.
	if _, err := buffer.WriteTo(w); err != nil {
		// Transport errors can contain remote endpoint details. The operational
		// request log supplies safe correlation without retaining that error text.
		log.Print("could not write response")
	}
}
