(() => {
	"use strict";

	const tabs = Array.from(document.querySelectorAll("[data-orders-tab]"));
	const panels = Array.from(document.querySelectorAll("[data-orders-panel]"));
	if (tabs.length === 0 || panels.length === 0) {
		return;
	}

	const allowedTabs = new Set(["all", "board", "drafts", "returns"]);
	const boardStatuses = new Set(["to-pack", "packed", "shipped", "delivered"]);
	const statusLabels = {
		paid: "Paid",
		"to-pack": "To pack",
		packed: "Packed",
		shipped: "Shipped",
		delivered: "Delivered",
		returned: "Returned",
	};
	const boardStorageKey = "zafarmand-admin-orders-board";
	const ordersStorageKey = "zafarmand-admin-demo-orders";
	const announcer = document.querySelector("[data-orders-announcer]");

	const announce = (message) => {
		if (announcer instanceof HTMLElement) {
			announcer.textContent = message;
		}
	};

	const activateTab = (name, focus = false) => {
		if (!allowedTabs.has(name)) {
			return;
		}
		for (const tab of tabs) {
			if (!(tab instanceof HTMLButtonElement)) {
				continue;
			}
			const selected = tab.dataset.ordersTab === name;
			tab.setAttribute("aria-selected", String(selected));
			tab.tabIndex = selected ? 0 : -1;
			if (selected && focus) {
				tab.focus();
			}
		}
		for (const panel of panels) {
			if (panel instanceof HTMLElement) {
				panel.hidden = panel.dataset.ordersPanel !== name;
			}
		}
		announce(`${tabs.find((tab) => tab.dataset.ordersTab === name)?.textContent.trim() || name} view selected.`);
	};

	for (const [index, tab] of tabs.entries()) {
		tab.addEventListener("click", () => activateTab(tab.dataset.ordersTab || "all"));
		tab.addEventListener("keydown", (event) => {
			let next = index;
			if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
			else if (event.key === "ArrowLeft") next = (index - 1 + tabs.length) % tabs.length;
			else if (event.key === "Home") next = 0;
			else if (event.key === "End") next = tabs.length - 1;
			else return;
			event.preventDefault();
			activateTab(tabs[next].dataset.ordersTab || "all", true);
		});
	}

	const query = new URLSearchParams(window.location.search);
	const requestedView = query.get("view");
	activateTab(allowedTabs.has(requestedView) ? requestedView : "all");

	const toast = document.querySelector("[data-orders-toast]");
	const toastMessage = document.querySelector("[data-orders-toast-message]");
	const undoButton = document.querySelector("[data-orders-undo]");
	let toastTimer = 0;
	let undoAction = null;

	const hideToast = () => {
		if (toast instanceof HTMLElement) toast.hidden = true;
		undoAction = null;
	};

	const showToast = (message, undo = null) => {
		window.clearTimeout(toastTimer);
		if (toastMessage instanceof HTMLElement) toastMessage.textContent = message;
		undoAction = undo;
		if (undoButton instanceof HTMLButtonElement) undoButton.hidden = typeof undo !== "function";
		if (toast instanceof HTMLElement) toast.hidden = false;
		announce(message);
		toastTimer = window.setTimeout(hideToast, 6500);
	};

	undoButton?.addEventListener("click", () => {
		if (typeof undoAction === "function") undoAction();
		hideToast();
	});

	const rows = () => Array.from(document.querySelectorAll("[data-order-row]"));
	const cards = () => Array.from(document.querySelectorAll("[data-order-card]"));
	const updateAllCount = () => {
		const count = document.querySelector('[data-orders-count="all"]');
		if (count instanceof HTMLElement) {
			count.textContent = String(rows().length);
		}
	};
	const search = document.querySelector("[data-orders-search]");
	const statusFilter = document.querySelector("[data-orders-status-filter]");
	const channelFilter = document.querySelector("[data-orders-channel-filter]");
	const dateFilter = document.querySelector("[data-orders-date-filter]");
	const sort = document.querySelector("[data-orders-sort]");
	const resultCount = document.querySelector("[data-orders-result-count]");
	const empty = document.querySelector("[data-orders-empty]");
	const tableBody = document.querySelector("[data-orders-table-body]");
	const selectAll = document.querySelector("[data-orders-select-all]");
	const selectionCount = document.querySelector("[data-orders-selection-count]");
	const bulkAction = document.querySelector("[data-orders-bulk-action]");
	const bulkApply = document.querySelector("[data-orders-bulk-apply]");

	const slugLabel = (value) => value.split("-").map((part) => {
		return part.charAt(0).toUpperCase() + part.slice(1);
	}).join(" ");

	const visibleRows = () => rows().filter((row) => row instanceof HTMLTableRowElement && !row.hidden);
	const selectedRows = () => rows().filter((row) => {
		const checkbox = row.querySelector("[data-order-select]");
		return checkbox instanceof HTMLInputElement && checkbox.checked;
	});

	const updateSelection = () => {
		const selected = selectedRows().length;
		if (selectionCount instanceof HTMLElement) selectionCount.textContent = `${selected} selected`;
		if (bulkApply instanceof HTMLButtonElement) {
			bulkApply.disabled = selected === 0 || !(bulkAction instanceof HTMLSelectElement) || !bulkAction.value;
		}
		if (selectAll instanceof HTMLInputElement) {
			const visible = visibleRows();
			const checked = visible.filter((row) => {
				const checkbox = row.querySelector("[data-order-select]");
				return checkbox instanceof HTMLInputElement && checkbox.checked;
			}).length;
			selectAll.checked = visible.length > 0 && checked === visible.length;
			selectAll.indeterminate = checked > 0 && checked < visible.length;
		}
	};

	const matchesDate = (dateString, filter) => {
		if (filter === "all") return true;
		const date = new Date(`${dateString}T12:00:00`);
		if (Number.isNaN(date.getTime())) return false;
		const today = new Date();
		today.setHours(12, 0, 0, 0);
		const age = Math.floor((today.getTime() - date.getTime()) / 86400000);
		if (filter === "today") return age === 0;
		if (filter === "7d") return age >= 0 && age <= 7;
		if (filter === "30d") return age >= 0 && age <= 30;
		return true;
	};

	const applyFilters = () => {
		const text = search instanceof HTMLInputElement ? search.value.trim().toLocaleLowerCase() : "";
		const status = statusFilter instanceof HTMLSelectElement ? statusFilter.value : "all";
		const channel = channelFilter instanceof HTMLSelectElement ? channelFilter.value : "all";
		const date = dateFilter instanceof HTMLSelectElement ? dateFilter.value : "all";
		let visible = 0;

		for (const row of rows()) {
			if (!(row instanceof HTMLTableRowElement)) continue;
			const searchable = `${row.dataset.orderId || ""} ${row.dataset.customer || ""}`.toLocaleLowerCase();
			const matched = (!text || searchable.includes(text)) &&
				(status === "all" || row.dataset.status === status) &&
				(channel === "all" || row.dataset.channel === channel) &&
				matchesDate(row.dataset.date || "", date);
			row.hidden = !matched;
			if (matched) visible += 1;
		}

		if (resultCount instanceof HTMLElement) resultCount.textContent = `${visible} ${visible === 1 ? "order" : "orders"}`;
		if (empty instanceof HTMLElement) empty.hidden = visible !== 0;
		updateSelection();
	};

	const applySort = () => {
		if (!(tableBody instanceof HTMLTableSectionElement) || !(sort instanceof HTMLSelectElement)) return;
		const sorted = rows().slice().sort((left, right) => {
			const mode = sort.value;
			if (mode === "oldest") return (left.dataset.date || "").localeCompare(right.dataset.date || "");
			if (mode === "total-desc") return Number(right.dataset.total || 0) - Number(left.dataset.total || 0);
			if (mode === "total-asc") return Number(left.dataset.total || 0) - Number(right.dataset.total || 0);
			if (mode === "customer") return (left.dataset.customer || "").localeCompare(right.dataset.customer || "");
			return (right.dataset.date || "").localeCompare(left.dataset.date || "");
		});
		tableBody.append(...sorted);
	};

	for (const control of [search, statusFilter, channelFilter, dateFilter]) {
		control?.addEventListener(control === search ? "input" : "change", applyFilters);
	}
	sort?.addEventListener("change", () => { applySort(); applyFilters(); });
	document.querySelector("[data-orders-clear]")?.addEventListener("click", () => {
		if (search instanceof HTMLInputElement) search.value = "";
		if (statusFilter instanceof HTMLSelectElement) statusFilter.value = "all";
		if (channelFilter instanceof HTMLSelectElement) channelFilter.value = "all";
		if (dateFilter instanceof HTMLSelectElement) dateFilter.value = "all";
		if (sort instanceof HTMLSelectElement) sort.value = "newest";
		applySort();
		applyFilters();
		announce("Order filters cleared.");
	});

	document.addEventListener("change", (event) => {
		if (event.target instanceof HTMLInputElement && event.target.matches("[data-order-select]")) updateSelection();
	});
	selectAll?.addEventListener("change", () => {
		if (!(selectAll instanceof HTMLInputElement)) return;
		for (const row of visibleRows()) {
			const checkbox = row.querySelector("[data-order-select]");
			if (checkbox instanceof HTMLInputElement) checkbox.checked = selectAll.checked;
		}
		updateSelection();
	});
	bulkAction?.addEventListener("change", updateSelection);

	const statusClass = (status) => {
		if (status === "returned") return "admin-orders__status--red";
		if (status === "to-pack") return "admin-orders__status--amber";
		if (["packed", "shipped", "delivered"].includes(status)) return "admin-orders__status--sage";
		return "";
	};

	const updateRowStatus = (id, status) => {
		const row = rows().find((entry) => entry.dataset.orderId === id);
		if (!(row instanceof HTMLTableRowElement)) return;
		row.dataset.status = status;
		const label = row.querySelector("[data-order-status-label]");
		if (label instanceof HTMLElement) {
			label.textContent = statusLabels[status] || slugLabel(status);
			label.className = `admin-orders__status ${statusClass(status)}`.trim();
		}
	};

	const initialBoardState = Object.fromEntries(cards().map((card) => [card.dataset.orderId, card.dataset.status]));
	const readBoardState = () => {
		try {
			const parsed = JSON.parse(window.localStorage.getItem(boardStorageKey) || "{}");
			if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
			return Object.fromEntries(Object.entries(parsed).filter(([id, status]) => {
				return typeof id === "string" && typeof status === "string" && boardStatuses.has(status);
			}));
		} catch {
			return {};
		}
	};

	const persistBoard = () => {
		const state = Object.fromEntries(cards().map((card) => [card.dataset.orderId, card.dataset.status]));
		try {
			window.localStorage.setItem(boardStorageKey, JSON.stringify(state));
		} catch {
			// The board remains usable for the current page when storage is blocked.
		}
	};

	const updateBoardCounts = () => {
		for (const status of boardStatuses) {
			const count = cards().filter((card) => card.dataset.status === status).length;
			for (const target of document.querySelectorAll(`[data-order-column-count="${status}"]`)) {
				target.textContent = String(count);
			}
		}
		const boardCount = document.querySelector('[data-orders-count="board"]');
		if (boardCount instanceof HTMLElement) boardCount.textContent = String(cards().length);
	};

	const placeCard = (card, status) => {
		if (!(card instanceof HTMLElement) || !boardStatuses.has(status)) return false;
		const zone = document.querySelector(`[data-order-dropzone="${status}"]`);
		if (!(zone instanceof HTMLElement)) return false;
		zone.append(card);
		card.dataset.status = status;
		const move = card.querySelector("[data-order-move]");
		if (move instanceof HTMLSelectElement) move.value = status;
		updateRowStatus(card.dataset.orderId || "", status);
		return true;
	};

	const moveCard = (card, status, options = {}) => {
		if (!(card instanceof HTMLElement) || !boardStatuses.has(status)) return;
		const previous = card.dataset.status || "to-pack";
		if (previous === status) return;
		if (!placeCard(card, status)) return;
		updateBoardCounts();
		persistBoard();
		applyFilters();
		const id = card.dataset.orderId || "Order";
		if (!options.silent) {
			showToast(`${id} · ${statusLabels[status]}`, () => {
				placeCard(card, previous);
				updateBoardCounts();
				persistBoard();
				applyFilters();
				announce(`${id} returned to ${statusLabels[previous]}.`);
			});
		}
	};

	for (const [id, status] of Object.entries(readBoardState())) {
		const card = cards().find((entry) => entry.dataset.orderId === id);
		if (card) placeCard(card, status);
	}
	updateBoardCounts();

	let draggedCard = null;
	for (const card of cards()) {
		card.addEventListener("dragstart", (event) => {
			draggedCard = card;
			card.classList.add("admin-order-card--dragging");
			if (event.dataTransfer) {
				event.dataTransfer.effectAllowed = "move";
				event.dataTransfer.setData("text/plain", card.dataset.orderId || "");
			}
		});
		card.addEventListener("dragend", () => {
			card.classList.remove("admin-order-card--dragging");
			draggedCard = null;
			for (const zone of document.querySelectorAll("[data-order-dropzone]")) zone.classList.remove("admin-orders__board-cards--over");
		});
		const move = card.querySelector("[data-order-move]");
		move?.addEventListener("change", () => {
			if (move instanceof HTMLSelectElement) moveCard(card, move.value);
		});
	}

	for (const zone of document.querySelectorAll("[data-order-dropzone]")) {
		zone.addEventListener("dragover", (event) => {
			event.preventDefault();
			zone.classList.add("admin-orders__board-cards--over");
			if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
		});
		zone.addEventListener("dragleave", () => zone.classList.remove("admin-orders__board-cards--over"));
		zone.addEventListener("drop", (event) => {
			event.preventDefault();
			zone.classList.remove("admin-orders__board-cards--over");
			if (draggedCard instanceof HTMLElement) moveCard(draggedCard, zone.dataset.orderDropzone || "");
		});
	}

	document.querySelector("[data-orders-reset-board]")?.addEventListener("click", () => {
		for (const [id, status] of Object.entries(initialBoardState)) {
			const card = cards().find((entry) => entry.dataset.orderId === id);
			if (card) placeCard(card, status);
		}
		try { window.localStorage.removeItem(boardStorageKey); } catch { /* Reset still applies visually. */ }
		updateBoardCounts();
		applyFilters();
		showToast("Packing board reset to its original preview state.");
	});

	bulkApply?.addEventListener("click", () => {
		if (!(bulkAction instanceof HTMLSelectElement) || !boardStatuses.has(bulkAction.value)) return;
		const affected = selectedRows();
		const previous = affected.map((row) => [row.dataset.orderId, row.dataset.status]);
		for (const row of affected) {
			const id = row.dataset.orderId || "";
			updateRowStatus(id, bulkAction.value);
			const card = cards().find((entry) => entry.dataset.orderId === id);
			if (card) placeCard(card, bulkAction.value);
			const checkbox = row.querySelector("[data-order-select]");
			if (checkbox instanceof HTMLInputElement) checkbox.checked = false;
		}
		persistBoard();
		updateBoardCounts();
		applyFilters();
		showToast(`${affected.length} preview orders moved to ${statusLabels[bulkAction.value]}.`, () => {
			for (const [id, status] of previous) {
				updateRowStatus(id || "", status || "paid");
				const card = cards().find((entry) => entry.dataset.orderId === id);
				if (card && status && boardStatuses.has(status)) placeCard(card, status);
			}
			persistBoard();
			updateBoardCounts();
			applyFilters();
		});
		bulkAction.value = "";
		updateSelection();
	});

	const csvEscape = (value) => `"${String(value).replaceAll('"', '""')}"`;
	const exportCSV = () => {
		const records = visibleRows();
		const lines = ["Order,Customer,Date,Channel,Items,Total,Status"];
		for (const row of records) {
			lines.push([
				row.dataset.orderId,
				row.dataset.customer,
				row.dataset.date,
				slugLabel(row.dataset.channel || ""),
				row.dataset.items,
				row.dataset.total,
				statusLabels[row.dataset.status || ""] || row.dataset.status,
			].map(csvEscape).join(","));
		}
		const url = URL.createObjectURL(new Blob([lines.join("\r\n")], { type: "text/csv;charset=utf-8" }));
		const link = document.createElement("a");
		link.href = url;
		link.download = "zafarmand-demo-orders.csv";
		link.click();
		URL.revokeObjectURL(url);
		showToast(`${records.length} visible preview orders exported.`);
	};
	for (const button of document.querySelectorAll("[data-orders-export]")) button.addEventListener("click", exportCSV);

	const detailDialog = document.querySelector("[data-orders-detail-dialog]");
	let activeDetailIndex = 0;
	const detailOrderIDs = () => rows().map((row) => row.dataset.orderId).filter(Boolean);

	const renderDetails = (id) => {
		const row = rows().find((entry) => entry.dataset.orderId === id);
		const card = cards().find((entry) => entry.dataset.orderId === id);
		const set = (selector, value) => {
			const target = detailDialog?.querySelector(selector);
			if (target instanceof HTMLElement) target.textContent = value;
		};
		set("[data-order-dialog-id]", id || "Preview order");
		set("[data-order-dialog-customer]", row?.dataset.customer || card?.querySelector(".admin-order-card__customer b")?.textContent || "Preview customer");
		set("[data-order-dialog-channel]", slugLabel(row?.dataset.channel || card?.querySelector("footer span")?.textContent || "web store"));
		set("[data-order-dialog-items]", row?.dataset.items || Array.from(card?.querySelectorAll("li") || []).map((item) => item.textContent).join(", ") || "Sample products");
		set("[data-order-dialog-total]", row ? `$${Number(row.dataset.total || 0).toFixed(2)}` : card?.querySelector("footer strong")?.textContent || "$0.00");
		set("[data-order-dialog-stage]", `Status · ${statusLabels[row?.dataset.status || card?.dataset.status || "paid"] || "Paid"}`);
		const ids = detailOrderIDs();
		activeDetailIndex = Math.max(0, ids.indexOf(id));
	};

	const openDetails = (id) => {
		renderDetails(id);
		if (detailDialog instanceof HTMLDialogElement && !detailDialog.open) detailDialog.showModal();
	};

	document.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const detailButton = event.target.closest("[data-order-details]");
		if (detailButton instanceof HTMLButtonElement) openDetails(detailButton.dataset.orderDetails || "");
	});
	for (const button of document.querySelectorAll("[data-order-dialog-close]")) button.addEventListener("click", () => detailDialog?.close());
	document.querySelector("[data-order-dialog-previous]")?.addEventListener("click", () => {
		const ids = detailOrderIDs();
		if (ids.length) renderDetails(ids[(activeDetailIndex - 1 + ids.length) % ids.length]);
	});
	document.querySelector("[data-order-dialog-next]")?.addEventListener("click", () => {
		const ids = detailOrderIDs();
		if (ids.length) renderDetails(ids[(activeDetailIndex + 1) % ids.length]);
	});

	const newDialog = document.querySelector("[data-orders-new-dialog]");
	const newForm = document.querySelector("[data-orders-new-form]");
	const openNewDialog = () => {
		if (newDialog instanceof HTMLDialogElement && !newDialog.open) newDialog.showModal();
	};
	document.querySelector("[data-orders-new]")?.addEventListener("click", openNewDialog);
	for (const button of document.querySelectorAll("[data-orders-new-close]")) button.addEventListener("click", () => newDialog?.close());
	for (const button of document.querySelectorAll("[data-orders-edit-draft]")) {
		button.addEventListener("click", () => {
			openNewDialog();
			const customer = newForm?.querySelector('[name="customer"]');
			if (customer instanceof HTMLInputElement) customer.value = button.dataset.ordersEditDraft === "D-021" ? "Amelia Ross" : "";
		});
	}

	const makeCell = (tag, text) => {
		const cell = document.createElement(tag);
		cell.textContent = text;
		return cell;
	};

	const appendDemoOrder = (order) => {
		if (!(tableBody instanceof HTMLTableSectionElement)) return;
		const row = document.createElement("tr");
		row.dataset.orderRow = "";
		row.dataset.orderId = order.id;
		row.dataset.customer = order.customer;
		row.dataset.date = order.date;
		row.dataset.channel = order.channel;
		row.dataset.status = order.status;
		row.dataset.total = String(order.total);
		row.dataset.items = order.items;
		const selectCell = document.createElement("td");
		const checkbox = document.createElement("input");
		checkbox.type = "checkbox";
		checkbox.dataset.orderSelect = "";
		checkbox.setAttribute("aria-label", `Select order ${order.id}`);
		selectCell.append(checkbox);
		const idCell = makeCell("th", order.id);
		idCell.scope = "row";
		const statusCell = document.createElement("td");
		const status = document.createElement("span");
		status.dataset.orderStatusLabel = "";
		status.className = `admin-orders__status ${statusClass(order.status)}`.trim();
		status.textContent = statusLabels[order.status];
		statusCell.append(status);
		const actionCell = document.createElement("td");
		const details = document.createElement("button");
		details.type = "button";
		details.dataset.orderDetails = order.id;
		details.textContent = "Details";
		actionCell.append(details);
		row.append(
			selectCell,
			idCell,
			makeCell("td", order.customer),
			makeCell("td", "Today"),
			makeCell("td", slugLabel(order.channel)),
			makeCell("td", String(order.items.split(",").length)),
			makeCell("td", `$${order.total.toFixed(2)}`),
			statusCell,
			actionCell,
		);
		tableBody.prepend(row);
	};

	const readCustomOrders = () => {
		try {
			const value = JSON.parse(window.localStorage.getItem(ordersStorageKey) || "[]");
			if (!Array.isArray(value)) return [];
			return value.filter((order) => {
				return order &&
					typeof order.id === "string" && /^ZF-\d+$/.test(order.id) &&
					typeof order.customer === "string" && order.customer.trim() !== "" &&
					typeof order.items === "string" && order.items.trim() !== "" &&
					typeof order.channel === "string" &&
					typeof order.status === "string" && Object.hasOwn(statusLabels, order.status) &&
					typeof order.date === "string" && /^\d{4}-\d{2}-\d{2}$/.test(order.date) &&
					typeof order.total === "number" && Number.isFinite(order.total) && order.total > 0;
			});
		} catch {
			return [];
		}
	};

	const customOrders = readCustomOrders();
	for (const order of customOrders) appendDemoOrder(order);
	updateAllCount();

	newForm?.addEventListener("submit", (event) => {
		event.preventDefault();
		if (!(newForm instanceof HTMLFormElement)) return;
		const data = new FormData(newForm);
		const customer = String(data.get("customer") || "").trim();
		const items = String(data.get("items") || "").trim();
		const channel = String(data.get("channel") || "web-store");
		const total = Number(data.get("total"));
		if (!customer || !items || !Number.isFinite(total) || total <= 0) return;
		const highestCustomNumber = customOrders.reduce((highest, order) => {
			const number = Number(order.id.slice(3));
			return Number.isFinite(number) ? Math.max(highest, number) : highest;
		}, 1100);
		const nextNumber = highestCustomNumber + 1;
		const order = { id: `ZF-${nextNumber}`, customer, items, channel, total, status: "paid", date: new Date().toISOString().slice(0, 10) };
		customOrders.push(order);
		appendDemoOrder(order);
		try { window.localStorage.setItem(ordersStorageKey, JSON.stringify(customOrders)); } catch { /* Current page still updates. */ }
		newForm.reset();
		if (newDialog instanceof HTMLDialogElement) newDialog.close();
		applySort();
		applyFilters();
		updateAllCount();
		showToast(`${order.id} added to this browser preview.`, () => {
			const row = rows().find((entry) => entry.dataset.orderId === order.id);
			row?.remove();
			const index = customOrders.findIndex((entry) => entry.id === order.id);
			if (index !== -1) customOrders.splice(index, 1);
			try { window.localStorage.setItem(ordersStorageKey, JSON.stringify(customOrders)); } catch { /* No-op. */ }
			updateAllCount();
			applyFilters();
		});
	});

	const returnFilter = document.querySelector("[data-returns-filter]");
	const returnDialog = document.querySelector("[data-return-detail-dialog]");
	const setReturnDialogText = (selector, value) => {
		const target = returnDialog?.querySelector(selector);
		if (target instanceof HTMLElement) target.textContent = value;
	};
	for (const button of document.querySelectorAll("[data-return-dialog-close]")) {
		button.addEventListener("click", () => {
			if (returnDialog instanceof HTMLDialogElement) returnDialog.close();
		});
	}
	const filterReturns = () => {
		if (!(returnFilter instanceof HTMLSelectElement)) return;
		for (const card of document.querySelectorAll("[data-return-card]")) {
			card.hidden = returnFilter.value !== "all" && card.dataset.returnStatus !== returnFilter.value;
		}
	};
	returnFilter?.addEventListener("change", filterReturns);

	document.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const detailButton = event.target.closest("[data-return-details]");
		if (detailButton instanceof HTMLButtonElement) {
			const returnCard = detailButton.closest("[data-return-card]");
			if (returnCard instanceof HTMLElement) {
				setReturnDialogText("[data-return-dialog-id]", returnCard.dataset.returnId || "Preview return");
				setReturnDialogText("[data-return-dialog-customer]", returnCard.dataset.returnCustomer || "Preview customer");
				setReturnDialogText("[data-return-dialog-order]", `Order ${returnCard.dataset.returnOrder || "unknown"}`);
				setReturnDialogText("[data-return-dialog-product]", returnCard.dataset.returnProduct || "Preview item");
				setReturnDialogText("[data-return-dialog-reason]", returnCard.dataset.returnReason || "No reason supplied");
				setReturnDialogText("[data-return-dialog-status]", slugLabel(returnCard.dataset.returnStatus || "requested"));
				if (returnDialog instanceof HTMLDialogElement && !returnDialog.open) returnDialog.showModal();
			}
			return;
		}
		const button = event.target.closest("[data-return-action]");
		if (!(button instanceof HTMLButtonElement)) return;
		const card = button.closest("[data-return-card]");
		if (!(card instanceof HTMLElement)) return;
		const previous = card.dataset.returnStatus || "requested";
		const action = button.dataset.returnAction || "";
		const next = action === "approve" ? "approved" : action === "restock" ? "restocked" : action === "reject" ? "rejected" : previous;
		card.dataset.returnStatus = next;
		const label = card.querySelector("[data-return-label]");
		if (label instanceof HTMLElement) {
			label.textContent = slugLabel(next);
			label.className = `admin-orders__status ${next === "rejected" ? "admin-orders__status--red" : next === "approved" ? "admin-orders__status--sage" : ""}`.trim();
		}
		filterReturns();
		showToast(`Return marked ${slugLabel(next).toLocaleLowerCase()} in this browser preview.`, () => {
			card.dataset.returnStatus = previous;
			if (label instanceof HTMLElement) {
				label.textContent = slugLabel(previous);
				label.className = `admin-orders__status ${previous === "rejected" ? "admin-orders__status--red" : previous === "approved" ? "admin-orders__status--sage" : previous === "requested" ? "admin-orders__status--amber" : ""}`.trim();
			}
			filterReturns();
		});
	});

	if (query.get("new") === "1") openNewDialog();
	applySort();
	applyFilters();
})();
