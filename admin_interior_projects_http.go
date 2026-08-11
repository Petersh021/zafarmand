package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

const (
	// adminInteriorProjectNavigationPath is the canonical collection route and
	// trusted active-navigation value for every protected Interior page.
	adminInteriorProjectNavigationPath = "/admin/interior-projects"
	// adminInteriorProjectTimeLayout is display-ready UTC interface text.
	adminInteriorProjectTimeLayout = "02 Jan 2006, 15:04 UTC"
)

// Application construction errors make missing protected Interior dependencies
// visible before the server accepts requests.
var (
	// errAdminInteriorProjectReaderRequired rejects application construction
	// without the all-state project and exact-cover read dependency.
	errAdminInteriorProjectReaderRequired = errors.New(
		"create application: admin interior project reader is required",
	)
	// errAdminInteriorProjectWriterRequired rejects application construction
	// without the explicit text and cover mutation dependency.
	errAdminInteriorProjectWriterRequired = errors.New(
		"create application: admin interior project writer is required",
	)
)

// adminInteriorProjectListPageData is the complete protected list template
// contract. An empty slice represents a successful empty database.
type adminInteriorProjectListPageData struct {
	// NewPath is the trusted protected route for creating a project.
	NewPath string
	// Items contains every validated lifecycle state in editorial order.
	Items []adminInteriorProjectSummaryPageData
	// EmptyMessage explains the normal zero-row state.
	EmptyMessage string
}

// adminInteriorProjectSummaryPageData contains display-ready values for one
// protected list card.
type adminInteriorProjectSummaryPageData struct {
	// Reference is a stable administrator label derived from internal ID.
	Reference string
	// Path is the canonical protected detail URL.
	Path string
	// Title is escaped contextually by html/template.
	Title string
	// Slug is shown so administrators can review the public route spelling.
	Slug string
	// Typology is the stored Interior category.
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

// interiorProjectCoverPageData is the binary-free protected image-preview
// contract. Exact cover bytes remain on the authenticated media route.
type interiorProjectCoverPageData struct {
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

// adminInteriorProjectDetailPageData contains protected record facts and
// trusted navigation. Every mutation remains on a separate native form.
type adminInteriorProjectDetailPageData struct {
	// Reference is the stable administrator label derived from internal ID.
	Reference string
	// EditPath opens the canonical current-record form.
	EditPath string
	// Title is the page's primary heading.
	Title string
	// Slug is the stored canonical route segment.
	Slug string
	// Typology is the required reviewed Interior category.
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
	Cover *interiorProjectCoverPageData
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

// adminInteriorProjectListHandler reads every lifecycle state for an
// authenticated authorized administrator and renders the isolated workspace.
func (app *application) adminInteriorProjectListHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminInteriorProjectNavigationPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project catalogue request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminInteriorProjects == nil {
		log.Print("admin Interior project reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	projects, err := app.adminInteriorProjects.List(ctx)
	cancel()
	if err != nil || !isValidAdminInteriorProjectList(projects) {
		log.Print("admin Interior project list failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	items := make([]adminInteriorProjectSummaryPageData, 0, len(projects))
	for _, project := range projects {
		item, valid := newAdminInteriorProjectSummaryPageData(project)
		if !valid {
			log.Print("admin Interior project list mapping failed")
			http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

			return
		}
		items = append(items, item)
	}

	data := newAuthenticatedAdminPageData(
		"Interior projects",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectList = &adminInteriorProjectListPageData{
		NewPath:      adminInteriorProjectNewPath,
		Items:        items,
		EmptyMessage: "No Interior projects have been created yet.",
	}

	app.renderAdmin(w, http.StatusOK, "interior-projects.html", data)
}

// adminInteriorProjectDetailHandler accepts one canonical positive ID after
// shared authentication and explicit role authorization have run.
func (app *application) adminInteriorProjectDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Interior project request", http.StatusBadRequest)

		return
	}

	projectID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	canonicalPath := adminInteriorProjectPath(projectID)
	if r.URL.EscapedPath() != canonicalPath {
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
	if err != nil {
		log.Print("admin Interior project detail failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	detail, valid := newAdminInteriorProjectDetailPageData(project)
	if !valid || project.ID != projectID {
		log.Print("admin Interior project detail mapping failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Interior project detail",
		adminInteriorProjectNavigationPath,
		requestIdentity,
	)
	data.InteriorProjectDetail = &detail

	app.renderAdmin(w, http.StatusOK, "interior-project-detail.html", data)
}

// isValidAdminInteriorProjectList rechecks order, identity, slug uniqueness,
// and stored-field validity at the handler-facing interface boundary.
func isValidAdminInteriorProjectList(
	projects []adminInteriorProjectRecord,
) bool {
	seenIDs := make(map[int64]struct{}, len(projects))
	seenSlugs := make(map[string]struct{}, len(projects))
	var previous adminInteriorProjectRecord
	for index, project := range projects {
		if !isValidStoredAdminInteriorProject(project) ||
			(index > 0 && !adminInteriorProjectFollows(project, previous)) {
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

// newAdminInteriorProjectSummaryPageData converts one validated record into
// escaped text plus trusted display-only values.
func newAdminInteriorProjectSummaryPageData(
	project adminInteriorProjectRecord,
) (adminInteriorProjectSummaryPageData, bool) {
	if !isValidStoredAdminInteriorProject(project) {
		return adminInteriorProjectSummaryPageData{}, false
	}

	statusLabel, statusClass, _, valid := adminInteriorProjectStatusPresentation(
		project.PublicationStatus,
	)
	if !valid {
		return adminInteriorProjectSummaryPageData{}, false
	}
	updatedAt := project.UpdatedAt.UTC()

	return adminInteriorProjectSummaryPageData{
		Reference:      adminInteriorProjectReference(project.ID),
		Path:           adminInteriorProjectPath(project.ID),
		Title:          project.Title,
		Slug:           project.Slug,
		Typology:       project.Typology,
		SortOrder:      strconv.Itoa(project.SortOrder),
		StatusLabel:    statusLabel,
		StatusClass:    statusClass,
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminInteriorProjectTimeLayout),
	}, true
}

// newAdminInteriorProjectDetailPageData maps one validated record into the
// complete protected read view and trusted management paths.
func newAdminInteriorProjectDetailPageData(
	project adminInteriorProjectRecord,
) (adminInteriorProjectDetailPageData, bool) {
	if !isValidStoredAdminInteriorProject(project) {
		return adminInteriorProjectDetailPageData{}, false
	}

	statusLabel, statusClass, visibilityMessage, valid :=
		adminInteriorProjectStatusPresentation(project.PublicationStatus)
	if !valid {
		return adminInteriorProjectDetailPageData{}, false
	}
	createdAt := project.CreatedAt.UTC()
	updatedAt := project.UpdatedAt.UTC()
	detail := adminInteriorProjectDetailPageData{
		Reference:         adminInteriorProjectReference(project.ID),
		EditPath:          adminInteriorProjectPath(project.ID) + "/edit",
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
		CoverManagementPath: adminInteriorProjectCoverPath(
			project.ID,
		),
		CreatedAtISO:   createdAt.Format(time.RFC3339),
		CreatedAtLabel: createdAt.Format(adminInteriorProjectTimeLayout),
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminInteriorProjectTimeLayout),
	}
	if project.ProjectYear != 0 {
		detail.ProjectYear = strconv.Itoa(project.ProjectYear)
	}
	if project.PublicationStatus == publishedInteriorProjectStatus {
		detail.PublicPath = interiorProjectDetailPath(project.Slug)
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
		detail.Cover = &cover
	}

	return detail, true
}

// adminInteriorProjectStatusPresentation maps one stored lifecycle value to
// trusted visible text, a CSS modifier, and an explicit visibility explanation.
func adminInteriorProjectStatusPresentation(
	status string,
) (string, string, string, bool) {
	switch status {
	case draftInteriorProjectStatus:
		return "Draft", "draft", "This Interior project is not visible on the public website.", true
	case publishedInteriorProjectStatus:
		return "Published", "published", "This Interior project is visible in the public portfolio.", true
	case archivedInteriorProjectStatus:
		return "Archived", "archived", "This Interior project remains stored but is hidden from the public website.", true
	default:
		return "", "", "", false
	}
}

// adminInteriorProjectReference formats a positive internal identity as concise
// administrator-facing text.
func adminInteriorProjectReference(projectID int64) string {
	return fmt.Sprintf("I-%04d", projectID)
}

// adminInteriorProjectPath builds the canonical protected detail URL from a
// validated positive identity.
func adminInteriorProjectPath(projectID int64) string {
	return adminInteriorProjectNavigationPath + "/" +
		strconv.FormatInt(projectID, 10)
}
