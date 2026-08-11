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

// adminInteriorProjectQueryStub records one list invocation and returns a
// controlled iterator or dependency failure.
type adminInteriorProjectQueryStub struct {
	// rows is the iterator returned by Query.
	rows adminInteriorProjectRows
	// queryError simulates database/sql failing before iteration.
	queryError error
	// calls records the number of attempted list queries.
	calls int
	// context records the exact caller context.
	context context.Context
	// query records the fixed SQL statement.
	query string
	// arguments records any bound values; the list query must have none.
	arguments []any
}

// Query implements adminInteriorProjectQuery and records its complete call.
func (stub *adminInteriorProjectQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (adminInteriorProjectRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.rows, stub.queryError
}

// adminInteriorProjectRowsStub exposes records through the exact iterator used
// by the production list method.
type adminInteriorProjectRowsStub struct {
	// projects are exposed in their configured order.
	projects []adminInteriorProjectRecord
	// scanErrorAt is the zero-based failing row; -1 disables that failure.
	scanErrorAt int
	// scanError is returned for the configured row.
	scanError error
	// iterationError is reported after the final Next call.
	iterationError error
	// closeError simulates result finalization failure.
	closeError error
	// nextIndex identifies the next record exposed by Next.
	nextIndex int
	// currentIndex identifies the record currently available to Scan.
	currentIndex int
	// closeCalls proves every acquired iterator is finalized.
	closeCalls int
}

// newAdminInteriorProjectRowsStub initializes the disabled scan index and the
// pre-iteration current-row marker.
func newAdminInteriorProjectRowsStub(
	projects []adminInteriorProjectRecord,
) *adminInteriorProjectRowsStub {
	return &adminInteriorProjectRowsStub{
		projects:     projects,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances through the finite configured project slice.
func (stub *adminInteriorProjectRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.projects) {
		return false
	}

	stub.currentIndex = stub.nextIndex
	stub.nextIndex++

	return true
}

// Scan copies the current record into the fixed protected projection.
func (stub *adminInteriorProjectRowsStub) Scan(destinations ...any) error {
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.projects) {
		return errors.New("admin interior project scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}

	return setAdminInteriorProjectScanDestinations(
		stub.projects[stub.currentIndex],
		destinations,
	)
}

// Err returns the configured post-iteration failure.
func (stub *adminInteriorProjectRowsStub) Err() error {
	return stub.iterationError
}

// Close records finalization and returns the configured result.
func (stub *adminInteriorProjectRowsStub) Close() error {
	stub.closeCalls++

	return stub.closeError
}

// adminInteriorProjectQueryRowStub records one protected detail or cover query.
type adminInteriorProjectQueryRowStub struct {
	// row is returned to the repository.
	row adminInteriorProjectRowScanner
	// calls records the number of attempts.
	calls int
	// context records the exact caller context.
	context context.Context
	// query records the fixed SQL statement.
	query string
	// arguments records bound project/media coordinates.
	arguments []any
}

// QueryRow implements adminInteriorProjectQueryRow and records its invocation.
func (stub *adminInteriorProjectQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) adminInteriorProjectRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// adminInteriorProjectRowStub returns one project projection or scan error.
type adminInteriorProjectRowStub struct {
	// project supplies every successful projection value.
	project adminInteriorProjectRecord
	// scanError simulates no rows, driver failure, or decoding failure.
	scanError error
}

// Scan implements the fixed protected project projection.
func (stub *adminInteriorProjectRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}

	return setAdminInteriorProjectScanDestinations(
		stub.project,
		destinations,
	)
}

// adminInteriorProjectScannerFunc adapts a focused closure to the projection
// interface for malformed-nullability tests.
type adminInteriorProjectScannerFunc func(...any) error

// Scan delegates to the configured focused closure.
func (scanner adminInteriorProjectScannerFunc) Scan(
	destinations ...any,
) error {
	return scanner(destinations...)
}

// setAdminInteriorProjectScanDestinations copies one record into the exact
// eighteen-column SELECT shape used by both list and detail operations.
func setAdminInteriorProjectScanDestinations(
	project adminInteriorProjectRecord,
	destinations []any,
) error {
	if len(destinations) != 18 {
		return errors.New("admin interior project scan expected eighteen destinations")
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
		!projectYearOK || !projectStatusOK || !descriptionOK ||
		!sortOrderOK || !publicationStatusOK || !versionOK ||
		!createdAtOK || !updatedAtOK {
		return errors.New("admin interior project scan received unexpected destinations")
	}

	*id = project.ID
	*slug = project.Slug
	*title = project.Title
	*typology = project.Typology
	*location = project.Location
	if project.ProjectYear == 0 {
		*projectYear = sql.NullInt64{}
	} else {
		*projectYear = sql.NullInt64{
			Int64: int64(project.ProjectYear),
			Valid: true,
		}
	}
	*projectStatus = project.ProjectStatus
	*description = project.Description
	*sortOrder = project.SortOrder
	*publicationStatus = project.PublicationStatus
	if err := setAdminInteriorProjectCoverDestinations(
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

// setAdminInteriorProjectCoverDestinations fills the nullable LEFT JOIN columns
// for either an absent or complete cover.
func setAdminInteriorProjectCoverDestinations(
	cover *interiorProjectCoverMetadata,
	destinations []any,
) error {
	if len(destinations) != 5 {
		return errors.New("admin interior cover scan expected five destinations")
	}

	version, versionOK := destinations[0].(*sql.NullInt64)
	width, widthOK := destinations[1].(*sql.NullInt64)
	height, heightOK := destinations[2].(*sql.NullInt64)
	altText, altTextOK := destinations[3].(*sql.NullString)
	caption, captionOK := destinations[4].(*sql.NullString)
	if !versionOK || !widthOK || !heightOK || !altTextOK || !captionOK {
		return errors.New("admin interior cover scan received unexpected destinations")
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

// validAdminInteriorProjectRecord returns one deterministic protected record.
func validAdminInteriorProjectRecord(
	id int64,
	slug string,
	sortOrder int,
) adminInteriorProjectRecord {
	createdAt := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)

	return adminInteriorProjectRecord{
		ID:                id,
		Slug:              slug,
		Title:             "Atrium Residence",
		Typology:          "Residential",
		Location:          "Tehran",
		ProjectYear:       2025,
		ProjectStatus:     "Completed",
		Description:       "A fictional reviewed Interior project.",
		SortOrder:         sortOrder,
		PublicationStatus: draftInteriorProjectStatus,
		Version:           3,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt.Add(time.Hour),
	}
}

// TestNewPostgresAdminInteriorProjectReader verifies constructor dependency
// validation without issuing a database query.
func TestNewPostgresAdminInteriorProjectReader(t *testing.T) {
	reader, err := newPostgresAdminInteriorProjectReader(nil)
	if !errors.Is(err, errAdminInteriorProjectReaderDatabaseRequired) ||
		reader != nil {
		t.Fatalf("nil database result: reader=%#v err=%v", reader, err)
	}

	reader, err = newPostgresAdminInteriorProjectReader(&sql.DB{})
	if err != nil {
		t.Fatalf("construct reader: %v", err)
	}
	if reader == nil || reader.query == nil || reader.queryRow == nil {
		t.Fatal("constructed reader did not install query adapters")
	}
}

// TestPostgresAdminInteriorProjectReaderList verifies exact SQL, caller context,
// ordering, nullable year, and optional-cover projection behavior.
func TestPostgresAdminInteriorProjectReaderList(t *testing.T) {
	first := validAdminInteriorProjectRecord(4, "atrium-residence", 2)
	first.ProjectYear = 0
	second := validAdminInteriorProjectRecord(7, "courtyard-workplace", 2)
	second.Cover = &interiorProjectCoverMetadata{
		Version: 2,
		Width:   1600,
		Height:  1000,
		AltText: "A fictional courtyard workplace interior",
		Caption: "Reviewed test cover.",
	}
	rows := newAdminInteriorProjectRowsStub(
		[]adminInteriorProjectRecord{first, second},
	)
	query := &adminInteriorProjectQueryStub{rows: rows}
	reader := &postgresAdminInteriorProjectReader{query: query.Query}
	ctx := context.WithValue(
		context.Background(),
		struct{ name string }{"admin-interior-list"},
		"sentinel",
	)

	projects, err := reader.List(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if !reflect.DeepEqual(projects, []adminInteriorProjectRecord{first, second}) {
		t.Errorf("projects: got %#v", projects)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != listAdminInteriorProjectsSQL ||
		len(query.arguments) != 0 {
		t.Errorf(
			"list invocation: calls=%d query=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
	if rows.closeCalls != 1 {
		t.Errorf("row close calls: got %d, want 1", rows.closeCalls)
	}
	if projects != nil && cap(projects) != len(projects) {
		t.Errorf("returned slice capacity: got %d, want %d", cap(projects), len(projects))
	}
}

// TestPostgresAdminInteriorProjectReaderListRejectsBrokenContracts proves the
// reader rejects invalid ordering, duplicate identity/slug, and iterator errors.
func TestPostgresAdminInteriorProjectReaderListRejectsBrokenContracts(t *testing.T) {
	valid := validAdminInteriorProjectRecord(4, "valid-interior", 2)
	tests := []struct {
		// name identifies the malformed dependency behavior.
		name string
		// rows supplies that behavior.
		rows adminInteriorProjectRows
		// queryError simulates failure before iteration.
		queryError error
	}{
		{name: "nil rows"},
		{name: "query failure", rows: newAdminInteriorProjectRowsStub(nil), queryError: errors.New("unsafe query detail")},
		{name: "invalid record", rows: newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{func() adminInteriorProjectRecord { value := valid; value.Title = " bad "; return value }()})},
		{name: "out of order", rows: newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{validAdminInteriorProjectRecord(5, "later", 3), validAdminInteriorProjectRecord(6, "earlier", 2)})},
		{name: "duplicate identity", rows: newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{valid, validAdminInteriorProjectRecord(valid.ID, "different-slug", 3)})},
		{name: "duplicate slug", rows: newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{valid, validAdminInteriorProjectRecord(8, valid.Slug, 3)})},
		{name: "iteration failure", rows: func() adminInteriorProjectRows {
			rows := newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{valid})
			rows.iterationError = errors.New("unsafe iteration detail")
			return rows
		}()},
		{name: "close failure", rows: func() adminInteriorProjectRows {
			rows := newAdminInteriorProjectRowsStub([]adminInteriorProjectRecord{valid})
			rows.closeError = errors.New("unsafe close detail")
			return rows
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &adminInteriorProjectQueryStub{
				rows:       test.rows,
				queryError: test.queryError,
			}
			reader := &postgresAdminInteriorProjectReader{query: query.Query}
			projects, err := reader.List(context.Background())
			if err != errAdminInteriorProjectReadFailed || projects != nil {
				t.Fatalf("projects=%#v err=%v, want safe read failure", projects, err)
			}
			if strings.Contains(err.Error(), "unsafe") {
				t.Error("reader error exposed dependency detail")
			}
		})
	}
}

// TestPostgresAdminInteriorProjectReaderFindByID verifies exact positive-ID
// binding and complete protected-record validation.
func TestPostgresAdminInteriorProjectReaderFindByID(t *testing.T) {
	want := validAdminInteriorProjectRecord(17, "gallery-apartment", 4)
	query := &adminInteriorProjectQueryRowStub{
		row: &adminInteriorProjectRowStub{project: want},
	}
	reader := &postgresAdminInteriorProjectReader{queryRow: query.QueryRow}
	ctx := context.Background()

	project, err := reader.FindByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if !reflect.DeepEqual(project, want) {
		t.Errorf("project: got %#v, want %#v", project, want)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findAdminInteriorProjectByIDSQL ||
		!reflect.DeepEqual(query.arguments, []any{want.ID}) {
		t.Errorf("detail invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}
}

// TestPostgresAdminInteriorProjectReaderFindByIDClassifiesFailures keeps a
// genuine missing row distinct while redacting all other dependency failures.
func TestPostgresAdminInteriorProjectReaderFindByIDClassifiesFailures(t *testing.T) {
	tests := []struct {
		// name identifies the scanner outcome.
		name string
		// row is returned by the query seam.
		row adminInteriorProjectRowScanner
		// want is the stable expected error.
		want error
	}{
		{name: "nil row", want: errAdminInteriorProjectReadFailed},
		{name: "not found", row: &adminInteriorProjectRowStub{scanError: sql.ErrNoRows}, want: errAdminInteriorProjectNotFound},
		{name: "driver failure", row: &adminInteriorProjectRowStub{scanError: errors.New("unsafe credentials")}, want: errAdminInteriorProjectReadFailed},
		{name: "wrong identity", row: &adminInteriorProjectRowStub{project: validAdminInteriorProjectRecord(18, "wrong-id", 1)}, want: errAdminInteriorProjectReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &adminInteriorProjectQueryRowStub{row: test.row}
			reader := &postgresAdminInteriorProjectReader{queryRow: query.QueryRow}
			project, err := reader.FindByID(context.Background(), 17)
			if err != test.want || project != (adminInteriorProjectRecord{}) {
				t.Fatalf("project=%#v err=%v, want %v", project, err, test.want)
			}
		})
	}
}

// TestScanAdminInteriorProjectRejectsPartialCover proves a corrupt LEFT JOIN
// projection cannot be mistaken for either a complete cover or no cover.
func TestScanAdminInteriorProjectRejectsPartialCover(t *testing.T) {
	base := validAdminInteriorProjectRecord(1, "partial-cover", 1)
	scanner := adminInteriorProjectScannerFunc(func(destinations ...any) error {
		if err := setAdminInteriorProjectScanDestinations(base, destinations); err != nil {
			return err
		}
		coverVersion := destinations[10].(*sql.NullInt64)
		*coverVersion = sql.NullInt64{Int64: 1, Valid: true}

		return nil
	})

	project, err := scanAdminInteriorProject(scanner)
	if err != errAdminInteriorProjectReadFailed ||
		project != (adminInteriorProjectRecord{}) {
		t.Fatalf("project=%#v err=%v, want partial-cover failure", project, err)
	}
}

// TestIsValidStoredAdminInteriorProject verifies the protected projection adds
// identity, lifecycle, cover, revision, ordering, and timestamp invariants to
// the shared public text boundaries.
func TestIsValidStoredAdminInteriorProject(t *testing.T) {
	valid := validAdminInteriorProjectRecord(1, "stored-interior", 1)
	validWithCover := valid
	validWithCover.Cover = &interiorProjectCoverMetadata{
		Version: 1,
		Width:   1600,
		Height:  1000,
		AltText: "A fictional stored Interior cover",
	}
	validWithoutYear := valid
	validWithoutYear.ProjectYear = 0

	tests := []struct {
		// name identifies one stored-record invariant.
		name string
		// project is the complete candidate record.
		project adminInteriorProjectRecord
		// want records whether the candidate is valid.
		want bool
	}{
		{name: "valid", project: valid, want: true},
		{name: "valid cover", project: validWithCover, want: true},
		{name: "valid missing year", project: validWithoutYear, want: true},
		{name: "zero identity", project: func() adminInteriorProjectRecord { value := valid; value.ID = 0; return value }()},
		{name: "invalid slug", project: func() adminInteriorProjectRecord { value := valid; value.Slug = "Bad Slug"; return value }()},
		{name: "invalid year", project: func() adminInteriorProjectRecord { value := valid; value.ProjectYear = 999; return value }()},
		{name: "zero order", project: func() adminInteriorProjectRecord { value := valid; value.SortOrder = 0; return value }()},
		{name: "unsupported lifecycle", project: func() adminInteriorProjectRecord { value := valid; value.PublicationStatus = "deleted"; return value }()},
		{name: "invalid cover", project: func() adminInteriorProjectRecord {
			value := validWithCover
			cover := *value.Cover
			cover.Version = 0
			value.Cover = &cover
			return value
		}()},
		{name: "zero revision", project: func() adminInteriorProjectRecord { value := valid; value.Version = 0; return value }()},
		{name: "missing creation time", project: func() adminInteriorProjectRecord { value := valid; value.CreatedAt = time.Time{}; return value }()},
		{name: "reversed timestamps", project: func() adminInteriorProjectRecord {
			value := valid
			value.UpdatedAt = value.CreatedAt.Add(-time.Second)
			return value
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isValidStoredAdminInteriorProject(test.project); got != test.want {
				t.Errorf("validity: got %t, want %t", got, test.want)
			}
		})
	}
}

// TestPostgresAdminInteriorProjectReaderRejectsInvalidQueries proves invalid
// caller coordinates and unusable receivers never reach a database seam.
func TestPostgresAdminInteriorProjectReaderRejectsInvalidQueries(t *testing.T) {
	query := &adminInteriorProjectQueryStub{}
	queryRow := &adminInteriorProjectQueryRowStub{}
	reader := &postgresAdminInteriorProjectReader{
		query:    query.Query,
		queryRow: queryRow.QueryRow,
	}

	if _, err := reader.List(nil); err != errAdminInteriorProjectInvalidQuery {
		t.Errorf("nil list context error: got %v", err)
	}
	for _, id := range []int64{0, -1} {
		if _, err := reader.FindByID(context.Background(), id); err != errAdminInteriorProjectInvalidQuery {
			t.Errorf("ID %d error: got %v", id, err)
		}
	}
	if query.calls != 0 || queryRow.calls != 0 {
		t.Errorf("invalid query reached dependency: list=%d row=%d", query.calls, queryRow.calls)
	}

	var nilReader *postgresAdminInteriorProjectReader
	if _, err := nilReader.List(context.Background()); err != errAdminInteriorProjectReadFailed {
		t.Errorf("nil reader list error: got %v", err)
	}
	if _, err := nilReader.FindByID(context.Background(), 1); err != errAdminInteriorProjectReadFailed {
		t.Errorf("nil reader detail error: got %v", err)
	}
}
