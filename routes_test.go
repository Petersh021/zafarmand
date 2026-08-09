package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recordingInquiryRepository is the database-free Contact persistence double
// shared by route tests.
//
// It records immutable copies of submitted values and returns a configurable
// outcome. The mutex keeps the helper correct if a future test serves requests
// concurrently or opts into t.Parallel.
type recordingInquiryRepository struct {
	mu          sync.Mutex
	submissions []inquirySubmission
	result      inquiryCreateResult
	err         error
}

// Create implements inquiryRepository without opening PostgreSQL. Copying the
// byte slice prevents a caller from mutating the recorded idempotency key after
// the assertion boundary.
func (repository *recordingInquiryRepository) Create(
	_ context.Context,
	submission inquirySubmission,
) (inquiryCreateResult, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	submission.SubmissionKey = append(
		[]byte(nil),
		submission.SubmissionKey...,
	)
	repository.submissions = append(
		repository.submissions,
		submission,
	)

	return repository.result, repository.err
}

// snapshot returns a separate slice for assertions so tests never read shared
// state while another request could append to it.
func (repository *recordingInquiryRepository) snapshot() []inquirySubmission {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	result := make(
		[]inquirySubmission,
		len(repository.submissions),
	)
	copy(result, repository.submissions)

	return result
}

// setOutcome changes the next repository result under the same lock used by
// Create. It lets a sequential retry test model PostgreSQL recovering without
// replacing the application dependency between requests.
func (repository *recordingInquiryRepository) setOutcome(
	result inquiryCreateResult,
	err error,
) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	repository.result = result
	repository.err = err
}

// newTestApplication builds an application for a test and stops that test
// immediately if shared initialization fails.
//
// Calling t.Helper marks this function as test infrastructure so failure line
// numbers point to its caller, where the failed setup was requested.
func newTestApplication(t *testing.T) *application {
	t.Helper()

	repository := &recordingInquiryRepository{
		result: inquiryCreateResultCreated,
	}

	return newTestApplicationWithInquiryRepository(
		t,
		repository,
	)
}

// newTestApplicationWithInquiryRepository builds the normal template and route
// graph around a caller-controlled Contact persistence outcome.
func newTestApplicationWithInquiryRepository(
	t *testing.T,
	repository inquiryRepository,
) *application {
	t.Helper()

	adminRepository := newRecordingAdminRepository()
	passwords := newTestAdminPasswordManager(t)
	app, err := newApplication(
		repository,
		adminRepository,
		newRecordingAdminInquiryReader(),
		passwords,
	)
	if err != nil {
		t.Fatalf("create test application: %v", err)
	}

	return app
}

// TestNewApplicationRequiresInquiryRepository protects the production
// composition boundary: a server cannot start with a Contact form that has no
// persistence dependency.
func TestNewApplicationRequiresInquiryRepository(t *testing.T) {
	app, err := newApplication(
		nil,
		newRecordingAdminRepository(),
		newRecordingAdminInquiryReader(),
		newTestAdminPasswordManager(t),
	)
	if !errors.Is(err, errInquiryRepositoryRequired) {
		t.Fatalf(
			"nil repository error: got %v, want required sentinel",
			err,
		)
	}
	if app != nil {
		t.Error("nil repository returned a usable application")
	}
}

// assertHomeDisciplineEntrances verifies the number, fields, and order of the
// discipline rows inside the homepage section.
//
// Starting at the section id prevents repeated links in the shared header or
// drawer from satisfying these assertions. Moving through the remaining
// substring after every match proves the template preserves the Go slice order.
func assertHomeDisciplineEntrances(
	t *testing.T,
	body string,
	entrances []disciplineEntranceData,
) {
	t.Helper()

	if count := strings.Count(
		body,
		`class="home-discipline"`,
	); count != len(entrances) {
		t.Fatalf(
			"discipline entrance count: got %d, want %d",
			count,
			len(entrances),
		)
	}

	sectionPosition := strings.Index(body, `id="disciplines"`)
	if sectionPosition == -1 {
		t.Fatal("response does not contain the disciplines section")
	}

	remainingBody := body[sectionPosition:]

	for _, entrance := range entrances {
		pathPosition := strings.Index(
			remainingBody,
			`href="`+entrance.Path+`"`,
		)
		numberPosition := strings.Index(
			remainingBody,
			entrance.Number,
		)
		namePosition := strings.Index(
			remainingBody,
			entrance.Name,
		)

		if pathPosition == -1 ||
			numberPosition == -1 ||
			namePosition == -1 {
			t.Fatalf(
				"response does not contain entrance %q at %q",
				entrance.Name,
				entrance.Path,
			)
		}

		if numberPosition < pathPosition ||
			namePosition < numberPosition {
			t.Fatalf(
				"entrance %q fields are not in template order",
				entrance.Name,
			)
		}

		remainingBody = remainingBody[namePosition+len(entrance.Name):]
	}
}

// TestPageRoutes verifies the shared contract of every public page route:
// successful GET responses, correct title and active navigation state, and a
// homepage hero that appears only at the root URL.
//
// A table-driven test keeps identical assertions in one place while each row
// documents the inputs and expected output for a route.
func TestPageRoutes(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	tests := []struct {
		// name labels the subtest in verbose test output.
		name string
		// path is the URL sent through the real application router.
		path string
		// currentPath is the value expected in the rendered body attribute.
		currentPath string
		// title is the page-specific portion of the document title.
		title string
		// activeLinks accounts for desktop and drawer versions of navigation.
		activeLinks int
		// activeToken distinguishes exact pages from nested section locations.
		activeToken string
	}{
		{
			name:        "home",
			path:        "/",
			currentPath: "/",
			title:       "Home",
			activeLinks: 1,
			activeToken: "page",
		},
		{
			name:        "products",
			path:        "/products",
			currentPath: "/products",
			title:       "Products",
			activeLinks: 2,
			activeToken: "page",
		},
		{
			name:        "product detail",
			path:        "/products/furniture-study-01",
			currentPath: "/products/furniture-study-01",
			title:       "Furniture Study 01",
			activeLinks: 2,
			activeToken: "location",
		},
		{
			name:        "interior design",
			path:        "/interior-design",
			currentPath: "/interior-design",
			title:       "Interior Design",
			activeLinks: 2,
			activeToken: "page",
		},
		{
			name:        "interior project detail",
			path:        "/interior-design/interior-study-01",
			currentPath: "/interior-design/interior-study-01",
			title:       "Interior Study 01",
			activeLinks: 2,
			activeToken: "location",
		},
		{
			name:        "architecture design",
			path:        "/architecture-design",
			currentPath: "/architecture-design",
			title:       "Architecture Design",
			activeLinks: 2,
			activeToken: "page",
		},
		{
			name:        "architecture project detail",
			path:        "/architecture-design/architecture-study-01",
			currentPath: "/architecture-design/architecture-study-01",
			title:       "Architecture Study 01",
			activeLinks: 2,
			activeToken: "location",
		},
		{
			name:        "contact",
			path:        "/contact",
			currentPath: "/contact",
			title:       "Contact",
			activeLinks: 1,
			activeToken: "page",
		},
	}

	for _, test := range tests {
		// Each row runs as a named subtest, making a route failure easy to locate.
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				test.path,
				nil,
			)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			// Result exposes the recorded response using the same shape as a
			// response returned by an HTTP client.
			response := recorder.Result()
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK {
				t.Fatalf(
					"status code: got %d, want %d",
					response.StatusCode,
					http.StatusOK,
				)
			}

			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}

			// The base template publishes CurrentPath for navigation behavior
			// and for browser-level responsive tests.
			expectedPath := `data-current-path="` +
				test.currentPath +
				`"`

			if !strings.Contains(string(body), expectedPath) {
				t.Errorf(
					"response does not contain %q",
					expectedPath,
				)
			}

			// Checking the complete title catches both incorrect handler data
			// and accidental changes to the shared title format.
			expectedTitle := "<title>" +
				test.title +
				" | Zafarmand</title>"

			if !strings.Contains(string(body), expectedTitle) {
				t.Errorf(
					"response does not contain %q",
					expectedTitle,
				)
			}

			// Discipline pages and nested detail pages render their active parent
			// in desktop and drawer navigation; Home uses one drawer link.
			expectedCurrent := `aria-current="` +
				test.activeToken +
				`"`
			activeLinks := strings.Count(
				string(body),
				expectedCurrent,
			)

			if activeLinks != test.activeLinks {
				t.Errorf(
					"%s active links: got %d, want %d",
					test.activeToken,
					activeLinks,
					test.activeLinks,
				)
			}

			// Only GET / receives HomeHero data and uses the homepage template.
			hasHomeHero := strings.Contains(
				string(body),
				`class="home-hero"`,
			)

			if hasHomeHero != (test.path == "/") {
				t.Errorf(
					"home hero presence: got %t, want %t",
					hasHomeHero,
					test.path == "/",
				)
			}

			// Homepage presentation, the Stage 4C fragment link, and its
			// destination belong to the root route only. Checking all three
			// protects other public templates from inheriting homepage-only
			// assets or markup.
			hasHomeStyles := strings.Contains(
				string(body),
				`href="/static/css/home.css"`,
			)
			hasHomeScrollCue := strings.Contains(
				string(body),
				`href="#disciplines"`,
			)
			hasDisciplineTarget := strings.Contains(
				string(body),
				`id="disciplines"`,
			)
			wantsHomeComposition := test.path == "/"

			if hasHomeStyles != wantsHomeComposition {
				t.Errorf(
					"home stylesheet presence: got %t, want %t",
					hasHomeStyles,
					wantsHomeComposition,
				)
			}

			if hasHomeScrollCue != wantsHomeComposition {
				t.Errorf(
					"home scroll cue presence: got %t, want %t",
					hasHomeScrollCue,
					wantsHomeComposition,
				)
			}

			if hasDisciplineTarget != wantsHomeComposition {
				t.Errorf(
					"discipline target presence: got %t, want %t",
					hasDisciplineTarget,
					wantsHomeComposition,
				)
			}
		})
	}
}

// TestHomeHero checks the performance- and accessibility-relevant contract of
// the above-the-fold homepage image and its visible identity content.
//
// The image includes intrinsic dimensions to reduce layout shift and receives
// high fetch priority instead of lazy loading because it is immediately visible.
func TestHomeHero(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	expectedContent := []string{
		`href="/static/css/home.css"`,
		`src="/static/images/home-hero-placeholder.jpg"`,
		`width="1536"`,
		`height="1024"`,
		`fetchpriority="high"`,
		`<h1`,
		`Zafarmand`,
		`Design Studio`,
		`Explore disciplines`,
	}

	for _, content := range expectedContent {
		if !strings.Contains(body, content) {
			t.Errorf(
				"response does not contain %q",
				content,
			)
		}
	}

	// Inspect only the hero <img> start tag. Future below-the-fold images should
	// be allowed to use loading="lazy" without weakening the eager hero contract.
	heroImageClassPosition := strings.Index(
		body,
		`class="home-hero__image"`,
	)
	if heroImageClassPosition == -1 {
		t.Fatal("response does not contain the hero image")
	}

	// Starting at the preceding <img catches a future loading attribute whether
	// it appears before or after class in the template's attribute order.
	heroImageStart := strings.LastIndex(
		body[:heroImageClassPosition],
		"<img",
	)
	if heroImageStart == -1 {
		t.Fatal("hero image class is not on an img element")
	}

	heroImageEnd := strings.Index(body[heroImageStart:], ">")
	if heroImageEnd == -1 {
		t.Fatal("hero image start tag is not closed")
	}

	heroImageTag := body[heroImageStart : heroImageStart+heroImageEnd+1]
	if strings.Contains(heroImageTag, `loading="lazy"`) {
		t.Error("above-the-fold hero image must not be lazy-loaded")
	}
}

// TestHomeHeroRailUsesAvailablePageData proves the supporting rail reads the
// current hero fields and derives its count from the current discipline slice.
//
// Sentinel text distinguishes the rail from production handler values, while a
// two-item slice catches an accidentally hard-coded production count of three.
func TestHomeHeroRailUsesAvailablePageData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	hero := homeHeroData{
		StudioName: "Rail Studio Sentinel",
		Descriptor: "Rail Descriptor Sentinel",
	}
	entrances := []disciplineEntranceData{
		{
			Number: "S-01",
			Name:   "First Rail Discipline",
			Path:   "/first-rail-discipline",
		},
		{
			Number: "S-02",
			Name:   "Second Rail Discipline",
			Path:   "/second-rail-discipline",
		},
	}

	app.render(
		recorder,
		http.StatusOK,
		"home.html",
		pageData{
			Title:           "Rail Sentinel",
			CurrentPath:     "/",
			HomeHero:        &hero,
			HomeDisciplines: entrances,
		},
	)

	body := recorder.Body.String()
	railStart := strings.Index(body, `class="home-hero__rail"`)
	if railStart == -1 {
		t.Fatal("response does not contain the hero rail")
	}

	heroEnd := strings.Index(body[railStart:], "</section>")
	if heroEnd == -1 {
		t.Fatal("hero section is not closed after the rail")
	}

	// Collapsing formatting whitespace makes the visible count assertion
	// independent of template indentation while preserving element boundaries.
	rail := strings.Join(
		strings.Fields(body[railStart:railStart+heroEnd]),
		" ",
	)
	expectedRailContent := []string{
		"Rail Studio Sentinel",
		"Rail Descriptor Sentinel",
		"> 2 </span>",
		"Explore disciplines",
	}

	for _, content := range expectedRailContent {
		if !strings.Contains(rail, content) {
			t.Errorf(
				"hero rail does not contain %q",
				content,
			)
		}
	}
}

// TestHomeHeroOmitsScrollCueWithoutDisciplines verifies that a hero can render
// independently without advertising a fragment destination that is not present.
//
// This protects the shared template if a future handler temporarily has hero
// data but no published discipline entrances.
func TestHomeHeroOmitsScrollCueWithoutDisciplines(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	hero := homeHeroData{
		StudioName: "Hero Without Disciplines",
		Descriptor: "Sentinel Descriptor",
	}

	app.render(
		recorder,
		http.StatusOK,
		"home.html",
		pageData{
			Title:       "Empty Discipline Sentinel",
			CurrentPath: "/",
			HomeHero:    &hero,
		},
	)

	body := recorder.Body.String()
	if strings.Contains(body, `href="#disciplines"`) {
		t.Error("hero with no disciplines must not render a scroll cue")
	}

	if strings.Contains(body, `id="disciplines"`) {
		t.Error("empty discipline data must not render a fragment target")
	}
}

// TestHomeScrollCueTargetsDisciplines verifies that the Stage 4C call to action
// is one native fragment link with one unambiguous destination in the document.
//
// Exact counts catch duplicate ids, duplicate hero controls, and fragment links
// accidentally copied into shared navigation. A native anchor requires no
// JavaScript to support keyboard activation or browser history.
func TestHomeScrollCueTargetsDisciplines(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	scrollCue := `href="#disciplines"`
	disciplineTarget := `id="disciplines"`

	if count := strings.Count(body, scrollCue); count != 1 {
		t.Errorf(
			"scroll cue count: got %d, want 1",
			count,
		)
	}

	if count := strings.Count(body, disciplineTarget); count != 1 {
		t.Errorf(
			"discipline target count: got %d, want 1",
			count,
		)
	}

	// Locate the start of the element carrying href and require a real anchor.
	// An href attribute on an arbitrary element would not provide native link
	// semantics despite satisfying the fragment-count assertion above.
	scrollCuePosition := strings.Index(body, scrollCue)
	if scrollCuePosition == -1 {
		t.Fatal("response does not contain the scroll cue")
	}

	scrollCueStart := strings.LastIndex(
		body[:scrollCuePosition],
		"<",
	)
	if scrollCueStart == -1 {
		t.Fatal("scroll cue does not have an opening element")
	}

	scrollCueOpening := strings.TrimSpace(
		body[scrollCueStart:scrollCuePosition],
	)
	if !strings.HasPrefix(scrollCueOpening, "<a") {
		t.Errorf(
			"scroll cue opening element: got %q, want an anchor",
			scrollCueOpening,
		)
	}
}

// TestHomeDisciplineEntrancesUseTemplateData renders the homepage with custom
// sentinel values to prove the entrance markup comes from pageData and one
// template range rather than three hard-coded HTML links.
func TestHomeDisciplineEntrancesUseTemplateData(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()

	entrances := []disciplineEntranceData{
		{
			Number: "S-01",
			Name:   "First Sentinel",
			Path:   "/first-sentinel",
		},
		{
			Number: "S-02",
			Name:   "Second Sentinel",
			Path:   "/second-sentinel",
		},
	}

	app.render(
		recorder,
		http.StatusOK,
		"home.html",
		pageData{
			Title:           "Sentinel",
			CurrentPath:     "/",
			HomeDisciplines: entrances,
		},
	)

	body := recorder.Body.String()
	assertHomeDisciplineEntrances(t, body, entrances)
}

// TestHomeDisciplineEntrances verifies the production homepage contains exactly
// three unique entrances, keeps their intended visual order, and points every
// link at a registered server-rendered route.
func TestHomeDisciplineEntrances(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	expectedEntrances := []disciplineEntranceData{
		{
			Number: "01",
			Name:   "Interior Design",
			Path:   "/interior-design",
		},
		{
			Number: "02",
			Name:   "Architecture Design",
			Path:   "/architecture-design",
		},
		{
			Number: "03",
			Name:   "Products",
			Path:   "/products",
		},
	}

	assertHomeDisciplineEntrances(
		t,
		body,
		expectedEntrances,
	)

	for _, entrance := range expectedEntrances {
		// Sending the destination through the application router verifies the
		// homepage never advertises an unregistered or placeholder URL.
		routeRecorder := httptest.NewRecorder()
		routeRequest := httptest.NewRequest(
			http.MethodGet,
			entrance.Path,
			nil,
		)

		handler.ServeHTTP(routeRecorder, routeRequest)

		if routeRecorder.Code != http.StatusOK {
			t.Errorf(
				"discipline path %q status: got %d, want %d",
				entrance.Path,
				routeRecorder.Code,
				http.StatusOK,
			)
		}
	}
}

// TestUnknownRoute verifies that an unregistered URL produces 404 Not Found
// instead of accidentally falling through to the homepage.
func TestUnknownRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/not-a-route",
		nil,
	)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}
}

// TestPageRoutesRejectUnsupportedMethods verifies that method-aware ServeMux
// patterns return 405 Method Not Allowed for POST requests to read-only pages.
// Contact is intentionally absent because its persistence workflow accepts POST;
// contact-specific method boundaries are covered in contact_test.go.
func TestPageRoutesRejectUnsupportedMethods(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	// Every route in this table remains a read-only GET resource.
	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/interior-design",
		"/interior-design/interior-study-01",
		"/architecture-design",
		"/architecture-design/architecture-study-01",
	}

	for _, path := range paths {
		// The callback isolates each URL as a named subtest while reusing the
		// same application handler and method assertion.
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				path,
				nil,
			)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf(
					"status code: got %d, want %d",
					recorder.Code,
					http.StatusMethodNotAllowed,
				)
			}
		})
	}
}

// TestStaticFileRoute sends a CSS request through the application router to
// verify both the /static/ path mapping and the response media type.
func TestStaticFileRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/static/css/main.css",
		nil,
	)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code: got %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	contentType := recorder.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/css") {
		t.Errorf(
			"content type: got %q, want text/css",
			contentType,
		)
	}
}
