"use strict";

/*
 * Site drawer controller
 * ----------------------
 * The public documents and their route links are server-rendered rather than
 * being replaced by a single-page application. This small enhancement controls
 * the off-canvas navigation drawer: it synchronizes visual state with ARIA
 * attributes, locks background scrolling, keeps keyboard focus inside the open
 * drawer, and restores focus when the drawer closes.
 *
 * Data attributes are used as JavaScript hooks so styling classes can evolve
 * independently. Optional checks let the same script load safely on a page even
 * if one of the enhanced components is absent.
 */

/* The body receives the scroll-lock class while modal navigation is active. */
const body = document.body;

/*
 * The desktop homepage disclosure starts open like the approved composition.
 * Its data hook supports hero-bound viewport sizing without overriding the
 * visitor's native close or reopen choice.
 */
const homeReferenceMenu = document.querySelector(
	"[data-home-reference-menu]",
);

/* The hero supplies the visual boundary for the desktop landing disclosure. */
const homeHero = document.querySelector("[data-home-hero]");

/* The Projects fragment link hands focus to its real destination before hiding. */
const homeProjectsLink = document.querySelector("[data-home-projects-link]");

/* The disciplines section is the focusable destination of the Projects link. */
const homeDisciplines = document.querySelector("[data-home-disciplines]");

/* The Interior hero cue enhances one real fragment link with GIF-like motion. */
const interiorScrollLink = document.querySelector("[data-interior-scroll]");

/* The labelled work section is both the visual and keyboard-focus destination. */
const interiorWork = document.querySelector("#selected-work");

/* A null frame means no geometry synchronization is waiting to run. */
let homeReferenceMenuSyncFrame = null;

/*
 * The page shell contains the header and main content, but not the drawer.
 * Applying `inert` to this shell temporarily removes background controls from
 * keyboard navigation and the accessibility interaction model.
 */
const pageShell = document.querySelector("[data-page-shell]");

/* The outer drawer owns open/closed visibility and the aria-hidden state. */
const drawer = document.querySelector("[data-site-drawer]");

/* The panel bounds keyboard focus while the drawer behaves like a modal. */
const drawerPanel = document.querySelector("[data-drawer-panel]");

/* The header menu button opens the drawer and exposes aria-expanded. */
const openButton = document.querySelector("[data-menu-open]");

/*
 * The template marks the preferred first focus destination, normally the close
 * control. focusDrawer() supplies safe fallbacks if that hook is ever absent.
 */
const initialFocusElement = document.querySelector(
	"[data-menu-initial-focus]",
);

/* Every element with this hook dismisses the drawer, including the backdrop. */
const closeButtons = document.querySelectorAll("[data-menu-close]");

/* Selecting an actual drawer destination also clears the temporary menu state. */
const menuLinks = document.querySelectorAll("[data-menu-link]");

/*
 * Store the element active before opening so closing returns keyboard users to
 * their exact place. null means there is currently no saved focus target.
 */
let previouslyFocusedElement = null;

/*
 * A single selector describes normally focusable HTML controls plus explicit
 * tabindex values other than the project's programmatic-only tabindex="-1".
 * Disabled form controls and that -1 panel value are excluded from the Tab
 * order.
 */
const focusableSelector = [
	'a[href]',
	'button:not([disabled])',
	'input:not([disabled])',
	'select:not([disabled])',
	'textarea:not([disabled])',
	'[tabindex]:not([tabindex="-1"])',
].join(",");

/**
 * Report whether the drawer currently carries its visual open-state class.
 *
 * Optional chaining allows a safe false result when the drawer is not present,
 * and nullish coalescing converts the missing result to a real boolean.
 *
 * @returns {boolean} True only when the drawer exists and is open.
 */
function isMenuOpen() {
	return drawer?.classList.contains("is-open") ?? false;
}

/**
 * Return controls that can actually participate in the drawer's current Tab
 * order, not merely elements that match a focusable selector in the DOM.
 *
 * getClientRects() excludes elements with no rendered box (for example,
 * display:none desktop/mobile variants), while computed visibility excludes
 * hidden elements. This keeps the focus trap correct across responsive states.
 *
 * @returns {Element[]} Rendered, visible focusable elements in DOM order.
 */
function getFocusableElements() {
	if (!drawerPanel) {
		return [];
	}

	/*
	 * Test each candidate at call time because responsive CSS can change which
	 * drawer navigation group is rendered after the window is resized.
	 */
	return Array.from(
		drawerPanel.querySelectorAll(focusableSelector),
	).filter((element) => {
		const style = window.getComputedStyle(element);

		return (
			element.getClientRects().length > 0 &&
			style.visibility !== "hidden"
		);
	});
}

/**
 * Enable or disable interaction with the page behind the drawer.
 *
 * The native `inert` attribute removes all descendants of the page shell from
 * focus and pointer interaction without rewriting tabindex values one by one.
 *
 * @param {boolean} isInert Whether background content should be non-interactive.
 * @returns {void}
 */
function setPageInert(isInert) {
	if (!pageShell) {
		return;
	}

	pageShell.toggleAttribute("inert", isInert);
}

/**
 * Move keyboard focus into the open drawer.
 *
 * The explicitly marked initial control is preferred, followed by the first
 * currently focusable element and finally the panel itself. preventScroll keeps
 * focus movement from unexpectedly shifting the full-height drawer.
 *
 * @returns {void}
 */
function focusDrawer() {
	if (!drawerPanel) {
		return;
	}

	const focusableElements = getFocusableElements();
	const focusTarget =
		initialFocusElement ??
		focusableElements[0] ??
		drawerPanel;

	focusTarget.focus({ preventScroll: true });
}

/**
 * Open the navigation drawer and establish its accessible modal-like state.
 *
 * The guard prevents partial state changes if required markup is missing and
 * avoids re-opening an already open drawer. Focus is saved before the page shell
 * becomes inert, then moved into the drawer after all state attributes update.
 *
 * @returns {void}
 */
function openMenu() {
	if (
		!drawer ||
		!drawerPanel ||
		!openButton ||
		isMenuOpen()
	) {
		return;
	}

	previouslyFocusedElement = document.activeElement;

	drawer.setAttribute("aria-hidden", "false");
	drawer.classList.add("is-open");

	openButton.setAttribute("aria-expanded", "true");
	openButton.setAttribute(
		"aria-label",
		"Close navigation menu",
	);

	body.classList.add("is-menu-open");
	setPageInert(true);

	focusDrawer();
}

/**
 * Close the navigation drawer and return focus to the invoking control.
 *
 * The function first removes visual and background-lock state, then tries the
 * element that had focus before opening. A connected HTMLElement check avoids
 * focusing stale DOM references after navigation or dynamic updates. The menu
 * button is a dependable fallback. aria-hidden is restored after focus leaves
 * the drawer so assistive technology receives a coherent final state.
 *
 * @returns {void}
 */
function closeMenu() {
	if (!drawer || !openButton || !isMenuOpen()) {
		return;
	}

	const focusTarget = previouslyFocusedElement;

	drawer.classList.remove("is-open");
	body.classList.remove("is-menu-open");
	setPageInert(false);

	openButton.setAttribute("aria-expanded", "false");
	openButton.setAttribute(
		"aria-label",
		"Open navigation menu",
	);

	previouslyFocusedElement = null;

	let focusWasRestored = false;

	if (
		focusTarget instanceof HTMLElement &&
		focusTarget !== body &&
		focusTarget.isConnected
	) {
		focusTarget.focus({ preventScroll: true });
		focusWasRestored =
			document.activeElement === focusTarget;
	}

	if (!focusWasRestored) {
		openButton.focus({ preventScroll: true });
	}

	drawer.setAttribute("aria-hidden", "true");
}

/**
 * Keep Tab and Shift+Tab navigation within the open drawer.
 *
 * Ordinary keystrokes and the closed state exit immediately. When no focusable
 * child exists, the panel becomes the fallback target. If focus is outside the
 * valid list, it is redirected to the first or last item according to travel
 * direction. At either boundary, focus wraps to the opposite end.
 *
 * @param {KeyboardEvent} event The document-level keydown event.
 * @returns {void}
 */
function trapFocus(event) {
	if (
		event.key !== "Tab" ||
		!drawerPanel ||
		!isMenuOpen()
	) {
		return;
	}

	const focusableElements = getFocusableElements();

	if (focusableElements.length === 0) {
		event.preventDefault();
		drawerPanel.focus({ preventScroll: true });
		return;
	}

	const firstElement = focusableElements[0];
	const lastElement =
		focusableElements[focusableElements.length - 1];
	const activeElement = document.activeElement;

	if (
		!activeElement ||
		!drawerPanel.contains(activeElement) ||
		!focusableElements.includes(activeElement)
	) {
		event.preventDefault();

		if (event.shiftKey) {
			lastElement.focus();
			return;
		}

		firstElement.focus();
		return;
	}

	if (
		event.shiftKey &&
		activeElement === firstElement
	) {
		event.preventDefault();
		lastElement.focus();
		return;
	}

	if (
		!event.shiftKey &&
		activeElement === lastElement
	) {
		event.preventDefault();
		firstElement.focus();
	}
}

/**
 * Restore a known closed state when a document is shown from browser history.
 *
 * Browsers may preserve classes, focus, and form state in the back-forward
 * cache. Running this on `pageshow` prevents a returned page from displaying a
 * stale open drawer or leaving its content inert. If preserved focus was inside
 * the drawer, it moves to the public menu button before the drawer is hidden.
 *
 * @returns {void}
 */
function resetMenu() {
	if (!drawer || !openButton) {
		return;
	}

	const focusWasInsideDrawer =
		document.activeElement instanceof Node &&
		drawer.contains(document.activeElement);

	drawer.classList.remove("is-open");
	body.classList.remove("is-menu-open");
	setPageInert(false);

	openButton.setAttribute("aria-expanded", "false");
	openButton.setAttribute(
		"aria-label",
		"Open navigation menu",
	);

	previouslyFocusedElement = null;

	if (focusWasInsideDrawer) {
		openButton.focus({ preventScroll: true });
	}

	drawer.setAttribute("aria-hidden", "true");
}

/**
 * Synchronize the landing rail with the hero's rendered viewport boundary.
 *
 * Fixed positioning is active only while the hero fills the visual viewport.
 * As soon as later content enters, the rail is removed from layout and made
 * inert so it cannot cover or receive focus over that content. This function
 * never changes the native details open state, so a visitor's close/reopen
 * choice survives scrolling and bfcache.
 *
 * @returns {void}
 */
function syncHomeReferenceMenuGeometry() {
	homeReferenceMenuSyncFrame = null;

	if (!homeReferenceMenu || !homeHero) {
		return;
	}

	const heroBounds = homeHero.getBoundingClientRect();
	const viewportHeight =
		window.visualViewport?.height ?? window.innerHeight;
	const heroFillsViewport =
		heroBounds.top <= 1 &&
		heroBounds.bottom >= viewportHeight - 1;

	homeReferenceMenu.classList.toggle(
		"is-home-hero-active",
		heroFillsViewport,
	);
	homeReferenceMenu.classList.toggle(
		"is-home-hero-inactive",
		!heroFillsViewport,
	);
	homeReferenceMenu.toggleAttribute("inert", !heroFillsViewport);
}

/**
 * Coalesce scroll and viewport events into one layout read per animation frame.
 *
 * @returns {void}
 */
function scheduleHomeReferenceMenuGeometrySync() {
	if (
		!homeReferenceMenu ||
		!homeHero ||
		homeReferenceMenuSyncFrame !== null
	) {
		return;
	}

	homeReferenceMenuSyncFrame = window.requestAnimationFrame(
		syncHomeReferenceMenuGeometry,
	);
}

/**
 * Move focus to the Projects destination before native fragment navigation
 * scrolls the landing disclosure out of its active hero state.
 *
 * @returns {void}
 */
function focusHomeDisciplines() {
	if (!homeDisciplines) {
		return;
	}

	homeDisciplines.focus({ preventScroll: true });
}

/**
 * Follow the Interior hero's real fragment link with optional smooth movement.
 *
 * The server-rendered href remains a complete no-script fallback. Enhancement
 * first moves focus to the labelled destination, then scrolls it into view and
 * records the same hash native navigation would have produced. Visitors who
 * request reduced motion receive an immediate scroll instead.
 *
 * @param {MouseEvent} event The activation of the Interior scroll cue.
 * @returns {void}
 */
function revealInteriorWork(event) {
	if (!interiorWork) {
		return;
	}

	event.preventDefault();
	interiorWork.focus({ preventScroll: true });
	interiorWork.scrollIntoView({
		behavior: window.matchMedia(
			"(prefers-reduced-motion: reduce)",
		).matches ? "auto" : "smooth",
		block: "start",
	});

	if (window.location.hash !== "#selected-work") {
		window.history.pushState(null, "", "#selected-work");
	}
}

/* A missing menu button simply leaves the server-rendered page unenhanced. */
if (openButton) {
	openButton.addEventListener("click", openMenu);
}

/*
 * Register every dismiss control separately. The click callback intentionally
 * ignores button-specific data because all close controls share one outcome.
 */
closeButtons.forEach((button) => {
	button.addEventListener("click", () => {
		closeMenu();
	});
});

/*
 * Clear drawer state before a real menu link follows its normal server-rendered
 * URL. This matters if navigation is cancelled or restored from browser cache.
 */
menuLinks.forEach((link) => {
	link.addEventListener("click", () => {
		closeMenu();
	});
});

/*
 * One document-level keyboard listener handles both Escape dismissal and the
 * Tab focus loop. Escape is consumed only while the menu is open; all other
 * keys pass through to normal browser behavior.
 */
document.addEventListener("keydown", (event) => {
	if (event.key === "Escape" && isMenuOpen()) {
		event.preventDefault();
		closeMenu();
		return;
	}

	trapFocus(event);
});

/*
 * `inert` and the keydown trap cover normal navigation, while focusin catches
 * programmatic or assistive-technology focus that lands outside the panel. It
 * immediately redirects that focus to the drawer's preferred starting point.
 */
document.addEventListener("focusin", (event) => {
	if (
		!drawerPanel ||
		!isMenuOpen() ||
		(event.target instanceof Node &&
			drawerPanel.contains(event.target))
	) {
		return;
	}

	focusDrawer();
});

/*
 * pageshow fires after ordinary loads and back-forward-cache restoration. It
 * clears stale modal state and remeasures the landing rail without overriding
 * the disclosure state preserved by the browser.
 */
window.addEventListener("pageshow", () => {
	resetMenu();
	scheduleHomeReferenceMenuGeometrySync();
});

/*
 * Passive scroll observation cannot delay scrolling. Resize and hash changes
 * cover desktop window changes, fragment navigation, and restored URL state;
 * visualViewport handles browser-chrome changes on capable touch devices.
 */
if (homeReferenceMenu && homeHero) {
	window.addEventListener(
		"scroll",
		scheduleHomeReferenceMenuGeometrySync,
		{ passive: true },
	);
	window.addEventListener(
		"resize",
		scheduleHomeReferenceMenuGeometrySync,
	);
	window.addEventListener(
		"hashchange",
		scheduleHomeReferenceMenuGeometrySync,
	);
	window.visualViewport?.addEventListener(
		"resize",
		scheduleHomeReferenceMenuGeometrySync,
	);
}

/* Preserve native fragment navigation while preventing focus from being hidden. */
if (homeProjectsLink && homeDisciplines) {
	homeProjectsLink.addEventListener("click", focusHomeDisciplines);
}

/* Preserve the native href when either route-specific enhancement hook is absent. */
if (interiorScrollLink && interiorWork) {
	interiorScrollLink.addEventListener("click", revealInteriorWork);
}

/*
 * Reveal the enhanced control only after parsing, dependency discovery, and
 * listener registration all complete. If this script is blocked, throws, or
 * encounters incomplete markup, the native details navigation remains usable.
 */
if (
	pageShell &&
	drawer &&
	drawerPanel &&
	openButton &&
	closeButtons.length > 0
) {
	document.documentElement.classList.add(
		"has-enhanced-navigation",
	);
}

/* Geometry enhancement is independent of the compact modal drawer controller. */
if (homeReferenceMenu && homeHero) {
	document.documentElement.classList.add(
		"has-enhanced-home-reference-menu",
	);
}

/* Establish the correct full-height or outside-hero state before interaction. */
syncHomeReferenceMenuGeometry();
