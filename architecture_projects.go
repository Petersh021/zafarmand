package main

// architectureProject is the temporary application-level source record used
// by the Architecture Design portfolio listing and detail routes.
//
// Stage 11 adds only the trusted slug needed for real detail URLs. Locations,
// years, descriptions, images, database identifiers, and publishing controls
// remain deferred. The application owns the ordered slice, and handlers treat
// it as read-only.
type architectureProject struct {
	// Number is the zero-padded editorial order shared with the preview article.
	Number string
	// Slug is the exact case-sensitive path value accepted after /architecture-design/.
	Slug string
	// Title is an explicitly temporary study name rather than final project copy.
	Title string
	// Typology identifies the broad architecture category represented by the slot.
	Typology string
	// Status communicates that approved project material is still pending.
	Status string
}

// temporaryArchitectureProjects returns the ordered Architecture Design source
// records used until PostgreSQL and the admin publishing workflow are added.
//
// Returning a fresh slice during application construction avoids mutable
// package-level state. Neutral study titles distinguish learning data from
// approved Zafarmand projects while still exercising the complete data flow.
func temporaryArchitectureProjects() []architectureProject {
	return []architectureProject{
		{
			Number:   "01",
			Slug:     "architecture-study-01",
			Title:    "Architecture Study 01",
			Typology: "Residential",
			Status:   "Portfolio preview",
		},
		{
			Number:   "02",
			Slug:     "architecture-study-02",
			Title:    "Architecture Study 02",
			Typology: "Commercial",
			Status:   "Portfolio preview",
		},
		{
			Number:   "03",
			Slug:     "architecture-study-03",
			Title:    "Architecture Study 03",
			Typology: "Cultural",
			Status:   "Portfolio preview",
		},
		{
			Number:   "04",
			Slug:     "architecture-study-04",
			Title:    "Architecture Study 04",
			Typology: "Civic",
			Status:   "Portfolio preview",
		},
	}
}

// architectureProjectDetailPath builds the canonical public URL for one
// trusted Architecture Design project slug.
//
// Slugs originate from temporaryArchitectureProjects rather than visitor
// input. Centralizing the route prefix prevents listing links and the detail
// handler from constructing different destinations.
func architectureProjectDetailPath(slug string) string {
	return "/architecture-design/" + slug
}

// architectureProjectPreviews maps ordered source records into the narrower
// view model consumed by architecture-design.html.
//
// Allocating the result at the source length preserves editorial order and
// makes nil or empty input naturally activate the template's truthful empty
// state. The explicit mapping prevents future persistence fields from leaking
// into the public template contract.
func architectureProjectPreviews(
	projects []architectureProject,
) []architectureProjectPreviewData {
	previews := make(
		[]architectureProjectPreviewData,
		len(projects),
	)

	for index, project := range projects {
		previews[index] = architectureProjectPreviewData{
			Number:   project.Number,
			Title:    project.Title,
			Typology: project.Typology,
			Status:   project.Status,
			Path:     architectureProjectDetailPath(project.Slug),
		}
	}

	return previews
}

// findArchitectureProjectBySlug performs an exact, case-sensitive lookup in
// the ordered temporary Architecture Design source.
//
// A linear scan is clear and sufficient for four in-memory records. Returning
// a boolean separately from the record distinguishes a real zero-value project
// from a missing slug so the HTTP handler can return 404 before rendering.
func findArchitectureProjectBySlug(
	projects []architectureProject,
	slug string,
) (architectureProject, bool) {
	for _, project := range projects {
		if project.Slug == slug {
			return project, true
		}
	}

	return architectureProject{}, false
}

// newArchitectureProjectDetailData maps an application source record to the
// narrow view model consumed by architecture-project-detail.html.
//
// The explicit conversion keeps routing-only fields such as Slug out of the
// template contract and leaves a stable boundary for a future repository or
// database record to populate.
func newArchitectureProjectDetailData(
	project architectureProject,
) architectureProjectDetailData {
	return architectureProjectDetailData{
		Number:   project.Number,
		Title:    project.Title,
		Typology: project.Typology,
		Status:   project.Status,
	}
}
