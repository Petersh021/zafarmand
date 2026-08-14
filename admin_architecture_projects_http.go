package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Protected Architecture pages share canonical navigation and timestamp
// presentation values so list and detail templates cannot drift.
const (
	// adminArchitectureProjectNavigationPath is the canonical collection route and
	// trusted active-navigation value for every protected Architecture page.
	adminArchitectureProjectNavigationPath = "/admin/architecture-projects"
	// adminArchitectureProjectTimeLayout is display-ready UTC interface text.
	adminArchitectureProjectTimeLayout = "02 Jan 2006, 15:04 UTC"
)

// Application construction errors make missing protected Architecture dependencies
// visible before the server accepts requests.
var (
	// errAdminArchitectureProjectReaderRequired rejects application construction
	// without the all-state project and exact-cover read dependency.
	errAdminArchitectureProjectReaderRequired = errors.New(
		"create application: admin architecture project reader is required",
	)
	// errAdminArchitectureProjectWriterRequired rejects application construction
	// without the explicit text and cover mutation dependency.
	errAdminArchitectureProjectWriterRequired = errors.New(
		"create application: admin architecture project writer is required",
	)
)

// adminArchitectureProjectListPageData is the complete protected list template
// contract. An empty slice represents a successful empty database.
type adminArchitectureProjectListPageData struct {
	// NewPath is the trusted protected route for creating a project.
	NewPath string
	// Items contains every validated lifecycle state in editorial order.
	Items []adminArchitectureProjectSummaryPageData
	// EmptyMessage explains the normal zero-row state.
	EmptyMessage string
}

// adminArchitectureProjectSummaryPageData contains display-ready values for one
// protected list card.
type adminArchitectureProjectSummaryPageData struct {
	// Reference is a stable administrator label derived from internal ID.
	Reference string
	// Path is the canonical protected detail URL.
	Path string
	// Title is escaped contextually by html/template.
	Title string
	// Slug is shown so administrators can review the public route spelling.
	Slug string
	// Typology is the stored Architecture category.
	Typology string
	// SortOrder is formatted base-10 interface text.
	SortOrder string
	// StatusLabel is trusted visible publication-state text.
	StatusLabel string
	// StatusClass is a trusted CSS modifier selected by Go.
	StatusClass string
	// UpdatedAtISO is the machine-readable UTC timestamp.
	UpdatedAtISO string
	// UpdatedAtLabel is concise human-readable UTC text.
	UpdatedAtLabel string
}

// architectureProjectCoverPageData is the binary-free protected image-preview
// contract. Exact cover bytes remain on the authenticated media route.
type architectureProjectCoverPageData struct {
	// Path is a trusted exact-revision media URL.
	Path string
	// AltText is the required reviewed image alternative.
	AltText string
	// Caption is optional visible cover copy.
	Caption string
	// Width reserves horizontal layout space.
	Width string
	// Height reserves vertical layout space.
	Height string
}

// adminArchitectureProjectDetailPageData contains protected record facts and
// trusted navigation. Every mutation remains on a separate native form.
type adminArchitectureProjectDetailPageData struct {
	// Reference is the stable administrator label derived from internal ID.
	Reference string
	// EditPath opens the canonical current-record form.
	EditPath string
	// Title is the page's primary heading.
	Title string
	// Slug is the stored canonical route segment.
	Slug string
	// Typology is the required reviewed Architecture category.
	Typology string
	// Location is optional reviewed geographic text.
	Location string
	// ProjectYear is an optional display-ready four-digit year.
	ProjectYear string
	// ProjectStatus is the real-world project state, distinct from publication.
	ProjectStatus string
	// Description is optional reviewed long-form public copy.
	Description string
	// SortOrder is formatted base-10 interface text.
	SortOrder string
	// Version is the positive revision shown as concurrency context.
	Version string
	// StatusLabel is trusted visible publication-state text.
	StatusLabel string
	// StatusClass is a trusted CSS modifier selected by Go.
	StatusClass string
	// VisibilityMessage explains the public consequence of the current lifecycle.
	VisibilityMessage string
	// PublicPath is present only while this project is Published.
	PublicPath string
	// Cover contains protected preview metadata, or nil when absent.
	Cover *architectureProjectCoverPageData
	// CoverManagementPath opens the authenticated upload-or-replace form.
	CoverManagementPath string
	// CreatedAtISO is the machine-readable UTC creation timestamp.
	CreatedAtISO string
	// CreatedAtLabel is concise human-readable UTC creation text.
	CreatedAtLabel string
	// UpdatedAtISO is the machine-readable UTC update timestamp.
	UpdatedAtISO string
	// UpdatedAtLabel is concise human-readable UTC update text.
	UpdatedAtLabel string
}

// adminArchitectureProjectListHandler reads every lifecycle state for an
// authenticated authorized administrator and renders the isolated workspace.
func (app *application) adminArchitectureProjectListHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminArchitectureProjectNavigationPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project catalogue request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminArchitectureProjects == nil {
		log.Print("admin Architecture project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	projects, err := app.adminArchitectureProjects.List(ctx)
	cancel()
	if err != nil || !isValidAdminArchitectureProjectList(projects) {
		log.Print("admin Architecture project list failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	items := make([]adminArchitectureProjectSummaryPageData, 0, len(projects))
	for _, project := range projects {
		item, valid := newAdminArchitectureProjectSummaryPageData(project)
		if !valid {
			log.Print("admin Architecture project list mapping failed")
			http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

			return
		}
		items = append(items, item)
	}

	data := newAuthenticatedAdminPageData(
		"Architecture projects",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectList = &adminArchitectureProjectListPageData{
		NewPath:      adminArchitectureProjectNewPath,
		Items:        items,
		EmptyMessage: "No Architecture projects have been created yet.",
	}

	app.renderAdmin(w, http.StatusOK, "architecture-projects.html", data)
}

// adminArchitectureProjectDetailHandler accepts one canonical positive ID after
// shared authentication and explicit role authorization have run.
func (app *application) adminArchitectureProjectDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Architecture project request", http.StatusBadRequest)

		return
	}

	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	canonicalPath := adminArchitectureProjectPath(projectID)
	if r.URL.EscapedPath() != canonicalPath {
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
	if err != nil {
		log.Print("admin Architecture project detail failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	detail, valid := newAdminArchitectureProjectDetailPageData(project)
	if !valid || project.ID != projectID {
		log.Print("admin Architecture project detail mapping failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Architecture project detail",
		adminArchitectureProjectNavigationPath,
		requestIdentity,
	)
	data.ArchitectureProjectDetail = &detail

	app.renderAdmin(w, http.StatusOK, "architecture-project-detail.html", data)
}

// isValidAdminArchitectureProjectList rechecks order, identity, slug uniqueness,
// and stored-field validity at the handler-facing interface boundary.
func isValidAdminArchitectureProjectList(
	projects []adminArchitectureProjectRecord,
) bool {
	seenIDs := make(map[int64]struct{}, len(projects))
	seenSlugs := make(map[string]struct{}, len(projects))
	var previous adminArchitectureProjectRecord
	for index, project := range projects {
		if !isValidStoredAdminArchitectureProject(project) ||
			(index > 0 && !adminArchitectureProjectFollows(project, previous)) {
			return false
		}
		if _, exists := seenIDs[project.ID]; exists {
			return false
		}
		if _, exists := seenSlugs[project.Slug]; exists {
			return false
		}

		seenIDs[project.ID] = struct{}{}
		seenSlugs[project.Slug] = struct{}{}
		previous = project
	}

	return true
}

// newAdminArchitectureProjectSummaryPageData converts one validated record into
// escaped text plus trusted display-only values.
func newAdminArchitectureProjectSummaryPageData(
	project adminArchitectureProjectRecord,
) (adminArchitectureProjectSummaryPageData, bool) {
	if !isValidStoredAdminArchitectureProject(project) {
		return adminArchitectureProjectSummaryPageData{}, false
	}

	statusLabel, statusClass, _, valid := adminArchitectureProjectStatusPresentation(
		project.PublicationStatus,
	)
	if !valid {
		return adminArchitectureProjectSummaryPageData{}, false
	}
	updatedAt := project.UpdatedAt.UTC()

	return adminArchitectureProjectSummaryPageData{
		Reference:      adminArchitectureProjectReference(project.ID),
		Path:           adminArchitectureProjectPath(project.ID),
		Title:          project.Title,
		Slug:           project.Slug,
		Typology:       project.Typology,
		SortOrder:      strconv.Itoa(project.SortOrder),
		StatusLabel:    statusLabel,
		StatusClass:    statusClass,
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminArchitectureProjectTimeLayout),
	}, true
}

// newAdminArchitectureProjectDetailPageData maps one validated record into the
// complete protected read view and trusted management paths.
func newAdminArchitectureProjectDetailPageData(
	project adminArchitectureProjectRecord,
) (adminArchitectureProjectDetailPageData, bool) {
	if !isValidStoredAdminArchitectureProject(project) {
		return adminArchitectureProjectDetailPageData{}, false
	}

	statusLabel, statusClass, visibilityMessage, valid :=
		adminArchitectureProjectStatusPresentation(project.PublicationStatus)
	if !valid {
		return adminArchitectureProjectDetailPageData{}, false
	}
	createdAt := project.CreatedAt.UTC()
	updatedAt := project.UpdatedAt.UTC()
	detail := adminArchitectureProjectDetailPageData{
		Reference:         adminArchitectureProjectReference(project.ID),
		EditPath:          adminArchitectureProjectPath(project.ID) + "/edit",
		Title:             project.Title,
		Slug:              project.Slug,
		Typology:          project.Typology,
		Location:          project.Location,
		ProjectStatus:     project.ProjectStatus,
		Description:       project.Description,
		SortOrder:         strconv.Itoa(project.SortOrder),
		Version:           strconv.FormatInt(project.Version, 10),
		StatusLabel:       statusLabel,
		StatusClass:       statusClass,
		VisibilityMessage: visibilityMessage,
		CoverManagementPath: adminArchitectureProjectCoverPath(
			project.ID,
		),
		CreatedAtISO:   createdAt.Format(time.RFC3339),
		CreatedAtLabel: createdAt.Format(adminArchitectureProjectTimeLayout),
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminArchitectureProjectTimeLayout),
	}
	if project.ProjectYear != 0 {
		detail.ProjectYear = strconv.Itoa(project.ProjectYear)
	}
	if project.PublicationStatus == publishedArchitectureProjectStatus {
		detail.PublicPath = architectureProjectDetailPath(project.Slug)
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
		detail.Cover = &cover
	}

	return detail, true
}

// adminArchitectureProjectStatusPresentation maps one stored lifecycle value to
// trusted visible text, a CSS modifier, and an explicit visibility explanation.
func adminArchitectureProjectStatusPresentation(
	status string,
) (string, string, string, bool) {
	switch status {
	case draftArchitectureProjectStatus:
		return "Draft", "draft", "This Architecture project is not visible on the public website.", true
	case publishedArchitectureProjectStatus:
		return "Published", "published", "This Architecture project is visible in the public portfolio.", true
	case archivedArchitectureProjectStatus:
		return "Archived", "archived", "This Architecture project remains stored but is hidden from the public website.", true
	default:
		return "", "", "", false
	}
}

// adminArchitectureProjectReference formats a positive internal identity as concise
// administrator-facing text.
func adminArchitectureProjectReference(projectID int64) string {
	return fmt.Sprintf("A-%04d", projectID)
}

// adminArchitectureProjectPath builds the canonical protected detail URL from a
// validated positive identity.
func adminArchitectureProjectPath(projectID int64) string {
	return adminArchitectureProjectNavigationPath + "/" +
		strconv.FormatInt(projectID, 10)
}
