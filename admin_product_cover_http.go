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

const (
	// adminProductCoverRequestMaximumBytes leaves a small fixed allowance for
	// multipart boundaries and four text controls around the bounded image.
	adminProductCoverRequestMaximumBytes = productCoverMaximumBytes + 64*1024
	// adminProductCoverTextPartMaximumBytes rejects an abusive individual text
	// part before semantic Unicode-character validation runs.
	adminProductCoverTextPartMaximumBytes = 8 * 1024
)

// adminProductCoverFormPageData is the complete protected upload-or-replace
// template contract. It contains metadata and paths, never binary image bytes.
type adminProductCoverFormPageData struct {
	// Reference is the stable administrative Product label.
	Reference string
	// ProductName identifies the record being edited.
	ProductName string
	// Action is the one canonical multipart POST destination.
	Action string
	// CancelPath returns to Product detail without mutation.
	CancelPath string
	// ProductVersion is the optimistic revision submitted as a hidden field.
	ProductVersion string
	// ExistingCover contains the current protected preview, or nil.
	ExistingCover *productCoverPageData
	// Values restores reviewed text after semantic validation fails.
	Values adminProductCoverFormValues
	// Errors contains fixed field-level explanations.
	Errors adminProductCoverFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// SubmitLabel distinguishes the first upload from a replacement.
	SubmitLabel string
}

// adminProductCoverFormValues contains the two visible reviewed text controls.
// The browser-selected file is intentionally never echoed into HTML.
type adminProductCoverFormValues struct {
	// AltText is required meaningful alternative text.
	AltText string
	// Caption is optional visible cover copy.
	Caption string
}

// adminProductCoverFormErrors stores safe semantic explanations for the three
// visible cover controls.
type adminProductCoverFormErrors struct {
	// Image explains a missing, corrupt, unsupported, or unsafe image.
	Image string
	// AltText explains the required trimmed alternative-text boundary.
	AltText string
	// Caption explains the optional trimmed caption boundary.
	Caption string
}

// parsedAdminProductCoverForm contains exact-cardinality multipart values after
// transport parsing but before CSRF, revision, image, or text validation.
type parsedAdminProductCoverForm struct {
	// CSRFToken is compared with the authenticated session secret.
	CSRFToken string
	// ProductVersion is the server-issued optimistic revision text.
	ProductVersion string
	// AltText is the untrusted visible alternative-text value.
	AltText string
	// Caption is the untrusted visible caption value.
	Caption string
	// Image contains the bounded encoded file selected by the browser.
	Image []byte
}

// adminProductCoverPath constructs the canonical management path for one
// positive Product identity.
func adminProductCoverPath(productID int64) string {
	return adminProductPath(productID) + "/cover"
}

// adminProductCoverAssetPath adds one positive media revision to the protected
// management path.
func adminProductCoverAssetPath(productID int64, version int64) string {
	return adminProductCoverPath(productID) + "/" + strconv.FormatInt(version, 10)
}

// adminProductCoverFormHandler renders one current Product revision without
// reading or mutating binary content.
func (app *application) adminProductCoverFormHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	productID, canonicalPath, valid := canonicalAdminProductCoverPath(r, false)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product cover request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminProducts == nil {
		log.Print("admin product reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	product, ok := app.readAdminProductForCover(w, r, productID)
	if !ok {
		return
	}
	form := newAdminProductCoverFormPageData(
		product,
		canonicalPath,
		adminProductCoverFormValuesFromRecord(product),
		adminProductCoverFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Manage Product cover",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductCoverForm = &form

	app.renderAdmin(w, http.StatusOK, "product-cover-form.html", data)
}

// adminProductCoverUploadHandler validates one exact multipart request and
// atomically inserts or replaces the cover only for the submitted current
// Product revision.
func (app *application) adminProductCoverUploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	productID, canonicalPath, valid := canonicalAdminProductCoverPath(r, false)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product cover request", http.StatusBadRequest)

		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return
	}

	parsed, parsedOK := parseStrictAdminProductCoverForm(w, r)
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

	expectedVersion, valid := parseCanonicalPositiveInt64(parsed.ProductVersion)
	if !valid || expectedVersion == math.MaxInt64 {
		http.Error(w, "invalid Product revision", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminProducts == nil || app.adminProductWrites == nil {
		log.Print("admin product cover dependency unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	product, ok := app.readAdminProductForCover(w, r, productID)
	if !ok {
		return
	}
	if product.Version != expectedVersion {
		app.renderAdminProductCoverConflict(
			w,
			requestIdentity,
			productID,
		)

		return
	}

	values := adminProductCoverFormValues{
		AltText: parsed.AltText,
		Caption: parsed.Caption,
	}
	input, validationErrors := validateAdminProductCoverForm(parsed)
	if validationErrors != (adminProductCoverFormErrors{}) {
		form := newAdminProductCoverFormPageData(
			product,
			canonicalPath,
			values,
			validationErrors,
		)
		data := newAuthenticatedAdminPageData(
			"Manage Product cover",
			adminProductNavigationPath,
			requestIdentity,
		)
		data.ProductCoverForm = &form
		app.renderAdmin(
			w,
			http.StatusUnprocessableEntity,
			"product-cover-form.html",
			data,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminProductWrites.UpsertCover(
		ctx,
		productID,
		expectedVersion,
		input,
	)
	cancel()
	switch {
	case errors.Is(err, errAdminProductNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, errAdminProductWriteConflict):
		app.renderAdminProductCoverConflict(
			w,
			requestIdentity,
			productID,
		)

		return
	case err != nil || result.ProductID != productID ||
		result.ProductVersion != expectedVersion+1 ||
		result.CoverVersion <= 0:
		log.Print("admin product cover update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, adminProductPath(productID), http.StatusSeeOther)
}

// adminProductCoverAssetHandler serves one exact cover revision inside the
// no-store authenticated shell for Draft, Published, or Archived Products.
func (app *application) adminProductCoverAssetHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := authenticatedAdminFromContext(r.Context()); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	productID, canonicalPath, valid := canonicalAdminProductCoverPath(r, true)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product cover request", http.StatusBadRequest)

		return
	}
	coverVersion, valid := parseCanonicalPositiveInt64(r.PathValue("version"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminProducts == nil {
		log.Print("admin product cover reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	asset, err := app.adminProducts.FindCoverByProductID(
		ctx,
		productID,
		coverVersion,
	)
	cancel()
	if errors.Is(err, errProductCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidProductCoverAsset(asset) ||
		asset.ProductID != productID || asset.Version != coverVersion {
		log.Print("admin product cover read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	if _, err := w.Write(asset.Content); err != nil {
		log.Print("admin product cover response write failed")
	}
}

// canonicalAdminProductCoverPath validates the decoded Product ID, optional
// revision, and exact escaped management or revision path.
func canonicalAdminProductCoverPath(
	r *http.Request,
	withVersion bool,
) (int64, string, bool) {
	productID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminProductCoverPath(productID)
	if withVersion {
		version, versionValid := parseCanonicalPositiveInt64(
			r.PathValue("version"),
		)
		if !versionValid {
			return 0, "", false
		}
		canonicalPath = adminProductCoverAssetPath(productID, version)
	}
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return productID, canonicalPath, true
}

// readAdminProductForCover centralizes the bounded protected lookup and safe
// 404/503 mapping shared by cover GET and POST forms.
func (app *application) readAdminProductForCover(
	w http.ResponseWriter,
	r *http.Request,
	productID int64,
) (adminProductRecord, bool) {
	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	product, err := app.adminProducts.FindByID(ctx, productID)
	cancel()
	if errors.Is(err, errAdminProductNotFound) {
		http.NotFound(w, r)

		return adminProductRecord{}, false
	}
	if err != nil || product.ID != productID ||
		!isValidStoredAdminProduct(product) {
		log.Print("admin product cover form read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminProductRecord{}, false
	}

	return product, true
}

// parseStrictAdminProductCoverForm accepts exactly four text parts and one file
// part, each once, with no unknown multipart fields or content codings.
func parseStrictAdminProductCoverForm(
	w http.ResponseWriter,
	r *http.Request,
) (parsedAdminProductCoverForm, bool) {
	mediaType, parameters, err := mime.ParseMediaType(
		r.Header.Get("Content-Type"),
	)
	boundary := parameters["boundary"]
	if err != nil || mediaType != "multipart/form-data" || boundary == "" {
		http.Error(w, "content type must be multipart/form-data", http.StatusUnsupportedMediaType)

		return parsedAdminProductCoverForm{}, false
	}

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		adminProductCoverRequestMaximumBytes,
	)
	reader := multipart.NewReader(r.Body, boundary)
	expected := map[string]bool{
		"csrf_token": false,
		"version":    false,
		"alt_text":   false,
		"caption":    false,
		"image":      false,
	}
	textValues := make(map[string]string, 4)
	var imageContent []byte

	for {
		// NextRawPart preserves Content-Transfer-Encoding so this boundary can
		// reject it explicitly. NextPart would silently decode quoted-printable
		// input and remove the header before validation.
		part, nextErr := reader.NextRawPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeAdminProductCoverMultipartError(w, nextErr)

			return parsedAdminProductCoverForm{}, false
		}

		name, isFile, headerOK := strictAdminProductCoverPartIdentity(part)
		seen, known := expected[name]
		if !headerOK || !known || seen {
			_ = part.Close()
			http.Error(w, "invalid cover form submission", http.StatusBadRequest)

			return parsedAdminProductCoverForm{}, false
		}
		if part.Header.Get("Content-Encoding") != "" ||
			part.Header.Get("Content-Transfer-Encoding") != "" {
			_ = part.Close()
			http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

			return parsedAdminProductCoverForm{}, false
		}

		limit := int64(adminProductCoverTextPartMaximumBytes)
		if isFile {
			limit = productCoverMaximumBytes
		}
		content, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		closeErr := part.Close()
		if readErr != nil {
			writeAdminProductCoverMultipartError(w, readErr)

			return parsedAdminProductCoverForm{}, false
		}
		if closeErr != nil {
			// Close drains the remainder of an oversized part. Preserve a
			// MaxBytesError discovered during that drain so the total request cap
			// remains a 413 rather than being misclassified as malformed syntax.
			writeAdminProductCoverMultipartError(w, closeErr)

			return parsedAdminProductCoverForm{}, false
		}
		if int64(len(content)) > limit {
			http.Error(w, "cover form submission is too large", http.StatusRequestEntityTooLarge)

			return parsedAdminProductCoverForm{}, false
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
			http.Error(w, "invalid cover form submission", http.StatusBadRequest)

			return parsedAdminProductCoverForm{}, false
		}
	}

	return parsedAdminProductCoverForm{
		CSRFToken:      textValues["csrf_token"],
		ProductVersion: textValues["version"],
		AltText:        textValues["alt_text"],
		Caption:        textValues["caption"],
		Image:          imageContent,
	}, true
}

// strictAdminProductCoverPartIdentity validates Content-Disposition and returns
// whether the exact expected part is the single file control.
func strictAdminProductCoverPartIdentity(
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

// writeAdminProductCoverMultipartError translates a body-cap error to 413 and
// every malformed multipart stream to a generic 400 without reflecting details.
func writeAdminProductCoverMultipartError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, "invalid cover form submission", status)
}

// validateAdminProductCoverForm converts decoded parts into the narrow writer
// input while keeping each safe field error independent.
func validateAdminProductCoverForm(
	parsed parsedAdminProductCoverForm,
) (adminProductCoverWriteInput, adminProductCoverFormErrors) {
	var validationErrors adminProductCoverFormErrors
	if !isValidRequiredProductCoverText(
		parsed.AltText,
		productCoverAltTextMaximumLength,
	) {
		validationErrors.AltText = "Enter trimmed alternative text between 1 and 300 characters."
	}
	if !isValidOptionalProductCoverText(
		parsed.Caption,
		productCoverCaptionMaximumLength,
	) {
		validationErrors.Caption = "Use at most 500 characters and remove leading or trailing whitespace."
	}

	normalizedContent, inspection, err := normalizeProductCover(parsed.Image)
	if err != nil {
		validationErrors.Image = "Choose a complete JPEG or PNG up to 8 MiB and 25 megapixels."
	}
	if validationErrors != (adminProductCoverFormErrors{}) {
		return adminProductCoverWriteInput{}, validationErrors
	}

	return adminProductCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     normalizedContent,
		ByteSize:    len(normalizedContent),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     parsed.AltText,
		Caption:     parsed.Caption,
	}, adminProductCoverFormErrors{}
}

// adminProductCoverFormValuesFromRecord pre-fills reviewed text when replacing
// an existing cover and otherwise returns an empty form.
func adminProductCoverFormValuesFromRecord(
	product adminProductRecord,
) adminProductCoverFormValues {
	if product.Cover == nil {
		return adminProductCoverFormValues{}
	}

	return adminProductCoverFormValues{
		AltText: product.Cover.AltText,
		Caption: product.Cover.Caption,
	}
}

// newAdminProductCoverFormPageData derives every trusted path, label, and
// existing-preview value outside the template.
func newAdminProductCoverFormPageData(
	product adminProductRecord,
	action string,
	values adminProductCoverFormValues,
	validationErrors adminProductCoverFormErrors,
) adminProductCoverFormPageData {
	form := adminProductCoverFormPageData{
		Reference:      adminProductReference(product.ID),
		ProductName:    product.Name,
		Action:         action,
		CancelPath:     adminProductPath(product.ID),
		ProductVersion: strconv.FormatInt(product.Version, 10),
		Values:         values,
		Errors:         validationErrors,
		HasErrors:      validationErrors != (adminProductCoverFormErrors{}),
		SubmitLabel:    "Upload cover image",
	}
	if product.Cover != nil {
		cover := productCoverPageData{
			Path: adminProductCoverAssetPath(
				product.ID,
				product.Cover.Version,
			),
			AltText: product.Cover.AltText,
			Caption: product.Cover.Caption,
			Width:   strconv.Itoa(product.Cover.Width),
			Height:  strconv.Itoa(product.Cover.Height),
		}
		form.ExistingCover = &cover
		form.SubmitLabel = "Replace cover image"
	}

	return form
}

// renderAdminProductCoverConflict builds the fixed non-echoing recovery page
// used only by stale cover forms.
func (app *application) renderAdminProductCoverConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	productID int64,
) {
	detailPath := adminProductPath(productID)
	data := newAuthenticatedAdminPageData(
		"Product changed",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductConflict = &adminProductConflictPageData{
		DetailPath:  detailPath,
		EditPath:    adminProductCoverPath(productID),
		ActionLabel: "Open latest cover form",
		Guidance: "Open a fresh cover form, review the current image metadata, " +
			"and choose the file again if the replacement is still needed.",
	}
	app.renderAdmin(w, http.StatusConflict, "product-conflict.html", data)
}
