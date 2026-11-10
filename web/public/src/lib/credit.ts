// The live "Designed & built by" credit served by the main baoopn site.
// Everything is validated before use; any failure means "keep the fallback".

export const CREDIT_ENDPOINT = 'https://baoopn.com/api/v1/credit';

export type CreditLogo = { url: string; width: number; height: number };

export type Credit = {
	text: string;
	url: string;
	alt: string;
	logo: { light: CreditLogo; dark: CreditLogo };
};

const MAX_TEXT = 80;

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

function parseLogo(v: unknown): CreditLogo | null {
	if (!isRecord(v) || !isLogoUrl(v.url) || !isSize(v.width) || !isSize(v.height)) return null;
	return { url: v.url, width: v.width, height: v.height };
}

/** Validates a `GET /api/v1/credit` body; returns null when anything is off. */
export function parseCredit(json: unknown): Credit | null {
	if (!isRecord(json) || !isRecord(json.data)) return null;
	const d = json.data;
	if (typeof d.text !== 'string') return null;
	const text = d.text.trim().slice(0, MAX_TEXT).trim();
	if (!text || !isHttps(d.url) || !isRecord(d.logo)) return null;
	const light = parseLogo(d.logo.light);
	const dark = parseLogo(d.logo.dark);
	if (!light || !dark) return null;
	// The image is the link's only content, so it always needs a name.
	const rawAlt = typeof d.alt === 'string' ? d.alt.trim().slice(0, MAX_TEXT).trim() : '';
	const alt = rawAlt || 'baoopn.com';
	return { text, url: d.url, alt, logo: { light, dark } };
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
