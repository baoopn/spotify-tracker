// The viewer's effective time zone. Order: user override (localStorage) ->
// Intl's resolved zone -> unknown. When a zone is known, the server's tz data
// (newer than many browsers' bundled ICU) is compared with Intl's; if they
// disagree, every time is formatted at the server's fixed offset and labelled
// "GMT-6". See docs/admin-api.md "Stale browser time-zone data".

import { dateTime, clockTime, relative, type ZoneFormat } from './format';
import {
	browserOffset,
	browserShortName,
	resolveZone,
	type Resolution,
	type ServerZone
} from './tz';

const KEY = 'st-public:tz';
const HALF_HOUR = 30 * 60_000;

function readOverride(): string | null {
	try {
		return localStorage.getItem(KEY) || null;
	} catch {
		return null;
	}
}

function writeOverride(v: string | null): void {
	try {
		if (v) localStorage.setItem(KEY, v);
		else localStorage.removeItem(KEY);
	} catch {
		// storage unavailable: keep the in-memory choice
	}
}

function detectZone(): string | null {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || null;
	} catch {
		return null;
	}
}

class ZoneState {
	override = $state<string | null>(null);
	detected = $state<string | null>(null);
	server = $state<ServerZone | null>(null);
	/** Browser's own offset for the zone at the last check (minutes), or null. */
	browserMinutes = $state<number | null>(null);
	/** Browser's own short name for the zone at the last check ("MDT"), or null. */
	browserName = $state<string | null>(null);

	/** The zone we try to honour: override first, then Intl's. */
	readonly zone = $derived(this.override ?? this.detected);
	readonly resolution: Resolution = $derived(
		resolveZone(this.zone, this.server, this.browserMinutes, this.browserName)
	);
	/** What every formatter receives. */
	readonly format: ZoneFormat = $derived(
		this.resolution.kind === 'corrected'
			? { zone: this.resolution.zone, label: this.resolution.label }
			: this.resolution.kind === 'intl'
				? { zone: this.resolution.zone }
				: {}
	);
	readonly correctionNote = $derived(
		this.resolution.kind === 'corrected'
			? `Corrected: your browser's time-zone data is out of date for ${this.resolution.tz}`
			: null
	);

	#timer: ReturnType<typeof setTimeout> | null = null;
	#abort: AbortController | null = null;

	start(): () => void {
		this.override = readOverride();
		this.detected = detectZone();
		void this.check();
		this.#schedule();
		const onVisible = () => {
			if (document.visibilityState === 'visible') void this.check();
		};
		document.addEventListener('visibilitychange', onVisible);
		return () => {
			document.removeEventListener('visibilitychange', onVisible);
			if (this.#timer) clearTimeout(this.#timer);
			this.#abort?.abort();
		};
	}

	setOverride(tz: string | null): void {
		this.override = tz;
		writeOverride(tz);
		this.server = null;
		void this.check();
	}

	/** Ask the server for its view of the zone and compare with the browser's. */
	async check(): Promise<void> {
		const zone = this.zone;
		this.#abort?.abort();
		if (!zone) {
			this.server = null;
			this.browserMinutes = null;
			return;
		}
		const ctrl = new AbortController();
		this.#abort = ctrl;
		try {
			const res = await fetch(`/api/v1/timezone?tz=${encodeURIComponent(zone)}`, {
				headers: { accept: 'application/json' },
				signal: ctrl.signal
			});
			if (!res.ok) throw new Error(String(res.status));
			const body = (await res.json()) as { data?: ServerZone };
			if (ctrl.signal.aborted || zone !== this.zone) return;
			this.browserMinutes = browserOffset(zone);
			this.browserName = browserShortName(zone);
			this.server = body.data ?? null;
		} catch {
			// Endpoint unavailable: fall back silently to Intl's own data.
			if (!ctrl.signal.aborted) this.server = null;
		}
	}

	/**
	 * Re-check on the next half hour (+30 s) and every half hour after: hourly
	 * as asked, and close enough to any :00 or :30 transition that a fixed
	 * offset never outlives a DST change by more than a minute.
	 */
	#schedule(): void {
		const now = Date.now();
		const next = Math.ceil(now / HALF_HOUR) * HALF_HOUR + 30_000 - now;
		this.#timer = setTimeout(() => {
			void this.check();
			this.#schedule();
		}, next);
	}

	// Formatting entry points used by components.
	dateTime(v: string | Date | null | undefined): string {
		return dateTime(v, this.format);
	}
	clockTime(v: string | Date | null | undefined): string {
		return clockTime(v, this.format);
	}
	relative(v: string | Date | null | undefined, now: number): string {
		return relative(v, now, this.format.zone);
	}
}

export const zone = new ZoneState();
