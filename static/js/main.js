"use strict";

const body = document.body;
const drawer = document.querySelector("[data-site-drawer]");
const openButton = document.querySelector("[data-menu-open]");
const closeButtons = document.querySelectorAll("[data-menu-close]");
const menuLinks = document.querySelectorAll("[data-menu-link]");

let previouslyFocusedElement = null;

const focusableSelector = [
	'a[href]',
	'button:not([disabled])',
	'input:not([disabled])',
	'select:not([disabled])',
	'textarea:not([disabled])',
	'[tabindex]:not([tabindex="-1"])',
].join(",");

function getFocusableElements() {
	if (!drawer) {
		return [];
	}

	return Array.from(
		drawer.querySelectorAll(focusableSelector),
	).filter((element) => {
		return element.offsetParent !== null;
	});
}

function openMenu() {
	if (!drawer || !openButton) {
		return;
	}

	previouslyFocusedElement = document.activeElement;

	drawer.classList.add("is-open");
	drawer.setAttribute("aria-hidden", "false");

	openButton.setAttribute("aria-expanded", "true");
	openButton.setAttribute(
		"aria-label",
		"Navigation menu is open",
	);

	body.classList.add("is-menu-open");

	const focusableElements = getFocusableElements();

	if (focusableElements.length > 0) {
		focusableElements[0].focus();
	}
}

function closeMenu() {
	if (!drawer || !openButton) {
		return;
	}

	drawer.classList.remove("is-open");
	drawer.setAttribute("aria-hidden", "true");

	openButton.setAttribute("aria-expanded", "false");
	openButton.setAttribute(
		"aria-label",
		"Open navigation menu",
	);

	body.classList.remove("is-menu-open");

	if (
		previouslyFocusedElement instanceof HTMLElement
	) {
		previouslyFocusedElement.focus();
	}
}

function trapFocus(event) {
	if (
		event.key !== "Tab" ||
		!drawer?.classList.contains("is-open")
	) {
		return;
	}

	const focusableElements = getFocusableElements();

	if (focusableElements.length === 0) {
		return;
	}

	const firstElement = focusableElements[0];
	const lastElement =
		focusableElements[focusableElements.length - 1];

	if (
		event.shiftKey &&
		document.activeElement === firstElement
	) {
		event.preventDefault();
		lastElement.focus();
		return;
	}

	if (
		!event.shiftKey &&
		document.activeElement === lastElement
	) {
		event.preventDefault();
		firstElement.focus();
	}
}

if (openButton) {
	openButton.addEventListener("click", openMenu);
}

closeButtons.forEach((button) => {
	button.addEventListener("click", closeMenu);
});

menuLinks.forEach((link) => {
	link.addEventListener("click", closeMenu);
});

document.addEventListener("keydown", (event) => {
	if (event.key === "Escape") {
		closeMenu();
		return;
	}

	trapFocus(event);
});

window.addEventListener("pageshow", () => {
	if (!drawer || !openButton) {
		return;
	}

	drawer.classList.remove("is-open");
	drawer.setAttribute("aria-hidden", "true");
	openButton.setAttribute("aria-expanded", "false");
	body.classList.remove("is-menu-open");
});