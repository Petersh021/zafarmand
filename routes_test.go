package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestApplication builds an application for a test and stops that test
// immediately if shared initialization fails.
//
// Calling t.Helper marks this function as test infrastructure so failure line
// numbers point to its caller, where the failed setup was requested.
func newTestApplication(t *testing.T) *application {
	t.Helper()

	app, err := newApplication()
	if err != nil {
		t.Fatalf("create test application: %v", err)
	}

	return app
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
	}{
		{
			name:        "home",
			path:        "/",
			currentPath: "/",
			title:       "Home",
			activeLinks: 1,
		},
		{
			name:        "products",
			path:        "/products",
			currentPath: "/products",
			title:       "Products",
			activeLinks: 2,
		},
		{
			name:        "interior design",
			path:        "/interior-design",
			currentPath: "/interior-design",
			title:       "Interior Design",
			activeLinks: 2,
		},
		{
			name:        "architecture design",
			path:        "/architecture-design",
			currentPath: "/architecture-design",
			title:       "Architecture Design",
			activeLinks: 2,
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

			// Discipline pages render active links in both the desktop
			// navigation and drawer; the homepage renders only its drawer link.
			activeLinks := strings.Count(
				string(body),
				`aria-current="page"`,
			)

			if activeLinks != test.activeLinks {
				t.Errorf(
					"active links: got %d, want %d",
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
	}

	for _, content := range expectedContent {
		if !strings.Contains(body, content) {
			t.Errorf(
				"response does not contain %q",
				content,
			)
		}
	}

	// Lazy loading an above-the-fold hero delays the page's largest visual
	// element, so this assertion protects the intentional eager-loading choice.
	if strings.Contains(body, `loading="lazy"`) {
		t.Error("above-the-fold hero image must not be lazy-loaded")
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
func TestPageRoutesRejectUnsupportedMethods(t *testing.T) {
	app := newTestApplication(t)
	handler := app.routes()

	// All current public pages share the same GET-only method policy.
	paths := []string{
		"/",
		"/products",
		"/interior-design",
		"/architecture-design",
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
