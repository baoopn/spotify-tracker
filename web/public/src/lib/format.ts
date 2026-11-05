// Formatting helpers (shared conventions with the admin dashboard).

// Time rules (docs/admin-api.md "Time handling"): API timestamps are UTC
// instants; everything shown goes through Intl in the viewer's zone (or an
// explicit IANA zone, for tests). No hand-rolled offsets, no 24 h-day math.
// Date-only values (album release_date) are calendar strings, never Dates.

type Zone = string | undefined;
const cache = new Map<string, Intl.DateTimeFormat>();
function fmt(zone: Zone, key: string, opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
	const k = `${key}|${zone ?? ''}`;
	let f = cache.get(k);
	if (!f) {
		f = new Intl.DateTimeFormat(undefined, { ...opts, timeZone: zone });
		cache.set(k, f);
	}
	return f;
}
const dtfOpts: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'long' };
// e.g. "1:06:53 PM PST" / "1:06:53 PM GMT+7": the zone name comes from Intl.
const clockOpts: Intl.DateTimeFormatOptions = {
	hour: 'numeric',
	minute: '2-digit',
	second: '2-digit',
	timeZoneName: 'short'
};
const ymdOpts: Intl.DateTimeFormatOptions = { year: 'numeric', month: 'numeric', day: 'numeric' };
const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto', style: 'short' });

export function duration(msTotal: number | null | undefined): string {
	if (msTotal == null || Number.isNaN(msTotal) || msTotal < 0) return '–:––';
	const total = Math.floor(msTotal / 1000);
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = (total % 60).toString().padStart(2, '0');
	return h > 0 ? `${h}:${m.toString().padStart(2, '0')}:${s}` : `${m}:${s}`;
}

const DATE_ONLY = /^\d{4}(-\d{2}(-\d{2})?)?$/;

/** Parse an RFC 3339 instant. Date-only calendar strings are refused on purpose. */
export function toDate(v: string | Date | null | undefined): Date | null {
	if (!v) return null;
	if (typeof v === 'string' && DATE_ONLY.test(v.trim())) return null;
	const d = v instanceof Date ? v : new Date(v);
	return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * How to render times: `zone` is passed to Intl as `timeZone` (an IANA name,
 * a fixed offset such as "-06:00", or undefined for the browser default);
 * `label`, when set, replaces Intl's zone name (used when Intl's tz data is
 * stale and we format at a server-supplied fixed offset).
 */
export type ZoneFormat = { zone?: string; label?: string | null };

const dtNoZoneOpts: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'medium' };
const clockNoZoneOpts: Intl.DateTimeFormatOptions = {
	hour: 'numeric',
	minute: '2-digit',
	second: '2-digit'
};

export function dateTime(v: string | Date | null | undefined, z: ZoneFormat = {}): string {
	const d = toDate(v);
	if (!d) return '–';
	if (z.label) return `${fmt(z.zone, 'dt-nz', dtNoZoneOpts).format(d)} ${z.label}`;
	return fmt(z.zone, 'dt', dtfOpts).format(d);
}

export function clockTime(v: string | Date | null | undefined, z: ZoneFormat = {}): string {
	const d = toDate(v);
	if (!d) return '–';
	if (z.label) return `${fmt(z.zone, 'clock-nz', clockNoZoneOpts).format(d)} ${z.label}`;
	return fmt(z.zone, 'clock', clockOpts).format(d);
}

/** Ordinal of the local calendar day of an instant (via Intl parts, DST-safe). */
function dayNumber(ms: number, zone: Zone): { day: number; month: number } {
	const parts = fmt(zone, 'ymd', ymdOpts).formatToParts(ms);
	const get = (t: string) => Number(parts.find((p) => p.type === t)?.value);
	const y = get('year');
	const m = get('month');
	// Date.UTC on calendar fields only numbers the day; it applies no zone offset.
	return { day: Math.round(Date.UTC(y, m - 1, get('day')) / 86_400_000), month: y * 12 + m };
}

/**
 * "just now", "3 min. ago", "5 hr. ago" from the instant difference; days,
 * weeks, months and years ("yesterday", "2 days ago") from local calendar
 * dates, so a 23 h or 25 h DST day never mislabels "yesterday".
 */
export function relative(
	v: string | Date | null | undefined,
	now: number = Date.now(),
	zone?: Zone
): string {
	const d = toDate(v);
	if (!d) return '–';
	const diffSec = (d.getTime() - now) / 1000;
	const abs = Math.abs(diffSec);
	if (abs < 60) return 'just now';
	const a = dayNumber(d.getTime(), zone);
	const b = dayNumber(now, zone);
	const days = a.day - b.day;
	if (days === 0 || abs < 3600) {
		return abs < 3600
			? rtf.format(Math.round(diffSec / 60), 'minute')
			: rtf.format(Math.round(diffSec / 3600), 'hour');
	}
	const months = a.month - b.month;
	if (Math.abs(days) < 7) return rtf.format(days, 'day');
	if (Math.abs(months) < 1 || Math.abs(days) < 28) return rtf.format(Math.trunc(days / 7), 'week');
	if (Math.abs(months) < 12) return rtf.format(months, 'month');
	return rtf.format(Math.trunc(months / 12), 'year');
}

/** Year of a calendar release date ("2020", "2020-05", "2020-05-01"); no Date parsing. */
export function releaseYear(date: string | null | undefined): string | null {
	const m = date?.trim().match(/^(\d{4})(?:-\d{2}){0,2}$/);
	return m ? m[1] : null;
}

export function artistNames(artists: { name: string }[] | undefined): string {
	return artists?.map((a) => a.name).join(', ') || 'Unknown artist';
}

/** Spotify links only: never render an arbitrary URL from the API as a link. */
export function spotifyUrl(url: string | undefined): string | undefined {
	return url && /^https:\/\/open\.spotify\.com\//.test(url) ? url : undefined;
}

/**
 * Cover art only from Spotify's image CDNs, matching the server CSP img-src
 * (https://*.scdn.co, https://*.spotifycdn.com). Anything else gets the fallback mark.
 */
const COVER_HOST = /^([a-z0-9-]+\.)+(scdn\.co|spotifycdn\.com)$/i;

export function coverUrl(url: string | undefined): string | undefined {
	if (!url) return undefined;
	try {
		const u = new URL(url);
		return u.protocol === 'https:' && !u.port && COVER_HOST.test(u.hostname) ? u.href : undefined;
	} catch {
		return undefined;
	}
}
