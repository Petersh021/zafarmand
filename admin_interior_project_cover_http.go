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
	// adminInteriorProjectCoverRequestMaximumBytes leaves a fixed allowance for
	// multipart boundaries and four small text controls around the bounded image.
	adminInteriorProjectCoverRequestMaximumBytes = reviewedCoverMaximumBytes + 64*1024
	// adminInteriorProjectCoverTextPartMaximumBytes rejects an abusive text part
	// before semantic Unicode-character validation.
	adminInteriorProjectCoverTextPartMaximumBytes = 8 * 1024
)

// adminInteriorProjectCoverFormPageData is the complete protected
// upload-or-replace contract. It contains metadata and paths, never image bytes.
type adminInteriorProjectCoverFormPageData struct {
	// Reference is the stable administrator project label.
	Reference string
	// ProjectTitle identifies the record being edited.
	ProjectTitle string
	// Action is the canonical multipart POST destination.
	Action string
	// CancelPath returns to project detail without mutation.
	CancelPath string
	// ProjectVersion is the optimistic revision submitted as a hidden field.
	ProjectVersion string
	// ExistingCover contains the current protected preview, or nil.
	ExistingCover *interiorProjectCoverPageData
	// Values restores escaped stored or submitted text for display or correction.
	Values adminInteriorProjectCoverFormValues
	// Errors contains fixed field-level explanations.
	Errors adminInteriorProjectCoverFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// SubmitLabel distinguishes initial upload from replacement.
	SubmitLabel string
}

// adminInteriorProjectCoverFormValues contains the two administrator-controlled
// visible text values. Browsers do not permit a selected file to be restored.
type adminInteriorProjectCoverFormValues struct {
	// AltText retains the visible required alternative-text input.
	AltText string
	// Caption retains the visible optional caption input.
	Caption string
}

// adminInteriorProjectCoverFormErrors stores safe semantic explanations for the
// three visible cover controls.
type adminInteriorProjectCoverFormErrors struct {
	// Image explains a missing, corrupt, unsupported, or unsafe image.
	Image string
	// AltText explains the required trimmed alternative-text boundary.
	AltText string
	// Caption explains the optional trimmed caption boundary.
	Caption string
}

// parsedAdminInteriorProjectCoverForm contains exact-cardinality multipart
// values before CSRF, revision, image, or text validation.
type parsedAdminInteriorProjectCoverForm struct {
	// CSRFToken is compared with the authenticated session secret.
	CSRFToken string
	// ProjectVersion is the server-issued optimistic revision text.
	ProjectVersion string
	// AltText is the untrusted visible alternative-text value.
	AltText string
	// Caption is the untrusted visible caption value.
	Caption string
	// Image contains the bounded encoded browser-selected file.
	Image []byte
}

// adminInteriorProjectCoverPath constructs the canonical cover-management path
// for one positive project identity.
func adminInteriorProjectCoverPath(projectID int64) string {
	return adminInteriorProjectPath(projectID) + "/cover"
}

// adminInteriorProjectCoverAssetPath adds one exact positive cover revision to
// the protected management path.
func adminInteriorProjectCoverAssetPath(
	projectID int64,
	coverVersion int64,
) string {
	return adminInteriorProjectCoverPath(projectID) + "/" +
		strconv.FormatInt(coverVersion, 10)
}

// adminInteriorProjectCoverFormHandler renders one current project revision
// without loading or mutating binary content.
func (app *application) adminInteriorProjectCoverFormHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminInteriorProjectCoverPath(
		r,
		false,
	)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project cover request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminInteriorProjects == nil {
		log.Print("admin Interior project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	// Resolve and compare the stored revision before decoding and re-encoding an
	// uploaded image that can no longer be applied safely.
	project, ok := app.readAdminInteriorProjectForCover(w, r, projectID)
	if !ok {
		return
	}
	form := newAdminInteriorProjectCoverFormPageData(
		project,
		canonicalPath,
		adminInteriorProjectCoverFormValuesFromRecord(project),
		adminInteriorProjectCoverFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Manage Interior project cover",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectCoverForm = &form

	app.renderAdmin(
		w,
		http.StatusOK,
		"interior-project-cover-form.html",
		data,
	)
}

// adminInteriorProjectCoverUploadHandler validates one exact multipart request
// and atomically inserts or replaces cover content for the current revision.
func (app *application) adminInteriorProjectCoverUploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminInteriorProjectCoverPath(
		r,
		false,
	)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project cover request", http.StatusBadRequest)

		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return
	}

	parsed, parsedOK := parseStrictAdminInteriorProjectCoverForm(w, r)
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

	expectedVersion, valid := parseCanonicalPositiveInt64(parsed.ProjectVersion)
	if !valid || expectedVersion == math.MaxInt64 {
		// Version is a server-issued concurrency control, not a visible project
		// field. Tampering or overflow is a malformed request rather than a form
		// correction opportunity.
		http.Error(w, "invalid Interior project revision", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminInteriorProjects == nil ||
		app.adminInteriorProjectWrites == nil {
		log.Print("admin Interior project cover dependency unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	project, ok := app.readAdminInteriorProjectForCover(w, r, projectID)
	if !ok {
		return
	}
	if project.Version != expectedVersion {
		app.renderAdminInteriorProjectCoverConflict(
			w,
			requestIdentity,
			projectID,
		)

		return
	}

	values := adminInteriorProjectCoverFormValues{
		AltText: parsed.AltText,
		Caption: parsed.Caption,
	}
	input, validationErrors := validateAdminInteriorProjectCoverForm(parsed)
	if validationErrors != (adminInteriorProjectCoverFormErrors{}) {
		form := newAdminInteriorProjectCoverFormPageData(
			project,
			canonicalPath,
			values,
			validationErrors,
		)
		data := newAuthenticatedAdminPageData(
			"Manage Interior project cover",
			adminInteriorProjectNavigationPath,
			requestIdentity,
		)
		data.InteriorProjectCoverForm = &form
		app.renderAdmin(
			w,
			http.StatusUnprocessableEntity,
			"interior-project-cover-form.html",
			data,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminInteriorProjectWrites.UpsertCover(
		ctx,
		projectID,
		expectedVersion,
		input,
	)
	cancel()
	switch {
	case errors.Is(err, errAdminInteriorProjectNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, errAdminInteriorProjectWriteConflict):
		app.renderAdminInteriorProjectCoverConflict(
			w,
			requestIdentity,
			projectID,
		)

		return
	case err != nil || result.ProjectID != projectID ||
		result.ProjectVersion != expectedVersion+1 ||
		result.CoverVersion <= 0:
		log.Print("admin Interior project cover update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(
		w,
		r,
		adminInteriorProjectPath(projectID),
		http.StatusSeeOther,
	)
}

// adminInteriorProjectCoverAssetHandler serves one exact revision inside the
// authenticated no-store shell for Draft, Published, or Archived projects.
func (app *application) adminInteriorProjectCoverAssetHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := authenticatedAdminFromContext(r.Context()); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminInteriorProjectCoverPath(
		r,
		true,
	)
	if !valid || r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project cover request", http.StatusBadRequest)

		return
	}
	coverVersion, valid := parseCanonicalPositiveInt64(r.PathValue("version"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminInteriorProjects == nil {
		log.Print("admin Interior project cover reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	asset, err := app.adminInteriorProjects.FindCoverByProjectID(
		ctx,
		projectID,
		coverVersion,
	)
	cancel()
	if errors.Is(err, errInteriorProjectCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidInteriorProjectCoverAsset(asset) ||
		asset.InteriorProjectID != projectID || asset.Version != coverVersion {
		log.Print("admin Interior project cover read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	if _, err := w.Write(asset.Content); err != nil {
		log.Print("admin Interior project cover response write failed")
	}
}

// canonicalAdminInteriorProjectCoverPath validates the decoded project ID,
// optional cover revision, and exact escaped path.
func canonicalAdminInteriorProjectCoverPath(
	r *http.Request,
	withVersion bool,
) (int64, string, bool) {
	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminInteriorProjectCoverPath(projectID)
	if withVersion {
		version, versionValid := parseCanonicalPositiveInt64(
			r.PathValue("version"),
		)
		if !versionValid {
			return 0, "", false
		}
		canonicalPath = adminInteriorProjectCoverAssetPath(projectID, version)
	}
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return projectID, canonicalPath, true
}

// readAdminInteriorProjectForCover centralizes the bounded protected lookup and
// safe 404/503 mapping shared by cover GET and POST forms.
func (app *application) readAdminInteriorProjectForCover(
	w http.ResponseWriter,
	r *http.Request,
	projectID int64,
) (adminInteriorProjectRecord, bool) {
	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	project, err := app.adminInteriorProjects.FindByID(ctx, projectID)
	cancel()
	if errors.Is(err, errAdminInteriorProjectNotFound) {
		http.NotFound(w, r)

		return adminInteriorProjectRecord{}, false
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminInteriorProject(project) {
		log.Print("admin Interior project cover form read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminInteriorProjectRecord{}, false
	}

	return project, true
}

// parseStrictAdminInteriorProjectCoverForm accepts exactly four text parts and
// one file part, each once, without unknown fields or content codings.
func parseStrictAdminInteriorProjectCoverForm(
	w http.ResponseWriter,
	r *http.Request,
) (parsedAdminInteriorProjectCoverForm, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	boundary := parameters["boundary"]
	if err != nil || mediaType != "multipart/form-data" || boundary == "" {
		http.Error(w, "content type must be multipart/form-data", http.StatusUnsupportedMediaType)

		return parsedAdminInteriorProjectCoverForm{}, false
	}

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		adminInteriorProjectCoverRequestMaximumBytes,
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
		// NextRawPart retains Content-Transfer-Encoding so it can be rejected
		// instead of silently decoded by multipart.NextPart.
		part, nextErr := reader.NextRawPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeAdminInteriorProjectCoverMultipartError(w, nextErr)

			return parsedAdminInteriorProjectCoverForm{}, false
		}

		name, isFile, headerOK := strictAdminInteriorProjectCoverPartIdentity(part)
		seen, known := expected[name]
		if !headerOK || !known || seen {
			_ = part.Close()
			http.Error(w, "invalid Interior cover form submission", http.StatusBadRequest)

			return parsedAdminInteriorProjectCoverForm{}, false
		}
		if part.Header.Get("Content-Encoding") != "" ||
			part.Header.Get("Content-Transfer-Encoding") != "" {
			_ = part.Close()
			http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

			return parsedAdminInteriorProjectCoverForm{}, false
		}

		limit := int64(adminInteriorProjectCoverTextPartMaximumBytes)
		if isFile {
			limit = reviewedCoverMaximumBytes
		}
		content, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		closeErr := part.Close()
		if readErr != nil {
			writeAdminInteriorProjectCoverMultipartError(w, readErr)

			return parsedAdminInteriorProjectCoverForm{}, false
		}
		if closeErr != nil {
			writeAdminInteriorProjectCoverMultipartError(w, closeErr)

			return parsedAdminInteriorProjectCoverForm{}, false
		}
		if int64(len(content)) > limit {
			http.Error(w, "Interior cover form submission is too large", http.StatusRequestEntityTooLarge)

			return parsedAdminInteriorProjectCoverForm{}, false
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
			http.Error(w, "invalid Interior cover form submission", http.StatusBadRequest)

			return parsedAdminInteriorProjectCoverForm{}, false
		}
	}

	return parsedAdminInteriorProjectCoverForm{
		CSRFToken:      textValues["csrf_token"],
		ProjectVersion: textValues["version"],
		AltText:        textValues["alt_text"],
		Caption:        textValues["caption"],
		Image:          imageContent,
	}, true
}

// strictAdminInteriorProjectCoverPartIdentity validates Content-Disposition and
// reports whether the named part uses the single permitted file-control shape.
// The caller separately enforces known names, uniqueness, and cardinality.
func strictAdminInteriorProjectCoverPartIdentity(
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

// writeAdminInteriorProjectCoverMultipartError maps a body-cap failure to 413
// and malformed multipart syntax to a generic 400 without reflecting details.
func writeAdminInteriorProjectCoverMultipartError(
	w http.ResponseWriter,
	err error,
) {
	status := http.StatusBadRequest
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, "invalid Interior cover form submission", status)
}

// validateAdminInteriorProjectCoverForm converts decoded parts into the writer
// input while keeping every field error independent and safe.
func validateAdminInteriorProjectCoverForm(
	parsed parsedAdminInteriorProjectCoverForm,
) (adminInteriorProjectCoverWriteInput, adminInteriorProjectCoverFormErrors) {
	var validationErrors adminInteriorProjectCoverFormErrors
	if !isValidRequiredReviewedCoverText(
		parsed.AltText,
		reviewedCoverAltTextMaximumLength,
	) {
		validationErrors.AltText = "Enter trimmed alternative text between 1 and 300 characters."
	}
	if !isValidOptionalReviewedCoverText(
		parsed.Caption,
		reviewedCoverCaptionMaximumLength,
	) {
		validationErrors.Caption = "Use at most 500 characters and remove leading or trailing whitespace."
	}

	normalizedContent, inspection, err := normalizeReviewedCover(parsed.Image)
	if err != nil {
		validationErrors.Image = "Choose a complete JPEG or PNG up to 8 MiB and 25 megapixels."
	}
	if validationErrors != (adminInteriorProjectCoverFormErrors{}) {
		return adminInteriorProjectCoverWriteInput{}, validationErrors
	}

	return adminInteriorProjectCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     normalizedContent,
		ByteSize:    len(normalizedContent),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     parsed.AltText,
		Caption:     parsed.Caption,
	}, adminInteriorProjectCoverFormErrors{}
}

// adminInteriorProjectCoverFormValuesFromRecord pre-fills reviewed text when a
// cover exists and otherwise returns an empty form.
func adminInteriorProjectCoverFormValuesFromRecord(
	project adminInteriorProjectRecord,
) adminInteriorProjectCoverFormValues {
	if project.Cover == nil {
		return adminInteriorProjectCoverFormValues{}
	}

	return adminInteriorProjectCoverFormValues{
		AltText: project.Cover.AltText,
		Caption: project.Cover.Caption,
	}
}

// newAdminInteriorProjectCoverFormPageData derives every trusted path, label,
// and protected-preview value outside the template.
func newAdminInteriorProjectCoverFormPageData(
	project adminInteriorProjectRecord,
	action string,
	values adminInteriorProjectCoverFormValues,
	validationErrors adminInteriorProjectCoverFormErrors,
) adminInteriorProjectCoverFormPageData {
	form := adminInteriorProjectCoverFormPageData{
		Reference:      adminInteriorProjectReference(project.ID),
		ProjectTitle:   project.Title,
		Action:         action,
		CancelPath:     adminInteriorProjectPath(project.ID),
		ProjectVersion: strconv.FormatInt(project.Version, 10),
		Values:         values,
		Errors:         validationErrors,
		HasErrors:      validationErrors != (adminInteriorProjectCoverFormErrors{}),
		SubmitLabel:    "Upload cover image",
	}
	if project.Cover != nil {
		cover := interiorProjectCoverPageData{
			Path: adminInteriorProjectCoverAssetPath(
				project.ID,
				project.Cover.Version,
			),
			AltText: project.Cover.AltText,
			Caption: project.Cover.Caption,
			Width:   strconv.Itoa(project.Cover.Width),
			Height:  strconv.Itoa(project.Cover.Height),
		}
		form.ExistingCover = &cover
		form.SubmitLabel = "Replace cover image"
	}

	return form
}

// renderAdminInteriorProjectCoverConflict builds fixed recovery navigation for
// a stale cover form and never echoes an unpersisted browser file selection.
func (app *application) renderAdminInteriorProjectCoverConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	projectID int64,
) {
	app.renderAdminInteriorProjectConflict(
		w,
		requestIdentity,
		projectID,
		adminInteriorProjectCoverPath(projectID),
		"Open latest cover form",
		"Open a fresh cover form, review the current image metadata, and choose the file again if the replacement is still needed.",
	)
}
