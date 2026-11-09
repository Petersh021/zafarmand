(() => {
	"use strict";

	const tabNames = ["catalogue", "stock", "restocking", "collections"];
	const planStatuses = ["planned", "ordered", "received"];
	const tabStorageKey = "zafarmand-admin-products-tab-v1";
	const planStorageKey = "zafarmand-admin-products-plans-v1";
	const collectionStorageKey = "zafarmand-admin-product-collections-v1";
	const tabs = Array.from(document.querySelectorAll("[data-product-tab]"));
	const panels = Array.from(document.querySelectorAll("[data-product-panel]"));
	const announcer = document.querySelector("[data-product-announcer]");
	const toast = document.querySelector("[data-product-toast]");
	const toastMessage = document.querySelector("[data-product-toast-message]");
	const toastUndo = document.querySelector("[data-product-toast-undo]");
	let undoAction = null;
	let toastTimer = 0;

	const announce = (message) => {
		if (announcer instanceof HTMLElement) announcer.textContent = message;
	};

	const readStorage = (key) => {
		try {
			return window.localStorage.getItem(key);
		} catch (_error) {
			return null;
		}
	};

	const writeStorage = (key, value) => {
		try {
			window.localStorage.setItem(key, value);
		} catch (_error) {
			// The preview remains usable for this page load when storage is blocked.
		}
	};

	const removeStorage = (key) => {
		try {
			window.localStorage.removeItem(key);
		} catch (_error) {
			// Storage is optional for every preview interaction.
		}
	};

	const showToast = (message, action = null) => {
		if (!(toast instanceof HTMLElement) || !(toastMessage instanceof HTMLElement)) return;
		window.clearTimeout(toastTimer);
		undoAction = typeof action === "function" ? action : null;
		toastMessage.textContent = message;
		if (toastUndo instanceof HTMLButtonElement) toastUndo.hidden = undoAction === null;
		toast.hidden = false;
		toastTimer = window.setTimeout(() => {
			toast.hidden = true;
			undoAction = null;
		}, 7000);
	};

	toastUndo?.addEventListener("click", () => {
		const action = undoAction;
		undoAction = null;
		if (toast instanceof HTMLElement) toast.hidden = true;
		if (typeof action === "function") action();
	});

	const activateTab = (name, focus = false) => {
		if (!tabNames.includes(name)) return;
		for (const tab of tabs) {
			if (!(tab instanceof HTMLButtonElement)) continue;
			const active = tab.dataset.productTab === name;
			tab.setAttribute("aria-selected", String(active));
			tab.tabIndex = active ? 0 : -1;
			if (active && focus) tab.focus();
		}
		for (const panel of panels) {
			if (!(panel instanceof HTMLElement)) continue;
			panel.hidden = panel.dataset.productPanel !== name;
		}
		try {
			window.sessionStorage.setItem(tabStorageKey, name);
		} catch (_error) {
			// The active view can safely reset to Catalogue next visit.
		}
		announce(`${name[0].toUpperCase()}${name.slice(1)} Product view selected.`);
	};

	for (const [index, tab] of tabs.entries()) {
		if (!(tab instanceof HTMLButtonElement)) continue;
		tab.addEventListener("click", () => activateTab(tab.dataset.productTab || "catalogue"));
		tab.addEventListener("keydown", (event) => {
			let next = index;
			if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
			else if (event.key === "ArrowLeft") next = (index - 1 + tabs.length) % tabs.length;
			else if (event.key === "Home") next = 0;
			else if (event.key === "End") next = tabs.length - 1;
			else return;
			event.preventDefault();
			activateTab(tabs[next]?.dataset.productTab || "catalogue", true);
		});
	}

	let storedTab = "catalogue";
	try {
		const candidate = window.sessionStorage.getItem(tabStorageKey);
		if (candidate && tabNames.includes(candidate)) storedTab = candidate;
	} catch (_error) {
		storedTab = "catalogue";
	}

	const productList = document.querySelector("[data-product-list]");
	const productRecords = Array.from(document.querySelectorAll("li[data-product-record]"));
	const productSearch = document.querySelector("[data-product-search]");
	const productStatus = document.querySelector("select[data-product-status]");
	const productCategory = document.querySelector("select[data-product-category]");
	const productSort = document.querySelector("[data-product-sort]");
	const productCount = document.querySelector("[data-product-result-count]");
	const productEmpty = document.querySelector("[data-product-filter-empty]");

	if (productCategory instanceof HTMLSelectElement) {
		const categories = [...new Set(productRecords.map((record) => record.dataset.productCategory || "").filter(Boolean))]
			.sort((left, right) => left.localeCompare(right));
		for (const category of categories) {
			const option = document.createElement("option");
			option.value = category.toLocaleLowerCase();
			option.textContent = category;
			productCategory.append(option);
		}
	}

	const sortProducts = () => {
		if (!(productList instanceof HTMLOListElement) || !(productSort instanceof HTMLSelectElement)) return;
		const sorted = productRecords.slice().sort((left, right) => {
			if (productSort.value === "name") return (left.dataset.productName || "").localeCompare(right.dataset.productName || "");
			if (productSort.value === "category") return (left.dataset.productCategory || "").localeCompare(right.dataset.productCategory || "");
			if (productSort.value === "updated") return (right.dataset.productUpdated || "").localeCompare(left.dataset.productUpdated || "");
			return Number(left.dataset.productIndex) - Number(right.dataset.productIndex);
		});
		productList.append(...sorted);
	};

	const filterProducts = () => {
		const query = productSearch instanceof HTMLInputElement ? productSearch.value.trim().toLocaleLowerCase() : "";
		const status = productStatus instanceof HTMLSelectElement ? productStatus.value : "all";
		const category = productCategory instanceof HTMLSelectElement ? productCategory.value : "all";
		let visible = 0;
		for (const record of productRecords) {
			const haystack = `${record.dataset.productName || ""} ${record.dataset.productSlug || ""} ${record.dataset.productCategory || ""}`.toLocaleLowerCase();
			const matches = (!query || haystack.includes(query)) &&
				(status === "all" || record.dataset.productStatus === status) &&
				(category === "all" || (record.dataset.productCategory || "").toLocaleLowerCase() === category);
			record.hidden = !matches;
			if (matches) visible += 1;
		}
		if (productCount instanceof HTMLElement) productCount.textContent = String(visible);
		if (productEmpty instanceof HTMLElement) productEmpty.hidden = visible !== 0;
	};

	productSearch?.addEventListener("input", filterProducts);
	productStatus?.addEventListener("change", filterProducts);
	productCategory?.addEventListener("change", filterProducts);
	productSort?.addEventListener("change", () => {
		sortProducts();
		filterProducts();
	});
	document.querySelector("[data-product-filter-reset]")?.addEventListener("click", () => {
		if (productSearch instanceof HTMLInputElement) productSearch.value = "";
		if (productStatus instanceof HTMLSelectElement) productStatus.value = "all";
		if (productCategory instanceof HTMLSelectElement) productCategory.value = "all";
		if (productSort instanceof HTMLSelectElement) productSort.value = "stored";
		sortProducts();
		filterProducts();
		productSearch?.focus();
	});

	const csvCell = (value) => `"${String(value).replaceAll('"', '""')}"`;
	document.querySelector("[data-product-export]")?.addEventListener("click", () => {
		const visible = productRecords.filter((record) => !record.hidden);
		const lines = ["Name,Slug,Category,Status,Updated"];
		for (const record of visible) {
			lines.push([
				record.dataset.productName,
				record.dataset.productSlug,
				record.dataset.productCategory,
				record.dataset.productStatus,
				record.dataset.productUpdated,
			].map(csvCell).join(","));
		}
		const url = URL.createObjectURL(new Blob([lines.join("\r\n")], { type: "text/csv;charset=utf-8" }));
		const link = document.createElement("a");
		link.href = url;
		link.download = "zafarmand-products.csv";
		link.click();
		URL.revokeObjectURL(url);
		announce(`${visible.length} visible Product records exported.`);
	});

	const stockCards = Array.from(document.querySelectorAll("[data-stock-card]"));
	const stockBaseStates = new Map(stockCards.map((card) => [card.dataset.stockId, card.dataset.stockState]));
	const stockFilter = document.querySelector("[data-stock-filter]");
	const stockVisibleCount = document.querySelector("[data-stock-visible-count]");
	const planTemplate = document.querySelector("[data-restock-template]");
	const stockDialog = document.querySelector("[data-stock-dialog]");
	const stockDialogProduct = document.querySelector("[data-stock-dialog-product]");
	const stockDialogQuantity = document.querySelector("[data-stock-dialog-quantity]");
	let activeStockCard = null;

	const initialPlans = [
		{ id: "plan-stone-110", stockId: "travertine-table", product: "Travertine coffee table", detail: "12 units · North Stone", status: "planned" },
		{ id: "plan-light-109", stockId: "noir-lamp", product: "Noir pendant lamp", detail: "18 units · Atelier Light", status: "ordered" },
		{ id: "plan-bowl-108", stockId: "bronze-bowl", product: "Bronze bowl", detail: "10 units · received 08 Nov", status: "received" },
	];

	const validStockIDs = new Set(stockCards.map((card) => card.dataset.stockId));
	const validProducts = new Map(stockCards.map((card) => [card.dataset.stockId, card.querySelector("h3")?.textContent.trim() || "Product"]));

	const validPlan = (plan) => plan && typeof plan === "object" &&
		typeof plan.id === "string" && /^[a-z0-9-]{3,64}$/.test(plan.id) &&
		validStockIDs.has(plan.stockId) && validProducts.get(plan.stockId) === plan.product &&
		typeof plan.detail === "string" && plan.detail.length <= 100 && planStatuses.includes(plan.status);

	const loadPlans = () => {
		const raw = readStorage(planStorageKey);
		if (!raw) return initialPlans.map((plan) => ({ ...plan }));
		try {
			const parsed = JSON.parse(raw);
			if (!Array.isArray(parsed) || parsed.length > 24 || !parsed.every(validPlan)) throw new Error("invalid preview plans");
			return parsed.map((plan) => ({ ...plan }));
		} catch (_error) {
			removeStorage(planStorageKey);
			return initialPlans.map((plan) => ({ ...plan }));
		}
	};

	let plans = loadPlans();
	const savePlans = () => writeStorage(planStorageKey, JSON.stringify(plans));

	const applyStockFilter = () => {
		const filter = stockFilter instanceof HTMLSelectElement ? stockFilter.value : "all";
		let visible = 0;
		for (const card of stockCards) {
			const matches = filter === "all" || card.dataset.stockState === filter;
			card.hidden = !matches;
			if (matches) visible += 1;
		}
		if (stockVisibleCount instanceof HTMLElement) stockVisibleCount.textContent = String(visible);
	};

	const syncStockCards = () => {
		for (const card of stockCards) {
			const activePlan = plans.find((plan) => plan.stockId === card.dataset.stockId && plan.status !== "received");
			card.dataset.stockState = activePlan ? "planned" : stockBaseStates.get(card.dataset.stockId) || "healthy";
			const button = card.querySelector("[data-stock-restock]");
			if (button instanceof HTMLButtonElement) {
				button.disabled = Boolean(activePlan);
				button.textContent = activePlan ? (activePlan.status === "ordered" ? "On order" : "Restock planned") : "Plan restock";
			}
		}
		applyStockFilter();
	};

	const updatePlanCounts = () => {
		for (const status of planStatuses) {
			const count = plans.filter((plan) => plan.status === status).length;
			const target = document.querySelector(`[data-restock-count="${status}"]`);
			if (target instanceof HTMLElement) target.textContent = String(count);
		}
		for (const target of document.querySelectorAll("[data-restock-total], [data-restock-tab-count]")) {
			target.textContent = String(plans.length);
		}
	};

	const movePlan = (id, status, withToast = true) => {
		if (!planStatuses.includes(status)) return;
		const plan = plans.find((candidate) => candidate.id === id);
		if (!plan || plan.status === status) return;
		const previous = plan.status;
		plan.status = status;
		savePlans();
		renderPlans();
		if (withToast) {
			showToast(`${plan.product} moved to ${status}.`, () => {
				movePlan(id, previous, false);
				showToast(`${plan.product} move undone.`);
			});
		}
		announce(`${plan.product} is now ${status}.`);
	};

	function renderPlans() {
		for (const zone of document.querySelectorAll("[data-restock-dropzone]")) zone.replaceChildren();
		if (planTemplate instanceof HTMLTemplateElement) {
			for (const plan of plans) {
				const fragment = planTemplate.content.cloneNode(true);
				const card = fragment.querySelector("[data-restock-card]");
				const reference = fragment.querySelector("[data-restock-reference]");
				const name = fragment.querySelector("[data-restock-name]");
				const detail = fragment.querySelector("[data-restock-detail]");
				const move = fragment.querySelector("[data-restock-move]");
				if (card instanceof HTMLElement) card.dataset.restockId = plan.id;
				if (reference instanceof HTMLElement) reference.textContent = plan.id.startsWith("local-") ? "Local preview plan" : plan.id.replaceAll("-", " ").toUpperCase();
				if (name instanceof HTMLElement) name.textContent = plan.product;
				if (detail instanceof HTMLElement) detail.textContent = plan.detail;
				if (move instanceof HTMLSelectElement) {
					move.value = plan.status;
					move.addEventListener("change", () => movePlan(plan.id, move.value));
				}
				const zone = document.querySelector(`[data-restock-dropzone="${plan.status}"]`);
				zone?.append(fragment);
			}
		}
		updatePlanCounts();
		syncStockCards();
	}

	stockFilter?.addEventListener("change", applyStockFilter);
	for (const card of stockCards) {
		card.querySelector("[data-stock-restock]")?.addEventListener("click", () => {
			activeStockCard = card;
			const name = card.querySelector("h3")?.textContent.trim() || "this Product";
			if (stockDialogProduct instanceof HTMLElement) stockDialogProduct.textContent = `Create a local restock plan for ${name}.`;
			if (stockDialogQuantity instanceof HTMLInputElement) {
				stockDialogQuantity.value = String(Math.max(1, Number(card.dataset.stockReorder) - Number(card.dataset.stockQuantityValue)));
			}
			if (stockDialog instanceof HTMLDialogElement) stockDialog.showModal();
		});
	}

	stockDialog?.addEventListener("close", () => {
		if (!(stockDialog instanceof HTMLDialogElement) || stockDialog.returnValue !== "confirm" || !(activeStockCard instanceof HTMLElement)) {
			activeStockCard = null;
			return;
		}
		const quantity = stockDialogQuantity instanceof HTMLInputElement ? Number(stockDialogQuantity.value) : 0;
		if (!Number.isInteger(quantity) || quantity < 1 || quantity > 999) {
			activeStockCard = null;
			return;
		}
		const stockId = activeStockCard.dataset.stockId;
		const product = validProducts.get(stockId);
		if (!stockId || !product) {
			activeStockCard = null;
			return;
		}
		const previousPlans = plans.map((plan) => ({ ...plan }));
		plans.push({ id: `local-${Date.now()}`, stockId, product, detail: `${quantity} units · Local preview`, status: "planned" });
		savePlans();
		renderPlans();
		showToast(`${product} restock plan added locally.`, () => {
			plans = previousPlans;
			savePlans();
			renderPlans();
			showToast(`${product} plan removed.`);
		});
		announce(`${product} restock plan added to the browser preview.`);
		activeStockCard = null;
	});

	const resetPlans = () => {
		const previous = plans.map((plan) => ({ ...plan }));
		plans = initialPlans.map((plan) => ({ ...plan }));
		removeStorage(planStorageKey);
		renderPlans();
		showToast("Inventory preview reset.", () => {
			plans = previous;
			savePlans();
			renderPlans();
			showToast("Previous inventory preview restored.");
		});
	};
	document.querySelector("[data-stock-reset]")?.addEventListener("click", resetPlans);
	document.querySelector("[data-restock-reset]")?.addEventListener("click", resetPlans);

	const collectionCards = Array.from(document.querySelectorAll("[data-collection-card]"));
	const validCollectionIDs = new Set(collectionCards.map((card) => card.dataset.collectionId));
	const loadFeatured = () => {
		const raw = readStorage(collectionStorageKey);
		if (!raw) return new Set(["furniture"]);
		try {
			const parsed = JSON.parse(raw);
			if (!Array.isArray(parsed) || !parsed.every((id) => validCollectionIDs.has(id))) throw new Error("invalid collections");
			return new Set(parsed);
		} catch (_error) {
			removeStorage(collectionStorageKey);
			return new Set(["furniture"]);
		}
	};
	let featuredCollections = loadFeatured();
	const saveFeatured = () => writeStorage(collectionStorageKey, JSON.stringify([...featuredCollections]));
	const renderCollections = () => {
		for (const card of collectionCards) {
			const button = card.querySelector("[data-collection-feature]");
			if (!(button instanceof HTMLButtonElement)) continue;
			const featured = featuredCollections.has(card.dataset.collectionId);
			button.setAttribute("aria-pressed", String(featured));
			button.textContent = featured ? "Featured" : "Feature collection";
		}
	};
	for (const card of collectionCards) {
		card.querySelector("[data-collection-feature]")?.addEventListener("click", () => {
			const id = card.dataset.collectionId;
			if (!id || !validCollectionIDs.has(id)) return;
			const wasFeatured = featuredCollections.has(id);
			if (wasFeatured) featuredCollections.delete(id); else featuredCollections.add(id);
			saveFeatured();
			renderCollections();
			const name = card.querySelector("h3")?.textContent.trim() || "Collection";
			showToast(`${name} ${wasFeatured ? "removed from" : "added to"} featured preview.`, () => {
				if (wasFeatured) featuredCollections.add(id); else featuredCollections.delete(id);
				saveFeatured();
				renderCollections();
				showToast(`${name} feature change undone.`);
			});
		});
	}
	document.querySelector("[data-collections-reset]")?.addEventListener("click", () => {
		const previous = new Set(featuredCollections);
		featuredCollections = new Set(["furniture"]);
		removeStorage(collectionStorageKey);
		renderCollections();
		showToast("Collection preview reset.", () => {
			featuredCollections = previous;
			saveFeatured();
			renderCollections();
			showToast("Previous collection preview restored.");
		});
	});

	sortProducts();
	filterProducts();
	renderPlans();
	renderCollections();
	activateTab(storedTab);
})();
