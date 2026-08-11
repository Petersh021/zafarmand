package main

import (
	"context"
	"sync"
	"time"
)

// recordingInteriorProjectCatalogueReader is the database-free public reader
// shared by application-construction and Interior HTTP tests. Its mutex keeps
// configurable results and call snapshots safe under future concurrent tests.
type recordingInteriorProjectCatalogueReader struct {
	// mu protects every field below.
	mu sync.Mutex
	// projects is copied on assignment and return.
	projects []catalogueInteriorProject
	// listErr is the configured ListPublished result error.
	listErr error
	// findErr is the configured FindPublishedBySlug result error.
	findErr error
	// findResult, when non-nil, bypasses fixture lookup so handler tests can
	// exercise a malformed or mismatched injected-reader contract.
	findResult *catalogueInteriorProject
	// coverAsset is the configured exact public cover.
	coverAsset interiorProjectCoverAsset
	// coverErr is the configured FindPublishedCover result error.
	coverErr error
	// coverResult, when non-nil, bypasses exact-version fixture lookup so an
	// HTTP test can prove the handler distrusts a malformed injected reader.
	coverResult *interiorProjectCoverAsset
	// listCalls records deadline properties for list requests.
	listCalls []recordingInteriorProjectListCall
	// findCalls records canonical detail coordinates and deadlines.
	findCalls []recordingInteriorProjectFindCall
	// coverCalls records exact media coordinates and deadlines.
	coverCalls []recordingInteriorProjectCoverFindCall
}

// recordingInteriorProjectListCall captures the handler-bounded read context.
type recordingInteriorProjectListCall struct {
	// Deadline is the absolute deadline when present.
	Deadline time.Time
	// HasDeadline distinguishes a real deadline from the zero time.
	HasDeadline bool
}

// recordingInteriorProjectFindCall captures one detail dependency call.
type recordingInteriorProjectFindCall struct {
	// Slug is the exact canonical path segment supplied by the handler.
	Slug string
	// Deadline is the absolute deadline when present.
	Deadline time.Time
	// HasDeadline distinguishes a real deadline from the zero time.
	HasDeadline bool
}

// recordingInteriorProjectCoverFindCall captures one public media lookup.
type recordingInteriorProjectCoverFindCall struct {
	// Slug is the canonical owning project path segment.
	Slug string
	// Version is the requested exact cover revision.
	Version int64
	// HasDeadline proves the handler bounded its read.
	HasDeadline bool
}

// newRecordingInteriorProjectCatalogueReader returns the four fictional
// structural records historically used by public UI tests. Production never
// calls this helper and migrations never seed these values.
func newRecordingInteriorProjectCatalogueReader() *recordingInteriorProjectCatalogueReader {
	return &recordingInteriorProjectCatalogueReader{
		projects: []catalogueInteriorProject{
			{
				ID:              1,
				PortfolioNumber: 1,
				Slug:            "interior-study-01",
				Title:           "Interior Study 01",
				Typology:        "Residential",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              2,
				PortfolioNumber: 2,
				Slug:            "interior-study-02",
				Title:           "Interior Study 02",
				Typology:        "Hospitality",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              3,
				PortfolioNumber: 3,
				Slug:            "interior-study-03",
				Title:           "Interior Study 03",
				Typology:        "Workplace",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              4,
				PortfolioNumber: 4,
				Slug:            "interior-study-04",
				Title:           "Interior Study 04",
				Typology:        "Cultural",
				ProjectStatus:   "Portfolio preview",
			},
		},
	}
}

// ListPublished returns an isolated configured result after recording whether
// the caller supplied a deadline.
func (reader *recordingInteriorProjectCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueInteriorProject, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.listCalls = append(
		reader.listCalls,
		recordingInteriorProjectListCall{
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)

	return cloneCatalogueInteriorProjects(reader.projects), reader.listErr
}

// FindPublishedBySlug returns one isolated matching fixture or the production
// not-found category after recording the exact call.
func (reader *recordingInteriorProjectCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueInteriorProject, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.findCalls = append(
		reader.findCalls,
		recordingInteriorProjectFindCall{
			Slug:        slug,
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)
	if reader.findErr != nil {
		return catalogueInteriorProject{}, reader.findErr
	}
	if reader.findResult != nil {
		return cloneCatalogueInteriorProject(*reader.findResult), nil
	}
	for _, project := range reader.projects {
		if project.Slug == slug {
			return cloneCatalogueInteriorProject(project), nil
		}
	}

	return catalogueInteriorProject{}, errInteriorProjectCatalogueNotFound
}

// setFindResult configures one explicit detail result. Passing nil restores
// exact fixture lookup behavior.
func (reader *recordingInteriorProjectCatalogueReader) setFindResult(
	project *catalogueInteriorProject,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if project == nil {
		reader.findResult = nil

		return
	}
	clone := cloneCatalogueInteriorProject(*project)
	reader.findResult = &clone
}

// FindPublishedCover returns an isolated exact configured asset or the public
// not-found category after recording its coordinates.
func (reader *recordingInteriorProjectCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (interiorProjectCoverAsset, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.coverCalls = append(
		reader.coverCalls,
		recordingInteriorProjectCoverFindCall{
			Slug:        slug,
			Version:     version,
			HasDeadline: hasDeadline,
		},
	)
	if reader.coverErr != nil {
		return interiorProjectCoverAsset{}, reader.coverErr
	}
	if reader.coverResult != nil {
		return cloneInteriorProjectCoverAsset(*reader.coverResult), nil
	}
	if reader.coverAsset.InteriorProjectID <= 0 ||
		reader.coverAsset.Version != version {
		return interiorProjectCoverAsset{}, errInteriorProjectCoverNotFound
	}

	return cloneInteriorProjectCoverAsset(reader.coverAsset), nil
}

// setProjects replaces public fixtures with a deep-enough isolated copy.
func (reader *recordingInteriorProjectCatalogueReader) setProjects(
	projects []catalogueInteriorProject,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.projects = cloneCatalogueInteriorProjects(projects)
}

// setErrors independently configures list and detail failures.
func (reader *recordingInteriorProjectCatalogueReader) setErrors(
	listErr error,
	findErr error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.listErr = listErr
	reader.findErr = findErr
}

// setCover configures one isolated public media outcome.
func (reader *recordingInteriorProjectCatalogueReader) setCover(
	asset interiorProjectCoverAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.coverAsset = cloneInteriorProjectCoverAsset(asset)
	reader.coverErr = err
}

// setCoverResult configures one explicit media result. Passing nil restores
// the recording reader's exact-version fixture behavior.
func (reader *recordingInteriorProjectCatalogueReader) setCoverResult(
	asset *interiorProjectCoverAsset,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if asset == nil {
		reader.coverResult = nil

		return
	}
	clone := cloneInteriorProjectCoverAsset(*asset)
	reader.coverResult = &clone
}

// listCallSnapshot returns an isolated record of list calls.
func (reader *recordingInteriorProjectCatalogueReader) listCallSnapshot() []recordingInteriorProjectListCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingInteriorProjectListCall(nil), reader.listCalls...)
}

// findCallSnapshot returns an isolated record of detail calls.
func (reader *recordingInteriorProjectCatalogueReader) findCallSnapshot() []recordingInteriorProjectFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingInteriorProjectFindCall(nil), reader.findCalls...)
}

// coverCallSnapshot returns an isolated record of public media calls.
func (reader *recordingInteriorProjectCatalogueReader) coverCallSnapshot() []recordingInteriorProjectCoverFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingInteriorProjectCoverFindCall(nil), reader.coverCalls...)
}

// cloneCatalogueInteriorProjects copies both the slice and optional metadata so
// tests cannot mutate the reader through a returned pointer.
func cloneCatalogueInteriorProjects(
	projects []catalogueInteriorProject,
) []catalogueInteriorProject {
	clones := make([]catalogueInteriorProject, len(projects))
	for index, project := range projects {
		clones[index] = cloneCatalogueInteriorProject(project)
	}

	return clones
}

// cloneCatalogueInteriorProject isolates one optional cover metadata pointer.
func cloneCatalogueInteriorProject(
	project catalogueInteriorProject,
) catalogueInteriorProject {
	if project.Cover != nil {
		cover := *project.Cover
		project.Cover = &cover
	}

	return project
}
