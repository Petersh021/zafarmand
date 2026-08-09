package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// adminInquiryPageSize fixes the number of inquiry summaries shown on one
// admin page. Keeping the limit server-owned prevents a query-string value from
// requesting an unexpectedly large collection of visitor data from PostgreSQL.
const adminInquiryPageSize = 20

// Repository construction and read errors remain safe to return across an HTTP
// boundary: none contains SQL, driver diagnostics, or visitor-provided data.
var (
	// errAdminInquiryReaderDatabaseRequired rejects construction without the
	// application-owned PostgreSQL connection pool.
	errAdminInquiryReaderDatabaseRequired = errors.New(
		"create admin inquiry reader: database is required",
	)
	// errAdminInquiryInvalidQuery identifies a negative cursor or non-positive
	// detail identity before any database operation is attempted.
	errAdminInquiryInvalidQuery = errors.New("admin inquiry query is invalid")
	// errAdminInquiryNotFound maps only database/sql's no-row result and does not
	// reveal any other database condition.
	errAdminInquiryNotFound = errors.New("admin inquiry not found")
	// errAdminInquiryReadFailed deliberately does not wrap a driver error because
	// PostgreSQL diagnostics can contain contact data from a malformed row.
	errAdminInquiryReadFailed = errors.New(
		"admin inquiry database operation failed",
	)
)

// inquiryStatus is the closed review-state vocabulary already protected by the
// inquiries_status_supported PostgreSQL constraint.
type inquiryStatus string

const (
	// inquiryStatusNew identifies a submission that has not yet been reviewed.
	inquiryStatusNew inquiryStatus = "new"
	// inquiryStatusReviewed identifies the persisted reviewed state.
	inquiryStatusReviewed inquiryStatus = "reviewed"
	// inquiryStatusArchived identifies the persisted archived state.
	inquiryStatusArchived inquiryStatus = "archived"
)

// adminInquirySummary contains only the fields needed by the protected queue.
// Email and message are intentionally reserved for the detail read so a list
// page does not expose more visitor information than its interface requires.
type adminInquirySummary struct {
	// ID is PostgreSQL's stable, non-secret inquiry identity.
	ID int64
	// Name identifies the visitor in the administrative queue.
	Name string
	// Discipline is one machine value from the public Contact whitelist.
	Discipline string
	// Status is one trusted review state from the closed database vocabulary.
	Status inquiryStatus
	// CreatedAt is the inquiry's stored creation timestamp.
	CreatedAt time.Time
}

// adminInquiryDetail contains the complete visitor-facing payload needed by one
// protected detail page. It deliberately excludes the opaque submission key,
// which is an idempotency implementation detail and has no administrative use.
type adminInquiryDetail struct {
	// ID is PostgreSQL's stable, non-secret inquiry identity.
	ID int64
	// Name is the normalized visitor name accepted by the Contact form.
	Name string
	// Email is the normalized address to which the studio may reply.
	Email string
	// Discipline is one machine value from the public Contact whitelist.
	Discipline string
	// Message is the visitor's normalized project summary.
	Message string
	// Status is one trusted review state from the closed database vocabulary.
	Status inquiryStatus
	// CreatedAt is the inquiry's stored creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the stored update timestamp and cannot predate creation.
	UpdatedAt time.Time
}

// adminInquiryPage is one bounded keyset-paginated slice of the private queue.
type adminInquiryPage struct {
	// Items contains at most adminInquiryPageSize summaries in descending ID order.
	Items []adminInquirySummary
	// HasMore tells the template whether an older page exists.
	HasMore bool
	// NextBeforeID is the last retained ID and becomes the next page's exclusive
	// upper bound. It remains zero when HasMore is false.
	NextBeforeID int64
}

// adminInquiryReader is the read-only persistence behavior needed by the first
// inquiry-management stage. Keeping it separate from the public write
// repository prevents an admin feature from widening the Contact handler's
// dependency or authority.
type adminInquiryReader interface {
	// List returns one bounded descending-ID page below an optional cursor.
	List(context.Context, int64) (adminInquiryPage, error)
	// FindByID returns exactly one complete protected record or a safe category.
	FindByID(context.Context, int64) (adminInquiryDetail, error)
}

// The descending list can use the existing inquiries primary-key B-tree in
// reverse order. Fetching one extra row proves whether another page exists
// without a separate COUNT query. No email, message, or submission key crosses
// this queue query boundary.
const listLatestAdminInquiriesSQL = `SELECT
    id,
    name,
    discipline,
    status,
    created_at
FROM public.inquiries
ORDER BY id DESC
LIMIT $1`

// listAdminInquiriesBeforeSQL applies an exclusive ID cursor. Unlike OFFSET,
// this remains stable when a new inquiry is inserted above the current page and
// does not force PostgreSQL to discard every earlier row on deep pages.
const listAdminInquiriesBeforeSQL = `SELECT
    id,
    name,
    discipline,
    status,
    created_at
FROM public.inquiries
WHERE id < $1
ORDER BY id DESC
LIMIT $2`

// findAdminInquiryByIDSQL selects exactly the data required by one protected
// detail view. The submission key is intentionally absent because displaying
// or logging that token cannot help an administrator review the inquiry.
const findAdminInquiryByIDSQL = `SELECT
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

// adminInquiryRows is the narrow database/sql multi-row surface required by
// List. Tests can implement it without adding a database mocking dependency.
type adminInquiryRows interface {
	// Next advances to the next result row when one exists.
	Next() bool
	// Scan copies the current row into the supplied destinations.
	Scan(...any) error
	// Err reports an iteration failure that occurred after the last Scan.
	Err() error
	// Close releases the database result as soon as reading ends.
	Close() error
}

// adminInquiryQuery adapts database/sql's concrete *sql.Rows to the narrow
// multi-row seam used by the repository and its deterministic tests.
type adminInquiryQuery func(
	context.Context,
	string,
	...any,
) (adminInquiryRows, error)

// adminInquiryRowScanner is the one-row behavior required by the detail read.
type adminInquiryRowScanner interface {
	// Scan copies the single-row result into the supplied destinations.
	Scan(...any) error
}

// adminInquiryQueryRow adapts database/sql's concrete *sql.Row to a scanner
// interface while preserving the request context and positional parameters.
type adminInquiryQueryRow func(
	context.Context,
	string,
	...any,
) adminInquiryRowScanner

// postgresAdminInquiryReader borrows the process-wide PostgreSQL pool for
// concurrent read-only admin requests. The process that opens the pool remains
// responsible for closing it.
type postgresAdminInquiryReader struct {
	// query runs the bounded queue statement and returns an iterable result.
	query adminInquiryQuery
	// queryRow runs the exact-ID detail statement.
	queryRow adminInquiryQueryRow
}

// Compile-time interface verification catches accidental changes to either
// read method before they reach application wiring.
var _ adminInquiryReader = (*postgresAdminInquiryReader)(nil)

// newPostgresAdminInquiryReader adapts the application-owned database pool to
// the isolated Stage 16 read contract. Construction opens no connection and
// performs no query.
func newPostgresAdminInquiryReader(
	database *sql.DB,
) (*postgresAdminInquiryReader, error) {
	if database == nil {
		return nil, errAdminInquiryReaderDatabaseRequired
	}

	return &postgresAdminInquiryReader{
		query: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) (adminInquiryRows, error) {
			return database.QueryContext(ctx, query, arguments...)
		},
		queryRow: func(
			ctx context.Context,
			query string,
			arguments ...any,
		) adminInquiryRowScanner {
			return database.QueryRowContext(ctx, query, arguments...)
		},
	}, nil
}

// List returns the newest inquiry summaries, or an older slice whose IDs are
// strictly below beforeID. A zero cursor selects the first page; negative
// cursors are rejected before PostgreSQL is contacted.
func (reader *postgresAdminInquiryReader) List(
	ctx context.Context,
	beforeID int64,
) (adminInquiryPage, error) {
	if beforeID < 0 || ctx == nil {
		return adminInquiryPage{}, errAdminInquiryInvalidQuery
	}
	if reader == nil || reader.query == nil {
		return adminInquiryPage{}, errAdminInquiryReadFailed
	}

	// Both trusted statements fetch exactly one sentinel row beyond the visible
	// page. The cursor variant adds its exclusive upper bound as $1.
	query := listLatestAdminInquiriesSQL
	arguments := []any{adminInquiryPageSize + 1}
	if beforeID > 0 {
		query = listAdminInquiriesBeforeSQL
		arguments = []any{beforeID, adminInquiryPageSize + 1}
	}

	rows, err := reader.query(ctx, query, arguments...)
	if err != nil {
		// Driver errors are intentionally collapsed instead of wrapped because a
		// diagnostic may repeat visitor data from a malformed database row.
		if rows != nil {
			// Although database/sql normally returns nil rows with an error, closing a
			// non-nil substituted result keeps the repository defensive and leak-free.
			_ = rows.Close()
		}

		return adminInquiryPage{}, errAdminInquiryReadFailed
	}
	if rows == nil {
		return adminInquiryPage{}, errAdminInquiryReadFailed
	}

	items := make([]adminInquirySummary, 0, adminInquiryPageSize+1)
	var previousID int64
	for rows.Next() {
		// LIMIT should make a twenty-second row impossible. Enforcing the bound at
		// the Go boundary also prevents a broken or substituted query source from
		// growing memory with unbounded personal data.
		if len(items) >= adminInquiryPageSize+1 {
			_ = rows.Close()

			return adminInquiryPage{}, errAdminInquiryReadFailed
		}

		var summary adminInquirySummary
		var status string
		if err := rows.Scan(
			&summary.ID,
			&summary.Name,
			&summary.Discipline,
			&status,
			&summary.CreatedAt,
		); err != nil {
			_ = rows.Close()

			return adminInquiryPage{}, errAdminInquiryReadFailed
		}
		summary.Status = inquiryStatus(status)

		if !isValidStoredAdminInquirySummary(summary) {
			// Database constraints protect normal writes, while this check prevents a
			// corrupted or unexpectedly shaped row from becoming trusted admin HTML.
			_ = rows.Close()

			return adminInquiryPage{}, errAdminInquiryReadFailed
		}
		if (beforeID > 0 && summary.ID >= beforeID) ||
			(previousID > 0 && summary.ID >= previousID) {
			// Strict descending order is part of the keyset cursor contract. Rejecting
			// a violation avoids duplicated or skipped records on the next request.
			_ = rows.Close()

			return adminInquiryPage{}, errAdminInquiryReadFailed
		}

		items = append(items, summary)
		previousID = summary.ID
	}

	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil || closeErr != nil {
		// Neither lower-level error is wrapped because it may contain private row
		// values or connection details not suitable for an HTTP response or log.
		return adminInquiryPage{}, errAdminInquiryReadFailed
	}

	page := adminInquiryPage{
		Items: items,
	}
	if len(page.Items) == adminInquiryPageSize+1 {
		// The extra record is evidence only; trim it before returning the page and
		// derive the next exclusive bound from the final visible record. The full
		// slice expression also prevents a caller from reslicing into the sentinel.
		page.Items = page.Items[:adminInquiryPageSize:adminInquiryPageSize]
		page.HasMore = true
		page.NextBeforeID = page.Items[len(page.Items)-1].ID
	}

	return page, nil
}

// FindByID returns one complete inquiry for a protected detail page. It accepts
// only a positive identity and maps a genuine missing row separately from all
// operational or data-integrity failures.
func (reader *postgresAdminInquiryReader) FindByID(
	ctx context.Context,
	id int64,
) (adminInquiryDetail, error) {
	if id <= 0 || ctx == nil {
		return adminInquiryDetail{}, errAdminInquiryInvalidQuery
	}
	if reader == nil || reader.queryRow == nil {
		return adminInquiryDetail{}, errAdminInquiryReadFailed
	}

	row := reader.queryRow(ctx, findAdminInquiryByIDSQL, id)
	if row == nil {
		return adminInquiryDetail{}, errAdminInquiryReadFailed
	}

	var detail adminInquiryDetail
	var status string
	if err := row.Scan(
		&detail.ID,
		&detail.Name,
		&detail.Email,
		&detail.Discipline,
		&detail.Message,
		&status,
		&detail.CreatedAt,
		&detail.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Only the standard no-row category becomes a not-found result; every
			// driver or scan failure remains deliberately indistinguishable.
			return adminInquiryDetail{}, errAdminInquiryNotFound
		}

		return adminInquiryDetail{}, errAdminInquiryReadFailed
	}
	detail.Status = inquiryStatus(status)

	if detail.ID != id || !isValidStoredAdminInquiryDetail(detail) {
		// Refuse malformed persisted personal data instead of assuming that every
		// future database writer preserved the public form's validation rules. The
		// identity equality also prevents a substituted scanner from satisfying one
		// URL with a different inquiry record.
		return adminInquiryDetail{}, errAdminInquiryReadFailed
	}

	return detail, nil
}

// valid reports whether a status belongs to the database's closed Stage 13
// vocabulary. No database string is rendered as a trusted status by default.
func (status inquiryStatus) valid() bool {
	return status == inquiryStatusNew ||
		status == inquiryStatusReviewed ||
		status == inquiryStatusArchived
}

// isValidStoredAdminInquirySummary verifies every field returned by a list
// query before the repository exposes it to a template-facing handler.
func isValidStoredAdminInquirySummary(summary adminInquirySummary) bool {
	if summary.ID <= 0 ||
		!isValidStoredInquiryText(summary.Name, 1, inquiryNameMaxLength) ||
		!summary.Status.valid() ||
		summary.CreatedAt.IsZero() {
		return false
	}

	_, supportedDiscipline := inquiryDisciplineLabel(summary.Discipline)

	return supportedDiscipline
}

// isValidStoredAdminInquiryDetail extends summary validation with the private
// email, message, and timestamp-order invariants required by a detail page.
func isValidStoredAdminInquiryDetail(detail adminInquiryDetail) bool {
	summary := adminInquirySummary{
		ID:         detail.ID,
		Name:       detail.Name,
		Discipline: detail.Discipline,
		Status:     detail.Status,
		CreatedAt:  detail.CreatedAt,
	}
	if !isValidStoredAdminInquirySummary(summary) ||
		!isValidStoredInquiryText(detail.Email, 3, inquiryEmailMaxLength) ||
		!isExactInquiryEmail(detail.Email) ||
		!isValidStoredInquiryText(detail.Message, 1, inquiryMessageMaxLength) ||
		detail.UpdatedAt.IsZero() ||
		detail.UpdatedAt.Before(detail.CreatedAt) {
		return false
	}

	return true
}

// isValidStoredInquiryText applies the common UTF-8, NUL, trimming, and rune-
// length rules shared by persisted name, email, and message values.
func isValidStoredInquiryText(value string, minimumRunes int, maximumRunes int) bool {
	if minimumRunes < 0 || maximumRunes < minimumRunes ||
		!utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		value != strings.TrimSpace(value) {
		return false
	}

	length := utf8.RuneCountInString(value)

	return length >= minimumRunes && length <= maximumRunes
}
