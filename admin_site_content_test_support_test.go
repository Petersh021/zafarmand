package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

// testAdminHomepageHeroPNG encodes a tiny deterministic fictional image so
// Stage 24 upload tests exercise the real decoder without depending on a
// Product, Interior, or Architecture test fixture.
func testAdminHomepageHeroPNG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetNRGBA(x, y, color.NRGBA{
				R: uint8(45 + x*18),
				G: uint8(35 + y*24),
				B: uint8(65 + x*8 + y*6),
				A: 255,
			})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatalf("encode deterministic Homepage hero: %v", err)
	}

	return buffer.Bytes()
}

// recordingAdminSiteContentReader is an in-memory protected read test double.
// Construction and HTTP tests can arrange each operation independently without
// opening PostgreSQL.
type recordingAdminSiteContentReader struct {
	// homepageResult and homepageError control ReadHomepage.
	homepageResult adminHomepageContentRecord
	homepageError  error
	// homepageCalls and context record ReadHomepage invocations.
	homepageCalls   int
	homepageContext context.Context
	// contactResult and contactError control ReadContact.
	contactResult adminContactContentRecord
	contactError  error
	// contactCalls and context record ReadContact invocations.
	contactCalls   int
	contactContext context.Context
	// candidateResult and candidateError control ListFeatureCandidates.
	candidateResult []adminHomepageFeatureCandidate
	candidateError  error
	// candidateCalls and context record candidate-list invocations.
	candidateCalls   int
	candidateContext context.Context
	// heroResult and heroError control FindHomepageHero.
	heroResult homepageHeroAsset
	heroError  error
	// hero calls and coordinates record exact protected media reads.
	heroCalls   int
	heroContext context.Context
	heroVersion int64
}

// newRecordingAdminSiteContentReader returns a dependency with valid seeded
// singleton projections and an allocated empty candidate collection.
func newRecordingAdminSiteContentReader() *recordingAdminSiteContentReader {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	return &recordingAdminSiteContentReader{
		homepageResult: adminHomepageContentRecord{
			ID:             siteContentSingletonID,
			StudioName:     "Zafarmand",
			Descriptor:     "Design Studio",
			SEOTitle:       "Home | Zafarmand",
			SEODescription: "Zafarmand design studio",
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		contactResult: adminContactContentRecord{
			ID:             siteContentSingletonID,
			Eyebrow:        "Contact",
			Heading:        "Begin a conversation",
			Introduction:   "Choose a discipline and share the context Zafarmand should review.",
			SEOTitle:       "Contact | Zafarmand",
			SEODescription: "Zafarmand design studio",
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		candidateResult: make([]adminHomepageFeatureCandidate, 0),
	}
}

// ReadHomepage records its caller and returns an isolated configured record.
func (reader *recordingAdminSiteContentReader) ReadHomepage(
	ctx context.Context,
) (adminHomepageContentRecord, error) {
	reader.homepageCalls++
	reader.homepageContext = ctx
	return cloneAdminHomepageContentRecord(reader.homepageResult), reader.homepageError
}

// ReadContact records its caller and returns the configured immutable-value
// record.
func (reader *recordingAdminSiteContentReader) ReadContact(
	ctx context.Context,
) (adminContactContentRecord, error) {
	reader.contactCalls++
	reader.contactContext = ctx
	return reader.contactResult, reader.contactError
}

// ListFeatureCandidates records its caller and returns an isolated slice.
func (reader *recordingAdminSiteContentReader) ListFeatureCandidates(
	ctx context.Context,
) ([]adminHomepageFeatureCandidate, error) {
	reader.candidateCalls++
	reader.candidateContext = ctx
	if reader.candidateError != nil {
		return nil, reader.candidateError
	}
	result := make([]adminHomepageFeatureCandidate, len(reader.candidateResult))
	copy(result, reader.candidateResult)

	return result, nil
}

// FindHomepageHero records one exact revision and returns an isolated asset.
func (reader *recordingAdminSiteContentReader) FindHomepageHero(
	ctx context.Context,
	version int64,
) (homepageHeroAsset, error) {
	reader.heroCalls++
	reader.heroContext = ctx
	reader.heroVersion = version
	return cloneHomepageHeroAsset(reader.heroResult), reader.heroError
}

// cloneAdminHomepageContentRecord isolates optional selection and hero pointers
// whenever a protected singleton crosses a test-double boundary.
func cloneAdminHomepageContentRecord(
	record adminHomepageContentRecord,
) adminHomepageContentRecord {
	if record.FeaturedInterior != nil {
		selection := *record.FeaturedInterior
		record.FeaturedInterior = &selection
	}
	if record.FeaturedArchitecture != nil {
		selection := *record.FeaturedArchitecture
		record.FeaturedArchitecture = &selection
	}
	if record.FeaturedProduct != nil {
		selection := *record.FeaturedProduct
		record.FeaturedProduct = &selection
	}
	if record.Hero != nil {
		hero := *record.Hero
		record.Hero = &hero
	}

	return record
}

// recordingAdminSiteContentWriter is an in-memory mutation test double. Each
// operation records its complete optimistic input and returns a configurable
// result.
type recordingAdminSiteContentWriter struct {
	// homepageResult and homepageError control UpdateHomepage.
	homepageResult adminSiteContentWriteResult
	homepageError  error
	// Homepage call fields record context, revision, and complete input.
	homepageCalls           int
	homepageContext         context.Context
	homepageExpectedVersion int64
	homepageInput           adminHomepageContentWriteInput
	// contactResult and contactError control UpdateContact.
	contactResult adminSiteContentWriteResult
	contactError  error
	// Contact call fields record context, revision, and complete input.
	contactCalls           int
	contactContext         context.Context
	contactExpectedVersion int64
	contactInput           adminContactContentWriteInput
	// heroResult and heroError control UpsertHomepageHero.
	heroResult adminHomepageHeroWriteResult
	heroError  error
	// Hero call fields record context, parent revision, and isolated media input.
	heroCalls           int
	heroContext         context.Context
	heroExpectedVersion int64
	heroInput           adminHomepageHeroWriteInput
}

// newRecordingAdminSiteContentWriter returns a nonnil dependency with no
// implicit successful mutation result.
func newRecordingAdminSiteContentWriter() *recordingAdminSiteContentWriter {
	return &recordingAdminSiteContentWriter{}
}

// UpdateHomepage records one complete optimistic Homepage edit.
func (writer *recordingAdminSiteContentWriter) UpdateHomepage(
	ctx context.Context,
	expectedVersion int64,
	input adminHomepageContentWriteInput,
) (adminSiteContentWriteResult, error) {
	writer.homepageCalls++
	writer.homepageContext = ctx
	writer.homepageExpectedVersion = expectedVersion
	writer.homepageInput = input

	return writer.homepageResult, writer.homepageError
}

// UpdateContact records one complete optimistic Contact edit.
func (writer *recordingAdminSiteContentWriter) UpdateContact(
	ctx context.Context,
	expectedVersion int64,
	input adminContactContentWriteInput,
) (adminSiteContentWriteResult, error) {
	writer.contactCalls++
	writer.contactContext = ctx
	writer.contactExpectedVersion = expectedVersion
	writer.contactInput = input

	return writer.contactResult, writer.contactError
}

// UpsertHomepageHero records one media mutation and copies its mutable bytes
// before retaining them for assertions.
func (writer *recordingAdminSiteContentWriter) UpsertHomepageHero(
	ctx context.Context,
	expectedVersion int64,
	input adminHomepageHeroWriteInput,
) (adminHomepageHeroWriteResult, error) {
	writer.heroCalls++
	writer.heroContext = ctx
	writer.heroExpectedVersion = expectedVersion
	input.Content = append([]byte(nil), input.Content...)
	writer.heroInput = input

	return writer.heroResult, writer.heroError
}
