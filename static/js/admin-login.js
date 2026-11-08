/*
 * The login controller progressively enhances the real password form with the
 * optical-code reference interaction. It never creates an authenticated
 * session: only the existing server POST can do that.
 */
(() => {
	"use strict";

	const root = document.querySelector("[data-admin-login]");

	if (!root) {
		return;
	}

	const card = root.querySelector(".admin-vault__card");
	const tabs = Array.from(root.querySelectorAll("[data-admin-method]"));
	const panels = Array.from(root.querySelectorAll("[data-admin-panel]"));
	const title = root.querySelector("[data-admin-login-title]");
	const eyebrow = root.querySelector("[data-admin-login-eyebrow]");
	const description = root.querySelector("[data-admin-login-description]");
	const liveStatus = root.querySelector("[data-admin-live-status]");
	const otpRequestForm = root.querySelector("[data-admin-otp-request]");
	const otpRequestSubmit = root.querySelector("[data-admin-otp-request-submit]");
	const otpRequestOpen = root.querySelector("[data-admin-open-request]");
	const otpRequestClose = root.querySelector("[data-admin-close-request]");
	const otpForm = root.querySelector("[data-admin-otp-form]");
	const otpFooter = root.querySelector("[data-admin-otp-footer]");
	const otpFooterCopy = root.querySelector("[data-admin-otp-footer-copy]");
	const otpInput = root.querySelector("[data-admin-otp-input]");
	const otpSlots = Array.from(root.querySelectorAll("[data-admin-otp-slot]"));
	const nodes = Array.from(root.querySelectorAll("[data-admin-node]"));
	const connectors = Array.from(root.querySelectorAll("[data-admin-connector]"));
	const telemetry = root.querySelector("[data-admin-telemetry]");
	const submitButton = root.querySelector("[data-admin-otp-submit]");
	const submitLabel = root.querySelector("[data-admin-otp-submit-label]");
	const submitIcon = root.querySelector("[data-admin-otp-submit-icon]");
	const resendButton = root.querySelector("[data-admin-resend]");
	const soundButton = root.querySelector("[data-admin-sound]");
	const themeButton = root.querySelector("[data-admin-theme]");
	const googleButton = root.querySelector(".admin-google-button");
	const googleForm = root.querySelector("[data-admin-google-form]");
	const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
	const cipherGlyphs = "0123456789#%&§$Ø";
	const methodCopy = {
		otp: {
			eyebrow: "Email verification",
			title: "Verify your email",
			description: "Enter the four-digit verification code sent to your email."
		},
		password: {
			eyebrow: "Administrator sign-in",
			title: "Administration",
			description: "Sign in with your email address and password."
		},
		google: {
			eyebrow: "Google sign-in",
			title: "Sign in with Google",
			description: "Use the Google account that matches your Zafarmand administrator email."
		}
	};

	let otpState = "idle";
	let previousCode = "";
	let verificationTimer = 0;
	let resetTimer = 0;
	let resendTimer = 0;
	let soundEnabled = false;
	let themeIndex = 0;
	let audioContext = null;
	const cipherTimers = new Map();

	/* The live region announces state without exposing decorative telemetry. */
	function announce(message) {
		liveStatus.textContent = "";
		window.setTimeout(() => {
			liveStatus.textContent = message;
		}, 20);
	}

	function activateMethod(method, focusControl = false) {
		const copy = methodCopy[method] || methodCopy.password;

		tabs.forEach((tab) => {
			const selected = tab.dataset.adminMethod === method;
			tab.setAttribute("aria-selected", String(selected));
			tab.tabIndex = selected ? 0 : -1;
		});

		panels.forEach((panel) => {
			panel.hidden = panel.dataset.adminPanel !== method;
		});

		eyebrow.textContent = copy.eyebrow;
		title.textContent = copy.title;
		description.textContent = copy.description;
		root.dataset.activeMethod = method;

		if (!focusControl) {
			return;
		}

		if (method === "otp") {
			otpInput.focus();
		} else if (method === "password") {
			root.querySelector("#admin-email")?.focus();
		} else {
			googleButton?.focus();
		}
	}

	/* Tabs remain operable with the standard arrow-key interaction model. */
	const enabledTabs = tabs;

	tabs.forEach((tab) => {
		tab.addEventListener("click", () => {
			activateMethod(tab.dataset.adminMethod, true);
		});

		tab.addEventListener("keydown", (event) => {
			const index = enabledTabs.indexOf(tab);
			let nextIndex = index;

			if (index < 0) {
				return;
			}

			if (event.key === "ArrowRight" || event.key === "ArrowDown") {
				nextIndex = (index + 1) % enabledTabs.length;
			} else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
				nextIndex = (index - 1 + enabledTabs.length) % enabledTabs.length;
			} else if (event.key === "Home") {
				nextIndex = 0;
			} else if (event.key === "End") {
				nextIndex = enabledTabs.length - 1;
			} else {
				return;
			}

			event.preventDefault();
			activateMethod(enabledTabs[nextIndex].dataset.adminMethod);
			enabledTabs[nextIndex].focus();
		});
	});

	function openOTPDelivery() {
		otpRequestForm.hidden = false;
		otpRequestForm.querySelector("input[name='email']")?.focus();
		announce(
			root.dataset.otpEnabled === "true"
				? "Enter the email address that should receive the verification code."
				: "Email verification is not currently available."
		);
	}

	otpRequestOpen?.addEventListener("click", openOTPDelivery);
	otpRequestClose?.addEventListener("click", () => {
		otpRequestForm.hidden = true;
		otpInput.focus();
	});

	function clearCipherTimer(element) {
		const timer = cipherTimers.get(element);

		if (timer) {
			window.clearInterval(timer);
			cipherTimers.delete(element);
		}
	}

	/* Each fresh digit briefly rolls through optical hash glyphs. */
	function cipherRoll(element, finalValue, duration = 100) {
		clearCipherTimer(element);

		if (reducedMotion.matches) {
			element.textContent = finalValue;
			return;
		}

		const startedAt = performance.now();
		const timer = window.setInterval(() => {
			if (performance.now() - startedAt >= duration) {
				window.clearInterval(timer);
				cipherTimers.delete(element);
				element.textContent = finalValue;
				return;
			}

			element.textContent = cipherGlyphs[
				Math.floor(Math.random() * cipherGlyphs.length)
			];
		}, 28);

		cipherTimers.set(element, timer);
	}

	function setButtonCopy(label, icon) {
		submitLabel.textContent = label;
		submitIcon.textContent = icon;
	}

	function setTelemetry(value) {
		telemetry.textContent = value;
	}

	function playTone(frequency, duration = 0.08, type = "sine", volume = 0.035) {
		if (!soundEnabled) {
			return;
		}

		const AudioContext = window.AudioContext || window.webkitAudioContext;

		if (!AudioContext) {
			return;
		}

		audioContext ||= new AudioContext();
		const oscillator = audioContext.createOscillator();
		const gain = audioContext.createGain();
		const now = audioContext.currentTime;

		oscillator.type = type;
		oscillator.frequency.setValueAtTime(frequency, now);
		gain.gain.setValueAtTime(0.0001, now);
		gain.gain.exponentialRampToValueAtTime(volume, now + 0.012);
		gain.gain.exponentialRampToValueAtTime(0.0001, now + duration);
		oscillator.connect(gain);
		gain.connect(audioContext.destination);
		oscillator.start(now);
		oscillator.stop(now + duration + 0.02);
	}

	function playResult(success) {
		if (success) {
			[440, 554.37, 659.25].forEach((frequency, index) => {
				window.setTimeout(() => playTone(frequency, 0.55, "sine", 0.025), index * 70);
			});
			return;
		}

		playTone(130, 0.2, "sawtooth", 0.028);
		window.setTimeout(() => playTone(95, 0.24, "sawtooth", 0.026), 90);
	}

	function shakeCard() {
		card.classList.remove("is-shaking");
		void card.offsetWidth;
		card.classList.add("is-shaking");
		window.setTimeout(() => card.classList.remove("is-shaking"), 650);
	}

	function updateIris(codeLength) {
		if (otpState === "idle") {
			setTelemetry(codeLength === 0 ? "Awaiting code" : `${codeLength} of 4 digits entered`);
		}
	}

	function renderCode(code, animateIndex = -1) {
		const cleanCode = code.replace(/\D/g, "").slice(0, 4);
		const activeIndex = cleanCode.length < 4 ? cleanCode.length : 3;

		otpInput.value = cleanCode;
		root.dataset.otpLength = String(cleanCode.length);

		otpSlots.forEach((slot, index) => {
			const digit = cleanCode[index] || "";
			const value = slot.firstElementChild;
			const filled = digit !== "";

			slot.classList.toggle("is-filled", filled);
			slot.classList.toggle(
				"is-active",
				otpState === "idle" && index === activeIndex
			);

			if (index === animateIndex && filled) {
				cipherRoll(value, digit);
			} else {
				clearCipherTimer(value);
				value.textContent = digit;
			}
		});

		nodes.forEach((node, index) => {
			const digit = cleanCode[index] || String(index + 1);
			const nodeValue = node.querySelector("[data-admin-node-value]");
			const filled = index < cleanCode.length;

			node.classList.toggle("is-filled", filled);
			connectors[index]?.classList.toggle("is-filled", filled);

			if (index === animateIndex && filled) {
				cipherRoll(nodeValue, digit, 120);
			} else {
				clearCipherTimer(nodeValue);
				nodeValue.textContent = digit;
			}
		});

		updateIris(cleanCode.length);

		if (otpState === "idle") {
			setButtonCopy(cleanCode.length === 4 ? "Verify code" : "Enter 4 digits", "→");
		}

		previousCode = cleanCode;
	}

	function clearStateClasses() {
		root.classList.remove("is-verifying", "is-success", "is-error");
	}

	function resetOtp(focusInput = false) {
		window.clearTimeout(verificationTimer);
		window.clearTimeout(resetTimer);
		otpState = "idle";
		clearStateClasses();
		otpInput.disabled = false;
		submitButton.disabled = false;
		previousCode = "";
		renderCode("");
		setButtonCopy("Enter 4 digits", "→");
		setTelemetry("Awaiting code");

		if (focusInput) {
			otpInput.focus();
		}
	}

	otpInput.addEventListener("input", () => {
		if (otpState !== "idle") {
			return;
		}

		const cleanCode = otpInput.value.replace(/\D/g, "").slice(0, 4);
		const animateIndex = cleanCode.length > previousCode.length
			? cleanCode.length - 1
			: -1;

		renderCode(cleanCode, animateIndex);

		if (animateIndex >= 0) {
			playTone(270 + (Number(cleanCode[animateIndex]) * 16));
		}
	});

	otpInput.addEventListener("focus", () => root.classList.add("otp-has-focus"));
	otpInput.addEventListener("blur", () => root.classList.remove("otp-has-focus"));

	/* The request response is deliberately account-neutral on every address. */
	otpRequestForm.addEventListener("submit", async (event) => {
		event.preventDefault();
		const endpoint = sameOriginURL(root.dataset.otpRequestEndpoint);

		if (!endpoint) {
			announce("Email verification is not available. Use another sign-in method.");
			return;
		}

		otpRequestSubmit.disabled = true;
		otpRequestSubmit.firstElementChild.textContent = "Sending...";

		try {
			const response = await fetch(endpoint.href, {
				method: "POST",
				credentials: "same-origin",
				headers: {
					"Accept": "application/json",
					"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"
				},
				body: new URLSearchParams(new FormData(otpRequestForm))
			});
			const payload = await response.json().catch(() => ({}));

			if (!response.ok || payload.ok !== true) {
				throw new Error("token request rejected");
			}

			root.dataset.otpRequested = "true";
			otpRequestForm.hidden = true;
			otpForm.hidden = false;
			otpFooter.hidden = false;
			description.textContent = "Enter the four-digit verification code sent to your email.";
			setTelemetry("Code sent");
			otpFooterCopy.textContent = "Didn’t receive the code?";
			otpRequestOpen.hidden = true;
			resendButton.hidden = false;
			startResendCountdown(Number(payload.resend_after) || 30);
			announce(payload.status_message || "If that address is authorized, a code has been sent.");
			otpInput.focus();
		} catch (_error) {
			otpRequestSubmit.disabled = false;
			otpRequestSubmit.firstElementChild.textContent = "Send verification code";
			announce("A verification code could not be requested. Try again later.");
		}
	});

	googleForm?.addEventListener("submit", (event) => {
		if (root.dataset.googleEnabled === "true") {
			return;
		}

		event.preventDefault();
		announce("Google sign-in is not currently available.");
	});

	function setVerifyingState() {
		otpState = "verifying";
		root.classList.add("is-verifying");
		otpInput.disabled = true;
		submitButton.disabled = true;
		setButtonCopy("Verifying...", "•••");
		setTelemetry("Checking code...");
		announce("Verifying code.");
		playTone(400, 0.5, "sine", 0.025);
	}

	function showVerificationError(hashMismatch) {
		root.classList.remove("is-verifying");
		root.classList.add("is-error");
		otpState = "error";

		if (hashMismatch) {
			setButtonCopy("Invalid code", "×");
			setTelemetry("Invalid or expired code");
			announce("The code is invalid or expired. Try again.");
		} else {
			setButtonCopy("Service unavailable", "!");
			setTelemetry("Secure channel unavailable");
			announce("The verification service is unavailable. Use email and password or try again later.");
		}

		shakeCard();
		playResult(false);
		resetTimer = window.setTimeout(
			() => resetOtp(true),
			reducedMotion.matches ? 450 : 1650
		);
	}

	function showVerificationSuccess(payload) {
		root.classList.remove("is-verifying");
		root.classList.add("is-success");
		otpState = "success";
		setButtonCopy("Verified", "✓");
		setTelemetry("Verification complete");
		eyebrow.textContent = "Email verified";
		title.textContent = "Sign-in complete";
		description.textContent = "Opening the administration panel.";
		announce("Verification succeeded. Opening the administration panel.");
		playResult(true);

		const redirect = sameOriginURL(payload.redirect);

		if (redirect) {
			window.setTimeout(
				() => window.location.assign(redirect.href),
				reducedMotion.matches ? 0 : 650
			);
		}
	}

	/* Authentication endpoints and redirects must stay on this deployment. */
	function sameOriginURL(value) {
		if (!value) {
			return null;
		}

		try {
			const url = new URL(value, window.location.origin);
			return url.origin === window.location.origin ? url : null;
		} catch (_error) {
			return null;
		}
	}

	/*
	 * OTP verification is server-authoritative. The endpoint must return JSON
	 * with ok:true before the success state can ever be displayed.
	 */
	otpForm.addEventListener("submit", async (event) => {
		event.preventDefault();

		if (otpState !== "idle") {
			return;
		}

		const code = otpInput.value;

		if (code.length !== 4) {
			shakeCard();
			announce(`Enter ${4 - code.length} more digit${code.length === 3 ? "" : "s"}.`);
			otpInput.focus();
			return;
		}

		const endpoint = sameOriginURL(root.dataset.otpVerifyEndpoint);

		if (!endpoint) {
			announce("Email verification is not available. Use another sign-in method.");
			shakeCard();
			return;
		}

		/* Capture the code before the loading state disables its native input. */
		const requestBody = new URLSearchParams(new FormData(otpForm));
		setVerifyingState();

		try {
			const request = fetch(endpoint.href, {
				method: "POST",
				credentials: "same-origin",
				headers: {
					"Accept": "application/json",
					"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"
				},
				body: requestBody
			});
			const minimumAnimation = new Promise((resolve) => {
				verificationTimer = window.setTimeout(
					resolve,
					reducedMotion.matches ? 180 : 1100
				);
			});
			const [response] = await Promise.all([request, minimumAnimation]);
			const payload = await response.json().catch(() => ({}));

			if (response.ok && payload.ok === true) {
				showVerificationSuccess(payload);
				return;
			}

			showVerificationError(
				response.status === 401 ||
				response.status === 422 ||
				payload.code === "hash_mismatch"
			);
		} catch (_error) {
			showVerificationError(false);
		}
	});

	function startResendCountdown(seconds) {
		window.clearInterval(resendTimer);
		let remaining = seconds;

		resendButton.disabled = true;
		resendButton.textContent = `Resend (${remaining}s)`;

		resendTimer = window.setInterval(() => {
			remaining -= 1;

			if (remaining <= 0) {
				window.clearInterval(resendTimer);
				resendButton.disabled = false;
				resendButton.textContent = "Resend now";
				return;
			}

			resendButton.textContent = `Resend (${remaining}s)`;
		}, 1000);
	}

	resendButton.addEventListener("click", async () => {
		const endpoint = sameOriginURL(root.dataset.otpResendEndpoint);

		if (!endpoint) {
			announce("Verification email is not available. Use another sign-in method.");
			return;
		}

		resendButton.disabled = true;
		resendButton.textContent = "Requesting...";

		try {
			const formData = new FormData(otpRequestForm);
			const response = await fetch(endpoint.href, {
				method: "POST",
				credentials: "same-origin",
				headers: {
					"Accept": "application/json",
					"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"
				},
				body: new URLSearchParams(formData)
			});
			const payload = await response.json().catch(() => ({}));

			if (!response.ok || payload.ok !== true) {
				throw new Error("token request rejected");
			}

			startResendCountdown(Number(payload.resend_after) || 24);
			setTelemetry("Request accepted");
			announce(payload.status_message || "If that address is authorized, a code has been sent.");
		} catch (_error) {
			resendButton.disabled = false;
			resendButton.textContent = "Try again";
			announce("We couldn’t send a new code. Try again later.");
		}
	});

	soundButton.addEventListener("click", () => {
		soundEnabled = !soundEnabled;
		soundButton.setAttribute("aria-pressed", String(soundEnabled));
		soundButton.setAttribute(
			"aria-label",
			soundEnabled ? "Disable interface sounds" : "Enable interface sounds"
		);

		if (soundEnabled) {
			playTone(520, 0.09, "sine", 0.025);
		}
	});

	themeButton?.addEventListener("click", () => {
		const themes = ["", "stone", "clay"];
		themeIndex = (themeIndex + 1) % themes.length;
		const theme = themes[themeIndex];

		if (theme) {
			root.dataset.vaultTheme = theme;
		} else {
			delete root.dataset.vaultTheme;
		}

		playTone(310 + (themeIndex * 90), 0.09, "sine", 0.025);
	});

	/*
	 * Pointer tilt is visual-only and is removed for reduced-motion users. A
	 * finite data state keeps the effect compatible with the strict style CSP;
	 * no script-authored inline style is needed.
	 */
	if (!reducedMotion.matches && window.matchMedia("(hover: hover)").matches) {
		card.addEventListener("pointermove", (event) => {
			const bounds = card.getBoundingClientRect();
			const x = (event.clientX - bounds.left) / bounds.width;
			const y = (event.clientY - bounds.top) / bounds.height;
			const horizontal = x < 0.34 ? "w" : x > 0.66 ? "e" : "c";
			const vertical = y < 0.34 ? "n" : y > 0.66 ? "s" : "c";

			if (horizontal === "c" && vertical === "c") {
				delete card.dataset.vaultTilt;
				return;
			}
			card.dataset.vaultTilt = `${vertical}${horizontal}`;
		});

		card.addEventListener("pointerleave", () => {
			delete card.dataset.vaultTilt;
		});
	}

	/* Enhancement is enabled only after all required controls are bound. */
	root.classList.add("admin-login--enhanced");
	activateMethod(root.dataset.defaultMethod || "otp");
	renderCode("");

	if (root.dataset.otpRequested === "true") {
		startResendCountdown(24);
	}
})();
