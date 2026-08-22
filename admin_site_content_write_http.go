package main

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Site-content forms use a reviewed cap large enough for maximum-length
// four-byte UTF-8 copy after URL percent encoding, while remaining tightly
// bounded compared with media uploads.
const adminSiteContentFormMaximumBytes = 64 * 1024

// adminHomepageContentFormPageData is the complete Homepage edit contract.
type adminHomepageContentFormPageData struct {
	// Action and CancelPath are fixed protected destinations.
	Action     string
	CancelPath string
	// Version is the server-issued positive optimistic revision.
	Version string
	// Values restores stored or rejected administrator-visible fields.
	Values adminHomepageContentFormValues
	// Errors contains one safe explanation for each rejected visible control.
	Errors adminHomepageContentFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// Fixed feature options are grouped by their discipline selector.
	InteriorOptions     []adminHomepageFeatureOptionPageData
	ArchitectureOptions []adminHomepageFeatureOptionPageData
	ProductOptions      []adminHomepageFeatureOptionPageData
	// SelectNone booleans preserve an explicitly clear slot.
	InteriorSelectNone     bool
	ArchitectureSelectNone bool
	ProductSelectNone      bool
	// ManagedHeroAvailable tells the template whether managed mode can be chosen.
	ManagedHeroAvailable bool
}

// adminHomepageContentFormValues contains only visible administrator-controlled
// strings. Positive IDs remain text until strict semantic parsing succeeds.
type adminHomepageContentFormValues struct {
	// StudioName and Descriptor restore visible Homepage identity copy.
	StudioName string
	Descriptor string
	// HeroSource is exactly fallback or managed when valid.
	HeroSource string
	// Featured IDs are empty to clear or canonical decimal text.
	FeaturedInteriorProjectID     string
	FeaturedArchitectureProjectID string
	FeaturedProductID             string
	// SEOTitle and SEODescription restore complete managed metadata.
	SEOTitle       string
	SEODescription string
}

// adminHomepageContentFormErrors stores one field-level semantic explanation
// per Homepage control. Empty values mean the field passed validation.
type adminHomepageContentFormErrors struct {
	// StudioName and Descriptor explain required trimmed single-line bounds.
	StudioName string
	Descriptor string
	// HeroSource explains the closed source vocabulary or missing managed image.
	HeroSource string
	// Featured fields explain malformed or currently unavailable selections.
	FeaturedInteriorProjectID     string
	FeaturedArchitectureProjectID string
	FeaturedProductID             string
	// SEO fields explain required complete metadata bounds.
	SEOTitle       string
	SEODescription string
}

// adminHomepageFeatureOptionPageData pairs one trusted eligible identity with
// display text and its exact selected state.
type adminHomepageFeatureOptionPageData struct {
	// Value is a canonical positive decimal identity.
	Value string
	// Label combines reviewed title and route context for administrators.
	Label string
	// Selected is true only for an exact current or submitted match.
	Selected bool
	// Unavailable marks a stored selection retained only so it can be cleared.
	Unavailable bool
}

// adminContactContentFormPageData is the complete Contact edit contract.
type adminContactContentFormPageData struct {
	// Action and CancelPath are fixed protected destinations.
	Action     string
	CancelPath string
	// Version is the server-issued positive optimistic revision.
	Version string
	// Values restores stored or rejected administrator-visible fields.
	Values adminContactContentFormValues
	// Errors contains safe field explanations.
	Errors adminContactContentFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
}

// adminContactContentFormValues contains only visible Contact form strings.
type adminContactContentFormValues struct {
	// Rejected public copy and contact details remain unchanged for honest
	// correction, except browser textarea CRLF is canonicalized to LF.
	Eyebrow        string
	Heading        string
	Introduction   string
	ContactEmail   string
	PhoneDisplay   string
	PhoneE164      string
	Address        string
	SEOTitle       string
	SEODescription string
}

// adminContactContentFormErrors stores one semantic error per visible control.
type adminContactContentFormErrors struct {
	// Copy fields explain required or optional reviewed text boundaries.
	Eyebrow      string
	Heading      string
	Introduction string
	// Contact fields explain normalized email and paired phone rules.
	ContactEmail string
	PhoneDisplay string
	PhoneE164    string
	Address      string
	// SEO fields explain required complete metadata bounds.
	SEOTitle       string
	SEODescription string
}

// adminSiteContentConflictPageData provides fixed recovery navigation after a
// stale Homepage, Contact, or hero form without echoing submitted content.
type adminSiteContentConflictPageData struct {
	// Heading is trusted workflow-specific conflict text.
	Heading string
	// Guidance is fixed application copy explaining the safe next step.
	Guidance string
	// DetailPath opens the current protected record.
	DetailPath string
	// EditPath fetches a fresh form and revision.
	EditPath string
	// ActionLabel names the primary fixed recovery link.
	ActionLabel string
}

// adminHomepageContentEditHandler renders the current Homepage revision and
// eligible fixed-slot selectors without mutating either singleton.
func (app *application) adminHomepageContentEditHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminHomepageContentEditPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	record, ok := app.readAdminHomepageContent(w, r)
	if !ok {
		return
	}
	candidates, ok := app.readAdminHomepageFeatureCandidates(w, r)
	if !ok {
		return
	}
	form := newAdminHomepageContentFormPageData(
		record,
		candidates,
		adminHomepageContentFormValuesFromRecord(record),
		adminHomepageContentFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Edit Homepage content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.HomepageContentForm = &form
	app.renderAdmin(w, http.StatusOK, "site-content-homepage-form.html", data)
}

// adminHomepageContentUpdateHandler validates one exact native form and applies
// it only to the current Homepage revision.
func (app *application) adminHomepageContentUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminSiteContentMutationRequestIsCanonical(
		w,
		r,
		adminHomepageContentPath,
	) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"version",
			"studio_name",
			"descriptor",
			"hero_source",
			"featured_interior_project_id",
			"featured_architecture_project_id",
			"featured_product_id",
			"seo_title",
			"seo_description",
		},
		adminSiteContentFormMaximumBytes,
	)
	if !parsed {
		return
	}
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	expectedVersion, valid := parseCanonicalPositiveInt64(form.Get("version"))
	if !valid || expectedVersion == math.MaxInt64 {
		http.Error(w, "invalid Homepage revision", http.StatusBadRequest)

		return
	}
	// Reading the current protected projection preserves a stored unavailable
	// selector on a 422 response and rejects an already-stale form before offering
	// corrections against a different revision. The writer still rechecks the
	// version and eligibility atomically to close the later race window.
	record, ok := app.readAdminHomepageContent(w, r)
	if !ok {
		return
	}
	if record.Version != expectedVersion {
		app.renderAdminSiteContentConflict(
			w,
			requestIdentity,
			"Homepage content changed",
			"Another administrator saved Homepage settings after this form was opened. Review the current values before applying your change again.",
			adminHomepageContentPath,
			adminHomepageContentEditPath,
			"Open latest Homepage form",
		)

		return
	}
	candidates, ok := app.readAdminHomepageFeatureCandidates(w, r)
	if !ok {
		return
	}

	values := adminHomepageContentValuesFromForm(form)
	input, validationErrors := validateAdminHomepageContentFormValues(
		values,
		record,
		candidates,
	)
	if validationErrors != (adminHomepageContentFormErrors{}) {
		app.renderAdminHomepageContentFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			record,
			candidates,
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminSiteContentWrites == nil {
		log.Print("admin site content writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminSiteContentWrites.UpdateHomepage(
		ctx,
		expectedVersion,
		input,
	)
	cancel()
	switch {
	case errors.Is(err, errAdminSiteContentWriteConflict):
		app.renderAdminSiteContentConflict(
			w,
			requestIdentity,
			"Homepage content changed",
			"Another administrator saved Homepage settings after this form was opened. Review the current values before applying your change again.",
			adminHomepageContentPath,
			adminHomepageContentEditPath,
			"Open latest Homepage form",
		)

		return
	case errors.Is(err, errAdminHomepageInteriorFeatureUnavailable):
		validationErrors.FeaturedInteriorProjectID = "Choose a Published Interior project with a current cover, or clear this slot."
	case errors.Is(err, errAdminHomepageArchitectureFeatureUnavailable):
		validationErrors.FeaturedArchitectureProjectID = "Choose a Published Architecture project with a current cover, or clear this slot."
	case errors.Is(err, errAdminHomepageProductFeatureUnavailable):
		validationErrors.FeaturedProductID = "Choose a Published Product with a current cover, or clear this slot."
	case errors.Is(err, errAdminHomepageHeroRequired):
		validationErrors.HeroSource = "Upload a reviewed managed hero before selecting managed mode."
	case err != nil:
		log.Print("admin Homepage content update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}
	if validationErrors != (adminHomepageContentFormErrors{}) {
		app.renderAdminHomepageContentFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			record,
			candidates,
			values,
			validationErrors,
		)

		return
	}
	if result.Version != expectedVersion+1 {
		log.Print("admin Homepage content update result invalid")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, adminHomepageContentPath, http.StatusSeeOther)
}

// adminContactContentEditHandler renders the current Contact singleton revision.
func (app *application) adminContactContentEditHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminContactContentEditPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	record, ok := app.readAdminContactContent(w, r)
	if !ok {
		return
	}
	form := newAdminContactContentFormPageData(
		record,
		adminContactContentFormValuesFromRecord(record),
		adminContactContentFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Edit Contact content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.ContactContentForm = &form
	app.renderAdmin(w, http.StatusOK, "site-content-contact-form.html", data)
}

// adminContactContentUpdateHandler validates one exact native form and applies
// it only to the current Contact revision.
func (app *application) adminContactContentUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminSiteContentMutationRequestIsCanonical(w, r, adminContactContentPath) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"version",
			"eyebrow",
			"heading",
			"introduction",
			"contact_email",
			"phone_display",
			"phone_e164",
			"address",
			"seo_title",
			"seo_description",
		},
		adminSiteContentFormMaximumBytes,
	)
	if !parsed {
		return
	}
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	expectedVersion, valid := parseCanonicalPositiveInt64(form.Get("version"))
	if !valid || expectedVersion == math.MaxInt64 {
		http.Error(w, "invalid Contact revision", http.StatusBadRequest)

		return
	}
	values := adminContactContentValuesFromForm(form)
	input, validationErrors := validateAdminContactContentFormValues(values)
	if validationErrors != (adminContactContentFormErrors{}) {
		record := adminContactContentRecord{Version: expectedVersion}
		app.renderAdminContactContentFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			record,
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminSiteContentWrites == nil {
		log.Print("admin site content writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminSiteContentWrites.UpdateContact(
		ctx,
		expectedVersion,
		input,
	)
	cancel()
	if errors.Is(err, errAdminSiteContentWriteConflict) {
		app.renderAdminSiteContentConflict(
			w,
			requestIdentity,
			"Contact content changed",
			"Another administrator saved Contact settings after this form was opened. Review the current values before applying your change again.",
			adminContactContentPath,
			adminContactContentEditPath,
			"Open latest Contact form",
		)

		return
	}
	if err != nil || result.Version != expectedVersion+1 {
		log.Print("admin Contact content update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, adminContactContentPath, http.StatusSeeOther)
}

// adminSiteContentMutationRequestIsCanonical rejects alternate paths, queries,
// and content codings before a strict parser reads any request body.
func adminSiteContentMutationRequestIsCanonical(
	w http.ResponseWriter,
	r *http.Request,
	canonicalPath string,
) bool {
	if !isCanonicalAdminSiteContentRequest(r, canonicalPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return false
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return false
	}

	return true
}

// readAdminHomepageFeatureCandidates centralizes the bounded eligible-choice
// lookup and safe dependency-failure mapping.
func (app *application) readAdminHomepageFeatureCandidates(
	w http.ResponseWriter,
	r *http.Request,
) ([]adminHomepageFeatureCandidate, bool) {
	if app == nil || app.adminSiteContent == nil {
		log.Print("admin site content reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return nil, false
	}
	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	candidates, err := app.adminSiteContent.ListFeatureCandidates(ctx)
	cancel()
	if err != nil || !isValidAdminHomepageFeatureCandidateList(candidates) {
		log.Print("admin Homepage feature candidate read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return nil, false
	}

	return candidates, true
}

// adminHomepageContentValuesFromForm copies visible controls from an already
// cardinality-checked form.
func adminHomepageContentValuesFromForm(
	form mapFormValues,
) adminHomepageContentFormValues {
	return adminHomepageContentFormValues{
		StudioName:                    form.Get("studio_name"),
		Descriptor:                    form.Get("descriptor"),
		HeroSource:                    form.Get("hero_source"),
		FeaturedInteriorProjectID:     form.Get("featured_interior_project_id"),
		FeaturedArchitectureProjectID: form.Get("featured_architecture_project_id"),
		FeaturedProductID:             form.Get("featured_product_id"),
		SEOTitle:                      form.Get("seo_title"),
		SEODescription:                form.Get("seo_description"),
	}
}

// adminContactContentValuesFromForm copies visible controls from an already
// cardinality-checked form.
func adminContactContentValuesFromForm(
	form mapFormValues,
) adminContactContentFormValues {
	return adminContactContentFormValues{
		Eyebrow:        form.Get("eyebrow"),
		Heading:        form.Get("heading"),
		Introduction:   normalizeAdminTextareaLineEndings(form.Get("introduction")),
		ContactEmail:   form.Get("contact_email"),
		PhoneDisplay:   form.Get("phone_display"),
		PhoneE164:      form.Get("phone_e164"),
		Address:        normalizeAdminTextareaLineEndings(form.Get("address")),
		SEOTitle:       form.Get("seo_title"),
		SEODescription: form.Get("seo_description"),
	}
}

// normalizeAdminTextareaLineEndings converts the CRLF sequences browsers use
// for submitted textarea controls to the application's canonical LF storage.
// A lone carriage return is deliberately preserved so validation rejects that
// malformed control character instead of silently changing untrusted copy.
func normalizeAdminTextareaLineEndings(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

// validateAdminHomepageContentFormValues converts visible strings into the
// narrow writer input without silently normalizing rejected content.
func validateAdminHomepageContentFormValues(
	values adminHomepageContentFormValues,
	record adminHomepageContentRecord,
	candidates []adminHomepageFeatureCandidate,
) (adminHomepageContentWriteInput, adminHomepageContentFormErrors) {
	var validationErrors adminHomepageContentFormErrors
	if !isValidSiteContentSingleLine(
		values.StudioName,
		homepageStudioNameMaximumLength,
	) {
		validationErrors.StudioName = "Enter a trimmed studio name between 1 and 120 characters."
	}
	if !isValidSiteContentSingleLine(
		values.Descriptor,
		homepageDescriptorMaximumLength,
	) {
		validationErrors.Descriptor = "Enter a trimmed descriptor between 1 and 160 characters."
	}
	managedHeroEnabled := false
	switch values.HeroSource {
	case "fallback":
	case "managed":
		managedHeroEnabled = true
		if record.Hero == nil {
			validationErrors.HeroSource = "Upload a reviewed managed hero before selecting managed mode."
		}
	default:
		validationErrors.HeroSource = "Choose the static fallback or managed hero."
	}

	interiorID, valid := parseOptionalAdminHomepageFeatureID(
		values.FeaturedInteriorProjectID,
	)
	if !valid || (interiorID > 0 && !adminHomepageCandidateExists(
		candidates,
		homepageFeatureInterior,
		interiorID,
	)) {
		validationErrors.FeaturedInteriorProjectID = "Choose a Published Interior project with a current cover, or clear this slot."
	}
	architectureID, valid := parseOptionalAdminHomepageFeatureID(
		values.FeaturedArchitectureProjectID,
	)
	if !valid || (architectureID > 0 && !adminHomepageCandidateExists(
		candidates,
		homepageFeatureArchitecture,
		architectureID,
	)) {
		validationErrors.FeaturedArchitectureProjectID = "Choose a Published Architecture project with a current cover, or clear this slot."
	}
	productID, valid := parseOptionalAdminHomepageFeatureID(
		values.FeaturedProductID,
	)
	if !valid || (productID > 0 && !adminHomepageCandidateExists(
		candidates,
		homepageFeatureProduct,
		productID,
	)) {
		validationErrors.FeaturedProductID = "Choose a Published Product with a current cover, or clear this slot."
	}
	if !isValidSiteContentSingleLine(values.SEOTitle, siteSEOTitleMaximumLength) {
		validationErrors.SEOTitle = "Enter a trimmed complete SEO title between 1 and 160 characters."
	}
	if !isValidSiteContentSingleLine(
		values.SEODescription,
		siteSEODescriptionMaximumLength,
	) {
		validationErrors.SEODescription = "Enter a trimmed single-line SEO description between 1 and 320 characters."
	}

	if validationErrors != (adminHomepageContentFormErrors{}) {
		return adminHomepageContentWriteInput{}, validationErrors
	}
	return adminHomepageContentWriteInput{
		StudioName:                    values.StudioName,
		Descriptor:                    values.Descriptor,
		ManagedHeroEnabled:            managedHeroEnabled,
		FeaturedInteriorProjectID:     interiorID,
		FeaturedArchitectureProjectID: architectureID,
		FeaturedProductID:             productID,
		SEOTitle:                      values.SEOTitle,
		SEODescription:                values.SEODescription,
	}, adminHomepageContentFormErrors{}
}

// validateAdminContactContentFormValues converts visible strings into the
// narrow writer input while retaining invalid values for honest correction.
func validateAdminContactContentFormValues(
	values adminContactContentFormValues,
) (adminContactContentWriteInput, adminContactContentFormErrors) {
	var validationErrors adminContactContentFormErrors
	if !isValidSiteContentSingleLine(values.Eyebrow, contactEyebrowMaximumLength) {
		validationErrors.Eyebrow = "Enter a trimmed eyebrow between 1 and 80 characters."
	}
	if !isValidSiteContentSingleLine(values.Heading, contactHeadingMaximumLength) {
		validationErrors.Heading = "Enter a trimmed heading between 1 and 160 characters."
	}
	if !isValidSiteContentMultiline(
		values.Introduction,
		contactIntroductionMaximumLength,
		true,
	) {
		validationErrors.Introduction = "Enter trimmed introduction copy between 1 and 1200 characters."
	}
	if !isValidPublicContactEmail(values.ContactEmail) {
		validationErrors.ContactEmail = "Enter one lowercase email address with a dotted domain, or leave it empty."
	}
	if !isValidPublicContactPhone(values.PhoneDisplay, values.PhoneE164) {
		message := "Provide both a display phone and an E.164 value such as +442071234567, or leave both empty."
		validationErrors.PhoneDisplay = message
		validationErrors.PhoneE164 = message
	}
	if !isValidSiteContentMultiline(
		values.Address,
		contactAddressMaximumLength,
		false,
	) {
		validationErrors.Address = "Use at most 500 trimmed characters and supported line breaks."
	}
	if !isValidSiteContentSingleLine(values.SEOTitle, siteSEOTitleMaximumLength) {
		validationErrors.SEOTitle = "Enter a trimmed complete SEO title between 1 and 160 characters."
	}
	if !isValidSiteContentSingleLine(
		values.SEODescription,
		siteSEODescriptionMaximumLength,
	) {
		validationErrors.SEODescription = "Enter a trimmed single-line SEO description between 1 and 320 characters."
	}

	if validationErrors != (adminContactContentFormErrors{}) {
		return adminContactContentWriteInput{}, validationErrors
	}
	return adminContactContentWriteInput{
		Eyebrow:        values.Eyebrow,
		Heading:        values.Heading,
		Introduction:   values.Introduction,
		ContactEmail:   values.ContactEmail,
		PhoneDisplay:   values.PhoneDisplay,
		PhoneE164:      values.PhoneE164,
		Address:        values.Address,
		SEOTitle:       values.SEOTitle,
		SEODescription: values.SEODescription,
	}, adminContactContentFormErrors{}
}

// parseOptionalAdminHomepageFeatureID maps the exact empty control to zero and
// accepts only canonical positive decimal identities otherwise.
func parseOptionalAdminHomepageFeatureID(value string) (int64, bool) {
	if value == "" {
		return 0, true
	}

	return parseCanonicalPositiveInt64(value)
}

// adminHomepageCandidateExists proves one submitted identity belongs to the
// expected fixed slot in the freshly read eligible candidate set.
func adminHomepageCandidateExists(
	candidates []adminHomepageFeatureCandidate,
	discipline homepageFeatureDiscipline,
	id int64,
) bool {
	for _, candidate := range candidates {
		if candidate.Discipline == discipline && candidate.ID == id {
			return true
		}
	}

	return false
}

// isValidAdminHomepageFeatureCandidateList verifies every candidate and strict
// database ordering at the handler-facing interface boundary.
func isValidAdminHomepageFeatureCandidateList(
	candidates []adminHomepageFeatureCandidate,
) bool {
	seen := make(map[[2]int64]struct{}, len(candidates))
	var previous adminHomepageFeatureCandidate
	for index, candidate := range candidates {
		if !isValidAdminHomepageFeatureCandidate(candidate) ||
			(index > 0 && !adminHomepageFeatureCandidateFollows(candidate, previous)) {
			return false
		}
		key := [2]int64{int64(candidate.Discipline), candidate.ID}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		previous = candidate
	}

	return true
}

// adminHomepageContentFormValuesFromRecord copies the current protected fields
// into visible form strings.
func adminHomepageContentFormValuesFromRecord(
	record adminHomepageContentRecord,
) adminHomepageContentFormValues {
	values := adminHomepageContentFormValues{
		StudioName:     record.StudioName,
		Descriptor:     record.Descriptor,
		HeroSource:     "fallback",
		SEOTitle:       record.SEOTitle,
		SEODescription: record.SEODescription,
	}
	if record.ManagedHeroEnabled {
		values.HeroSource = "managed"
	}
	if record.FeaturedInterior != nil {
		values.FeaturedInteriorProjectID = strconv.FormatInt(record.FeaturedInterior.ID, 10)
	}
	if record.FeaturedArchitecture != nil {
		values.FeaturedArchitectureProjectID = strconv.FormatInt(record.FeaturedArchitecture.ID, 10)
	}
	if record.FeaturedProduct != nil {
		values.FeaturedProductID = strconv.FormatInt(record.FeaturedProduct.ID, 10)
	}

	return values
}

// adminContactContentFormValuesFromRecord copies current Contact fields into
// visible form strings.
func adminContactContentFormValuesFromRecord(
	record adminContactContentRecord,
) adminContactContentFormValues {
	return adminContactContentFormValues{
		Eyebrow:        record.Eyebrow,
		Heading:        record.Heading,
		Introduction:   record.Introduction,
		ContactEmail:   record.ContactEmail,
		PhoneDisplay:   record.PhoneDisplay,
		PhoneE164:      record.PhoneE164,
		Address:        record.Address,
		SEOTitle:       record.SEOTitle,
		SEODescription: record.SEODescription,
	}
}

// newAdminHomepageContentFormPageData builds trusted options around escaped
// stored or submitted values. A stored unavailable selection is appended only
// to its own slot and remains selectable solely so the editor can see and clear it.
func newAdminHomepageContentFormPageData(
	record adminHomepageContentRecord,
	candidates []adminHomepageFeatureCandidate,
	values adminHomepageContentFormValues,
	validationErrors adminHomepageContentFormErrors,
) adminHomepageContentFormPageData {
	form := adminHomepageContentFormPageData{
		Action:                 adminHomepageContentPath,
		CancelPath:             adminHomepageContentPath,
		Version:                strconv.FormatInt(record.Version, 10),
		Values:                 values,
		Errors:                 validationErrors,
		HasErrors:              validationErrors != (adminHomepageContentFormErrors{}),
		InteriorSelectNone:     values.FeaturedInteriorProjectID == "",
		ArchitectureSelectNone: values.FeaturedArchitectureProjectID == "",
		ProductSelectNone:      values.FeaturedProductID == "",
		ManagedHeroAvailable:   record.Hero != nil,
	}
	for _, candidate := range candidates {
		option := adminHomepageFeatureOptionPageData{
			Value: strconv.FormatInt(candidate.ID, 10),
			Label: candidate.Title + " — /" + candidate.Slug,
		}
		switch candidate.Discipline {
		case homepageFeatureInterior:
			option.Selected = option.Value == values.FeaturedInteriorProjectID
			form.InteriorOptions = append(form.InteriorOptions, option)
		case homepageFeatureArchitecture:
			option.Selected = option.Value == values.FeaturedArchitectureProjectID
			form.ArchitectureOptions = append(form.ArchitectureOptions, option)
		case homepageFeatureProduct:
			option.Selected = option.Value == values.FeaturedProductID
			form.ProductOptions = append(form.ProductOptions, option)
		}
	}
	form.InteriorOptions = appendStoredUnavailableAdminHomepageOption(
		form.InteriorOptions,
		record.FeaturedInterior,
		values.FeaturedInteriorProjectID,
	)
	form.ArchitectureOptions = appendStoredUnavailableAdminHomepageOption(
		form.ArchitectureOptions,
		record.FeaturedArchitecture,
		values.FeaturedArchitectureProjectID,
	)
	form.ProductOptions = appendStoredUnavailableAdminHomepageOption(
		form.ProductOptions,
		record.FeaturedProduct,
		values.FeaturedProductID,
	)

	return form
}

// appendStoredUnavailableAdminHomepageOption reconciles the independently read
// stored selection and candidate list by identity. Eligibility can change
// between those reads: a missing stored identity must remain visible and
// clearable, while an identity already in the current candidate list must not
// be duplicated. Arbitrary submitted IDs never create options.
func appendStoredUnavailableAdminHomepageOption(
	options []adminHomepageFeatureOptionPageData,
	selection *adminHomepageFeatureSelection,
	selectedValue string,
) []adminHomepageFeatureOptionPageData {
	if selection == nil {
		return options
	}
	value := strconv.FormatInt(selection.ID, 10)
	for _, option := range options {
		if option.Value == value {
			return options
		}
	}

	return append(options, adminHomepageFeatureOptionPageData{
		Value:       value,
		Label:       "Unavailable — " + selection.Title + " — /" + selection.Slug,
		Selected:    selectedValue == value,
		Unavailable: true,
	})
}

// newAdminContactContentFormPageData builds the Contact edit presentation.
func newAdminContactContentFormPageData(
	record adminContactContentRecord,
	values adminContactContentFormValues,
	validationErrors adminContactContentFormErrors,
) adminContactContentFormPageData {
	return adminContactContentFormPageData{
		Action:     adminContactContentPath,
		CancelPath: adminContactContentPath,
		Version:    strconv.FormatInt(record.Version, 10),
		Values:     values,
		Errors:     validationErrors,
		HasErrors:  validationErrors != (adminContactContentFormErrors{}),
	}
}

// renderAdminHomepageContentFormResponse reuses one form contract for semantic
// and transactional availability errors.
func (app *application) renderAdminHomepageContentFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	record adminHomepageContentRecord,
	candidates []adminHomepageFeatureCandidate,
	values adminHomepageContentFormValues,
	validationErrors adminHomepageContentFormErrors,
) {
	form := newAdminHomepageContentFormPageData(
		record,
		candidates,
		values,
		validationErrors,
	)
	data := newAuthenticatedAdminPageData(
		"Edit Homepage content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.HomepageContentForm = &form
	app.renderAdmin(w, status, "site-content-homepage-form.html", data)
}

// renderAdminContactContentFormResponse reuses one form contract for semantic
// Contact validation errors.
func (app *application) renderAdminContactContentFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	record adminContactContentRecord,
	values adminContactContentFormValues,
	validationErrors adminContactContentFormErrors,
) {
	form := newAdminContactContentFormPageData(record, values, validationErrors)
	data := newAuthenticatedAdminPageData(
		"Edit Contact content",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.ContactContentForm = &form
	app.renderAdmin(w, status, "site-content-contact-form.html", data)
}

// renderAdminSiteContentConflict builds a fixed non-echoing 409 page shared by
// Homepage text, Contact text, and hero replacement workflows.
func (app *application) renderAdminSiteContentConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	heading string,
	guidance string,
	detailPath string,
	editPath string,
	actionLabel string,
) {
	data := newAuthenticatedAdminPageData(
		heading,
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.SiteContentConflict = &adminSiteContentConflictPageData{
		Heading:     heading,
		Guidance:    guidance,
		DetailPath:  detailPath,
		EditPath:    editPath,
		ActionLabel: actionLabel,
	}
	app.renderAdmin(w, http.StatusConflict, "site-content-conflict.html", data)
}
