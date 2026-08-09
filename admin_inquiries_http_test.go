package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stage16HTTPResponse captures the observable HTTP result needed by inbox
// tests without keeping a response body open between assertions.
type stage16HTTPResponse struct {
	// StatusCode is the final status written by the complete route graph.
	StatusCode int
	// Header includes both handler headers and the private admin policy.
	Header http.Header
	// Body is the rendered HTML or deliberately generic error response.
	Body string
}

// stage16MalformedInquiryReader returns one caller-supplied list page. It lets
// the HTTP test prove that handlers distrust even an injected repository and
// fail closed before an invalid cursor or visitor value reaches a template.
type stage16MalformedInquiryReader struct {
	// page is intentionally allowed to violate the adminInquiryReader contract.
	page adminInquiryPage
}

// List implements the read interface while preserving the malformed page
// exactly as arranged by the test.
func (reader *stage16MalformedInquiryReader) List(
	_ context.Context,
	_ int64,
) (adminInquiryPage, error) {
	return reader.page, nil
}

// FindByID is unused by the malformed-list test and fails with the same safe
// sentinel as a genuine missing detail record.
func (reader *stage16MalformedInquiryReader) FindByID(
	_ context.Context,
	_ int64,
) (adminInquiryDetail, error) {
	return adminInquiryDetail{}, errAdminInquiryNotFound
}

// stage16ServeAdminRequest runs the real middleware and ServeMux stack. Tests
// therefore cover authentication ordering, route matching, templates, and
// security headers rather than calling a handler in isolation.
func stage16ServeAdminRequest(
	t *testing.T,
	app *application,
	request *http.Request,
) stage16HTTPResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(recorder, request)
	result := recorder.Result()
	defer result.Body.Close()

	return stage16HTTPResponse{
		StatusCode: result.StatusCode,
		Header:     result.Header.Clone(),
		Body:       recorder.Body.String(),
	}
}

// assertStage16AdminStatus checks the expected route outcome and the headers
// that prevent protected visitor data from being cached, framed, or indexed.
func assertStage16AdminStatus(
	t *testing.T,
	response stage16HTTPResponse,
	wantStatus int,
) {
	t.Helper()

	if response.StatusCode != wantStatus {
		t.Errorf(
			"status: got %d, want %d; body: %q",
			response.StatusCode,
			wantStatus,
			response.Body,
		)
	}
	assertAdminHTTPSecurityHeaders(t, response.Header)
}

// assertStage16InquiryCalls verifies which persistence operation crossed the
// protected HTTP boundary. Invalid and unauthenticated requests must leave
// both slices empty, which is an important authorization property.
func assertStage16InquiryCalls(
	t *testing.T,
	reader *recordingAdminInquiryReader,
	wantList []int64,
	wantFind []int64,
) {
	t.Helper()

	gotList, gotFind := reader.callSnapshot()
	if !reflect.DeepEqual(gotList, wantList) {
		t.Errorf("list calls: got %v, want %v", gotList, wantList)
	}
	if !reflect.DeepEqual(gotFind, wantFind) {
		t.Errorf("detail calls: got %v, want %v", gotFind, wantFind)
	}
}

// assertStage16BodyContains reports every missing marker in one response so a
// failed rendering assertion remains useful without stopping at the first item.
func assertStage16BodyContains(
	t *testing.T,
	body string,
	markers ...string,
) {
	t.Helper()

	for _, marker := range markers {
		if !strings.Contains(body, marker) {
			t.Errorf("response body does not contain %q", marker)
		}
	}
}

// assertStage16BodyOmits is the privacy counterpart to the presence helper.
// It is used for raw HTML, list-only personal data, tokens, and password hashes.
func assertStage16BodyOmits(
	t *testing.T,
	body string,
	markers ...string,
) {
	t.Helper()

	for _, marker := range markers {
		if marker != "" && strings.Contains(body, marker) {
			t.Errorf("response body unexpectedly contains %q", marker)
		}
	}
}

// stage16Inquiry creates one valid stored inquiry with deterministic fields.
// Tests can replace individual fields when they need escaping or privacy probes.
func stage16Inquiry(id int64) adminInquiryDetail {
	createdAt := time.Date(2032, 3, 4, 5, 6, 7, 0, time.UTC).
		Add(time.Duration(id) * time.Minute)

	return adminInquiryDetail{
		ID:         id,
		Name:       fmt.Sprintf("Visitor %d", id),
		Email:      fmt.Sprintf("visitor-%d@example.test", id),
		Discipline: "products",
		Message:    fmt.Sprintf("Private project message %d", id),
		Status:     inquiryStatusNew,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
}

// stage16InquiryRange builds IDs 1 through count. The recording reader sorts
// this insertion-order slice descending, mirroring PostgreSQL's keyset query.
func stage16InquiryRange(count int) []adminInquiryDetail {
	inquiries := make([]adminInquiryDetail, 0, count)
	for id := 1; id <= count; id++ {
		inquiries = append(inquiries, stage16Inquiry(int64(id)))
	}

	return inquiries
}

// TestAdminInquiryRoutesRequireAuthentication proves that both personal-data
// routes redirect before parsing IDs or consulting the inquiry repository.
func TestAdminInquiryRoutesRequireAuthentication(t *testing.T) {
	paths := []string{
		"/admin/inquiries",
		"/admin/inquiries/17",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			repository := newRecordingAdminRepository()
			passwords := newTestAdminPasswordManager(t)
			app := newAdminHTTPTestApplicationWithInquiryReader(
				t,
				repository,
				passwords,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				path,
				nil,
				false,
			)
			response := stage16ServeAdminRequest(t, app, request)

			// Authentication is deliberately the first data-access boundary: an
			// anonymous browser receives only the account-neutral login location.
			assertStage16AdminStatus(t, response, http.StatusSeeOther)
			if location := response.Header.Get("Location"); location != "/admin/login" {
				t.Errorf("Location: got %q, want /admin/login", location)
			}
			assertStage16InquiryCalls(t, reader, nil, nil)
		})
	}
}

// TestAdminInquiryRoutesAllowCurrentReaderRoles verifies the explicit Stage 16
// owner/editor allowlist on both list and detail routes.
func TestAdminInquiryRoutesAllowCurrentReaderRoles(t *testing.T) {
	tests := []struct {
		// name describes the trusted role shown in the subtest output.
		name string
		// role is the repository-backed authorization value.
		role adminRole
		// roleLabel is the server-owned label rendered by the private shell.
		roleLabel string
	}{
		{name: "owner", role: adminRoleOwner, roleLabel: "Owner"},
		{name: "editor", role: adminRoleEditor, roleLabel: "Editor"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			reader.setInquiries(stage16Inquiry(17))
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				test.role,
				reader,
			)

			listRequest := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/inquiries",
				nil,
				false,
				fixture.cookies()...,
			)
			listResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				listRequest,
			)
			assertStage16AdminStatus(t, listResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				listResponse.Body,
				"Inquiry inbox",
				test.roleLabel,
				`href="/admin/inquiries/17"`,
				`aria-current="page"`,
				`action="/admin/logout"`,
			)

			detailRequest := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/inquiries/17",
				nil,
				false,
				fixture.cookies()...,
			)
			detailResponse := stage16ServeAdminRequest(
				t,
				fixture.app,
				detailRequest,
			)
			assertStage16AdminStatus(t, detailResponse, http.StatusOK)
			assertStage16BodyContains(
				t,
				detailResponse.Body,
				"Visitor 17",
				test.roleLabel,
				`href="/admin/inquiries"`,
			)
			assertStage16InquiryCalls(t, reader, []int64{0}, []int64{17})
		})
	}
}

// TestAdminInquiryListAcceptsOnlyCanonicalCursorQuery verifies that pagination
// has one deterministic URL spelling and rejects ambiguous or extra fields
// before the reader receives a cursor.
func TestAdminInquiryListAcceptsOnlyCanonicalCursorQuery(t *testing.T) {
	t.Run("canonical cursor reaches reader", func(t *testing.T) {
		reader := newRecordingAdminInquiryReader()
		fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
			t,
			adminRoleOwner,
			reader,
		)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/inquiries?before=42",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusOK)
		assertStage16InquiryCalls(t, reader, []int64{42}, nil)
	})

	t.Run("encoded equivalent route is rejected", func(t *testing.T) {
		reader := newRecordingAdminInquiryReader()
		fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
			t,
			adminRoleOwner,
			reader,
		)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/%69nquiries",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		// A decoded path may reach the same ServeMux pattern, but only the
		// canonical visible spelling is accepted as the protected list URL.
		assertStage16AdminStatus(t, response, http.StatusNotFound)
		assertStage16InquiryCalls(t, reader, nil, nil)
	})

	invalidQueries := []string{
		"before=",
		"before=0",
		"before=-1",
		"before=+1",
		"before=01",
		"before=1%20",
		"before=abc",
		"before=9223372036854775808",
		"before=2&before=1",
		"after=1",
		"before=1&after=2",
		"before=%34%32",
		"before=42&",
		"before=42;after=2",
	}

	for _, rawQuery := range invalidQueries {
		t.Run(rawQuery, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				adminRoleOwner,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				"/admin/inquiries?"+rawQuery,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			// Malformed navigation is a client error, but the response never
			// reflects the supplied query and no database-shaped read is attempted.
			assertStage16AdminStatus(t, response, http.StatusBadRequest)
			assertStage16BodyContains(t, response.Body, "invalid inquiry page")
			assertStage16BodyOmits(t, response.Body, rawQuery)
			assertStage16InquiryCalls(t, reader, nil, nil)
		})
	}
}

// TestAdminInquiryDetailAcceptsOnlyCanonicalID verifies exact positive decimal
// path IDs and rejects detail query strings before the reader is called.
func TestAdminInquiryDetailAcceptsOnlyCanonicalID(t *testing.T) {
	invalidPaths := []string{
		"/admin/inquiries/0",
		"/admin/inquiries/-1",
		"/admin/inquiries/+1",
		"/admin/inquiries/01",
		"/admin/inquiries/1.0",
		"/admin/inquiries/abc",
		"/admin/inquiries/%31",
		"/admin/inquiries/9223372036854775808",
	}

	for _, path := range invalidPaths {
		t.Run(path, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				adminRoleEditor,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			// A malformed identifier is indistinguishable from any other absent
			// resource and is rejected without probing repository state.
			assertStage16AdminStatus(t, response, http.StatusNotFound)
			assertStage16InquiryCalls(t, reader, nil, nil)
		})
	}

	t.Run("detail query is rejected", func(t *testing.T) {
		reader := newRecordingAdminInquiryReader()
		reader.setInquiries(stage16Inquiry(17))
		fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
			t,
			adminRoleOwner,
			reader,
		)
		request := adminHTTPNewRequest(
			http.MethodGet,
			"/admin/inquiries/17?source=email",
			nil,
			false,
			fixture.cookies()...,
		)
		response := stage16ServeAdminRequest(t, fixture.app, request)

		assertStage16AdminStatus(t, response, http.StatusBadRequest)
		assertStage16BodyContains(t, response.Body, "invalid inquiry request")
		assertStage16BodyOmits(t, response.Body, "source=email")
		assertStage16InquiryCalls(t, reader, nil, nil)
	})
}

// TestAdminInquiryRoutesRejectBareQueryDelimiter verifies that a trailing
// question mark is not treated as an alternate spelling of either protected
// route. URL.ForceQuery preserves this distinction even though RawQuery is empty.
func TestAdminInquiryRoutesRejectBareQueryDelimiter(t *testing.T) {
	tests := []struct {
		// name identifies the list or detail route under test.
		name string
		// path deliberately ends in a bare query delimiter.
		path string
		// message is the route-specific account-neutral validation response.
		message string
	}{
		{
			name:    "list",
			path:    "/admin/inquiries?",
			message: "invalid inquiry page",
		},
		{
			name:    "detail",
			path:    "/admin/inquiries/17?",
			message: "invalid inquiry request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			reader.setInquiries(stage16Inquiry(17))
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				adminRoleOwner,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				test.path,
				nil,
				false,
				fixture.cookies()...,
			)
			if !request.URL.ForceQuery {
				t.Fatal("test request did not preserve its bare query delimiter")
			}
			response := stage16ServeAdminRequest(t, fixture.app, request)

			// Canonical validation happens before persistence, so an empty query
			// delimiter cannot silently become a list or exact-record read.
			assertStage16AdminStatus(t, response, http.StatusBadRequest)
			assertStage16BodyContains(t, response.Body, test.message)
			assertStage16InquiryCalls(t, reader, nil, nil)
		})
	}
}

// TestAdminInquiryListPaginatesWithExclusiveIDCursor exercises the first,
// middle, and final keyset pages without JavaScript or OFFSET state.
func TestAdminInquiryListPaginatesWithExclusiveIDCursor(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	reader.setInquiries(stage16InquiryRange(45)...)
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleOwner,
		reader,
	)

	tests := []struct {
		// name identifies the pagination position under test.
		name string
		// path contains either no cursor or the previous page's final ID.
		path string
		// firstName and lastName are the inclusive visible boundaries.
		firstName string
		lastName  string
		// omittedName proves the cursor and page-size boundary is exclusive.
		omittedName string
		// itemCount is the number of summary cards expected on this page.
		itemCount int
		// olderPath is empty only when no older row exists.
		olderPath string
		// returnToLatest distinguishes every non-initial page.
		returnToLatest bool
	}{
		{
			name:        "first page",
			path:        "/admin/inquiries",
			firstName:   "Visitor 45",
			lastName:    "Visitor 26",
			omittedName: "Visitor 25",
			itemCount:   20,
			olderPath:   "/admin/inquiries?before=26",
		},
		{
			name:           "older page",
			path:           "/admin/inquiries?before=26",
			firstName:      "Visitor 25",
			lastName:       "Visitor 6",
			omittedName:    "Visitor 26",
			itemCount:      20,
			olderPath:      "/admin/inquiries?before=6",
			returnToLatest: true,
		},
		{
			name:           "last page",
			path:           "/admin/inquiries?before=6",
			firstName:      "Visitor 5",
			lastName:       "Visitor 1",
			omittedName:    "Visitor 6",
			itemCount:      5,
			returnToLatest: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := adminHTTPNewRequest(
				http.MethodGet,
				test.path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			assertStage16AdminStatus(t, response, http.StatusOK)
			assertStage16BodyContains(
				t,
				response.Body,
				test.firstName,
				test.lastName,
			)
			assertStage16BodyOmits(t, response.Body, test.omittedName)
			if got := strings.Count(
				response.Body,
				`class="admin-inquiry-card"`,
			); got != test.itemCount {
				t.Errorf("summary card count: got %d, want %d", got, test.itemCount)
			}
			if test.olderPath == "" {
				assertStage16BodyOmits(t, response.Body, "View older inquiries")
			} else {
				assertStage16BodyContains(
					t,
					response.Body,
					`href="`+test.olderPath+`"`,
					"View older inquiries",
				)
			}
			if test.returnToLatest {
				assertStage16BodyContains(t, response.Body, "Return to latest")
			} else {
				assertStage16BodyOmits(t, response.Body, "Return to latest")
			}
		})
	}

	assertStage16InquiryCalls(t, reader, []int64{0, 26, 6}, nil)
}

// TestAdminInquiryListCursorRemainsStableAfterNewInsertion demonstrates the
// practical advantage of an exclusive ID cursor: a newly received inquiry can
// appear above page one without shifting or duplicating its original older rows.
func TestAdminInquiryListCursorRemainsStableAfterNewInsertion(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	originalInquiries := stage16InquiryRange(45)
	reader.setInquiries(originalInquiries...)
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleEditor,
		reader,
	)

	firstRequest := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries",
		nil,
		false,
		fixture.cookies()...,
	)
	firstResponse := stage16ServeAdminRequest(
		t,
		fixture.app,
		firstRequest,
	)
	assertStage16AdminStatus(t, firstResponse, http.StatusOK)
	if got := strings.Count(
		firstResponse.Body,
		`class="admin-inquiry-card"`,
	); got != adminInquiryPageSize {
		t.Errorf(
			"first-page summary count: got %d, want %d",
			got,
			adminInquiryPageSize,
		)
	}

	// The initial 45-row snapshot ends page one at ID 26. Capturing that emitted
	// link before changing the reader models a browser holding the original URL.
	originalOlderPath := "/admin/inquiries?before=26"
	assertStage16BodyContains(
		t,
		firstResponse.Body,
		`href="`+originalOlderPath+`"`,
	)
	for inquiryID := int64(45); inquiryID >= 26; inquiryID-- {
		assertStage16BodyContains(
			t,
			firstResponse.Body,
			fmt.Sprintf(`href="/admin/inquiries/%d"`, inquiryID),
		)
	}

	// ID 100 represents a submission accepted after page one was rendered. It
	// sorts above that page but cannot affect the stored exclusive bound of 26.
	withNewArrival := append(
		append([]adminInquiryDetail(nil), originalInquiries...),
		stage16Inquiry(100),
	)
	reader.setInquiries(withNewArrival...)
	secondRequest := adminHTTPNewRequest(
		http.MethodGet,
		originalOlderPath,
		nil,
		false,
		fixture.cookies()...,
	)
	secondResponse := stage16ServeAdminRequest(
		t,
		fixture.app,
		secondRequest,
	)
	assertStage16AdminStatus(t, secondResponse, http.StatusOK)
	if got := strings.Count(
		secondResponse.Body,
		`class="admin-inquiry-card"`,
	); got != adminInquiryPageSize {
		t.Errorf(
			"second-page summary count: got %d, want %d",
			got,
			adminInquiryPageSize,
		)
	}

	// Every original ID immediately below the boundary remains present exactly
	// once, while IDs from page one and the new arrival remain above the cursor.
	for inquiryID := int64(25); inquiryID >= 6; inquiryID-- {
		marker := fmt.Sprintf(`href="/admin/inquiries/%d"`, inquiryID)
		if got := strings.Count(secondResponse.Body, marker); got != 1 {
			t.Errorf(
				"second-page link for inquiry %d: got %d copies, want 1",
				inquiryID,
				got,
			)
		}
	}
	for _, inquiryID := range []int64{100, 45, 26} {
		assertStage16BodyOmits(
			t,
			secondResponse.Body,
			fmt.Sprintf(`href="/admin/inquiries/%d"`, inquiryID),
		)
	}
	assertStage16InquiryCalls(t, reader, []int64{0, 26}, nil)
}

// TestAdminInquiryListRendersTruthfulEmptyStates distinguishes a studio with no
// submissions from an exhausted older-page cursor and keeps navigation useful.
func TestAdminInquiryListRendersTruthfulEmptyStates(t *testing.T) {
	tests := []struct {
		// name identifies why the page is empty.
		name string
		// path selects the newest page or an exhausted older page.
		path string
		// cursor is the exact reader argument expected for path.
		cursor int64
		// message is the human-readable empty-state explanation.
		message string
		// returnToLatest is true only after the administrator has paginated.
		returnToLatest bool
	}{
		{
			name:    "no inquiries received",
			path:    "/admin/inquiries",
			message: "No inquiries have been received yet.",
		},
		{
			name:           "no older inquiries remain",
			path:           "/admin/inquiries?before=1",
			cursor:         1,
			message:        "No older inquiries remain.",
			returnToLatest: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				adminRoleEditor,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				test.path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			assertStage16AdminStatus(t, response, http.StatusOK)
			assertStage16BodyContains(
				t,
				response.Body,
				"No inquiries to display",
				test.message,
			)
			assertStage16BodyOmits(t, response.Body, "admin-inquiry-card")
			if test.returnToLatest {
				assertStage16BodyContains(t, response.Body, "Return to latest")
			} else {
				assertStage16BodyOmits(t, response.Body, "Return to latest")
			}
			assertStage16InquiryCalls(t, reader, []int64{test.cursor}, nil)
		})
	}
}

// TestAdminInquiryListLimitsPersonalDataAndEscapesNames checks the deliberate
// summary-only view model and html/template's contextual escaping boundary.
func TestAdminInquiryListLimitsPersonalDataAndEscapesNames(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	inquiry := stage16Inquiry(321)
	inquiry.Name = `<script>alert("visitor")</script>`
	inquiry.Email = "private-list-address@example.test"
	inquiry.Message = "PRIVATE LIST MESSAGE MUST NOT APPEAR"
	inquiry.Discipline = "interior-design"
	inquiry.Status = inquiryStatusArchived
	inquiry.CreatedAt = time.Date(2033, 5, 6, 7, 8, 9, 0, time.UTC)
	inquiry.UpdatedAt = inquiry.CreatedAt
	reader.setInquiries(inquiry)
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleOwner,
		reader,
	)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)

	assertStage16AdminStatus(t, response, http.StatusOK)
	assertStage16BodyContains(
		t,
		response.Body,
		html.EscapeString(inquiry.Name),
		"Inquiry #000321",
		`href="/admin/inquiries/321"`,
		"Interior Design",
		"Archived",
		`datetime="2033-05-06T07:08:09Z"`,
		"06 May 2033, 07:08 UTC",
	)

	// The overview needs enough information to choose a record, but exposing
	// reply addresses, full messages, idempotency keys, or raw HTML is excessive.
	assertStage16BodyOmits(
		t,
		response.Body,
		inquiry.Name,
		inquiry.Email,
		inquiry.Message,
		"submission_key",
		"submission_token",
		fixture.sessionToken,
		fixture.user.PasswordHash,
	)
	assertStage16InquiryCalls(t, reader, []int64{0}, nil)
}

// TestAdminInquiryDetailRendersEscapedReadOnlyRecord verifies the full record's
// trusted labels and UTC time while keeping the browser title and secret values
// independent of visitor-controlled content.
func TestAdminInquiryDetailRendersEscapedReadOnlyRecord(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	inquiry := stage16Inquiry(417)
	inquiry.Name = `<b>Ada & "Co"</b>`
	inquiry.Email = "visitor+private@example.test"
	inquiry.Discipline = "architecture-design"
	inquiry.Message = "<img src=x onerror=alert(1)>\nSecond & final line"
	inquiry.Status = inquiryStatusReviewed
	inquiry.CreatedAt = time.Date(
		2031,
		time.July,
		8,
		10,
		20,
		11,
		0,
		time.FixedZone("IRST", 3*60*60+30*60),
	)
	inquiry.UpdatedAt = inquiry.CreatedAt
	reader.setInquiries(inquiry)
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleEditor,
		reader,
	)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries/417",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)

	assertStage16AdminStatus(t, response, http.StatusOK)
	assertStage16BodyContains(
		t,
		response.Body,
		"<title>Inquiry detail | Zafarmand Admin</title>",
		html.EscapeString(inquiry.Name),
		"visitor&#43;private@example.test",
		"Inquiry #000417",
		"Architecture Design",
		"Reviewed",
		`admin-inquiry-status--reviewed`,
		`datetime="2031-07-08T06:50:11Z"`,
		"08 Jul 2031, 06:50 UTC",
		html.EscapeString(inquiry.Message),
		"Project message",
	)

	// A detail may display the visitor payload as escaped text, but the page
	// omits raw markup, submission keys, bearer tokens, and stored hash material.
	assertStage16BodyOmits(
		t,
		response.Body,
		inquiry.Name,
		"<img src=x onerror=alert(1)>",
		"submission_key",
		"submission_token",
		fixture.sessionToken,
		fixture.user.PasswordHash,
		base64.RawURLEncoding.EncodeToString(fixture.sessionHash),
		base64.RawURLEncoding.EncodeToString(fixture.csrfHash),
		"<title>"+html.EscapeString(inquiry.Name),
	)
	assertStage16InquiryCalls(t, reader, nil, []int64{417})
}

// TestAdminInquiryDetailReturnsNotFoundForMissingRecord confirms that a valid
// but absent ID produces a generic 404 after one exact repository lookup.
func TestAdminInquiryDetailReturnsNotFoundForMissingRecord(t *testing.T) {
	reader := newRecordingAdminInquiryReader()
	fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
		t,
		adminRoleOwner,
		reader,
	)
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries/999",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)

	assertStage16AdminStatus(t, response, http.StatusNotFound)
	assertStage16BodyContains(t, response.Body, "404 page not found")
	assertStage16BodyOmits(t, response.Body, adminHTTPTestEmail)
	assertStage16InquiryCalls(t, reader, nil, []int64{999})
}

// TestAdminInquiryReaderFailuresReturnGenericServiceResponse covers both read
// operations and ensures injected diagnostics or visitor data are not reflected.
func TestAdminInquiryReaderFailuresReturnGenericServiceResponse(t *testing.T) {
	privateDiagnostic := "SELECT failed for private-reader@example.test"
	tests := []struct {
		// name identifies the repository operation under test.
		name string
		// path selects either the queue or one exact detail record.
		path string
		// arrange installs the private diagnostic on the matching operation.
		arrange func(*recordingAdminInquiryReader)
		// wantList and wantFind prove only the expected method was reached.
		wantList []int64
		wantFind []int64
	}{
		{
			name: "list read failure",
			path: "/admin/inquiries?before=55",
			arrange: func(reader *recordingAdminInquiryReader) {
				reader.listErr = errors.New(privateDiagnostic)
			},
			wantList: []int64{55},
		},
		{
			name: "detail read failure",
			path: "/admin/inquiries/55",
			arrange: func(reader *recordingAdminInquiryReader) {
				reader.findErr = errors.New(privateDiagnostic)
			},
			wantFind: []int64{55},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			test.arrange(reader)
			fixture := newAdminHTTPAuthenticatedFixtureWithInquiryReader(
				t,
				adminRoleEditor,
				reader,
			)
			request := adminHTTPNewRequest(
				http.MethodGet,
				test.path,
				nil,
				false,
				fixture.cookies()...,
			)
			response := stage16ServeAdminRequest(t, fixture.app, request)

			// The public failure contract is retryable and account-neutral. Driver
			// text, visitor addresses, path details, and authenticated identity stay out.
			assertStage16AdminStatus(
				t,
				response,
				http.StatusServiceUnavailable,
			)
			assertStage16BodyContains(
				t,
				response.Body,
				"service temporarily unavailable",
			)
			assertStage16BodyOmits(
				t,
				response.Body,
				privateDiagnostic,
				"private-reader@example.test",
				adminHTTPTestEmail,
				test.path,
			)
			assertStage16InquiryCalls(
				t,
				reader,
				test.wantList,
				test.wantFind,
			)
		})
	}
}

// TestAdminInquiryListRejectsMalformedReaderPage proves the handler validates
// pagination invariants independently and does not render malformed item data.
func TestAdminInquiryListRejectsMalformedReaderPage(t *testing.T) {
	privateName := "PRIVATE MALFORMED READER VALUE"
	malformedReader := &stage16MalformedInquiryReader{
		page: adminInquiryPage{
			Items: []adminInquirySummary{
				{
					ID:         7,
					Name:       privateName,
					Discipline: "products",
					Status:     inquiryStatusNew,
					CreatedAt:  time.Now().UTC(),
				},
			},
			HasMore:      true,
			NextBeforeID: 999,
		},
	}
	fixture := newAdminHTTPAuthenticatedFixture(t, adminRoleOwner)
	fixture.app.adminInquiries = malformedReader
	request := adminHTTPNewRequest(
		http.MethodGet,
		"/admin/inquiries",
		nil,
		false,
		fixture.cookies()...,
	)
	response := stage16ServeAdminRequest(t, fixture.app, request)

	// An invalid HasMore/cursor relationship becomes the same generic service
	// failure as corrupt database data; neither the item nor cursor is reflected.
	assertStage16AdminStatus(t, response, http.StatusServiceUnavailable)
	assertStage16BodyContains(
		t,
		response.Body,
		"service temporarily unavailable",
	)
	assertStage16BodyOmits(t, response.Body, privateName, "999")
}

// TestAdminInquiryRouteShapeRejectsMethodsAndExtraSegments verifies ServeMux's
// exact method/segment contract and the outer security policy on generated errors.
func TestAdminInquiryRouteShapeRejectsMethodsAndExtraSegments(t *testing.T) {
	tests := []struct {
		// name describes the unsupported route shape.
		name string
		// method and path form the request target.
		method string
		path   string
		// status is generated by ServeMux without entering a reader handler.
		status int
		// wantsAllow checks the method hint on a 405 response.
		wantsAllow bool
	}{
		{
			name:       "list POST",
			method:     http.MethodPost,
			path:       "/admin/inquiries",
			status:     http.StatusMethodNotAllowed,
			wantsAllow: true,
		},
		{
			name:       "detail PUT",
			method:     http.MethodPut,
			path:       "/admin/inquiries/7",
			status:     http.StatusMethodNotAllowed,
			wantsAllow: true,
		},
		{
			name:   "detail extra segment",
			method: http.MethodGet,
			path:   "/admin/inquiries/7/extra",
			status: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingAdminInquiryReader()
			repository := newRecordingAdminRepository()
			passwords := newTestAdminPasswordManager(t)
			app := newAdminHTTPTestApplicationWithInquiryReader(
				t,
				repository,
				passwords,
				reader,
			)
			request := adminHTTPNewRequest(
				test.method,
				test.path,
				nil,
				false,
			)
			response := stage16ServeAdminRequest(t, app, request)

			assertStage16AdminStatus(t, response, test.status)
			if test.wantsAllow && !strings.Contains(
				response.Header.Get("Allow"),
				http.MethodGet,
			) {
				t.Errorf(
					"Allow: got %q, want GET capability",
					response.Header.Get("Allow"),
				)
			}
			assertStage16InquiryCalls(t, reader, nil, nil)
		})
	}
}

// TestRequireAdminRolesFailsClosed exercises authorization independently from
// session middleware so empty, invalid, and future-role configurations cannot
// accidentally reach a protected handler.
func TestRequireAdminRolesFailsClosed(t *testing.T) {
	tests := []struct {
		// name records the authorization condition.
		name string
		// roles build the middleware's immutable allowlist.
		roles []adminRole
		// contextRole is installed only when hasIdentity is true.
		contextRole adminRole
		// hasIdentity distinguishes absent authentication from unsupported roles.
		hasIdentity bool
		// wantStatus is 204 only for an explicitly allowed current role.
		wantStatus int
		// wantNext confirms whether the protected handler executed.
		wantNext bool
	}{
		{
			name:       "missing authenticated context",
			roles:      []adminRole{adminRoleOwner},
			wantStatus: http.StatusForbidden,
		},
		{
			name:        "empty allowlist",
			contextRole: adminRoleOwner,
			hasIdentity: true,
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "invalid configured role ignored",
			roles:       []adminRole{"future-role"},
			contextRole: adminRoleOwner,
			hasIdentity: true,
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "future identity denied by current allowlist",
			roles:       []adminRole{adminRoleOwner, adminRoleEditor},
			contextRole: "future-role",
			hasIdentity: true,
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "explicit owner access",
			roles:       []adminRole{adminRoleOwner},
			contextRole: adminRoleOwner,
			hasIdentity: true,
			wantStatus:  http.StatusNoContent,
			wantNext:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusNoContent)
			})
			handler := requireAdminRoles(test.roles...)(next)
			request := httptest.NewRequest(
				http.MethodGet,
				"http://example.test/admin/inquiries",
				nil,
			)
			if test.hasIdentity {
				identity := authenticatedAdminRequest{
					Identity: adminIdentity{Role: test.contextRole},
				}
				request = request.WithContext(context.WithValue(
					request.Context(),
					authenticatedAdminContextKey{},
					identity,
				))
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// Authorization denies by default and calls next only for membership in
			// the explicit valid-role set; no database role expands access implicitly.
			if recorder.Code != test.wantStatus {
				t.Errorf(
					"status: got %d, want %d",
					recorder.Code,
					test.wantStatus,
				)
			}
			if nextCalled != test.wantNext {
				t.Errorf("next called: got %t, want %t", nextCalled, test.wantNext)
			}
		})
	}
}
