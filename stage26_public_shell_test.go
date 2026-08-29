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
	landingMenuPosition := strings.Index(
		body,
		`class="home-reference-menu"`,
	)
	disciplineNavigationPosition := strings.Index(
		body,
		`class="discipline-nav"`,
	)
	if landingMenuPosition == -1 || disciplineNavigationPosition == -1 {
		t.Fatal("homepage navigation order cannot be inspected")
	}
	if landingMenuPosition > disciplineNavigationPosition {
		t.Error("landing rail follows the visually later discipline navigation")
	}

	for _, required := range []string{
		`href="/static/css/no-script.css"`,
		`class="no-script-navigation"`,
		`aria-label="Website navigation fallback"`,
		`data-menu-open`,
		`data-site-drawer`,
		`class="home-reference-menu"`,
		`data-home-reference-menu`,
		`data-home-hero`,
		`data-home-projects-link`,
		`data-home-disciplines`,
		`tabindex="-1"`,
		`aria-label="Landing page navigation"`,
		`class="home-reference-menu__socials"`,
		`aria-label="Social platforms"`,
		`class="home-hero__search-motif"`,
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
		`aria-disabled="true"`,
	} {
		if strings.Contains(body, placeholder) {
			t.Errorf("public shell still contains unfinished placeholder %q", placeholder)
		}
	}

	// Projects and Contact keep real destinations. The three reference-only
	// labels stay plain text until matching canonical routes own reviewed pages.
	landingNavigation := extractElementByMarker(
		t,
		body,
		`aria-label="Landing page navigation"`,
		"nav",
	)
	for _, destination := range []string{
		`href="#disciplines"`,
		`href="/contact"`,
	} {
		if !strings.Contains(landingNavigation, destination) {
			t.Errorf(
				"landing navigation does not contain real destination %q",
				destination,
			)
		}
	}
	if strings.Contains(landingNavigation, `href="#"`) ||
		strings.Contains(landingNavigation, `aria-disabled="true"`) {
		t.Error("landing navigation contains a dead destination")
	}
	if !strings.Contains(landingNavigation, `>Projects</a>`) {
		t.Error("landing navigation does not preserve the reference Projects label")
	}
	for _, label := range []string{
		`<span class="home-reference-menu__label">Services</span>`,
		`<span class="home-reference-menu__label">About Us</span>`,
		`<span class="home-reference-menu__label">Journal</span>`,
	} {
		if !strings.Contains(landingNavigation, label) {
			t.Errorf("landing navigation does not contain reference label %q", label)
		}
	}
	previousLabelPosition := -1
	for _, label := range []string{
		`>Projects</a>`,
		`>Services</span>`,
		`>About Us</span>`,
		`>Journal</span>`,
		`>Contact</a>`,
	} {
		position := strings.Index(landingNavigation, label)
		if position == -1 {
			t.Errorf("landing navigation does not contain ordered label %q", label)
			continue
		}
		if position <= previousLabelPosition {
			t.Errorf("landing navigation label %q is out of reference order", label)
		}
		previousLabelPosition = position
	}
	for _, misleadingDestination := range []string{
		`href="/interior-design"`,
		`href="/architecture-design"`,
		`href="/products"`,
		`href="/"`,
	} {
		if strings.Contains(landingNavigation, misleadingDestination) {
			t.Errorf(
				"landing reference label points to unrelated destination %q",
				misleadingDestination,
			)
		}
	}
	if count := strings.Count(landingNavigation, "<a"); count != 2 {
		t.Errorf("landing navigation link count: got %d, want 2 real routes", count)
	}

	// The reference rail is persistent page navigation, not a second modal.
	// Keeping modal semantics exclusive to the compact shared drawer avoids
	// duplicate focus traps and makes the native disclosure useful without JS.
	landingMenu := extractElementByMarker(
		t,
		body,
		`class="home-reference-menu"`,
		"details",
	)
	landingMenuOpeningEnd := strings.IndexByte(landingMenu, '>')
	if landingMenuOpeningEnd == -1 {
		t.Fatal("landing reference menu opening element is not closed")
	}
	menuStartsOpen := false
	for _, attribute := range strings.Fields(
		landingMenu[:landingMenuOpeningEnd],
	) {
		if attribute == "open" {
			menuStartsOpen = true
			break
		}
	}
	if !menuStartsOpen {
		t.Error("landing reference menu does not start open")
	}
	for _, modalMarker := range []string{
		`role="dialog"`,
		`aria-modal="true"`,
		`data-site-drawer`,
	} {
		if strings.Contains(landingMenu, modalMarker) {
			t.Errorf(
				"landing reference menu contains modal marker %q",
				modalMarker,
			)
		}
	}

	if strings.Contains(landingMenu, "home-reference-menu__quick") {
		t.Error("landing reference menu retains the removed lower label group")
	}
	for _, removedLabel := range []string{
		"<span>Interior</span>",
		"<span>Architecture</span>",
		"<span>Objects</span>",
	} {
		if strings.Contains(landingMenu, removedLabel) {
			t.Errorf("landing reference menu retains removed label %q", removedLabel)
		}
	}

	// The supplied desktop reference ends with three social names. Until reviewed
	// profile URLs exist, the shell must preserve their order without publishing
	// fake anchors or disabled interactive controls.
	socials := extractElementByMarker(
		t,
		landingMenu,
		`class="home-reference-menu__socials"`,
		"ul",
	)
	previousSocialPosition := -1
	for _, social := range []string{
		">Instagram</li>",
		">Pinterest</li>",
		">LinkedIn</li>",
	} {
		position := strings.Index(socials, social)
		if position == -1 {
			t.Errorf("landing social group does not contain %q", social)
			continue
		}
		if position <= previousSocialPosition {
			t.Errorf("landing social label %q is out of reference order", social)
		}
		previousSocialPosition = position
	}
	for _, falseInteraction := range []string{"<a", "href=", `aria-disabled="true"`} {
		if strings.Contains(socials, falseInteraction) {
			t.Errorf(
				"landing social group contains false interaction %q",
				falseInteraction,
			)
		}
	}
	primaryPosition := strings.Index(
		landingMenu,
		`class="home-reference-menu__primary"`,
	)
	socialPosition := strings.Index(
		landingMenu,
		`class="home-reference-menu__socials"`,
	)
	if primaryPosition == -1 || socialPosition <= primaryPosition {
		t.Error("landing social group does not follow the primary navigation")
	}

	// The magnifier reproduces reference geometry without becoming a fake
	// search action or adding an unusable keyboard destination.
	if strings.Contains(body, `home-hero__catalogue-shortcut`) ||
		strings.Contains(body, `aria-label="Browse the product catalogue"`) {
		t.Error("decorative search motif still promises a catalogue action")
	}
	searchMarker := strings.Index(body, `class="home-hero__search-motif"`)
	if searchMarker == -1 {
		t.Fatal("homepage does not contain its decorative search motif")
	}
	searchOpeningStart := strings.LastIndex(body[:searchMarker], "<span")
	searchOpeningEnd := strings.Index(body[searchMarker:], ">")
	if searchOpeningStart == -1 || searchOpeningEnd == -1 {
		t.Fatal("decorative search motif opening element cannot be inspected")
	}
	searchOpening := body[searchOpeningStart : searchMarker+searchOpeningEnd+1]
	if !strings.Contains(searchOpening, `aria-hidden="true"`) {
		t.Error("decorative search motif is exposed as an interactive promise")
	}
}

// TestStage26LandingMenuFillsTheViewportWithoutAnInsetRule protects the three
// corrections approved from the browser screenshot: the glass fills the hero
// viewport, the former internal divider is absent, and its removed labels
// cannot return through stale presentation rules.
func TestStage26LandingMenuFillsTheViewportWithoutAnInsetRule(t *testing.T) {
	handler := newStaticAssetHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/static/css/home.css", nil),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("home stylesheet status: got %d, want 200", recorder.Code)
	}
	homeCSS := recorder.Body.String()
	menuRule := stage26CSSRule(t, homeCSS, ".home-reference-menu")
	for _, required := range []string{
		"position: absolute;",
		"height: 100vh;",
		"height: 100svh;",
	} {
		if !strings.Contains(menuRule, required) {
			t.Errorf("landing menu rule does not contain %q", required)
		}
	}
	if strings.Contains(menuRule, "position: fixed;") {
		t.Error("unenhanced landing menu is fixed over below-hero content")
	}
	activeMenuRule := stage26CSSRule(
		t,
		homeCSS,
		".has-enhanced-home-reference-menu .home-reference-menu.is-home-hero-active",
	)
	for _, required := range []string{
		"position: fixed;",
		"height: 100vh;",
		"height: 100dvh;",
	} {
		if !strings.Contains(activeMenuRule, required) {
			t.Errorf("active landing menu rule does not contain %q", required)
		}
	}
	inactiveMenuRule := stage26CSSRule(
		t,
		homeCSS,
		".has-enhanced-home-reference-menu .home-reference-menu.is-home-hero-inactive",
	)
	if !strings.Contains(inactiveMenuRule, "display: none;") {
		t.Error("inactive landing menu remains rendered over later content")
	}
	heroRule := stage26CSSRule(
		t,
		homeCSS,
		".has-enhanced-home-reference-menu .home-hero",
	)
	for _, required := range []string{"min-height: 100vh;", "min-height: 100dvh;"} {
		if !strings.Contains(heroRule, required) {
			t.Errorf("enhanced hero rule does not contain %q", required)
		}
	}
	panelRule := stage26CSSRule(t, homeCSS, ".home-reference-menu__panel")
	for _, required := range []string{
		"position: relative;",
		"display: flex;",
		"flex-direction: column;",
		"height: 100vh;",
		"height: 100svh;",
		"overflow-x: hidden;",
		"overflow-y: auto;",
		"overscroll-behavior: contain;",
	} {
		if !strings.Contains(panelRule, required) {
			t.Errorf("landing menu panel rule does not contain %q", required)
		}
	}
	if strings.Contains(panelRule, "height: 100%;") {
		t.Error("landing panel relies on an unresolved details-wrapper percentage height")
	}
	socialRule := stage26CSSRule(t, homeCSS, ".home-reference-menu__socials")
	for _, required := range []string{
		"display: grid;",
		"margin: auto 0 0 clamp(0.45rem, 0.65vw, 0.65rem);",
		"list-style: none;",
		"text-transform: uppercase;",
	} {
		if !strings.Contains(socialRule, required) {
			t.Errorf("landing social rule does not contain %q", required)
		}
	}
	activePanelRule := stage26CSSRule(
		t,
		homeCSS,
		".has-enhanced-home-reference-menu .home-reference-menu.is-home-hero-active .home-reference-menu__panel",
	)
	for _, required := range []string{"height: 100vh;", "height: 100dvh;"} {
		if !strings.Contains(activePanelRule, required) {
			t.Errorf("active landing panel rule does not contain %q", required)
		}
	}
	for _, removed := range []string{
		".home-reference-menu__panel::before",
		".home-reference-menu__quick",
	} {
		if strings.Contains(homeCSS, removed) {
			t.Errorf("home stylesheet retains removed selector %q", removed)
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
	if scriptRecorder.Code != http.StatusOK {
		t.Fatalf(
			"navigation script status: got %d, want 200",
			scriptRecorder.Code,
		)
	}
	navigationScript := scriptRecorder.Body.String()
	for _, required := range []string{
		`"has-enhanced-navigation"`,
		`"has-enhanced-home-reference-menu"`,
		`"[data-home-reference-menu]"`,
		`"[data-home-hero]"`,
		`"[data-home-projects-link]"`,
		`"[data-home-disciplines]"`,
		"function syncHomeReferenceMenuGeometry()",
		"heroBounds.top <= 1",
		"heroBounds.bottom >= viewportHeight - 1",
		`"is-home-hero-active"`,
		`"is-home-hero-inactive"`,
		`homeReferenceMenu.toggleAttribute("inert", !heroFillsViewport);`,
		`homeDisciplines.focus({ preventScroll: true });`,
	} {
		if !strings.Contains(navigationScript, required) {
			t.Errorf("navigation script does not contain %q", required)
		}
	}
	if strings.Contains(navigationScript, "homeReferenceMenu.open = true;") {
		t.Error("navigation script overrides the visitor's disclosure state")
	}
	if strings.Contains(navigationScript, "heroBounds.bottom > 0") {
		t.Error("a partially visible hero incorrectly keeps the full viewport menu active")
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
