package main

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
)

const (
	// adminInteriorProjectNewPath is the canonical GET address for an empty form.
	adminInteriorProjectNewPath = "/admin/interior-projects/new"
	// adminInteriorProjectCreatePath is the fixed collection POST destination.
	adminInteriorProjectCreatePath = "/admin/interior-projects"
	// adminInteriorProjectFormMaximumBytes accommodates the largest valid
	// four-byte UTF-8 form after percent encoding while retaining a hard cap.
	adminInteriorProjectFormMaximumBytes = 128 * 1024
)

// adminInteriorProjectFormPageData is the complete create/edit template
// contract. Paths and option values are trusted; submitted text remains escaped.
type adminInteriorProjectFormPageData struct {
	// Eyebrow distinguishes a new record from an existing revision.
	Eyebrow string
	// Heading is the page's single primary heading.
	Heading string
	// Introduction explains the consequence of a successful save.
	Introduction string
	// Action is the canonical POST destination.
	Action string
	// CancelPath returns without mutation.
	CancelPath string
	// SubmitLabel is the visible native button name.
	SubmitLabel string
	// IsEdit selects the hidden revision and edit-specific guidance.
	IsEdit bool
	// Version is the canonical positive revision rendered only for an edit.
	Version string
	// Values restores administrator-entered visible fields after validation.
	Values adminInteriorProjectFormValues
	// Errors contains one safe explanation for each rejected visible field.
	Errors adminInteriorProjectFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// StatusOptions is the closed trusted publication-state vocabulary.
	StatusOptions []adminInteriorProjectStatusOptionPageData
	// HasValidStatus prevents an invalid submitted value from selecting another
	// lifecycle option silently.
	HasValidStatus bool
}

// adminInteriorProjectFormValues contains only visible administrator-controlled
// text. It is presentation data, not a trusted persistence value.
type adminInteriorProjectFormValues struct {
	// Slug is restored to the public-path field.
	Slug string
	// Title is restored to the public heading field.
	Title string
	// Typology is restored to the Interior category field.
	Typology string
	// Location restores optional reviewed geographic text.
	Location string
	// ProjectYear is kept as entered so formatting errors remain visible.
	ProjectYear string
	// ProjectStatus restores the real-world project-state field.
	ProjectStatus string
	// Description restores optional long-form public copy.
	Description string
	// SortOrder is kept as entered so formatting errors remain visible.
	SortOrder string
	// PublicationStatus is compared only with trusted option values.
	PublicationStatus string
}

// adminInteriorProjectFormErrors stores one field-level semantic validation
// message per visible control. Empty values mean that field passed validation.
type adminInteriorProjectFormErrors struct {
	// Slug explains the canonical route rule or a uniqueness conflict.
	Slug string
	// Title explains the required trimmed character boundary.
	Title string
	// Typology explains the required trimmed character boundary.
	Typology string
	// Location explains the optional trimmed character boundary.
	Location string
	// ProjectYear explains the optional four-digit range.
	ProjectYear string
	// ProjectStatus explains the required real-world state boundary.
	ProjectStatus string
	// Description explains the optional long-form boundary.
	Description string
	// SortOrder explains the positive 32-bit integer boundary.
	SortOrder string
	// PublicationStatus explains the closed lifecycle choice.
	PublicationStatus string
}

// adminInteriorProjectStatusOptionPageData pairs a trusted machine value with
// visible text and its server-selected state.
type adminInteriorProjectStatusOptionPageData struct {
	// Value is one exact database lifecycle value.
	Value string
	// Label is its trusted administrator-facing name.
	Label string
	// Selected is true only for an exact current-form match.
	Selected bool
}

// adminInteriorProjectConflictPageData gives a stale editor fixed recovery
// navigation without echoing submitted or newer database content.
type adminInteriorProjectConflictPageData struct {
	// DetailPath opens the current protected record.
	DetailPath string
	// EditPath fetches a fresh text or cover form.
	EditPath string
	// ActionLabel optionally specializes the primary link text.
	ActionLabel string
	// Guidance optionally specializes the fixed recovery explanation.
	Guidance string
}

// adminInteriorProjectNewHandler renders a database-free Draft form. Opening or
// refreshing this GET never creates a record.
func (app *application) adminInteriorProjectNewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminInteriorProjectNewPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project form request", http.StatusBadRequest)

		return
	}

	form := newAdminInteriorProjectFormPageData(
		false,
		adminInteriorProjectCreatePath,
		adminInteriorProjectNavigationPath,
		"",
		adminInteriorProjectFormValues{
			SortOrder:         "1",
			PublicationStatus: draftInteriorProjectStatus,
		},
		adminInteriorProjectFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Create Interior project",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectForm = &form

	app.renderAdmin(w, http.StatusOK, "interior-project-form.html", data)
}

// adminInteriorProjectEditHandler reads one current revision and renders a
// strictly read-only GET form.
func (app *application) adminInteriorProjectEditHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project form request", http.StatusBadRequest)

		return
	}
	projectID, _, valid := canonicalAdminInteriorProjectWritePath(r, "/edit")
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminInteriorProjects == nil {
		log.Print("admin Interior project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	project, err := app.adminInteriorProjects.FindByID(ctx, projectID)
	cancel()
	if errors.Is(err, errAdminInteriorProjectNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminInteriorProject(project) {
		log.Print("admin Interior project edit read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	detailPath := adminInteriorProjectPath(projectID)
	projectYear := ""
	if project.ProjectYear != 0 {
		projectYear = strconv.Itoa(project.ProjectYear)
	}
	form := newAdminInteriorProjectFormPageData(
		true,
		detailPath,
		detailPath,
		strconv.FormatInt(project.Version, 10),
		adminInteriorProjectFormValues{
			Slug:              project.Slug,
			Title:             project.Title,
			Typology:          project.Typology,
			Location:          project.Location,
			ProjectYear:       projectYear,
			ProjectStatus:     project.ProjectStatus,
			Description:       project.Description,
			SortOrder:         strconv.Itoa(project.SortOrder),
			PublicationStatus: project.PublicationStatus,
		},
		adminInteriorProjectFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Edit Interior project",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectForm = &form

	app.renderAdmin(w, http.StatusOK, "interior-project-form.html", data)
}

// adminInteriorProjectCreateHandler validates one exact native form, inserts
// through the narrow writer, and redirects to the canonical detail GET.
func (app *application) adminInteriorProjectCreateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminInteriorProjectMutationRequestIsCanonical(
		w,
		r,
		adminInteriorProjectCreatePath,
	) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"slug",
			"title",
			"typology",
			"location",
			"project_year",
			"project_status",
			"description",
			"sort_order",
			"publication_status",
		},
		adminInteriorProjectFormMaximumBytes,
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

	values := adminInteriorProjectValuesFromForm(form)
	input, validationErrors := validateAdminInteriorProjectFormValues(values)
	if validationErrors != (adminInteriorProjectFormErrors{}) {
		app.renderAdminInteriorProjectFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			false,
			adminInteriorProjectCreatePath,
			adminInteriorProjectNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminInteriorProjectWrites == nil {
		log.Print("admin Interior project writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminInteriorProjectWrites.Create(ctx, input)
	cancel()
	if errors.Is(err, errAdminInteriorProjectSlugConflict) {
		validationErrors.Slug = "That slug is already used by another Interior project."
		app.renderAdminInteriorProjectFormResponse(
			w,
			requestIdentity,
			http.StatusConflict,
			false,
			adminInteriorProjectCreatePath,
			adminInteriorProjectNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if err != nil || !isValidAdminInteriorProjectWriteResult(result) {
		log.Print("admin Interior project create failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(
		w,
		r,
		adminInteriorProjectPath(result.ID),
		http.StatusSeeOther,
	)
}

// adminInteriorProjectUpdateHandler validates the hidden revision before a
// version-guarded edit. Stale valid forms receive a fixed 409 recovery page.
func (app *application) adminInteriorProjectUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminInteriorProjectWritePath(r, "")
	if !valid {
		http.NotFound(w, r)

		return
	}
	if !adminInteriorProjectMutationRequestIsCanonical(w, r, canonicalPath) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"version",
			"slug",
			"title",
			"typology",
			"location",
			"project_year",
			"project_status",
			"description",
			"sort_order",
			"publication_status",
		},
		adminInteriorProjectFormMaximumBytes,
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
		// Version is a server-issued concurrency control, not a visible project
		// field. Tampering or overflow is a malformed request rather than a form
		// correction opportunity.
		http.Error(w, "invalid Interior project revision", http.StatusBadRequest)

		return
	}
	values := adminInteriorProjectValuesFromForm(form)
	input, validationErrors := validateAdminInteriorProjectFormValues(values)
	detailPath := adminInteriorProjectPath(projectID)
	editPath := detailPath + "/edit"
	if validationErrors != (adminInteriorProjectFormErrors{}) {
		app.renderAdminInteriorProjectFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			true,
			detailPath,
			detailPath,
			strconv.FormatInt(expectedVersion, 10),
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminInteriorProjectWrites == nil {
		log.Print("admin Interior project writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminInteriorProjectWrites.Update(
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
		app.renderAdminInteriorProjectConflict(
			w,
			requestIdentity,
			projectID,
			editPath,
			"",
			"",
		)

		return
	case errors.Is(err, errAdminInteriorProjectSlugConflict):
		validationErrors.Slug = "That slug is already used by another Interior project."
		app.renderAdminInteriorProjectFormResponse(
			w,
			requestIdentity,
			http.StatusConflict,
			true,
			detailPath,
			detailPath,
			strconv.FormatInt(expectedVersion, 10),
			values,
			validationErrors,
		)

		return
	case err != nil || result.ID != projectID ||
		result.Version != expectedVersion+1:
		log.Print("admin Interior project update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, detailPath, http.StatusSeeOther)
}

// adminInteriorProjectMutationRequestIsCanonical rejects alternate paths,
// queries, and content codings before the strict form parser reads a body.
func adminInteriorProjectMutationRequestIsCanonical(
	w http.ResponseWriter,
	r *http.Request,
	canonicalPath string,
) bool {
	if r.URL.EscapedPath() != canonicalPath ||
		r.URL.ForceQuery || r.URL.RawQuery != "" {
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			http.Error(w, "invalid Interior project request", http.StatusBadRequest)
		} else {
			http.NotFound(w, r)
		}

		return false
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return false
	}

	return true
}

// canonicalAdminInteriorProjectWritePath validates one positive ID and the
// exact optional suffix used by edit-form and update routes.
func canonicalAdminInteriorProjectWritePath(
	r *http.Request,
	suffix string,
) (int64, string, bool) {
	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminInteriorProjectPath(projectID) + suffix
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return projectID, canonicalPath, true
}

// adminInteriorProjectValuesFromForm copies only visible controls from an
// already cardinality-checked form.
func adminInteriorProjectValuesFromForm(
	form mapFormValues,
) adminInteriorProjectFormValues {
	return adminInteriorProjectFormValues{
		Slug:              form.Get("slug"),
		Title:             form.Get("title"),
		Typology:          form.Get("typology"),
		Location:          form.Get("location"),
		ProjectYear:       form.Get("project_year"),
		ProjectStatus:     form.Get("project_status"),
		Description:       form.Get("description"),
		SortOrder:         form.Get("sort_order"),
		PublicationStatus: form.Get("publication_status"),
	}
}

// validateAdminInteriorProjectFormValues converts visible strings into the
// narrow writer input without normalizing invalid content into another value.
func validateAdminInteriorProjectFormValues(
	values adminInteriorProjectFormValues,
) (adminInteriorProjectWriteInput, adminInteriorProjectFormErrors) {
	var validationErrors adminInteriorProjectFormErrors
	if !isCanonicalInteriorProjectSlug(values.Slug) {
		validationErrors.Slug = "Use 1–120 lowercase letters or numbers, separated only by single hyphens."
	}
	if !isValidInteriorProjectCatalogueText(
		values.Title,
		interiorProjectTitleMaximumLength,
	) {
		validationErrors.Title = "Enter a trimmed project title between 1 and 160 characters."
	}
	if !isValidInteriorProjectCatalogueText(
		values.Typology,
		interiorProjectTypologyMaximumLength,
	) {
		validationErrors.Typology = "Enter a trimmed typology between 1 and 80 characters."
	}
	if values.Location != "" && !isValidInteriorProjectCatalogueText(
		values.Location,
		interiorProjectLocationMaximumLength,
	) {
		validationErrors.Location = "Use at most 160 characters and remove leading or trailing whitespace."
	}

	projectYear := 0
	if values.ProjectYear != "" {
		parsedYear, valid := parseCanonicalPositiveInt64(values.ProjectYear)
		if !valid || parsedYear > math.MaxInt ||
			!isValidInteriorProjectYear(int(parsedYear)) {
			validationErrors.ProjectYear = "Enter a four-digit year from 1000 to 9999, or leave it empty."
		} else {
			projectYear = int(parsedYear)
		}
	}
	if !isValidInteriorProjectCatalogueText(
		values.ProjectStatus,
		interiorProjectStatusMaximumLength,
	) {
		validationErrors.ProjectStatus = "Enter a trimmed project status between 1 and 80 characters."
	}
	if !isValidOptionalEditorialText(
		values.Description,
		interiorProjectDescriptionMaximumLength,
	) {
		validationErrors.Description = "Use at most 6000 characters and remove leading or trailing whitespace."
	}

	sortOrder64, valid := parseCanonicalPositiveInt64(values.SortOrder)
	if !valid || sortOrder64 > math.MaxInt32 {
		validationErrors.SortOrder = "Enter a whole number from 1 to 2147483647."
	}
	if !isValidInteriorProjectPublicationStatus(values.PublicationStatus) {
		validationErrors.PublicationStatus = "Choose Draft, Published, or Archived."
	}

	if validationErrors != (adminInteriorProjectFormErrors{}) {
		return adminInteriorProjectWriteInput{}, validationErrors
	}

	return adminInteriorProjectWriteInput{
		Slug:              values.Slug,
		Title:             values.Title,
		Typology:          values.Typology,
		Location:          values.Location,
		ProjectYear:       projectYear,
		ProjectStatus:     values.ProjectStatus,
		Description:       values.Description,
		SortOrder:         int(sortOrder64),
		PublicationStatus: values.PublicationStatus,
	}, adminInteriorProjectFormErrors{}
}

// newAdminInteriorProjectFormPageData builds trusted labels, paths, and options
// around escaped visible values for either create or edit mode.
func newAdminInteriorProjectFormPageData(
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminInteriorProjectFormValues,
	validationErrors adminInteriorProjectFormErrors,
) adminInteriorProjectFormPageData {
	form := adminInteriorProjectFormPageData{
		Eyebrow:       "New Interior record",
		Heading:       "Create Interior project",
		Introduction:  "Create durable project facts, reviewed editorial content, and a fail-closed publication state.",
		Action:        action,
		CancelPath:    cancelPath,
		SubmitLabel:   "Create Interior project",
		IsEdit:        isEdit,
		Version:       version,
		Values:        values,
		Errors:        validationErrors,
		HasErrors:     validationErrors != (adminInteriorProjectFormErrors{}),
		StatusOptions: adminInteriorProjectStatusOptions(values.PublicationStatus),
		HasValidStatus: isValidInteriorProjectPublicationStatus(
			values.PublicationStatus,
		),
	}
	if isEdit {
		form.Eyebrow = "Interior revision " + version
		form.Heading = "Edit Interior project"
		form.Introduction = "Save deliberate project, editorial, or publication changes. A stale form cannot overwrite a newer revision."
		form.SubmitLabel = "Save Interior project"
	}

	return form
}

// adminInteriorProjectStatusOptions creates the closed select list and never
// turns an untrusted submitted value into option markup.
func adminInteriorProjectStatusOptions(
	selected string,
) []adminInteriorProjectStatusOptionPageData {
	return []adminInteriorProjectStatusOptionPageData{
		{Value: draftInteriorProjectStatus, Label: "Draft", Selected: selected == draftInteriorProjectStatus},
		{Value: publishedInteriorProjectStatus, Label: "Published", Selected: selected == publishedInteriorProjectStatus},
		{Value: archivedInteriorProjectStatus, Label: "Archived", Selected: selected == archivedInteriorProjectStatus},
	}
}

// renderAdminInteriorProjectFormResponse reuses one form contract for semantic
// validation and safe slug conflicts while preserving the authenticated shell.
func (app *application) renderAdminInteriorProjectFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminInteriorProjectFormValues,
	validationErrors adminInteriorProjectFormErrors,
) {
	form := newAdminInteriorProjectFormPageData(
		isEdit,
		action,
		cancelPath,
		version,
		values,
		validationErrors,
	)
	title := "Create Interior project"
	if isEdit {
		title = "Edit Interior project"
	}
	data := newAuthenticatedAdminPageData(
		title,
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectForm = &form

	app.renderAdmin(w, status, "interior-project-form.html", data)
}

// renderAdminInteriorProjectConflict builds the fixed non-echoing stale-write
// page shared by text and cover updates.
func (app *application) renderAdminInteriorProjectConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	projectID int64,
	editPath string,
	actionLabel string,
	guidance string,
) {
	detailPath := adminInteriorProjectPath(projectID)
	data := newAuthenticatedAdminPageData(
		"Interior project changed",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectConflict = &adminInteriorProjectConflictPageData{
		DetailPath:  detailPath,
		EditPath:    editPath,
		ActionLabel: actionLabel,
		Guidance:    guidance,
	}

	app.renderAdmin(
		w,
		http.StatusConflict,
		"interior-project-conflict.html",
		data,
	)
}
