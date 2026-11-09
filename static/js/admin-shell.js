(() => {
	"use strict";

	const body = document.querySelector(".admin-body--workspace");
	if (!(body instanceof HTMLElement)) {
		return;
	}

	const toggle = document.querySelector(".admin-sidebar-toggle");
	const toggleLabel = document.querySelector(".admin-sidebar-toggle__label");
	const sidebarStorageKey = "zafarmand-admin-sidebar-collapsed";

	// Storage is optional: privacy settings must never make navigation unusable.
	const readSidebarPreference = () => {
		try {
			return window.localStorage.getItem(sidebarStorageKey) === "true";
		} catch {
			return false;
		}
	};

	const writeSidebarPreference = (collapsed) => {
		try {
			window.localStorage.setItem(sidebarStorageKey, String(collapsed));
		} catch {
			// The visible state still works when storage is unavailable.
		}
	};

	const renderSidebar = (collapsed) => {
		body.classList.toggle("admin-body--sidebar-collapsed", collapsed);
		if (!(toggle instanceof HTMLButtonElement)) {
			return;
		}

		toggle.setAttribute("aria-expanded", String(!collapsed));
		toggle.title = collapsed ? "Expand sidebar" : "Collapse sidebar";
		if (toggleLabel instanceof HTMLElement) {
			toggleLabel.textContent = collapsed ? "Expand sidebar" : "Collapse sidebar";
		}
	};

	if (toggle instanceof HTMLButtonElement) {
		renderSidebar(readSidebarPreference());
		toggle.hidden = false;
		toggle.addEventListener("click", () => {
			const collapsed = !body.classList.contains("admin-body--sidebar-collapsed");
			renderSidebar(collapsed);
			writeSidebarPreference(collapsed);
		});
	}
	window.requestAnimationFrame(() => body.classList.add("admin-shell-ready"));

	const currentDate = document.querySelector("[data-admin-current-date]");
	if (currentDate instanceof HTMLElement) {
		currentDate.textContent = new Intl.DateTimeFormat("en", {
			month: "short",
			day: "numeric",
			year: "numeric",
		}).format(new Date());
	}

	const themeButton = document.querySelector("[data-admin-theme-toggle]");
	const themeLabel = document.querySelector("[data-admin-theme-label]");
	const themeStorageKey = "zafarmand-admin-theme";
	const topbarMenus = Array.from(document.querySelectorAll(".admin-topbar-menu"));
	const renderTheme = (light) => {
		body.classList.toggle("admin-theme--light", light);
		if (themeButton instanceof HTMLButtonElement) {
			themeButton.setAttribute("aria-pressed", String(light));
			themeButton.title = light ? "Use dark theme" : "Use light theme";
		}
		if (themeLabel instanceof HTMLElement) {
			themeLabel.textContent = light ? "Use dark theme" : "Use light theme";
		}
	};

	let lightTheme = false;
	try {
		lightTheme = window.localStorage.getItem(themeStorageKey) === "light";
	} catch {
		// The dark architectural theme remains the safe default.
	}
	renderTheme(lightTheme);
	themeButton?.addEventListener("click", () => {
		lightTheme = !body.classList.contains("admin-theme--light");
		renderTheme(lightTheme);
		try {
			window.localStorage.setItem(themeStorageKey, lightTheme ? "light" : "dark");
		} catch {
			// Theme selection still works for the current page.
		}
	});

	for (const menu of topbarMenus) {
		menu.addEventListener("toggle", () => {
			if (!(menu instanceof HTMLDetailsElement) || !menu.open) {
				return;
			}
			for (const other of topbarMenus) {
				if (other instanceof HTMLDetailsElement && other !== menu) {
					other.open = false;
				}
			}
		});
	}

	const palette = document.querySelector("[data-admin-command-palette]");
	const openButton = document.querySelector("[data-admin-command-open]");
	const input = document.querySelector("[data-admin-command-input]");
	const empty = document.querySelector("[data-admin-command-empty]");
	const items = Array.from(document.querySelectorAll("[data-admin-command-item]"));
	const closeButtons = Array.from(document.querySelectorAll("[data-admin-command-close]"));
	let focusBeforePalette = null;

	const visibleItems = () => items.filter((item) => {
		return item instanceof HTMLAnchorElement && !item.closest("li")?.hidden;
	});

	const filterCommands = () => {
		if (!(input instanceof HTMLInputElement)) {
			return;
		}

		const query = input.value.trim().toLocaleLowerCase();
		let matches = 0;
		for (const item of items) {
			if (!(item instanceof HTMLAnchorElement)) {
				continue;
			}
			const row = item.closest("li");
			const matched = item.textContent.toLocaleLowerCase().includes(query);
			if (row instanceof HTMLLIElement) {
				row.hidden = !matched;
			}
			if (matched) {
				matches += 1;
			}
		}
		if (empty instanceof HTMLElement) {
			empty.hidden = matches !== 0;
		}
	};

	const setPaletteOpen = (open) => {
		if (!(palette instanceof HTMLElement)) {
			return;
		}

		palette.hidden = !open;
		body.classList.toggle("admin-body--command-open", open);
		if (openButton instanceof HTMLButtonElement) {
			openButton.setAttribute("aria-expanded", String(open));
		}

		if (open) {
			focusBeforePalette = document.activeElement instanceof HTMLElement
				? document.activeElement
				: null;
			if (input instanceof HTMLInputElement) {
				input.value = "";
				filterCommands();
				window.requestAnimationFrame(() => input.focus());
			}
			return;
		}

		if (focusBeforePalette instanceof HTMLElement) {
			focusBeforePalette.focus();
		}
	};

	if (openButton instanceof HTMLButtonElement && palette instanceof HTMLElement) {
		openButton.setAttribute("aria-expanded", "false");
		openButton.addEventListener("click", () => setPaletteOpen(true));
		for (const closeButton of closeButtons) {
			closeButton.addEventListener("click", () => setPaletteOpen(false));
		}
		if (input instanceof HTMLInputElement) {
			input.addEventListener("input", filterCommands);
		}
	}

	document.addEventListener("keydown", (event) => {
		if ((event.ctrlKey || event.metaKey) && event.key.toLocaleLowerCase() === "k") {
			event.preventDefault();
			setPaletteOpen(palette instanceof HTMLElement && palette.hidden);
			return;
		}

		const paletteIsOpen = palette instanceof HTMLElement && !palette.hidden;
		if (event.key === "Escape" && paletteIsOpen) {
			event.preventDefault();
			setPaletteOpen(false);
			return;
		}

		if (event.key === "Escape") {
			const openMenu = topbarMenus.find((menu) => {
				return menu instanceof HTMLDetailsElement && menu.open;
			});
			if (openMenu instanceof HTMLDetailsElement) {
				event.preventDefault();
				openMenu.open = false;
				openMenu.querySelector("summary")?.focus();
				return;
			}
		}

		if (paletteIsOpen && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
			const commands = visibleItems();
			if (commands.length === 0) {
				return;
			}
			event.preventDefault();
			const current = commands.indexOf(document.activeElement);
			const offset = event.key === "ArrowDown" ? 1 : -1;
			const next = current === -1
				? (offset === 1 ? 0 : commands.length - 1)
				: (current + offset + commands.length) % commands.length;
			commands[next].focus();
			return;
		}

		if (paletteIsOpen && event.key === "Enter" && document.activeElement === input) {
			const first = visibleItems()[0];
			if (first instanceof HTMLAnchorElement) {
				event.preventDefault();
				first.click();
			}
			return;
		}

		if (paletteIsOpen && event.key === "Tab") {
			const focusable = [
				input,
				...visibleItems(),
				...closeButtons,
			].filter((element) => element instanceof HTMLElement && !element.hidden);
			if (focusable.length === 0) {
				return;
			}
			const first = focusable[0];
			const last = focusable[focusable.length - 1];
			if (event.shiftKey && document.activeElement === first) {
				event.preventDefault();
				last.focus();
			} else if (!event.shiftKey && document.activeElement === last) {
				event.preventDefault();
				first.focus();
			}
			return;
		}

		// Escape offers a predictable way back to the fully labelled navigation.
		if (
			event.key === "Escape" &&
			body.classList.contains("admin-body--sidebar-collapsed")
		) {
			renderSidebar(false);
			writeSidebarPreference(false);
			if (toggle instanceof HTMLButtonElement) {
				toggle.focus();
			}
		}
	});

	document.addEventListener("click", (event) => {
		for (const menu of topbarMenus) {
			if (!(menu instanceof HTMLDetailsElement) || !menu.open) {
				continue;
			}
			if (!menu.contains(event.target)) {
				menu.open = false;
			}
		}
	});
})();
