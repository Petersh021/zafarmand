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

// Architecture cover requests bound both the complete multipart body and each
// small reviewed text part independently from semantic validation.
const (
	// adminArchitectureProjectCoverRequestMaximumBytes leaves a fixed allowance for
	// multipart boundaries and four small text controls around the bounded image.
	adminArchitectureProjectCoverRequestMaximumBytes = reviewedCoverMaximumBytes + 64*1024
	// adminArchitectureProjectCoverTextPartMaximumBytes rejects an abusive text part
	// before semantic Unicode-character validation.
	adminArchitectureProjectCoverTextPartMaximumBytes = 8 * 1024
)

// adminArchitectureProjectCoverFormPageData is the complete protected
// upload-or-replace contract. It contains metadata and paths, never image bytes.
type adminArchitectureProjectCoverFormPageData struct {
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
	ExistingCover *architectureProjectCoverPageData
	// Values restores escaped stored or submitted text for display or correction.
	Values adminArchitectureProjectCoverFormValues
	// Errors contains fixed field-level explanations.
	Errors adminArchitectureProjectCoverFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// SubmitLabel distinguishes initial upload from replacement.
	SubmitLabel string
}

// adminArchitectureProjectCoverFormValues contains the two administrator-controlled
// visible text values. Browsers do not permit a selected file to be restored.
type adminArchitectureProjectCoverFormValues struct {
	// AltText retains the visible required alternative-text input.
	AltText string
	// Caption retains the visible optional caption input.
	Caption string
}

// adminArchitectureProjectCoverFormErrors stores safe semantic explanations for the
// three visible cover controls.
type adminArchitectureProjectCoverFormErrors struct {
	// Image explains a missing, corrupt, unsupported, or unsafe image.
	Image string
	// AltText explains the required trimmed alternative-text boundary.
	AltText string
	// Caption explains the optional trimmed caption boundary.
	Caption string
}

// parsedAdminArchitectureProjectCoverForm contains exact-cardinality multipart
// values before CSRF, revision, image, or text validation.
type parsedAdminArchitectureProjectCoverForm struct {
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

// adminArchitectureProjectCoverPath constructs the canonical cover-management path
// for one positive project identity.
func adminArchitectureProjectCoverPath(projectID int64) string {
	return adminArchitectureProjectPath(projectID) + "/cover"
}

// adminArchitectureProjectCoverAssetPath adds one exact positive cover revision to
// the protected management path.
func adminArchitectureProjectCoverAssetPath(
	projectID int64,
	coverVersion int64,
) string {
	return adminArchitectureProjectCoverPath(projectID) + "/" +
		strconv.FormatInt(coverVersion, 10)
}

// adminArchitectureProjectCoverFormHandler renders one current project revision
// without loading or mutating binary content.
func (app *application) adminArchitectureProjectCoverFormHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminArchitectureProjectCoverPath(
		r,
		false,
	)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project cover request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminArchitectureProjects == nil {
		log.Print("admin Architecture project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	// Read the current protected record so the GET form can show existing cover
	// metadata and carry the database-owned project revision into the later POST.
	project, ok := app.readAdminArchitectureProjectForCover(w, r, projectID)
	if !ok {
		return
	}
	form := newAdminArchitectureProjectCoverFormPageData(
		project,
		canonicalPath,
		adminArchitectureProjectCoverFormValuesFromRecord(project),
		adminArchitectureProjectCoverFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Manage Architecture project cover",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectCoverForm = &form

	app.renderAdmin(
		w,
		http.StatusOK,
		"architecture-project-cover-form.html",
		data,
	)
}

// adminArchitectureProjectCoverUploadHandler validates one exact multipart request
// and atomically inserts or replaces cover content for the current revision.
func (app *application) adminArchitectureProjectCoverUploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminArchitectureProjectCoverPath(
		r,
		false,
	)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project cover request", http.StatusBadRequest)

		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return
	}

	parsed, parsedOK := parseStrictAdminArchitectureProjectCoverForm(w, r)
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
		http.Error(w, "invalid Architecture project revision", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminArchitectureProjects == nil ||
		app.adminArchitectureProjectWrites == nil {
		log.Print("admin Architecture project cover dependency unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	// Resolve and compare the stored revision before decoding and re-encoding an
	// uploaded image that can no longer be applied safely.
	project, ok := app.readAdminArchitectureProjectForCover(w, r, projectID)
	if !ok {
		return
	}
	if project.Version != expectedVersion {
		app.renderAdminArchitectureProjectCoverConflict(
			w,
			requestIdentity,
			projectID,
		)

		return
	}

	values := adminArchitectureProjectCoverFormValues{
		AltText: parsed.AltText,
		Caption: parsed.Caption,
	}
	input, validationErrors := validateAdminArchitectureProjectCoverForm(parsed)
	if validationErrors != (adminArchitectureProjectCoverFormErrors{}) {
		form := newAdminArchitectureProjectCoverFormPageData(
			project,
			canonicalPath,
			values,
			validationErrors,
		)
		data := newAuthenticatedAdminPageData(
			"Manage Architecture project cover",
			adminArchitectureProjectNavigationPath,
			requestIdentity,
		)
		data.ArchitectureProjectCoverForm = &form
		app.renderAdmin(
			w,
			http.StatusUnprocessableEntity,
			"architecture-project-cover-form.html",
			data,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminArchitectureProjectWrites.UpsertCover(
		ctx,
		projectID,
		expectedVersion,
		input,
	)
	cancel()
	switch {
	case errors.Is(err, errAdminArchitectureProjectNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, errAdminArchitectureProjectWriteConflict):
		app.renderAdminArchitectureProjectCoverConflict(
			w,
			requestIdentity,
			projectID,
		)

		return
	case err != nil || result.ProjectID != projectID ||
		result.ProjectVersion != expectedVersion+1 ||
		result.CoverVersion <= 0:
		log.Print("admin Architecture project cover update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(
		w,
		r,
		adminArchitectureProjectPath(projectID),
		http.StatusSeeOther,
	)
}

// adminArchitectureProjectCoverAssetHandler serves one exact revision inside the
// authenticated no-store shell for Draft, Published, or Archived projects.
func (app *application) adminArchitectureProjectCoverAssetHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := authenticatedAdminFromContext(r.Context()); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminArchitectureProjectCoverPath(
		r,
		true,
	)
	if !valid || r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project cover request", http.StatusBadRequest)

		return
	}
	coverVersion, valid := parseCanonicalPositiveInt64(r.PathValue("version"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminArchitectureProjects == nil {
		log.Print("admin Architecture project cover reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	asset, err := app.adminArchitectureProjects.FindCoverByProjectID(
		ctx,
		projectID,
		coverVersion,
	)
	cancel()
	if errors.Is(err, errArchitectureProjectCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidArchitectureProjectCoverAsset(asset) ||
		asset.ArchitectureProjectID != projectID || asset.Version != coverVersion {
		log.Print("admin Architecture project cover read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	if _, err := w.Write(asset.Content); err != nil {
		log.Print("admin Architecture project cover response write failed")
	}
}

// canonicalAdminArchitectureProjectCoverPath validates the decoded project ID,
// optional cover revision, and exact escaped path.
func canonicalAdminArchitectureProjectCoverPath(
	r *http.Request,
	withVersion bool,
) (int64, string, bool) {
	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminArchitectureProjectCoverPath(projectID)
	if withVersion {
		version, versionValid := parseCanonicalPositiveInt64(
			r.PathValue("version"),
		)
		if !versionValid {
			return 0, "", false
		}
		canonicalPath = adminArchitectureProjectCoverAssetPath(projectID, version)
	}
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return projectID, canonicalPath, true
}

// readAdminArchitectureProjectForCover centralizes the bounded protected lookup and
// safe 404/503 mapping shared by cover GET and POST forms.
func (app *application) readAdminArchitectureProjectForCover(
	w http.ResponseWriter,
	r *http.Request,
	projectID int64,
) (adminArchitectureProjectRecord, bool) {
	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	project, err := app.adminArchitectureProjects.FindByID(ctx, projectID)
	cancel()
	if errors.Is(err, errAdminArchitectureProjectNotFound) {
		http.NotFound(w, r)

		return adminArchitectureProjectRecord{}, false
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminArchitectureProject(project) {
		log.Print("admin Architecture project cover form read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return adminArchitectureProjectRecord{}, false
	}

	return project, true
}

// parseStrictAdminArchitectureProjectCoverForm accepts exactly four text parts and
// one file part, each once, without unknown fields or content codings.
func parseStrictAdminArchitectureProjectCoverForm(
	w http.ResponseWriter,
	r *http.Request,
) (parsedAdminArchitectureProjectCoverForm, bool) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	boundary := parameters["boundary"]
	if err != nil || mediaType != "multipart/form-data" || boundary == "" {
		http.Error(w, "content type must be multipart/form-data", http.StatusUnsupportedMediaType)

		return parsedAdminArchitectureProjectCoverForm{}, false
	}

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		adminArchitectureProjectCoverRequestMaximumBytes,
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
			writeAdminArchitectureProjectCoverMultipartError(w, nextErr)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}

		name, isFile, headerOK := strictAdminArchitectureProjectCoverPartIdentity(part)
		seen, known := expected[name]
		if !headerOK || !known || seen {
			_ = part.Close()
			http.Error(w, "invalid Architecture cover form submission", http.StatusBadRequest)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}
		if part.Header.Get("Content-Encoding") != "" ||
			part.Header.Get("Content-Transfer-Encoding") != "" {
			_ = part.Close()
			http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}

		limit := int64(adminArchitectureProjectCoverTextPartMaximumBytes)
		if isFile {
			limit = reviewedCoverMaximumBytes
		}
		content, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		closeErr := part.Close()
		if readErr != nil {
			writeAdminArchitectureProjectCoverMultipartError(w, readErr)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}
		if closeErr != nil {
			writeAdminArchitectureProjectCoverMultipartError(w, closeErr)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}
		if int64(len(content)) > limit {
			http.Error(w, "Architecture cover form submission is too large", http.StatusRequestEntityTooLarge)

			return parsedAdminArchitectureProjectCoverForm{}, false
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
			http.Error(w, "invalid Architecture cover form submission", http.StatusBadRequest)

			return parsedAdminArchitectureProjectCoverForm{}, false
		}
	}

	return parsedAdminArchitectureProjectCoverForm{
		CSRFToken:      textValues["csrf_token"],
		ProjectVersion: textValues["version"],
		AltText:        textValues["alt_text"],
		Caption:        textValues["caption"],
		Image:          imageContent,
	}, true
}

// strictAdminArchitectureProjectCoverPartIdentity validates Content-Disposition and
// reports whether the named part uses the single permitted file-control shape.
// The caller separately enforces known names, uniqueness, and cardinality.
func strictAdminArchitectureProjectCoverPartIdentity(
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

// writeAdminArchitectureProjectCoverMultipartError maps a body-cap failure to 413
// and malformed multipart syntax to a generic 400 without reflecting details.
func writeAdminArchitectureProjectCoverMultipartError(
	w http.ResponseWriter,
	err error,
) {
	status := http.StatusBadRequest
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, "invalid Architecture cover form submission", status)
}

// validateAdminArchitectureProjectCoverForm converts decoded parts into the writer
// input while keeping every field error independent and safe.
func validateAdminArchitectureProjectCoverForm(
	parsed parsedAdminArchitectureProjectCoverForm,
) (adminArchitectureProjectCoverWriteInput, adminArchitectureProjectCoverFormErrors) {
	var validationErrors adminArchitectureProjectCoverFormErrors
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
	if validationErrors != (adminArchitectureProjectCoverFormErrors{}) {
		return adminArchitectureProjectCoverWriteInput{}, validationErrors
	}

	return adminArchitectureProjectCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     normalizedContent,
		ByteSize:    len(normalizedContent),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     parsed.AltText,
		Caption:     parsed.Caption,
	}, adminArchitectureProjectCoverFormErrors{}
}

// adminArchitectureProjectCoverFormValuesFromRecord pre-fills reviewed text when a
// cover exists and otherwise returns an empty form.
func adminArchitectureProjectCoverFormValuesFromRecord(
	project adminArchitectureProjectRecord,
) adminArchitectureProjectCoverFormValues {
	if project.Cover == nil {
		return adminArchitectureProjectCoverFormValues{}
	}

	return adminArchitectureProjectCoverFormValues{
		AltText: project.Cover.AltText,
		Caption: project.Cover.Caption,
	}
}

// newAdminArchitectureProjectCoverFormPageData derives every trusted path, label,
// and protected-preview value outside the template.
func newAdminArchitectureProjectCoverFormPageData(
	project adminArchitectureProjectRecord,
	action string,
	values adminArchitectureProjectCoverFormValues,
	validationErrors adminArchitectureProjectCoverFormErrors,
) adminArchitectureProjectCoverFormPageData {
	form := adminArchitectureProjectCoverFormPageData{
		Reference:      adminArchitectureProjectReference(project.ID),
		ProjectTitle:   project.Title,
		Action:         action,
		CancelPath:     adminArchitectureProjectPath(project.ID),
		ProjectVersion: strconv.FormatInt(project.Version, 10),
		Values:         values,
		Errors:         validationErrors,
		HasErrors:      validationErrors != (adminArchitectureProjectCoverFormErrors{}),
		SubmitLabel:    "Upload cover image",
	}
	if project.Cover != nil {
		cover := architectureProjectCoverPageData{
			Path: adminArchitectureProjectCoverAssetPath(
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

// renderAdminArchitectureProjectCoverConflict builds fixed recovery navigation for
// a stale cover form and never echoes an unpersisted browser file selection.
func (app *application) renderAdminArchitectureProjectCoverConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	projectID int64,
) {
	app.renderAdminArchitectureProjectConflict(
		w,
		requestIdentity,
		projectID,
		adminArchitectureProjectCoverPath(projectID),
		"Open latest cover form",
		"Open a fresh cover form, review the current image metadata, and choose the file again if the replacement is still needed.",
	)
}
