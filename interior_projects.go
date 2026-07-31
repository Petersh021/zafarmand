package main

// interiorProject is the temporary application-level source record used by
// the Interior Design portfolio listing and detail routes.
//
// The source is deliberately smaller than the future database Project model.
// Stage 9 adds only the trusted slug needed for real detail URLs; locations,
// years, descriptions, images, publication controls, and database identifiers
// remain deferred. application.interiorProjects owns the ordered slice and
// handlers treat it as read-only.
type interiorProject struct {
	// Number is the zero-padded editorial order shared with the preview card.
	Number string
	// Slug is the exact case-sensitive path value accepted after /interior-design/.
	Slug string
	// Title is an explicitly temporary study name rather than final project copy.
	Title string
	// Typology identifies the broad interior category represented by the slot.
	Typology string
	// Status communicates that approved project material is still pending.
	Status string
}

// temporaryInteriorProjects returns the ordered Interior Design source records
// used until PostgreSQL and the admin publishing workflow are introduced.
//
// Returning a fresh slice during application construction prevents mutable
// package-level state. Neutral "Study" titles make the distinction between
// structural learning data and published Zafarmand projects explicit.
func temporaryInteriorProjects() []interiorProject {
	return []interiorProject{
		{
			Number:   "01",
			Slug:     "interior-study-01",
			Title:    "Interior Study 01",
			Typology: "Residential",
			Status:   "Portfolio preview",
		},
		{
			Number:   "02",
			Slug:     "interior-study-02",
			Title:    "Interior Study 02",
			Typology: "Hospitality",
			Status:   "Portfolio preview",
		},
		{
			Number:   "03",
			Slug:     "interior-study-03",
			Title:    "Interior Study 03",
			Typology: "Workplace",
			Status:   "Portfolio preview",
		},
		{
			Number:   "04",
			Slug:     "interior-study-04",
			Title:    "Interior Study 04",
			Typology: "Cultural",
			Status:   "Portfolio preview",
		},
	}
}

// interiorProjectDetailPath builds the canonical public URL for one trusted
// Interior Design project slug.
//
// Slugs originate from temporaryInteriorProjects rather than visitor input.
// Centralizing the prefix prevents listing links and the detail handler from
// accidentally constructing different routes.
func interiorProjectDetailPath(slug string) string {
	return "/interior-design/" + slug
}

// interiorProjectPreviews maps ordered source records into the narrower view
// model required by interior-design.html.
//
// Allocating the result at the source length preserves editorial order and
// makes nil or empty input naturally activate the template's truthful empty
// state. The mapping seam can later accept database records without exposing
// persistence fields directly to html/template.
func interiorProjectPreviews(
	projects []interiorProject,
) []interiorProjectPreviewData {
	previews := make(
		[]interiorProjectPreviewData,
		len(projects),
	)

	for index, project := range projects {
		previews[index] = interiorProjectPreviewData{
			Number:   project.Number,
			Title:    project.Title,
			Typology: project.Typology,
			Status:   project.Status,
			Path:     interiorProjectDetailPath(project.Slug),
		}
	}

	return previews
}

// findInteriorProjectBySlug performs an exact, case-sensitive lookup in the
// ordered temporary Interior Design source.
//
// A linear scan is clear and sufficient for four in-memory records. Returning a
// boolean separately from the record distinguishes a real zero-value project
// from a missing slug so the HTTP handler can respond with 404.
func findInteriorProjectBySlug(
	projects []interiorProject,
	slug string,
) (interiorProject, bool) {
	for _, project := range projects {
		if project.Slug == slug {
			return project, true
		}
	}

	return interiorProject{}, false
}

// newInteriorProjectDetailData maps an application source record to the narrow
// view model consumed by interior-project-detail.html.
//
// The explicit conversion keeps routing-only fields such as Slug out of the
// template contract and leaves a stable boundary for a future repository or
// database record to populate.
func newInteriorProjectDetailData(
	project interiorProject,
) interiorProjectDetailData {
	return interiorProjectDetailData{
		Number:   project.Number,
		Title:    project.Title,
		Typology: project.Typology,
		Status:   project.Status,
	}
}
