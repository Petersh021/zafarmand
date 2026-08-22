package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// TestPostgresAdminSiteContentPersistenceIntegration proves behavior scanner
// stubs cannot: seeded singleton reads, all three cover-backed candidate slots,
// atomic eligibility checks, independent Contact revisions, stale rejection,
// managed-hero parent/media revisions, replacement privacy, and preservation of
// a stored selection that later becomes unavailable.
//
// The shared explicit opt-in and `_test` database-name guard make ordinary test
// runs skip this function without reading the development DATABASE_URL.
func TestPostgresAdminSiteContentPersistenceIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin site-content integration database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin site-content integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})
	applyRepositoryIntegrationMigrations(t, database)

	reader, err := newPostgresAdminSiteContentReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin site-content reader: %v", err)
	}
	writer, err := newPostgresAdminSiteContentWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin site-content writer: %v", err)
	}
	productWriter, err := newPostgresAdminProductWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL Product writer: %v", err)
	}
	interiorWriter, err := newPostgresAdminInteriorProjectWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL Interior writer: %v", err)
	}
	architectureWriter, err := newPostgresAdminArchitectureProjectWriter(database)
	if err != nil {
		t.Fatalf("create PostgreSQL Architecture writer: %v", err)
	}

	homepage, err := reader.ReadHomepage(t.Context())
	if err != nil || homepage.Version != 1 || homepage.ManagedHeroEnabled ||
		homepage.Hero != nil || homepage.FeaturedInterior != nil ||
		homepage.FeaturedArchitecture != nil || homepage.FeaturedProduct != nil {
		t.Fatalf("seeded protected Homepage: record=%#v err=%v", homepage, err)
	}
	contact, err := reader.ReadContact(t.Context())
	if err != nil || contact.Version != 1 || contact.ContactEmail != "" ||
		contact.PhoneDisplay != "" || contact.PhoneE164 != "" ||
		contact.Address != "" {
		t.Fatalf("seeded protected Contact: record=%#v err=%v", contact, err)
	}

	// Contact changes use their own revision and cannot make a Homepage form
	// stale. A repeated old Contact revision is rejected without changing data.
	contactInput := validAdminContactContentWriteInput()
	contactResult, err := writer.UpdateContact(
		t.Context(),
		contact.Version,
		contactInput,
	)
	if err != nil || contactResult.Version != 2 {
		t.Fatalf("update live Contact: result=%#v err=%v", contactResult, err)
	}
	if _, err := writer.UpdateContact(
		t.Context(),
		contact.Version,
		contactInput,
	); !errors.Is(err, errAdminSiteContentWriteConflict) {
		t.Errorf("stale Contact update: got %v, want conflict", err)
	}
	storedContact, err := reader.ReadContact(t.Context())
	if err != nil || storedContact.ContactEmail != contactInput.ContactEmail ||
		storedContact.PhoneE164 != contactInput.PhoneE164 ||
		storedContact.Version != contactResult.Version {
		t.Errorf("stored Contact: record=%#v err=%v", storedContact, err)
	}

	// Each candidate must be Published and own a current reviewed cover. The
	// discipline writers establish those facts through their ordinary contracts.
	candidateCoverContent, candidateCoverInspection, err := normalizeReviewedCover(
		testAdminHomepageHeroPNG(t),
	)
	if err != nil {
		t.Fatalf("normalize Stage 24 candidate cover: %v", err)
	}
	productInput := adminProductWriteInput{
		Slug:              "stage-24-feature-product",
		Name:              "Stage 24 Feature Product",
		Category:          "Furniture",
		SortOrder:         1,
		PublicationStatus: publishedProductStatus,
		Description:       "A fictional Product candidate for managed Homepage content.",
		Material:          "Fictional reviewed material",
		Dimensions:        "40 × 40 × 70 cm",
	}
	product, err := productWriter.Create(t.Context(), productInput)
	if err != nil {
		t.Fatalf("create live Product candidate: %v", err)
	}
	productCover, err := productWriter.UpsertCover(
		t.Context(),
		product.ID,
		product.Version,
		adminProductCoverWriteInput{
			ContentType: candidateCoverInspection.ContentType,
			Content:     append([]byte(nil), candidateCoverContent...),
			ByteSize:    len(candidateCoverContent),
			Width:       candidateCoverInspection.Width,
			Height:      candidateCoverInspection.Height,
			SHA256:      candidateCoverInspection.SHA256,
			AltText:     "A fictional Product candidate for the Homepage",
			Caption:     "Stage 24 candidate fixture.",
		},
	)
	if err != nil {
		t.Fatalf("store live Product candidate cover: %v", err)
	}

	interiorInput := adminInteriorProjectWriteInput{
		Slug:              "stage-24-feature-interior",
		Title:             "Stage 24 Feature Interior",
		Typology:          "Residential",
		Location:          "Fictional location",
		ProjectYear:       2026,
		ProjectStatus:     "Completed",
		Description:       "A fictional Interior candidate for managed Homepage content.",
		SortOrder:         1,
		PublicationStatus: publishedInteriorProjectStatus,
	}
	interior, err := interiorWriter.Create(t.Context(), interiorInput)
	if err != nil {
		t.Fatalf("create live Interior candidate: %v", err)
	}
	interiorCover, err := interiorWriter.UpsertCover(
		t.Context(),
		interior.ID,
		interior.Version,
		adminInteriorProjectCoverWriteInput{
			ContentType: candidateCoverInspection.ContentType,
			Content:     append([]byte(nil), candidateCoverContent...),
			ByteSize:    len(candidateCoverContent),
			Width:       candidateCoverInspection.Width,
			Height:      candidateCoverInspection.Height,
			SHA256:      candidateCoverInspection.SHA256,
			AltText:     "A fictional Interior candidate for the Homepage",
			Caption:     "Stage 24 candidate fixture.",
		},
	)
	if err != nil {
		t.Fatalf("store live Interior candidate cover: %v", err)
	}

	architectureInput := adminArchitectureProjectWriteInput{
		Slug:              "stage-24-feature-architecture",
		Title:             "Stage 24 Feature Architecture",
		Typology:          "Residential",
		Location:          "Fictional location",
		ProjectYear:       2026,
		ProjectStatus:     "Completed",
		Description:       "A fictional Architecture candidate for managed Homepage content.",
		SortOrder:         1,
		PublicationStatus: publishedArchitectureProjectStatus,
	}
	architecture, err := architectureWriter.Create(t.Context(), architectureInput)
	if err != nil {
		t.Fatalf("create live Architecture candidate: %v", err)
	}
	architectureCover, err := architectureWriter.UpsertCover(
		t.Context(),
		architecture.ID,
		architecture.Version,
		adminArchitectureProjectCoverWriteInput{
			ContentType: candidateCoverInspection.ContentType,
			Content:     append([]byte(nil), candidateCoverContent...),
			ByteSize:    len(candidateCoverContent),
			Width:       candidateCoverInspection.Width,
			Height:      candidateCoverInspection.Height,
			SHA256:      candidateCoverInspection.SHA256,
			AltText:     "A fictional Architecture candidate for the Homepage",
			Caption:     "Stage 24 candidate fixture.",
		},
	)
	if err != nil {
		t.Fatalf("store live Architecture candidate cover: %v", err)
	}

	candidates, err := reader.ListFeatureCandidates(t.Context())
	if err != nil || len(candidates) != 3 ||
		candidates[0].Discipline != homepageFeatureInterior ||
		candidates[0].ID != interior.ID ||
		candidates[1].Discipline != homepageFeatureArchitecture ||
		candidates[1].ID != architecture.ID ||
		candidates[2].Discipline != homepageFeatureProduct ||
		candidates[2].ID != product.ID {
		t.Fatalf("live feature candidates: candidates=%#v err=%v", candidates, err)
	}

	homepageInput := validAdminHomepageContentWriteInput()
	homepageInput.ManagedHeroEnabled = false
	homepageInput.FeaturedInteriorProjectID = interior.ID
	homepageInput.FeaturedArchitectureProjectID = architecture.ID
	homepageInput.FeaturedProductID = product.ID
	homepageResult, err := writer.UpdateHomepage(
		t.Context(),
		homepage.Version,
		homepageInput,
	)
	if err != nil || homepageResult.Version != 2 {
		t.Fatalf("update live Homepage features: result=%#v err=%v", homepageResult, err)
	}
	storedHomepage, err := reader.ReadHomepage(t.Context())
	if err != nil || storedHomepage.FeaturedInterior == nil ||
		!storedHomepage.FeaturedInterior.Eligible ||
		storedHomepage.FeaturedArchitecture == nil ||
		!storedHomepage.FeaturedArchitecture.Eligible ||
		storedHomepage.FeaturedProduct == nil ||
		!storedHomepage.FeaturedProduct.Eligible {
		t.Fatalf("stored live Homepage features: record=%#v err=%v", storedHomepage, err)
	}

	// Lifecycle changes do not erase the fixed FK. Protected reads retain the
	// now-unavailable Product so an editor can clear it, while both candidate and
	// writer eligibility checks reject it for a new revision.
	productInput.PublicationStatus = productPublicationStatusArchived
	if _, err := productWriter.Update(
		t.Context(),
		product.ID,
		productCover.ProductVersion,
		productInput,
	); err != nil {
		t.Fatalf("archive selected Product: %v", err)
	}
	storedHomepage, err = reader.ReadHomepage(t.Context())
	if err != nil || storedHomepage.FeaturedProduct == nil ||
		storedHomepage.FeaturedProduct.Eligible ||
		storedHomepage.FeaturedProduct.PublicationStatus != productPublicationStatusArchived {
		t.Fatalf("unavailable stored Product selection: record=%#v err=%v", storedHomepage, err)
	}
	candidates, err = reader.ListFeatureCandidates(t.Context())
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates after Product archive: candidates=%#v err=%v", candidates, err)
	}
	if _, err := writer.UpdateHomepage(
		t.Context(),
		homepageResult.Version,
		homepageInput,
	); !errors.Is(err, errAdminHomepageProductFeatureUnavailable) {
		t.Errorf("reuse unavailable Product: got %v, want Product unavailable", err)
	}

	// A normalized hero upload advances the parent and media revisions together,
	// enables managed mode, and makes only the current exact revision readable.
	heroInput := validAdminHomepageHeroWriteInput(t)
	heroResult, err := writer.UpsertHomepageHero(
		t.Context(),
		homepageResult.Version,
		heroInput,
	)
	if err != nil || heroResult != (adminHomepageHeroWriteResult{
		HomepageVersion: 3,
		HeroVersion:     1,
	}) {
		t.Fatalf("store live Homepage hero: result=%#v err=%v", heroResult, err)
	}
	storedHomepage, err = reader.ReadHomepage(t.Context())
	if err != nil || !storedHomepage.ManagedHeroEnabled ||
		storedHomepage.Hero == nil ||
		storedHomepage.Hero.Version != heroResult.HeroVersion ||
		storedHomepage.Version != heroResult.HomepageVersion {
		t.Fatalf("Homepage after hero upload: record=%#v err=%v", storedHomepage, err)
	}
	asset, err := reader.FindHomepageHero(t.Context(), heroResult.HeroVersion)
	if err != nil || !bytes.Equal(asset.Content, heroInput.Content) ||
		asset.AltText != heroInput.AltText {
		t.Fatalf("protected live hero: asset=%#v err=%v", asset, err)
	}

	replacement := heroInput
	replacement.AltText = "A replacement fictional managed Homepage hero"
	replaced, err := writer.UpsertHomepageHero(
		t.Context(),
		heroResult.HomepageVersion,
		replacement,
	)
	if err != nil || replaced != (adminHomepageHeroWriteResult{
		HomepageVersion: 4,
		HeroVersion:     2,
	}) {
		t.Fatalf("replace live Homepage hero: result=%#v err=%v", replaced, err)
	}
	if _, err := reader.FindHomepageHero(
		t.Context(),
		heroResult.HeroVersion,
	); !errors.Is(err, errAdminHomepageHeroNotFound) {
		t.Errorf("old protected hero revision: got %v, want not found", err)
	}
	if _, err := writer.UpsertHomepageHero(
		t.Context(),
		heroResult.HomepageVersion,
		heroInput,
	); !errors.Is(err, errAdminSiteContentWriteConflict) {
		t.Errorf("stale hero replacement: got %v, want conflict", err)
	}

	// Clearing the unavailable Product and choosing fallback is one valid later
	// Homepage revision; the managed hero remains stored for a future selection.
	homepageInput.ManagedHeroEnabled = false
	homepageInput.FeaturedProductID = 0
	cleared, err := writer.UpdateHomepage(
		t.Context(),
		replaced.HomepageVersion,
		homepageInput,
	)
	if err != nil || cleared.Version != 5 {
		t.Fatalf("clear unavailable Product: result=%#v err=%v", cleared, err)
	}
	storedHomepage, err = reader.ReadHomepage(t.Context())
	if err != nil || storedHomepage.ManagedHeroEnabled ||
		storedHomepage.FeaturedProduct != nil || storedHomepage.Hero == nil ||
		storedHomepage.Hero.Version != replaced.HeroVersion {
		t.Fatalf("final protected Homepage: record=%#v err=%v", storedHomepage, err)
	}

	// A content edit and hero replacement begin with the same parent revision.
	// Parent-first locking must let exactly one commit and turn the waiter into
	// an optimistic conflict instead of exposing a PostgreSQL deadlock.
	homepageInput.ManagedHeroEnabled = true
	concurrentReplacement := heroInput
	concurrentReplacement.AltText = "A concurrently replaced fictional Homepage hero"
	type concurrentHomepageWriteOutcome struct {
		operation      string
		homepageResult adminSiteContentWriteResult
		heroResult     adminHomepageHeroWriteResult
		err            error
	}

	concurrencyContext, cancelConcurrency := context.WithTimeout(
		t.Context(),
		5*time.Second,
	)
	defer cancelConcurrency()
	startConcurrentWrites := make(chan struct{})
	concurrentOutcomes := make(chan concurrentHomepageWriteOutcome, 2)
	go func() {
		<-startConcurrentWrites
		result, updateErr := writer.UpdateHomepage(
			concurrencyContext,
			cleared.Version,
			homepageInput,
		)
		concurrentOutcomes <- concurrentHomepageWriteOutcome{
			operation:      "content edit",
			homepageResult: result,
			err:            updateErr,
		}
	}()
	go func() {
		<-startConcurrentWrites
		result, updateErr := writer.UpsertHomepageHero(
			concurrencyContext,
			cleared.Version,
			concurrentReplacement,
		)
		concurrentOutcomes <- concurrentHomepageWriteOutcome{
			operation:  "hero replacement",
			heroResult: result,
			err:        updateErr,
		}
	}()
	close(startConcurrentWrites)

	concurrentResults := make([]concurrentHomepageWriteOutcome, 0, 2)
	for len(concurrentResults) < 2 {
		select {
		case outcome := <-concurrentOutcomes:
			concurrentResults = append(concurrentResults, outcome)
		case <-concurrencyContext.Done():
			t.Fatalf(
				"concurrent Homepage writes exceeded bounded context: %v",
				concurrencyContext.Err(),
			)
		}
	}

	successCount := 0
	conflictCount := 0
	winner := ""
	var winningHeroResult adminHomepageHeroWriteResult
	for _, outcome := range concurrentResults {
		switch {
		case outcome.err == nil:
			successCount++
			winner = outcome.operation
			winningHeroResult = outcome.heroResult
			if outcome.operation == "content edit" &&
				outcome.homepageResult.Version != cleared.Version+1 {
				t.Errorf(
					"concurrent content winner version: got %d, want %d",
					outcome.homepageResult.Version,
					cleared.Version+1,
				)
			}
			if outcome.operation == "hero replacement" &&
				outcome.heroResult.HomepageVersion != cleared.Version+1 {
				t.Errorf(
					"concurrent hero winner parent version: got %d, want %d",
					outcome.heroResult.HomepageVersion,
					cleared.Version+1,
				)
			}
		case errors.Is(outcome.err, errAdminSiteContentWriteConflict):
			conflictCount++
		default:
			t.Errorf(
				"concurrent %s: got %v, want success or optimistic conflict",
				outcome.operation,
				outcome.err,
			)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf(
			"concurrent Homepage outcomes: results=%#v successes=%d conflicts=%d",
			concurrentResults,
			successCount,
			conflictCount,
		)
	}

	storedHomepage, err = reader.ReadHomepage(t.Context())
	if err != nil || storedHomepage.Version != cleared.Version+1 ||
		!storedHomepage.ManagedHeroEnabled || storedHomepage.Hero == nil {
		t.Fatalf(
			"Homepage after concurrent writes: record=%#v err=%v",
			storedHomepage,
			err,
		)
	}
	if winner == "hero replacement" {
		if winningHeroResult.HeroVersion != replaced.HeroVersion+1 ||
			storedHomepage.Hero.Version != winningHeroResult.HeroVersion ||
			storedHomepage.Hero.AltText != concurrentReplacement.AltText {
			t.Errorf(
				"hero-replacement winner revisions: result=%#v hero=%#v",
				winningHeroResult,
				storedHomepage.Hero,
			)
		}
	} else if storedHomepage.Hero.Version != replaced.HeroVersion ||
		storedHomepage.Hero.AltText != replacement.AltText {
		t.Errorf(
			"content-edit winner preserved hero: hero=%#v want version=%d alt=%q",
			storedHomepage.Hero,
			replaced.HeroVersion,
			replacement.AltText,
		)
	}

	// Cover result variables are intentionally asserted so the three discipline
	// fixtures all exercised their optimistic media contracts successfully.
	if interiorCover.ProjectVersion <= interior.Version ||
		architectureCover.ProjectVersion <= architecture.Version {
		t.Error("discipline cover fixtures did not advance parent revisions")
	}
}
