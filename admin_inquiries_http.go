package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	// adminInquiryNavigationPath is the trusted parent route used by both inbox
	// pages to select one active item in the shared administration navigation.
	adminInquiryNavigationPath = "/admin/inquiries"
	// adminInquiryTimeLayout is a concise UTC presentation kept in Go so the
	// template receives display-ready text rather than calling time methods.
	adminInquiryTimeLayout = "02 Jan 2006, 15:04 UTC"
)

// HTTP-layer sentinels describe local request and dependency failures without
// retaining cursor text, database details, or visitor-provided values.
var (
	// errAdminInquiryCursorInvalid identifies malformed list navigation without
	// retaining the supplied query text.
	errAdminInquiryCursorInvalid = errors.New(
		"admin inquiry cursor is invalid",
	)
	// errAdminInquiryReaderRequired prevents the server from starting with
	// protected routes that have no read dependency.
	errAdminInquiryReaderRequired = errors.New(
		"create application: admin inquiry reader is required",
	)
)

// adminInquiryListPageData is the complete template contract for one read-only
// inbox page. It intentionally contains neither full messages nor email
// addresses, reducing personal data rendered by the overview.
type adminInquiryListPageData struct {
	// Items contains at most adminInquiryPageSize summaries in descending ID order.
	Items []adminInquirySummaryPageData
	// EmptyMessage truthfully distinguishes an empty inbox from an exhausted
	// older-page cursor.
	EmptyMessage string
	// IsPaginated lets the page offer a direct return to the newest records.
	IsPaginated bool
	// NextPath is the trusted older-page URL, or empty when no older row exists.
	NextPath string
}

// adminInquirySummaryPageData contains only values rendered by one inbox card.
// All visitor strings remain ordinary strings for html/template escaping.
type adminInquirySummaryPageData struct {
	// Reference is a non-sensitive display label derived from the internal ID.
	Reference string
	// Path is the canonical protected detail URL derived from the internal ID.
	Path string
	// Name identifies the person who submitted the inquiry.
	Name string
	// DisciplineLabel is resolved from the server-owned discipline vocabulary.
	DisciplineLabel string
	// StatusLabel is trusted visible text from the closed status vocabulary.
	StatusLabel string
	// StatusClass selects one trusted CSS modifier without visitor input.
	StatusClass string
	// CreatedAtISO is the machine-readable UTC value used by the time element.
	CreatedAtISO string
	// CreatedAtLabel is the concise human-readable UTC value.
	CreatedAtLabel string
}

// adminInquiryDetailPageData is the display-ready contract for one protected
// inquiry. The idempotency key and every authentication value remain absent.
type adminInquiryDetailPageData struct {
	// Reference is the stable non-sensitive label derived from the inquiry ID.
	Reference string
	// Name is escaped when rendered as the primary heading and fact value.
	Name string
	// Email is displayed as text only; Stage 16 does not initiate external mail.
	Email string
	// DisciplineLabel is trusted server-owned interface text.
	DisciplineLabel string
	// StatusLabel is trusted visible text from the closed status vocabulary.
	StatusLabel string
	// StatusClass selects one trusted CSS modifier without visitor input.
	StatusClass string
	// CreatedAtISO is the machine-readable UTC submission time.
	CreatedAtISO string
	// CreatedAtLabel is the concise human-readable UTC submission time.
	CreatedAtLabel string
	// Message is ordinary escaped text whose line breaks are preserved by CSS.
	Message string
}

// adminInquiryListHandler validates one optional keyset cursor, reads at most
// one repository page, maps stored values to trusted labels, and renders the
// protected inbox without logging personal data.
func (app *application) adminInquiryListHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminInquiryNavigationPath {
		// ServeMux may match an equivalent percent-encoded segment after unescaping
		// it. Keep the inbox at one canonical protected address instead.
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery {
		// A trailing question mark supplies no cursor but creates a second visible
		// spelling. Treat it like every other malformed list query.
		http.Error(w, "invalid inquiry page", http.StatusBadRequest)

		return
	}

	beforeID, err := parseAdminInquiryBeforeID(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "invalid inquiry page", http.StatusBadRequest)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	page, err := app.adminInquiries.List(ctx, beforeID)
	cancel()
	if err != nil {
		if errors.Is(err, errAdminInquiryInvalidQuery) {
			http.Error(w, "invalid inquiry page", http.StatusBadRequest)

			return
		}

		// Repository errors are deliberately safe sentinels, but the HTTP layer
		// still avoids logging injected implementation text or query parameters.
		log.Printf("admin inquiry list failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}
	if !isValidAdminInquiryPage(page, beforeID) {
		// Treat a broken injected reader exactly like a corrupt database result.
		// No item value or cursor is included in the log or response.
		log.Printf("admin inquiry list contract failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	items := make([]adminInquirySummaryPageData, 0, len(page.Items))
	for _, inquiry := range page.Items {
		item, valid := newAdminInquirySummaryPageData(inquiry)
		if !valid {
			log.Printf("admin inquiry list mapping failed")
			http.Error(
				w,
				"service temporarily unavailable",
				http.StatusServiceUnavailable,
			)

			return
		}
		items = append(items, item)
	}

	emptyMessage := "No inquiries have been received yet."
	if beforeID > 0 {
		emptyMessage = "No older inquiries remain."
	}
	listData := &adminInquiryListPageData{
		Items:        items,
		EmptyMessage: emptyMessage,
		IsPaginated:  beforeID > 0,
	}
	if page.HasMore {
		listData.NextPath = adminInquiryNavigationPath + "?before=" +
			strconv.FormatInt(page.NextBeforeID, 10)
	}

	data := newAuthenticatedAdminPageData(
		"Inquiries",
		adminInquiryNavigationPath,
		requestIdentity,
	)
	data.InquiryList = listData

	app.renderAdmin(
		w,
		http.StatusOK,
		"inquiries.html",
		data,
	)
}

// isValidAdminInquiryPage rechecks the interface-level keyset contract before
// any repository implementation can influence a navigation URL. Production
// PostgreSQL already enforces these invariants; this second boundary keeps a
// future implementation or test double fail-closed as well.
func isValidAdminInquiryPage(page adminInquiryPage, beforeID int64) bool {
	if beforeID < 0 || len(page.Items) > adminInquiryPageSize {
		return false
	}
	if page.HasMore {
		if len(page.Items) != adminInquiryPageSize ||
			page.NextBeforeID != page.Items[len(page.Items)-1].ID {
			return false
		}
	} else if page.NextBeforeID != 0 {
		return false
	}

	var previousID int64
	for _, inquiry := range page.Items {
		if inquiry.ID <= 0 ||
			(beforeID > 0 && inquiry.ID >= beforeID) ||
			(previousID > 0 && inquiry.ID >= previousID) {
			return false
		}
		previousID = inquiry.ID
	}

	return true
}

// adminInquiryDetailHandler accepts one canonical positive path ID only after
// authentication and role authorization, then renders either one escaped
// record, a generic 404, or a personal-data-safe service failure.
func (app *application) adminInquiryDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid inquiry request", http.StatusBadRequest)

		return
	}

	inquiryID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	canonicalPath := adminInquiryNavigationPath + "/" +
		strconv.FormatInt(inquiryID, 10)
	if r.URL.EscapedPath() != canonicalPath {
		// ServeMux correctly matches escaped path segments after unescaping them.
		// The protected resource nevertheless keeps one visible URL spelling so an
		// encoded digit cannot become a second address for the same inquiry.
		http.NotFound(w, r)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), adminRepositoryTimeout)
	inquiry, err := app.adminInquiries.FindByID(ctx, inquiryID)
	cancel()
	if err != nil {
		if errors.Is(err, errAdminInquiryNotFound) ||
			errors.Is(err, errAdminInquiryInvalidQuery) {
			http.NotFound(w, r)

			return
		}

		log.Printf("admin inquiry detail failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	detail, valid := newAdminInquiryDetailPageData(inquiry)
	if !valid {
		log.Printf("admin inquiry detail mapping failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Inquiry detail",
		adminInquiryNavigationPath,
		requestIdentity,
	)
	data.InquiryDetail = &detail

	app.renderAdmin(
		w,
		http.StatusOK,
		"inquiry-detail.html",
		data,
	)
}

// parseAdminInquiryBeforeID accepts either no query or exactly one canonical
// positive base-10 `before` value. Unknown and duplicate fields fail together.
func parseAdminInquiryBeforeID(rawQuery string) (int64, error) {
	if rawQuery == "" {
		return 0, nil
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 1 {
		return 0, errAdminInquiryCursorInvalid
	}
	beforeValues, exists := values["before"]
	if !exists || len(beforeValues) != 1 {
		return 0, errAdminInquiryCursorInvalid
	}

	beforeID, valid := parseCanonicalPositiveInt64(beforeValues[0])
	if !valid {
		return 0, errAdminInquiryCursorInvalid
	}
	if rawQuery != "before="+strconv.FormatInt(beforeID, 10) {
		// Because the only permitted value contains decimal digits, its canonical
		// query spelling needs no escaping. Rejecting alternate encodings and empty
		// separators gives the private route one deterministic URL contract.
		return 0, errAdminInquiryCursorInvalid
	}

	return beforeID, nil
}

// parseCanonicalPositiveInt64 rejects signs, whitespace, zero, leading zeroes,
// non-decimal text, and overflow by requiring strconv's canonical round trip.
func parseCanonicalPositiveInt64(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, false
	}

	return parsed, true
}

// newAdminInquirySummaryPageData validates and translates one repository
// summary into trusted interface labels and UTC timestamp strings.
func newAdminInquirySummaryPageData(
	inquiry adminInquirySummary,
) (adminInquirySummaryPageData, bool) {
	if !isValidStoredAdminInquirySummary(inquiry) {
		return adminInquirySummaryPageData{}, false
	}

	disciplineLabel, exists := inquiryDisciplineLabel(inquiry.Discipline)
	if !exists {
		return adminInquirySummaryPageData{}, false
	}
	statusLabel, statusClass, exists := adminInquiryStatusPresentation(
		inquiry.Status,
	)
	if !exists || inquiry.ID <= 0 || inquiry.CreatedAt.IsZero() {
		return adminInquirySummaryPageData{}, false
	}

	createdAt := inquiry.CreatedAt.UTC()

	return adminInquirySummaryPageData{
		Reference:       adminInquiryReference(inquiry.ID),
		Path:            adminInquiryNavigationPath + "/" + strconv.FormatInt(inquiry.ID, 10),
		Name:            inquiry.Name,
		DisciplineLabel: disciplineLabel,
		StatusLabel:     statusLabel,
		StatusClass:     statusClass,
		CreatedAtISO:    createdAt.Format(time.RFC3339),
		CreatedAtLabel:  createdAt.Format(adminInquiryTimeLayout),
	}, true
}

// newAdminInquiryDetailPageData validates and translates one repository detail
// without introducing links, HTML fragments, or database-only fields.
func newAdminInquiryDetailPageData(
	inquiry adminInquiryDetail,
) (adminInquiryDetailPageData, bool) {
	if !isValidStoredAdminInquiryDetail(inquiry) {
		return adminInquiryDetailPageData{}, false
	}

	disciplineLabel, exists := inquiryDisciplineLabel(inquiry.Discipline)
	if !exists {
		return adminInquiryDetailPageData{}, false
	}
	statusLabel, statusClass, exists := adminInquiryStatusPresentation(
		inquiry.Status,
	)
	if !exists || inquiry.ID <= 0 || inquiry.CreatedAt.IsZero() {
		return adminInquiryDetailPageData{}, false
	}

	createdAt := inquiry.CreatedAt.UTC()

	return adminInquiryDetailPageData{
		Reference:       adminInquiryReference(inquiry.ID),
		Name:            inquiry.Name,
		Email:           inquiry.Email,
		DisciplineLabel: disciplineLabel,
		StatusLabel:     statusLabel,
		StatusClass:     statusClass,
		CreatedAtISO:    createdAt.Format(time.RFC3339),
		CreatedAtLabel:  createdAt.Format(adminInquiryTimeLayout),
		Message:         inquiry.Message,
	}, true
}

// adminInquiryStatusPresentation converts the closed persistence vocabulary to
// trusted labels and CSS suffixes. Unknown values never reach a template.
func adminInquiryStatusPresentation(
	status inquiryStatus,
) (string, string, bool) {
	switch status {
	case inquiryStatusNew:
		return "New", "new", true
	case inquiryStatusReviewed:
		return "Reviewed", "reviewed", true
	case inquiryStatusArchived:
		return "Archived", "archived", true
	default:
		return "", "", false
	}
}

// adminInquiryReference creates a compact stable label without exposing any
// visitor field. Six digits are a minimum width, not a truncation boundary.
func adminInquiryReference(inquiryID int64) string {
	return fmt.Sprintf("#%06d", inquiryID)
}
