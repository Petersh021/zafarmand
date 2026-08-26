package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"testing"
	"time"
)

// interiorProjectCatalogueContextKey is a private marker used to prove that
// repository methods forward their caller's exact context.
type interiorProjectCatalogueContextKey struct{}

// interiorProjectCatalogueQueryStub records one list invocation and supplies
// a controlled iterator or database-like failure.
type interiorProjectCatalogueQueryStub struct {
	// rows is returned even on error so cleanup behavior can be exercised.
	rows interiorProjectCatalogueRows
	// queryError simulates QueryContext failure.
	queryError error
	// calls is the number of attempted statements.
	calls int
	// context is the exact received context.
	context context.Context
	// query is the complete fixed SQL statement.
	query string
	// arguments are isolated copies of bound values.
	arguments []any
}

// Query records and returns the configured list result.
func (stub *interiorProjectCatalogueQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (interiorProjectCatalogueRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.rows, stub.queryError
}

// interiorProjectCatalogueRowsStub exposes projects through the exact iterator
// used by ListPublished and controls each lifecycle failure independently.
type interiorProjectCatalogueRowsStub struct {
	// projects are returned in supplied order.
	projects []catalogueInteriorProject
	// scanErrorAt is the zero-based failing row; -1 disables it.
	scanErrorAt int
	// scanError is returned at scanErrorAt.
	scanError error
	// iterationError is returned by Err.
	iterationError error
	// closeError is returned by Close.
	closeError error
	// nextIndex identifies the next configured row.
	nextIndex int
	// currentIndex identifies the row available to Scan.
	currentIndex int
	// scanCalls counts successful or attempted scans.
	scanCalls int
	// closeCalls proves cleanup occurs exactly once.
	closeCalls int
}

// newInteriorProjectCatalogueRowsStub initializes the disabled scan sentinel.
func newInteriorProjectCatalogueRowsStub(
	projects []catalogueInteriorProject,
) *interiorProjectCatalogueRowsStub {
	return &interiorProjectCatalogueRowsStub{
		projects:     projects,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances through the finite configured result.
func (stub *interiorProjectCatalogueRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.projects) {
		return false
	}

	stub.currentIndex = stub.nextIndex
	stub.nextIndex++

	return true
}

// Scan copies one fourteen-column project projection.
func (stub *interiorProjectCatalogueRowsStub) Scan(destinations ...any) error {
	stub.scanCalls++
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.projects) {
		return errors.New("interior list scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}

	return setInteriorProjectCatalogueDestinations(
		stub.projects[stub.currentIndex],
		destinations,
	)
}

// Err returns the controlled post-iteration failure.
func (stub *interiorProjectCatalogueRowsStub) Err() error {
	return stub.iterationError
}

// Close records cleanup and returns the controlled close result.
func (stub *interiorProjectCatalogueRowsStub) Close() error {
	stub.closeCalls++

	return stub.closeError
}

// interiorProjectCatalogueQueryRowStub records detail or media query calls.
type interiorProjectCatalogueQueryRowStub struct {
	// row is returned to the repository.
	row interiorProjectCatalogueRowScanner
	// calls records query attempts.
	calls int
	// context is the exact received context.
	context context.Context
	// query is the complete fixed SQL statement.
	query string
	// arguments are isolated bound values.
	arguments []any
}

// QueryRow captures one single-row operation.
func (stub *interiorProjectCatalogueQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) interiorProjectCatalogueRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// interiorProjectCatalogueRowStub supplies one controlled detail result.
type interiorProjectCatalogueRowStub struct {
	// project contains every successful destination value.
	project catalogueInteriorProject
	// scanError simulates no rows or driver failure.
	scanError error
	// calls counts scan attempts.
	calls int
}

// Scan implements the single-row public projection.
func (stub *interiorProjectCatalogueRowStub) Scan(
	destinations ...any,
) error {
	stub.calls++
	if stub.scanError != nil {
		return stub.scanError
	}

	return setInteriorProjectCatalogueDestinations(
		stub.project,
		destinations,
	)
}

// interiorProjectCatalogueScannerFunc adapts one malformed-projection closure
// to the repository scanner interface.
type interiorProjectCatalogueScannerFunc func(...any) error

// Scan delegates to the configured focused behavior.
func (scanner interiorProjectCatalogueScannerFunc) Scan(
	destinations ...any,
) error {
	return scanner(destinations...)
}

// setInteriorProjectCatalogueDestinations copies one complete project into the
// fourteen destinations shared by list and detail SQL.
func setInteriorProjectCatalogueDestinations(
	project catalogueInteriorProject,
	destinations []any,
) error {
	if len(destinations) != 14 {
		return errors.New("interior project scan expected fourteen destinations")
	}

	id, idOK := destinations[0].(*int64)
	number, numberOK := destinations[1].(*int64)
	slug, slugOK := destinations[2].(*string)
	title, titleOK := destinations[3].(*string)
	typology, typologyOK := destinations[4].(*string)
	location, locationOK := destinations[5].(*string)
	year, yearOK := destinations[6].(*sql.NullInt64)
	projectStatus, statusOK := destinations[7].(*string)
	description, descriptionOK := destinations[8].(*string)
	if !idOK || !numberOK || !slugOK || !titleOK || !typologyOK ||
		!locationOK || !yearOK || !statusOK || !descriptionOK {
		return errors.New("interior project scan received unexpected destinations")
	}

	*id = project.ID
	*number = project.PortfolioNumber
	*slug = project.Slug
	*title = project.Title
	*typology = project.Typology
	*location = project.Location
	if project.ProjectYear == 0 {
		*year = sql.NullInt64{}
	} else {
		*year = sql.NullInt64{Int64: int64(project.ProjectYear), Valid: true}
	}
	*projectStatus = project.ProjectStatus
	*description = project.Description

	return setInteriorProjectCoverMetadataDestinations(
		project.Cover,
		destinations[9:],
	)
}

// setInteriorProjectCoverMetadataDestinations writes the five nullable LEFT
// JOIN values for either a complete cover or an entirely absent cover.
func setInteriorProjectCoverMetadataDestinations(
	cover *interiorProjectCoverMetadata,
	destinations []any,
) error {
	if len(destinations) != 5 {
		return errors.New("interior cover metadata expected five destinations")
	}

	version, versionOK := destinations[0].(*sql.NullInt64)
	width, widthOK := destinations[1].(*sql.NullInt64)
	height, heightOK := destinations[2].(*sql.NullInt64)
	altText, altTextOK := destinations[3].(*sql.NullString)
	caption, captionOK := destinations[4].(*sql.NullString)
	if !versionOK || !widthOK || !heightOK || !altTextOK || !captionOK {
		return errors.New("interior cover metadata received unexpected destinations")
	}
	if cover == nil {
		*version = sql.NullInt64{}
		*width = sql.NullInt64{}
		*height = sql.NullInt64{}
		*altText = sql.NullString{}
		*caption = sql.NullString{}

		return nil
	}

	*version = sql.NullInt64{Int64: cover.Version, Valid: true}
	*width = sql.NullInt64{Int64: int64(cover.Width), Valid: true}
	*height = sql.NullInt64{Int64: int64(cover.Height), Valid: true}
	*altText = sql.NullString{String: cover.AltText, Valid: true}
	*caption = sql.NullString{String: cover.Caption, Valid: true}

	return nil
}

// interiorProjectCoverRowStub supplies one fixed twelve-column binary record.
type interiorProjectCoverRowStub struct {
	// asset is copied into destinations on success.
	asset interiorProjectCoverAsset
	// scanError simulates no rows or dependency failure.
	scanError error
}

// Scan implements the binary and binary-free public cover scanner seams.
func (stub *interiorProjectCoverRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) == 11 {
		metadata := stub.asset.responseMetadata()
		*destinations[0].(*int64) = metadata.OwnerID
		*destinations[1].(*int64) = metadata.Version
		*destinations[2].(*string) = metadata.ContentType
		*destinations[3].(*int) = metadata.ByteSize
		*destinations[4].(*int) = metadata.Width
		*destinations[5].(*int) = metadata.Height
		*destinations[6].(*[]byte) = append([]byte(nil), metadata.SHA256[:]...)
		*destinations[7].(*string) = metadata.AltText
		*destinations[8].(*string) = metadata.Caption
		*destinations[9].(*time.Time) = metadata.CreatedAt
		*destinations[10].(*time.Time) = metadata.UpdatedAt

		return nil
	}
	if len(destinations) != 12 {
		return errors.New("interior cover scan expected eleven or twelve destinations")
	}

	ownerID, ownerOK := destinations[0].(*int64)
	version, versionOK := destinations[1].(*int64)
	contentType, contentTypeOK := destinations[2].(*string)
	content, contentOK := destinations[3].(*[]byte)
	byteSize, byteSizeOK := destinations[4].(*int)
	width, widthOK := destinations[5].(*int)
	height, heightOK := destinations[6].(*int)
	digest, digestOK := destinations[7].(*[]byte)
	altText, altTextOK := destinations[8].(*string)
	caption, captionOK := destinations[9].(*string)
	createdAt, createdAtOK := destinations[10].(*time.Time)
	updatedAt, updatedAtOK := destinations[11].(*time.Time)
	if !ownerOK || !versionOK || !contentTypeOK || !contentOK ||
		!byteSizeOK || !widthOK || !heightOK || !digestOK || !altTextOK ||
		!captionOK || !createdAtOK || !updatedAtOK {
		return errors.New("interior cover scan received unexpected destinations")
	}

	*ownerID = stub.asset.InteriorProjectID
	*version = stub.asset.Version
	*contentType = stub.asset.ContentType
	*content = append([]byte(nil), stub.asset.Content...)
	*byteSize = stub.asset.ByteSize
	*width = stub.asset.Width
	*height = stub.asset.Height
	*digest = append([]byte(nil), stub.asset.SHA256[:]...)
	*altText = stub.asset.AltText
	*caption = stub.asset.Caption
	*createdAt = stub.asset.CreatedAt
	*updatedAt = stub.asset.UpdatedAt

	return nil
}

// validCatalogueInteriorProject returns one deterministic valid public record.
func validCatalogueInteriorProject(
	id int64,
	number int64,
	slug string,
) catalogueInteriorProject {
	return catalogueInteriorProject{
		ID:              id,
		PortfolioNumber: number,
		Slug:            slug,
		Title:           "Stage Twenty-Two Interior",
		Typology:        "Residential",
		Location:        "Tehran",
		ProjectYear:     2034,
		ProjectStatus:   "Completed",
		Description:     "A fictional reviewed Interior project used only by tests.",
	}
}

// validTestInteriorProjectCoverAsset returns an internally consistent image
// record using the shared deterministic PNG fixture.
func validTestInteriorProjectCoverAsset(
	t *testing.T,
	projectID int64,
	version int64,
) interiorProjectCoverAsset {
	t.Helper()

	content := testInteriorProjectCoverPNG(t)
	inspection, err := inspectReviewedCover(content, true)
	if err != nil {
		t.Fatalf("inspect deterministic Interior cover: %v", err)
	}
	createdAt := time.Date(2035, time.April, 5, 6, 7, 8, 0, time.UTC)

	return interiorProjectCoverAsset{
		InteriorProjectID: projectID,
		Version:           version,
		ContentType:       inspection.ContentType,
		Content:           append([]byte(nil), content...),
		ByteSize:          len(content),
		Width:             inspection.Width,
		Height:            inspection.Height,
		SHA256:            inspection.SHA256,
		AltText:           "A fictional Interior room with geometric openings",
		Caption:           "Synthetic Stage 22 cover fixture.",
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt.Add(time.Minute),
	}
}

// testInteriorProjectCoverPNG returns a small deterministic image whose real
// decoder facts exercise the shared reviewed-cover validation boundary.
func testInteriorProjectCoverPNG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetRGBA(
				x,
				y,
				color.RGBA{
					R: uint8(25 + x*20),
					G: uint8(45 + y*25),
					B: 95,
					A: 255,
				},
			)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode deterministic Interior cover PNG: %v", err)
	}

	return encoded.Bytes()
}

// TestNewPostgresInteriorProjectCatalogueReader verifies construction rejects
// a missing pool and otherwise installs both adapters without connecting.
func TestNewPostgresInteriorProjectCatalogueReader(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		reader, err := newPostgresInteriorProjectCatalogueReader(nil)
		if !errors.Is(err, errInteriorProjectCatalogueReaderDatabaseRequired) {
			t.Fatalf("error: got %v, want database-required sentinel", err)
		}
		if reader != nil {
			t.Errorf("reader: got %#v, want nil", reader)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		reader, err := newPostgresInteriorProjectCatalogueReader(new(sql.DB))
		if err != nil {
			t.Fatalf("create reader: %v", err)
		}
		if reader == nil || reader.query == nil || reader.queryRow == nil {
			t.Fatalf("reader adapters are incomplete: %#v", reader)
		}
	})
}

// TestPostgresInteriorProjectCatalogueReaderListsPublished verifies fixed SQL,
// publication binding, context forwarding, full mapping, and row cleanup.
func TestPostgresInteriorProjectCatalogueReaderListsPublished(t *testing.T) {
	expected := []catalogueInteriorProject{
		validCatalogueInteriorProject(8, 1, "first-interior"),
		validCatalogueInteriorProject(3, 2, "second-interior"),
	}
	expected[1].Title = "Second Interior"
	expected[1].ProjectYear = 0
	expected[1].Cover = &interiorProjectCoverMetadata{
		Version: 2,
		Width:   4,
		Height:  3,
		AltText: "A fictional second Interior cover",
		Caption: "Reviewed fixture caption.",
	}
	rows := newInteriorProjectCatalogueRowsStub(expected)
	query := &interiorProjectCatalogueQueryStub{rows: rows}
	reader := &postgresInteriorProjectCatalogueReader{query: query.Query}
	ctx := context.WithValue(
		context.Background(),
		interiorProjectCatalogueContextKey{},
		"list-context",
	)

	actual, err := reader.ListPublished(ctx)
	if err != nil {
		t.Fatalf("list published Interior projects: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("projects: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != listPublishedInteriorProjectsSQL ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{publishedInteriorProjectStatus},
		) {
		t.Errorf(
			"query: calls=%d text=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
	if rows.scanCalls != len(expected) || rows.closeCalls != 1 {
		t.Errorf(
			"row lifecycle: scans=%d closes=%d, want %d/1",
			rows.scanCalls,
			rows.closeCalls,
			len(expected),
		)
	}

	// The returned slice is capacity-clipped so a caller's append cannot reuse
	// repository-owned spare capacity.
	if cap(actual) != len(actual) {
		t.Errorf("result capacity: got %d, want %d", cap(actual), len(actual))
	}
}

// TestPostgresInteriorProjectCatalogueReaderListsEmpty treats a fresh unseeded
// catalogue as a successful allocated empty result.
func TestPostgresInteriorProjectCatalogueReaderListsEmpty(t *testing.T) {
	rows := newInteriorProjectCatalogueRowsStub(nil)
	reader := &postgresInteriorProjectCatalogueReader{
		query: (&interiorProjectCatalogueQueryStub{rows: rows}).Query,
	}

	projects, err := reader.ListPublished(context.Background())
	if err != nil {
		t.Fatalf("list empty Interior catalogue: %v", err)
	}
	if projects == nil || len(projects) != 0 {
		t.Errorf("empty projects: got %#v, want allocated empty slice", projects)
	}
	if rows.closeCalls != 1 {
		t.Errorf("close calls: got %d, want 1", rows.closeCalls)
	}
}

// TestPostgresInteriorProjectCatalogueReaderRejectsInvalidListResults proves
// malformed, duplicate, and out-of-order records never cross the repository.
func TestPostgresInteriorProjectCatalogueReaderRejectsInvalidListResults(
	t *testing.T,
) {
	invalid := []struct {
		// name identifies one violated invariant.
		name string
		// projects is the controlled complete result.
		projects []catalogueInteriorProject
	}{
		{name: "non-positive id", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(0, 1, "first-interior"),
		}},
		{name: "number starts after one", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(1, 2, "first-interior"),
		}},
		{name: "number gap", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(1, 1, "first-interior"),
			validCatalogueInteriorProject(2, 3, "second-interior"),
		}},
		{name: "duplicate id", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(1, 1, "first-interior"),
			validCatalogueInteriorProject(1, 2, "second-interior"),
		}},
		{name: "duplicate slug", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(1, 1, "first-interior"),
			validCatalogueInteriorProject(2, 2, "first-interior"),
		}},
		{name: "invalid slug", projects: []catalogueInteriorProject{
			validCatalogueInteriorProject(1, 1, "First-Interior"),
		}},
		{name: "empty title", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.Title = ""
				return project
			}(),
		}},
		{name: "untrimmed typology", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.Typology = " Residential"
				return project
			}(),
		}},
		{name: "invalid year", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.ProjectYear = 999
				return project
			}(),
		}},
		{name: "empty project status", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.ProjectStatus = ""
				return project
			}(),
		}},
		{name: "untrimmed optional description", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.Description = " untrimmed"
				return project
			}(),
		}},
		{name: "invalid cover metadata", projects: []catalogueInteriorProject{
			func() catalogueInteriorProject {
				project := validCatalogueInteriorProject(1, 1, "first-interior")
				project.Cover = &interiorProjectCoverMetadata{
					Version: 1,
					Width:   reviewedCoverMaximumDimension + 1,
					Height:  1,
					AltText: "Synthetic cover",
				}
				return project
			}(),
		}},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			rows := newInteriorProjectCatalogueRowsStub(test.projects)
			reader := &postgresInteriorProjectCatalogueReader{
				query: (&interiorProjectCatalogueQueryStub{rows: rows}).Query,
			}

			projects, err := reader.ListPublished(context.Background())
			if !errors.Is(err, errInteriorProjectCatalogueReadFailed) {
				t.Fatalf("error: got %v, want read-failure sentinel", err)
			}
			if projects != nil || rows.closeCalls != 1 {
				t.Errorf(
					"invalid result: projects=%#v closes=%d, want nil/1",
					projects,
					rows.closeCalls,
				)
			}
		})
	}
}

// TestPostgresInteriorProjectCatalogueReaderListFailures verifies local input,
// unavailable adapter, query, scanning, iteration, and cleanup failures.
func TestPostgresInteriorProjectCatalogueReaderListFailures(t *testing.T) {
	query := &interiorProjectCatalogueQueryStub{}
	reader := &postgresInteriorProjectCatalogueReader{query: query.Query}
	if projects, err := reader.ListPublished(nil); !errors.Is(
		err,
		errInteriorProjectCatalogueInvalidQuery,
	) || projects != nil || query.calls != 0 {
		t.Fatalf("nil context: projects=%#v calls=%d err=%v", projects, query.calls, err)
	}

	var nilReader *postgresInteriorProjectCatalogueReader
	if projects, err := nilReader.ListPublished(context.Background()); !errors.Is(err, errInteriorProjectCatalogueReadFailed) || projects != nil {
		t.Fatalf("nil reader: projects=%#v err=%v", projects, err)
	}

	unsafeError := errors.New("postgres://private-interior-detail")
	tests := []struct {
		// name identifies the failing lifecycle point.
		name string
		// configure mutates controlled query and rows.
		configure func(
			*interiorProjectCatalogueQueryStub,
			*interiorProjectCatalogueRowsStub,
		)
	}{
		{name: "query error with rows", configure: func(
			query *interiorProjectCatalogueQueryStub,
			_ *interiorProjectCatalogueRowsStub,
		) {
			query.queryError = unsafeError
		}},
		{name: "scan error", configure: func(
			_ *interiorProjectCatalogueQueryStub,
			rows *interiorProjectCatalogueRowsStub,
		) {
			rows.scanErrorAt, rows.scanError = 0, unsafeError
		}},
		{name: "iteration error", configure: func(
			_ *interiorProjectCatalogueQueryStub,
			rows *interiorProjectCatalogueRowsStub,
		) {
			rows.iterationError = unsafeError
		}},
		{name: "close error", configure: func(
			_ *interiorProjectCatalogueQueryStub,
			rows *interiorProjectCatalogueRowsStub,
		) {
			rows.closeError = unsafeError
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := newInteriorProjectCatalogueRowsStub([]catalogueInteriorProject{
				validCatalogueInteriorProject(1, 1, "first-interior"),
			})
			query := &interiorProjectCatalogueQueryStub{rows: rows}
			test.configure(query, rows)
			reader := &postgresInteriorProjectCatalogueReader{query: query.Query}

			projects, err := reader.ListPublished(context.Background())
			if err != errInteriorProjectCatalogueReadFailed || projects != nil ||
				strings.Contains(err.Error(), "private-interior") {
				t.Fatalf("projects=%#v err=%v", projects, err)
			}
			if rows.closeCalls != 1 {
				t.Errorf("close calls: got %d, want 1", rows.closeCalls)
			}
		})
	}

	t.Run("nil rows", func(t *testing.T) {
		reader := &postgresInteriorProjectCatalogueReader{
			query: (&interiorProjectCatalogueQueryStub{}).Query,
		}
		projects, err := reader.ListPublished(context.Background())
		if err != errInteriorProjectCatalogueReadFailed || projects != nil {
			t.Fatalf("projects=%#v err=%v", projects, err)
		}
	})
}

// TestPostgresInteriorProjectCatalogueReaderFindsPublishedBySlug verifies the
// fixed detail statement, parameter order, context forwarding, and full map.
func TestPostgresInteriorProjectCatalogueReaderFindsPublishedBySlug(
	t *testing.T,
) {
	expected := validCatalogueInteriorProject(23, 4, "stone-residence")
	expected.Cover = &interiorProjectCoverMetadata{
		Version: 3,
		Width:   4,
		Height:  3,
		AltText: "A fictional stone residence Interior",
	}
	row := &interiorProjectCatalogueRowStub{project: expected}
	query := &interiorProjectCatalogueQueryRowStub{row: row}
	reader := &postgresInteriorProjectCatalogueReader{queryRow: query.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		interiorProjectCatalogueContextKey{},
		"detail-context",
	)

	actual, err := reader.FindPublishedBySlug(ctx, expected.Slug)
	if err != nil {
		t.Fatalf("find published Interior project: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("project: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx || row.calls != 1 ||
		query.query != findPublishedInteriorProjectBySlugSQL ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{publishedInteriorProjectStatus, expected.Slug},
		) {
		t.Errorf(
			"query: calls=%d scans=%d text=%q args=%#v",
			query.calls,
			row.calls,
			query.query,
			query.arguments,
		)
	}
}

// TestPostgresInteriorProjectCatalogueReaderRejectsInvalidDetailQueries proves
// malformed visitor path values and nil context never reach SQL.
func TestPostgresInteriorProjectCatalogueReaderRejectsInvalidDetailQueries(
	t *testing.T,
) {
	tests := []struct {
		// name describes the rejected boundary.
		name string
		// context is nil only for that dedicated case.
		context context.Context
		// slug is the untrusted path value.
		slug string
	}{
		{name: "nil context", slug: "stone-residence"},
		{name: "empty", context: context.Background()},
		{name: "uppercase", context: context.Background(), slug: "Stone-Residence"},
		{name: "double hyphen", context: context.Background(), slug: "stone--residence"},
		{name: "slash", context: context.Background(), slug: "stone/residence"},
		{name: "unicode", context: context.Background(), slug: "st\u00f6ne"},
		{name: "too long", context: context.Background(), slug: strings.Repeat(
			"a",
			interiorProjectSlugMaximumLength+1,
		)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &interiorProjectCatalogueQueryRowStub{}
			reader := &postgresInteriorProjectCatalogueReader{queryRow: query.QueryRow}
			project, err := reader.FindPublishedBySlug(test.context, test.slug)
			if err != errInteriorProjectCatalogueInvalidQuery ||
				project != (catalogueInteriorProject{}) || query.calls != 0 {
				t.Fatalf("project=%#v calls=%d err=%v", project, query.calls, err)
			}
		})
	}
}

// TestPostgresInteriorProjectCatalogueReaderDetailFailures verifies not-found,
// adapter, dependency-redaction, mismatched-row, and validation behavior.
func TestPostgresInteriorProjectCatalogueReaderDetailFailures(t *testing.T) {
	const slug = "stone-residence"
	unsafeDetail := "postgres://private-interior-detail"
	invalid := validCatalogueInteriorProject(1, 1, slug)
	invalid.ProjectStatus = ""
	mismatch := validCatalogueInteriorProject(1, 1, "other-residence")
	tests := []struct {
		// name identifies one outcome.
		name string
		// reader supplies the controlled adapter.
		reader *postgresInteriorProjectCatalogueReader
		// want is the stable safe category.
		want error
	}{
		{name: "nil receiver", want: errInteriorProjectCatalogueReadFailed},
		{name: "missing query", reader: &postgresInteriorProjectCatalogueReader{}, want: errInteriorProjectCatalogueReadFailed},
		{name: "nil row", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner { return nil }}, want: errInteriorProjectCatalogueReadFailed},
		{name: "not found", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCatalogueRowStub{scanError: sql.ErrNoRows}
		}}, want: errInteriorProjectCatalogueNotFound},
		{name: "driver error", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCatalogueRowStub{scanError: errors.New(unsafeDetail)}
		}}, want: errInteriorProjectCatalogueReadFailed},
		{name: "mismatched slug", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCatalogueRowStub{project: mismatch}
		}}, want: errInteriorProjectCatalogueReadFailed},
		{name: "invalid stored record", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCatalogueRowStub{project: invalid}
		}}, want: errInteriorProjectCatalogueReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project, err := test.reader.FindPublishedBySlug(
				context.Background(),
				slug,
			)
			if err != test.want || project != (catalogueInteriorProject{}) ||
				strings.Contains(err.Error(), unsafeDetail) {
				t.Fatalf("project=%#v err=%v, want %v", project, err, test.want)
			}
		})
	}
}

// TestScanCatalogueInteriorProjectRejectsPartialCoverProjection proves one
// present nullable cover column cannot become an absent or valid cover.
func TestScanCatalogueInteriorProjectRejectsPartialCoverProjection(t *testing.T) {
	base := &interiorProjectCatalogueRowStub{
		project: validCatalogueInteriorProject(1, 1, "stone-residence"),
	}
	scanner := interiorProjectCatalogueScannerFunc(func(destinations ...any) error {
		if err := base.Scan(destinations...); err != nil {
			return err
		}
		*destinations[9].(*sql.NullInt64) = sql.NullInt64{Int64: 1, Valid: true}

		return nil
	})

	project, err := scanCatalogueInteriorProject(scanner)
	if err != errInteriorProjectCatalogueReadFailed ||
		project != (catalogueInteriorProject{}) {
		t.Fatalf("project=%#v err=%v", project, err)
	}
}

// TestPostgresInteriorProjectCatalogueReaderFindsPublishedCover verifies exact
// SQL coordinates, full mapping, and mutable-byte isolation.
func TestPostgresInteriorProjectCatalogueReaderFindsPublishedCover(t *testing.T) {
	expected := validTestInteriorProjectCoverAsset(t, 9, 4)
	query := &interiorProjectCatalogueQueryRowStub{
		row: &interiorProjectCoverRowStub{asset: expected},
	}
	reader := &postgresInteriorProjectCatalogueReader{queryRow: query.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		interiorProjectCatalogueContextKey{},
		"cover-context",
	)

	actual, err := reader.FindPublishedCover(ctx, "stone-residence", 4)
	if err != nil {
		t.Fatalf("find published Interior cover: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("asset: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findPublishedInteriorProjectCoverSQL ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{"stone-residence", publishedInteriorProjectStatus, int64(4)},
		) {
		t.Errorf("query=%q args=%#v", query.query, query.arguments)
	}
	actual.Content[0] ^= 0xff
	if reflect.DeepEqual(actual.Content, expected.Content) {
		t.Error("cover result shares mutable scanner bytes")
	}
}

// TestPostgresInteriorProjectCatalogueReaderFindsPublishedCoverMetadata proves
// the first public media read uses exact predicates without selecting bytea.
func TestPostgresInteriorProjectCatalogueReaderFindsPublishedCoverMetadata(t *testing.T) {
	asset := validTestInteriorProjectCoverAsset(t, 9, 4)
	query := &interiorProjectCatalogueQueryRowStub{
		row: &interiorProjectCoverRowStub{asset: asset},
	}
	reader := &postgresInteriorProjectCatalogueReader{queryRow: query.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		interiorProjectCatalogueContextKey{},
		"cover-metadata-context",
	)

	metadata, err := reader.FindPublishedCoverMetadata(
		ctx,
		"stone-residence",
		asset.Version,
	)
	if err != nil {
		t.Fatalf("find published Interior cover metadata: %v", err)
	}
	if metadata != asset.responseMetadata() {
		t.Errorf("metadata: got %#v, want %#v", metadata, asset.responseMetadata())
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findPublishedInteriorProjectCoverMetadataSQL ||
		strings.Contains(query.query, "cover.content,") ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{"stone-residence", publishedInteriorProjectStatus, asset.Version},
		) {
		t.Errorf("metadata query=%q args=%#v", query.query, query.arguments)
	}
}

// TestPostgresInteriorProjectCatalogueReaderCoverFailures keeps hidden media a
// not-found category and redacts every unsafe stored or dependency detail.
func TestPostgresInteriorProjectCatalogueReaderCoverFailures(t *testing.T) {
	unsafeDetail := "postgres://private-interior-cover"
	valid := validTestInteriorProjectCoverAsset(t, 1, 1)
	wrongVersion := cloneInteriorProjectCoverAsset(valid)
	wrongVersion.Version = 2
	invalid := cloneInteriorProjectCoverAsset(valid)
	invalid.AltText = ""
	tests := []struct {
		// name identifies one outcome.
		name string
		// reader supplies the configured adapter.
		reader *postgresInteriorProjectCatalogueReader
		// want is the stable expected category.
		want error
	}{
		{name: "nil receiver", want: errInteriorProjectCoverReadFailed},
		{name: "missing query", reader: &postgresInteriorProjectCatalogueReader{}, want: errInteriorProjectCoverReadFailed},
		{name: "nil row", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner { return nil }}, want: errInteriorProjectCoverReadFailed},
		{name: "not found", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCoverRowStub{scanError: sql.ErrNoRows}
		}}, want: errInteriorProjectCoverNotFound},
		{name: "driver error", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCoverRowStub{scanError: errors.New(unsafeDetail)}
		}}, want: errInteriorProjectCoverReadFailed},
		{name: "wrong version", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCoverRowStub{asset: wrongVersion}
		}}, want: errInteriorProjectCoverReadFailed},
		{name: "invalid asset", reader: &postgresInteriorProjectCatalogueReader{queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			return &interiorProjectCoverRowStub{asset: invalid}
		}}, want: errInteriorProjectCoverReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := test.reader.FindPublishedCover(
				context.Background(),
				"stone-residence",
				1,
			)
			if err != test.want || !reflect.DeepEqual(
				asset,
				interiorProjectCoverAsset{},
			) || strings.Contains(err.Error(), unsafeDetail) {
				t.Fatalf("asset=%#v err=%v, want %v", asset, err, test.want)
			}
		})
	}

	reader := &postgresInteriorProjectCatalogueReader{
		queryRow: func(context.Context, string, ...any) interiorProjectCatalogueRowScanner {
			t.Fatal("invalid cover input reached SQL")
			return nil
		},
	}
	for _, test := range []struct {
		// name describes the invalid coordinate.
		name string
		// context is nil only in the dedicated case.
		context context.Context
		// slug and version are the public media coordinates.
		slug    string
		version int64
	}{
		{name: "nil context", slug: "stone-residence", version: 1},
		{name: "invalid slug", context: context.Background(), slug: "Bad Slug", version: 1},
		{name: "zero version", context: context.Background(), slug: "stone-residence"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := reader.FindPublishedCover(test.context, test.slug, test.version)
			if err != errInteriorProjectCatalogueInvalidQuery {
				t.Errorf("error: got %v, want invalid query", err)
			}
		})
	}
}

// TestInteriorProjectCatalogueValidationBoundaries locks migration-aligned
// Unicode, year, optional-copy, and cover metadata limits.
func TestInteriorProjectCatalogueValidationBoundaries(t *testing.T) {
	if !isCanonicalInteriorProjectSlug(
		strings.Repeat("a", interiorProjectSlugMaximumLength),
	) || isCanonicalInteriorProjectSlug(
		strings.Repeat("a", interiorProjectSlugMaximumLength+1),
	) {
		t.Error("canonical slug length boundary drifted")
	}
	if !isValidInteriorProjectCatalogueText(
		strings.Repeat("\u00e9", interiorProjectTitleMaximumLength),
		interiorProjectTitleMaximumLength,
	) || isValidInteriorProjectCatalogueText(
		strings.Repeat("\u00e9", interiorProjectTitleMaximumLength+1),
		interiorProjectTitleMaximumLength,
	) {
		t.Error("multibyte title rune boundary drifted")
	}
	for _, validYear := range []int{0, 1000, 2035, 9999} {
		if !isValidInteriorProjectYear(validYear) {
			t.Errorf("valid year %d was rejected", validYear)
		}
	}
	for _, invalidYear := range []int{-1, 1, 999, 10000} {
		if isValidInteriorProjectYear(invalidYear) {
			t.Errorf("invalid year %d was accepted", invalidYear)
		}
	}
	if !isValidOptionalEditorialText("", interiorProjectLocationMaximumLength) {
		t.Error("absent optional location was rejected")
	}
	if isValidOptionalEditorialText(
		"description\x00",
		interiorProjectDescriptionMaximumLength,
	) {
		t.Error("PostgreSQL-incompatible NUL was accepted")
	}
}
