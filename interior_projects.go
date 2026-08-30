package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// interiorProjectCatalogueReadTimeout prevents an unavailable public
	// repository from holding an Interior HTML or cover request indefinitely.
	interiorProjectCatalogueReadTimeout = 5 * time.Second
)

// errInteriorProjectCatalogueReaderRequired prevents server construction with
// public Interior routes that cannot answer from durable published state.
var errInteriorProjectCatalogueReaderRequired = errors.New(
	"create application: interior project catalogue reader is required",
)

// interiorProjectDetailPath builds the canonical public HTML path for one
// validated Interior Design project slug.
//
// Keeping this prefix in Go prevents list cards and detail handlers from
// drifting to different route shapes. Callers must validate visitor-controlled
// slugs before using the returned path as a canonical destination.
func interiorProjectDetailPath(slug string) string {
	return "/interior-design/" + slug
}

// interiorProjectCoverPath builds the revision-specific public cover URL for
// one canonical Interior project slug and positive cover version.
func interiorProjectCoverPath(slug string, version int64) string {
	return interiorProjectDetailPath(slug) +
		"/cover/" + strconv.FormatInt(version, 10)
}

// formatInteriorProjectNumber converts PostgreSQL's one-based published window
// position into the editorial label shared by list and detail pages. Two digits
// are a minimum width, so a real catalogue larger than 99 is never truncated.
func formatInteriorProjectNumber(number int64) string {
	return fmt.Sprintf("%02d", number)
}

// formatInteriorProjectYear turns the repository's zero representation of SQL
// NULL into an omitted template value and formats a real four-digit year.
func formatInteriorProjectYear(year int) string {
	if year == 0 {
		return ""
	}

	return strconv.Itoa(year)
}

// newPublicInteriorProjectCoverPageData maps reviewed metadata to one current
// public URL. A nil cover remains nil so templates can render an honest
// decorative fallback without constructing a nonexistent image request.
func newPublicInteriorProjectCoverPageData(
	slug string,
	cover *interiorProjectCoverMetadata,
) *publicInteriorProjectCoverPageData {
	if cover == nil {
		return nil
	}

	return &publicInteriorProjectCoverPageData{
		Path:    interiorProjectCoverPath(slug, cover.Version),
		Width:   cover.Width,
		Height:  cover.Height,
		AltText: cover.AltText,
		Caption: cover.Caption,
	}
}

// isValidPublishedInteriorProjectCatalogue verifies the complete result from
// any injected public reader before a handler maps it into HTML. PostgreSQL
// performs the same checks while scanning, but this application boundary also
// makes substituted and future implementations fail closed.
func isValidPublishedInteriorProjectCatalogue(
	projects []catalogueInteriorProject,
) bool {
	seenIDs := make(map[int64]struct{}, len(projects))
	seenSlugs := make(map[string]struct{}, len(projects))

	for index, project := range projects {
		if !isValidCatalogueInteriorProject(project) ||
			project.PortfolioNumber != int64(index+1) {
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
	}

	return true
}

// interiorProjectPreviews maps ordered public repository records into the
// smaller presentation contract consumed by interior-design.html.
//
// The repository has already filtered lifecycle state and established each
// consecutive PortfolioNumber. This mapper formats that number and derives the
// trusted path without exposing database identity or publication controls.
func interiorProjectPreviews(
	projects []catalogueInteriorProject,
) []interiorProjectPreviewData {
	previews := make([]interiorProjectPreviewData, len(projects))

	for index, project := range projects {
		previews[index] = interiorProjectPreviewData{
			Number:        formatInteriorProjectNumber(project.PortfolioNumber),
			Title:         project.Title,
			Typology:      project.Typology,
			Location:      project.Location,
			YearLabel:     formatInteriorProjectYear(project.ProjectYear),
			ProjectStatus: project.ProjectStatus,
			Path:          interiorProjectDetailPath(project.Slug),
			Cover: newPublicInteriorProjectCoverPageData(
				project.Slug,
				project.Cover,
			),
		}
	}

	return previews
}

// interiorReferenceProjectPreviews returns fresh presentation values for the
// four approved concept cards copied into the public static asset tree.
//
// A function avoids exposing a mutable package-level slice. These previews are
// deliberately non-interactive and are replaced by the published-only database
// projection as soon as at least one real public project exists.
func interiorReferenceProjectPreviews() []interiorReferenceProjectPreviewData {
	return []interiorReferenceProjectPreviewData{
		{
			Title:     "Hillside Residence",
			Typology:  "Residential",
			ImagePath: "/static/images/interior-design/hillside-residence.jpg",
			Width:     1681,
			Height:    936,
			AltText: "Warm living room overlooking a wooded hillside through " +
				"a tall central window",
		},
		{
			Title:     "Karimi Apartment",
			Typology:  "Residential",
			ImagePath: "/static/images/interior-design/karimi-apartment.jpg",
			Width:     1678,
			Height:    937,
			AltText: "Low-lit apartment living area beside a dark timber " +
				"kitchen and full-height window",
		},
		{
			Title:     "Noor Office",
			Typology:  "Commercial",
			ImagePath: "/static/images/interior-design/noor-office.jpg",
			Width:     1681,
			Height:    936,
			AltText: "Dark open office with shared tables, a window wall, and " +
				"warm perimeter lighting",
		},
		{
			Title:     "Atrium Lobby",
			Typology:  "Hospitality",
			ImagePath: "/static/images/interior-design/atrium-lobby.jpg",
			Width:     1679,
			Height:    937,
			AltText: "Enclosed lobby lounge with curved seating, indoor trees, " +
				"and a garden-facing glass wall",
		},
	}
}

// newInteriorProjectDetailData maps one validated public repository record to
// the narrow detail-template contract. Routing identity stays in the handler,
// while this conversion makes every newly presented field an explicit choice.
func newInteriorProjectDetailData(
	project catalogueInteriorProject,
) interiorProjectDetailData {
	return interiorProjectDetailData{
		Number:        formatInteriorProjectNumber(project.PortfolioNumber),
		Title:         project.Title,
		Typology:      project.Typology,
		Location:      project.Location,
		YearLabel:     formatInteriorProjectYear(project.ProjectYear),
		ProjectStatus: project.ProjectStatus,
		Description:   project.Description,
		Cover: newPublicInteriorProjectCoverPageData(
			project.Slug,
			project.Cover,
		),
	}
}
