(() => {
	"use strict";

	const chart = document.querySelector("[data-dashboard-chart]");
	const rangeButtons = Array.from(document.querySelectorAll("[data-dashboard-range]"));
	if (!(chart instanceof HTMLElement) || rangeButtons.length === 0) {
		return;
	}

	const ranges = {
		"12m": {
			name: "last 12 months",
			label: "Last 12 months",
			today: "$244",
			todayCaption: "+12% vs last Sunday",
			range: "$312,480",
			rangeCaption: "+14.6% vs last year",
			orders: "6",
			ordersCaption: "+2 vs last Sunday",
			comparisonLabel: "Last year",
			labels: ["Nov", "Dec", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct"],
			values: [18400, 21300, 19600, 24800, 27100, 25400, 30200, 28700, 33100, 35600, 34200, 38900],
			comparison: [16200, 18500, 19100, 21900, 23100, 22700, 26100, 27400, 29800, 30500, 31800, 32600],
		},
		"6m": {
			name: "last 6 months",
			label: "Last 6 months",
			today: "$244",
			todayCaption: "+12% vs last Sunday",
			range: "$149,820",
			rangeCaption: "+10.2% vs previous",
			orders: "6",
			ordersCaption: "+2 vs last Sunday",
			comparisonLabel: "Previous",
			labels: ["May", "Jun", "Jul", "Aug", "Sep", "Oct"],
			values: [21400, 25100, 23800, 26700, 24400, 28420],
			comparison: [19800, 22100, 21600, 23900, 23200, 25400],
		},
		"30d": {
			name: "last 30 days",
			label: "Last 30 days",
			today: "$244",
			todayCaption: "+12% vs last Sunday",
			range: "$24,870",
			rangeCaption: "+6.7% vs previous",
			orders: "6",
			ordersCaption: "+2 vs last Sunday",
			comparisonLabel: "Previous",
			labels: ["Oct 3", "Oct 7", "Oct 11", "Oct 15", "Oct 19", "Oct 23", "Oct 27", "Nov 1"],
			values: [620, 910, 740, 1280, 1050, 1560, 1340, 1840],
			comparison: [540, 760, 690, 1100, 980, 1250, 1210, 1420],
		},
		"7d": {
			name: "last 7 days",
			label: "Last 7 days",
			today: "$244",
			todayCaption: "+12% vs last Sunday",
			range: "$6,409",
			rangeCaption: "+8.4% vs previous",
			orders: "6",
			ordersCaption: "+2 vs last Sunday",
			comparisonLabel: "Previous",
			labels: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"],
			values: [540, 780, 920, 710, 1180, 960, 1319],
			comparison: [490, 650, 760, 680, 940, 820, 1080],
		},
	};

	const metricToday = document.querySelector('[data-dashboard-metric="today"]');
	const metricRange = document.querySelector('[data-dashboard-metric="range"]');
	const metricOrders = document.querySelector('[data-dashboard-metric="orders"]');
	const captionToday = document.querySelector('[data-dashboard-caption="today"]');
	const captionRange = document.querySelector('[data-dashboard-caption="range"]');
	const captionOrders = document.querySelector('[data-dashboard-caption="orders"]');
	const rangeLabel = document.querySelector('[data-dashboard-label="range"]');
	const comparisonLabel = document.querySelector('[data-dashboard-label="comparison"]');
	const line = document.querySelector("[data-dashboard-chart-line]");
	const comparison = document.querySelector("[data-dashboard-chart-comparison]");
	const area = document.querySelector("[data-dashboard-chart-area]");
	const point = document.querySelector("[data-dashboard-chart-point]");
	const labels = document.querySelector("[data-dashboard-chart-labels]");
	const scale = document.querySelector("[data-dashboard-chart-scale]");
	const announcer = document.querySelector("[data-dashboard-announcer]");
	const periodStorageKey = "zafarmand-admin-dashboard-period";

	const linePath = (values, maximum) => {
		const width = 720;
		const top = 18;
		const bottom = 214;
		const usableHeight = bottom - top;
		return values.map((value, index) => {
			const x = values.length === 1 ? width / 2 : (index / (values.length - 1)) * width;
			const y = bottom - (value / maximum) * usableHeight;
			return `${index === 0 ? "M" : "L"}${x.toFixed(1)} ${y.toFixed(1)}`;
		}).join(" ");
	};

	const setText = (element, value) => {
		if (element instanceof HTMLElement) {
			element.textContent = value;
		}
	};

	const renderRange = (key, announce = true) => {
		const selected = ranges[key];
		if (!selected) {
			return;
		}

		for (const button of rangeButtons) {
			if (!(button instanceof HTMLButtonElement)) {
				continue;
			}
			const active = button.dataset.dashboardRange === key;
			button.setAttribute("aria-pressed", String(active));
		}

		setText(metricToday, selected.today);
		setText(metricRange, selected.range);
		setText(metricOrders, selected.orders);
		setText(captionToday, selected.todayCaption);
		setText(captionRange, selected.rangeCaption);
		setText(captionOrders, selected.ordersCaption);
		setText(rangeLabel, selected.label);
		setText(comparisonLabel, selected.comparisonLabel);

		const maximum = Math.max(...selected.values, ...selected.comparison) * 1.08;
		const currentPath = linePath(selected.values, maximum);
		const comparisonPath = linePath(selected.comparison, maximum);
		if (line instanceof SVGPathElement) {
			line.setAttribute("d", currentPath);
		}
		if (comparison instanceof SVGPathElement) {
			comparison.setAttribute("d", comparisonPath);
		}
		if (area instanceof SVGPathElement) {
			area.setAttribute("d", `${currentPath} L720 230 L0 230 Z`);
		}
		if (point instanceof SVGCircleElement) {
			const last = currentPath.split("L").at(-1)?.trim().split(" ");
			if (last && last.length === 2) {
				point.setAttribute("cx", last[0]);
				point.setAttribute("cy", last[1]);
			}
		}

		if (labels instanceof HTMLOListElement) {
			const labelItems = selected.labels.map((label) => {
				const item = document.createElement("li");
				item.textContent = label;
				return item;
			});
			labels.replaceChildren(...labelItems);
		}

		if (scale instanceof HTMLOListElement) {
			const compactMoney = new Intl.NumberFormat("en-US", {
				style: "currency",
				currency: "USD",
				notation: "compact",
				maximumFractionDigits: 1,
			});
			const scaleItems = [maximum, maximum / 2, 0].map((value) => {
				const item = document.createElement("li");
				item.textContent = compactMoney.format(value);
				return item;
			});
			scale.replaceChildren(...scaleItems);
		}

		chart.setAttribute(
			"aria-label",
			`Illustrative sales trend for the ${selected.name}; solid line is current preview data and dotted line is the comparison period.`,
		);
		if (announce && announcer instanceof HTMLElement) {
			announcer.textContent = `Showing preview sales for the ${selected.name}.`;
		}
		try {
			window.sessionStorage.setItem(periodStorageKey, key);
		} catch {
			// Period selection remains usable without storage.
		}
	};

	for (const button of rangeButtons) {
		button.addEventListener("click", () => {
			renderRange(button.dataset.dashboardRange || "7d");
		});
	}

	let initialRange = "7d";
	try {
		const stored = window.sessionStorage.getItem(periodStorageKey);
		if (stored && ranges[stored]) {
			initialRange = stored;
		}
	} catch {
		// The default remains available when storage is blocked.
	}
	renderRange(initialRange, false);

	const goalDialog = document.querySelector("[data-dashboard-goal-dialog]");
	const goalOpen = document.querySelector("[data-dashboard-goal-open]");
	const goalCancel = document.querySelector("[data-dashboard-goal-cancel]");
	const goalForm = document.querySelector("[data-dashboard-goal-form]");
	const goalInput = document.querySelector("[data-dashboard-goal-input]");
	const goalPercent = document.querySelector("[data-dashboard-goal-percent]");
	const goalLabel = document.querySelector("[data-dashboard-goal-label]");
	const goalPace = document.querySelector("[data-dashboard-goal-pace]");
	const goalRing = document.querySelector("[data-dashboard-goal-ring]");
	const goalStorageKey = "zafarmand-admin-dashboard-goal";
	const booked = 244;

	const formatMoney = (value) => new Intl.NumberFormat("en-US", {
		style: "currency",
		currency: "USD",
		maximumFractionDigits: 0,
	}).format(value);

	const renderGoal = (goal) => {
		const safeGoal = Number.isFinite(goal) && goal >= 1000 && goal <= 1000000 ? goal : 24000;
		const percent = Math.max(1, Math.min(100, Math.round((booked / safeGoal) * 100)));
		setText(goalPercent, `${percent}%`);
		setText(goalLabel, `of ${formatMoney(safeGoal)}`);
		setText(goalPace, formatMoney(Math.max(0, Math.ceil((safeGoal - booked) / 21))));
		if (goalRing instanceof SVGCircleElement) {
			goalRing.setAttribute("stroke-dasharray", `${percent} ${100 - percent}`);
		}
		const ringContainer = goalRing?.closest("[role='img']");
		if (ringContainer instanceof HTMLElement) {
			ringContainer.setAttribute("aria-label", `Illustrative monthly goal progress: ${percent} percent of ${formatMoney(safeGoal)}.`);
		}
		if (goalInput instanceof HTMLInputElement) {
			goalInput.value = String(safeGoal);
		}
	};

	let storedGoal = 24000;
	try {
		storedGoal = Number(window.localStorage.getItem(goalStorageKey)) || 24000;
	} catch {
		// Use the trusted default when storage is unavailable.
	}
	renderGoal(storedGoal);

	goalOpen?.addEventListener("click", () => {
		if (goalDialog instanceof HTMLDialogElement) {
			goalDialog.showModal();
		}
	});
	goalCancel?.addEventListener("click", () => {
		if (goalDialog instanceof HTMLDialogElement) {
			goalDialog.close();
		}
	});
	goalForm?.addEventListener("submit", () => {
		if (!(goalInput instanceof HTMLInputElement)) {
			return;
		}
		const nextGoal = Number(goalInput.value);
		if (!Number.isFinite(nextGoal) || nextGoal < 1000 || nextGoal > 1000000) {
			return;
		}
		try {
			window.localStorage.setItem(goalStorageKey, String(nextGoal));
		} catch {
			// The visible update still works without persistence.
		}
		renderGoal(nextGoal);
	});

	const toast = document.querySelector("[data-dashboard-toast]");
	const toastMessage = document.querySelector("[data-dashboard-toast-message]");
	const toastUndo = document.querySelector("[data-dashboard-toast-undo]");
	let toastTimer = 0;
	let undoAction = null;

	const hideToast = () => {
		if (toast instanceof HTMLElement) {
			toast.hidden = true;
		}
		undoAction = null;
	};

	const showToast = (message, undo = null) => {
		window.clearTimeout(toastTimer);
		setText(toastMessage, message);
		undoAction = undo;
		if (toastUndo instanceof HTMLButtonElement) {
			toastUndo.hidden = typeof undo !== "function";
		}
		if (toast instanceof HTMLElement) {
			toast.hidden = false;
		}
		toastTimer = window.setTimeout(hideToast, 6000);
	};

	toastUndo?.addEventListener("click", () => {
		if (typeof undoAction === "function") {
			undoAction();
		}
		hideToast();
	});

	for (const button of document.querySelectorAll("[data-dashboard-restock]")) {
		button.addEventListener("click", () => {
			if (!(button instanceof HTMLButtonElement)) {
				return;
			}
			const product = button.dataset.dashboardRestock || "Product";
			button.disabled = true;
			button.textContent = "Requested";
			showToast(`${product} restock added to the browser preview.`, () => {
				button.disabled = false;
				button.textContent = "Restock";
			});
		});
	}
})();
