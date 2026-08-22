package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Protected site-content pages share one active navigation value, canonical
// paths, and UTC timestamp presentation so templates cannot drift.
const (
	// adminSiteContentNavigationPath is both the overview route and shared active
	// navigation value.
	adminSiteContentNavigationPath = "/admin/site-content"
	// adminHomepageContentPath is the protected read-only Homepage settings route.
	adminHomepageContentPath = "/admin/site-content/homepage"
	// adminHomepageContentEditPath is the canonical Homepage edit form route.
	adminHomepageContentEditPath = "/admin/site-content/homepage/edit"
	// adminHomepageHeroPath is the canonical managed-hero form and POST route.
	adminHomepageHeroPath = "/admin/site-content/homepage/hero"
	// adminContactContentPath is the protected read-only Contact settings route.
	adminContactContentPath = "/admin/site-content/contact"
	// adminContactContentEditPath is the canonical Contact edit form route.
	adminContactContentEditPath = "/admin/site-content/contact/edit"
	// adminSiteContentTimeLayout is concise display-ready UTC interface text.
	adminSiteContentTimeLayout = "02 Jan 2006, 15:04 UTC"
)

// Application construction errors make missing protected site-content
// dependencies visible before the server accepts requests.
var (
	// errAdminSiteContentReaderRequired rejects application construction without
	// the protected settings and exact-hero read dependency.
	errAdminSiteContentReaderRequired = errors.New(
		"create application: admin site content reader is required",
	)
	// errAdminSiteContentWriterRequired rejects application construction without
	// the protected singleton and hero mutation dependency.
	errAdminSiteContentWriterRequired = errors.New(
		"create application: admin site content writer is required",
	)
)

// adminSiteContentOverviewPageData contains trusted navigation to the two
// independent settings documents.
type adminSiteContentOverviewPageData struct {
	// HomepagePath opens Homepage identity, feature, SEO, and hero settings.
	HomepagePath string
	// ContactPath opens Contact copy, details, and SEO settings.
	ContactPath string
}

// adminHomepageFeaturePageData is one selected fixed-slot presentation. It may
// remain visible while unavailable so an administrator can understand and clear
// a stale editorial reference.
type adminHomepageFeaturePageData struct {
	// DisciplineLabel is trusted application-owned visible slot text.
	DisciplineLabel string
	// HasSelection distinguishes an empty fixed slot from managed record data.
	HasSelection bool
	// Reference is a stable internal administrator label.
	Reference string
	// Title and Classification are escaped managed text.
	Title          string
	Classification string
	// Slug supplies useful public-route context.
	Slug string
	// AvailabilityLabel and AvailabilityClass are trusted status presentation.
	AvailabilityLabel string
	AvailabilityClass string
	// AvailabilityMessage explains whether the record remains selectable.
	AvailabilityMessage string
}

// adminHomepageHeroPageData contains protected current-hero preview metadata.
type adminHomepageHeroPageData struct {
	// Path is the exact authenticated media revision URL.
	Path string
	// AltText is the required meaningful image alternative.
	AltText string
	// Width and Height reserve layout space without exposing arbitrary attributes.
	Width  string
	Height string
	// Version is display-ready revision context.
	Version string
}

// adminHomepageContentDetailPageData is the complete protected Homepage read
// contract. Binary bytes remain on the exact authenticated media route.
type adminHomepageContentDetailPageData struct {
	// EditPath opens the canonical current text and selection form.
	EditPath string
	// HeroManagementPath opens the separate reviewed-image workflow.
	HeroManagementPath string
	// StudioName and Descriptor are current visible Homepage copy.
	StudioName string
	Descriptor string
	// HeroSourceLabel and HeroSourceMessage explain fallback versus managed mode.
	HeroSourceLabel   string
	HeroSourceMessage string
	// Hero contains current protected preview metadata even if fallback is active.
	Hero *adminHomepageHeroPageData
	// Each always-present value corresponds to one fixed discipline slot. Its
	// HasSelection flag controls whether managed record details are displayed.
	FeaturedInterior     adminHomepageFeaturePageData
	FeaturedArchitecture adminHomepageFeaturePageData
	FeaturedProduct      adminHomepageFeaturePageData
	// SEOTitle and SEODescription show complete managed metadata.
	SEOTitle       string
	SEODescription string
	// Version is display-ready optimistic concurrency context.
	Version string
	// CreatedAt and UpdatedAt pairs support semantic time elements.
	CreatedAtISO   string
	CreatedAtLabel string
	UpdatedAtISO   string
	UpdatedAtLabel string
}

// adminContactContentDetailPageData is the complete protected Contact read
// contract with fixed navigation and display-ready timestamps.
type adminContactContentDetailPageData struct {
	// EditPath opens the canonical current Contact form.
	EditPath string
	// Managed visible and link fields remain escaped by html/template.
	Eyebrow        string
	Heading        string
	Introduction   string
	ContactEmail   string
	PhoneDisplay   string
	PhoneE164      string
	Address        string
	SEOTitle       string
	SEODescription string
	// Version is display-ready optimistic concurrency context.
	Version string
	// CreatedAt and UpdatedAt pairs support semantic time elements.
	CreatedAtISO   string
	CreatedAtLabel string
	UpdatedAtISO   string
	UpdatedAtLabel string
}

// adminSiteContentOverviewHandler renders the authenticated settings index
// without querying either singleton merely to show links.
func (app *application) adminSiteContentOverviewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminSiteContentNavigationPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Site content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.SiteContentOverview = &adminSiteContentOverviewPageData{
		HomepagePath: adminHomepageContentPath,
		ContactPath:  adminContactContentPath,
	}
	app.renderAdmin(w, http.StatusOK, "site-content.html", data)
}

// adminHomepageContentDetailHandler reads one validated singleton and renders
// its protected read-only view.
func (app *application) adminHomepageContentDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminHomepageContentPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	record, ok := app.readAdminHomepageContent(w, r)
	if !ok {
		return
	}
	detail, valid := newAdminHomepageContentDetailPageData(record)
	if !valid {
		log.Print("admin Homepage content mapping failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Homepage content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.HomepageContentDetail = &detail
	app.renderAdmin(w, http.StatusOK, "site-content-homepage-detail.html", data)
}

// adminContactContentDetailHandler reads one validated singleton and renders
// its protected read-only view.
func (app *application) adminContactContentDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminContactContentPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	record, ok := app.readAdminContactContent(w, r)
	if !ok {
		return
	}
	detail, valid := newAdminContactContentDetailPageData(record)
	if !valid {
		log.Print("admin Contact content mapping failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Contact content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.ContactContentDetail = &detail
	app.renderAdmin(w, http.StatusOK, "site-content-contact-detail.html", data)
}

// readAdminHomepageContent centralizes the bounded singleton lookup and safe
// 503 mapping shared by detail, edit, and hero workflows.
func (app *application) readAdminHomepageContent(
	w http.ResponseWriter,
	r *http.Request,
) (adminHomepageContentRecord, bool) {
	if app == nil || app.adminSiteContent == nil {
		log.Print("admin site content reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminHomepageContentRecord{}, false
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	record, err := app.adminSiteContent.ReadHomepage(ctx)
	cancel()
	if err != nil || !isValidStoredAdminHomepageContent(record) {
		log.Print("admin Homepage content read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminHomepageContentRecord{}, false
	}

	return record, true
}

// readAdminContactContent centralizes the bounded singleton lookup and safe 503
// mapping shared by Contact detail and edit workflows.
func (app *application) readAdminContactContent(
	w http.ResponseWriter,
	r *http.Request,
) (adminContactContentRecord, bool) {
	if app == nil || app.adminSiteContent == nil {
		log.Print("admin site content reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminContactContentRecord{}, false
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	record, err := app.adminSiteContent.ReadContact(ctx)
	cancel()
	if err != nil || !isValidStoredAdminContactContent(record) {
		log.Print("admin Contact content read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminContactContentRecord{}, false
	}

	return record, true
}

// newAdminHomepageContentDetailPageData converts one validated record into
// escaped copy, trusted status presentation, and exact protected paths.
func newAdminHomepageContentDetailPageData(
	record adminHomepageContentRecord,
) (adminHomepageContentDetailPageData, bool) {
	if !isValidStoredAdminHomepageContent(record) {
		return adminHomepageContentDetailPageData{}, false
	}

	createdAt := record.CreatedAt.UTC()
	updatedAt := record.UpdatedAt.UTC()
	detail := adminHomepageContentDetailPageData{
		EditPath:           adminHomepageContentEditPath,
		HeroManagementPath: adminHomepageHeroPath,
		StudioName:         record.StudioName,
		Descriptor:         record.Descriptor,
		SEOTitle:           record.SEOTitle,
		SEODescription:     record.SEODescription,
		Version:            strconv.FormatInt(record.Version, 10),
		CreatedAtISO:       createdAt.Format(time.RFC3339),
		CreatedAtLabel:     createdAt.Format(adminSiteContentTimeLayout),
		UpdatedAtISO:       updatedAt.Format(time.RFC3339),
		UpdatedAtLabel:     updatedAt.Format(adminSiteContentTimeLayout),
		HeroSourceLabel:    "Static fallback",
		HeroSourceMessage:  "The checked-in fallback photograph is shown on the public homepage.",
	}
	if record.ManagedHeroEnabled {
		detail.HeroSourceLabel = "Managed hero"
		detail.HeroSourceMessage = "The reviewed managed image is shown on the public homepage."
	}
	if record.Hero != nil {
		detail.Hero = newAdminHomepageHeroPageData(*record.Hero)
	}
	detail.FeaturedInterior = newAdminHomepageFeaturePageData(
		record.FeaturedInterior,
		"Interior Design",
		"I",
	)
	detail.FeaturedArchitecture = newAdminHomepageFeaturePageData(
		record.FeaturedArchitecture,
		"Architecture Design",
		"A",
	)
	detail.FeaturedProduct = newAdminHomepageFeaturePageData(
		record.FeaturedProduct,
		"Product",
		"P",
	)

	return detail, true
}

// newAdminContactContentDetailPageData maps one validated Contact record into
// the complete read-only presentation contract.
func newAdminContactContentDetailPageData(
	record adminContactContentRecord,
) (adminContactContentDetailPageData, bool) {
	if !isValidStoredAdminContactContent(record) {
		return adminContactContentDetailPageData{}, false
	}

	createdAt := record.CreatedAt.UTC()
	updatedAt := record.UpdatedAt.UTC()
	return adminContactContentDetailPageData{
		EditPath:       adminContactContentEditPath,
		Eyebrow:        record.Eyebrow,
		Heading:        record.Heading,
		Introduction:   record.Introduction,
		ContactEmail:   record.ContactEmail,
		PhoneDisplay:   record.PhoneDisplay,
		PhoneE164:      record.PhoneE164,
		Address:        record.Address,
		SEOTitle:       record.SEOTitle,
		SEODescription: record.SEODescription,
		Version:        strconv.FormatInt(record.Version, 10),
		CreatedAtISO:   createdAt.Format(time.RFC3339),
		CreatedAtLabel: createdAt.Format(adminSiteContentTimeLayout),
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminSiteContentTimeLayout),
	}, true
}

// newAdminHomepageFeaturePageData maps one fixed discipline slot and its
// optional stored selection. Returning an always-present value preserves the
// application-owned discipline label even when no record is selected.
func newAdminHomepageFeaturePageData(
	selection *adminHomepageFeatureSelection,
	disciplineLabel string,
	referencePrefix string,
) adminHomepageFeaturePageData {
	data := adminHomepageFeaturePageData{DisciplineLabel: disciplineLabel}
	if selection == nil {
		return data
	}

	data = adminHomepageFeaturePageData{
		DisciplineLabel:     disciplineLabel,
		HasSelection:        true,
		Reference:           referencePrefix + "-" + strconv.FormatInt(selection.ID, 10),
		Title:               selection.Title,
		Classification:      selection.Classification,
		Slug:                selection.Slug,
		AvailabilityLabel:   "Unavailable",
		AvailabilityClass:   "unavailable",
		AvailabilityMessage: "This stored selection is no longer Published with a current cover. Choose another record or clear the slot.",
	}
	if selection.Eligible {
		data.AvailabilityLabel = "Eligible"
		data.AvailabilityClass = "eligible"
		data.AvailabilityMessage = "This selection is Published and has a current reviewed cover."
	}

	return data
}

// newAdminHomepageHeroPageData constructs one exact protected preview contract.
func newAdminHomepageHeroPageData(
	metadata homepageHeroMetadata,
) *adminHomepageHeroPageData {
	return &adminHomepageHeroPageData{
		Path:    adminHomepageHeroAssetPath(metadata.Version),
		AltText: metadata.AltText,
		Width:   strconv.Itoa(metadata.Width),
		Height:  strconv.Itoa(metadata.Height),
		Version: strconv.FormatInt(metadata.Version, 10),
	}
}

// adminHomepageHeroAssetPath builds the canonical protected exact-revision URL.
func adminHomepageHeroAssetPath(version int64) string {
	return adminHomepageHeroPath + "/" + strconv.FormatInt(version, 10)
}

// isCanonicalAdminSiteContentRequest accepts one exact escaped path and rejects
// query strings so each settings representation has one canonical address.
func isCanonicalAdminSiteContentRequest(r *http.Request, path string) bool {
	return r != nil && r.URL.EscapedPath() == path &&
		!r.URL.ForceQuery && r.URL.RawQuery == ""
}

// returnAdminSiteContentCanonicalError maps a wrong escaped path to 404 and a
// query-bearing canonical path to a generic 400.
func returnAdminSiteContentCanonicalError(w http.ResponseWriter, r *http.Request) {
	if r != nil && (r.URL.ForceQuery || r.URL.RawQuery != "") {
		http.Error(w, "invalid site content request", http.StatusBadRequest)

		return
	}
	http.NotFound(w, r)
}
