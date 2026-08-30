package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var referenceMenuHrefPattern = regexp.MustCompile(`href="([^"]+)"`)
var referenceMenuHookPattern = regexp.MustCompile(`\sdata-reference-menu(?:\s|>)`)

// TestReferenceMenuIsConsistentAcrossPublicPages protects the one approved
// desktop rail while retaining the shared compact and no-script navigation.
func TestReferenceMenuIsConsistentAcrossPublicPages(t *testing.T) {
	t.Parallel()

	app := newTestApplication(t)
	handler := app.routes()
	routes := []struct {
		path            string
		projectsPath    string
		includesHome    bool
		usesSharedStyle bool
	}{
		{path: "/", projectsPath: "#disciplines"},
		{path: "/products", projectsPath: "#selected-work", includesHome: true, usesSharedStyle: true},
		{path: "/products/furniture-study-01", projectsPath: "/products#selected-work", includesHome: true, usesSharedStyle: true},
		{path: "/interior-design", projectsPath: "#selected-work", includesHome: true},
		{path: "/interior-design/interior-study-01", projectsPath: "/interior-design#selected-work", includesHome: true, usesSharedStyle: true},
		{path: "/architecture-design", projectsPath: "#selected-work", includesHome: true, usesSharedStyle: true},
		{path: "/architecture-design/architecture-study-01", projectsPath: "/architecture-design#selected-work", includesHome: true, usesSharedStyle: true},
		{path: "/contact", projectsPath: "/#disciplines", includesHome: true, usesSharedStyle: true},
	}

	for _, route := range routes {
		route := route
		t.Run(route.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, route.path, nil),
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200", recorder.Code)
			}
			body := recorder.Body.String()

			if count := len(referenceMenuHookPattern.FindAllStringIndex(body, -1)); count != 1 {
				t.Fatalf("reference menu count: got %d, want 1", count)
			}
			if count := strings.Count(body, "<details"); count != 2 {
				t.Errorf("native disclosure count: got %d, want reference plus fallback", count)
			}
			if count := strings.Count(body, `data-site-drawer`); count != 1 {
				t.Errorf("enhanced drawer count: got %d, want 1", count)
			}
			for _, sharedMarker := range []string{
				`class="no-script-navigation"`,
				`class="mobile-home-link"`,
			} {
				if count := strings.Count(body, sharedMarker); count != 1 {
					t.Errorf("shared navigation marker %q count: got %d, want 1", sharedMarker, count)
				}
			}

			hasSharedStyle := strings.Contains(body, `href="/static/css/reference-menu.css"`)
			if hasSharedStyle != route.usesSharedStyle {
				t.Errorf("shared rail stylesheet presence: got %t, want %t", hasSharedStyle, route.usesSharedStyle)
			}
			hasSharedBodyClass := strings.Contains(
				extractOpeningTag(t, extractElementByMarker(t, body, `class="site-body`, "body")),
				"has-site-reference-menu",
			)
			if hasSharedBodyClass != route.usesSharedStyle {
				t.Errorf("shared rail body class presence: got %t, want %t", hasSharedBodyClass, route.usesSharedStyle)
			}

			rail := extractElementByMarker(t, body, "data-reference-menu", "details")
			opening := extractOpeningTag(t, rail)
			if !referenceMenuOpeningHasAttribute(opening, "open") {
				t.Error("reference menu does not start open")
			}
			for _, modalMarker := range []string{`role="dialog"`, `aria-modal="true"`, `data-site-drawer`} {
				if strings.Contains(rail, modalMarker) {
					t.Errorf("native reference menu contains modal marker %q", modalMarker)
				}
			}

			toggle := extractElementByMarker(t, rail, "data-reference-menu-toggle", "summary")
			if !strings.Contains(extractOpeningTag(t, toggle), `aria-label="Toggle`) {
				t.Error("reference menu toggle lacks an accessible Toggle label")
			}
			primary := extractElementByMarker(t, rail, "data-reference-menu-primary", "nav")
			labels := []string{"Projects", "Services", "About Us", "Journal", "Contact"}
			if route.includesHome {
				labels = append([]string{"Home"}, labels...)
			}
			previousPosition := -1
			for _, label := range labels {
				position := strings.Index(normalizeHTMLWhitespace(primary), ">"+label+"<")
				if position == -1 {
					t.Errorf("primary menu does not contain label %q", label)
					continue
				}
				if position <= previousPosition {
					t.Errorf("primary menu label %q is out of order", label)
				}
				previousPosition = position
			}
			for _, plainLabel := range []string{"Services", "About Us", "Journal"} {
				if strings.Contains(normalizeHTMLWhitespace(primary), ">"+plainLabel+"</a>") {
					t.Errorf("unfinished destination %q is published as a link", plainLabel)
				}
			}
			for _, deadMarker := range []string{`href="#"`, `aria-disabled="true"`, `role="link"`} {
				if strings.Contains(primary, deadMarker) {
					t.Errorf("primary menu contains dead interaction %q", deadMarker)
				}
			}

			expectedLinkCount := 2
			if route.includesHome {
				expectedLinkCount = 3
				if !strings.Contains(primary, `href="/"`) {
					t.Error("non-landing reference menu does not link Home")
				}
			}
			hrefs := referenceMenuHrefPattern.FindAllStringSubmatch(primary, -1)
			if len(hrefs) != expectedLinkCount {
				t.Errorf("primary menu link count: got %d, want %d", len(hrefs), expectedLinkCount)
			}
			if !strings.Contains(primary, `href="`+route.projectsPath+`"`) {
				t.Errorf("Projects destination: want %q", route.projectsPath)
			}
			for _, match := range hrefs {
				assertReferenceMenuDestination(t, handler, route.path, match[1])
			}

			socials := extractElementByMarker(t, rail, "data-reference-menu-socials", "ul")
			for marker, expected := range map[string]int{
				"<li":                3,
				"<svg":               3,
				`aria-hidden="true"`: 3,
				`focusable="false"`:  3,
			} {
				if count := strings.Count(socials, marker); count != expected {
					t.Errorf("social marker %q count: got %d, want %d", marker, count, expected)
				}
			}
			socialCursor := 0
			for _, platform := range []string{"Instagram", "Pinterest", "LinkedIn"} {
				svgPosition := strings.Index(socials[socialCursor:], "<svg")
				labelPosition := strings.Index(socials[socialCursor:], ">"+platform+"</span>")
				if svgPosition == -1 || labelPosition <= svgPosition {
					t.Errorf("%s logo does not precede its label", platform)
					continue
				}
				socialCursor += labelPosition + len(platform)
			}
			for _, falseInteraction := range []string{"<a", "<button", "href=", "tabindex=", `role="img"`} {
				if strings.Contains(socials, falseInteraction) {
					t.Errorf("social group contains false interaction %q", falseInteraction)
				}
			}
		})
	}
}

// TestSharedReferenceMenuStylesMatchApprovedGeometry locks the common desktop
// scale while proving compact routes continue to depend on the shared drawer.
func TestSharedReferenceMenuStylesMatchApprovedGeometry(t *testing.T) {
	t.Parallel()

	handler := newStaticAssetHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/static/css/reference-menu.css", nil),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stylesheet status: got %d, want 200", recorder.Code)
	}
	stylesheet := recorder.Body.String()

	menuRule := stage26CSSRule(t, stylesheet, ".site-reference-menu")
	for _, required := range []string{
		"position: absolute;",
		"display: none;",
		"height: 100vh;",
		"height: 100svh;",
		"pointer-events: none;",
	} {
		if !strings.Contains(menuRule, required) {
			t.Errorf("shared reference menu rule does not contain %q", required)
		}
	}
	panelRule := stage26CSSRule(t, stylesheet, ".site-reference-menu__panel")
	for _, required := range []string{
		"display: flex;",
		"padding-block-start: clamp(10rem, 27vh, 15.5rem);",
		"padding-block-end: clamp(3.25rem, 6.5vh, 4.25rem);",
		"overflow-y: auto;",
		"pointer-events: auto;",
	} {
		if !strings.Contains(panelRule, required) {
			t.Errorf("shared reference panel rule does not contain %q", required)
		}
	}
	primaryRule := stage26CSSRule(t, stylesheet, ".site-reference-menu__primary")
	for _, required := range []string{
		"width: 100%;",
		"justify-items: center;",
		"gap: clamp(1.65rem, 4.1vh, 2.4rem);",
		"text-align: center;",
	} {
		if !strings.Contains(primaryRule, required) {
			t.Errorf("shared reference primary rule does not contain %q", required)
		}
	}
	if strings.Contains(stylesheet, "data-home-reference-menu") {
		t.Error("shared reference rail reuses the Home-only JavaScript hook")
	}
}

func referenceMenuOpeningHasAttribute(opening string, attribute string) bool {
	opening = strings.TrimSuffix(strings.TrimSpace(opening), ">")
	for _, field := range strings.Fields(opening) {
		if field == attribute {
			return true
		}
	}
	return false
}

func assertReferenceMenuDestination(
	t *testing.T,
	handler http.Handler,
	currentPath string,
	href string,
) {
	t.Helper()

	destination, err := url.Parse(href)
	if err != nil {
		t.Fatalf("parse reference-menu destination %q: %v", href, err)
	}
	if destination.IsAbs() || destination.Host != "" {
		t.Fatalf("reference-menu destination %q is not application-relative", href)
	}
	targetPath := destination.Path
	if targetPath == "" {
		targetPath = currentPath
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, targetPath, nil),
	)
	if recorder.Code != http.StatusOK {
		t.Errorf("destination %q status: got %d, want 200", href, recorder.Code)
		return
	}
	if destination.Fragment != "" && !strings.Contains(
		recorder.Body.String(),
		`id="`+destination.Fragment+`"`,
	) {
		t.Errorf("destination %q has no matching fragment target", href)
	}
}
