package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Public Architecture reads use one bounded timeout shared by HTML and media
// handlers so a stalled dependency cannot hold a request indefinitely.
const (
	// architectureProjectCatalogueReadTimeout prevents an unavailable public
	// repository from holding an Architecture HTML or cover request indefinitely.
	architectureProjectCatalogueReadTimeout = 5 * time.Second
)

// errArchitectureProjectCatalogueReaderRequired prevents server construction
// with public Architecture routes that cannot answer from durable published state.
var errArchitectureProjectCatalogueReaderRequired = errors.New(
	"create application: architecture project catalogue reader is required",
)

// architectureProjectDetailPath builds the canonical public HTML path for one
// validated Architecture Design project slug.
//
// Keeping this prefix in Go prevents list cards and detail handlers from
// drifting to different route shapes. Callers must validate visitor-controlled
// slugs before using the returned path as a canonical destination.
func architectureProjectDetailPath(slug string) string {
	return "/architecture-design/" + slug
}

// architectureProjectCoverPath builds the revision-specific public cover URL
// for one canonical Architecture project slug and positive cover version.
func architectureProjectCoverPath(slug string, version int64) string {
	return architectureProjectDetailPath(slug) +
		"/cover/" + strconv.FormatInt(version, 10)
}

// formatArchitectureProjectNumber converts PostgreSQL's one-based published
// window position into the editorial label shared by list and detail pages.
// Two digits are a minimum width, so a catalogue larger than 99 is not cut off.
func formatArchitectureProjectNumber(number int64) string {
	return fmt.Sprintf("%02d", number)
}

// formatArchitectureProjectYear turns the repository's zero representation of
// SQL NULL into an omitted template value and formats a real four-digit year.
func formatArchitectureProjectYear(year int) string {
	if year == 0 {
		return ""
	}

	return strconv.Itoa(year)
}

// newPublicArchitectureProjectCoverPageData maps reviewed metadata to one
// current public URL. A nil cover remains nil so templates can render an honest
// decorative fallback without constructing a nonexistent image request.
func newPublicArchitectureProjectCoverPageData(
	slug string,
	cover *architectureProjectCoverMetadata,
) *publicArchitectureProjectCoverPageData {
	if cover == nil {
		return nil
	}

	return &publicArchitectureProjectCoverPageData{
		Path:    architectureProjectCoverPath(slug, cover.Version),
		Width:   cover.Width,
		Height:  cover.Height,
		AltText: cover.AltText,
		Caption: cover.Caption,
	}
}

// architectureProjectPreviews maps ordered public repository records into the
// smaller presentation contract consumed by architecture-design.html.
//
// The repository has already filtered lifecycle state and established each
// consecutive PortfolioNumber. This mapper formats that number and derives the
// trusted path without exposing database identity or publication controls.
func architectureProjectPreviews(
	projects []catalogueArchitectureProject,
) []architectureProjectPreviewData {
	previews := make([]architectureProjectPreviewData, len(projects))

	for index, project := range projects {
		previews[index] = architectureProjectPreviewData{
			Number:        formatArchitectureProjectNumber(project.PortfolioNumber),
			Title:         project.Title,
			Typology:      project.Typology,
			Location:      project.Location,
			YearLabel:     formatArchitectureProjectYear(project.ProjectYear),
			ProjectStatus: project.ProjectStatus,
			Path:          architectureProjectDetailPath(project.Slug),
			Cover: newPublicArchitectureProjectCoverPageData(
				project.Slug,
				project.Cover,
			),
		}
	}

	return previews
}

// architectureReferenceProjectPreviews returns fresh presentation values for
// the four approved concept cards stored in the public Architecture asset tree.
//
// Returning a new slice prevents callers from mutating package-level state.
// The cards remain non-interactive and disappear as soon as the published-only
// repository projection contains at least one real project.
func architectureReferenceProjectPreviews() []architectureReferenceProjectPreviewData {
	return []architectureReferenceProjectPreviewData{
		{
			Title:     "Mountain House",
			Typology:  "Residential",
			ImagePath: "/static/images/architecture-design/mountain-house.jpg",
			Width:     1600,
			Height:    686,
			AltText: "Low pale-stone residence glowing at dusk against a dark " +
				"mountain landscape",
		},
		{
			Title:     "Terra Office Building",
			Typology:  "Commercial",
			ImagePath: "/static/images/architecture-design/terra-office-building.jpg",
			Width:     1200,
			Height:    900,
			AltText: "Dark multi-storey office building with staggered volumes " +
				"and illuminated glass façades",
		},
		{
			Title:     "Silk Museum",
			Typology:  "Cultural",
			ImagePath: "/static/images/architecture-design/silk-museum.jpg",
			Width:     1200,
			Height:    900,
			AltText: "Pale monolithic museum with rhythmic vertical fins above " +
				"a recessed glass entrance",
		},
		{
			Title:     "Coastal Retreat",
			Typology:  "Residential",
			ImagePath: "/static/images/architecture-design/coastal-retreat.jpg",
			Width:     1200,
			Height:    900,
			AltText: "Angular two-storey residence with broad warm-lit windows " +
				"in a quiet dusk landscape",
		},
	}
}

// newArchitectureProjectDetailData maps one validated public repository record
// to the narrow detail-template contract. Routing identity stays in the
// handler, while this conversion makes every presented field an explicit choice.
func newArchitectureProjectDetailData(
	project catalogueArchitectureProject,
) architectureProjectDetailData {
	return architectureProjectDetailData{
		Number:        formatArchitectureProjectNumber(project.PortfolioNumber),
		Title:         project.Title,
		Typology:      project.Typology,
		Location:      project.Location,
		YearLabel:     formatArchitectureProjectYear(project.ProjectYear),
		ProjectStatus: project.ProjectStatus,
		Description:   project.Description,
		Cover: newPublicArchitectureProjectCoverPageData(
			project.Slug,
			project.Cover,
		),
	}
}
