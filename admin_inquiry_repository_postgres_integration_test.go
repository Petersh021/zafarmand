package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// TestPostgresAdminInquiryReaderIntegration exercises behavior that iterator
// stubs cannot prove: PostgreSQL's descending primary-key result order,
// byte-independent projection, keyset boundaries, timestamp decoding, and
// sql.ErrNoRows mapping. It does not assert a particular query plan.
//
// The test uses the existing two-part destructive opt-in and `_test` database
// guard. Ordinary go test runs skip it, and it never falls back to DATABASE_URL.
func TestPostgresAdminInquiryReaderIntegration(t *testing.T) {
	config := loadMigrationIntegrationConfig(t)

	database, err := openPostgresDatabase(t.Context(), config)
	if err != nil {
		t.Fatalf("open confirmed admin inquiry integration database: %v", err)
	}
	// Cleanup functions run last-in, first-out. Register pool closure before
	// schema cleanup so the latter can still use the live connection.
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error("close admin inquiry integration database")
		}
	})

	requireMigrationIntegrationSchemaEmpty(t, database)
	t.Cleanup(func() {
		cleanupMigrationIntegrationSchema(t, database)
	})

	// Stage 16 is deliberately read-only and adds no migration. Applying the
	// existing v1-v3 catalog proves the reader works against the committed schema.
	applyInquiryIntegrationMigrations(t, database)

	reader, err := newPostgresAdminInquiryReader(database)
	if err != nil {
		t.Fatalf("create PostgreSQL admin inquiry reader: %v", err)
	}

	fixtures := make([]adminInquiryDetail, 0, adminInquiryPageSize+3)
	for index := 0; index < adminInquiryPageSize+3; index++ {
		fixtures = append(
			fixtures,
			insertPostgresAdminInquiryFixture(t, database, index),
		)
	}

	newestPage, err := reader.List(t.Context(), 0)
	if err != nil {
		t.Fatalf("list newest PostgreSQL admin inquiries: %v", err)
	}
	if len(newestPage.Items) != adminInquiryPageSize || !newestPage.HasMore {
		t.Fatalf(
			"newest page: got items=%d more=%t, want %d/true",
			len(newestPage.Items),
			newestPage.HasMore,
			adminInquiryPageSize,
		)
	}
	// Twenty visible records from twenty-three ascending inserts cover fixture
	// indexes 22 down through 3; index 3 is therefore the next exclusive cursor.
	expectedCursor := fixtures[3].ID
	if newestPage.NextBeforeID != expectedCursor {
		t.Errorf(
			"newest cursor: got %d, want %d",
			newestPage.NextBeforeID,
			expectedCursor,
		)
	}
	for pageIndex, summary := range newestPage.Items {
		fixtureIndex := len(fixtures) - 1 - pageIndex
		assertPostgresAdminInquirySummary(
			t,
			summary,
			fixtures[fixtureIndex],
		)
	}

	oldestPage, err := reader.List(t.Context(), newestPage.NextBeforeID)
	if err != nil {
		t.Fatalf("list older PostgreSQL admin inquiries: %v", err)
	}
	if len(oldestPage.Items) != 3 || oldestPage.HasMore ||
		oldestPage.NextBeforeID != 0 {
		t.Fatalf(
			"oldest page: got items=%d more=%t cursor=%d, want 3/false/0",
			len(oldestPage.Items),
			oldestPage.HasMore,
			oldestPage.NextBeforeID,
		)
	}
	for pageIndex, summary := range oldestPage.Items {
		fixtureIndex := 2 - pageIndex
		assertPostgresAdminInquirySummary(
			t,
			summary,
			fixtures[fixtureIndex],
		)
	}

	expectedDetail := fixtures[adminInquiryPageSize/2]
	actualDetail, err := reader.FindByID(t.Context(), expectedDetail.ID)
	if err != nil {
		t.Fatalf("find PostgreSQL admin inquiry detail: %v", err)
	}
	if actualDetail != expectedDetail {
		t.Errorf("detail: got %#v, want %#v", actualDetail, expectedDetail)
	}

	missingID := fixtures[len(fixtures)-1].ID + 1000
	missingDetail, err := reader.FindByID(t.Context(), missingID)
	if !errors.Is(err, errAdminInquiryNotFound) {
		t.Fatalf("missing detail error: got %v, want not-found sentinel", err)
	}
	if missingDetail != (adminInquiryDetail{}) {
		t.Errorf("missing read returned partial detail: %#v", missingDetail)
	}
}

// insertPostgresAdminInquiryFixture writes one synthetic, constraint-valid row
// directly so the integration test focuses on the reader rather than repeating
// the already-covered public write repository behavior.
func insertPostgresAdminInquiryFixture(
	t *testing.T,
	database *sql.DB,
	index int,
) adminInquiryDetail {
	t.Helper()

	// Repeating a distinct non-zero byte produces one readable, fixed-width key
	// per fixture without treating any real browser token as test data.
	submissionKey := bytes.Repeat([]byte{byte(index + 1)}, 32)
	disciplines := [...]string{
		"interior-design",
		"architecture-design",
		"products",
	}
	statuses := [...]inquiryStatus{
		inquiryStatusNew,
		inquiryStatusReviewed,
		inquiryStatusArchived,
	}

	name := fmt.Sprintf("Stage Sixteen Visitor %02d", index+1)
	email := fmt.Sprintf("stage16.visitor.%02d@example.test", index+1)
	discipline := disciplines[index%len(disciplines)]
	message := fmt.Sprintf(
		"Synthetic Stage 16 inquiry number %02d for live read verification.",
		index+1,
	)
	status := statuses[index%len(statuses)]

	var detail adminInquiryDetail
	var storedStatus string
	if err := database.QueryRowContext(
		t.Context(),
		`INSERT INTO public.inquiries (
    submission_key,
    name,
    email,
    discipline,
    message,
    status
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
    id,
    name,
    email,
    discipline,
    message,
    status,
    created_at,
    updated_at`,
		submissionKey,
		name,
		email,
		discipline,
		message,
		string(status),
	).Scan(
		&detail.ID,
		&detail.Name,
		&detail.Email,
		&detail.Discipline,
		&detail.Message,
		&storedStatus,
		&detail.CreatedAt,
		&detail.UpdatedAt,
	); err != nil {
		t.Fatal("insert synthetic PostgreSQL admin inquiry fixture")
	}
	detail.Status = inquiryStatus(storedStatus)
	if !isValidStoredAdminInquiryDetail(detail) {
		t.Fatal("PostgreSQL returned an invalid synthetic inquiry fixture")
	}

	return detail
}

// assertPostgresAdminInquirySummary compares every projected list value with
// its full synthetic fixture and thereby proves email and message are not needed
// to reconstruct the queue row.
func assertPostgresAdminInquirySummary(
	t *testing.T,
	actual adminInquirySummary,
	expected adminInquiryDetail,
) {
	t.Helper()

	want := adminInquirySummary{
		ID:         expected.ID,
		Name:       expected.Name,
		Discipline: expected.Discipline,
		Status:     expected.Status,
		CreatedAt:  expected.CreatedAt,
	}
	if actual != want {
		t.Errorf("summary: got %#v, want %#v", actual, want)
	}
}
