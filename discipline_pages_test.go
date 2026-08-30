package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// extractMainElement returns the response substring from the one opening main
// tag through its closing tag.
func extractMainElement(t *testing.T, body string) string {
	t.Helper()

	mainStart := strings.Index(body, "<main")
	if mainStart == -1 {
		t.Fatal("response does not contain a main element")
	}
	mainEnd := strings.Index(body[mainStart:], "</main>")
	if mainEnd == -1 {
		t.Fatal("response main element is not closed")
	}

	return body[mainStart : mainStart+mainEnd+len("</main>")]
}

// normalizeHTMLWhitespace collapses template indentation and line breaks into
// browser-equivalent single spaces.
func normalizeHTMLWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// extractElementByMarker isolates one complete non-void HTML element whose
// opening tag or text contains marker.
func extractElementByMarker(
	t *testing.T,
	source string,
	marker string,
	tagName string,
) string {
	t.Helper()

	markerPosition := strings.Index(source, marker)
	if markerPosition == -1 {
		t.Fatalf("source does not contain marker %q", marker)
	}
	elementStart := strings.LastIndex(source[:markerPosition], "<")
	if elementStart == -1 {
		t.Fatalf("marker %q does not follow an opening tag", marker)
	}
	expectedOpening := "<" + tagName
	if !strings.HasPrefix(source[elementStart:], expectedOpening) {
		t.Fatalf(
			"marker %q belongs to %q, want %q",
			marker,
			source[elementStart:markerPosition],
			expectedOpening,
		)
	}
	closingTag := "</" + tagName + ">"
	elementEnd := strings.Index(source[elementStart:], closingTag)
	if elementEnd == -1 {
		t.Fatalf("%s element for marker %q is not closed", tagName, marker)
	}

	return source[elementStart : elementStart+elementEnd+len(closingTag)]
}

// extractOpeningTag returns the first start tag from an isolated element.
func extractOpeningTag(t *testing.T, element string) string {
	t.Helper()

	openingEnd := strings.Index(element, ">")
	if openingEnd == -1 {
		t.Fatal("element does not contain a complete opening tag")
	}

	return element[:openingEnd+1]
}

// TestDisciplinePresentationDoesNotLeakIntoHomepage verifies that retaining
// the dormant compatibility partial does not execute its blocks on the root.
func TestDisciplinePresentationDoesNotLeakIntoHomepage(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/", nil),
	)

	body := recorder.Body.String()
	if strings.Contains(body, `href="/static/css/discipline.css"`) {
		t.Error("homepage must not load the discipline stylesheet")
	}
	if strings.Contains(body, `class="discipline-page"`) {
		t.Error("homepage must not render discipline-page structure")
	}
	if count := strings.Count(body, `href="/static/css/home.css"`); count != 1 {
		t.Errorf("homepage stylesheet count: got %d, want 1", count)
	}
}

// TestDisciplineStylesheetRoute keeps the dormant compatibility asset's static
// file contract explicit until that legacy partial is removed in its own change.
func TestDisciplineStylesheetRoute(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/static/css/discipline.css", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", recorder.Code)
	}
	contentType := recorder.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/css") {
		t.Errorf("content type: got %q, want text/css", contentType)
	}
	if !strings.Contains(recorder.Body.String(), ".discipline-page") {
		t.Error("stylesheet response does not contain discipline-page rules")
	}
}
