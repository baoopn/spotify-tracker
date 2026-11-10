// The live footer credit served by the main baoopn site: "<prefix> <mark> <suffix>"
// inside one link. The page asks for the mark (?logo=mark); the style saved in
// the baoopn admin decides between the square "color" badge and the wide "mono"
// glyph. Everything is validated before use; any failure means "keep the fallback".

export const CREDIT_ENDPOINT = 'https://baoopn.com/api/v1/credit?logo=mark';

/** Static text shown before the credit loads, and whenever it fails. */
export const FALLBACK = { prefix: 'by', suffix: 'baoopn' } as const;

/** Rendered logo height in px; the width follows the image's aspect ratio. */
export const LOGO_HEIGHT = 24;
const MAX_LOGO_WIDTH = 120;
const MAX_TEXT = 80;

export type CreditStyle = 'color' | 'mono';

/** One logo file with its rendered size (height LOGO_HEIGHT, width from the ratio). */
export type CreditLogo = { url: string; width: number; height: number };

export type Credit = {
	url: string;
	prefix: string; // '' when empty
	suffix: string; // '' when empty
	alt: string;
	style: CreditStyle;
	/** Only the square colour badge gets rounded corners; the mono glyph is transparent. */
	rounded: boolean;
	logo: { light: CreditLogo; dark: CreditLogo };
};

function isRecord(v: unknown): v is Record<string, unknown> {
	return typeof v === 'object' && v !== null && !Array.isArray(v);
}

const isHttps = (v: unknown): v is string => typeof v === 'string' && v.startsWith('https://');

const isSize = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v > 0;

// Logos may only come from the brand media host, the one the page CSP allows.
const LOGO_ORIGIN = 'https://media.baoopn.com';

function isLogoUrl(v: unknown): v is string {
	if (!isHttps(v)) return false;
	try {
		return new URL(v).origin === LOGO_ORIGIN;
	} catch {
		return false;
	}
}

/** Rendered width for an image of w x h at LOGO_HEIGHT, clamped to a sane range. */
export function logoWidth(w: number, h: number): number {
	return Math.min(Math.max(1, Math.round((LOGO_HEIGHT * w) / h)), MAX_LOGO_WIDTH);
}

function parseLogo(v: unknown): CreditLogo | null {
	if (!isRecord(v) || !isLogoUrl(v.url) || !isSize(v.width) || !isSize(v.height)) return null;
	return { url: v.url, width: logoWidth(v.width, v.height), height: LOGO_HEIGHT };
}

const text = (v: unknown): string =>
	typeof v === 'string' ? v.trim().slice(0, MAX_TEXT).trim() : '';

/** Validates a `GET /api/v1/credit` body; returns null when anything is off. */
export function parseCredit(json: unknown): Credit | null {
	if (!isRecord(json) || !isRecord(json.data)) return null;
	const d = json.data;
	// Only the mark fits this footer; an endpoint that ignores ?logo=mark
	// (a wordmark) keeps the fallback text.
	if (!isHttps(d.url) || !isRecord(d.logo) || d.logo.kind !== 'mark') return null;
	const light = parseLogo(d.logo.light);
	const dark = parseLogo(d.logo.dark);
	if (!light || !dark) return null;
	// Unknown style: treat as the transparent glyph, so nothing gets clipped.
	const style: CreditStyle = d.logo.style === 'color' ? 'color' : 'mono';
	return {
		url: d.url,
		prefix: text(d.prefix),
		suffix: text(d.suffix),
		alt: text(d.alt) || 'baoopn.com',
		style,
		rounded: style === 'color',
		logo: { light, dark }
	};
}

let memo: Promise<Credit | null> | null = null;

async function load(signal?: AbortSignal): Promise<Credit | null> {
	try {
		const res = await fetch(CREDIT_ENDPOINT, { signal });
		if (!res.ok) return null;
		return parseCredit(await res.json());
	} catch {
		return null;
	}
}

/** Fetches the credit at most once per page load; never throws. */
export function fetchCredit(signal?: AbortSignal): Promise<Credit | null> {
	if (!memo) {
		memo = load(signal);
		// An aborted attempt (e.g. the footer unmounted) may be retried by the next caller.
		memo.then((c) => {
			if (c === null && signal?.aborted) memo = null;
		});
	}
	return memo;
}
