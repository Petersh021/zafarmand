package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// recordingAdminSiteContentWriteQuery captures one mutation and returns a
// controlled scanner without requiring PostgreSQL.
type recordingAdminSiteContentWriteQuery struct {
	// calls, context, query, and arguments record the complete invocation.
	calls     int
	context   context.Context
	query     string
	arguments []any
	// row is returned to the writer.
	row adminSiteContentWriteRowScanner
}

// QueryRow implements the writer's narrow database seam.
func (query *recordingAdminSiteContentWriteQuery) QueryRow(
	ctx context.Context,
	statement string,
	arguments ...any,
) adminSiteContentWriteRowScanner {
	query.calls++
	query.context = ctx
	query.query = statement
	query.arguments = append([]any(nil), arguments...)

	return query.row
}

// adminSiteContentWriteRowStub supplies Homepage, Contact, or hero mutation
// result columns and can simulate a scan failure.
type adminSiteContentWriteRowStub struct {
	// homepageVersion and its seven classification facts supply Homepage updates.
	homepageVersion       int64
	singletonExists       bool
	versionMatches        bool
	interiorAvailable     bool
	architectureAvailable bool
	productAvailable      bool
	heroAvailable         bool
	// contactVersion supplies Contact update results.
	contactVersion int64
	// heroResult supplies parent and exact-media revisions.
	heroResult adminHomepageHeroWriteResult
	// scanError is returned before any destination changes.
	scanError error
}

// Scan copies the configured two-, three-, or seven-column statement result.
func (row *adminSiteContentWriteRowStub) Scan(destinations ...any) error {
	if row.scanError != nil {
		return row.scanError
	}
	switch len(destinations) {
	case 2:
		*destinations[0].(*int64) = row.contactVersion
		*destinations[1].(*bool) = row.singletonExists
	case 3:
		*destinations[0].(*int64) = row.heroResult.HomepageVersion
		*destinations[1].(*int64) = row.heroResult.HeroVersion
		*destinations[2].(*bool) = row.singletonExists
	case 7:
		*destinations[0].(*int64) = row.homepageVersion
		*destinations[1].(*bool) = row.singletonExists
		*destinations[2].(*bool) = row.versionMatches
		*destinations[3].(*bool) = row.interiorAvailable
		*destinations[4].(*bool) = row.architectureAvailable
		*destinations[5].(*bool) = row.productAvailable
		*destinations[6].(*bool) = row.heroAvailable
	default:
		return errors.New("admin site content write received unexpected destinations")
	}

	return nil
}

// validAdminHomepageContentWriteInput returns one deterministic value satisfying
// all administrator-owned Homepage constraints.
func validAdminHomepageContentWriteInput() adminHomepageContentWriteInput {
	return adminHomepageContentWriteInput{
		StudioName:                    "Zafarmand",
		Descriptor:                    "Design Studio",
		ManagedHeroEnabled:            true,
		FeaturedInteriorProjectID:     11,
		FeaturedArchitectureProjectID: 12,
		FeaturedProductID:             13,
		SEOTitle:                      "Home | Zafarmand",
		SEODescription:                "Selected work by Zafarmand design studio",
	}
}

// validAdminContactContentWriteInput returns one deterministic complete Contact
// mutation satisfying migration 9.
func validAdminContactContentWriteInput() adminContactContentWriteInput {
	return adminContactContentWriteInput{
		Eyebrow:        "Contact",
		Heading:        "Begin a conversation",
		Introduction:   "Share the project context Zafarmand should review.",
		ContactEmail:   "studio@example.com",
		PhoneDisplay:   "+98 21 5555 0101",
		PhoneE164:      "+982155550101",
		Address:        "Studio 4\nTehran",
		SEOTitle:       "Contact | Zafarmand",
		SEODescription: "Contact Zafarmand design studio",
	}
}

// validAdminHomepageHeroWriteInput derives stored facts from deterministic
// normalized pixels rather than trusting test constants.
func validAdminHomepageHeroWriteInput(t *testing.T) adminHomepageHeroWriteInput {
	t.Helper()
	content, inspection, err := normalizeReviewedCover(
		testAdminHomepageHeroPNG(t),
	)
	if err != nil {
		t.Fatalf("normalize deterministic Homepage hero: %v", err)
	}

	return adminHomepageHeroWriteInput{
		ContentType: inspection.ContentType,
		Content:     content,
		ByteSize:    len(content),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "A fictional managed Homepage hero",
	}
}

// TestNewPostgresAdminSiteContentWriter verifies nil dependency rejection and
// successful adapter construction without issuing SQL.
func TestNewPostgresAdminSiteContentWriter(t *testing.T) {
	writer, err := newPostgresAdminSiteContentWriter(nil)
	if !errors.Is(err, errAdminSiteContentWriterDatabaseRequired) || writer != nil {
		t.Fatalf("nil database: writer=%#v err=%v", writer, err)
	}
	writer, err = newPostgresAdminSiteContentWriter(&sql.DB{})
	if err != nil || writer == nil || writer.queryRow == nil {
		t.Fatalf("constructed writer: writer=%#v err=%v", writer, err)
	}
}

// TestPostgresAdminSiteContentWriterUpdateHomepage verifies exact nullable ID
// binding and the required one-step revision result.
func TestPostgresAdminSiteContentWriterUpdateHomepage(t *testing.T) {
	ctx := context.Background()
	input := validAdminHomepageContentWriteInput()
	query := &recordingAdminSiteContentWriteQuery{
		row: &adminSiteContentWriteRowStub{
			homepageVersion:       8,
			singletonExists:       true,
			versionMatches:        true,
			interiorAvailable:     true,
			architectureAvailable: true,
			productAvailable:      true,
			heroAvailable:         true,
		},
	}
	writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}

	result, err := writer.UpdateHomepage(ctx, 7, input)
	if err != nil || result != (adminSiteContentWriteResult{Version: 8}) {
		t.Fatalf("update Homepage: result=%#v err=%v", result, err)
	}
	wantArguments := []any{
		int64(7),
		input.StudioName,
		input.Descriptor,
		input.FeaturedInteriorProjectID,
		input.FeaturedArchitectureProjectID,
		input.FeaturedProductID,
		input.ManagedHeroEnabled,
		input.SEOTitle,
		input.SEODescription,
	}
	if query.calls != 1 || query.context != ctx ||
		query.query != updateAdminHomepageContentSQL ||
		!reflect.DeepEqual(query.arguments, wantArguments) {
		t.Errorf("Homepage invocation: calls=%d query=%q args=%#v", query.calls, query.query, query.arguments)
	}

	input.FeaturedInteriorProjectID = 0
	query.row = &adminSiteContentWriteRowStub{
		homepageVersion:       9,
		singletonExists:       true,
		versionMatches:        true,
		interiorAvailable:     true,
		architectureAvailable: true,
		productAvailable:      true,
		heroAvailable:         true,
	}
	if _, err := writer.UpdateHomepage(ctx, 8, input); err != nil {
		t.Fatalf("clear Interior slot: %v", err)
	}
	if query.arguments[3] != nil {
		t.Errorf("cleared Interior argument: got %#v, want nil", query.arguments[3])
	}
}

// TestPostgresAdminSiteContentWriterClassifiesHomepageOutcomes keeps stale,
// unavailable-slot, missing-hero, and operational outcomes distinct and safe.
func TestPostgresAdminSiteContentWriterClassifiesHomepageOutcomes(t *testing.T) {
	base := adminSiteContentWriteRowStub{
		singletonExists:       true,
		versionMatches:        true,
		interiorAvailable:     true,
		architectureAvailable: true,
		productAvailable:      true,
		heroAvailable:         true,
	}
	tests := []struct {
		// name identifies the atomic Homepage outcome.
		name string
		// row supplies the controlled SQL scanner result.
		row adminSiteContentWriteRowScanner
		// input is the validated update sent to the writer.
		input adminHomepageContentWriteInput
		// want is the safe repository-level classification.
		want error
	}{
		{name: "nil row", input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteFailed},
		{name: "missing singleton", row: &adminSiteContentWriteRowStub{interiorAvailable: true, architectureAvailable: true, productAvailable: true, heroAvailable: true}, input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteFailed},
		{name: "stale", row: func() *adminSiteContentWriteRowStub { value := base; value.versionMatches = false; return &value }(), input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteConflict},
		{name: "concurrent zero row", row: &base, input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteConflict},
		{name: "Interior unavailable", row: func() *adminSiteContentWriteRowStub { value := base; value.interiorAvailable = false; return &value }(), input: validAdminHomepageContentWriteInput(), want: errAdminHomepageInteriorFeatureUnavailable},
		{name: "Architecture unavailable", row: func() *adminSiteContentWriteRowStub {
			value := base
			value.architectureAvailable = false
			return &value
		}(), input: validAdminHomepageContentWriteInput(), want: errAdminHomepageArchitectureFeatureUnavailable},
		{name: "Product unavailable", row: func() *adminSiteContentWriteRowStub { value := base; value.productAvailable = false; return &value }(), input: validAdminHomepageContentWriteInput(), want: errAdminHomepageProductFeatureUnavailable},
		{name: "managed hero absent", row: func() *adminSiteContentWriteRowStub { value := base; value.heroAvailable = false; return &value }(), input: validAdminHomepageContentWriteInput(), want: errAdminHomepageHeroRequired},
		{name: "wrong revision result", row: func() *adminSiteContentWriteRowStub { value := base; value.homepageVersion = 9; return &value }(), input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteFailed},
		{name: "scan error", row: &adminSiteContentWriteRowStub{scanError: errors.New("private driver detail")}, input: validAdminHomepageContentWriteInput(), want: errAdminSiteContentWriteFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminSiteContentWriteQuery{row: test.row}
			writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}
			result, err := writer.UpdateHomepage(t.Context(), 7, test.input)
			if !errors.Is(err, test.want) || result != (adminSiteContentWriteResult{}) {
				t.Fatalf("result=%#v err=%v, want %v", result, err, test.want)
			}
		})
	}
}

// TestPostgresAdminSiteContentWriterRejectsInvalidHomepageInputs proves invalid
// contexts, revisions, fields, and negative selector IDs fail before SQL.
func TestPostgresAdminSiteContentWriterRejectsInvalidHomepageInputs(t *testing.T) {
	valid := validAdminHomepageContentWriteInput()
	tests := []struct {
		// name identifies the invalid caller input.
		name string
		// ctx is nil only for the explicit missing-context case.
		ctx context.Context
		// version is the supplied optimistic coordinate.
		version int64
		// input is the Homepage mutation being rejected.
		input adminHomepageContentWriteInput
	}{
		{name: "nil context", version: 1, input: valid},
		{name: "zero revision", ctx: t.Context(), input: valid},
		{name: "maximum revision", ctx: t.Context(), version: math.MaxInt64, input: valid},
		{name: "untrimmed name", ctx: t.Context(), version: 1, input: func() adminHomepageContentWriteInput { value := valid; value.StudioName = " Zafarmand "; return value }()},
		{name: "negative selector", ctx: t.Context(), version: 1, input: func() adminHomepageContentWriteInput { value := valid; value.FeaturedProductID = -1; return value }()},
		{name: "multiline SEO", ctx: t.Context(), version: 1, input: func() adminHomepageContentWriteInput {
			value := valid
			value.SEODescription = "line one\nline two"
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &recordingAdminSiteContentWriteQuery{}
			writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}
			if _, err := writer.UpdateHomepage(test.ctx, test.version, test.input); !errors.Is(err, errAdminSiteContentWriteInvalid) || query.calls != 0 {
				t.Fatalf("error=%v calls=%d", err, query.calls)
			}
		})
	}
}

// TestPostgresAdminSiteContentWriterUpdateContact verifies exact binding,
// optimistic success, and stale/missing classification.
func TestPostgresAdminSiteContentWriterUpdateContact(t *testing.T) {
	input := validAdminContactContentWriteInput()
	query := &recordingAdminSiteContentWriteQuery{
		row: &adminSiteContentWriteRowStub{contactVersion: 5, singletonExists: true},
	}
	writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}
	result, err := writer.UpdateContact(t.Context(), 4, input)
	if err != nil || result != (adminSiteContentWriteResult{Version: 5}) ||
		query.query != updateAdminContactContentSQL || len(query.arguments) != 10 {
		t.Fatalf("Contact update: result=%#v query=%q args=%#v err=%v", result, query.query, query.arguments, err)
	}

	query.row = &adminSiteContentWriteRowStub{singletonExists: true}
	if _, err := writer.UpdateContact(t.Context(), 4, input); !errors.Is(err, errAdminSiteContentWriteConflict) {
		t.Errorf("stale Contact: got %v, want conflict", err)
	}
	query.row = &adminSiteContentWriteRowStub{}
	if _, err := writer.UpdateContact(t.Context(), 4, input); !errors.Is(err, errAdminSiteContentWriteFailed) {
		t.Errorf("missing Contact singleton: got %v, want write failure", err)
	}
}

// TestPostgresAdminSiteContentWriterRejectsInvalidContactInput proves email,
// phone-pair, trim, and revision failures do not cross the database seam.
func TestPostgresAdminSiteContentWriterRejectsInvalidContactInput(t *testing.T) {
	valid := validAdminContactContentWriteInput()
	invalid := []adminContactContentWriteInput{
		func() adminContactContentWriteInput {
			value := valid
			value.ContactEmail = "UPPER@example.com"
			return value
		}(),
		func() adminContactContentWriteInput { value := valid; value.PhoneE164 = ""; return value }(),
		func() adminContactContentWriteInput { value := valid; value.Address = " address "; return value }(),
		func() adminContactContentWriteInput { value := valid; value.Introduction = ""; return value }(),
	}
	for index, input := range invalid {
		query := &recordingAdminSiteContentWriteQuery{}
		writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}
		if _, err := writer.UpdateContact(t.Context(), 1, input); !errors.Is(err, errAdminSiteContentWriteInvalid) || query.calls != 0 {
			t.Errorf("invalid Contact %d: err=%v calls=%d", index, err, query.calls)
		}
	}
}

// TestPostgresAdminSiteContentWriterUpsertHomepageHero verifies exact normalized
// image binding, atomic revision results, stale classification, and byte checks.
func TestPostgresAdminSiteContentWriterUpsertHomepageHero(t *testing.T) {
	input := validAdminHomepageHeroWriteInput(t)
	query := &recordingAdminSiteContentWriteQuery{
		row: &adminSiteContentWriteRowStub{
			heroResult:      adminHomepageHeroWriteResult{HomepageVersion: 8, HeroVersion: 3},
			singletonExists: true,
		},
	}
	writer := &postgresAdminSiteContentWriter{queryRow: query.QueryRow}
	result, err := writer.UpsertHomepageHero(t.Context(), 7, input)
	if err != nil || result != (adminHomepageHeroWriteResult{HomepageVersion: 8, HeroVersion: 3}) ||
		query.query != upsertAdminHomepageHeroSQL || len(query.arguments) != 8 {
		t.Fatalf("hero upsert: result=%#v query=%q args=%d err=%v", result, query.query, len(query.arguments), err)
	}
	if digest, ok := query.arguments[6].([]byte); !ok || !bytes.Equal(digest, input.SHA256[:]) {
		t.Errorf("hero digest argument: %#v", query.arguments[6])
	}

	query.row = &adminSiteContentWriteRowStub{singletonExists: true}
	if _, err := writer.UpsertHomepageHero(t.Context(), 7, input); !errors.Is(err, errAdminSiteContentWriteConflict) {
		t.Errorf("stale hero: got %v, want conflict", err)
	}
	invalid := input
	invalid.SHA256[0] ^= 0xff
	query.calls = 0
	if _, err := writer.UpsertHomepageHero(t.Context(), 7, invalid); !errors.Is(err, errAdminSiteContentWriteInvalid) || query.calls != 0 {
		t.Errorf("invalid hero: err=%v calls=%d", err, query.calls)
	}
}

// TestAdminSiteContentWriteSQLRetainsAtomicBoundaries guards feature rechecks,
// optimistic revisions, singleton media ownership, and exact hero replacement.
func TestAdminSiteContentWriteSQLRetainsAtomicBoundaries(t *testing.T) {
	homepageFragments := []string{
		"WITH current_homepage AS MATERIALIZED",
		"FOR UPDATE\n),\neligibility AS MATERIALIZED",
		"FROM current_homepage\n),\nupdated_homepage AS",
		"project.publication_status = 'published'",
		"INNER JOIN public.interior_project_cover_images",
		"INNER JOIN public.architecture_project_cover_images",
		"INNER JOIN public.product_cover_images",
		"FOR SHARE OF project, cover",
		"FOR SHARE OF product, cover",
		"WHERE id = 1",
		"AND version = $1",
		"version = version + 1",
	}
	for _, fragment := range homepageFragments {
		if !strings.Contains(updateAdminHomepageContentSQL, fragment) {
			t.Errorf("Homepage update SQL lacks %q", fragment)
		}
	}
	for _, fragment := range []string{
		"FROM updated_homepage",
		"ON CONFLICT (homepage_content_id) DO UPDATE",
		"homepage_hero_images.version + 1",
		"managed_hero_enabled = true",
	} {
		if !strings.Contains(upsertAdminHomepageHeroSQL, fragment) {
			t.Errorf("hero upsert SQL lacks %q", fragment)
		}
	}
}
