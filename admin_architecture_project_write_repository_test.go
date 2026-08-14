package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// testAdminArchitectureProjectCoverPNG encodes a tiny deterministic fictional
// image so cover tests exercise the real standard-library decoder.
func testAdminArchitectureProjectCoverPNG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetNRGBA(x, y, color.NRGBA{
				R: uint8(30 + x*20),
				G: uint8(40 + y*30),
				B: uint8(70 + x*10 + y*5),
				A: 255,
			})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatalf("encode deterministic Architecture cover: %v", err)
	}
	return buffer.Bytes()
}

// validAdminArchitectureProjectCoverWriteInput derives all persisted facts
// from one decoded deterministic image instead of trusting test constants.
func validAdminArchitectureProjectCoverWriteInput(
	t *testing.T,
) adminArchitectureProjectCoverWriteInput {
	t.Helper()
	content := testAdminArchitectureProjectCoverPNG(t)
	inspection, err := inspectReviewedCover(content, false)
	if err != nil {
		t.Fatalf("inspect deterministic Architecture cover: %v", err)
	}
	return adminArchitectureProjectCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     content,
		ByteSize:    len(content),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "A fictional Architecture model viewed from a courtyard",
		Caption:     "Reviewed fictional test cover.",
	}
}

// recordingAdminArchitectureProjectWriteQuery captures one mutation and
// returns a controlled scanner without requiring PostgreSQL.
type recordingAdminArchitectureProjectWriteQuery struct {
	// calls, context, query, and arguments record the complete invocation.
	calls     int
	context   context.Context
	query     string
	arguments []any
	// row is returned to the repository.
	row adminArchitectureProjectWriteRowScanner
}

// QueryRow implements the writer's narrow database seam.
func (query *recordingAdminArchitectureProjectWriteQuery) QueryRow(
	ctx context.Context,
	statement string,
	arguments ...any,
) adminArchitectureProjectWriteRowScanner {
	query.calls++
	query.context = ctx
	query.query = statement
	query.arguments = append([]any(nil), arguments...)
	return query.row
}

// adminArchitectureProjectWriteRowStub supplies text or media mutation result
// columns and can simulate a scan failure.
type adminArchitectureProjectWriteRowStub struct {
	// result supplies identity and revision for text mutations.
	result adminArchitectureProjectWriteResult
	// projectExists is the final existence column.
	projectExists bool
	// coverResult supplies project and cover revisions for media mutations.
	coverResult adminArchitectureProjectCoverWriteResult
	// scanError is returned before any destination changes.
	scanError error
}

// Scan copies the configured two-, three-, or four-column result shape.
func (row *adminArchitectureProjectWriteRowStub) Scan(
	destinations ...any,
) error {
	if row.scanError != nil {
		return row.scanError
	}

	switch len(destinations) {
	case 2:
		id, idOK := destinations[0].(*int64)
		version, versionOK := destinations[1].(*int64)
		if !idOK || !versionOK {
			return errors.New("architecture create received unexpected destinations")
		}
		*id = row.result.ID
		*version = row.result.Version
	case 3:
		id, idOK := destinations[0].(*int64)
		version, versionOK := destinations[1].(*int64)
		exists, existsOK := destinations[2].(*bool)
		if !idOK || !versionOK || !existsOK {
			return errors.New("architecture update received unexpected destinations")
		}
		*id = row.result.ID
		*version = row.result.Version
		*exists = row.projectExists
	case 4:
		projectID, projectIDOK := destinations[0].(*int64)
		projectVersion, projectVersionOK := destinations[1].(*int64)
		coverVersion, coverVersionOK := destinations[2].(*int64)
		exists, existsOK := destinations[3].(*bool)
		if !projectIDOK || !projectVersionOK || !coverVersionOK || !existsOK {
			return errors.New("architecture cover received unexpected destinations")
		}
		*projectID = row.coverResult.ProjectID
		*projectVersion = row.coverResult.ProjectVersion
		*coverVersion = row.coverResult.CoverVersion
		*exists = row.projectExists
	default:
		return errors.New("architecture write received unexpected column count")
	}
	return nil
}

// validAdminArchitectureProjectWriteInput returns one deterministic value that
// satisfies every editable migration-8 constraint.
func validAdminArchitectureProjectWriteInput() adminArchitectureProjectWriteInput {
	return adminArchitectureProjectWriteInput{
		Slug:              "courtyard-house",
		Title:             "Courtyard House",
		Typology:          "Residential",
		Location:          "Tehran",
		ProjectYear:       2026,
		ProjectStatus:     "Completed",
		Description:       "A fictional reviewed Architecture project.",
		SortOrder:         4,
		PublicationStatus: draftArchitectureProjectStatus,
	}
}

// TestNewPostgresAdminArchitectureProjectWriter verifies constructor
// dependency validation without performing a write.
func TestNewPostgresAdminArchitectureProjectWriter(t *testing.T) {
	writer, err := newPostgresAdminArchitectureProjectWriter(nil)
	if !errors.Is(err, errAdminArchitectureProjectWriterDatabaseRequired) ||
		writer != nil {
		t.Fatalf("nil database result: writer=%#v err=%v", writer, err)
	}

	writer, err = newPostgresAdminArchitectureProjectWriter(&sql.DB{})
	if err != nil {
		t.Fatalf("construct writer: %v", err)
	}
	if writer == nil || writer.queryRow == nil {
		t.Fatal("constructed writer did not install query adapter")
	}
}

// TestPostgresAdminArchitectureProjectWriterCreate verifies exact SQL binding,
// nullable year conversion, and the database-owned result pair.
func TestPostgresAdminArchitectureProjectWriterCreate(t *testing.T) {
	ctx := context.Background()
	input := validAdminArchitectureProjectWriteInput()
	query := &recordingAdminArchitectureProjectWriteQuery{
		row: &adminArchitectureProjectWriteRowStub{
			result: adminArchitectureProjectWriteResult{ID: 41, Version: 1},
		},
	}
	writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}

	result, err := writer.Create(ctx, input)
	if err != nil {
		t.Fatalf("create Architecture project: %v", err)
	}
	if result != (adminArchitectureProjectWriteResult{ID: 41, Version: 1}) {
		t.Errorf("create result: %#v", result)
	}
	wantArguments := []any{
		input.Slug,
		input.Title,
		input.Typology,
		input.Location,
		input.ProjectYear,
		input.ProjectStatus,
		input.Description,
		input.SortOrder,
		input.PublicationStatus,
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != createAdminArchitectureProjectSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("create invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}

	input.ProjectYear = 0
	query = &recordingAdminArchitectureProjectWriteQuery{
		row: &adminArchitectureProjectWriteRowStub{
			result: adminArchitectureProjectWriteResult{ID: 42, Version: 1},
		},
	}
	writer.queryRow = query.QueryRow
	if _, err := writer.Create(ctx, input); err != nil {
		t.Fatalf("create without year: %v", err)
	}
	if query.arguments[4] != nil {
		t.Errorf("missing year argument: got %#v, want nil", query.arguments[4])
	}
}

// TestPostgresAdminArchitectureProjectWriterCreateRejectsInvalidInput proves
// every editable boundary and nil context fail before the database seam.
func TestPostgresAdminArchitectureProjectWriterCreateRejectsInvalidInput(t *testing.T) {
	valid := validAdminArchitectureProjectWriteInput()
	tests := []struct {
		name  string
		ctx   context.Context
		input adminArchitectureProjectWriteInput
	}{
		{name: "nil context", input: valid},
		{name: "bad slug", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.Slug = "Bad Slug"; return value }()},
		{name: "empty title", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.Title = ""; return value }()},
		{name: "long title", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.Title = strings.Repeat("t", architectureProjectTitleMaximumLength+1)
			return value
		}()},
		{name: "untrimmed typology", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.Typology = " Residential "
			return value
		}()},
		{name: "long location", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.Location = strings.Repeat("l", architectureProjectLocationMaximumLength+1)
			return value
		}()},
		{name: "year below range", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.ProjectYear = 999; return value }()},
		{name: "empty project status", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.ProjectStatus = ""; return value }()},
		{name: "untrimmed description", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.Description = " copy "; return value }()},
		{name: "description contains nul", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.Description = "copy\x00tail"
			return value
		}()},
		{name: "long description", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.Description = strings.Repeat("d", architectureProjectDescriptionMaximumLength+1)
			return value
		}()},
		{name: "zero order", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput { value := valid; value.SortOrder = 0; return value }()},
		{name: "large order", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.SortOrder = math.MaxInt32 + 1
			return value
		}()},
		{name: "unsupported lifecycle", ctx: context.Background(), input: func() adminArchitectureProjectWriteInput {
			value := valid
			value.PublicationStatus = "deleted"
			return value
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminArchitectureProjectWriteQuery{}
			writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Create(test.ctx, test.input)
			if err != errAdminArchitectureProjectWriteInvalid ||
				result != (adminArchitectureProjectWriteResult{}) || query.calls != 0 {
				t.Fatalf("result=%#v err=%v calls=%d", result, err, query.calls)
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectWriterCreateClassifiesFailures verifies
// the one safe slug conflict and redaction of every other database failure.
func TestPostgresAdminArchitectureProjectWriterCreateClassifiesFailures(t *testing.T) {
	unsafeDetail := "password=unsafe-architecture-detail"
	tests := []struct {
		name string
		row  adminArchitectureProjectWriteRowScanner
		want error
	}{
		{name: "nil row", want: errAdminArchitectureProjectWriteFailed},
		{name: "slug conflict", row: &adminArchitectureProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "architecture_projects_slug_unique"}}, want: errAdminArchitectureProjectSlugConflict},
		{name: "other unique", row: &adminArchitectureProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "future_unique"}}, want: errAdminArchitectureProjectWriteFailed},
		{name: "driver detail", row: &adminArchitectureProjectWriteRowStub{scanError: errors.New(unsafeDetail)}, want: errAdminArchitectureProjectWriteFailed},
		{name: "zero result", row: &adminArchitectureProjectWriteRowStub{}, want: errAdminArchitectureProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminArchitectureProjectWriteQuery{row: test.row}
			writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Create(context.Background(), validAdminArchitectureProjectWriteInput())
			if err != test.want || result != (adminArchitectureProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
			if strings.Contains(err.Error(), unsafeDetail) {
				t.Error("writer error exposed dependency detail")
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectWriterUpdate verifies optimistic-lock SQL
// arguments and the required one-step revision increment.
func TestPostgresAdminArchitectureProjectWriterUpdate(t *testing.T) {
	ctx := context.Background()
	input := validAdminArchitectureProjectWriteInput()
	query := &recordingAdminArchitectureProjectWriteQuery{
		row: &adminArchitectureProjectWriteRowStub{
			result:        adminArchitectureProjectWriteResult{ID: 17, Version: 6},
			projectExists: true,
		},
	}
	writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}

	result, err := writer.Update(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("update Architecture project: %v", err)
	}
	if result != (adminArchitectureProjectWriteResult{ID: 17, Version: 6}) {
		t.Errorf("update result: %#v", result)
	}
	wantArguments := []any{int64(17), int64(5), input.Slug, input.Title, input.Typology, input.Location, input.ProjectYear, input.ProjectStatus, input.Description, input.SortOrder, input.PublicationStatus}
	if query.query != updateAdminArchitectureProjectSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("update query=%q args=%#v", query.query, query.arguments)
	}
}

// TestPostgresAdminArchitectureProjectWriterUpdateClassifiesOutcomes keeps
// missing, stale, slug-collision, and operational outcomes distinct and safe.
func TestPostgresAdminArchitectureProjectWriterUpdateClassifiesOutcomes(t *testing.T) {
	tests := []struct {
		name string
		row  adminArchitectureProjectWriteRowScanner
		want error
	}{
		{name: "missing", row: &adminArchitectureProjectWriteRowStub{}, want: errAdminArchitectureProjectNotFound},
		{name: "stale", row: &adminArchitectureProjectWriteRowStub{projectExists: true}, want: errAdminArchitectureProjectWriteConflict},
		{name: "slug conflict", row: &adminArchitectureProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "architecture_projects_slug_unique"}}, want: errAdminArchitectureProjectSlugConflict},
		{name: "wrong identity", row: &adminArchitectureProjectWriteRowStub{result: adminArchitectureProjectWriteResult{ID: 18, Version: 6}, projectExists: true}, want: errAdminArchitectureProjectWriteFailed},
		{name: "wrong revision", row: &adminArchitectureProjectWriteRowStub{result: adminArchitectureProjectWriteResult{ID: 17, Version: 7}, projectExists: true}, want: errAdminArchitectureProjectWriteFailed},
		{name: "result without existence", row: &adminArchitectureProjectWriteRowStub{result: adminArchitectureProjectWriteResult{ID: 17, Version: 6}}, want: errAdminArchitectureProjectWriteFailed},
		{name: "nil row", want: errAdminArchitectureProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminArchitectureProjectWriteQuery{row: test.row}
			writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Update(context.Background(), 17, 5, validAdminArchitectureProjectWriteInput())
			if err != test.want || result != (adminArchitectureProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectWriterUpdateRejectsCoordinates proves
// invalid IDs, revisions, inputs, contexts, and receivers cannot write.
func TestPostgresAdminArchitectureProjectWriterUpdateRejectsCoordinates(t *testing.T) {
	valid := validAdminArchitectureProjectWriteInput()
	tests := []struct {
		name    string
		writer  *postgresAdminArchitectureProjectWriter
		ctx     context.Context
		id      int64
		version int64
		input   adminArchitectureProjectWriteInput
		want    error
	}{
		{name: "nil context", writer: &postgresAdminArchitectureProjectWriter{}, id: 1, version: 1, input: valid, want: errAdminArchitectureProjectWriteInvalid},
		{name: "zero identity", writer: &postgresAdminArchitectureProjectWriter{}, ctx: context.Background(), version: 1, input: valid, want: errAdminArchitectureProjectWriteInvalid},
		{name: "zero revision", writer: &postgresAdminArchitectureProjectWriter{}, ctx: context.Background(), id: 1, input: valid, want: errAdminArchitectureProjectWriteInvalid},
		{name: "maximum revision", writer: &postgresAdminArchitectureProjectWriter{}, ctx: context.Background(), id: 1, version: math.MaxInt64, input: valid, want: errAdminArchitectureProjectWriteInvalid},
		{name: "invalid input", writer: &postgresAdminArchitectureProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminArchitectureProjectWriteInput { value := valid; value.Title = ""; return value }(), want: errAdminArchitectureProjectWriteInvalid},
		{name: "nil receiver", ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminArchitectureProjectWriteFailed},
		{name: "missing query", writer: &postgresAdminArchitectureProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminArchitectureProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.writer.Update(test.ctx, test.id, test.version, test.input)
			if err != test.want || result != (adminArchitectureProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestArchitectureProjectWriteSQLRetainsConcurrencyBoundaries guards the CTE
// and revision assumptions independently from driver behavior.
func TestArchitectureProjectWriteSQLRetainsConcurrencyBoundaries(t *testing.T) {
	for _, required := range []string{
		"WITH current_project AS MATERIALIZED",
		"WHERE id = $1 AND version = $2",
		"version = version + 1",
		"EXISTS(SELECT 1 FROM current_project)",
	} {
		if !strings.Contains(updateAdminArchitectureProjectSQL, required) {
			t.Errorf("update SQL does not contain %q", required)
		}
	}
}

// TestPostgresAdminArchitectureProjectWriterUpsertCover verifies exact image
// binding and the atomic one-step project/cover revision result.
func TestPostgresAdminArchitectureProjectWriterUpsertCover(t *testing.T) {
	ctx := context.Background()
	input := validAdminArchitectureProjectCoverWriteInput(t)
	query := &recordingAdminArchitectureProjectWriteQuery{
		row: &adminArchitectureProjectWriteRowStub{
			coverResult: adminArchitectureProjectCoverWriteResult{
				ProjectID:      17,
				ProjectVersion: 6,
				CoverVersion:   2,
			},
			projectExists: true,
		},
	}
	writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}

	result, err := writer.UpsertCover(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("upsert Architecture cover: %v", err)
	}
	if result != (adminArchitectureProjectCoverWriteResult{
		ProjectID:      17,
		ProjectVersion: 6,
		CoverVersion:   2,
	}) {
		t.Errorf("cover result: %#v", result)
	}
	if query.query != upsertAdminArchitectureProjectCoverSQL ||
		query.calls != 1 || query.context != ctx || len(query.arguments) != 10 {
		t.Errorf("cover invocation: calls=%d query=%q args=%d", query.calls, query.query, len(query.arguments))
	}
	if got, ok := query.arguments[7].([]byte); !ok ||
		!bytes.Equal(got, input.SHA256[:]) {
		t.Errorf("digest argument: %#v", query.arguments[7])
	}
}

// TestPostgresAdminArchitectureProjectWriterUpsertCoverRejectsInvalidInput
// proves invalid coordinates and byte metadata fail before any write.
func TestPostgresAdminArchitectureProjectWriterUpsertCoverRejectsInvalidInput(t *testing.T) {
	valid := validAdminArchitectureProjectCoverWriteInput(t)
	tests := []struct {
		name    string
		ctx     context.Context
		id      int64
		version int64
		input   adminArchitectureProjectCoverWriteInput
	}{
		{name: "nil context", id: 1, version: 1, input: valid},
		{name: "zero identity", ctx: context.Background(), version: 1, input: valid},
		{name: "zero version", ctx: context.Background(), id: 1, input: valid},
		{name: "maximum version", ctx: context.Background(), id: 1, version: math.MaxInt64, input: valid},
		{name: "wrong byte size", ctx: context.Background(), id: 1, version: 1, input: func() adminArchitectureProjectCoverWriteInput { value := valid; value.ByteSize++; return value }()},
		{name: "wrong digest", ctx: context.Background(), id: 1, version: 1, input: func() adminArchitectureProjectCoverWriteInput { value := valid; value.SHA256[0] ^= 0xff; return value }()},
		{name: "empty alt text", ctx: context.Background(), id: 1, version: 1, input: func() adminArchitectureProjectCoverWriteInput { value := valid; value.AltText = ""; return value }()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminArchitectureProjectWriteQuery{}
			writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}
			result, err := writer.UpsertCover(
				test.ctx,
				test.id,
				test.version,
				test.input,
			)
			if err != errAdminArchitectureProjectWriteInvalid ||
				result != (adminArchitectureProjectCoverWriteResult{}) || query.calls != 0 {
				t.Fatalf("result=%#v err=%v calls=%d", result, err, query.calls)
			}
		})
	}
}

// TestPostgresAdminArchitectureProjectWriterUpsertCoverClassifiesOutcomes
// keeps missing, stale, malformed, and dependency outcomes separate and safe.
func TestPostgresAdminArchitectureProjectWriterUpsertCoverClassifiesOutcomes(t *testing.T) {
	tests := []struct {
		name string
		row  adminArchitectureProjectWriteRowScanner
		want error
	}{
		{name: "missing", row: &adminArchitectureProjectWriteRowStub{}, want: errAdminArchitectureProjectNotFound},
		{name: "stale", row: &adminArchitectureProjectWriteRowStub{projectExists: true}, want: errAdminArchitectureProjectWriteConflict},
		{name: "wrong owner", row: &adminArchitectureProjectWriteRowStub{coverResult: adminArchitectureProjectCoverWriteResult{ProjectID: 18, ProjectVersion: 6, CoverVersion: 2}, projectExists: true}, want: errAdminArchitectureProjectWriteFailed},
		{name: "wrong project revision", row: &adminArchitectureProjectWriteRowStub{coverResult: adminArchitectureProjectCoverWriteResult{ProjectID: 17, ProjectVersion: 7, CoverVersion: 2}, projectExists: true}, want: errAdminArchitectureProjectWriteFailed},
		{name: "zero cover revision", row: &adminArchitectureProjectWriteRowStub{coverResult: adminArchitectureProjectCoverWriteResult{ProjectID: 17, ProjectVersion: 6}, projectExists: true}, want: errAdminArchitectureProjectWriteFailed},
		{name: "scan failure", row: &adminArchitectureProjectWriteRowStub{scanError: errors.New("unsafe dependency detail")}, want: errAdminArchitectureProjectWriteFailed},
		{name: "nil row", want: errAdminArchitectureProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminArchitectureProjectWriteQuery{row: test.row}
			writer := &postgresAdminArchitectureProjectWriter{queryRow: query.QueryRow}
			result, err := writer.UpsertCover(
				context.Background(),
				17,
				5,
				validAdminArchitectureProjectCoverWriteInput(t),
			)
			if err != test.want ||
				result != (adminArchitectureProjectCoverWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestArchitectureProjectCoverSQLRetainsAtomicBoundaries guards the one-cover
// upsert and optimistic project revision assumptions independent of a driver.
func TestArchitectureProjectCoverSQLRetainsAtomicBoundaries(t *testing.T) {
	for _, required := range []string{
		"WITH current_project AS MATERIALIZED",
		"WHERE id = $1 AND version = $2",
		"FROM updated_project",
		"ON CONFLICT (architecture_project_id) DO UPDATE",
		"architecture_project_cover_images.version + 1",
		"EXISTS(SELECT 1 FROM current_project)",
	} {
		if !strings.Contains(upsertAdminArchitectureProjectCoverSQL, required) {
			t.Errorf("cover SQL does not contain %q", required)
		}
	}
}
