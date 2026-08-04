package main

// architectureProject is the temporary application-level source record used
// by the Architecture Design portfolio listing.
//
// The type intentionally contains only the truthful structural fields required
// by Stage 10. Slugs, routes, locations, years, descriptions, images, database
// identifiers, and publishing controls remain deferred. The application owns
// the ordered slice, and handlers treat it as read-only.
type architectureProject struct {
	// Number is the zero-padded editorial order shared with the preview article.
	Number string
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
			Title:    "Architecture Study 01",
			Typology: "Residential",
			Status:   "Portfolio preview",
		},
		{
			Number:   "02",
			Title:    "Architecture Study 02",
			Typology: "Commercial",
			Status:   "Portfolio preview",
		},
		{
			Number:   "03",
			Title:    "Architecture Study 03",
			Typology: "Cultural",
			Status:   "Portfolio preview",
		},
		{
			Number:   "04",
			Title:    "Architecture Study 04",
			Typology: "Civic",
			Status:   "Portfolio preview",
		},
	}
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
		}
	}

	return previews
}
