// The effective time zone for every formatter in time.ts.
//
// 1. Zone: user override (localStorage "st-admin:tz") -> the browser's own zone
//    (Intl resolvedOptions) -> unknown.
// 2. Browsers bundle their own (sometimes stale) tz database, so the server is
//    the authority: GET /api/v1/timezone?tz=... is compared with Intl's offset.
//    On a mismatch we format with the server's fixed offset ("-06:00"), labelled
//    "GMT-6", and the top bar says so. Re-checked hourly and when the tab returns.
//    If the endpoint fails we fall back to plain Intl.

import { api } from './api.js';
import { detectedTimeZone, isValidTimeZone, resolveZone, setZoneProvider } from './time.js';
import type { ServerZone } from './types.js';

const OVERRIDE_KEY = 'st-admin:tz';
const RECHECK_MS = 60 * 60 * 1000;

function readOverride(): string | null {
	try {
		const v = localStorage.getItem(OVERRIDE_KEY);
		return v && isValidTimeZone(v) ? v : null;
	} catch {
		return null;
	}
}

class ZoneState {
	/** User-chosen IANA zone (Appearance → Time zone), or null for automatic. */
	override = $state<string | null>(null);
	/** What the browser reports, or null when it can't tell. */
	detected = $state<string | null>(null);
	/** The server's current view of the effective zone, when it answered. */
	server = $state.raw<ServerZone | null>(null);
	/** Bumped on each check so the comparison uses a fresh "now". */
	checkedAt = $state(Date.now());

	/** IANA zone in effect, or null when unknown (then times are UTC). */
	readonly iana: string | null = $derived(this.override ?? this.detected);

	readonly spec = $derived(resolveZone(this.iana, this.server, new Date(this.checkedAt)));

	/** True when the browser's tz data disagrees with the server for this zone. */
	readonly corrected: boolean = $derived(this.spec.corrected);

	#started = false;
	#ctrl: AbortController | null = null;

	constructor() {
		// Every formatter in time.ts reads the zone through this (reactively).
		setZoneProvider(() => this.spec);
	}

	start(): () => void {
		if (this.#started) return () => {};
		this.#started = true;
		this.detected = detectedTimeZone();
		this.override = readOverride();
		void this.check();

		const timer = setInterval(() => void this.check(), RECHECK_MS);
		const onVisible = () => {
			if (document.visibilityState === 'visible') void this.check();
		};
		document.addEventListener('visibilitychange', onVisible);
		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', onVisible);
			this.#ctrl?.abort();
			this.#started = false;
		};
	}

	/** Ask the server about the effective zone; on failure keep plain Intl. */
	async check(): Promise<void> {
		const tz = this.iana;
		this.#ctrl?.abort();
		this.checkedAt = Date.now();
		if (!tz) {
			this.server = null;
			return;
		}
		const ctrl = new AbortController();
		this.#ctrl = ctrl;
		try {
			const res = await api.timezone(tz, { signal: ctrl.signal });
			if (!ctrl.signal.aborted) this.server = res.data?.tz === tz ? res.data : null;
		} catch {
			if (!ctrl.signal.aborted) this.server = null;
		}
	}

	setOverride(tz: string | null): void {
		const v = tz && isValidTimeZone(tz) ? tz : null;
		this.override = v;
		try {
			if (v) localStorage.setItem(OVERRIDE_KEY, v);
			else localStorage.removeItem(OVERRIDE_KEY);
		} catch {
			// storage unavailable: keep the in-memory choice
		}
		this.server = null;
		void this.check();
	}
}

export const zone = new ZoneState();
