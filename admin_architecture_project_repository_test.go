package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// adminArchitectureProjectQueryStub records a list invocation and returns a
// controlled iterator or dependency failure.
type adminArchitectureProjectQueryStub struct {
	// rows and queryError control the returned dependency outcome.
	rows       adminArchitectureProjectRows
	queryError error
	// calls, context, query, and arguments record the complete invocation.
	calls     int
	context   context.Context
	query     string
	arguments []any
}

// Query implements the reader's narrow ordered-list seam.
func (stub *adminArchitectureProjectQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (adminArchitectureProjectRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)
	return stub.rows, stub.queryError
}

// adminArchitectureProjectRowsStub exposes configured records through the
// production iterator contract.
type adminArchitectureProjectRowsStub struct {
	// projects are exposed in configured order.
	projects []adminArchitectureProjectRecord
	// scanErrorAt and scanError configure one row-level failure.
	scanErrorAt int
	scanError   error
	// iterationError and closeError configure finalization failures.
	iterationError error
	closeError     error
	// indices and closeCalls record iterator state.
	nextIndex    int
	currentIndex int
	closeCalls   int
}

// newAdminArchitectureProjectRowsStub initializes the disabled scan index and
// pre-iteration marker.
func newAdminArchitectureProjectRowsStub(
	projects []adminArchitectureProjectRecord,
) *adminArchitectureProjectRowsStub {
	return &adminArchitectureProjectRowsStub{
		projects:     projects,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances through the finite configured slice.
func (stub *adminArchitectureProjectRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.projects) {
		return false
	}
	stub.currentIndex = stub.nextIndex
	stub.nextIndex++
	return true
}

// Scan copies the current record into the fixed protected projection.
func (stub *adminArchitectureProjectRowsStub) Scan(destinations ...any) error {
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.projects) {
		return errors.New("admin architecture scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}
	return setAdminArchitectureProjectScanDestinations(
		stub.projects[stub.currentIndex],
		destinations,
	)
}

// Err returns the configured post-iteration failure.
func (stub *adminArchitectureProjectRowsStub) Err() error {
	return stub.iterationError
}

// Close records finalization and returns the configured result.
func (stub *adminArchitectureProjectRowsStub) Close() error {
	stub.closeCalls++
	return stub.closeError
}

// adminArchitectureProjectQueryRowStub records one protected detail or cover
// query and returns its controlled row.
type adminArchitectureProjectQueryRowStub struct {
	// row is returned to the repository.
	row adminArchitectureProjectRowScanner
	// calls, context, query, and arguments record the invocation.
	calls     int
	context   context.Context
	query     string
	arguments []any
}

// QueryRow implements the reader's narrow single-row seam.
func (stub *adminArchitectureProjectQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) adminArchitectureProjectRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)
	return stub.row
}

// adminArchitectureProjectRowStub returns one project projection or error.
type adminArchitectureProjectRowStub struct {
	// project supplies every successful projection value.
	project adminArchitectureProjectRecord
	// scanError simulates no rows, a driver failure, or decoding failure.
	scanError error
}

// Scan implements the fixed protected project projection.
func (stub *adminArchitectureProjectRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	return setAdminArchitectureProjectScanDestinations(stub.project, destinations)
}

// adminArchitectureProjectCoverRowStub returns one exact cover projection.
type adminArchitectureProjectCoverRowStub struct {
	// asset supplies the successful cover values.
	asset architectureProjectCoverAsset
	// scanError simulates no rows or dependency failure.
	scanError error
}

// Scan copies the configured twelve-column cover projection.
func (stub *adminArchitectureProjectCoverRowStub) Scan(
	destinations ...any,
) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 12 {
		return errors.New("admin architecture cover scan expected twelve destinations")
	}

	projectID, projectIDOK := destinations[0].(*int64)
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
	if !projectIDOK || !versionOK || !contentTypeOK || !contentOK ||
		!byteSizeOK || !widthOK || !heightOK || !digestOK || !altTextOK ||
		!captionOK || !createdAtOK || !updatedAtOK {
		return errors.New("admin architecture cover received unexpected destinations")
	}

	*projectID = stub.asset.ArchitectureProjectID
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

// setAdminArchitectureProjectScanDestinations copies one record into the exact
// eighteen-column SELECT shape used by list and detail operations.
func setAdminArchitectureProjectScanDestinations(
	project adminArchitectureProjectRecord,
	destinations []any,
) error {
	if len(destinations) != 18 {
		return errors.New("admin architecture scan expected eighteen destinations")
	}

	id, idOK := destinations[0].(*int64)
	slug, slugOK := destinations[1].(*string)
	title, titleOK := destinations[2].(*string)
	typology, typologyOK := destinations[3].(*string)
	location, locationOK := destinations[4].(*string)
	projectYear, projectYearOK := destinations[5].(*sql.NullInt64)
	projectStatus, projectStatusOK := destinations[6].(*string)
	description, descriptionOK := destinations[7].(*string)
	sortOrder, sortOrderOK := destinations[8].(*int)
	publicationStatus, publicationStatusOK := destinations[9].(*string)
	version, versionOK := destinations[15].(*int64)
	createdAt, createdAtOK := destinations[16].(*time.Time)
	updatedAt, updatedAtOK := destinations[17].(*time.Time)
	if !idOK || !slugOK || !titleOK || !typologyOK || !locationOK ||
		!projectYearOK || !projectStatusOK || !descriptionOK || !sortOrderOK ||
		!publicationStatusOK || !versionOK || !createdAtOK || !updatedAtOK {
		return errors.New("admin architecture received unexpected destinations")
	}

	*id = project.ID
	*slug = project.Slug
	*title = project.Title
	*typology = project.Typology
	*location = project.Location
	if project.ProjectYear == 0 {
		*projectYear = sql.NullInt64{}
	} else {
		*projectYear = sql.NullInt64{Int64: int64(project.ProjectYear), Valid: true}
	}
	*projectStatus = project.ProjectStatus
	*description = project.Description
	*sortOrder = project.SortOrder
	*publicationStatus = project.PublicationStatus
	if err := setAdminArchitectureProjectCoverDestinations(
		project.Cover,
		destinations[10:15],
	); err != nil {
		return err
	}
	*version = project.Version
	*createdAt = project.CreatedAt
	*updatedAt = project.UpdatedAt
	return nil
}

// setAdminArchitectureProjectCoverDestinations fills the nullable LEFT JOIN
// columns for either an absent or complete cover.
func setAdminArchitectureProjectCoverDestinations(
	cover *architectureProjectCoverMetadata,
	destinations []any,
) error {
	if len(destinations) != 5 {
		return errors.New("admin architecture cover expected five destinations")
	}

	version, versionOK := destinations[0].(*sql.NullInt64)
	width, widthOK := destinations[1].(*sql.NullInt64)
	height, heightOK := destinations[2].(*sql.NullInt64)
	altText, altTextOK := destinations[3].(*sql.NullString)
	caption, captionOK := destinations[4].(*sql.NullString)
	if !versionOK || !widthOK || !heightOK || !altTextOK || !captionOK {
		return errors.New("admin architecture cover received unexpected destinations")
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

// validAdminArchitectureProjectRecord returns one deterministic protected
// Architecture record satisfying every stored invariant.
func validAdminArchitectureProjectRecord(
	id int64,
	slug string,
	sortOrder int,
) adminArchitectureProjectRecord {
	createdAt := time.Date(2026, time.August, 15, 8, 0, 0, 0, time.UTC)
	return adminArchitectureProjectRecord{
		ID:                id,
		Slug:              slug,
		Title:             "Courtyard House",
		Typology:          "Residential",
		Location:          "Tehran",
		ProjectYear:       2026,
		ProjectStatus:     "Completed",
		Description:       "A fictional reviewed Architecture project.",
		SortOrder:         sortOrder,
		PublicationStatus: draftArchitectureProjectStatus,
		Version:           3,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt.Add(time.Hour),
	}
}

// validAdminArchitectureProjectCoverAsset returns a normalized, internally
// consistent cover record suitable for exact-read tests.
func validAdminArchitectureProjectCoverAsset(
	t *testing.T,
	projectID int64,
	version int64,
) architectureProjectCoverAsset {
	t.Helper()
	content := testAdminArchitectureProjectCoverPNG(t)
	inspection, err := inspectReviewedCover(content, false)
	if err != nil {
		t.Fatalf("inspect deterministic Architecture cover: %v", err)
	}
	createdAt := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	return architectureProjectCoverAsset{
		ArchitectureProjectID: projectID,
		Version:               version,
		ContentType:           inspection.ContentType,
		Content:               append([]byte(nil), content...),
		ByteSize:              len(content),
		Width:                 inspection.Width,
		Height:                inspection.Height,
		SHA256:                inspection.SHA256,
		AltText:               "A fictional Architecture cover",
		Caption:               "Reviewed test cover.",
		CreatedAt:             createdAt,
		UpdatedAt:             createdAt.Add(time.Minute),
	}
}

// TestNewPostgresAdminArchitectureProjectReader verifies constructor
// dependency validation without issuing a query.
func TestNewPostgresAdminArchitectureProjectReader(t *testing.T) {
	reader, err := newPostgresAdminArchitectureProjectReader(nil)
	if !errors.Is(err, errAdminArchitectureProjectReaderDatabaseRequired) ||
		reader != nil {
		t.Fatalf("nil database result: reader=%#v err=%v", reader, err)
	}

	reader, err = newPostgresAdminArchitectureProjectReader(&sql.DB{})
	if err != nil {
		t.Fatalf("construct reader: %v", err)
	}
	if reader == nil || reader.query == nil || reader.queryRow == nil {
		t.Fatal("constructed reader did not install query adapters")
	}
}

// TestPostgresAdminArchitectureProjectReaderList verifies exact SQL, caller
// context, ordering, nullable year, and optional cover projection behavior.
func TestPostgresAdminArchitectureProjectReaderList(t *testing.T) {
	first := validAdminArchitectureProjectRecord(4, "courtyard-house", 2)
	first.ProjectYear = 0
	second := validAdminArchitectureProjectRecord(7, "civic-courtyard", 2)
	second.Cover = &architectureProjectCoverMetadata{
		Version: 2,
		Width:   1600,
		Height:  1000,
		AltText: "A fictional civic courtyard",
		Caption: "Reviewed test cover.",
	}
	rows := newAdminArchitectureProjectRowsStub(
		[]adminArchitectureProjectRecord{first, second},
	)
	query := &adminArchitectureProjectQueryStub{rows: rows}
	reader := &postgresAdminArchitectureProjectReader{query: query.Query}
	ctx := context.Background()

	projects, err := reader.List(ctx)
	if err != nil {
		t.Fatalf("list Architecture projects: %v", err)
	}
	if !reflect.DeepEqual(projects, []adminArchitectureProjectRecord{first, second}) {
		t.Errorf("projects: got %#v", projects)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != listAdminArchitectureProjectsSQL ||
		len(query.arguments) != 0 || rows.closeCalls != 1 {
		t.Errorf("list invocation: calls=%d query=%q args=%#v closes=%d", query.calls, query.query, query.arguments, rows.closeCalls)
	}
	if projects != nil && cap(projects) != len(projects) {
		t.Errorf("slice capacity: got %d, want %d", cap(projects), len(projects))
	}
}

// TestPostgresAdminArchitectureProjectReaderListRejectsBrokenContracts proves
// invalid ordering, duplicate keys, and iterator failures are fail-closed.
func TestPostgresAdminArchitectureProjectReaderListRejectsBrokenContracts(t *testing.T) {
	valid := validAdminArchitectureProjectRecord(4, "valid-architecture", 2)
	tests := []struct {
		// name identifies the malformed dependency behavior.
		name string
		// rows and queryError supply that behavior.
		rows       adminArchitectureProjectRows
		queryError error
	}{
		{name: "nil rows"},
		{name: "query failure", rows: newAdminArchitectureProjectRowsStub(nil), queryError: errors.New("unsafe query detail")},
		{name: "invalid record", rows: newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{func() adminArchitectureProjectRecord { value := valid; value.Title = " bad "; return value }()})},
		{name: "out of order", rows: newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{validAdminArchitectureProjectRecord(5, "later", 3), validAdminArchitectureProjectRecord(6, "earlier", 2)})},
		{name: "duplicate identity", rows: newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{valid, validAdminArchitectureProjectRecord(valid.ID, "different-slug", 3)})},
		{name: "duplicate slug", rows: newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{valid, validAdminArchitectureProjectRecord(8, valid.Slug, 3)})},
		{name: "iteration failure", rows: func() adminArchitectureProjectRows {
			rows := newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{valid})
			rows.iterationError = errors.New("unsafe iteration detail")
			return rows
		}()},
		{name: "close failure", rows: func() adminArchitectureProjectRows {
			rows := newAdminArchitectureProjectRowsStub([]adminArchitectureProjectRecord{valid})
			rows.closeError = errors.New("unsafe close detail")
			return rows
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &adminArchitectureProjectQueryStub{rows: test.rows, queryError: test.queryError}
			reader := &postgresAdminArchitectureProjectReader{query: query.Query}
			projects, err := reader.List(context.Background())
			if err != errAdminArchitectureProjectReadFailed || projects != nil {
				t.Fatalf("projects=%#v err=%v", projects, err)
			}
			if strings.Contains(err.Error(), "unsafe") {
				t.Error("reader error exposed dependency detail")
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectReaderFindByID verifies exact ID binding,
// complete projection, and safe missing/error classification.
func TestPostgresAdminArchitectureProjectReaderFindByID(t *testing.T) {
	want := validAdminArchitectureProjectRecord(17, "gallery-building", 4)
	query := &adminArchitectureProjectQueryRowStub{
		row: &adminArchitectureProjectRowStub{project: want},
	}
	reader := &postgresAdminArchitectureProjectReader{queryRow: query.QueryRow}

	project, err := reader.FindByID(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if !reflect.DeepEqual(project, want) ||
		query.query != findAdminArchitectureProjectByIDSQL ||
		!reflect.DeepEqual(query.arguments, []any{want.ID}) {
		t.Errorf("project=%#v query=%q args=%#v", project, query.query, query.arguments)
	}

	for _, test := range []struct {
		name string
		row  adminArchitectureProjectRowScanner
		want error
	}{
		{name: "nil row", want: errAdminArchitectureProjectReadFailed},
		{name: "not found", row: &adminArchitectureProjectRowStub{scanError: sql.ErrNoRows}, want: errAdminArchitectureProjectNotFound},
		{name: "driver", row: &adminArchitectureProjectRowStub{scanError: errors.New("unsafe")}, want: errAdminArchitectureProjectReadFailed},
		{name: "wrong identity", row: &adminArchitectureProjectRowStub{project: validAdminArchitectureProjectRecord(18, "wrong-id", 1)}, want: errAdminArchitectureProjectReadFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &postgresAdminArchitectureProjectReader{queryRow: (&adminArchitectureProjectQueryRowStub{row: test.row}).QueryRow}
			got, err := reader.FindByID(context.Background(), 17)
			if err != test.want || got != (adminArchitectureProjectRecord{}) {
				t.Fatalf("project=%#v err=%v, want %v", got, err, test.want)
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectReaderFindCover verifies exact cover
// coordinates, a defensive byte copy, and safe failure classification.
func TestPostgresAdminArchitectureProjectReaderFindCover(t *testing.T) {
	want := validAdminArchitectureProjectCoverAsset(t, 19, 3)
	query := &adminArchitectureProjectQueryRowStub{
		row: &adminArchitectureProjectCoverRowStub{asset: want},
	}
	reader := &postgresAdminArchitectureProjectReader{queryRow: query.QueryRow}

	got, err := reader.FindCoverByProjectID(context.Background(), 19, 3)
	if err != nil {
		t.Fatalf("find cover: %v", err)
	}
	if !reflect.DeepEqual(got, want) ||
		query.query != findAdminArchitectureProjectCoverSQL ||
		!reflect.DeepEqual(query.arguments, []any{int64(19), int64(3)}) {
		t.Errorf("cover=%#v query=%q args=%#v", got, query.query, query.arguments)
	}

	for _, test := range []struct {
		name string
		row  adminArchitectureProjectRowScanner
		want error
	}{
		{name: "nil row", want: errArchitectureProjectCoverReadFailed},
		{name: "not found", row: &adminArchitectureProjectCoverRowStub{scanError: sql.ErrNoRows}, want: errArchitectureProjectCoverNotFound},
		{name: "driver", row: &adminArchitectureProjectCoverRowStub{scanError: errors.New("unsafe")}, want: errArchitectureProjectCoverReadFailed},
		{name: "wrong owner", row: &adminArchitectureProjectCoverRowStub{asset: validAdminArchitectureProjectCoverAsset(t, 20, 3)}, want: errArchitectureProjectCoverReadFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &postgresAdminArchitectureProjectReader{queryRow: (&adminArchitectureProjectQueryRowStub{row: test.row}).QueryRow}
			got, err := reader.FindCoverByProjectID(context.Background(), 19, 3)
			if err != test.want ||
				!reflect.DeepEqual(got, architectureProjectCoverAsset{}) {
				t.Fatalf("cover=%#v err=%v, want %v", got, err, test.want)
			}
		})
	}
}

// TestScanAdminArchitectureProjectRejectsPartialCover proves a corrupt LEFT
// JOIN projection cannot masquerade as a complete or absent cover.
func TestScanAdminArchitectureProjectRejectsPartialCover(t *testing.T) {
	base := validAdminArchitectureProjectRecord(1, "partial-cover", 1)
	scanner := adminArchitectureProjectScannerFunc(func(destinations ...any) error {
		if err := setAdminArchitectureProjectScanDestinations(base, destinations); err != nil {
			return err
		}
		*destinations[10].(*sql.NullInt64) = sql.NullInt64{Int64: 1, Valid: true}
		return nil
	})

	project, err := scanAdminArchitectureProject(scanner)
	if err != errAdminArchitectureProjectReadFailed ||
		project != (adminArchitectureProjectRecord{}) {
		t.Fatalf("project=%#v err=%v", project, err)
	}
}

// adminArchitectureProjectScannerFunc adapts a focused closure to the
// projection interface for malformed-nullability tests.
type adminArchitectureProjectScannerFunc func(...any) error

// Scan delegates to the configured focused closure.
func (scanner adminArchitectureProjectScannerFunc) Scan(destinations ...any) error {
	return scanner(destinations...)
}

// TestIsValidStoredAdminArchitectureProject covers lifecycle, cover, revision,
// order, and timestamp invariants in addition to shared public field bounds.
func TestIsValidStoredAdminArchitectureProject(t *testing.T) {
	valid := validAdminArchitectureProjectRecord(1, "stored-architecture", 1)
	validWithCover := valid
	validWithCover.Cover = &architectureProjectCoverMetadata{Version: 1, Width: 1600, Height: 1000, AltText: "A fictional stored Architecture cover"}

	tests := []struct {
		name    string
		project adminArchitectureProjectRecord
		want    bool
	}{
		{name: "valid", project: valid, want: true},
		{name: "valid cover", project: validWithCover, want: true},
		{name: "missing year", project: func() adminArchitectureProjectRecord { value := valid; value.ProjectYear = 0; return value }(), want: true},
		{name: "zero identity", project: func() adminArchitectureProjectRecord { value := valid; value.ID = 0; return value }()},
		{name: "invalid slug", project: func() adminArchitectureProjectRecord { value := valid; value.Slug = "Bad Slug"; return value }()},
		{name: "invalid year", project: func() adminArchitectureProjectRecord { value := valid; value.ProjectYear = 999; return value }()},
		{name: "zero order", project: func() adminArchitectureProjectRecord { value := valid; value.SortOrder = 0; return value }()},
		{name: "unsupported lifecycle", project: func() adminArchitectureProjectRecord {
			value := valid
			value.PublicationStatus = "deleted"
			return value
		}()},
		{name: "invalid cover", project: func() adminArchitectureProjectRecord {
			value := validWithCover
			cover := *value.Cover
			cover.Version = 0
			value.Cover = &cover
			return value
		}()},
		{name: "zero revision", project: func() adminArchitectureProjectRecord { value := valid; value.Version = 0; return value }()},
		{name: "missing timestamp", project: func() adminArchitectureProjectRecord { value := valid; value.CreatedAt = time.Time{}; return value }()},
		{name: "reversed timestamps", project: func() adminArchitectureProjectRecord {
			value := valid
			value.UpdatedAt = value.CreatedAt.Add(-time.Second)
			return value
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isValidStoredAdminArchitectureProject(test.project); got != test.want {
				t.Errorf("validity: got %t, want %t", got, test.want)
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectReaderRejectsInvalidQueries proves bad
// coordinates and unusable receivers never reach a database seam.
func TestPostgresAdminArchitectureProjectReaderRejectsInvalidQueries(t *testing.T) {
	query := &adminArchitectureProjectQueryStub{}
	queryRow := &adminArchitectureProjectQueryRowStub{}
	reader := &postgresAdminArchitectureProjectReader{query: query.Query, queryRow: queryRow.QueryRow}

	if _, err := reader.List(nil); err != errAdminArchitectureProjectInvalidQuery {
		t.Errorf("nil list context error: %v", err)
	}
	if _, err := reader.FindByID(context.Background(), 0); err != errAdminArchitectureProjectInvalidQuery {
		t.Errorf("zero detail ID error: %v", err)
	}
	if _, err := reader.FindCoverByProjectID(context.Background(), 1, 0); err != errAdminArchitectureProjectInvalidQuery {
		t.Errorf("zero cover version error: %v", err)
	}
	if query.calls != 0 || queryRow.calls != 0 {
		t.Errorf("invalid query reached dependency: list=%d row=%d", query.calls, queryRow.calls)
	}

	var nilReader *postgresAdminArchitectureProjectReader
	if _, err := nilReader.List(context.Background()); err != errAdminArchitectureProjectReadFailed {
		t.Errorf("nil reader list error: %v", err)
	}
	if _, err := nilReader.FindByID(context.Background(), 1); err != errAdminArchitectureProjectReadFailed {
		t.Errorf("nil reader detail error: %v", err)
	}
}
