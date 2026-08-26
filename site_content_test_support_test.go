package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"
)

// recordingSiteContentReader is the database-free Stage 24 public dependency
// shared by constructor, Homepage, Contact, and hero HTTP tests.
type recordingSiteContentReader struct {
	// mu protects configurable results and recorded calls.
	mu sync.Mutex
	// homepage is copied on assignment and return so tests cannot share mutable
	// feature slices or cover pointers accidentally.
	homepage publicHomepageContent
	// contact contains the configured immutable Contact projection.
	contact publicContactContent
	// hero is copied on assignment and return to isolate encoded bytes.
	hero homepageHeroAsset
	// heroMetadata is the independently configurable binary-free first phase.
	heroMetadata reviewedCoverAssetMetadata
	// Independent errors let one test fail exactly one public operation.
	homepageErr error
	contactErr  error
	heroErr     error
	// heroMetadataErr is the configured first-phase failure.
	heroMetadataErr error
	// Calls retain only operation coordinates and deadline presence, never
	// managed content or media bytes.
	calls []recordingSiteContentCall
}

// recordingSiteContentCall identifies one public dependency invocation.
type recordingSiteContentCall struct {
	// Operation is homepage, contact, hero-metadata, or hero.
	Operation string
	// Version is non-zero only for an exact hero lookup.
	Version int64
	// HasDeadline proves the HTTP layer bounded its database work.
	HasDeadline bool
}

// newRecordingSiteContentReader returns the migration-seeded public defaults.
// It intentionally selects no features and no managed hero.
func newRecordingSiteContentReader() *recordingSiteContentReader {
	return &recordingSiteContentReader{
		homepage: publicHomepageContent{
			StudioName:     "Zafarmand",
			Descriptor:     "Design Studio",
			SEOTitle:       "Home | Zafarmand",
			SEODescription: "Zafarmand design studio",
			Features:       make([]publicHomepageFeature, 0),
		},
		contact: publicContactContent{
			Eyebrow:        "Contact",
			Heading:        "Begin a conversation",
			Introduction:   "Choose a discipline and share the context Zafarmand should review.",
			SEOTitle:       "Contact | Zafarmand",
			SEODescription: "Zafarmand design studio",
		},
	}
}

// ReadHomepage implements siteContentReader and records the caller's deadline.
func (reader *recordingSiteContentReader) ReadHomepage(
	ctx context.Context,
) (publicHomepageContent, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.calls = append(reader.calls, recordingSiteContentCall{
		Operation:   "homepage",
		HasDeadline: hasDeadline,
	})

	return clonePublicHomepageContent(reader.homepage), reader.homepageErr
}

// ReadContact implements siteContentReader and records the caller's deadline.
func (reader *recordingSiteContentReader) ReadContact(
	ctx context.Context,
) (publicContactContent, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.calls = append(reader.calls, recordingSiteContentCall{
		Operation:   "contact",
		HasDeadline: hasDeadline,
	})

	return reader.contact, reader.contactErr
}

// FindHomepageHeroMetadata records the binary-free first phase and returns
// configured current response facts without cloning the encoded hero bytes.
func (reader *recordingSiteContentReader) FindHomepageHeroMetadata(
	ctx context.Context,
	version int64,
) (reviewedCoverAssetMetadata, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.calls = append(reader.calls, recordingSiteContentCall{
		Operation:   "hero-metadata",
		Version:     version,
		HasDeadline: hasDeadline,
	})
	if reader.heroMetadataErr != nil {
		return reviewedCoverAssetMetadata{}, reader.heroMetadataErr
	}
	if reader.heroMetadata.Version != version {
		return reviewedCoverAssetMetadata{}, errHomepageHeroNotFound
	}

	return reader.heroMetadata, nil
}

// FindHomepageHero implements the exact public media read without PostgreSQL.
func (reader *recordingSiteContentReader) FindHomepageHero(
	ctx context.Context,
	version int64,
) (homepageHeroAsset, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	reader.calls = append(reader.calls, recordingSiteContentCall{
		Operation:   "hero",
		Version:     version,
		HasDeadline: hasDeadline,
	})
	if reader.heroErr != nil {
		return homepageHeroAsset{}, reader.heroErr
	}
	if reader.hero.Version != version {
		return homepageHeroAsset{}, errHomepageHeroNotFound
	}

	return cloneHomepageHeroAsset(reader.hero), nil
}

// setHomepage replaces the configured Homepage projection with an isolated copy.
func (reader *recordingSiteContentReader) setHomepage(
	content publicHomepageContent,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.homepage = clonePublicHomepageContent(content)
	reader.homepageErr = err
}

// setContact replaces the configured Contact projection and error.
func (reader *recordingSiteContentReader) setContact(
	content publicContactContent,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.contact = content
	reader.contactErr = err
}

// setHero replaces the configured exact hero asset and error with isolated bytes.
func (reader *recordingSiteContentReader) setHero(
	asset homepageHeroAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.hero = cloneHomepageHeroAsset(asset)
	reader.heroMetadata = asset.responseMetadata()
	reader.heroMetadataErr = err
	reader.heroErr = err
}

// setHeroContent configures only the second-phase hero result while retaining
// the metadata established by setHero.
func (reader *recordingSiteContentReader) setHeroContent(
	asset homepageHeroAsset,
	err error,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.hero = cloneHomepageHeroAsset(asset)
	reader.heroErr = err
}

// callSnapshot returns a separate call slice for race-free assertions.
func (reader *recordingSiteContentReader) callSnapshot() []recordingSiteContentCall {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]recordingSiteContentCall(nil), reader.calls...)
}

// clonePublicHomepageContent deep-copies the slice and optional image pointers.
func clonePublicHomepageContent(
	content publicHomepageContent,
) publicHomepageContent {
	if content.Hero != nil {
		hero := *content.Hero
		content.Hero = &hero
	}
	features := make([]publicHomepageFeature, len(content.Features))
	for index, feature := range content.Features {
		features[index] = feature
		if feature.Cover != nil {
			cover := *feature.Cover
			features[index].Cover = &cover
		}
	}
	content.Features = features[:len(features):len(features)]

	return content
}

// validTestHomepageHeroAsset creates one normalized PNG whose bytes, digest,
// dimensions, and timestamps satisfy the production media validator.
func validTestHomepageHeroAsset(
	t *testing.T,
	version int64,
) homepageHeroAsset {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 3, 2))
	canvas.Set(0, 0, color.RGBA{R: 120, G: 84, B: 55, A: 255})
	canvas.Set(1, 0, color.RGBA{R: 210, G: 196, B: 170, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode Homepage hero fixture: %v", err)
	}
	normalized, inspection, err := normalizeReviewedCover(encoded.Bytes())
	if err != nil {
		t.Fatalf("normalize Homepage hero fixture: %v", err)
	}
	timestamp := time.Date(2026, time.August, 15, 8, 0, 0, 0, time.UTC)
	asset := homepageHeroAsset{
		Version:     version,
		ContentType: inspection.ContentType,
		Content:     normalized,
		ByteSize:    len(normalized),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "Warm material study in the managed Homepage hero",
		CreatedAt:   timestamp,
		UpdatedAt:   timestamp,
	}
	if !isValidHomepageHeroAsset(asset) {
		t.Fatal("constructed Homepage hero fixture is invalid")
	}

	return asset
}
