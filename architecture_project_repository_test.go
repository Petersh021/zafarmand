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

// architectureProjectRowsStub implements the minimal list iterator while
// exposing close/error behavior needed to verify resource cleanup.
type architectureProjectRowsStub struct {
	rows         [][]any
	index        int
	scanErrAt    int
	iterationErr error
	closeErr     error
	closed       bool
}

// Next advances to a configured row until all fixtures have been visited.
func (rows *architectureProjectRowsStub) Next() bool {
	return rows != nil && rows.index < len(rows.rows)
}

// Scan copies the current fixture through the same concrete destinations used
// by database/sql, then advances the iterator.
func (rows *architectureProjectRowsStub) Scan(destinations ...any) error {
	if rows == nil || rows.index >= len(rows.rows) {
		return errors.New("test row unavailable")
	}
	current := rows.index
	rows.index++
	if rows.scanErrAt >= 0 && current == rows.scanErrAt {
		return errors.New("synthetic scan failure")
	}
	return assignArchitectureProjectRow(destinations, rows.rows[current])
}

// Err returns an iteration failure configured independently from Scan.
func (rows *architectureProjectRowsStub) Err() error {
	return rows.iterationErr
}

// Close records resource release and returns its configured failure.
func (rows *architectureProjectRowsStub) Close() error {
	rows.closed = true
	return rows.closeErr
}

// architectureProjectRowStub adapts a single fixture or error to QueryRow.
type architectureProjectRowStub struct {
	values []any
	err    error
}

// Scan copies a fixed row unless a synthetic database result was configured.
func (row *architectureProjectRowStub) Scan(destinations ...any) error {
	if row == nil {
		return errors.New("nil synthetic row")
	}
	if row.err != nil {
		return row.err
	}
	return assignArchitectureProjectRow(destinations, row.values)
}

// assignArchitectureProjectRow simulates database/sql conversions for the
// exact scalar and nullable destination types used by this repository.
func assignArchitectureProjectRow(destinations []any, values []any) error {
	if len(destinations) != len(values) {
		return errors.New("synthetic column count mismatch")
	}

	for index, destination := range destinations {
		value := values[index]
		switch output := destination.(type) {
		case *int64:
			converted, ok := value.(int64)
			if !ok {
				return errors.New("synthetic int64 type mismatch")
			}
			*output = converted
		case *int:
			converted, ok := value.(int)
			if !ok {
				return errors.New("synthetic int type mismatch")
			}
			*output = converted
		case *string:
			converted, ok := value.(string)
			if !ok {
				return errors.New("synthetic string type mismatch")
			}
			*output = converted
		case *[]byte:
			converted, ok := value.([]byte)
			if !ok {
				return errors.New("synthetic bytes type mismatch")
			}
			*output = append((*output)[:0], converted...)
		case *time.Time:
			converted, ok := value.(time.Time)
			if !ok {
				return errors.New("synthetic time type mismatch")
			}
			*output = converted
		case *sql.NullInt64:
			if value == nil {
				*output = sql.NullInt64{}
				continue
			}
			converted, ok := value.(int64)
			if !ok {
				return errors.New("synthetic nullable int64 type mismatch")
			}
			*output = sql.NullInt64{Int64: converted, Valid: true}
		case *sql.NullString:
			if value == nil {
				*output = sql.NullString{}
				continue
			}
			converted, ok := value.(string)
			if !ok {
				return errors.New("synthetic nullable string type mismatch")
			}
			*output = sql.NullString{String: converted, Valid: true}
		default:
			return errors.New("unsupported synthetic destination")
		}
	}

	return nil
}

// architectureProjectColumns builds one valid fourteen-column public row.
func architectureProjectColumns(
	id int64,
	number int64,
	slug string,
	cover *architectureProjectCoverMetadata,
) []any {
	values := []any{
		id,
		number,
		slug,
		"Architecture repository title",
		"Residential",
		"Tehran",
		int64(2026),
		"Completed",
		"Reviewed architecture description.",
		nil,
		nil,
		nil,
		nil,
		nil,
	}
	if cover != nil {
		values[9] = cover.Version
		values[10] = int64(cover.Width)
		values[11] = int64(cover.Height)
		values[12] = cover.AltText
		values[13] = cover.Caption
	}
	return values
}

// architectureProjectAssetColumns builds one valid twelve-column media row.
func architectureProjectAssetColumns(
	asset architectureProjectCoverAsset,
) []any {
	return []any{
		asset.ArchitectureProjectID,
		asset.Version,
		asset.ContentType,
		asset.Content,
		asset.ByteSize,
		asset.Width,
		asset.Height,
		asset.SHA256[:],
		asset.AltText,
		asset.Caption,
		asset.CreatedAt,
		asset.UpdatedAt,
	}
}

// validArchitectureProjectCoverAsset returns a security-contract-valid PNG
// fixture whose derived facts and digest match its exact decoded bytes.
func validArchitectureProjectCoverAsset(
	t *testing.T,
) architectureProjectCoverAsset {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetRGBA(
				x,
				y,
				color.RGBA{
					R: uint8(35 + x*20),
					G: uint8(55 + y*25),
					B: 105,
					A: 255,
				},
			)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode deterministic Architecture cover PNG: %v", err)
	}
	content := encoded.Bytes()
	inspection, err := inspectReviewedCover(content, true)
	if err != nil {
		t.Fatalf("inspect deterministic Architecture cover: %v", err)
	}
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	return architectureProjectCoverAsset{
		ArchitectureProjectID: 17,
		Version:               3,
		ContentType:           inspection.ContentType,
		Content:               content,
		ByteSize:              len(content),
		Width:                 inspection.Width,
		Height:                inspection.Height,
		SHA256:                inspection.SHA256,
		AltText:               "A fictional architecture model used in a test",
		Caption:               "Synthetic reviewed caption",
		CreatedAt:             now,
		UpdatedAt:             now.Add(time.Minute),
	}
}

// TestNewPostgresArchitectureProjectCatalogueReader verifies that composition
// fails early without a pool and otherwise installs both SQL adapters.
func TestNewPostgresArchitectureProjectCatalogueReader(t *testing.T) {
	reader, err := newPostgresArchitectureProjectCatalogueReader(nil)
	if reader != nil || !errors.Is(
		err,
		errArchitectureProjectCatalogueReaderDatabaseRequired,
	) {
		t.Fatalf("nil database result: reader=%#v error=%v", reader, err)
	}

	reader, err = newPostgresArchitectureProjectCatalogueReader(&sql.DB{})
	if err != nil {
		t.Fatalf("non-nil database: %v", err)
	}
	if reader == nil || reader.query == nil || reader.queryRow == nil {
		t.Fatal("constructed reader does not contain both SQL adapters")
	}
}

// TestPostgresArchitectureProjectCatalogueReaderListsPublished proves query
// arguments, public order, nullable conversion, cover mapping, and row cleanup.
func TestPostgresArchitectureProjectCatalogueReaderListsPublished(t *testing.T) {
	cover := &architectureProjectCoverMetadata{
		Version: 8,
		Width:   1920,
		Height:  1080,
		AltText: "Synthetic architecture cover",
		Caption: "Synthetic caption",
	}
	rows := &architectureProjectRowsStub{
		rows: [][]any{
			architectureProjectColumns(17, 1, "first-public-project", nil),
			architectureProjectColumns(29, 2, "second-public-project", cover),
		},
		scanErrAt: -1,
	}
	var actualQuery string
	var actualArguments []any
	reader := &postgresArchitectureProjectCatalogueReader{
		query: func(
			_ context.Context,
			query string,
			arguments ...any,
		) (architectureProjectCatalogueRows, error) {
			actualQuery = query
			actualArguments = append([]any(nil), arguments...)
			return rows, nil
		},
	}

	projects, err := reader.ListPublished(context.Background())
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	if actualQuery != listPublishedArchitectureProjectsSQL {
		t.Error("list used an unexpected SQL statement")
	}
	if !reflect.DeepEqual(
		actualArguments,
		[]any{publishedArchitectureProjectStatus},
	) {
		t.Errorf("list arguments: got %#v", actualArguments)
	}
	if !rows.closed {
		t.Error("successful list did not close rows")
	}
	if len(projects) != 2 || projects[0].PortfolioNumber != 1 ||
		projects[1].PortfolioNumber != 2 {
		t.Fatalf("published results: got %#v", projects)
	}
	if projects[0].Cover != nil {
		t.Errorf("absent cover mapped as %#v", projects[0].Cover)
	}
	if projects[1].Cover == nil || *projects[1].Cover != *cover {
		t.Errorf("cover metadata: got %#v, want %#v", projects[1].Cover, cover)
	}
}

// TestPostgresArchitectureProjectCatalogueReaderListsAllocatedEmpty verifies a
// zero-row result is a usable non-nil slice rather than an error or nil value.
func TestPostgresArchitectureProjectCatalogueReaderListsAllocatedEmpty(t *testing.T) {
	rows := &architectureProjectRowsStub{scanErrAt: -1}
	reader := &postgresArchitectureProjectCatalogueReader{
		query: func(
			context.Context,
			string,
			...any,
		) (architectureProjectCatalogueRows, error) {
			return rows, nil
		},
	}
	projects, err := reader.ListPublished(context.Background())
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if projects == nil || len(projects) != 0 {
		t.Fatalf("empty result: got %#v, want allocated empty slice", projects)
	}
}

// TestPostgresArchitectureProjectCatalogueReaderRejectsListFailures covers
// invalid calls, unavailable readers, SQL errors, malformed rows, duplicates,
// numbering gaps, iterator failures, and close failures.
func TestPostgresArchitectureProjectCatalogueReaderRejectsListFailures(t *testing.T) {
	validFirst := architectureProjectColumns(17, 1, "valid-first", nil)
	validSecond := architectureProjectColumns(29, 2, "valid-second", nil)
	invalidTitle := architectureProjectColumns(17, 1, "invalid-title", nil)
	invalidTitle[3] = " padded title "

	tests := []struct {
		name   string
		reader *postgresArchitectureProjectCatalogueReader
		ctx    context.Context
	}{
		{name: "nil context", reader: &postgresArchitectureProjectCatalogueReader{}, ctx: nil},
		{name: "nil reader", reader: nil, ctx: context.Background()},
		{name: "missing query", reader: &postgresArchitectureProjectCatalogueReader{}, ctx: context.Background()},
		{
			name: "query error",
			ctx:  context.Background(),
			reader: &postgresArchitectureProjectCatalogueReader{query: func(context.Context, string, ...any) (architectureProjectCatalogueRows, error) {
				return nil, errors.New("driver detail must stay hidden")
			}},
		},
		{
			name: "nil rows",
			ctx:  context.Background(),
			reader: &postgresArchitectureProjectCatalogueReader{query: func(context.Context, string, ...any) (architectureProjectCatalogueRows, error) {
				return nil, nil
			}},
		},
		{
			name:   "scan failure",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{validFirst}, scanErrAt: 0}),
		},
		{
			name:   "malformed stored text",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{invalidTitle}, scanErrAt: -1}),
		},
		{
			name:   "number gap",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{architectureProjectColumns(17, 2, "gap", nil)}, scanErrAt: -1}),
		},
		{
			name:   "duplicate identity",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{validFirst, architectureProjectColumns(17, 2, "other-slug", nil)}, scanErrAt: -1}),
		},
		{
			name:   "duplicate slug",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{validFirst, architectureProjectColumns(29, 2, "valid-first", nil)}, scanErrAt: -1}),
		},
		{
			name:   "iteration error",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{validFirst, validSecond}, scanErrAt: -1, iterationErr: errors.New("iteration")}),
		},
		{
			name:   "close error",
			ctx:    context.Background(),
			reader: readerWithArchitectureRows(&architectureProjectRowsStub{rows: [][]any{validFirst}, scanErrAt: -1, closeErr: errors.New("close")}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projects, err := test.reader.ListPublished(test.ctx)
			if !errors.Is(err, expectedArchitectureListError(test.ctx)) {
				t.Fatalf("error: got %v", err)
			}
			if projects != nil {
				t.Errorf("failed list exposed %#v", projects)
			}
			if err != nil && strings.Contains(err.Error(), "driver detail") {
				t.Errorf("repository exposed driver diagnostic: %q", err)
			}
		})
	}
}

// expectedArchitectureListError separates caller misuse from safe read failure.
func expectedArchitectureListError(ctx context.Context) error {
	if ctx == nil {
		return errArchitectureProjectCatalogueInvalidQuery
	}
	return errArchitectureProjectCatalogueReadFailed
}

// readerWithArchitectureRows installs one deterministic list result.
func readerWithArchitectureRows(
	rows architectureProjectCatalogueRows,
) *postgresArchitectureProjectCatalogueReader {
	return &postgresArchitectureProjectCatalogueReader{
		query: func(context.Context, string, ...any) (architectureProjectCatalogueRows, error) {
			return rows, nil
		},
	}
}

// TestPostgresArchitectureProjectCatalogueReaderFindsPublishedBySlug verifies
// the complete-window SQL, argument order, nullable year, and cover projection.
func TestPostgresArchitectureProjectCatalogueReaderFindsPublishedBySlug(t *testing.T) {
	cover := &architectureProjectCoverMetadata{
		Version: 5,
		Width:   1400,
		Height:  900,
		AltText: "Architecture detail cover",
		Caption: "Reviewed detail caption",
	}
	var query string
	var arguments []any
	reader := &postgresArchitectureProjectCatalogueReader{
		queryRow: func(_ context.Context, statement string, values ...any) architectureProjectCatalogueRowScanner {
			query = statement
			arguments = append([]any(nil), values...)
			return &architectureProjectRowStub{values: architectureProjectColumns(41, 7, "published-detail", cover)}
		},
	}

	project, err := reader.FindPublishedBySlug(context.Background(), "published-detail")
	if err != nil {
		t.Fatalf("find published detail: %v", err)
	}
	if query != findPublishedArchitectureProjectBySlugSQL {
		t.Error("detail used an unexpected SQL statement")
	}
	if !reflect.DeepEqual(arguments, []any{publishedArchitectureProjectStatus, "published-detail"}) {
		t.Errorf("detail arguments: got %#v", arguments)
	}
	if project.ID != 41 || project.PortfolioNumber != 7 || project.ProjectYear != 2026 ||
		project.Cover == nil || *project.Cover != *cover {
		t.Errorf("detail result: got %#v", project)
	}
}

// TestPostgresArchitectureProjectCatalogueReaderRejectsDetailFailures verifies
// local query validation and safe mapping of missing or malformed records.
func TestPostgresArchitectureProjectCatalogueReaderRejectsDetailFailures(t *testing.T) {
	for _, slug := range []string{"", "Uppercase", "two--hyphens", "trailing-", strings.Repeat("a", 121)} {
		project, err := (&postgresArchitectureProjectCatalogueReader{}).FindPublishedBySlug(context.Background(), slug)
		if !errors.Is(err, errArchitectureProjectCatalogueInvalidQuery) || project != (catalogueArchitectureProject{}) {
			t.Errorf("invalid slug %q: project=%#v error=%v", slug, project, err)
		}
	}

	tests := []struct {
		name     string
		reader   *postgresArchitectureProjectCatalogueReader
		expected error
	}{
		{name: "nil reader", reader: nil, expected: errArchitectureProjectCatalogueReadFailed},
		{name: "missing query adapter", reader: &postgresArchitectureProjectCatalogueReader{}, expected: errArchitectureProjectCatalogueReadFailed},
		{name: "nil row", reader: &postgresArchitectureProjectCatalogueReader{queryRow: func(context.Context, string, ...any) architectureProjectCatalogueRowScanner { return nil }}, expected: errArchitectureProjectCatalogueReadFailed},
		{name: "not found", reader: architectureDetailReader(&architectureProjectRowStub{err: sql.ErrNoRows}), expected: errArchitectureProjectCatalogueNotFound},
		{name: "scan error", reader: architectureDetailReader(&architectureProjectRowStub{err: errors.New("secret driver diagnostic")}), expected: errArchitectureProjectCatalogueReadFailed},
		{name: "mismatched slug", reader: architectureDetailReader(&architectureProjectRowStub{values: architectureProjectColumns(1, 1, "different-project", nil)}), expected: errArchitectureProjectCatalogueReadFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project, err := test.reader.FindPublishedBySlug(context.Background(), "requested-project")
			if !errors.Is(err, test.expected) {
				t.Fatalf("error: got %v, want %v", err, test.expected)
			}
			if project != (catalogueArchitectureProject{}) {
				t.Errorf("failed detail exposed %#v", project)
			}
		})
	}
}

// architectureDetailReader returns a reader with one configured single row.
func architectureDetailReader(
	row architectureProjectCatalogueRowScanner,
) *postgresArchitectureProjectCatalogueReader {
	return &postgresArchitectureProjectCatalogueReader{
		queryRow: func(context.Context, string, ...any) architectureProjectCatalogueRowScanner {
			return row
		},
	}
}

// TestScanCatalogueArchitectureProjectRejectsPartialCover verifies that a
// broken LEFT JOIN projection cannot appear as a valid absent image.
func TestScanCatalogueArchitectureProjectRejectsPartialCover(t *testing.T) {
	values := architectureProjectColumns(1, 1, "partial-cover", nil)
	values[9] = int64(3)
	_, err := scanCatalogueArchitectureProject(&architectureProjectRowStub{values: values})
	if !errors.Is(err, errArchitectureProjectCatalogueReadFailed) {
		t.Fatalf("partial cover error: got %v", err)
	}
}

// TestPostgresArchitectureProjectCatalogueReaderFindsPublishedCover proves
// exact SQL coordinates, asset validation, and byte-slice isolation.
func TestPostgresArchitectureProjectCatalogueReaderFindsPublishedCover(t *testing.T) {
	expected := validArchitectureProjectCoverAsset(t)
	var query string
	var arguments []any
	reader := &postgresArchitectureProjectCatalogueReader{
		queryRow: func(_ context.Context, statement string, values ...any) architectureProjectCatalogueRowScanner {
			query = statement
			arguments = append([]any(nil), values...)
			return &architectureProjectRowStub{values: architectureProjectAssetColumns(expected)}
		},
	}

	asset, err := reader.FindPublishedCover(context.Background(), "public-cover", expected.Version)
	if err != nil {
		t.Fatalf("find published cover: %v", err)
	}
	if query != findPublishedArchitectureProjectCoverSQL {
		t.Error("cover used an unexpected SQL statement")
	}
	if !reflect.DeepEqual(arguments, []any{"public-cover", publishedArchitectureProjectStatus, expected.Version}) {
		t.Errorf("cover arguments: got %#v", arguments)
	}
	if !reflect.DeepEqual(asset, expected) {
		t.Errorf("cover: got %#v, want %#v", asset, expected)
	}
	asset.Content[0] ^= 0xff
	if bytes.Equal(asset.Content, expected.Content) {
		t.Error("returned cover bytes were not isolated")
	}
}

// TestPostgresArchitectureProjectCatalogueReaderFindsPublishedCoverMetadata
// verifies exact public predicates and a projection that omits encoded content.
func TestPostgresArchitectureProjectCatalogueReaderFindsPublishedCoverMetadata(t *testing.T) {
	asset := validArchitectureProjectCoverAsset(t)
	metadata := asset.responseMetadata()
	values := []any{
		metadata.OwnerID,
		metadata.Version,
		metadata.ContentType,
		metadata.ByteSize,
		metadata.Width,
		metadata.Height,
		metadata.SHA256[:],
		metadata.AltText,
		metadata.Caption,
		metadata.CreatedAt,
		metadata.UpdatedAt,
	}
	var query string
	var arguments []any
	reader := &postgresArchitectureProjectCatalogueReader{
		queryRow: func(_ context.Context, statement string, bound ...any) architectureProjectCatalogueRowScanner {
			query = statement
			arguments = append([]any(nil), bound...)
			return &architectureProjectRowStub{values: values}
		},
	}

	got, err := reader.FindPublishedCoverMetadata(
		context.Background(),
		"public-cover",
		asset.Version,
	)
	if err != nil {
		t.Fatalf("find published cover metadata: %v", err)
	}
	if got != metadata {
		t.Errorf("metadata: got %#v, want %#v", got, metadata)
	}
	if query != findPublishedArchitectureProjectCoverMetadataSQL ||
		strings.Contains(query, "cover.content,") ||
		!reflect.DeepEqual(
			arguments,
			[]any{"public-cover", publishedArchitectureProjectStatus, asset.Version},
		) {
		t.Errorf("metadata query=%q args=%#v", query, arguments)
	}
}

// TestPostgresArchitectureProjectCatalogueReaderRejectsCoverFailures verifies
// malformed coordinates, hidden/missing rows, stale versions, and bad media.
func TestPostgresArchitectureProjectCatalogueReaderRejectsCoverFailures(t *testing.T) {
	for _, request := range []struct {
		slug    string
		version int64
	}{
		{slug: "", version: 1},
		{slug: "Invalid", version: 1},
		{slug: "valid-slug", version: 0},
		{slug: "valid-slug", version: -1},
	} {
		asset, err := (&postgresArchitectureProjectCatalogueReader{}).FindPublishedCover(context.Background(), request.slug, request.version)
		if !errors.Is(err, errArchitectureProjectCatalogueInvalidQuery) ||
			!reflect.DeepEqual(asset, architectureProjectCoverAsset{}) {
			t.Errorf("invalid cover request %#v: asset=%#v error=%v", request, asset, err)
		}
	}

	valid := validArchitectureProjectCoverAsset(t)
	wrongVersion := valid
	wrongVersion.Version++
	badDigest := architectureProjectAssetColumns(valid)
	badDigest[7] = []byte{1, 2, 3}
	tests := []struct {
		name     string
		reader   *postgresArchitectureProjectCatalogueReader
		expected error
	}{
		{name: "nil reader", reader: nil, expected: errArchitectureProjectCoverReadFailed},
		{name: "missing adapter", reader: &postgresArchitectureProjectCatalogueReader{}, expected: errArchitectureProjectCoverReadFailed},
		{name: "nil row", reader: &postgresArchitectureProjectCatalogueReader{queryRow: func(context.Context, string, ...any) architectureProjectCatalogueRowScanner { return nil }}, expected: errArchitectureProjectCoverReadFailed},
		{name: "not found", reader: architectureDetailReader(&architectureProjectRowStub{err: sql.ErrNoRows}), expected: errArchitectureProjectCoverNotFound},
		{name: "scan error", reader: architectureDetailReader(&architectureProjectRowStub{err: errors.New("driver")}), expected: errArchitectureProjectCoverReadFailed},
		{name: "bad digest", reader: architectureDetailReader(&architectureProjectRowStub{values: badDigest}), expected: errArchitectureProjectCoverReadFailed},
		{name: "stale returned version", reader: architectureDetailReader(&architectureProjectRowStub{values: architectureProjectAssetColumns(wrongVersion)}), expected: errArchitectureProjectCoverReadFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := test.reader.FindPublishedCover(context.Background(), "valid-cover", valid.Version)
			if !errors.Is(err, test.expected) {
				t.Fatalf("error: got %v, want %v", err, test.expected)
			}
			if !reflect.DeepEqual(asset, architectureProjectCoverAsset{}) {
				t.Errorf("failed cover exposed %#v", asset)
			}
		})
	}
}

// TestArchitectureProjectCatalogueValidationBoundaries covers route grammar,
// optional year semantics, cover integrity, and complete-list uniqueness.
func TestArchitectureProjectCatalogueValidationBoundaries(t *testing.T) {
	for _, slug := range []string{"a", "house-2026", strings.Repeat("a", 120)} {
		if !isCanonicalArchitectureProjectSlug(slug) {
			t.Errorf("valid slug rejected: %q", slug)
		}
	}
	for _, slug := range []string{"", "House", "house_1", "-house", "house-", "house--one", strings.Repeat("a", 121)} {
		if isCanonicalArchitectureProjectSlug(slug) {
			t.Errorf("invalid slug accepted: %q", slug)
		}
	}
	for _, year := range []int{0, 1000, 2026, 9999} {
		if !isValidArchitectureProjectYear(year) {
			t.Errorf("valid year rejected: %d", year)
		}
	}
	for _, year := range []int{-1, 1, 999, 10000} {
		if isValidArchitectureProjectYear(year) {
			t.Errorf("invalid year accepted: %d", year)
		}
	}

	valid := catalogueArchitectureProject{
		ID:              1,
		PortfolioNumber: 1,
		Slug:            "valid-project",
		Title:           "Valid project",
		Typology:        "Residential",
		ProjectStatus:   "Completed",
	}
	if !isValidCatalogueArchitectureProject(valid) {
		t.Fatal("valid public project was rejected")
	}
	duplicateID := valid
	duplicateID.PortfolioNumber = 2
	duplicateID.Slug = "other-project"
	if isValidPublishedArchitectureProjectCatalogue([]catalogueArchitectureProject{valid, duplicateID}) {
		t.Error("published catalogue accepted duplicate identity")
	}
	duplicateSlug := valid
	duplicateSlug.ID = 2
	duplicateSlug.PortfolioNumber = 2
	if isValidPublishedArchitectureProjectCatalogue([]catalogueArchitectureProject{valid, duplicateSlug}) {
		t.Error("published catalogue accepted duplicate slug")
	}
	gap := valid
	gap.ID = 2
	gap.Slug = "gap-project"
	gap.PortfolioNumber = 3
	if isValidPublishedArchitectureProjectCatalogue([]catalogueArchitectureProject{valid, gap}) {
		t.Error("published catalogue accepted non-consecutive numbers")
	}
}
