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

// adminInquiryReaderContextKey is a private comparable marker used to prove
// that both read methods forward the caller's exact request context.
type adminInquiryReaderContextKey struct{}

// adminInquiryQueryStub records one list invocation and returns controlled rows
// or a database-like failure without requiring a third-party SQL mock.
type adminInquiryQueryStub struct {
	// rows is returned when queryError is nil.
	rows adminInquiryRows
	// queryError simulates a database/sql or driver failure.
	queryError error
	// calls records how many list statements were attempted.
	calls int
	// context records the exact context supplied to QueryContext.
	context context.Context
	// query records the complete trusted SQL statement.
	query string
	// arguments records the positional cursor and limit values.
	arguments []any
}

// Query matches adminInquiryQuery and records its complete invocation before
// returning the configured rows or error.
func (stub *adminInquiryQueryStub) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (adminInquiryRows, error) {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.rows, stub.queryError
}

// adminInquiryRowsStub presents an ordered summary slice through the narrow
// iterator used by List and can independently fail scanning, iteration, or
// closure.
type adminInquiryRowsStub struct {
	// summaries are returned one at a time in their supplied order.
	summaries []adminInquirySummary
	// scanErrorAt is the zero-based row whose Scan should fail; -1 disables it.
	scanErrorAt int
	// scanError is the controlled failure returned at scanErrorAt.
	scanError error
	// iterationError simulates a failure reported after iteration stops.
	iterationError error
	// closeError simulates a result-finalization failure.
	closeError error
	// nextIndex identifies the next record that Next may expose.
	nextIndex int
	// currentIndex identifies the record currently available to Scan.
	currentIndex int
	// scanCalls proves rows are inspected only while Next returns true.
	scanCalls int
	// closeCalls proves every acquired result is closed.
	closeCalls int
}

// newAdminInquiryRowsStub initializes the disabled scan-failure index explicitly
// because its useful sentinel is -1 rather than the int zero value.
func newAdminInquiryRowsStub(
	summaries []adminInquirySummary,
) *adminInquiryRowsStub {
	return &adminInquiryRowsStub{
		summaries:    summaries,
		scanErrorAt:  -1,
		currentIndex: -1,
	}
}

// Next advances to one configured summary until the finite slice is exhausted.
func (stub *adminInquiryRowsStub) Next() bool {
	if stub.nextIndex >= len(stub.summaries) {
		return false
	}

	stub.currentIndex = stub.nextIndex
	stub.nextIndex++

	return true
}

// Scan copies the current summary into the five exact destinations expected by
// the production list statement.
func (stub *adminInquiryRowsStub) Scan(destinations ...any) error {
	stub.scanCalls++
	if stub.currentIndex < 0 || stub.currentIndex >= len(stub.summaries) {
		return errors.New("admin inquiry list scan has no current row")
	}
	if stub.currentIndex == stub.scanErrorAt {
		return stub.scanError
	}
	if len(destinations) != 5 {
		return errors.New("admin inquiry list scan expected five destinations")
	}

	summary := stub.summaries[stub.currentIndex]
	id, idOK := destinations[0].(*int64)
	name, nameOK := destinations[1].(*string)
	discipline, disciplineOK := destinations[2].(*string)
	status, statusOK := destinations[3].(*string)
	createdAt, createdAtOK := destinations[4].(*time.Time)
	if !idOK || !nameOK || !disciplineOK || !statusOK || !createdAtOK {
		return errors.New("admin inquiry list scan received unexpected destinations")
	}

	*id = summary.ID
	*name = summary.Name
	*discipline = summary.Discipline
	*status = string(summary.Status)
	*createdAt = summary.CreatedAt

	return nil
}

// Err returns the controlled post-iteration error.
func (stub *adminInquiryRowsStub) Err() error {
	return stub.iterationError
}

// Close records finalization and returns the controlled close result.
func (stub *adminInquiryRowsStub) Close() error {
	stub.closeCalls++

	return stub.closeError
}

// adminInquiryQueryRowStub records one detail invocation and returns a
// controlled scanner.
type adminInquiryQueryRowStub struct {
	// row is returned for the repository's detail Scan call.
	row adminInquiryRowScanner
	// calls records how many exact-ID statements were attempted.
	calls int
	// context records the exact context supplied to QueryRowContext.
	context context.Context
	// query records the complete trusted SQL statement.
	query string
	// arguments records the positional inquiry ID.
	arguments []any
}

// QueryRow matches adminInquiryQueryRow and records the complete invocation.
func (stub *adminInquiryQueryRowStub) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) adminInquiryRowScanner {
	stub.calls++
	stub.context = ctx
	stub.query = query
	stub.arguments = append([]any(nil), arguments...)

	return stub.row
}

// adminInquiryDetailRowStub copies one configured detail or returns a
// database-like scan failure.
type adminInquiryDetailRowStub struct {
	// detail contains every successful detail destination value.
	detail adminInquiryDetail
	// scanError simulates no rows, decoding failure, or a driver error.
	scanError error
	// calls proves the returned row is scanned exactly once.
	calls int
}

// Scan implements adminInquiryRowScanner for the eight-column detail query.
func (stub *adminInquiryDetailRowStub) Scan(destinations ...any) error {
	stub.calls++
	if stub.scanError != nil {
		return stub.scanError
	}
	if len(destinations) != 8 {
		return errors.New("admin inquiry detail scan expected eight destinations")
	}

	id, idOK := destinations[0].(*int64)
	name, nameOK := destinations[1].(*string)
	email, emailOK := destinations[2].(*string)
	discipline, disciplineOK := destinations[3].(*string)
	message, messageOK := destinations[4].(*string)
	status, statusOK := destinations[5].(*string)
	createdAt, createdAtOK := destinations[6].(*time.Time)
	updatedAt, updatedAtOK := destinations[7].(*time.Time)
	if !idOK || !nameOK || !emailOK || !disciplineOK || !messageOK ||
		!statusOK || !createdAtOK || !updatedAtOK {
		return errors.New("admin inquiry detail scan received unexpected destinations")
	}

	*id = stub.detail.ID
	*name = stub.detail.Name
	*email = stub.detail.Email
	*discipline = stub.detail.Discipline
	*message = stub.detail.Message
	*status = string(stub.detail.Status)
	*createdAt = stub.detail.CreatedAt
	*updatedAt = stub.detail.UpdatedAt

	return nil
}

// validAdminInquirySummary creates one deterministic, defensively valid list
// record whose ID can be varied by each test.
func validAdminInquirySummary(id int64) adminInquirySummary {
	return adminInquirySummary{
		ID:         id,
		Name:       "Stage Sixteen Visitor",
		Discipline: "architecture-design",
		Status:     inquiryStatusNew,
		CreatedAt:  time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
	}
}

// validAdminInquiryDetail creates one deterministic, defensively valid detail
// record for success and single-field mutation tests.
func validAdminInquiryDetail(id int64) adminInquiryDetail {
	createdAt := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

	return adminInquiryDetail{
		ID:         id,
		Name:       "Stage Sixteen Visitor",
		Email:      "stage16.visitor@example.com",
		Discipline: "architecture-design",
		Message:    "A private inquiry detail used only by repository tests.",
		Status:     inquiryStatusReviewed,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt.Add(time.Minute),
	}
}

// descendingAdminInquirySummaries creates count records beginning at highestID
// and stepping down by one, matching the trusted SQL order.
func descendingAdminInquirySummaries(
	highestID int64,
	count int,
) []adminInquirySummary {
	summaries := make([]adminInquirySummary, 0, count)
	for index := 0; index < count; index++ {
		summaries = append(
			summaries,
			validAdminInquirySummary(highestID-int64(index)),
		)
	}

	return summaries
}

// TestNewPostgresAdminInquiryReader verifies constructor dependency validation
// and confirms that a valid pool is adapted without opening a connection.
func TestNewPostgresAdminInquiryReader(t *testing.T) {
	t.Run("nil database", func(t *testing.T) {
		reader, err := newPostgresAdminInquiryReader(nil)
		if !errors.Is(err, errAdminInquiryReaderDatabaseRequired) {
			t.Fatalf("error: got %v, want database-required sentinel", err)
		}
		if reader != nil {
			t.Errorf("reader: got %#v, want nil", reader)
		}
	})

	t.Run("valid database", func(t *testing.T) {
		// The zero sql.DB is never queried; it is sufficient to prove both adapters
		// are installed without network activity during construction.
		database := new(sql.DB)
		reader, err := newPostgresAdminInquiryReader(database)
		if err != nil {
			t.Fatalf("create admin inquiry reader: %v", err)
		}
		if reader.query == nil || reader.queryRow == nil {
			t.Error("reader did not install both database adapters")
		}
	})
}

// TestPostgresAdminInquiryReaderList verifies first-page and cursor-page SQL,
// positional parameters, context propagation, trimming, and next-cursor logic.
func TestPostgresAdminInquiryReaderList(t *testing.T) {
	const expectedLatestSQL = `SELECT
    id,
    name,
    discipline,
    status,
    created_at
FROM public.inquiries
ORDER BY id DESC
LIMIT $1`
	const expectedBeforeSQL = `SELECT
    id,
    name,
    discipline,
    status,
    created_at
FROM public.inquiries
WHERE id < $1
ORDER BY id DESC
LIMIT $2`

	tests := []struct {
		// name labels the keyset position in verbose output.
		name string
		// beforeID is zero for newest or a positive exclusive bound.
		beforeID int64
		// summaries are the finite rows supplied by the database seam.
		summaries []adminInquirySummary
		// expectedSQL is kept test-owned so changing the production constant alone
		// cannot make the assertion pass.
		expectedSQL string
		// expectedArguments are the exact positional cursor and fetch limit.
		expectedArguments []any
		// expectedItems is the visible row count after trimming the sentinel.
		expectedItems int
		// expectedHasMore reports whether a twenty-first row was supplied.
		expectedHasMore bool
		// expectedCursor is the last visible ID when another page exists.
		expectedCursor int64
	}{
		{
			name:              "newest partial page",
			summaries:         descendingAdminInquirySummaries(9, 2),
			expectedSQL:       expectedLatestSQL,
			expectedArguments: []any{21},
			expectedItems:     2,
		},
		{
			name:              "older full page with sentinel",
			beforeID:          42,
			summaries:         descendingAdminInquirySummaries(41, 21),
			expectedSQL:       expectedBeforeSQL,
			expectedArguments: []any{int64(42), 21},
			expectedItems:     20,
			expectedHasMore:   true,
			expectedCursor:    22,
		},
		{
			name:              "empty newest page",
			expectedSQL:       expectedLatestSQL,
			expectedArguments: []any{21},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := newAdminInquiryRowsStub(test.summaries)
			query := &adminInquiryQueryStub{rows: rows}
			reader := &postgresAdminInquiryReader{query: query.Query}
			ctx := context.WithValue(
				context.Background(),
				adminInquiryReaderContextKey{},
				"stage16-list-context",
			)

			page, err := reader.List(ctx, test.beforeID)
			if err != nil {
				t.Fatalf("list admin inquiries: %v", err)
			}
			if query.calls != 1 || query.context != ctx {
				t.Errorf(
					"query calls/context: got %d/%v, want 1/caller context",
					query.calls,
					query.context,
				)
			}
			if query.query != test.expectedSQL {
				t.Errorf("query:\n%s\nwant:\n%s", query.query, test.expectedSQL)
			}
			if !reflect.DeepEqual(query.arguments, test.expectedArguments) {
				t.Errorf(
					"arguments: got %#v, want %#v",
					query.arguments,
					test.expectedArguments,
				)
			}
			if len(page.Items) != test.expectedItems ||
				page.HasMore != test.expectedHasMore ||
				page.NextBeforeID != test.expectedCursor {
				t.Errorf(
					"page: got items=%d more=%t cursor=%d, want %d/%t/%d",
					len(page.Items),
					page.HasMore,
					page.NextBeforeID,
					test.expectedItems,
					test.expectedHasMore,
					test.expectedCursor,
				)
			}
			if page.HasMore && cap(page.Items) != adminInquiryPageSize {
				t.Errorf(
					"visible page capacity: got %d, want %d so sentinel cannot be resliced",
					cap(page.Items),
					adminInquiryPageSize,
				)
			}
			if rows.closeCalls != 1 {
				t.Errorf("Close calls: got %d, want 1", rows.closeCalls)
			}
			for index, item := range page.Items {
				if item != test.summaries[index] {
					t.Errorf("item %d: got %#v, want %#v", index, item, test.summaries[index])
				}
			}
		})
	}
}

// TestPostgresAdminInquiryReaderListFailures proves invalid input, lower-level
// failures, malformed rows, broken ordering, and overproduction all collapse to
// safe categories without retaining a partial page.
func TestPostgresAdminInquiryReaderListFailures(t *testing.T) {
	const sensitiveDetail = "visitor@example.com password=database-secret"

	valid := validAdminInquirySummary(10)
	scanFailureRows := newAdminInquiryRowsStub([]adminInquirySummary{valid})
	scanFailureRows.scanErrorAt = 0
	scanFailureRows.scanError = errors.New(sensitiveDetail)
	iterationFailureRows := newAdminInquiryRowsStub([]adminInquirySummary{valid})
	iterationFailureRows.iterationError = errors.New(sensitiveDetail)
	closeFailureRows := newAdminInquiryRowsStub([]adminInquirySummary{valid})
	closeFailureRows.closeError = errors.New(sensitiveDetail)
	queryErrorRows := newAdminInquiryRowsStub([]adminInquirySummary{valid})
	invalidRow := valid
	invalidRow.Name = " untrimmed"

	tests := []struct {
		// name labels the rejected boundary.
		name string
		// reader is nil or contains one controlled query seam.
		reader *postgresAdminInquiryReader
		// ctx is nil only for the explicit context validation case.
		ctx context.Context
		// beforeID supplies the cursor under test.
		beforeID int64
		// expectedError distinguishes invalid caller input from safe read failure.
		expectedError error
	}{
		{
			name:          "nil context",
			reader:        &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{}).Query},
			beforeID:      0,
			expectedError: errAdminInquiryInvalidQuery,
		},
		{
			name:          "negative cursor",
			reader:        &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{}).Query},
			ctx:           context.Background(),
			beforeID:      -1,
			expectedError: errAdminInquiryInvalidQuery,
		},
		{
			name:          "nil reader",
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name:          "nil query",
			reader:        &postgresAdminInquiryReader{},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "query failure",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				queryError: errors.New(sensitiveDetail),
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "query failure with defensive rows closure",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows:       queryErrorRows,
				queryError: errors.New(sensitiveDetail),
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "nil rows",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows: nil,
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name:          "scan failure",
			reader:        &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{rows: scanFailureRows}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "invalid stored summary",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows: newAdminInquiryRowsStub([]adminInquirySummary{invalidRow}),
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "cursor boundary violation",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows: newAdminInquiryRowsStub([]adminInquirySummary{validAdminInquirySummary(10)}),
			}).Query},
			ctx:           context.Background(),
			beforeID:      10,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "non-descending rows",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows: newAdminInquiryRowsStub([]adminInquirySummary{
					validAdminInquirySummary(9),
					validAdminInquirySummary(10),
				}),
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "more than fetched bound",
			reader: &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{
				rows: newAdminInquiryRowsStub(descendingAdminInquirySummaries(30, 22)),
			}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name:          "iteration failure",
			reader:        &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{rows: iterationFailureRows}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name:          "close failure",
			reader:        &postgresAdminInquiryReader{query: (&adminInquiryQueryStub{rows: closeFailureRows}).Query},
			ctx:           context.Background(),
			expectedError: errAdminInquiryReadFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, err := test.reader.List(test.ctx, test.beforeID)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if len(page.Items) != 0 || page.HasMore || page.NextBeforeID != 0 {
				t.Errorf("failure returned a partial page: %#v", page)
			}
			if strings.Contains(err.Error(), "visitor@example.com") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive lower-level detail: %q", err)
			}
		})
	}
	if queryErrorRows.closeCalls != 1 {
		t.Errorf(
			"rows returned with query error Close calls: got %d, want 1",
			queryErrorRows.closeCalls,
		)
	}
}

// TestPostgresAdminInquiryReaderFindByID verifies the complete detail query,
// parameter order, context propagation, scan mapping, and returned value.
func TestPostgresAdminInquiryReaderFindByID(t *testing.T) {
	detail := validAdminInquiryDetail(37)
	row := &adminInquiryDetailRowStub{detail: detail}
	queryRow := &adminInquiryQueryRowStub{row: row}
	reader := &postgresAdminInquiryReader{queryRow: queryRow.QueryRow}
	ctx := context.WithValue(
		context.Background(),
		adminInquiryReaderContextKey{},
		"stage16-detail-context",
	)

	actual, err := reader.FindByID(ctx, detail.ID)
	if err != nil {
		t.Fatalf("find admin inquiry: %v", err)
	}
	if actual != detail {
		t.Errorf("detail: got %#v, want %#v", actual, detail)
	}
	if queryRow.calls != 1 || queryRow.context != ctx || row.calls != 1 {
		t.Errorf(
			"query/scan calls or context: got %d/%d/%v",
			queryRow.calls,
			row.calls,
			queryRow.context,
		)
	}
	const expectedSQL = `SELECT
    id,
    name,
    email,
    discipline,
    message,
    status,
    created_at,
    updated_at
FROM public.inquiries
WHERE id = $1`
	if queryRow.query != expectedSQL {
		t.Errorf("query:\n%s\nwant:\n%s", queryRow.query, expectedSQL)
	}
	if !reflect.DeepEqual(queryRow.arguments, []any{int64(37)}) {
		t.Errorf("arguments: got %#v, want [37]", queryRow.arguments)
	}
	if strings.Contains(queryRow.query, "submission_key") {
		t.Error("detail query unexpectedly selects the submission key")
	}
}

// TestPostgresAdminInquiryReaderFindByIDFailures verifies input validation,
// not-found classification, safe failure redaction, and malformed-row defense.
func TestPostgresAdminInquiryReaderFindByIDFailures(t *testing.T) {
	const sensitiveDetail = "visitor@example.com password=database-secret"

	invalidDetail := validAdminInquiryDetail(4)
	invalidDetail.Message = "malformed\x00message"
	mismatchedDetail := validAdminInquiryDetail(5)
	tests := []struct {
		// name labels the rejected detail boundary.
		name string
		// reader supplies the controlled query-row seam.
		reader *postgresAdminInquiryReader
		// ctx is nil only for the explicit context validation case.
		ctx context.Context
		// id is the requested inquiry identity.
		id int64
		// expectedError distinguishes invalid, missing, and failed reads.
		expectedError error
	}{
		{
			name:          "nil context",
			reader:        &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{}).QueryRow},
			id:            1,
			expectedError: errAdminInquiryInvalidQuery,
		},
		{
			name:          "zero ID",
			reader:        &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{}).QueryRow},
			ctx:           context.Background(),
			expectedError: errAdminInquiryInvalidQuery,
		},
		{
			name:          "negative ID",
			reader:        &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{}).QueryRow},
			ctx:           context.Background(),
			id:            -1,
			expectedError: errAdminInquiryInvalidQuery,
		},
		{
			name:          "nil reader",
			ctx:           context.Background(),
			id:            1,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name:          "nil query row",
			reader:        &postgresAdminInquiryReader{},
			ctx:           context.Background(),
			id:            1,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "nil row",
			reader: &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{
				row: nil,
			}).QueryRow},
			ctx:           context.Background(),
			id:            1,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "not found",
			reader: &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{
				row: &adminInquiryDetailRowStub{scanError: sql.ErrNoRows},
			}).QueryRow},
			ctx:           context.Background(),
			id:            1,
			expectedError: errAdminInquiryNotFound,
		},
		{
			name: "scan failure",
			reader: &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{
				row: &adminInquiryDetailRowStub{scanError: errors.New(sensitiveDetail)},
			}).QueryRow},
			ctx:           context.Background(),
			id:            1,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "invalid stored detail",
			reader: &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{
				row: &adminInquiryDetailRowStub{detail: invalidDetail},
			}).QueryRow},
			ctx:           context.Background(),
			id:            invalidDetail.ID,
			expectedError: errAdminInquiryReadFailed,
		},
		{
			name: "mismatched stored identity",
			reader: &postgresAdminInquiryReader{queryRow: (&adminInquiryQueryRowStub{
				row: &adminInquiryDetailRowStub{detail: mismatchedDetail},
			}).QueryRow},
			ctx:           context.Background(),
			id:            mismatchedDetail.ID + 1,
			expectedError: errAdminInquiryReadFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, err := test.reader.FindByID(test.ctx, test.id)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if detail != (adminInquiryDetail{}) {
				t.Errorf("failure returned partial detail: %#v", detail)
			}
			if strings.Contains(err.Error(), "visitor@example.com") ||
				strings.Contains(err.Error(), "database-secret") {
				t.Errorf("error exposes sensitive lower-level detail: %q", err)
			}
		})
	}
}

// TestStoredAdminInquirySummaryValidation exercises every list-row invariant
// independently so a later refactor cannot silently weaken persisted-data
// validation.
func TestStoredAdminInquirySummaryValidation(t *testing.T) {
	valid := validAdminInquirySummary(1)
	invalidUTF8 := string([]byte{0xff})

	tests := []struct {
		// name identifies the rejected persisted-field invariant.
		name string
		// mutate changes exactly one field on an otherwise valid record.
		mutate func(*adminInquirySummary)
	}{
		{name: "non-positive ID", mutate: func(value *adminInquirySummary) { value.ID = 0 }},
		{name: "empty name", mutate: func(value *adminInquirySummary) { value.Name = "" }},
		{name: "invalid UTF-8 name", mutate: func(value *adminInquirySummary) { value.Name = invalidUTF8 }},
		{name: "NUL name", mutate: func(value *adminInquirySummary) { value.Name = "bad\x00name" }},
		{name: "untrimmed name", mutate: func(value *adminInquirySummary) { value.Name = " name" }},
		{name: "long name", mutate: func(value *adminInquirySummary) { value.Name = strings.Repeat("n", inquiryNameMaxLength+1) }},
		{name: "unsupported discipline", mutate: func(value *adminInquirySummary) { value.Discipline = "landscape" }},
		{name: "unsupported status", mutate: func(value *adminInquirySummary) { value.Status = "pending" }},
		{name: "zero creation time", mutate: func(value *adminInquirySummary) { value.CreatedAt = time.Time{} }},
	}

	if !isValidStoredAdminInquirySummary(valid) {
		t.Fatal("known-valid summary was rejected")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if isValidStoredAdminInquirySummary(candidate) {
				t.Errorf("invalid summary was accepted: %#v", candidate)
			}
		})
	}
}

// TestStoredAdminInquiryDetailValidation exercises private-field encoding,
// NUL, trimming, length, address, and timestamp-order defense.
func TestStoredAdminInquiryDetailValidation(t *testing.T) {
	valid := validAdminInquiryDetail(1)
	invalidUTF8 := string([]byte{0xff})

	tests := []struct {
		// name identifies the rejected persisted-field invariant.
		name string
		// mutate changes exactly one field on an otherwise valid record.
		mutate func(*adminInquiryDetail)
	}{
		{name: "summary field", mutate: func(value *adminInquiryDetail) { value.Status = "pending" }},
		{name: "short email", mutate: func(value *adminInquiryDetail) { value.Email = "a@" }},
		{name: "long email", mutate: func(value *adminInquiryDetail) { value.Email = strings.Repeat("e", inquiryEmailMaxLength+1) }},
		{name: "invalid UTF-8 email", mutate: func(value *adminInquiryDetail) { value.Email = invalidUTF8 }},
		{name: "NUL email", mutate: func(value *adminInquiryDetail) { value.Email = "bad\x00@example.com" }},
		{name: "untrimmed email", mutate: func(value *adminInquiryDetail) { value.Email = " visitor@example.com" }},
		{name: "invalid mailbox", mutate: func(value *adminInquiryDetail) { value.Email = "Visitor <visitor@example.com>" }},
		{name: "empty message", mutate: func(value *adminInquiryDetail) { value.Message = "" }},
		{name: "long message", mutate: func(value *adminInquiryDetail) { value.Message = strings.Repeat("m", inquiryMessageMaxLength+1) }},
		{name: "invalid UTF-8 message", mutate: func(value *adminInquiryDetail) { value.Message = invalidUTF8 }},
		{name: "NUL message", mutate: func(value *adminInquiryDetail) { value.Message = "bad\x00message" }},
		{name: "untrimmed message", mutate: func(value *adminInquiryDetail) { value.Message = "message " }},
		{name: "zero update time", mutate: func(value *adminInquiryDetail) { value.UpdatedAt = time.Time{} }},
		{name: "update before creation", mutate: func(value *adminInquiryDetail) { value.UpdatedAt = value.CreatedAt.Add(-time.Second) }},
	}

	if !isValidStoredAdminInquiryDetail(valid) {
		t.Fatal("known-valid detail was rejected")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if isValidStoredAdminInquiryDetail(candidate) {
				t.Errorf("invalid detail was accepted: %#v", candidate)
			}
		})
	}
}

// TestInquiryStatusValidation proves the application accepts exactly the three
// values already protected by PostgreSQL and rejects the string zero value.
func TestInquiryStatusValidation(t *testing.T) {
	for _, status := range []inquiryStatus{
		inquiryStatusNew,
		inquiryStatusReviewed,
		inquiryStatusArchived,
	} {
		if !status.valid() {
			t.Errorf("supported status %q was rejected", status)
		}
	}
	for _, status := range []inquiryStatus{"", "pending", "NEW"} {
		if status.valid() {
			t.Errorf("unsupported status %q was accepted", status)
		}
	}
}

// TestValidStoredInquiryTextRejectsInvalidBounds protects the shared helper
// from accepting a caller-supplied impossible length interval.
func TestValidStoredInquiryTextRejectsInvalidBounds(t *testing.T) {
	if isValidStoredInquiryText("value", -1, 5) {
		t.Error("negative minimum was accepted")
	}
	if isValidStoredInquiryText("value", 6, 5) {
		t.Error("maximum below minimum was accepted")
	}
}
