// The live footer credit served by the main baoopn site. The footer reads
// "by [monogram] baoopn", so it asks for the monogram (?logo=mark) and uses
// only the link and the logo files, not the endpoint's text.
// Everything is validated before use; any failure means "keep the fallback".

export const CREDIT_ENDPOINT = 'https://baoopn.com/api/v1/credit?logo=mark';

export type CreditLogo = { url: string; width: number; height: number };

export type Credit = {
	url: string;
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

function parseLogo(v: unknown): CreditLogo | null {
	if (!isRecord(v) || !isLogoUrl(v.url) || !isSize(v.width) || !isSize(v.height)) return null;
	return { url: v.url, width: v.width, height: v.height };
}

/** Validates a `GET /api/v1/credit` body; returns null when anything is off. */
export function parseCredit(json: unknown): Credit | null {
	if (!isRecord(json) || !isRecord(json.data)) return null;
	const d = json.data;
	// Only the monogram fits this footer; an endpoint that ignores ?logo=mark
	// (a wordmark) keeps the plain "by baoopn" text.
	if (!isHttps(d.url) || !isRecord(d.logo) || d.logo.kind !== 'mark') return null;
	const light = parseLogo(d.logo.light);
	const dark = parseLogo(d.logo.dark);
	if (!light || !dark) return null;
	return { url: d.url, logo: { light, dark } };
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
