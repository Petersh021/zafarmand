package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

// adminSiteContentScannerFunc adapts one focused closure to the protected
// repository scan seam.
type adminSiteContentScannerFunc func(...any) error

// Scan delegates to the configured test closure.
func (scanner adminSiteContentScannerFunc) Scan(destinations ...any) error {
	return scanner(destinations...)
}

// adminSiteContentHomepageRowStub copies one complete protected Homepage
// projection into the reader's fixed destinations.
type adminSiteContentHomepageRowStub struct {
	// record is the expected protected projection.
	record adminHomepageContentRecord
	// scanError simulates no rows or a driver decoding failure.
	scanError error
}

// Scan implements the 31-column protected Homepage row contract.
func (stub *adminSiteContentHomepageRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 31 {
		return errors.New("admin Homepage scan expected 31 destinations")
	}

	*destinations[0].(*int64) = stub.record.ID
	*destinations[1].(*string) = stub.record.StudioName
	*destinations[2].(*string) = stub.record.Descriptor
	*destinations[3].(*bool) = stub.record.ManagedHeroEnabled
	setAdminSiteContentSelectionDestinations(destinations[4:10], stub.record.FeaturedInterior)
	setAdminSiteContentSelectionDestinations(destinations[10:16], stub.record.FeaturedArchitecture)
	setAdminSiteContentSelectionDestinations(destinations[16:22], stub.record.FeaturedProduct)
	*destinations[22].(*string) = stub.record.SEOTitle
	*destinations[23].(*string) = stub.record.SEODescription
	if stub.record.Hero != nil {
		*destinations[24].(*sql.NullInt64) = sql.NullInt64{Int64: stub.record.Hero.Version, Valid: true}
		*destinations[25].(*sql.NullInt64) = sql.NullInt64{Int64: int64(stub.record.Hero.Width), Valid: true}
		*destinations[26].(*sql.NullInt64) = sql.NullInt64{Int64: int64(stub.record.Hero.Height), Valid: true}
		*destinations[27].(*sql.NullString) = sql.NullString{String: stub.record.Hero.AltText, Valid: true}
	}
	*destinations[28].(*int64) = stub.record.Version
	*destinations[29].(*time.Time) = stub.record.CreatedAt
	*destinations[30].(*time.Time) = stub.record.UpdatedAt

	return nil
}

// setAdminSiteContentSelectionDestinations fills one six-column nullable join
// group while leaving a genuinely clear selector entirely NULL.
func setAdminSiteContentSelectionDestinations(
	destinations []any,
	selection *adminHomepageFeatureSelection,
) {
	if selection == nil {
		return
	}
	*destinations[0].(*sql.NullInt64) = sql.NullInt64{Int64: selection.ID, Valid: true}
	*destinations[1].(*sql.NullString) = sql.NullString{String: selection.Slug, Valid: true}
	*destinations[2].(*sql.NullString) = sql.NullString{String: selection.Title, Valid: true}
	*destinations[3].(*sql.NullString) = sql.NullString{String: selection.Classification, Valid: true}
	*destinations[4].(*sql.NullString) = sql.NullString{String: selection.PublicationStatus, Valid: true}
	if selection.CoverVersion > 0 {
		*destinations[5].(*sql.NullInt64) = sql.NullInt64{Int64: selection.CoverVersion, Valid: true}
	}
}

// adminSiteContentContactRowStub copies one complete protected Contact row.
type adminSiteContentContactRowStub struct {
	// record is the expected protected projection.
	record adminContactContentRecord
	// scanError simulates a database failure.
	scanError error
}

// Scan implements the 13-column protected Contact row contract.
func (stub *adminSiteContentContactRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 13 {
		return errors.New("admin Contact scan expected 13 destinations")
	}
	*destinations[0].(*int64) = stub.record.ID
	*destinations[1].(*string) = stub.record.Eyebrow
	*destinations[2].(*string) = stub.record.Heading
	*destinations[3].(*string) = stub.record.Introduction
	*destinations[4].(*string) = stub.record.ContactEmail
	*destinations[5].(*string) = stub.record.PhoneDisplay
	*destinations[6].(*string) = stub.record.PhoneE164
	*destinations[7].(*string) = stub.record.Address
	*destinations[8].(*string) = stub.record.SEOTitle
	*destinations[9].(*string) = stub.record.SEODescription
	*destinations[10].(*int64) = stub.record.Version
	*destinations[11].(*time.Time) = stub.record.CreatedAt
	*destinations[12].(*time.Time) = stub.record.UpdatedAt

	return nil
}

// adminSiteContentHeroRowStub copies one exact protected hero asset.
type adminSiteContentHeroRowStub struct {
	// asset is the complete expected binary projection.
	asset homepageHeroAsset
	// scanError simulates no rows or a driver failure.
	scanError error
}

// Scan implements the ten-column protected hero row contract.
func (stub *adminSiteContentHeroRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 10 {
		return errors.New("admin Homepage hero scan expected ten destinations")
	}
	*destinations[0].(*int64) = stub.asset.Version
	*destinations[1].(*string) = stub.asset.ContentType
	*destinations[2].(*[]byte) = append([]byte(nil), stub.asset.Content...)
	*destinations[3].(*int) = stub.asset.ByteSize
	*destinations[4].(*int) = stub.asset.Width
	*destinations[5].(*int) = stub.asset.Height
	*destinations[6].(*[]byte) = append([]byte(nil), stub.asset.SHA256[:]...)
	*destinations[7].(*string) = stub.asset.AltText
	*destinations[8].(*time.Time) = stub.asset.CreatedAt
	*destinations[9].(*time.Time) = stub.asset.UpdatedAt

	return nil
}

// adminSiteContentCandidateRowsStub provides deterministic ordered candidate
// rows and configurable iterator failures.
type adminSiteContentCandidateRowsStub struct {
	// candidates are copied in source order.
	candidates []adminHomepageFeatureCandidate
	// index is the current iterator coordinate.
	index int
	// iterationError and closeError simulate driver lifecycle failures.
	iterationError error
	closeError     error
}

// Next advances to the next arranged candidate.
func (stub *adminSiteContentCandidateRowsStub) Next() bool {
	if stub.index >= len(stub.candidates) {
		return false
	}
	stub.index++

	return true
}

// Scan copies the current seven-column candidate projection.
func (stub *adminSiteContentCandidateRowsStub) Scan(destinations ...any) error {
	if len(destinations) != 7 || stub.index <= 0 || stub.index > len(stub.candidates) {
		return errors.New("admin candidate scan coordinate is invalid")
	}
	candidate := stub.candidates[stub.index-1]
	*destinations[0].(*int64) = int64(candidate.Discipline)
	*destinations[1].(*int64) = candidate.ID
	*destinations[2].(*string) = candidate.Slug
	*destinations[3].(*string) = candidate.Title
	*destinations[4].(*string) = candidate.Classification
	*destinations[5].(*int) = candidate.SortOrder
	*destinations[6].(*int64) = candidate.CoverVersion

	return nil
}

// Err returns the arranged post-iteration failure.
func (stub *adminSiteContentCandidateRowsStub) Err() error {
	return stub.iterationError
}

// Close returns the arranged connection-release failure.
func (stub *adminSiteContentCandidateRowsStub) Close() error {
	return stub.closeError
}

// validAdminHomepageContentRecord returns a complete protected record with one
// eligible, one unavailable, and one clear fixed slot.
func validAdminHomepageContentRecord() adminHomepageContentRecord {
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	return adminHomepageContentRecord{
		ID:                 siteContentSingletonID,
		StudioName:         "Zafarmand",
		Descriptor:         "Design Studio",
		ManagedHeroEnabled: true,
		FeaturedInterior: &adminHomepageFeatureSelection{
			Discipline:        homepageFeatureInterior,
			ID:                11,
			Slug:              "quiet-courtyard",
			Title:             "Quiet Courtyard",
			Classification:    "Residential",
			PublicationStatus: "published",
			CoverVersion:      2,
			Eligible:          true,
		},
		FeaturedArchitecture: &adminHomepageFeatureSelection{
			Discipline:        homepageFeatureArchitecture,
			ID:                12,
			Slug:              "old-pavilion",
			Title:             "Old Pavilion",
			Classification:    "Cultural",
			PublicationStatus: "archived",
		},
		SEOTitle:       "Home | Zafarmand",
		SEODescription: "Selected work by Zafarmand design studio",
		Hero: &homepageHeroMetadata{
			Version: 3,
			Width:   1800,
			Height:  1200,
			AltText: "Stone interior opening toward a courtyard",
		},
		Version:   7,
		CreatedAt: now,
		UpdatedAt: now.Add(time.Hour),
	}
}

// validAdminContactContentRecord returns one complete protected Contact row.
func validAdminContactContentRecord() adminContactContentRecord {
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	return adminContactContentRecord{
		ID:             siteContentSingletonID,
		Eyebrow:        "Contact",
		Heading:        "Begin a conversation",
		Introduction:   "Share the project context Zafarmand should review.",
		ContactEmail:   "studio@example.com",
		PhoneDisplay:   "+98 21 5555 0101",
		PhoneE164:      "+982155550101",
		Address:        "Studio 4\nTehran",
		SEOTitle:       "Contact | Zafarmand",
		SEODescription: "Contact Zafarmand design studio",
		Version:        4,
		CreatedAt:      now,
		UpdatedAt:      now.Add(time.Hour),
	}
}

// TestNewPostgresAdminSiteContentReader verifies nil dependency rejection and
// successful adapter construction without issuing SQL.
func TestNewPostgresAdminSiteContentReader(t *testing.T) {
	reader, err := newPostgresAdminSiteContentReader(nil)
	if !errors.Is(err, errAdminSiteContentReaderDatabaseRequired) || reader != nil {
		t.Fatalf("nil database: reader=%#v err=%v", reader, err)
	}
	reader, err = newPostgresAdminSiteContentReader(&sql.DB{})
	if err != nil || reader == nil || reader.query == nil || reader.queryRow == nil {
		t.Fatalf("constructed reader: reader=%#v err=%v", reader, err)
	}
}

// TestPostgresAdminSiteContentReaderReadsSingletons verifies exact SQL,
// context forwarding, nullable selection behavior, and complete protected data.
func TestPostgresAdminSiteContentReaderReadsSingletons(t *testing.T) {
	homepage := validAdminHomepageContentRecord()
	contact := validAdminContactContentRecord()
	// call captures one protected singleton query for context and SQL assertions.
	type call struct {
		// ctx is the exact request-derived context passed to the adapter.
		ctx context.Context
		// query is the fixed SQL statement selected by the repository method.
		query string
	}
	var calls []call
	reader := &postgresAdminSiteContentReader{
		queryRow: func(
			ctx context.Context,
			query string,
			_ ...any,
		) adminSiteContentRowScanner {
			calls = append(calls, call{ctx: ctx, query: query})
			switch query {
			case readAdminHomepageContentSQL:
				return &adminSiteContentHomepageRowStub{record: homepage}
			case readAdminContactContentSQL:
				return &adminSiteContentContactRowStub{record: contact}
			default:
				return adminSiteContentScannerFunc(func(...any) error {
					return errors.New("unexpected protected settings query")
				})
			}
		},
	}
	ctx := context.WithValue(t.Context(), struct{ name string }{"stage24"}, "forwarded")

	gotHomepage, err := reader.ReadHomepage(ctx)
	if err != nil || !reflect.DeepEqual(gotHomepage, homepage) {
		t.Fatalf("Homepage: got=%#v err=%v want=%#v", gotHomepage, err, homepage)
	}
	gotContact, err := reader.ReadContact(ctx)
	if err != nil || !reflect.DeepEqual(gotContact, contact) {
		t.Fatalf("Contact: got=%#v err=%v want=%#v", gotContact, err, contact)
	}
	if len(calls) != 2 || calls[0].ctx != ctx || calls[0].query != readAdminHomepageContentSQL ||
		calls[1].ctx != ctx || calls[1].query != readAdminContactContentSQL {
		t.Errorf("read calls: %#v", calls)
	}
}

// TestPostgresAdminSiteContentReaderRejectsMalformedSingletons proves missing
// managed hero metadata, partial selection joins, and malformed Contact data
// collapse to the safe read-failure category.
func TestPostgresAdminSiteContentReaderRejectsMalformedSingletons(t *testing.T) {
	homepage := validAdminHomepageContentRecord()
	homepage.Hero = nil
	reader := &postgresAdminSiteContentReader{
		queryRow: func(context.Context, string, ...any) adminSiteContentRowScanner {
			return &adminSiteContentHomepageRowStub{record: homepage}
		},
	}
	if _, err := reader.ReadHomepage(t.Context()); !errors.Is(err, errAdminSiteContentReadFailed) {
		t.Errorf("enabled missing hero: got %v, want read failure", err)
	}

	reader.queryRow = func(context.Context, string, ...any) adminSiteContentRowScanner {
		return adminSiteContentScannerFunc(func(destinations ...any) error {
			if err := (&adminSiteContentHomepageRowStub{record: validAdminHomepageContentRecord()}).Scan(destinations...); err != nil {
				return err
			}
			*destinations[7].(*sql.NullString) = sql.NullString{}

			return nil
		})
	}
	if _, err := reader.ReadHomepage(t.Context()); !errors.Is(err, errAdminSiteContentReadFailed) {
		t.Errorf("partial selection: got %v, want read failure", err)
	}

	contact := validAdminContactContentRecord()
	contact.ContactEmail = "UPPER@example.com"
	reader.queryRow = func(context.Context, string, ...any) adminSiteContentRowScanner {
		return &adminSiteContentContactRowStub{record: contact}
	}
	if _, err := reader.ReadContact(t.Context()); !errors.Is(err, errAdminSiteContentReadFailed) {
		t.Errorf("malformed Contact: got %v, want read failure", err)
	}
}

// TestPostgresAdminSiteContentReaderListsEligibleCandidates verifies exact
// query use, fixed discipline ordering, and isolated returned storage.
func TestPostgresAdminSiteContentReaderListsEligibleCandidates(t *testing.T) {
	want := []adminHomepageFeatureCandidate{
		{Discipline: homepageFeatureInterior, ID: 3, Slug: "quiet-room", Title: "Quiet Room", Classification: "Residential", SortOrder: 1, CoverVersion: 2},
		{Discipline: homepageFeatureArchitecture, ID: 4, Slug: "garden-pavilion", Title: "Garden Pavilion", Classification: "Cultural", SortOrder: 2, CoverVersion: 1},
		{Discipline: homepageFeatureProduct, ID: 5, Slug: "folded-chair", Title: "Folded Chair", Classification: "Furniture", SortOrder: 3, CoverVersion: 4},
	}
	var gotQuery string
	reader := &postgresAdminSiteContentReader{
		query: func(_ context.Context, query string, _ ...any) (adminSiteContentRows, error) {
			gotQuery = query
			return &adminSiteContentCandidateRowsStub{candidates: want}, nil
		},
	}
	got, err := reader.ListFeatureCandidates(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) || gotQuery != listAdminHomepageFeatureCandidatesSQL {
		t.Fatalf("candidates: got=%#v query=%q err=%v", got, gotQuery, err)
	}
	got[0].Title = "mutated"
	if want[0].Title == got[0].Title {
		t.Error("candidate result shares slice storage")
	}
}

// TestPostgresAdminSiteContentReaderRejectsCandidateContractFailures covers
// invalid order, duplicates, iterator errors, close errors, and nil rows.
func TestPostgresAdminSiteContentReaderRejectsCandidateContractFailures(t *testing.T) {
	valid := adminHomepageFeatureCandidate{Discipline: homepageFeatureInterior, ID: 3, Slug: "quiet-room", Title: "Quiet Room", Classification: "Residential", SortOrder: 1, CoverVersion: 2}
	tests := []struct {
		// name identifies the row-iteration contract failure.
		name string
		// rows supplies controlled candidate rows when the query succeeds.
		rows adminSiteContentRows
		// err is the controlled query-level failure.
		err error
	}{
		{name: "nil rows"},
		{name: "query error", err: errors.New("private driver detail")},
		{name: "duplicate", rows: &adminSiteContentCandidateRowsStub{candidates: []adminHomepageFeatureCandidate{valid, valid}}},
		{name: "wrong order", rows: &adminSiteContentCandidateRowsStub{candidates: []adminHomepageFeatureCandidate{{Discipline: homepageFeatureArchitecture, ID: 4, Slug: "pavilion", Title: "Pavilion", Classification: "Cultural", SortOrder: 1, CoverVersion: 1}, valid}}},
		{name: "iteration error", rows: &adminSiteContentCandidateRowsStub{iterationError: errors.New("private iteration")}},
		{name: "close error", rows: &adminSiteContentCandidateRowsStub{closeError: errors.New("private close")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &postgresAdminSiteContentReader{
				query: func(context.Context, string, ...any) (adminSiteContentRows, error) {
					return test.rows, test.err
				},
			}
			if _, err := reader.ListFeatureCandidates(t.Context()); !errors.Is(err, errAdminSiteContentReadFailed) {
				t.Fatalf("error: got %v, want read failure", err)
			}
		})
	}
}

// TestPostgresAdminSiteContentReaderFindHomepageHero verifies exact revision
// binding, no-row mapping, byte isolation, and invalid coordinate rejection.
func TestPostgresAdminSiteContentReaderFindHomepageHero(t *testing.T) {
	asset := validTestHomepageHeroAsset(t, 5)
	var arguments []any
	reader := &postgresAdminSiteContentReader{
		queryRow: func(_ context.Context, query string, values ...any) adminSiteContentRowScanner {
			if query != findAdminHomepageHeroSQL {
				t.Errorf("query: got %q", query)
			}
			arguments = append([]any(nil), values...)
			return &adminSiteContentHeroRowStub{asset: asset}
		},
	}
	got, err := reader.FindHomepageHero(t.Context(), 5)
	if err != nil || !reflect.DeepEqual(got, asset) || !reflect.DeepEqual(arguments, []any{int64(5)}) {
		t.Fatalf("hero: got=%#v args=%#v err=%v", got, arguments, err)
	}
	got.Content[0] ^= 0xff
	if reflect.DeepEqual(got.Content, asset.Content) {
		t.Error("returned hero shares mutable bytes")
	}

	reader.queryRow = func(context.Context, string, ...any) adminSiteContentRowScanner {
		return &adminSiteContentHeroRowStub{scanError: sql.ErrNoRows}
	}
	if _, err := reader.FindHomepageHero(t.Context(), 5); !errors.Is(err, errAdminHomepageHeroNotFound) {
		t.Errorf("missing hero: got %v, want not found", err)
	}
	if _, err := reader.FindHomepageHero(t.Context(), 0); !errors.Is(err, errAdminSiteContentInvalidQuery) {
		t.Errorf("zero revision: got %v, want invalid query", err)
	}
}
