package main

import (
	"errors"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
)

// Managed Homepage hero uploads use the shared reviewed-image byte cap plus a
// small allowance for multipart boundaries and three bounded text controls.
const (
	// adminHomepageHeroRequestMaximumBytes bounds the complete encoded request.
	adminHomepageHeroRequestMaximumBytes = reviewedCoverMaximumBytes + 64*1024
	// adminHomepageHeroTextPartMaximumBytes bounds each individual text part
	// before UTF-8 and character validation.
	adminHomepageHeroTextPartMaximumBytes = 4 * 1024
)

// adminHomepageHeroFormPageData is the complete protected upload-or-replace
// template contract.
type adminHomepageHeroFormPageData struct {
	// Action is the canonical multipart POST destination.
	Action string
	// CancelPath returns to Homepage detail without mutation.
	CancelPath string
	// HomepageVersion is the optimistic parent revision submitted as hidden text.
	HomepageVersion string
	// ExistingHero contains the current protected preview, or nil.
	ExistingHero *adminHomepageHeroPageData
	// Values restores visible text; browsers do not restore selected files.
	Values adminHomepageHeroFormValues
	// Errors contains fixed field-level explanations.
	Errors adminHomepageHeroFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// SubmitLabel distinguishes initial upload from replacement.
	SubmitLabel string
}

// adminHomepageHeroFormValues contains the one administrator-controlled visible
// text value retained after validation.
type adminHomepageHeroFormValues struct {
	// AltText is the meaningful required image alternative.
	AltText string
}

// adminHomepageHeroFormErrors stores safe semantic explanations for the image
// and alternative-text controls.
type adminHomepageHeroFormErrors struct {
	// Image explains missing, corrupt, unsupported, or unsafe encoded content.
	Image string
	// AltText explains the required trimmed single-line boundary.
	AltText string
}

// parsedAdminHomepageHeroForm contains exact-cardinality multipart values before
// CSRF, revision, image, or text validation.
type parsedAdminHomepageHeroForm struct {
	// CSRFToken is compared with the authenticated session secret.
	CSRFToken string
	// HomepageVersion is the server-issued optimistic revision text.
	HomepageVersion string
	// AltText is the untrusted visible alternative-text value.
	AltText string
	// Image contains one bounded encoded browser-selected file.
	Image []byte
}

// adminHomepageHeroFormHandler renders the current parent revision and optional
// protected preview without loading binary bytes into the HTML read.
func (app *application) adminHomepageHeroFormHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !isCanonicalAdminSiteContentRequest(r, adminHomepageHeroPath) {
		returnAdminSiteContentCanonicalError(w, r)

		return
	}

	record, ok := app.readAdminHomepageContent(w, r)
	if !ok {
		return
	}
	form := newAdminHomepageHeroFormPageData(
		record,
		adminHomepageHeroFormValuesFromRecord(record),
		adminHomepageHeroFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Manage Homepage hero",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.HomepageHeroForm = &form
	app.renderAdmin(w, http.StatusOK, "site-content-homepage-hero-form.html", data)
}

// adminHomepageHeroUploadHandler validates one exact multipart request and
// atomically replaces hero media while advancing its Homepage parent revision.
func (app *application) adminHomepageHeroUploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminSiteContentMutationRequestIsCanonical(w, r, adminHomepageHeroPath) {
		return
	}

	parsed, parsedOK := parseStrictAdminHomepageHeroForm(w, r)
	if !parsedOK {
		return
	}
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		parsed.CSRFToken,
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	expectedVersion, valid := parseCanonicalPositiveInt64(parsed.HomepageVersion)
	if !valid || expectedVersion == math.MaxInt64 {
		http.Error(w, "invalid Homepage revision", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminSiteContent == nil ||
		app.adminSiteContentWrites == nil {
		log.Print("admin Homepage hero dependency unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	// Resolve the current revision before decoding and re-encoding an uploaded
	// image that cannot safely apply to a stale form. The writer performs the
	// definitive atomic revision comparison after this inexpensive early check.
	record, ok := app.readAdminHomepageContent(w, r)
	if !ok {
		return
	}
	if record.Version != expectedVersion {
		app.renderAdminHomepageHeroConflict(w, requestIdentity)

		return
	}
	values := adminHomepageHeroFormValues{AltText: parsed.AltText}
	input, validationErrors := validateAdminHomepageHeroForm(parsed)
	if validationErrors != (adminHomepageHeroFormErrors{}) {
		app.renderAdminHomepageHeroFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			record,
			values,
			validationErrors,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminSiteContentWrites.UpsertHomepageHero(
		ctx,
		expectedVersion,
		input,
	)
	cancel()
	if errors.Is(err, errAdminSiteContentWriteConflict) {
		app.renderAdminHomepageHeroConflict(w, requestIdentity)

		return
	}
	if err != nil || result.HomepageVersion != expectedVersion+1 ||
		result.HeroVersion <= 0 {
		log.Print("admin Homepage hero update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, adminHomepageContentPath, http.StatusSeeOther)
}

// adminHomepageHeroAssetHandler serves one exact current revision inside the
// authenticated no-store admin shell, including while static fallback is active.
func (app *application) adminHomepageHeroAssetHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := authenticatedAdminFromContext(r.Context()); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	version, valid := parseCanonicalPositiveInt64(r.PathValue("version"))
	if !valid || r.URL.EscapedPath() != adminHomepageHeroAssetPath(version) {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Homepage hero request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminSiteContent == nil {
		log.Print("admin Homepage hero reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	asset, err := app.adminSiteContent.FindHomepageHero(ctx, version)
	cancel()
	if errors.Is(err, errAdminHomepageHeroNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || asset.Version != version || !isValidHomepageHeroAsset(asset) {
		log.Print("admin Homepage hero read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	if _, err := w.Write(asset.Content); err != nil {
		log.Print("admin Homepage hero response write failed")
	}
}

// parseStrictAdminHomepageHeroForm accepts exactly three text parts and one file
// part, each once, without unknown fields or content codings.
func parseStrictAdminHomepageHeroForm(
	w http.ResponseWriter,
	r *http.Request,
) (parsedAdminHomepageHeroForm, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	boundary := parameters["boundary"]
	if err != nil || mediaType != "multipart/form-data" || boundary == "" {
		http.Error(w, "content type must be multipart/form-data", http.StatusUnsupportedMediaType)

		return parsedAdminHomepageHeroForm{}, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, adminHomepageHeroRequestMaximumBytes)
	reader := multipart.NewReader(r.Body, boundary)
	expected := map[string]bool{
		"csrf_token": false,
		"version":    false,
		"alt_text":   false,
		"image":      false,
	}
	textValues := make(map[string]string, 3)
	var imageContent []byte
	for {
		// NextRawPart retains transfer-coding headers so the handler can reject
		// them rather than accepting an implicitly transformed security input.
		part, nextErr := reader.NextRawPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeAdminHomepageHeroMultipartError(w, nextErr)

			return parsedAdminHomepageHeroForm{}, false
		}

		name, isFile, headerOK := strictAdminHomepageHeroPartIdentity(part)
		seen, known := expected[name]
		if !headerOK || !known || seen {
			_ = part.Close()
			http.Error(w, "invalid Homepage hero form submission", http.StatusBadRequest)

			return parsedAdminHomepageHeroForm{}, false
		}
		if part.Header.Get("Content-Encoding") != "" ||
			part.Header.Get("Content-Transfer-Encoding") != "" {
			_ = part.Close()
			http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

			return parsedAdminHomepageHeroForm{}, false
		}

		limit := int64(adminHomepageHeroTextPartMaximumBytes)
		if isFile {
			limit = reviewedCoverMaximumBytes
		}
		content, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		closeErr := part.Close()
		if readErr != nil {
			writeAdminHomepageHeroMultipartError(w, readErr)

			return parsedAdminHomepageHeroForm{}, false
		}
		if closeErr != nil {
			writeAdminHomepageHeroMultipartError(w, closeErr)

			return parsedAdminHomepageHeroForm{}, false
		}
		if int64(len(content)) > limit {
			http.Error(w, "Homepage hero form submission is too large", http.StatusRequestEntityTooLarge)

			return parsedAdminHomepageHeroForm{}, false
		}

		expected[name] = true
		if isFile {
			imageContent = append([]byte(nil), content...)
		} else {
			textValues[name] = string(content)
		}
	}
	for _, present := range expected {
		if !present {
			http.Error(w, "invalid Homepage hero form submission", http.StatusBadRequest)

			return parsedAdminHomepageHeroForm{}, false
		}
	}

	return parsedAdminHomepageHeroForm{
		CSRFToken:       textValues["csrf_token"],
		HomepageVersion: textValues["version"],
		AltText:         textValues["alt_text"],
		Image:           imageContent,
	}, true
}

// strictAdminHomepageHeroPartIdentity validates Content-Disposition and reports
// whether the named part uses the one permitted file-control shape.
func strictAdminHomepageHeroPartIdentity(
	part *multipart.Part,
) (string, bool, bool) {
	if part == nil {
		return "", false, false
	}
	mediaType, parameters, err := mime.ParseMediaType(
		part.Header.Get("Content-Disposition"),
	)
	if err != nil || mediaType != "form-data" {
		return "", false, false
	}
	name, hasName := parameters["name"]
	_, hasFilename := parameters["filename"]
	if !hasName || name == "" {
		return "", false, false
	}
	if name == "image" {
		return name, true, hasFilename
	}
	if hasFilename {
		return "", false, false
	}

	return name, false, true
}

// writeAdminHomepageHeroMultipartError maps a body-cap failure to 413 and
// malformed multipart syntax to a generic 400 without reflecting details.
func writeAdminHomepageHeroMultipartError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, "invalid Homepage hero form submission", status)
}

// validateAdminHomepageHeroForm normalizes decoded pixels and converts them into
// the narrow writer input while keeping field errors independent and safe.
func validateAdminHomepageHeroForm(
	parsed parsedAdminHomepageHeroForm,
) (adminHomepageHeroWriteInput, adminHomepageHeroFormErrors) {
	var validationErrors adminHomepageHeroFormErrors
	if !isValidRequiredReviewedCoverText(
		parsed.AltText,
		reviewedCoverAltTextMaximumLength,
	) {
		validationErrors.AltText = "Enter trimmed alternative text between 1 and 300 characters."
	}

	normalizedContent, inspection, err := normalizeReviewedCover(parsed.Image)
	if err != nil {
		validationErrors.Image = "Choose a complete JPEG or PNG up to 8 MiB and 25 megapixels."
	}
	if validationErrors != (adminHomepageHeroFormErrors{}) {
		return adminHomepageHeroWriteInput{}, validationErrors
	}

	return adminHomepageHeroWriteInput{
		ContentType: inspection.ContentType,
		Content:     normalizedContent,
		ByteSize:    len(normalizedContent),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     parsed.AltText,
	}, adminHomepageHeroFormErrors{}
}

// adminHomepageHeroFormValuesFromRecord pre-fills reviewed alt text when a hero
// exists and otherwise returns an empty form.
func adminHomepageHeroFormValuesFromRecord(
	record adminHomepageContentRecord,
) adminHomepageHeroFormValues {
	if record.Hero == nil {
		return adminHomepageHeroFormValues{}
	}

	return adminHomepageHeroFormValues{AltText: record.Hero.AltText}
}

// newAdminHomepageHeroFormPageData derives trusted paths, labels, and protected
// preview metadata outside the template.
func newAdminHomepageHeroFormPageData(
	record adminHomepageContentRecord,
	values adminHomepageHeroFormValues,
	validationErrors adminHomepageHeroFormErrors,
) adminHomepageHeroFormPageData {
	form := adminHomepageHeroFormPageData{
		Action:          adminHomepageHeroPath,
		CancelPath:      adminHomepageContentPath,
		HomepageVersion: strconv.FormatInt(record.Version, 10),
		Values:          values,
		Errors:          validationErrors,
		HasErrors:       validationErrors != (adminHomepageHeroFormErrors{}),
		SubmitLabel:     "Upload managed hero",
	}
	if record.Hero != nil {
		form.ExistingHero = newAdminHomepageHeroPageData(*record.Hero)
		form.SubmitLabel = "Replace managed hero"
	}

	return form
}

// renderAdminHomepageHeroFormResponse reuses one upload contract for semantic
// image and alternative-text validation errors.
func (app *application) renderAdminHomepageHeroFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	record adminHomepageContentRecord,
	values adminHomepageHeroFormValues,
	validationErrors adminHomepageHeroFormErrors,
) {
	form := newAdminHomepageHeroFormPageData(record, values, validationErrors)
	data := newAuthenticatedAdminPageData(
		"Manage Homepage hero",
		adminSiteContentNavigationPath,
		requestIdentity,
	)
	data.HomepageHeroForm = &form
	app.renderAdmin(w, status, "site-content-homepage-hero-form.html", data)
}

// renderAdminHomepageHeroConflict uses fixed recovery navigation and never
// echoes an unpersisted browser file selection.
func (app *application) renderAdminHomepageHeroConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
) {
	app.renderAdminSiteContentConflict(
		w,
		requestIdentity,
		"Homepage hero changed",
		"Another administrator changed Homepage settings after this form was opened. Open a fresh hero form and choose the file again if the replacement is still needed.",
		adminHomepageContentPath,
		adminHomepageHeroPath,
		"Open latest hero form",
	)
}
