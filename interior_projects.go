package main

// interiorProject is the temporary application-level source record used by
// the Interior Design portfolio listing.
//
// The source is deliberately smaller than the future database Project model.
// Stage 8 needs only truthful structural fields for a listing, so slugs, routes,
// locations, years, descriptions, images, publication controls, and database
// identifiers remain deferred. application.interiorProjects owns the ordered
// slice and handlers treat it as read-only.
type interiorProject struct {
	// Number is the zero-padded editorial order shared with the preview card.
	Number string
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
			Title:    "Interior Study 01",
			Typology: "Residential",
			Status:   "Portfolio preview",
		},
		{
			Number:   "02",
			Title:    "Interior Study 02",
			Typology: "Hospitality",
			Status:   "Portfolio preview",
		},
		{
			Number:   "03",
			Title:    "Interior Study 03",
			Typology: "Workplace",
			Status:   "Portfolio preview",
		},
		{
			Number:   "04",
			Title:    "Interior Study 04",
			Typology: "Cultural",
			Status:   "Portfolio preview",
		},
	}
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
		}
	}

	return previews
}
