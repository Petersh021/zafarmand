(() => {
	"use strict";

	const tabs = Array.from(document.querySelectorAll("[data-reference-tab]"));
	const panel = document.querySelector("[data-reference-view-panel]");
	const rows = Array.from(document.querySelectorAll("[data-reference-row-index]"));
	const cards = Array.from(document.querySelectorAll("[data-reference-card-index]"));
	const bars = Array.from(document.querySelectorAll(".admin-reference__chart-bar"));
	const search = document.querySelector("[data-reference-search]");
	const sort = document.querySelector("[data-reference-sort]");
	const tableBody = document.querySelector("[data-reference-table-body]");
	const activeView = document.querySelector("[data-reference-active-view]");
	const resultCount = document.querySelector("[data-reference-result-count]");
	const announcer = document.querySelector("[data-reference-announcer]");
	let activeTabIndex = 0;

	const originalLevels = bars.map((bar) => {
		const match = Array.from(bar.classList).find((name) => name.startsWith("admin-reference__chart-bar--") && /--\d+$/.test(name));
		return match ? Number(match.split("--").at(-1)) : 5;
	});

	const tabAllowsIndex = (itemIndex, total) => {
		if (activeTabIndex === 0 || total === 0) return true;
		return itemIndex === (activeTabIndex - 1) % total;
	};

	const applyRows = () => {
		const query = search instanceof HTMLInputElement ? search.value.trim().toLocaleLowerCase() : "";
		let visible = 0;
		for (const [index, row] of rows.entries()) {
			if (!(row instanceof HTMLTableRowElement)) continue;
			const matchesView = tabAllowsIndex(index, rows.length);
			const matchesSearch = !query || row.textContent.toLocaleLowerCase().includes(query);
			row.hidden = !(matchesView && matchesSearch);
			if (!row.hidden) visible += 1;
		}
		if (resultCount instanceof HTMLElement) resultCount.textContent = String(visible);
	};

	const applyCards = () => {
		for (const [index, card] of cards.entries()) {
			if (card instanceof HTMLElement) card.hidden = !tabAllowsIndex(index, cards.length);
		}
	};

	const animateChart = () => {
		for (const [index, bar] of bars.entries()) {
			const original = originalLevels[index] || 5;
			const level = ((original + activeTabIndex * 2 - 1) % 10) + 1;
			for (let candidate = 1; candidate <= 10; candidate += 1) {
				bar.classList.remove(`admin-reference__chart-bar--${candidate}`);
			}
			bar.classList.add(`admin-reference__chart-bar--${level}`);
		}
		const chart = document.querySelector(".admin-reference__chart");
		if (chart instanceof HTMLElement) {
			chart.setAttribute("aria-label", `Illustrative chart for ${tabs[activeTabIndex]?.textContent.trim() || "the selected view"}; values are not live business data.`);
		}
	};

	const activateTab = (index, focus = false) => {
		if (index < 0 || index >= tabs.length) return;
		activeTabIndex = index;
		for (const [tabIndex, tab] of tabs.entries()) {
			if (!(tab instanceof HTMLButtonElement)) continue;
			const selected = tabIndex === index;
			tab.setAttribute("aria-selected", String(selected));
			tab.tabIndex = selected ? 0 : -1;
			tab.closest("li")?.classList.toggle("admin-reference__view--active", selected);
			if (selected && focus) tab.focus();
		}
		if (panel instanceof HTMLElement) panel.setAttribute("aria-labelledby", tabs[index].id);
		const label = tabs[index].textContent.trim();
		if (activeView instanceof HTMLElement) activeView.textContent = label;
		applyRows();
		applyCards();
		animateChart();
		if (announcer instanceof HTMLElement) announcer.textContent = `${label} preview selected.`;
	};

	for (const [index, tab] of tabs.entries()) {
		tab.addEventListener("click", () => activateTab(index));
		tab.addEventListener("keydown", (event) => {
			let next = index;
			if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
			else if (event.key === "ArrowLeft") next = (index - 1 + tabs.length) % tabs.length;
			else if (event.key === "Home") next = 0;
			else if (event.key === "End") next = tabs.length - 1;
			else return;
			event.preventDefault();
			activateTab(next, true);
		});
	}

	search?.addEventListener("input", applyRows);
	sort?.addEventListener("change", () => {
		if (!(sort instanceof HTMLSelectElement) || !(tableBody instanceof HTMLTableSectionElement)) return;
		const sorted = rows.slice().sort((left, right) => {
			const leftCells = left.querySelectorAll("th, td");
			const rightCells = right.querySelectorAll("th, td");
			const column = sort.value === "value" ? 2 : sort.value === "status" ? 3 : 0;
			if (sort.value === "original") {
				return Number(left.dataset.referenceRowIndex) - Number(right.dataset.referenceRowIndex);
			}
			return (leftCells[column]?.textContent || "").localeCompare(rightCells[column]?.textContent || "", undefined, { numeric: true });
		});
		tableBody.append(...sorted);
		applyRows();
	});

	document.querySelector("[data-reference-reset]")?.addEventListener("click", () => {
		if (search instanceof HTMLInputElement) search.value = "";
		if (sort instanceof HTMLSelectElement) sort.value = "original";
		if (tableBody instanceof HTMLTableSectionElement) {
			tableBody.append(...rows.slice().sort((left, right) => Number(left.dataset.referenceRowIndex) - Number(right.dataset.referenceRowIndex)));
		}
		activateTab(0, true);
	});

	document.querySelector("[data-reference-export]")?.addEventListener("click", () => {
		const visibleRows = rows.filter((row) => !row.hidden);
		const csvEscape = (value) => `"${String(value).replaceAll('"', '""')}"`;
		const lines = ["Item,Detail,Value,Status"];
		for (const row of visibleRows) {
			lines.push(Array.from(row.querySelectorAll("th, td")).map((cell) => csvEscape(cell.textContent.trim())).join(","));
		}
		const url = URL.createObjectURL(new Blob([lines.join("\r\n")], { type: "text/csv;charset=utf-8" }));
		const link = document.createElement("a");
		link.href = url;
		link.download = `${window.location.pathname.split("/").at(-1) || "preview"}-demo.csv`;
		link.click();
		URL.revokeObjectURL(url);
		if (announcer instanceof HTMLElement) announcer.textContent = `${visibleRows.length} visible preview records exported.`;
	});

	const workspace = document.querySelector("[data-reference-module]");
	const moduleName = workspace instanceof HTMLElement ? workspace.dataset.referenceModule || "" : "";
	const toast = document.querySelector("[data-reference-toast]");
	const toastMessage = document.querySelector("[data-reference-toast-message]");
	const toastUndo = document.querySelector("[data-reference-toast-undo]");
	let toastTimer = 0;
	let undoAction = null;

	const showToast = (message, undo = null) => {
		window.clearTimeout(toastTimer);
		undoAction = typeof undo === "function" ? undo : null;
		if (toastMessage instanceof HTMLElement) toastMessage.textContent = message;
		if (toastUndo instanceof HTMLButtonElement) toastUndo.hidden = undoAction === null;
		if (toast instanceof HTMLElement) toast.hidden = false;
		if (announcer instanceof HTMLElement) announcer.textContent = message;
		toastTimer = window.setTimeout(() => {
			if (toast instanceof HTMLElement) toast.hidden = true;
			undoAction = null;
		}, 6500);
	};

	toastUndo?.addEventListener("click", () => {
		window.clearTimeout(toastTimer);
		const action = undoAction;
		undoAction = null;
		if (toast instanceof HTMLElement) toast.hidden = true;
		if (typeof action === "function") action();
	});

	const dialog = document.querySelector("[data-reference-dialog]");
	const dialogForm = document.querySelector("[data-reference-dialog-form]");
	const dialogKicker = document.querySelector("[data-reference-dialog-kicker]");
	const dialogTitle = document.querySelector("[data-reference-dialog-title]");
	const dialogCopy = document.querySelector("[data-reference-dialog-copy]");
	const dialogField = document.querySelector("[data-reference-dialog-field]");
	const dialogFieldLabel = document.querySelector("[data-reference-dialog-field-label]");
	const dialogInput = document.querySelector("[data-reference-dialog-input]");
	const dialogConfirm = document.querySelector("[data-reference-dialog-confirm]");
	const dialogSecondary = document.querySelector("[data-reference-dialog-secondary]");
	let dialogConfirmAction = null;
	let dialogSecondaryAction = null;

	const setText = (element, value) => {
		if (element instanceof HTMLElement) element.textContent = value;
	};

	const openDialog = (options) => {
		if (!(dialog instanceof HTMLDialogElement)) return;
		setText(dialogKicker, options.kicker || "Browser preview");
		setText(dialogTitle, options.title || "Preview action");
		setText(dialogCopy, options.copy || "No backend record will be changed.");
		const hasField = Boolean(options.fieldLabel);
		if (dialogField instanceof HTMLElement) dialogField.hidden = !hasField;
		if (dialogInput instanceof HTMLInputElement) {
			dialogInput.disabled = !hasField;
			dialogInput.required = hasField;
			dialogInput.type = options.fieldType === "email" ? "email" : "text";
			dialogInput.value = options.fieldValue || "";
		}
		setText(dialogFieldLabel, options.fieldLabel || "Reference");
		if (dialogConfirm instanceof HTMLButtonElement) {
			dialogConfirm.hidden = !options.confirmText;
			dialogConfirm.textContent = options.confirmText || "Confirm preview";
		}
		if (dialogSecondary instanceof HTMLButtonElement) {
			dialogSecondary.hidden = !options.secondaryText;
			dialogSecondary.textContent = options.secondaryText || "Secondary action";
		}
		dialogConfirmAction = typeof options.onConfirm === "function" ? options.onConfirm : null;
		dialogSecondaryAction = typeof options.onSecondary === "function" ? options.onSecondary : null;
		dialog.returnValue = "";
		dialog.showModal();
		if (hasField && dialogInput instanceof HTMLInputElement) dialogInput.focus();
	};

	dialogSecondary?.addEventListener("click", () => {
		if (typeof dialogSecondaryAction === "function") dialogSecondaryAction();
	});

	dialog?.addEventListener("close", () => {
		if (!(dialog instanceof HTMLDialogElement) || dialog.returnValue !== "confirm") return;
		const value = dialogInput instanceof HTMLInputElement ? dialogInput.value.trim() : "";
		if (typeof dialogConfirmAction === "function") dialogConfirmAction(value);
	});

	const downloadCSV = (filename, lines) => {
		const url = URL.createObjectURL(new Blob([lines.join("\r\n")], { type: "text/csv;charset=utf-8" }));
		const link = document.createElement("a");
		link.href = url;
		link.download = filename;
		link.click();
		URL.revokeObjectURL(url);
	};

	if (moduleName === "analytics") {
		const analyticsRanges = { "30d": { label: "Last 30 days", offset: 0 }, "90d": { label: "Last 90 days", offset: 2 }, "12m": { label: "Last 12 months", offset: 4 } };
		const periodLabel = document.querySelector("[data-analytics-period-label]");
		for (const button of document.querySelectorAll("[data-analytics-range]")) {
			button.addEventListener("click", () => {
				const range = analyticsRanges[button.dataset.analyticsRange];
				if (!range) return;
				for (const candidate of document.querySelectorAll("[data-analytics-range]")) candidate.setAttribute("aria-pressed", String(candidate === button));
				setText(periodLabel, range.label);
				for (const [index, bar] of bars.entries()) {
					for (let level = 1; level <= 10; level += 1) bar.classList.remove(`admin-reference__chart-bar--${level}`);
					bar.classList.add(`admin-reference__chart-bar--${((originalLevels[index] + range.offset - 1) % 10) + 1}`);
				}
				showToast(`${range.label} analytics preview selected.`);
			});
		}
		document.querySelector("[data-analytics-metric]")?.addEventListener("change", (event) => {
			if (event.target instanceof HTMLSelectElement) showToast(`${event.target.selectedOptions[0]?.textContent || "Metric"} preview selected.`);
		});
	}

	const customerSegment = document.querySelector("[data-customer-segment]");
	const customerCards = Array.from(document.querySelectorAll("[data-customer-card]"));
	const customerVisible = document.querySelector("[data-customer-visible]");
	const filterCustomers = () => {
		const segment = customerSegment instanceof HTMLSelectElement ? customerSegment.value : "all";
		let visible = 0;
		for (const card of customerCards) {
			card.hidden = segment !== "all" && card.dataset.segment !== segment;
			if (!card.hidden) visible += 1;
		}
		setText(customerVisible, String(visible));
	};
	customerSegment?.addEventListener("change", filterCustomers);

	const financePeriod = document.querySelector("[data-finance-period]");
	financePeriod?.addEventListener("change", () => {
		if (!(financePeriod instanceof HTMLSelectElement)) return;
		setText(document.querySelector("[data-finance-period-label]"), financePeriod.selectedOptions[0]?.textContent || "Selected period");
		showToast("Financial preview period updated.");
	});

	const invoiceList = document.querySelector(".admin-reference__document-list");
	const invoiceCards = () => Array.from(document.querySelectorAll("[data-invoice-card]"));
	const appendInvoice = (client) => {
		if (!(invoiceList instanceof HTMLElement)) return null;
		const card = document.createElement("article");
		card.dataset.invoiceCard = "";
		card.dataset.invoiceStatus = "open";
		const detail = document.createElement("div");
		const reference = document.createElement("p");
		const number = `INV-${2050 + invoiceCards().length}`;
		reference.textContent = number;
		const title = document.createElement("h3");
		title.textContent = client;
		const meta = document.createElement("span");
		meta.textContent = "$0 sample draft - due in 14 days";
		detail.append(reference, title, meta);
		const status = document.createElement("span");
		status.className = "admin-reference__status admin-reference__status--amber";
		status.dataset.invoiceStatusLabel = "";
		status.textContent = "Open";
		const button = document.createElement("button");
		button.type = "button";
		button.dataset.referenceAction = "invoice-detail";
		button.dataset.referenceLabel = number;
		button.textContent = "Details";
		card.append(detail, status, button);
		invoiceList.prepend(card);
		return card;
	};

	const discountGrid = document.querySelector(".admin-reference--discounts .admin-reference__mini-grid");
	const appendDiscount = (codeValue) => {
		if (!(discountGrid instanceof HTMLElement)) return null;
		const code = codeValue.toLocaleUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 18) || "SAMPLE10";
		const card = document.createElement("article");
		const reference = document.createElement("p");
		reference.textContent = code;
		const title = document.createElement("h3");
		title.textContent = "New preview code";
		const meta = document.createElement("span");
		meta.textContent = "0 preview uses";
		const button = document.createElement("button");
		button.type = "button";
		button.dataset.discountToggle = "";
		button.setAttribute("aria-pressed", "true");
		button.textContent = "Active";
		card.append(reference, title, meta, button);
		discountGrid.prepend(card);
		return card;
	};

	const setDiscountButton = (button, active) => {
		button.setAttribute("aria-pressed", String(active));
		button.textContent = active ? "Active" : "Activate preview";
	};
	document.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const discountButton = event.target.closest("[data-discount-toggle]");
		if (discountButton instanceof HTMLButtonElement) {
			const previous = discountButton.getAttribute("aria-pressed") === "true";
			setDiscountButton(discountButton, !previous);
			showToast(`Discount preview ${previous ? "paused" : "activated"}.`, () => setDiscountButton(discountButton, previous));
		}
	});

	const chatThreads = Array.from(document.querySelectorAll("[data-chat-thread]"));
	const chatSearch = document.querySelector("[data-chat-search]");
	let chatStatus = "open";
	let activeChat = chatThreads[0] || null;
	const filterChats = () => {
		const query = chatSearch instanceof HTMLInputElement ? chatSearch.value.trim().toLocaleLowerCase() : "";
		for (const thread of chatThreads) {
			const matches = thread.dataset.chatStatus === chatStatus && (!query || thread.textContent.toLocaleLowerCase().includes(query));
			thread.hidden = !matches;
		}
	};
	for (const button of document.querySelectorAll("[data-chat-filter]")) {
		button.addEventListener("click", () => {
			chatStatus = button.dataset.chatFilter || "open";
			for (const candidate of document.querySelectorAll("[data-chat-filter]")) candidate.setAttribute("aria-pressed", String(candidate === button));
			filterChats();
		});
	}
	chatSearch?.addEventListener("input", filterChats);
	for (const thread of chatThreads) {
		thread.addEventListener("click", () => {
			activeChat = thread;
			for (const candidate of chatThreads) candidate.classList.toggle("is-active", candidate === thread);
			setText(document.querySelector("[data-chat-name]"), thread.dataset.chatName || "Preview customer");
			setText(document.querySelector("[data-chat-channel]"), `${thread.querySelector("span")?.textContent || "Preview"} conversation`);
		});
	}
	document.querySelector("[data-chat-close]")?.addEventListener("click", () => {
		if (!(activeChat instanceof HTMLElement)) return;
		const targetChat = activeChat;
		const previous = targetChat.dataset.chatStatus || "open";
		targetChat.dataset.chatStatus = "closed";
		filterChats();
		showToast("Conversation closed in this browser preview.", () => {
			targetChat.dataset.chatStatus = previous;
			filterChats();
		});
	});
	for (const button of document.querySelectorAll("[data-chat-quick]")) {
		button.addEventListener("click", () => {
			const reply = document.querySelector("[data-chat-reply]");
			if (reply instanceof HTMLTextAreaElement) {
				reply.value = button.dataset.chatQuick || "";
				reply.focus();
			}
		});
	}
	document.querySelector("[data-chat-form]")?.addEventListener("submit", (event) => {
		event.preventDefault();
		const reply = document.querySelector("[data-chat-reply]");
		const messages = document.querySelector("[data-chat-messages]");
		if (!(reply instanceof HTMLTextAreaElement) || !(messages instanceof HTMLElement) || !reply.value.trim()) return;
		const message = document.createElement("p");
		message.className = "is-studio";
		const sender = document.createElement("span");
		sender.textContent = "Zafarmand preview";
		message.append(sender, document.createTextNode(reply.value.trim()));
		messages.append(message);
		reply.value = "";
		showToast("Reply added locally; no message was sent.", () => message.remove());
	});

	const emailButtons = Array.from(document.querySelectorAll("[data-email-message]"));
	const emailSearch = document.querySelector("[data-email-search]");
	for (const button of emailButtons) {
		button.addEventListener("click", () => {
			for (const candidate of emailButtons) candidate.classList.toggle("is-active", candidate === button);
			setText(document.querySelector("[data-email-detail-subject]"), button.dataset.emailSubject || "Preview message");
			setText(document.querySelector("[data-email-detail-meta]"), button.querySelector("span")?.textContent || "Preview sender");
			setText(document.querySelector("[data-email-detail-copy]"), "This invented email demonstrates the selected-message layout. No mailbox is connected.");
		});
	}
	emailSearch?.addEventListener("input", () => {
		if (!(emailSearch instanceof HTMLInputElement)) return;
		const query = emailSearch.value.trim().toLocaleLowerCase();
		for (const button of emailButtons) button.hidden = Boolean(query) && !button.textContent.toLocaleLowerCase().includes(query);
	});

	const appReviewButtons = Array.from(document.querySelectorAll("[data-app-review]"));
	const updateAppCount = () => setText(document.querySelector("[data-app-review-count]"), String(appReviewButtons.filter((button) => button.dataset.reviewed === "true").length));
	for (const button of appReviewButtons) {
		button.addEventListener("click", () => {
			const previous = button.dataset.reviewed === "true";
			button.dataset.reviewed = String(!previous);
			button.textContent = previous ? "Request review" : "Review requested";
			updateAppCount();
			showToast(previous ? "Integration review request removed." : "Integration review requested locally.", () => {
				button.dataset.reviewed = String(previous);
				button.textContent = previous ? "Review requested" : "Request review";
				updateAppCount();
			});
		});
	}

	const settingsKey = "zafarmand-admin-reference-settings";
	const settingsRoot = document.querySelector(".admin-reference--settings");
	const density = document.querySelector("[data-settings-density]");
	const locale = document.querySelector("[data-settings-locale]");
	const motion = document.querySelector("[data-settings-motion]");
	const notifications = document.querySelector("[data-settings-notifications]");
	const readSettings = () => {
		try {
			const parsed = JSON.parse(window.localStorage.getItem(settingsKey) || "{}");
			return parsed && typeof parsed === "object" ? parsed : {};
		} catch { return {}; }
	};
	const renderSettings = (state) => {
		if (density instanceof HTMLSelectElement) density.value = state.density === "compact" ? "compact" : "comfortable";
		if (locale instanceof HTMLSelectElement) locale.value = state.locale === "fa" ? "fa" : "en";
		if (motion instanceof HTMLInputElement) motion.checked = state.motion !== false;
		if (notifications instanceof HTMLInputElement) notifications.checked = state.notifications === true;
		settingsRoot?.classList.toggle("admin-reference--compact", state.density === "compact");
		settingsRoot?.classList.toggle("admin-reference--no-motion", state.motion === false);
	};
	const saveSettings = () => {
		const state = {
			density: density instanceof HTMLSelectElement ? density.value : "comfortable",
			locale: locale instanceof HTMLSelectElement ? locale.value : "en",
			motion: motion instanceof HTMLInputElement ? motion.checked : true,
			notifications: notifications instanceof HTMLInputElement ? notifications.checked : false,
		};
		try { window.localStorage.setItem(settingsKey, JSON.stringify(state)); } catch { /* Settings still update visually. */ }
		renderSettings(state);
		showToast("Workspace preview preferences saved in this browser.");
	};
	for (const control of [density, locale, motion, notifications]) control?.addEventListener("change", saveSettings);
	document.querySelector("[data-settings-reset]")?.addEventListener("click", () => {
		try { window.localStorage.removeItem(settingsKey); } catch { /* Reset still updates this page. */ }
		renderSettings({});
		showToast("Workspace preview preferences reset.");
	});
	if (moduleName === "settings") renderSettings(readSettings());

	const helpSearch = document.querySelector("[data-help-search]");
	helpSearch?.addEventListener("input", () => {
		if (!(helpSearch instanceof HTMLInputElement)) return;
		const query = helpSearch.value.trim().toLocaleLowerCase();
		for (const item of document.querySelectorAll("[data-help-faq] details")) item.hidden = Boolean(query) && !item.textContent.toLocaleLowerCase().includes(query);
	});

	document.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const action = event.target.closest("[data-reference-action]");
		if (!(action instanceof HTMLButtonElement)) return;
		const kind = action.dataset.referenceAction || "";
		const label = action.dataset.referenceLabel || "Preview record";
		if (kind === "email-report") {
			openDialog({ title: "Email analytics report", copy: "Enter a recipient for this browser-only demonstration. No email will be sent.", fieldLabel: "Recipient email", fieldType: "email", confirmText: "Prepare report", onConfirm: () => showToast("Analytics report prepared locally; no email was sent.") });
		} else if (kind === "profile") {
			openDialog({ title: label, copy: "Sample profile with order, project, segment, and review history. This identity is invented.", confirmText: "" });
		} else if (kind === "review-profile") {
			const previous = action.textContent;
			action.textContent = "Reviewed";
			action.disabled = true;
			showToast(`${label} marked reviewed in this preview.`, () => { action.textContent = previous; action.disabled = false; });
		} else if (kind === "new-segment") {
			openDialog({ title: "Create sample segment", copy: "This segment is not saved to a customer backend.", fieldLabel: "Segment name", confirmText: "Create preview", onConfirm: (value) => showToast(`${value || "Segment"} created for this page preview.`) });
		} else if (kind === "finance-export") {
			downloadCSV("zafarmand-finance-preview.csv", ["Category,Amount,Status", "Materials,2840,Recorded", "Fulfilment,1420,Recorded", "Studio services,980,Review"]);
			showToast("Sample financial statement exported.");
		} else if (kind === "new-expense") {
			openDialog({ title: "Add sample expense", copy: "This is a local planning example and does not change a ledger.", fieldLabel: "Expense description", confirmText: "Add preview expense", onConfirm: (value) => showToast(`${value || "Expense"} added to the local preview.`) });
		} else if (kind === "new-invoice") {
			openDialog({ title: "New sample invoice", copy: "Add an invented client to the browser-only document queue.", fieldLabel: "Client name", confirmText: "Create sample invoice", onConfirm: (value) => {
				const card = appendInvoice(value || "Sample client");
				showToast("Sample invoice added to this page.", () => card?.remove());
			} });
		} else if (kind === "invoice-detail") {
			const card = action.closest("[data-invoice-card]");
			const paid = card?.dataset.invoiceStatus === "paid";
			openDialog({ title: label, copy: "Sample line items, client details, due date, and payment timeline. No accounting record is connected.", secondaryText: "Print preview", onSecondary: () => window.print(), confirmText: paid ? "" : "Mark paid", onConfirm: () => {
				if (!(card instanceof HTMLElement)) return;
				const status = card.querySelector("[data-invoice-status-label]");
				const previous = card.dataset.invoiceStatus || "open";
				const previousText = status?.textContent || "Open";
				card.dataset.invoiceStatus = "paid";
				if (status instanceof HTMLElement) { status.textContent = "Paid"; status.className = "admin-reference__status admin-reference__status--green"; }
				showToast(`${label} marked paid in this preview.`, () => {
					card.dataset.invoiceStatus = previous;
					if (status instanceof HTMLElement) { status.textContent = previousText; status.className = `admin-reference__status admin-reference__status--${previous === "overdue" ? "red" : "amber"}`; }
				});
			} });
		} else if (kind === "new-discount") {
			openDialog({ title: "New sample discount", copy: "Create a local code for the preview campaign list.", fieldLabel: "Code", confirmText: "Create code", onConfirm: (value) => {
				const card = appendDiscount(value);
				showToast("Sample discount code created.", () => card?.remove());
			} });
		} else if (kind === "compose-email" || kind === "reply-email") {
			openDialog({ title: kind === "compose-email" ? "Compose sample email" : "Reply in preview", copy: "No mailbox is connected and this message will not be sent.", fieldLabel: "Subject", confirmText: "Save local draft", onConfirm: () => showToast("Email saved as a local preview draft; nothing was sent.") });
		} else if (kind === "flag-email") {
			const previous = action.getAttribute("aria-pressed") === "true";
			action.setAttribute("aria-pressed", String(!previous));
			action.textContent = previous ? "Flag preview" : "Flagged";
			showToast(previous ? "Preview flag removed." : "Preview email flagged.");
		} else if (kind === "support-ticket") {
			openDialog({ title: "Open sample support ticket", copy: "This demonstrates the form only; no ticket is transmitted.", fieldLabel: "Issue summary", confirmText: "Save local ticket", onConfirm: () => showToast("Support ticket saved locally; no request was sent.") });
		}
	});

	activateTab(0);
})();
