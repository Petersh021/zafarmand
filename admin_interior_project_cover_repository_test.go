package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// validAdminInteriorProjectCoverWriteInput derives a complete persistence value
// from a fully decoded deterministic PNG fixture.
func validAdminInteriorProjectCoverWriteInput(
	t *testing.T,
) adminInteriorProjectCoverWriteInput {
	t.Helper()

	content := testAdminInteriorProjectCoverPNG(t)
	inspection, err := inspectReviewedCover(content, true)
	if err != nil {
		t.Fatalf("inspect Interior cover fixture: %v", err)
	}

	return adminInteriorProjectCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     append([]byte(nil), content...),
		ByteSize:    len(content),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "A fictional Interior project with warm natural light",
		Caption:     "Stage 22 repository fixture.",
	}
}

// testAdminInteriorProjectCoverPNG encodes a tiny deterministic fictional image
// without depending on Product-specific test fixtures.
func testAdminInteriorProjectCoverPNG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 3, 2))
	canvas.Set(0, 0, color.RGBA{R: 86, G: 66, B: 48, A: 255})
	canvas.Set(1, 0, color.RGBA{R: 196, G: 180, B: 152, A: 255})
	canvas.Set(2, 0, color.RGBA{R: 45, G: 42, B: 36, A: 255})
	canvas.Set(0, 1, color.RGBA{R: 174, G: 148, B: 111, A: 255})
	canvas.Set(1, 1, color.RGBA{R: 28, G: 27, B: 24, A: 255})
	canvas.Set(2, 1, color.RGBA{R: 230, G: 222, B: 207, A: 255})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode deterministic Interior PNG: %v", err)
	}

	return append([]byte(nil), encoded.Bytes()...)
}

// testAdminInteriorProjectCoverJPEG encodes a different deterministic image
// used to prove a real format-changing cover replacement.
func testAdminInteriorProjectCoverJPEG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.Set(
				x,
				y,
				color.RGBA{
					R: uint8(40 + x*35),
					G: uint8(32 + y*45),
					B: uint8(24 + (x+y)*20),
					A: 255,
				},
			)
		}
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode deterministic Interior JPEG: %v", err)
	}

	return append([]byte(nil), encoded.Bytes()...)
}

// TestPostgresAdminInteriorProjectWriterUpsertsCover verifies exact SQL
// arguments and both database-owned revision values on success.
func TestPostgresAdminInteriorProjectWriterUpsertsCover(t *testing.T) {
	ctx := context.Background()
	input := validAdminInteriorProjectCoverWriteInput(t)
	query := &recordingAdminInteriorProjectWriteQuery{
		row: &adminInteriorProjectWriteRowStub{
			coverResult: adminInteriorProjectCoverWriteResult{
				ProjectID:      17,
				ProjectVersion: 6,
				CoverVersion:   2,
			},
			projectExists: true,
		},
	}
	writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}

	result, err := writer.UpsertCover(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("upsert Interior cover: %v", err)
	}
	if result != (adminInteriorProjectCoverWriteResult{
		ProjectID:      17,
		ProjectVersion: 6,
		CoverVersion:   2,
	}) {
		t.Errorf("cover result: got %#v", result)
	}
	wantArguments := []any{
		int64(17),
		int64(5),
		input.ContentType,
		input.Content,
		input.ByteSize,
		input.Width,
		input.Height,
		input.SHA256[:],
		input.AltText,
		input.Caption,
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != upsertAdminInteriorProjectCoverSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf(
			"cover invocation: calls=%d query=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
}

// TestPostgresAdminInteriorProjectWriterCoverRejectsInvalidBoundary proves
// context, identity, revision, image facts, and reviewed text fail before SQL.
func TestPostgresAdminInteriorProjectWriterCoverRejectsInvalidBoundary(t *testing.T) {
	valid := validAdminInteriorProjectCoverWriteInput(t)
	tests := []struct {
		// name identifies the rejected boundary.
		name string
		// writer is nil only for the receiver case.
		writer *postgresAdminInteriorProjectWriter
		// ctx is nil only for the missing-context case.
		ctx context.Context
		// id and version identify the optimistic project coordinate.
		id      int64
		version int64
		// input contains one isolated invalid image fact.
		input adminInteriorProjectCoverWriteInput
		// want is the stable expected category.
		want error
	}{
		{name: "nil context", writer: &postgresAdminInteriorProjectWriter{}, id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "zero identity", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), version: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "zero revision", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "maximum revision", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: math.MaxInt64, input: valid, want: errAdminInteriorProjectWriteInvalid},
		{name: "wrong byte count", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput { value := valid; value.ByteSize++; return value }(), want: errAdminInteriorProjectWriteInvalid},
		{name: "wrong type", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput {
			value := valid
			value.ContentType = "image/gif"
			return value
		}(), want: errAdminInteriorProjectWriteInvalid},
		{name: "wrong dimensions", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput { value := valid; value.Width++; return value }(), want: errAdminInteriorProjectWriteInvalid},
		{name: "wrong digest", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput { value := valid; value.SHA256[0] ^= 0xff; return value }(), want: errAdminInteriorProjectWriteInvalid},
		{name: "untrimmed alt", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput { value := valid; value.AltText = " bad "; return value }(), want: errAdminInteriorProjectWriteInvalid},
		{name: "multiline caption", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminInteriorProjectCoverWriteInput {
			value := valid
			value.Caption = "line one\nline two"
			return value
		}(), want: errAdminInteriorProjectWriteInvalid},
		{name: "nil receiver", ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteFailed},
		{name: "missing query", writer: &postgresAdminInteriorProjectWriter{}, ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminInteriorProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.writer.UpsertCover(
				test.ctx,
				test.id,
				test.version,
				test.input,
			)
			if err != test.want ||
				result != (adminInteriorProjectCoverWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestPostgresAdminInteriorProjectWriterCoverClassifiesOutcomes keeps missing
// and stale projects distinct while redacting all dependency failures.
func TestPostgresAdminInteriorProjectWriterCoverClassifiesOutcomes(t *testing.T) {
	unsafeDetail := "password=unsafe-interior-cover-detail"
	tests := []struct {
		// name identifies the database result shape.
		name string
		// row supplies that result.
		row adminInteriorProjectWriteRowScanner
		// want is the stable expected category.
		want error
	}{
		{name: "nil row", want: errAdminInteriorProjectWriteFailed},
		{name: "missing project", row: &adminInteriorProjectWriteRowStub{}, want: errAdminInteriorProjectNotFound},
		{name: "stale project", row: &adminInteriorProjectWriteRowStub{projectExists: true}, want: errAdminInteriorProjectWriteConflict},
		{name: "driver failure", row: &adminInteriorProjectWriteRowStub{scanError: errors.New(unsafeDetail)}, want: errAdminInteriorProjectWriteFailed},
		{name: "wrong project revision", row: &adminInteriorProjectWriteRowStub{coverResult: adminInteriorProjectCoverWriteResult{ProjectID: 7, ProjectVersion: 9, CoverVersion: 1}, projectExists: true}, want: errAdminInteriorProjectWriteFailed},
		{name: "wrong identity", row: &adminInteriorProjectWriteRowStub{coverResult: adminInteriorProjectCoverWriteResult{ProjectID: 8, ProjectVersion: 4, CoverVersion: 1}, projectExists: true}, want: errAdminInteriorProjectWriteFailed},
		{name: "zero cover revision", row: &adminInteriorProjectWriteRowStub{coverResult: adminInteriorProjectCoverWriteResult{ProjectID: 7, ProjectVersion: 4}, projectExists: true}, want: errAdminInteriorProjectWriteFailed},
		{name: "result without existence", row: &adminInteriorProjectWriteRowStub{coverResult: adminInteriorProjectCoverWriteResult{ProjectID: 7, ProjectVersion: 4, CoverVersion: 1}}, want: errAdminInteriorProjectWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminInteriorProjectWriteQuery{row: test.row}
			writer := &postgresAdminInteriorProjectWriter{queryRow: query.QueryRow}
			result, err := writer.UpsertCover(
				context.Background(),
				7,
				3,
				validAdminInteriorProjectCoverWriteInput(t),
			)
			if err != test.want ||
				result != (adminInteriorProjectCoverWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
			if strings.Contains(err.Error(), unsafeDetail) {
				t.Error("cover writer error exposed dependency detail")
			}
		})
	}
}

// TestInteriorProjectCoverUpsertSQLRetainsAtomicBoundary protects the fixed CTE,
// conflict target, and revision increments independently from a database driver.
func TestInteriorProjectCoverUpsertSQLRetainsAtomicBoundary(t *testing.T) {
	for _, required := range []string{
		"WITH current_project AS MATERIALIZED",
		"WHERE id = $1 AND version = $2",
		"version = version + 1",
		"INSERT INTO public.interior_project_cover_images",
		"ON CONFLICT (interior_project_id) DO UPDATE",
		"version = interior_project_cover_images.version + 1",
		"EXISTS(SELECT 1 FROM current_project)",
	} {
		if !strings.Contains(upsertAdminInteriorProjectCoverSQL, required) {
			t.Errorf("cover upsert SQL does not contain %q", required)
		}
	}
}

// adminInteriorProjectCoverAssetRowStub supplies one exact twelve-column media
// projection or a configured scan failure to the protected reader.
type adminInteriorProjectCoverAssetRowStub struct {
	// asset supplies every successful media value.
	asset interiorProjectCoverAsset
	// scanError simulates not-found or operational failure.
	scanError error
}

// Scan copies the exact binary cover projection expected by the shared scanner.
func (row *adminInteriorProjectCoverAssetRowStub) Scan(
	destinations ...any,
) error {
	if row.scanError != nil {
		return row.scanError
	}
	if len(destinations) != 12 {
		return errors.New("Interior cover asset scan expected twelve destinations")
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
		return errors.New("Interior cover asset scan received unexpected destinations")
	}

	*projectID = row.asset.InteriorProjectID
	*version = row.asset.Version
	*contentType = row.asset.ContentType
	*content = append([]byte(nil), row.asset.Content...)
	*byteSize = row.asset.ByteSize
	*width = row.asset.Width
	*height = row.asset.Height
	*digest = append([]byte(nil), row.asset.SHA256[:]...)
	*altText = row.asset.AltText
	*caption = row.asset.Caption
	*createdAt = row.asset.CreatedAt
	*updatedAt = row.asset.UpdatedAt

	return nil
}

// validInteriorProjectCoverAsset returns one complete exact-revision fixture.
func validInteriorProjectCoverAsset(
	t *testing.T,
	projectID int64,
	version int64,
) interiorProjectCoverAsset {
	t.Helper()

	input := validAdminInteriorProjectCoverWriteInput(t)
	createdAt := time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC)

	return interiorProjectCoverAsset{
		InteriorProjectID: projectID,
		Version:           version,
		ContentType:       input.ContentType,
		Content:           append([]byte(nil), input.Content...),
		ByteSize:          input.ByteSize,
		Width:             input.Width,
		Height:            input.Height,
		SHA256:            input.SHA256,
		AltText:           input.AltText,
		Caption:           input.Caption,
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt.Add(time.Minute),
	}
}

// TestPostgresAdminInteriorProjectReaderFindCover verifies exact protected
// project/revision binding and complete validated media projection.
func TestPostgresAdminInteriorProjectReaderFindCover(t *testing.T) {
	want := validInteriorProjectCoverAsset(t, 17, 3)
	query := &adminInteriorProjectQueryRowStub{
		row: &adminInteriorProjectCoverAssetRowStub{asset: want},
	}
	reader := &postgresAdminInteriorProjectReader{queryRow: query.QueryRow}
	ctx := context.Background()

	asset, err := reader.FindCoverByProjectID(ctx, 17, 3)
	if err != nil {
		t.Fatalf("find protected Interior cover: %v", err)
	}
	if !reflect.DeepEqual(asset, want) {
		t.Errorf("asset: got %#v, want %#v", asset, want)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findAdminInteriorProjectCoverSQL ||
		!reflect.DeepEqual(query.arguments, []any{int64(17), int64(3)}) {
		t.Errorf(
			"cover read invocation: calls=%d query=%q args=%#v",
			query.calls,
			query.query,
			query.arguments,
		)
	}
}

// TestPostgresAdminInteriorProjectReaderFindCoverClassifiesFailures keeps stale
// media paths as 404-ready errors and redacts malformed/dependency outcomes.
func TestPostgresAdminInteriorProjectReaderFindCoverClassifiesFailures(t *testing.T) {
	valid := validInteriorProjectCoverAsset(t, 17, 3)
	tests := []struct {
		// name identifies the scanner result.
		name string
		// row supplies that result.
		row adminInteriorProjectRowScanner
		// want is the stable expected category.
		want error
	}{
		{name: "nil row", want: errInteriorProjectCoverReadFailed},
		{name: "not found", row: &adminInteriorProjectCoverAssetRowStub{scanError: sql.ErrNoRows}, want: errInteriorProjectCoverNotFound},
		{name: "driver failure", row: &adminInteriorProjectCoverAssetRowStub{scanError: errors.New("unsafe cover detail")}, want: errInteriorProjectCoverReadFailed},
		{name: "wrong project", row: &adminInteriorProjectCoverAssetRowStub{asset: func() interiorProjectCoverAsset { value := valid; value.InteriorProjectID = 18; return value }()}, want: errInteriorProjectCoverReadFailed},
		{name: "wrong version", row: &adminInteriorProjectCoverAssetRowStub{asset: func() interiorProjectCoverAsset { value := valid; value.Version = 4; return value }()}, want: errInteriorProjectCoverReadFailed},
		{name: "invalid bytes", row: &adminInteriorProjectCoverAssetRowStub{asset: func() interiorProjectCoverAsset {
			value := valid
			value.Content = []byte("not an image")
			value.ByteSize = len(value.Content)
			return value
		}()}, want: errInteriorProjectCoverReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &adminInteriorProjectQueryRowStub{row: test.row}
			reader := &postgresAdminInteriorProjectReader{queryRow: query.QueryRow}
			asset, err := reader.FindCoverByProjectID(
				context.Background(),
				17,
				3,
			)
			if err != test.want ||
				!reflect.DeepEqual(asset, interiorProjectCoverAsset{}) {
				t.Fatalf("asset=%#v err=%v, want %v", asset, err, test.want)
			}
		})
	}
}

// TestPostgresAdminInteriorProjectReaderFindCoverRejectsCoordinates proves nil
// context and non-positive coordinates fail before the query seam.
func TestPostgresAdminInteriorProjectReaderFindCoverRejectsCoordinates(t *testing.T) {
	query := &adminInteriorProjectQueryRowStub{}
	reader := &postgresAdminInteriorProjectReader{queryRow: query.QueryRow}
	tests := []struct {
		// name identifies the rejected coordinate.
		name string
		// ctx is nil only for the missing-context case.
		ctx context.Context
		// projectID and version identify the requested media.
		projectID int64
		version   int64
	}{
		{name: "nil context", projectID: 1, version: 1},
		{name: "zero project", ctx: context.Background(), version: 1},
		{name: "zero version", ctx: context.Background(), projectID: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := reader.FindCoverByProjectID(
				test.ctx,
				test.projectID,
				test.version,
			)
			if err != errAdminInteriorProjectInvalidQuery ||
				!reflect.DeepEqual(asset, interiorProjectCoverAsset{}) {
				t.Fatalf("asset=%#v err=%v", asset, err)
			}
		})
	}
	if query.calls != 0 {
		t.Errorf("invalid cover requests reached query seam %d times", query.calls)
	}

	var nilReader *postgresAdminInteriorProjectReader
	asset, err := nilReader.FindCoverByProjectID(
		context.Background(),
		1,
		1,
	)
	if err != errInteriorProjectCoverReadFailed ||
		!reflect.DeepEqual(asset, interiorProjectCoverAsset{}) {
		t.Errorf("nil reader cover result: asset=%#v err=%v", asset, err)
	}
}
