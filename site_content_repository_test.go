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

// siteContentScannerFunc adapts one focused closure to the repository's
// database/sql scan seam.
type siteContentScannerFunc func(...any) error

// Scan delegates to the configured test closure.
func (scanner siteContentScannerFunc) Scan(destinations ...any) error {
	return scanner(destinations...)
}

// siteContentHomepageRowStub copies one complete joined Homepage projection
// into the repository's fixed destinations.
type siteContentHomepageRowStub struct {
	// content is the expected public projection.
	content publicHomepageContent
	// managedHeroEnabled is selected independently from optional hero metadata.
	managedHeroEnabled bool
	// scanError simulates sql.ErrNoRows or a driver decoding failure.
	scanError error
}

// Scan implements the 34-column Homepage row contract.
func (stub *siteContentHomepageRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 34 {
		return errors.New("Homepage content scan expected 34 destinations")
	}

	*destinations[0].(*int64) = siteContentSingletonID
	*destinations[1].(*string) = stub.content.StudioName
	*destinations[2].(*string) = stub.content.Descriptor
	*destinations[3].(*string) = stub.content.SEOTitle
	*destinations[4].(*string) = stub.content.SEODescription
	*destinations[5].(*bool) = stub.managedHeroEnabled
	if stub.content.Hero != nil {
		*destinations[6].(*sql.NullInt64) = sql.NullInt64{
			Int64: stub.content.Hero.Version,
			Valid: true,
		}
		*destinations[7].(*sql.NullInt64) = sql.NullInt64{
			Int64: int64(stub.content.Hero.Width),
			Valid: true,
		}
		*destinations[8].(*sql.NullInt64) = sql.NullInt64{
			Int64: int64(stub.content.Hero.Height),
			Valid: true,
		}
		*destinations[9].(*sql.NullString) = sql.NullString{
			String: stub.content.Hero.AltText,
			Valid:  true,
		}
	}

	featureOffsets := map[homepageFeatureDiscipline]int{
		homepageFeatureInterior:     10,
		homepageFeatureArchitecture: 18,
		homepageFeatureProduct:      26,
	}
	for index, feature := range stub.content.Features {
		offset, exists := featureOffsets[feature.Discipline]
		if !exists {
			continue
		}
		setSiteContentFeatureDestinations(
			destinations[offset:offset+8],
			int64(index+10),
			feature,
		)
	}

	return nil
}

// setSiteContentFeatureDestinations fills one nullable feature group. A nil
// cover deliberately leaves its four cover columns NULL for fail-closed tests.
func setSiteContentFeatureDestinations(
	destinations []any,
	id int64,
	feature publicHomepageFeature,
) {
	*destinations[0].(*sql.NullInt64) = sql.NullInt64{Int64: id, Valid: true}
	*destinations[1].(*sql.NullString) = sql.NullString{
		String: feature.Slug,
		Valid:  true,
	}
	*destinations[2].(*sql.NullString) = sql.NullString{
		String: feature.Title,
		Valid:  true,
	}
	*destinations[3].(*sql.NullString) = sql.NullString{
		String: feature.Classification,
		Valid:  true,
	}
	if feature.Cover == nil {
		return
	}
	*destinations[4].(*sql.NullInt64) = sql.NullInt64{
		Int64: feature.Cover.Version,
		Valid: true,
	}
	*destinations[5].(*sql.NullInt64) = sql.NullInt64{
		Int64: int64(feature.Cover.Width),
		Valid: true,
	}
	*destinations[6].(*sql.NullInt64) = sql.NullInt64{
		Int64: int64(feature.Cover.Height),
		Valid: true,
	}
	*destinations[7].(*sql.NullString) = sql.NullString{
		String: feature.Cover.AltText,
		Valid:  true,
	}
}

// siteContentContactRowStub copies one Contact singleton projection.
type siteContentContactRowStub struct {
	// content is the expected public projection.
	content publicContactContent
	// scanError simulates a database failure.
	scanError error
}

// Scan implements the ten-column Contact row contract.
func (stub *siteContentContactRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 10 {
		return errors.New("Contact content scan expected ten destinations")
	}

	*destinations[0].(*int64) = siteContentSingletonID
	*destinations[1].(*string) = stub.content.Eyebrow
	*destinations[2].(*string) = stub.content.Heading
	*destinations[3].(*string) = stub.content.Introduction
	*destinations[4].(*string) = stub.content.Email
	*destinations[5].(*string) = stub.content.PhoneDisplay
	*destinations[6].(*string) = stub.content.PhoneE164
	*destinations[7].(*string) = stub.content.Address
	*destinations[8].(*string) = stub.content.SEOTitle
	*destinations[9].(*string) = stub.content.SEODescription

	return nil
}

// siteContentHeroRowStub copies one exact hero asset and singleton owner.
type siteContentHeroRowStub struct {
	// asset contains the complete expected binary projection.
	asset homepageHeroAsset
	// homepageID permits a wrong-owner fail-closed case.
	homepageID int64
	// scanError simulates no rows or an operational failure.
	scanError error
}

// Scan implements the eleven-column exact hero row contract.
func (stub *siteContentHeroRowStub) Scan(destinations ...any) error {
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 11 {
		return errors.New("Homepage hero scan expected eleven destinations")
	}

	*destinations[0].(*int64) = stub.homepageID
	*destinations[1].(*int64) = stub.asset.Version
	*destinations[2].(*string) = stub.asset.ContentType
	*destinations[3].(*[]byte) = append([]byte(nil), stub.asset.Content...)
	*destinations[4].(*int) = stub.asset.ByteSize
	*destinations[5].(*int) = stub.asset.Width
	*destinations[6].(*int) = stub.asset.Height
	*destinations[7].(*[]byte) = append([]byte(nil), stub.asset.SHA256[:]...)
	*destinations[8].(*string) = stub.asset.AltText
	*destinations[9].(*time.Time) = stub.asset.CreatedAt
	*destinations[10].(*time.Time) = stub.asset.UpdatedAt

	return nil
}

// siteContentQueryCall records one fixed repository invocation.
type siteContentQueryCall struct {
	// context is retained to prove exact forwarding.
	context context.Context
	// query is the complete fixed SQL statement.
	query string
	// arguments are isolated bound values.
	arguments []any
}

// siteContentContextKey is a private marker for context-forwarding assertions.
type siteContentContextKey struct{}

// TestPostgresSiteContentReaderReadsValidatedSingletons verifies fixed SQL,
// trusted arguments, exact context forwarding, feature order, and Contact data.
func TestPostgresSiteContentReaderReadsValidatedSingletons(t *testing.T) {
	homepage := validRepositoryHomepageContent()
	contact := publicContactContent{
		Eyebrow:        "Write to us",
		Heading:        "Begin a considered conversation",
		Introduction:   "Share the project context and the discipline that should review it.",
		Email:          "studio@example.com",
		PhoneDisplay:   "+98 21 5555 0101",
		PhoneE164:      "+982155550101",
		Address:        "Studio 4\nTehran",
		SEOTitle:       "Contact Zafarmand",
		SEODescription: "Contact the Zafarmand design studio.",
	}
	var calls []siteContentQueryCall
	reader := &postgresSiteContentReader{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) siteContentRowScanner {
			calls = append(calls, siteContentQueryCall{
				context:   ctx,
				query:     query,
				arguments: append([]any(nil), arguments...),
			})
			switch query {
			case readHomepageContentSQL:
				return &siteContentHomepageRowStub{
					content:            homepage,
					managedHeroEnabled: true,
				}
			case readContactContentSQL:
				return &siteContentContactRowStub{content: contact}
			default:
				return siteContentScannerFunc(func(...any) error {
					return errors.New("unexpected site-content query")
				})
			}
		},
	}
	ctx := context.WithValue(t.Context(), siteContentContextKey{}, "forwarded")

	gotHomepage, err := reader.ReadHomepage(ctx)
	if err != nil {
		t.Fatalf("read Homepage content: %v", err)
	}
	if !reflect.DeepEqual(gotHomepage, homepage) {
		t.Errorf("Homepage content: got %#v, want %#v", gotHomepage, homepage)
	}
	gotContact, err := reader.ReadContact(ctx)
	if err != nil {
		t.Fatalf("read Contact content: %v", err)
	}
	if !reflect.DeepEqual(gotContact, contact) {
		t.Errorf("Contact content: got %#v, want %#v", gotContact, contact)
	}

	if len(calls) != 2 {
		t.Fatalf("site-content calls: got %d, want 2", len(calls))
	}
	if calls[0].context != ctx || calls[0].query != readHomepageContentSQL ||
		!reflect.DeepEqual(
			calls[0].arguments,
			[]any{publishedSiteContentStatus, siteContentSingletonID},
		) {
		t.Errorf("Homepage query call: %#v", calls[0])
	}
	if calls[1].context != ctx || calls[1].query != readContactContentSQL ||
		!reflect.DeepEqual(
			calls[1].arguments,
			[]any{siteContentSingletonID},
		) {
		t.Errorf("Contact query call: %#v", calls[1])
	}
}

// TestHomepageContentScanFailsClosed covers the managed-hero publication switch
// and the cover-required feature boundary against substituted malformed rows.
func TestHomepageContentScanFailsClosed(t *testing.T) {
	valid := validRepositoryHomepageContent()
	tests := []struct {
		// name identifies one malformed nullable contract.
		name string
		// scanner provides the substituted row.
		scanner siteContentRowScanner
	}{
		{
			name: "enabled hero missing",
			scanner: &siteContentHomepageRowStub{
				content: publicHomepageContent{
					StudioName:     valid.StudioName,
					Descriptor:     valid.Descriptor,
					SEOTitle:       valid.SEOTitle,
					SEODescription: valid.SEODescription,
					Features:       valid.Features,
				},
				managedHeroEnabled: true,
			},
		},
		{
			name: "disabled hero unexpectedly projected",
			scanner: &siteContentHomepageRowStub{
				content:            valid,
				managedHeroEnabled: false,
			},
		},
		{
			name: "published feature lacks cover",
			scanner: &siteContentHomepageRowStub{
				content: publicHomepageContent{
					StudioName:     valid.StudioName,
					Descriptor:     valid.Descriptor,
					SEOTitle:       valid.SEOTitle,
					SEODescription: valid.SEODescription,
					Hero:           valid.Hero,
					Features: []publicHomepageFeature{
						{
							Discipline:     homepageFeatureInterior,
							Slug:           "quiet-courtyard",
							Title:          "Quiet Courtyard",
							Classification: "Residential",
						},
					},
				},
				managedHeroEnabled: true,
			},
		},
		{
			name: "partial hero metadata",
			scanner: siteContentScannerFunc(func(destinations ...any) error {
				if err := (&siteContentHomepageRowStub{
					content:            valid,
					managedHeroEnabled: true,
				}).Scan(destinations...); err != nil {
					return err
				}
				*destinations[9].(*sql.NullString) = sql.NullString{}

				return nil
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := scanPublicHomepageContent(test.scanner); !errors.Is(err, errSiteContentReadFailed) {
				t.Fatalf("scan error: got %v, want safe read failure", err)
			}
		})
	}
}

// TestPostgresSiteContentReaderFindHomepageHero verifies exact revision args,
// isolated bytes, safe no-row mapping, and invalid-query rejection.
func TestPostgresSiteContentReaderFindHomepageHero(t *testing.T) {
	asset := validTestHomepageHeroAsset(t, 4)
	var calls []siteContentQueryCall
	reader := &postgresSiteContentReader{
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) siteContentRowScanner {
			calls = append(calls, siteContentQueryCall{
				context:   ctx,
				query:     query,
				arguments: append([]any(nil), arguments...),
			})
			return &siteContentHeroRowStub{
				asset:      asset,
				homepageID: siteContentSingletonID,
			}
		},
	}

	got, err := reader.FindHomepageHero(t.Context(), asset.Version)
	if err != nil {
		t.Fatalf("find Homepage hero: %v", err)
	}
	if !reflect.DeepEqual(got, asset) {
		t.Errorf("Homepage hero: got %#v, want %#v", got, asset)
	}
	got.Content[0] ^= 0xff
	if reflect.DeepEqual(got.Content, asset.Content) {
		t.Error("returned Homepage hero shares mutable content storage")
	}
	if len(calls) != 1 || calls[0].query != findHomepageHeroSQL ||
		!reflect.DeepEqual(
			calls[0].arguments,
			[]any{siteContentSingletonID, asset.Version},
		) {
		t.Errorf("Homepage hero query calls: %#v", calls)
	}

	reader.queryRow = func(
		context.Context,
		string,
		...any,
	) siteContentRowScanner {
		return &siteContentHeroRowStub{scanError: sql.ErrNoRows}
	}
	if _, err := reader.FindHomepageHero(t.Context(), asset.Version); !errors.Is(err, errHomepageHeroNotFound) {
		t.Fatalf("missing hero error: got %v, want not found", err)
	}
	if _, err := reader.FindHomepageHero(t.Context(), 0); !errors.Is(err, errSiteContentInvalidQuery) {
		t.Fatalf("invalid hero version: got %v, want invalid query", err)
	}
}

// TestNewPostgresSiteContentReaderRequiresDatabase verifies construction fails
// before a first-request nil-pool panic.
func TestNewPostgresSiteContentReaderRequiresDatabase(t *testing.T) {
	reader, err := newPostgresSiteContentReader(nil)
	if !errors.Is(err, errSiteContentReaderDatabaseRequired) {
		t.Fatalf("nil database error: got %v, want required sentinel", err)
	}
	if reader != nil {
		t.Error("nil database returned a usable site-content reader")
	}
}

// TestHomepageContentSQLKeepsSelectionsPrivate protects the two eligibility
// predicates and confirms private FK/lifecycle columns are not selected.
func TestHomepageContentSQLKeepsSelectionsPrivate(t *testing.T) {
	for _, fragment := range []string{
		"WHERE projects.publication_status = $1",
		"INNER JOIN public.interior_project_cover_images",
		"INNER JOIN public.architecture_project_cover_images",
		"INNER JOIN public.product_cover_images",
	} {
		if !strings.Contains(readHomepageContentSQL, fragment) {
			t.Errorf("Homepage SQL lacks public eligibility fragment %q", fragment)
		}
	}
	selectClause := strings.Split(readHomepageContentSQL, "FROM public.homepage_content")[0]
	for _, privateColumn := range []string{
		"featured_product_id",
		"featured_interior_project_id",
		"featured_architecture_project_id",
		"publication_status",
		"homepage.version",
	} {
		if strings.Contains(selectClause, privateColumn) {
			t.Errorf("Homepage public projection selects private column %q", privateColumn)
		}
	}
}

// validRepositoryHomepageContent returns a complete managed hero and all three
// fixed cover-backed feature disciplines in public order.
func validRepositoryHomepageContent() publicHomepageContent {
	return publicHomepageContent{
		StudioName:     "Zafarmand",
		Descriptor:     "Architecture, interiors, and objects",
		SEOTitle:       "Zafarmand Design Studio",
		SEODescription: "Selected architecture, interiors, and products by Zafarmand.",
		Hero: &homepageHeroMetadata{
			Version: 3,
			Width:   1800,
			Height:  1200,
			AltText: "Stone interior opening toward a planted courtyard",
		},
		Features: []publicHomepageFeature{
			{
				Discipline:     homepageFeatureInterior,
				Slug:           "quiet-courtyard",
				Title:          "Quiet Courtyard",
				Classification: "Residential",
				Cover: &homepageFeatureCover{
					Version: 2,
					Width:   1200,
					Height:  1500,
					AltText: "Quiet courtyard Interior project",
				},
			},
			{
				Discipline:     homepageFeatureArchitecture,
				Slug:           "garden-pavilion",
				Title:          "Garden Pavilion",
				Classification: "Cultural",
				Cover: &homepageFeatureCover{
					Version: 4,
					Width:   1600,
					Height:  1200,
					AltText: "Garden pavilion Architecture project",
				},
			},
			{
				Discipline:     homepageFeatureProduct,
				Slug:           "folded-chair",
				Title:          "Folded Chair",
				Classification: "Furniture",
				Cover: &homepageFeatureCover{
					Version: 6,
					Width:   1000,
					Height:  1400,
					AltText: "Folded timber chair",
				},
			},
		},
	}
}
