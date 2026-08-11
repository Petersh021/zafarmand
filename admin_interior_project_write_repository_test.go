package main

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// recordingAdminInteriorProjectWriteQuery captures one mutation invocation and
// returns a controlled scanner without requiring PostgreSQL.
type recordingAdminInteriorProjectWriteQuery struct {
	// calls records the number of attempted writes.
	calls int
	// context records the exact caller context.
	context context.Context
	// query records the fixed SQL statement.
	query string
	// arguments records every bound value in order.
	arguments []any
	// row is returned to the repository.
	row adminInteriorProjectWriteRowScanner
}

// QueryRow implements the writer's narrow database seam.
func (query *recordingAdminInteriorProjectWriteQuery) QueryRow(
	ctx context.Context,
	statement string,
	arguments ...any,
) adminInteriorProjectWriteRowScanner {
	query.calls++
	query.context = ctx
	query.query = statement
	query.arguments = append([]any(nil), arguments...)

	return query.row
}

// adminInteriorProjectWriteRowStub supplies create, update, or cover-upsert
// result columns and can simulate a scan failure.
type adminInteriorProjectWriteRowStub struct {
	// result supplies identity and version for text mutations.
	result adminInteriorProjectWriteResult
	// projectExists is the final UPDATE/cover existence column.
	projectExists bool
	// coverResult supplies project and cover revisions for media mutations.
	coverResult adminInteriorProjectCoverWriteResult
	// scanError is returned before any destination changes.
	scanError error
}

// Scan copies the configured two-, three-, or four-column result shape.
func (row *adminInteriorProjectWriteRowStub) Scan(
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
			return errors.New("interior create scan received unexpected destinations")
		}
		*id = row.result.ID
		*version = row.result.Version
	case 3:
		id, idOK := destinations[0].(*int64)
		version, versionOK := destinations[1].(*int64)
		exists, existsOK := destinations[2].(*bool)
		if !idOK || !versionOK || !existsOK {
			return errors.New("interior update scan received unexpected destinations")
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
			return errors.New("interior cover scan received unexpected destinations")
		}
		*projectID = row.coverResult.ProjectID
		*projectVersion = row.coverResult.ProjectVersion
		*coverVersion = row.coverResult.CoverVersion
		*exists = row.projectExists
	default:
		return errors.New("interior write scan received unexpected column count")
	}

	return nil
}

// validAdminInteriorProjectWriteInput returns one deterministic value satisfying
// every editable migration-7 constraint.
func validAdminInteriorProjectWriteInput() adminInteriorProjectWriteInput {
	return adminInteriorProjectWriteInput{
		Slug:              "atrium-residence",
		Title:             "Atrium Residence",
		Typology:          "Residential",
		Location:          "Tehran",
		ProjectYear:       2025,
		ProjectStatus:     "Completed",
		Description:       "A fictional reviewed Interior project.",
		SortOrder:         4,
		PublicationStatus: draftInteriorProjectStatus,
	}
}

// TestNewPostgresAdminInteriorProjectWriter verifies constructor dependency
// validation without performing a write.
func TestNewPostgresAdminInteriorProjectWriter(t *testing.T) {
	writer, err := newPostgresAdminInteriorProjectWriter(nil)
	if !errors.Is(err, errAdminInteriorProjectWriterDatabaseRequired) ||
		writer != nil {
		t.Fatalf("nil database result: writer=%#v err=%v", writer, err)
	}

	writer, err = newPostgresAdminInteriorProjectWriter(&sql.DB{})
	if err != nil {
		t.Fatalf("construct writer: %v", err)
	}
	if writer == nil || writer.queryRow == nil {
		t.Fatal("constructed writer did not install query adapter")
	}
}

// TestPostgresAdminInteriorProjectWriterCreate verifies exact SQL parameter
// binding and the returned database-owned identity/revision pair.
func TestPostgresAdminInteriorProjectWriterCreate(t *testing.T) {
	ctx := context.Background()
	input := validAdminInteriorProjectWriteInput()
	query := &recordingAdminInteriorProjectWriteQuery{
		row: &adminInteriorProjectWriteRowStub{
			result: adminInteriorProjectWriteResult{ID: 41, Version: 1},
		},
	}
	writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}

	result, err := writer.Create(ctx, input)
	if err != nil {
		t.Fatalf("create Interior project: %v", err)
	}
	if result != (adminInteriorProjectWriteResult{ID: 41, Version: 1}) {
		t.Errorf("create result: got %#v", result)
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
		query.query != createAdminInteriorProjectSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf(
			"create invocation: calls=%d query=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
}

// TestPostgresAdminInteriorProjectWriterCreateMapsMissingYearToNull proves the
// Go zero-domain value becomes SQL NULL rather than an invented year.
func TestPostgresAdminInteriorProjectWriterCreateMapsMissingYearToNull(t *testing.T) {
	input := validAdminInteriorProjectWriteInput()
	input.ProjectYear = 0
	query := &recordingAdminInteriorProjectWriteQuery{
		row: &adminInteriorProjectWriteRowStub{
			result: adminInteriorProjectWriteResult{ID: 5, Version: 1},
		},
	}
	writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}

	if _, err := writer.Create(context.Background(), input); err != nil {
		t.Fatalf("create without year: %v", err)
	}
	if query.arguments[4] != nil {
		t.Errorf("missing year argument: got %#v, want nil", query.arguments[4])
	}
}

// TestPostgresAdminInteriorProjectWriterCreateRejectsInvalidInput proves every
// editable field and nil context fail before the database seam.
func TestPostgresAdminInteriorProjectWriterCreateRejectsInvalidInput(t *testing.T) {
	valid := validAdminInteriorProjectWriteInput()
	tests := []struct {
		// name identifies the rejected boundary.
		name string
		// ctx is nil only for the missing-context case.
		ctx context.Context
		// input contains one isolated invalid field.
		input adminInteriorProjectWriteInput
	}{
		{name: "nil context", input: valid},
		{name: "bad slug", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.Slug = "Bad Slug"; return value }()},
		{name: "empty title", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.Title = ""; return value }()},
		{name: "long title", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.Title = strings.Repeat("t", interiorProjectTitleMaximumLength+1)
			return value
		}()},
		{name: "untrimmed typology", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.Typology = " Residential "; return value }()},
		{name: "long typology", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.Typology = strings.Repeat("t", interiorProjectTypologyMaximumLength+1)
			return value
		}()},
		{name: "untrimmed location", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.Location = " Tehran "; return value }()},
		{name: "long location", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.Location = strings.Repeat("l", interiorProjectLocationMaximumLength+1)
			return value
		}()},
		{name: "year below range", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.ProjectYear = 999; return value }()},
		{name: "year above range", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.ProjectYear = 10000; return value }()},
		{name: "empty project status", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.ProjectStatus = ""; return value }()},
		{name: "long project status", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.ProjectStatus = strings.Repeat("s", interiorProjectStatusMaximumLength+1)
			return value
		}()},
		{name: "untrimmed description", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.Description = " copy "; return value }()},
		{name: "description contains nul", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.Description = "copy\x00tail"
			return value
		}()},
		{name: "long description", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.Description = strings.Repeat("d", interiorProjectDescriptionMaximumLength+1)
			return value
		}()},
		{name: "zero sort order", ctx: context.Background(), input: func() adminInteriorProjectWriteInput { value := valid; value.SortOrder = 0; return value }()},
		{name: "large sort order", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.SortOrder = math.MaxInt32 + 1
			return value
		}()},
		{name: "unsupported publication", ctx: context.Background(), input: func() adminInteriorProjectWriteInput {
			value := valid
			value.PublicationStatus = "deleted"
			return value
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminInteriorProjectWriteQuery{}
			writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Create(test.ctx, test.input)
			if err != errAdminInteriorProjectWriteInvalid ||
				result != (adminInteriorProjectWriteResult{}) ||
				query.calls != 0 {
				t.Fatalf("result=%#v err=%v calls=%d", result, err, query.calls)
			}
		})
	}
}

// TestPostgresAdminInteriorProjectWriterCreateClassifiesFailures verifies the
// one safe slug conflict and redaction of every other database failure.
func TestPostgresAdminInteriorProjectWriterCreateClassifiesFailures(t *testing.T) {
	unsafeDetail := "password=unsafe-interior-detail"
	tests := []struct {
		// name identifies the result shape.
		name string
		// row supplies that result shape.
		row adminInteriorProjectWriteRowScanner
		// want is the stable expected category.
		want error
	}{
		{name: "nil row", want: errAdminInteriorProjectWriteFailed},
		{name: "slug conflict", row: &adminInteriorProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "interior_projects_slug_unique"}}, want: errAdminInteriorProjectSlugConflict},
		{name: "other unique", row: &adminInteriorProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "future_unique"}}, want: errAdminInteriorProjectWriteFailed},
		{name: "driver detail", row: &adminInteriorProjectWriteRowStub{scanError: errors.New(unsafeDetail)}, want: errAdminInteriorProjectWriteFailed},
		{name: "zero result", row: &adminInteriorProjectWriteRowStub{}, want: errAdminInteriorProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminInteriorProjectWriteQuery{row: test.row}
			writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Create(
				context.Background(),
				validAdminInteriorProjectWriteInput(),
			)
			if err != test.want || result != (adminInteriorProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
			if strings.Contains(err.Error(), unsafeDetail) {
				t.Error("writer error exposed dependency detail")
			}
		})
	}
}

// TestPostgresAdminInteriorProjectWriterUpdate verifies exact optimistic-lock
// SQL arguments and the required one-step revision increment.
func TestPostgresAdminInteriorProjectWriterUpdate(t *testing.T) {
	ctx := context.Background()
	input := validAdminInteriorProjectWriteInput()
	query := &recordingAdminInteriorProjectWriteQuery{
		row: &adminInteriorProjectWriteRowStub{
			result:        adminInteriorProjectWriteResult{ID: 17, Version: 6},
			projectExists: true,
		},
	}
	writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}

	result, err := writer.Update(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("update Interior project: %v", err)
	}
	if result != (adminInteriorProjectWriteResult{ID: 17, Version: 6}) {
		t.Errorf("update result: got %#v", result)
	}
	wantArguments := []any{
		int64(17),
		int64(5),
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
		query.query != updateAdminInteriorProjectSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf(
			"update invocation: calls=%d query=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
}

// TestPostgresAdminInteriorProjectWriterUpdateClassifiesOutcomes keeps missing,
// stale, slug-collision, and operational outcomes distinct and safe.
func TestPostgresAdminInteriorProjectWriterUpdateClassifiesOutcomes(t *testing.T) {
	tests := []struct {
		// name identifies the database result.
		name string
		// row supplies that result.
		row adminInteriorProjectWriteRowScanner
		// want is the stable expected category.
		want error
	}{
		{name: "missing", row: &adminInteriorProjectWriteRowStub{}, want: errAdminInteriorProjectNotFound},
		{name: "stale", row: &adminInteriorProjectWriteRowStub{projectExists: true}, want: errAdminInteriorProjectWriteConflict},
		{name: "slug conflict", row: &adminInteriorProjectWriteRowStub{scanError: &pgconn.PgError{Code: "23505", ConstraintName: "interior_projects_slug_unique"}}, want: errAdminInteriorProjectSlugConflict},
		{name: "wrong identity", row: &adminInteriorProjectWriteRowStub{result: adminInteriorProjectWriteResult{ID: 18, Version: 6}, projectExists: true}, want: errAdminInteriorProjectWriteFailed},
		{name: "wrong revision", row: &adminInteriorProjectWriteRowStub{result: adminInteriorProjectWriteResult{ID: 17, Version: 7}, projectExists: true}, want: errAdminInteriorProjectWriteFailed},
		{name: "result without existence", row: &adminInteriorProjectWriteRowStub{result: adminInteriorProjectWriteResult{ID: 17, Version: 6}}, want: errAdminInteriorProjectWriteFailed},
		{name: "nil row", want: errAdminInteriorProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminInteriorProjectWriteQuery{row: test.row}
			writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}
			result, err := writer.Update(
				context.Background(),
				17,
				5,
				validAdminInteriorProjectWriteInput(),
			)
			if err != test.want || result != (adminInteriorProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestPostgresAdminInteriorProjectWriterUpdateRejectsCoordinates proves invalid
// IDs, revisions, inputs, contexts, and receivers fail without a write.
func TestPostgresAdminInteriorProjectWriterUpdateRejectsCoordinates(t *testing.T) {
	valid := validAdminInteriorProjectWriteInput()
	tests := []struct {
		// name identifies the rejected coordinate.
		name string
		// writer is nil only for the receiver case.
		writer *postgresAdminInteriorProjectWriter
		// ctx is nil only for the context case.
		ctx context.Context
		// id and version identify the expected row revision.
		id      int64
		version int64
		// input is otherwise valid unless explicitly changed.
		input adminInteriorProjectWriteInput
		// want is the stable expected category.
		want error
	}{
		{name: "nil context", writer: &postgresAdminInteriorProjectWriter{}, id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "zero identity", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), version: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "zero revision", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "maximum revision", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: math.MaxInt64, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "invalid input", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectWriteInput { value := valid; value.Title = ""; return value }(), want: errAdminInteriorProjectWriteInvalid},
		{name: "nil receiver", ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteFailed},
		{name: "missing query", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.writer.Update(
				test.ctx,
				test.id,
				test.version,
				test.input,
			)
			if err != test.want || result != (adminInteriorProjectWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestInteriorProjectWriteSQLRetainsConcurrencyBoundaries guards the structural
// CTE and named-table assumptions independently from driver behavior.
func TestInteriorProjectWriteSQLRetainsConcurrencyBoundaries(t *testing.T) {
	for _, required := range []string{
		"WITH current_project AS MATERIALIZED",
		"WHERE id = $1 AND version = $2",
		"version = version + 1",
		"EXISTS(SELECT 1 FROM current_project)",
	} {
		if !strings.Contains(updateAdminInteriorProjectSQL, required) {
			t.Errorf("update SQL does not contain %q", required)
		}
	}
}
