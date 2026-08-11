package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestPostgresProductCatalogueReaderFindsPublishedCover verifies exact public
// SQL parameters, context forwarding, complete mapping, and byte isolation.
func TestPostgresProductCatalogueReaderFindsPublishedCover(t *testing.T) {
	expected := validTestProductCoverAsset(t, 9, 4)
	query := &productCatalogueQueryRowStub{
		row: &productCoverRowStub{asset: expected},
	}
	reader := &postgresProductCatalogueReader{queryRow: query.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		productCatalogueContextKey{},
		"cover-context",
	)

	actual, err := reader.FindPublishedCover(ctx, "stage-21-chair", 4)
	if err != nil {
		t.Fatalf("find published cover: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("cover: got %#v, want %#v", actual, expected)
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != findPublishedProductCoverSQL ||
		!reflect.DeepEqual(
			query.arguments,
			[]any{"stage-21-chair", publishedProductStatus, int64(4)},
		) {
		t.Errorf("query invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}
	actual.Content[0] ^= 0xff
	if reflect.DeepEqual(actual.Content, expected.Content) {
		t.Error("public cover result shares mutable scanner bytes")
	}
}

// TestPostgresProductCatalogueReaderCoverFailures keeps hidden/missing media a
// 404 category and collapses every unsafe dependency detail to one sentinel.
func TestPostgresProductCatalogueReaderCoverFailures(t *testing.T) {
	unsafeDetail := "postgres://secret-cover-read"
	valid := validTestProductCoverAsset(t, 1, 1)
	wrongVersion := valid
	wrongVersion.Version = 2
	invalidAsset := cloneProductCoverAsset(valid)
	invalidAsset.AltText = ""

	tests := []struct {
		// name identifies the dependency outcome.
		name string
		// reader provides the configured production seam.
		reader *postgresProductCatalogueReader
		// want is the stable expected category.
		want error
	}{
		{name: "nil receiver", want: errProductCoverReadFailed},
		{name: "missing query", reader: &postgresProductCatalogueReader{}, want: errProductCoverReadFailed},
		{name: "nil row", reader: &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner { return nil }}, want: errProductCoverReadFailed},
		{name: "not found", reader: &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner {
			return &productCoverRowStub{scanError: sql.ErrNoRows}
		}}, want: errProductCoverNotFound},
		{name: "unsafe driver error", reader: &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner {
			return &productCoverRowStub{scanError: errors.New(unsafeDetail)}
		}}, want: errProductCoverReadFailed},
		{name: "wrong version", reader: &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner {
			return &productCoverRowStub{asset: wrongVersion}
		}}, want: errProductCoverReadFailed},
		{name: "invalid asset", reader: &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner {
			return &productCoverRowStub{asset: invalidAsset}
		}}, want: errProductCoverReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := test.reader.FindPublishedCover(
				context.Background(),
				"stage-21-chair",
				1,
			)
			if !errors.Is(err, test.want) || !reflect.DeepEqual(asset, productCoverAsset{}) {
				t.Fatalf("result=%#v err=%v, want %v", asset, err, test.want)
			}
			if err != test.want || strings.Contains(err.Error(), unsafeDetail) {
				t.Error("public cover failure exposed dependency detail")
			}
		})
	}

	reader := &postgresProductCatalogueReader{queryRow: func(context.Context, string, ...any) productCatalogueRowScanner {
		t.Fatal("invalid public cover input reached query")
		return nil
	}}
	for _, test := range []struct {
		// name identifies one rejected query boundary.
		name    string
		ctx     context.Context
		slug    string
		version int64
	}{
		{name: "nil context", slug: "stage-21-chair", version: 1},
		{name: "invalid slug", ctx: context.Background(), slug: "Bad Slug", version: 1},
		{name: "zero version", ctx: context.Background(), slug: "stage-21-chair"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := reader.FindPublishedCover(test.ctx, test.slug, test.version)
			if !errors.Is(err, errProductCatalogueInvalidQuery) {
				t.Errorf("error: got %v, want invalid query", err)
			}
		})
	}
}

// TestPostgresAdminProductReaderFindsProtectedCover verifies the private path
// uses only Product identity and cover revision without a publication filter.
func TestPostgresAdminProductReaderFindsProtectedCover(t *testing.T) {
	expected := validTestProductCoverAsset(t, 13, 2)
	query := &adminProductQueryRowStub{row: &productCoverRowStub{asset: expected}}
	reader := &postgresAdminProductReader{queryRow: query.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		adminProductContextKey{},
		"protected-cover-context",
	)

	actual, err := reader.FindCoverByProductID(ctx, 13, 2)
	if err != nil {
		t.Fatalf("find protected cover: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) || query.calls != 1 ||
		query.context != ctx || query.query != findAdminProductCoverSQL ||
		!reflect.DeepEqual(query.arguments, []any{int64(13), int64(2)}) {
		t.Errorf("protected result=%#v query=%q args=%#v", actual, query.query, query.arguments)
	}
}

// TestPostgresAdminProductReaderCoverFailures verifies protected not-found,
// invalid input, and redacted dependency behavior.
func TestPostgresAdminProductReaderCoverFailures(t *testing.T) {
	unsafeDetail := "private-cover-database-password"
	tests := []struct {
		// name identifies the protected dependency outcome.
		name string
		// reader provides the configured seam.
		reader *postgresAdminProductReader
		// want is the stable expected category.
		want error
	}{
		{name: "nil receiver", want: errProductCoverReadFailed},
		{name: "missing query", reader: &postgresAdminProductReader{}, want: errProductCoverReadFailed},
		{name: "nil row", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner { return nil }}, want: errProductCoverReadFailed},
		{name: "not found", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			return &productCoverRowStub{scanError: sql.ErrNoRows}
		}}, want: errProductCoverNotFound},
		{name: "unsafe driver error", reader: &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
			return &productCoverRowStub{scanError: errors.New(unsafeDetail)}
		}}, want: errProductCoverReadFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := test.reader.FindCoverByProductID(
				context.Background(),
				1,
				1,
			)
			if !errors.Is(err, test.want) || !reflect.DeepEqual(asset, productCoverAsset{}) {
				t.Fatalf("result=%#v err=%v, want %v", asset, err, test.want)
			}
			if err != test.want || strings.Contains(err.Error(), unsafeDetail) {
				t.Error("protected cover failure exposed dependency detail")
			}
		})
	}

	reader := &postgresAdminProductReader{queryRow: func(context.Context, string, ...any) adminProductRowScanner {
		t.Fatal("invalid protected cover input reached query")
		return nil
	}}
	for _, test := range []struct {
		// name identifies one rejected query boundary.
		name    string
		ctx     context.Context
		id      int64
		version int64
	}{
		{name: "nil context", id: 1, version: 1},
		{name: "zero identity", ctx: context.Background(), version: 1},
		{name: "zero version", ctx: context.Background(), id: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := reader.FindCoverByProductID(test.ctx, test.id, test.version)
			if !errors.Is(err, errAdminProductInvalidQuery) {
				t.Errorf("error: got %v, want invalid query", err)
			}
		})
	}
}
