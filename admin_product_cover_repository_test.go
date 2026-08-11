package main

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// validAdminProductCoverWriteInput derives one complete persistence value from
// a fully decoded deterministic PNG.
func validAdminProductCoverWriteInput(t *testing.T) adminProductCoverWriteInput {
	t.Helper()

	content := testProductCoverPNG(t)
	inspection, err := inspectProductCover(content, true)
	if err != nil {
		t.Fatalf("inspect cover writer fixture: %v", err)
	}

	return adminProductCoverWriteInput{
		ContentType: inspection.ContentType,
		Content:     append([]byte(nil), content...),
		ByteSize:    len(content),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "A synthetic chair study against a warm neutral field",
		Caption:     "Stage 21 repository fixture.",
	}
}

// TestPostgresAdminProductWriterUpsertsCover verifies exact SQL arguments and
// both database-owned revision values for a successful cover mutation.
func TestPostgresAdminProductWriterUpsertsCover(t *testing.T) {
	ctx := context.Background()
	input := validAdminProductCoverWriteInput(t)
	query := &recordingAdminProductWriteQuery{
		row: &adminProductWriteRowStub{
			coverResult: adminProductCoverWriteResult{
				ProductID:      17,
				ProductVersion: 6,
				CoverVersion:   2,
			},
			productExists: true,
		},
	}
	writer := &postgresAdminProductWriter{queryRow: query.QueryRow}

	result, err := writer.UpsertCover(ctx, 17, 5, input)
	if err != nil {
		t.Fatalf("upsert Product cover: %v", err)
	}
	if result != (adminProductCoverWriteResult{
		ProductID:      17,
		ProductVersion: 6,
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
		query.query != upsertAdminProductCoverSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("cover invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}
}

// TestPostgresAdminProductWriterCoverRejectsInvalidBoundary proves context,
// identity, revision, image facts, and reviewed text fail before SQL.
func TestPostgresAdminProductWriterCoverRejectsInvalidBoundary(t *testing.T) {
	valid := validAdminProductCoverWriteInput(t)
	tests := []struct {
		// name identifies the rejected input.
		name string
		// writer permits receiver-boundary cases.
		writer *postgresAdminProductWriter
		// ctx is nil only for the invalid-context case.
		ctx context.Context
		// id and version are optimistic Product coordinates.
		id      int64
		version int64
		// input contains one mutated cover fact.
		input adminProductCoverWriteInput
		// want is the stable expected category.
		want error
	}{
		{name: "nil context", writer: &postgresAdminProductWriter{}, id: 1, version: 1, input: valid, want: errAdminProductWriteInvalid},
		{name: "zero identity", writer: &postgresAdminProductWriter{}, ctx: context.Background(), version: 1, input: valid, want: errAdminProductWriteInvalid},
		{name: "zero revision", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, input: valid, want: errAdminProductWriteInvalid},
		{name: "maximum revision", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, version: math.MaxInt64, input: valid, want: errAdminProductWriteInvalid},
		{name: "wrong byte count", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminProductCoverWriteInput { value := valid; value.ByteSize++; return value }(), want: errAdminProductWriteInvalid},
		{name: "untrimmed alt", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, version: 1, input: func() adminProductCoverWriteInput { value := valid; value.AltText = " bad "; return value }(), want: errAdminProductWriteInvalid},
		{name: "nil receiver", ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminProductWriteFailed},
		{name: "missing query", writer: &postgresAdminProductWriter{}, ctx: context.Background(), id: 1, version: 1, input: valid, want: errAdminProductWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.writer.UpsertCover(
				test.ctx,
				test.id,
				test.version,
				test.input,
			)
			if !errors.Is(err, test.want) ||
				result != (adminProductCoverWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestPostgresAdminProductWriterCoverClassifiesOutcomes keeps missing and stale
// rows distinct while every driver/contract failure stays redacted.
func TestPostgresAdminProductWriterCoverClassifiesOutcomes(t *testing.T) {
	unsafeDetail := "password=unsafe-cover-database-detail"
	tests := []struct {
		// name identifies the database result shape.
		name string
		// row supplies that shape.
		row adminProductWriteRowScanner
		// want is the stable expected category.
		want error
	}{
		{name: "nil row", want: errAdminProductWriteFailed},
		{name: "missing Product", row: &adminProductWriteRowStub{}, want: errAdminProductNotFound},
		{name: "stale Product", row: &adminProductWriteRowStub{productExists: true}, want: errAdminProductWriteConflict},
		{name: "unsafe driver failure", row: &adminProductWriteRowStub{scanError: errors.New(unsafeDetail)}, want: errAdminProductWriteFailed},
		{name: "wrong Product revision", row: &adminProductWriteRowStub{coverResult: adminProductCoverWriteResult{ProductID: 7, ProductVersion: 9, CoverVersion: 1}, productExists: true}, want: errAdminProductWriteFailed},
		{name: "wrong identity", row: &adminProductWriteRowStub{coverResult: adminProductCoverWriteResult{ProductID: 8, ProductVersion: 4, CoverVersion: 1}, productExists: true}, want: errAdminProductWriteFailed},
		{name: "zero cover revision", row: &adminProductWriteRowStub{coverResult: adminProductCoverWriteResult{ProductID: 7, ProductVersion: 4}, productExists: true}, want: errAdminProductWriteFailed},
		{name: "result without existence", row: &adminProductWriteRowStub{coverResult: adminProductCoverWriteResult{ProductID: 7, ProductVersion: 4, CoverVersion: 1}}, want: errAdminProductWriteFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminProductWriteQuery{row: test.row}
			writer := &postgresAdminProductWriter{queryRow: query.QueryRow}
			result, err := writer.UpsertCover(
				context.Background(),
				7,
				3,
				validAdminProductCoverWriteInput(t),
			)
			if !errors.Is(err, test.want) ||
				result != (adminProductCoverWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
			if err != test.want || strings.Contains(err.Error(), unsafeDetail) {
				t.Error("cover writer failure exposed dependency detail")
			}
		})
	}
}
