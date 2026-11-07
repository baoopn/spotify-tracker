// Time and date formatting. Rules (docs/admin-api.md "Time handling"):
// - API timestamps are RFC 3339 UTC instants; they are only ever formatted with
//   Intl in the effective zone (zone.svelte.ts: override, else the browser's),
//   corrected to the server's offset when the browser's bundled tz data is stale.
// - Durations / "N days" / relative times come from instant differences only.
// - Date-only values (album release_date: "2020", "2020-05", "2020-05-01") are
//   calendar strings, never instants: they are never passed to new Date().
//
// Dependency-free on purpose so scripts/check-time.mjs can import it directly.

type FormatOpts = { locale?: string; timeZone?: string };

const DAY_MS = 86_400_000;
const cache = new Map<string, Intl.DateTimeFormat>();

function dtf(options: Intl.DateTimeFormatOptions, { locale, timeZone }: FormatOpts = {}) {
	const key = JSON.stringify([locale ?? '', timeZone ?? '', options]);
	let f = cache.get(key);
	if (!f) {
		f = new Intl.DateTimeFormat(locale, timeZone ? { ...options, timeZone } : options);
		cache.set(key, f);
	}
	return f;
}

// ---------- effective zone (see zone.svelte.ts) ----------

/**
 * How to format "local" times. `timeZone` is what Intl gets: the IANA zone, or a
 * fixed offset ("-06:00") when the browser's tz data disagrees with the server.
 * `label` replaces Intl's zone name in that case ("GMT-6").
 */
export type ZoneSpec = { iana: string | null; timeZone?: string; label?: string };

/** The server's view of a zone (GET /api/v1/timezone). */
export type ServerZoneInfo = { tz: string; offset_minutes: number; offset: string };

let zoneProvider: () => ZoneSpec = () => ({ iana: null });

/** Installed by zone.svelte.ts; reading it inside templates keeps formatters reactive. */
export function setZoneProvider(fn: () => ZoneSpec): void {
	zoneProvider = fn;
}

/** The browser's own IANA zone, or null when it can't tell (some embedded browsers). */
export function detectedTimeZone(): string | null {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || null;
	} catch {
		return null;
	}
}

/** The zone the app formats in (override → detected), or 'UTC' when unknown. */
export function browserTimeZone(): string {
	return zoneProvider().iana ?? detectedTimeZone() ?? 'UTC';
}

export function isValidTimeZone(tz: string): boolean {
	try {
		new Intl.DateTimeFormat('en-US', { timeZone: tz });
		return true;
	} catch {
		return false;
	}
}

/** "GMT-6", "GMT+5:30", "GMT" from minutes east of UTC. */
export function offsetLabel(minutes: number): string {
	if (minutes === 0) return 'GMT';
	const sign = minutes < 0 ? '-' : '+';
	const abs = Math.abs(minutes);
	const h = Math.floor(abs / 60);
	const m = abs % 60;
	return `GMT${sign}${h}${m ? `:${String(m).padStart(2, '0')}` : ''}`;
}

/** The browser's (ICU) offset for `tz` at `at`, read from Intl's own longOffset name. */
export function intlOffsetMinutes(tz: string, at: Date = new Date()): number | null {
	try {
		const name = new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'longOffset' })
			.formatToParts(at)
			.find((p) => p.type === 'timeZoneName')?.value;
		if (!name) return null;
		if (name === 'GMT' || name === 'UTC') return 0;
		const m = /^GMT([+-])(\d{1,2})(?::?(\d{2}))?$/.exec(name);
		if (!m) return null;
		const v = Number(m[2]) * 60 + Number(m[3] ?? 0);
		return m[1] === '-' ? -v : v;
	} catch {
		return null;
	}
}

/** A fixed-offset zone Intl accepts: "-06:00", else the equivalent Etc/GMT zone. */
function fixedOffsetZone(server: ServerZoneInfo): string | null {
	if (isValidTimeZone(server.offset)) return server.offset;
	if (server.offset_minutes % 60 === 0) {
		const h = -server.offset_minutes / 60; // Etc/GMT signs are inverted
		const etc = h === 0 ? 'Etc/GMT' : `Etc/GMT${h > 0 ? '+' : '-'}${Math.abs(h)}`;
		if (isValidTimeZone(etc)) return etc;
	}
	return null;
}

/**
 * Decide how to format for `iana`, given the server's current view of it.
 * Server unreachable or in agreement → plain Intl with the IANA zone.
 */
export function resolveZone(
	iana: string | null,
	server: ServerZoneInfo | null,
	at: Date = new Date()
): ZoneSpec & { corrected: boolean } {
	if (!iana) return { iana: null, corrected: false };
	if (!server || server.tz !== iana) return { iana, timeZone: iana, corrected: false };
	const browser = intlOffsetMinutes(iana, at);
	if (browser === server.offset_minutes) return { iana, timeZone: iana, corrected: false };
	const fixed = fixedOffsetZone(server);
	if (!fixed) return { iana, timeZone: iana, corrected: false };
	return { iana, timeZone: fixed, label: offsetLabel(server.offset_minutes), corrected: true };
}

/** Zone spec for a formatter call: explicit zone wins; the effective zone is corrected. */
function zoneFor(opts: FormatOpts = {}): { timeZone?: string; label?: string } {
	const z = zoneProvider();
	if (opts.timeZone && opts.timeZone !== z.iana) return { timeZone: opts.timeZone };
	return { timeZone: z.timeZone, label: z.label };
}

function format(d: Date, options: Intl.DateTimeFormatOptions, opts: FormatOpts = {}): string {
	const z = zoneFor(opts);
	const f = dtf(options, { locale: opts.locale, timeZone: z.timeZone });
	if (!z.label || !options.timeZoneName) return f.format(d);
	return f
		.formatToParts(d)
		.map((p) => (p.type === 'timeZoneName' ? z.label : p.value))
		.join('');
}

const DATE_ONLY = /^\d{4}(-\d{2}(-\d{2})?)?$/;

/** Parse an RFC 3339 instant. Date-only strings are rejected (they are not instants). */
export function parseInstant(v: string | Date | null | undefined): Date | null {
	if (!v) return null;
	if (v instanceof Date) return Number.isNaN(v.getTime()) ? null : v;
	if (DATE_ONLY.test(v)) return null;
	const d = new Date(v);
	return Number.isNaN(d.getTime()) ? null : d;
}

/** Precise local date + time with the zone, e.g. "Nov 1, 2026, 1:30 AM EST". */
export function dateTime(v: string | Date | null | undefined, opts?: FormatOpts): string {
	const d = parseInstant(v);
	if (!d) return '–';
	return format(
		d,
		{
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit',
			timeZoneName: 'short'
		},
		opts
	);
}

/** Local wall time with the zone, e.g. "10:42 AM GMT-6". */
export function timeOfDay(v: string | Date | null | undefined, opts?: FormatOpts): string {
	const d = parseInstant(v);
	return d ? format(d, { hour: 'numeric', minute: '2-digit', timeZoneName: 'short' }, opts) : '–';
}

/** Local calendar date of an instant, e.g. "Nov 1, 2026". */
export function date(v: string | Date | null | undefined, opts?: FormatOpts): string {
	const d = parseInstant(v);
	return d ? format(d, { year: 'numeric', month: 'short', day: 'numeric' }, opts) : '–';
}

/**
 * The calendar date ("YYYY-MM-DD") an instant falls on in an IANA zone, e.g. the
 * `max` of a date input that must not pass "today" in the server's configured zone.
 */
export function calendarDateIn(timeZone: string, at: Date = new Date()): string {
	const parts = dtf(
		{ year: 'numeric', month: '2-digit', day: '2-digit' },
		{ locale: 'en-CA', timeZone }
	).formatToParts(at);
	const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
	return `${get('year')}-${get('month')}-${get('day')}`;
}

/** An instant shown explicitly in UTC, e.g. "May 4, 2027, 9:12 AM UTC". */
export function utcDateTime(v: string | Date | null | undefined, locale?: string): string {
	return dateTime(v, { locale, timeZone: 'UTC' });
}

/** Whole days from `now` until the instant (floored; negative once past). */
export function daysUntil(v: string | Date | null | undefined, now = Date.now()): number | null {
	const d = parseInstant(v);
	return d ? Math.floor((d.getTime() - now) / DAY_MS) : null;
}

/** "in 12 days" / "today" / "3 days ago" for a day count. */
export function inDays(days: number, locale?: string): string {
	return rtfFor(locale).format(days, 'day');
}

const rtfCache = new Map<string, Intl.RelativeTimeFormat>();
function rtfFor(locale?: string): Intl.RelativeTimeFormat {
	let f = rtfCache.get(locale ?? '');
	if (!f) {
		f = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
		rtfCache.set(locale ?? '', f);
	}
	return f;
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
	['year', 31_536_000],
	['month', 2_592_000],
	['week', 604_800],
	['day', 86_400],
	['hour', 3600],
	['minute', 60],
	['second', 1]
];

/** "3 min ago" style text, from the instant difference only. */
export function relative(
	v: string | Date | null | undefined,
	now = Date.now(),
	locale?: string
): string {
	const d = parseInstant(v);
	if (!d) return '–';
	const diff = (d.getTime() - now) / 1000;
	const abs = Math.abs(diff);
	if (abs < 30) return 'just now';
	const rtf = rtfFor(locale);
	for (const [unit, sec] of UNITS) {
		if (abs >= sec || unit === 'second') return rtf.format(Math.round(diff / sec), unit);
	}
	return dateTime(d);
}

// ---------- date-only calendar values (album release_date) ----------

const RELEASE = /^(\d{4})(?:-(\d{2})(?:-(\d{2}))?)?$/;

/** "2020-05-01" -> "2020". Never parses the string as a Date. */
export function releaseYear(s: string | null | undefined): string | null {
	const m = s ? RELEASE.exec(s.trim()) : null;
	return m ? m[1] : null;
}

/**
 * "2020" -> "2020", "2020-05" -> "May 2020", "2020-05-01" -> "May 1, 2020".
 * The calendar fields are formatted in UTC, so no zone can shift the day or year.
 */
export function formatReleaseDate(s: string | null | undefined, locale?: string): string {
	const m = s ? RELEASE.exec(s.trim()) : null;
	if (!m) return s ?? '–';
	const [, y, mo, d] = m;
	if (!mo) return y;
	const utc = new Date(Date.UTC(Number(y), Number(mo) - 1, d ? Number(d) : 1));
	return d
		? dtf({ year: 'numeric', month: 'short', day: 'numeric' }, { locale, timeZone: 'UTC' }).format(
				utc
			)
		: dtf({ year: 'numeric', month: 'short' }, { locale, timeZone: 'UTC' }).format(utc);
}

// ---------- analytics buckets (instants returned by the server) ----------

/**
 * Axis label for a bucket instant. Daily buckets are calendar days in `timeZone`
 * (the zone the server bucketed in; corrected if it is the effective zone);
 * hourly buckets show the local wall time.
 */
export function bucketLabel(
	iso: string | Date,
	kind: 'hour' | 'day',
	opts: FormatOpts = {}
): string {
	const d = parseInstant(iso);
	if (!d) return '–';
	return kind === 'day'
		? format(d, { month: 'short', day: 'numeric' }, opts)
		: format(d, { hour: 'numeric', minute: '2-digit' }, { locale: opts.locale });
}

/** Tooltip / table label for a bucket instant, with the zone for hourly buckets. */
export function bucketTitle(
	iso: string | Date,
	kind: 'hour' | 'day',
	opts: FormatOpts = {}
): string {
	const d = parseInstant(iso);
	if (!d) return '–';
	return kind === 'day'
		? format(d, { weekday: 'short', month: 'short', day: 'numeric' }, opts)
		: format(
				d,
				{
					month: 'short',
					day: 'numeric',
					hour: 'numeric',
					minute: '2-digit',
					timeZoneName: 'short'
				},
				{ locale: opts.locale }
			);
}
