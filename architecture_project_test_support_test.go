package main

import (
	"context"
	"sync"
	"time"
)

// recordingArchitectureProjectCatalogueReader is the database-free public
// reader shared by application and Architecture HTTP tests. A mutex keeps its
// configurable outcomes and call records safe under concurrent test execution.
type recordingArchitectureProjectCatalogueReader struct {
	mu sync.Mutex

	projects   []catalogueArchitectureProject
	listErr    error
	findErr    error
	findResult *catalogueArchitectureProject
	coverAsset architectureProjectCoverAsset
	// coverMetadata is the independently configurable binary-free first phase.
	coverMetadata reviewedCoverAssetMetadata
	// coverMetadataErr is the configured first-phase failure.
	coverMetadataErr error
	coverErr         error
	coverResult      *architectureProjectCoverAsset

	listCalls []recordingArchitectureProjectListCall
	findCalls []recordingArchitectureProjectFindCall
	// coverMetadataCalls records binary-free exact media lookups.
	coverMetadataCalls []recordingArchitectureProjectCoverFindCall
	coverCalls         []recordingArchitectureProjectCoverFindCall
}

// recordingArchitectureProjectListCall captures the bounded list context.
type recordingArchitectureProjectListCall struct {
	Deadline    time.Time
	HasDeadline bool
}

// recordingArchitectureProjectFindCall captures one canonical detail lookup.
type recordingArchitectureProjectFindCall struct {
	Slug        string
	Deadline    time.Time
	HasDeadline bool
}

// recordingArchitectureProjectCoverFindCall captures one exact media lookup.
type recordingArchitectureProjectCoverFindCall struct {
	Slug        string
	Version     int64
	HasDeadline bool
}

// newRecordingArchitectureProjectCatalogueReader returns fictional fixtures
// for tests only. Production and migrations never publish these records.
func newRecordingArchitectureProjectCatalogueReader() *recordingArchitectureProjectCatalogueReader {
	return &recordingArchitectureProjectCatalogueReader{
		projects: []catalogueArchitectureProject{
			{
				ID:              1,
				PortfolioNumber: 1,
				Slug:            "architecture-study-01",
				Title:           "Architecture Study 01",
				Typology:        "Residential",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              2,
				PortfolioNumber: 2,
				Slug:            "architecture-study-02",
				Title:           "Architecture Study 02",
				Typology:        "Commercial",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              3,
				PortfolioNumber: 3,
				Slug:            "architecture-study-03",
				Title:           "Architecture Study 03",
				Typology:        "Cultural",
				ProjectStatus:   "Portfolio preview",
			},
			{
				ID:              4,
				PortfolioNumber: 4,
				Slug:            "architecture-study-04",
				Title:           "Architecture Study 04",
				Typology:        "Civic",
				ProjectStatus:   "Portfolio preview",
			},
		},
	}
}

// ListPublished records the deadline and returns an isolated configured list.
func (reader *recordingArchitectureProjectCatalogueReader) ListPublished(
	ctx context.Context,
) ([]catalogueArchitectureProject, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.listCalls = append(
		reader.listCalls,
		recordingArchitectureProjectListCall{
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)

	return cloneCatalogueArchitectureProjects(reader.projects), reader.listErr
}

// FindPublishedBySlug records the exact slug and returns an isolated explicit
// result, matching fixture, configured error, or safe not-found category.
func (reader *recordingArchitectureProjectCatalogueReader) FindPublishedBySlug(
	ctx context.Context,
	slug string,
) (catalogueArchitectureProject, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	deadline, hasDeadline := ctx.Deadline()
	reader.findCalls = append(
		reader.findCalls,
		recordingArchitectureProjectFindCall{
			Slug:        slug,
			Deadline:    deadline,
			HasDeadline: hasDeadline,
		},
	)
	if reader.findErr != nil {
		return catalogueArchitectureProject{}, reader.findErr
	}
	if reader.findResult != nil {
		return cloneCatalogueArchitectureProject(*reader.findResult), nil
	}
	for _, project := range reader.projects {
		if project.Slug == slug {
			return cloneCatalogueArchitectureProject(project), nil
		}
	}

	return catalogueArchitectureProject{}, errArchitectureProjectCatalogueNotFound
}

// FindPublishedCoverMetadata records and returns the configured binary-free
// response facts without copying the Architecture image bytes.
func (reader *recordingArchitectureProjectCatalogueReader) FindPublishedCoverMetadata(
	ctx context.Context,
	slug string,
	version int64,
) (reviewedCoverAssetMetadata, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.coverMetadataCalls = append(
		reader.coverMetadataCalls,
		recordingArchitectureProjectCoverFindCall{
			Slug:        slug,
			Version:     version,
			HasDeadline: hasDeadline,
		},
	)
	if reader.coverMetadataErr != nil {
		return reviewedCoverAssetMetadata{}, reader.coverMetadataErr
	}
	if reader.coverResult != nil {
		return reader.coverResult.responseMetadata(), nil
	}
	if reader.coverMetadata.OwnerID <= 0 ||
		reader.coverMetadata.Version != version {
		return reviewedCoverAssetMetadata{}, errArchitectureProjectCoverNotFound
	}

	return reader.coverMetadata, nil
}

// FindPublishedCover records exact coordinates and returns isolated bytes only
// when the configured revision matches the request.
func (reader *recordingArchitectureProjectCatalogueReader) FindPublishedCover(
	ctx context.Context,
	slug string,
	version int64,
) (architectureProjectCoverAsset, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.coverCalls = append(
		reader.coverCalls,
		recordingArchitectureProjectCoverFindCall{
			Slug:        slug,
			Version:     version,
			HasDeadline: hasDeadline,
		},
	)
	if reader.coverErr != nil {
		return architectureProjectCoverAsset{}, reader.coverErr
	}
	if reader.coverResult != nil {
		return cloneArchitectureProjectCoverAsset(*reader.coverResult), nil
	}
	if reader.coverAsset.ArchitectureProjectID <= 0 ||
		reader.coverAsset.Version != version {
		return architectureProjectCoverAsset{}, errArchitectureProjectCoverNotFound
	}

	return cloneArchitectureProjectCoverAsset(reader.coverAsset), nil
}

// setProjects replaces fixtures with a deep-enough isolated copy.
func (reader *recordingArchitectureProjectCatalogueReader) setProjects(
	projects []catalogueArchitectureProject,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.projects = cloneCatalogueArchitectureProjects(projects)
}

// setErrors independently configures list and detail failures.
func (reader *recordingArchitectureProjectCatalogueReader) setErrors(
	listErr error,
	findErr error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.listErr = listErr
	reader.findErr = findErr
}

// setFindResult configures one explicit detail result; nil restores lookup.
func (reader *recordingArchitectureProjectCatalogueReader) setFindResult(
	project *catalogueArchitectureProject,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if project == nil {
		reader.findResult = nil
		return
	}
	clone := cloneCatalogueArchitectureProject(*project)
	reader.findResult = &clone
}

// setCover configures one exact public media outcome.
func (reader *recordingArchitectureProjectCatalogueReader) setCover(
	asset architectureProjectCoverAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.coverAsset = cloneArchitectureProjectCoverAsset(asset)
	reader.coverMetadata = asset.responseMetadata()
	reader.coverMetadataErr = err
	reader.coverErr = err
}

// setCoverContent configures only the second-phase Architecture result while
// retaining the metadata established by setCover.
func (reader *recordingArchitectureProjectCatalogueReader) setCoverContent(
	asset architectureProjectCoverAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.coverAsset = cloneArchitectureProjectCoverAsset(asset)
	reader.coverResult = nil
	reader.coverErr = err
}

// setCoverResult configures an explicit media record; nil restores revision matching.
func (reader *recordingArchitectureProjectCatalogueReader) setCoverResult(
	asset *architectureProjectCoverAsset,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if asset == nil {
		reader.coverResult = nil
		return
	}
	clone := cloneArchitectureProjectCoverAsset(*asset)
	reader.coverResult = &clone
}

// Snapshot helpers return isolated call histories for HTTP assertions.
func (reader *recordingArchitectureProjectCatalogueReader) listCallSnapshot() []recordingArchitectureProjectListCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return append([]recordingArchitectureProjectListCall(nil), reader.listCalls...)
}

// findCallSnapshot returns an isolated history of published-detail lookups.
func (reader *recordingArchitectureProjectCatalogueReader) findCallSnapshot() []recordingArchitectureProjectFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return append([]recordingArchitectureProjectFindCall(nil), reader.findCalls...)
}

// coverCallSnapshot returns an isolated history of exact public-cover lookups.
func (reader *recordingArchitectureProjectCatalogueReader) coverCallSnapshot() []recordingArchitectureProjectCoverFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return append([]recordingArchitectureProjectCoverFindCall(nil), reader.coverCalls...)
}

// coverMetadataCallSnapshot returns isolated first-phase media call records.
func (reader *recordingArchitectureProjectCatalogueReader) coverMetadataCallSnapshot() []recordingArchitectureProjectCoverFindCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return append(
		[]recordingArchitectureProjectCoverFindCall(nil),
		reader.coverMetadataCalls...,
	)
}

// cloneCatalogueArchitectureProjects isolates both the slice and cover pointers.
func cloneCatalogueArchitectureProjects(
	projects []catalogueArchitectureProject,
) []catalogueArchitectureProject {
	clones := make([]catalogueArchitectureProject, len(projects))
	for index, project := range projects {
		clones[index] = cloneCatalogueArchitectureProject(project)
	}
	return clones
}

// cloneCatalogueArchitectureProject isolates optional cover metadata.
func cloneCatalogueArchitectureProject(
	project catalogueArchitectureProject,
) catalogueArchitectureProject {
	if project.Cover != nil {
		cover := *project.Cover
		project.Cover = &cover
	}
	return project
}
