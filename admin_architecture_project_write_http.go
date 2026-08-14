package main

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
)

// Architecture create/edit handlers share canonical destinations and a bounded
// URL-encoded request size.
const (
	// adminArchitectureProjectNewPath is the canonical GET address for an empty form.
	adminArchitectureProjectNewPath = "/admin/architecture-projects/new"
	// adminArchitectureProjectCreatePath is the fixed collection POST destination.
	adminArchitectureProjectCreatePath = "/admin/architecture-projects"
	// adminArchitectureProjectFormMaximumBytes accommodates the largest valid
	// four-byte UTF-8 form after percent encoding while retaining a hard cap.
	adminArchitectureProjectFormMaximumBytes = 128 * 1024
)

// adminArchitectureProjectFormPageData is the complete create/edit template
// contract. Paths and option values are trusted; submitted text remains escaped.
type adminArchitectureProjectFormPageData struct {
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
	Values adminArchitectureProjectFormValues
	// Errors contains one safe explanation for each rejected visible field.
	Errors adminArchitectureProjectFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// StatusOptions is the closed trusted publication-state vocabulary.
	StatusOptions []adminArchitectureProjectStatusOptionPageData
	// HasValidStatus prevents an invalid submitted value from selecting another
	// lifecycle option silently.
	HasValidStatus bool
}

// adminArchitectureProjectFormValues contains only visible administrator-controlled
// text. It is presentation data, not a trusted persistence value.
type adminArchitectureProjectFormValues struct {
	// Slug is restored to the public-path field.
	Slug string
	// Title is restored to the public heading field.
	Title string
	// Typology is restored to the Architecture category field.
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

// adminArchitectureProjectFormErrors stores one field-level semantic validation
// message per visible control. Empty values mean that field passed validation.
type adminArchitectureProjectFormErrors struct {
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

// adminArchitectureProjectStatusOptionPageData pairs a trusted machine value with
// visible text and its server-selected state.
type adminArchitectureProjectStatusOptionPageData struct {
	// Value is one exact database lifecycle value.
	Value string
	// Label is its trusted administrator-facing name.
	Label string
	// Selected is true only for an exact current-form match.
	Selected bool
}

// adminArchitectureProjectConflictPageData gives a stale editor fixed recovery
// navigation without echoing submitted or newer database content.
type adminArchitectureProjectConflictPageData struct {
	// DetailPath opens the current protected record.
	DetailPath string
	// EditPath fetches a fresh text or cover form.
	EditPath string
	// ActionLabel optionally specializes the primary link text.
	ActionLabel string
	// Guidance optionally specializes the fixed recovery explanation.
	Guidance string
}

// adminArchitectureProjectNewHandler renders a database-free Draft form. Opening or
// refreshing this GET never creates a record.
func (app *application) adminArchitectureProjectNewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminArchitectureProjectNewPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project form request", http.StatusBadRequest)

		return
	}

	form := newAdminArchitectureProjectFormPageData(
		false,
		adminArchitectureProjectCreatePath,
		adminArchitectureProjectNavigationPath,
		"",
		adminArchitectureProjectFormValues{
			SortOrder:         "1",
			PublicationStatus: draftArchitectureProjectStatus,
		},
		adminArchitectureProjectFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Create Architecture project",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectForm = &form

	app.renderAdmin(w, http.StatusOK, "architecture-project-form.html", data)
}

// adminArchitectureProjectEditHandler reads one current revision and renders a
// strictly read-only GET form.
func (app *application) adminArchitectureProjectEditHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project form request", http.StatusBadRequest)

		return
	}
	projectID, _, valid := canonicalAdminArchitectureProjectWritePath(r, "/edit")
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminArchitectureProjects == nil {
		log.Print("admin Architecture project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	project, err := app.adminArchitectureProjects.FindByID(ctx, projectID)
	cancel()
	if errors.Is(err, errAdminArchitectureProjectNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || project.ID != projectID ||
		!isValidStoredAdminArchitectureProject(project) {
		log.Print("admin Architecture project edit read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	detailPath := adminArchitectureProjectPath(projectID)
	projectYear := ""
	if project.ProjectYear != 0 {
		projectYear = strconv.Itoa(project.ProjectYear)
	}
	form := newAdminArchitectureProjectFormPageData(
		true,
		detailPath,
		detailPath,
		strconv.FormatInt(project.Version, 10),
		adminArchitectureProjectFormValues{
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
		adminArchitectureProjectFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Edit Architecture project",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectForm = &form

	app.renderAdmin(w, http.StatusOK, "architecture-project-form.html", data)
}

// adminArchitectureProjectCreateHandler validates one exact native form, inserts
// through the narrow writer, and redirects to the canonical detail GET.
func (app *application) adminArchitectureProjectCreateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminArchitectureProjectMutationRequestIsCanonical(
		w,
		r,
		adminArchitectureProjectCreatePath,
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
		adminArchitectureProjectFormMaximumBytes,
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

	values := adminArchitectureProjectValuesFromForm(form)
	input, validationErrors := validateAdminArchitectureProjectFormValues(values)
	if validationErrors != (adminArchitectureProjectFormErrors{}) {
		app.renderAdminArchitectureProjectFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			false,
			adminArchitectureProjectCreatePath,
			adminArchitectureProjectNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminArchitectureProjectWrites == nil {
		log.Print("admin Architecture project writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminArchitectureProjectWrites.Create(ctx, input)
	cancel()
	if errors.Is(err, errAdminArchitectureProjectSlugConflict) {
		validationErrors.Slug = "That slug is already used by another Architecture project."
		app.renderAdminArchitectureProjectFormResponse(
			w,
			requestIdentity,
			http.StatusConflict,
			false,
			adminArchitectureProjectCreatePath,
			adminArchitectureProjectNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if err != nil || !isValidAdminArchitectureProjectWriteResult(result) {
		log.Print("admin Architecture project create failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(
		w,
		r,
		adminArchitectureProjectPath(result.ID),
		http.StatusSeeOther,
	)
}

// adminArchitectureProjectUpdateHandler validates the hidden revision before a
// version-guarded edit. Stale valid forms receive a fixed 409 recovery page.
func (app *application) adminArchitectureProjectUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	projectID, canonicalPath, valid := canonicalAdminArchitectureProjectWritePath(r, "")
	if !valid {
		http.NotFound(w, r)

		return
	}
	if !adminArchitectureProjectMutationRequestIsCanonical(w, r, canonicalPath) {
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
		adminArchitectureProjectFormMaximumBytes,
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
		http.Error(w, "invalid Architecture project revision", http.StatusBadRequest)

		return
	}
	values := adminArchitectureProjectValuesFromForm(form)
	input, validationErrors := validateAdminArchitectureProjectFormValues(values)
	detailPath := adminArchitectureProjectPath(projectID)
	editPath := detailPath + "/edit"
	if validationErrors != (adminArchitectureProjectFormErrors{}) {
		app.renderAdminArchitectureProjectFormResponse(
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
	if app == nil || app.adminArchitectureProjectWrites == nil {
		log.Print("admin Architecture project writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminArchitectureProjectWrites.Update(
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
		app.renderAdminArchitectureProjectConflict(
			w,
			requestIdentity,
			projectID,
			editPath,
			"",
			"",
		)

		return
	case errors.Is(err, errAdminArchitectureProjectSlugConflict):
		validationErrors.Slug = "That slug is already used by another Architecture project."
		app.renderAdminArchitectureProjectFormResponse(
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
		log.Print("admin Architecture project update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, detailPath, http.StatusSeeOther)
}

// adminArchitectureProjectMutationRequestIsCanonical rejects alternate paths,
// queries, and content codings before the strict form parser reads a body.
func adminArchitectureProjectMutationRequestIsCanonical(
	w http.ResponseWriter,
	r *http.Request,
	canonicalPath string,
) bool {
	if r.URL.EscapedPath() != canonicalPath ||
		r.URL.ForceQuery || r.URL.RawQuery != "" {
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			http.Error(w, "invalid Architecture project request", http.StatusBadRequest)
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

// canonicalAdminArchitectureProjectWritePath validates one positive ID and the
// exact optional suffix used by edit-form and update routes.
func canonicalAdminArchitectureProjectWritePath(
	r *http.Request,
	suffix string,
) (int64, string, bool) {
	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminArchitectureProjectPath(projectID) + suffix
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return projectID, canonicalPath, true
}

// adminArchitectureProjectValuesFromForm copies only visible controls from an
// already cardinality-checked form.
func adminArchitectureProjectValuesFromForm(
	form mapFormValues,
) adminArchitectureProjectFormValues {
	return adminArchitectureProjectFormValues{
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

// validateAdminArchitectureProjectFormValues converts visible strings into the
// narrow writer input without normalizing invalid content into another value.
func validateAdminArchitectureProjectFormValues(
	values adminArchitectureProjectFormValues,
) (adminArchitectureProjectWriteInput, adminArchitectureProjectFormErrors) {
	var validationErrors adminArchitectureProjectFormErrors
	if !isCanonicalArchitectureProjectSlug(values.Slug) {
		validationErrors.Slug = "Use 1 to 120 lowercase letters or numbers, separated only by single hyphens."
	}
	if !isValidArchitectureProjectCatalogueText(
		values.Title,
		architectureProjectTitleMaximumLength,
	) {
		validationErrors.Title = "Enter a trimmed project title between 1 and 160 characters."
	}
	if !isValidArchitectureProjectCatalogueText(
		values.Typology,
		architectureProjectTypologyMaximumLength,
	) {
		validationErrors.Typology = "Enter a trimmed typology between 1 and 80 characters."
	}
	if values.Location != "" && !isValidArchitectureProjectCatalogueText(
		values.Location,
		architectureProjectLocationMaximumLength,
	) {
		validationErrors.Location = "Use at most 160 characters and remove leading or trailing whitespace."
	}

	projectYear := 0
	if values.ProjectYear != "" {
		parsedYear, valid := parseCanonicalPositiveInt64(values.ProjectYear)
		if !valid || parsedYear > math.MaxInt ||
			!isValidArchitectureProjectYear(int(parsedYear)) {
			validationErrors.ProjectYear = "Enter a four-digit year from 1000 to 9999, or leave it empty."
		} else {
			projectYear = int(parsedYear)
		}
	}
	if !isValidArchitectureProjectCatalogueText(
		values.ProjectStatus,
		architectureProjectStatusMaximumLength,
	) {
		validationErrors.ProjectStatus = "Enter a trimmed project status between 1 and 80 characters."
	}
	if !isValidOptionalEditorialText(
		values.Description,
		architectureProjectDescriptionMaximumLength,
	) {
		validationErrors.Description = "Use at most 6000 characters and remove leading or trailing whitespace."
	}

	sortOrder64, valid := parseCanonicalPositiveInt64(values.SortOrder)
	if !valid || sortOrder64 > math.MaxInt32 {
		validationErrors.SortOrder = "Enter a whole number from 1 to 2147483647."
	}
	if !isValidArchitectureProjectPublicationStatus(values.PublicationStatus) {
		validationErrors.PublicationStatus = "Choose Draft, Published, or Archived."
	}

	if validationErrors != (adminArchitectureProjectFormErrors{}) {
		return adminArchitectureProjectWriteInput{}, validationErrors
	}

	return adminArchitectureProjectWriteInput{
		Slug:              values.Slug,
		Title:             values.Title,
		Typology:          values.Typology,
		Location:          values.Location,
		ProjectYear:       projectYear,
		ProjectStatus:     values.ProjectStatus,
		Description:       values.Description,
		SortOrder:         int(sortOrder64),
		PublicationStatus: values.PublicationStatus,
	}, adminArchitectureProjectFormErrors{}
}

// newAdminArchitectureProjectFormPageData builds trusted labels, paths, and options
// around escaped visible values for either create or edit mode.
func newAdminArchitectureProjectFormPageData(
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminArchitectureProjectFormValues,
	validationErrors adminArchitectureProjectFormErrors,
) adminArchitectureProjectFormPageData {
	form := adminArchitectureProjectFormPageData{
		Eyebrow:       "New Architecture record",
		Heading:       "Create Architecture project",
		Introduction:  "Create durable project facts, reviewed editorial content, and a fail-closed publication state.",
		Action:        action,
		CancelPath:    cancelPath,
		SubmitLabel:   "Create Architecture project",
		IsEdit:        isEdit,
		Version:       version,
		Values:        values,
		Errors:        validationErrors,
		HasErrors:     validationErrors != (adminArchitectureProjectFormErrors{}),
		StatusOptions: adminArchitectureProjectStatusOptions(values.PublicationStatus),
		HasValidStatus: isValidArchitectureProjectPublicationStatus(
			values.PublicationStatus,
		),
	}
	if isEdit {
		form.Eyebrow = "Architecture revision " + version
		form.Heading = "Edit Architecture project"
		form.Introduction = "Save deliberate project, editorial, or publication changes. A stale form cannot overwrite a newer revision."
		form.SubmitLabel = "Save Architecture project"
	}

	return form
}

// adminArchitectureProjectStatusOptions creates the closed select list and never
// turns an untrusted submitted value into option markup.
func adminArchitectureProjectStatusOptions(
	selected string,
) []adminArchitectureProjectStatusOptionPageData {
	return []adminArchitectureProjectStatusOptionPageData{
		{Value: draftArchitectureProjectStatus, Label: "Draft", Selected: selected == draftArchitectureProjectStatus},
		{Value: publishedArchitectureProjectStatus, Label: "Published", Selected: selected == publishedArchitectureProjectStatus},
		{Value: archivedArchitectureProjectStatus, Label: "Archived", Selected: selected == archivedArchitectureProjectStatus},
	}
}

// renderAdminArchitectureProjectFormResponse reuses one form contract for semantic
// validation and safe slug conflicts while preserving the authenticated shell.
func (app *application) renderAdminArchitectureProjectFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminArchitectureProjectFormValues,
	validationErrors adminArchitectureProjectFormErrors,
) {
	form := newAdminArchitectureProjectFormPageData(
		isEdit,
		action,
		cancelPath,
		version,
		values,
		validationErrors,
	)
	title := "Create Architecture project"
	if isEdit {
		title = "Edit Architecture project"
	}
	data := newAuthenticatedAdminPageData(
		title,
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectForm = &form

	app.renderAdmin(w, status, "architecture-project-form.html", data)
}

// renderAdminArchitectureProjectConflict builds the fixed non-echoing stale-write
// page shared by text and cover updates.
func (app *application) renderAdminArchitectureProjectConflict(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	projectID int64,
	editPath string,
	actionLabel string,
	guidance string,
) {
	detailPath := adminArchitectureProjectPath(projectID)
	data := newAuthenticatedAdminPageData(
		"Architecture project changed",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectConflict = &adminArchitectureProjectConflictPageData{
		DetailPath:  detailPath,
		EditPath:    editPath,
		ActionLabel: actionLabel,
		Guidance:    guidance,
	}

	app.renderAdmin(
		w,
		http.StatusConflict,
		"architecture-project-conflict.html",
		data,
	)
}
