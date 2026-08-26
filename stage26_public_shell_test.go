package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStage26PublicShellPublishesOnlyUsableNavigation protects the final public
// shell from regaining disabled destinations while proving native navigation is
// present before JavaScript enhancement and contains every real top-level route.
func TestStage26PublicShellPublishesOnlyUsableNavigation(t *testing.T) {
	app := newTestApplication(t)
	recorder := httptest.NewRecorder()
	app.routes().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/", nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("homepage status: got %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, required := range []string{
		`href="/static/css/no-script.css"`,
		`class="no-script-navigation"`,
		`aria-label="Website navigation fallback"`,
		`data-menu-open`,
		`data-site-drawer`,
	} {
		if !strings.Contains(body, required) {
			t.Errorf("public shell does not contain %q", required)
		}
	}

	fallback := extractElementByMarker(
		t,
		body,
		`aria-label="Website navigation fallback"`,
		"nav",
	)
	for _, path := range []string{
		`href="/"`,
		`href="/interior-design"`,
		`href="/architecture-design"`,
		`href="/products"`,
		`href="/contact"`,
	} {
		if !strings.Contains(fallback, path) {
			t.Errorf("fallback navigation does not contain %q", path)
		}
	}
	if count := strings.Count(fallback, "<a"); count != 5 {
		t.Errorf("fallback link count: got %d, want 5 real routes", count)
	}
	if count := strings.Count(fallback, `aria-current="page"`); count != 1 {
		t.Errorf("fallback current-page count: got %d, want 1", count)
	}

	for _, placeholder := range []string{
		"Search will be added later",
		"Projects",
		"Services",
		"About Us",
		"Journal",
		"Instagram",
		"Pinterest",
		"LinkedIn",
		`aria-disabled="true"`,
	} {
		if strings.Contains(body, placeholder) {
			t.Errorf("public shell still contains unfinished placeholder %q", placeholder)
		}
	}
}

// TestStage26NavigationStylesKeepHeaderContrastAndFallbackUsable verifies the
// shared header uses one stable dark surface and the native/enhanced controls
// swap only through declarations scoped to their intended selectors.
func TestStage26NavigationStylesKeepHeaderContrastAndFallbackUsable(t *testing.T) {
	handler := newStaticAssetHandler()

	navigationRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		navigationRecorder,
		httptest.NewRequest(
			http.MethodGet,
			"/static/css/navigation.css",
			nil,
		),
	)
	if navigationRecorder.Code != http.StatusOK {
		t.Fatalf(
			"navigation stylesheet status: got %d, want 200",
			navigationRecorder.Code,
		)
	}
	navigationCSS := navigationRecorder.Body.String()
	headerRule := stage26CSSRule(t, navigationCSS, ".site-header")
	for _, required := range []string{
		"background: rgba(14, 13, 11, 0.94);",
		"backdrop-filter: blur(0.75rem);",
	} {
		if !strings.Contains(headerRule, required) {
			t.Errorf("site-header rule does not contain %q", required)
		}
	}
	for _, required := range []string{
		"@media (forced-colors: active)",
		".menu-toggle__lines span,",
		".drawer-close__icon::before,",
		"background: ButtonText;",
		"forced-color-adjust: none;",
	} {
		if !strings.Contains(navigationCSS, required) {
			t.Errorf("navigation stylesheet does not contain %q", required)
		}
	}
	for _, obsolete := range []string{
		".search-button",
		".drawer-socials",
		".drawer-navigation__link.is-disabled",
	} {
		if strings.Contains(navigationCSS, obsolete) {
			t.Errorf("navigation stylesheet retains obsolete selector %q", obsolete)
		}
	}

	fallbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		fallbackRecorder,
		httptest.NewRequest(
			http.MethodGet,
			"/static/css/no-script.css",
			nil,
		),
	)
	if fallbackRecorder.Code != http.StatusOK {
		t.Fatalf(
			"no-script stylesheet status: got %d, want 200",
			fallbackRecorder.Code,
		)
	}
	fallbackCSS := fallbackRecorder.Body.String()
	if rule := stage26CSSRule(t, fallbackCSS, ".menu-toggle"); !strings.Contains(rule, "display: none;") {
		t.Error("fallback does not hide the uninitialized enhanced control")
	}
	if rule := stage26CSSRule(
		t,
		fallbackCSS,
		".has-enhanced-navigation .menu-toggle",
	); !strings.Contains(rule, "display: grid;") {
		t.Error("ready state does not reveal the enhanced control")
	}
	if rule := stage26CSSRule(
		t,
		fallbackCSS,
		".has-enhanced-navigation .no-script-navigation",
	); !strings.Contains(rule, "display: none;") {
		t.Error("ready state does not replace the native fallback")
	}

	scriptRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		scriptRecorder,
		httptest.NewRequest(http.MethodGet, "/static/js/main.js", nil),
	)
	if scriptRecorder.Code != http.StatusOK ||
		!strings.Contains(
			scriptRecorder.Body.String(),
			`"has-enhanced-navigation"`,
		) {
		t.Error("drawer controller does not publish its successful ready state")
	}
}

// stage26CSSRule isolates one ordinary declaration block so regression tests
// cannot pass merely because an expected property appears under another selector.
func stage26CSSRule(t *testing.T, stylesheet string, selector string) string {
	t.Helper()

	marker := selector + " {"
	start := strings.Index(stylesheet, marker)
	if start < 0 {
		t.Fatalf("stylesheet omits selector %q", selector)
	}
	declarations := stylesheet[start+len(marker):]
	end := strings.IndexByte(declarations, '}')
	if end < 0 {
		t.Fatalf("stylesheet does not close selector %q", selector)
	}

	return declarations[:end]
}

// TestStage26DetailImageLoadingPriorities verifies the first Architecture
// visual is requested eagerly while the Interior image that follows its text
// introduction remains eligible for deferred loading.
func TestStage26DetailImageLoadingPriorities(t *testing.T) {
	app := newTestApplication(t)

	t.Run("architecture above fold", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		detail := architectureProjectDetailData{
			Number:        "01",
			Title:         "Priority Architecture",
			Typology:      "Residential",
			ProjectStatus: "Completed",
			Cover: &publicArchitectureProjectCoverPageData{
				Path:    "/architecture-design/priority/cover/1",
				Width:   1600,
				Height:  1000,
				AltText: "Fictional architecture priority test cover",
			},
		}
		app.render(
			recorder,
			http.StatusOK,
			"architecture-project-detail.html",
			pageData{
				Title:                     detail.Title,
				CurrentPath:               "/architecture-design/priority",
				NavigationPath:            "/architecture-design",
				ArchitectureProjectDetail: &detail,
			},
		)

		figure := extractElementByMarker(
			t,
			recorder.Body.String(),
			`class="architecture-project-detail__media architecture-project-detail__media--image"`,
			"figure",
		)
		if !strings.Contains(figure, `fetchpriority="high"`) {
			t.Error("above-fold Architecture cover omits high fetch priority")
		}
		if strings.Contains(figure, `loading="lazy"`) {
			t.Error("above-fold Architecture cover is lazy-loaded")
		}
	})

	t.Run("interior after introduction", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		detail := interiorProjectDetailData{
			Number:        "01",
			Title:         "Deferred Interior",
			Typology:      "Residential",
			ProjectStatus: "Completed",
			Cover: &publicInteriorProjectCoverPageData{
				Path:    "/interior-design/deferred/cover/1",
				Width:   1600,
				Height:  1000,
				AltText: "Fictional interior loading test cover",
			},
		}
		app.render(
			recorder,
			http.StatusOK,
			"interior-project-detail.html",
			pageData{
				Title:                 detail.Title,
				CurrentPath:           "/interior-design/deferred",
				NavigationPath:        "/interior-design",
				InteriorProjectDetail: &detail,
			},
		)

		figure := extractElementByMarker(
			t,
			recorder.Body.String(),
			`class="interior-project-detail__media interior-project-detail__media--image"`,
			"figure",
		)
		if !strings.Contains(figure, `loading="lazy"`) {
			t.Error("later Interior cover omits native lazy loading")
		}
		if strings.Contains(figure, `fetchpriority="high"`) {
			t.Error("later Interior cover competes at high fetch priority")
		}
	})
}
