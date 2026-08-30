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

	// Each platform keeps a visible name and a preceding decorative brand mark.
	// Until reviewed profile URLs exist, none becomes a false interaction.
	socials := extractElementByMarker(
		t,
		landingMenu,
		`class="home-reference-menu__socials"`,
		"ul",
	)
	if !strings.Contains(extractOpeningTag(t, socials), `role="list"`) {
		t.Error("landing social group does not restore list semantics")
	}
	previousSocialPosition := -1
	for _, platform := range []struct {
		icon  string
		label string
	}{
		{icon: "social-icon--instagram", label: ">Instagram</span>"},
		{icon: "social-icon--pinterest", label: ">Pinterest</span>"},
		{icon: "social-icon--linkedin", label: ">LinkedIn</span>"},
	} {
		iconPosition := strings.Index(socials, platform.icon)
		labelPosition := strings.Index(socials, platform.label)
		if iconPosition == -1 || labelPosition == -1 {
			t.Errorf("landing social group does not contain %q and its label", platform.icon)
			continue
		}
		if iconPosition >= labelPosition {
			t.Errorf("landing social icon %q does not precede its label", platform.icon)
		}
		if iconPosition <= previousSocialPosition {
			t.Errorf("landing social icon %q is out of reference order", platform.icon)
		}
		previousSocialPosition = iconPosition
	}
	if count := strings.Count(socials, "<svg"); count != 3 {
		t.Errorf("landing social icon count: got %d, want 3", count)
	}
	if count := strings.Count(socials, `aria-hidden="true"`); count != 3 {
		t.Errorf("decorative landing social icon count: got %d, want 3", count)
	}
	if count := strings.Count(socials, `focusable="false"`); count != 3 {
		t.Errorf("unfocusable landing social icon count: got %d, want 3", count)
	}
	for _, falseInteraction := range []string{
		"<a",
		"<button",
		"href=",
		"tabindex=",
		`aria-disabled="true"`,
		`role="img"`,
	} {
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
	landingVariablesRule := stage26CSSRule(
		t,
		homeCSS,
		`body[data-current-path="/"]`,
	)
	for _, required := range []string{
		"--home-reference-menu-width: clamp(11.5rem, 20.35vw, 40rem);",
		"--home-reference-rail-width: clamp(3.75rem, 6.5vw, 13rem);",
	} {
		if !strings.Contains(landingVariablesRule, required) {
			t.Errorf("landing reference geometry does not contain %q", required)
		}
	}
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
	baseHeroRule := stage26CSSRule(t, homeCSS, ".home-hero")
	for _, required := range []string{"display: grid;", "place-items: center;"} {
		if !strings.Contains(baseHeroRule, required) {
			t.Errorf("landing hero centering rule does not contain %q", required)
		}
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
		"padding-block-start: clamp(10rem, 22vh, 13rem);",
		"padding-block-end: clamp(3.5rem, 9.25vh, 5.4rem);",
		"padding-inline: 0;",
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
	primaryRule := stage26CSSRule(t, homeCSS, ".home-reference-menu__primary")
	for _, required := range []string{
		"width: 100%;",
		"align-self: stretch;",
		"justify-items: center;",
		"gap: clamp(1.65rem, 4.1vh, 2.4rem);",
		"text-align: center;",
	} {
		if !strings.Contains(primaryRule, required) {
			t.Errorf("landing primary menu rule does not contain %q", required)
		}
	}
	menuTypeRule := stage26CSSRule(t, homeCSS, ".home-reference-menu__label")
	for _, required := range []string{
		"font-size: clamp(0.875rem, 0.86vw, 1.75rem);",
		"letter-spacing: 0.2em;",
	} {
		if !strings.Contains(menuTypeRule, required) {
			t.Errorf("landing menu typography rule does not contain %q", required)
		}
	}
	socialRule := stage26CSSRule(t, homeCSS, ".home-reference-menu__socials")
	for _, required := range []string{
		"display: grid;",
		"gap: clamp(0.5rem, 0.55vw, 1rem);",
		"margin: auto 0 0 calc(",
		"font-size: clamp(0.625rem, 0.6vw, 1.15rem);",
		"list-style: none;",
		"text-transform: uppercase;",
	} {
		if !strings.Contains(socialRule, required) {
			t.Errorf("landing social rule does not contain %q", required)
		}
	}
	homeSocialItemRule := stage26CSSRule(
		t,
		homeCSS,
		".home-reference-menu__social-item",
	)
	for _, required := range []string{
		"display: grid;",
		"grid-template-columns: clamp(1rem, 0.9vw, 1.65rem) max-content;",
	} {
		if !strings.Contains(homeSocialItemRule, required) {
			t.Errorf("landing social item rule does not contain %q", required)
		}
	}
	homeSocialIconRule := stage26CSSRule(
		t,
		homeCSS,
		".home-reference-menu__social-icon",
	)
	for _, required := range []string{
		"display: block;",
		"width: clamp(1rem, 0.9vw, 1.65rem);",
		"height: clamp(1rem, 0.9vw, 1.65rem);",
		"stroke: currentColor;",
		"stroke-width: 2.15;",
		"opacity: 1;",
	} {
		if !strings.Contains(homeSocialIconRule, required) {
			t.Errorf("landing social icon rule does not contain %q", required)
		}
	}
	identityRule := stage26CSSRule(t, homeCSS, ".home-hero__identity")
	for _, required := range []string{
		"justify-items: center;",
		"text-align: center;",
		"transform: none;",
	} {
		if !strings.Contains(identityRule, required) {
			t.Errorf("landing identity centering rule does not contain %q", required)
		}
	}
	for selector, expected := range map[string]string{
		".home-hero__title":      "font-size: clamp(4.25rem, min(6vw, 14vh), 6.5rem);",
		".home-hero__descriptor": "font-size: clamp(0.88rem, 1.4vw, 1.25rem);",
		".home-hero__scroll":     "font-size: max(clamp(0.68rem, 0.6rem + 0.25vw, 0.9rem), 0.64vw);",
	} {
		rule := stage26CSSRule(t, homeCSS, selector)
		if !strings.Contains(rule, expected) {
			t.Errorf("%s does not contain %q", selector, expected)
		}
	}
	chevronRule := stage26CSSRule(t, homeCSS, ".home-hero__scroll-chevron")
	for _, dimension := range []string{"width", "height"} {
		expected := dimension + ": max(clamp(1.15rem, 1.05rem + 0.15vw, 1.3rem), 0.82vw);"
		if !strings.Contains(chevronRule, expected) {
			t.Errorf("landing scroll chevron does not contain %q", expected)
		}
	}
	scrollRailRule := stage26CSSRule(t, homeCSS, ".home-hero__rail")
	for _, required := range []string{
		"inset-inline: 0;",
		"display: flex;",
		"justify-content: center;",
	} {
		if !strings.Contains(scrollRailRule, required) {
			t.Errorf("landing scroll rail rule does not contain %q", required)
		}
	}
	disciplineLinkRule := stage26CSSRule(
		t,
		homeCSS,
		`body[data-current-path="/"] .discipline-nav__link`,
	)
	for _, required := range []string{
		"--discipline-link-padding: clamp(1.75rem, 3.3vw, 2.9rem);",
		"font-size: clamp(0.78rem, 0.75vw, 1.5rem);",
		"letter-spacing: 0.24em;",
	} {
		if !strings.Contains(disciplineLinkRule, required) {
			t.Errorf("landing discipline link rule does not contain %q", required)
		}
	}
	disciplineSeparatorRule := stage26CSSRule(
		t,
		homeCSS,
		`body[data-current-path="/"] .discipline-nav__separator`,
	)
	if !strings.Contains(disciplineSeparatorRule, "height: 1.125rem;") {
		t.Error("landing discipline separator does not match the approved height")
	}
	metadataRule := stage26CSSRule(t, homeCSS, ".home-feature__classification")
	if expected := "font-size: max(clamp(0.8rem, 0.75vw, 0.84rem), 0.45vw);"; !strings.Contains(metadataRule, expected) {
		t.Errorf("landing feature metadata does not contain %q", expected)
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
