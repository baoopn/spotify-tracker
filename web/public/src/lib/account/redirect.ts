// Sign-in helpers for the public site (pure; tested by scripts/check-listen.mjs).
//
// Redirect rules:
// - Only same-origin relative paths are followed: they start with "/", not
//   "//" or "/\", and hold no control characters or backslashes. They are
//   normalized like a browser would ("/..//x" and "/%2e%2e//x" become "//x")
//   and refused when that result is protocol-relative. Paths under /admin
//   are refused too (the admin has its own sign-in). So a crafted
//   ?redirect_url= can never send someone to another site or to /admin.
// - On /listen, a safe ?redirect_url= or ?return_to= wins; otherwise
//   sign-in ends on /listen. Elsewhere (/), sign-in stays on the page and
//   the app moves an invited listener on to /listen itself.
// - Sign-out stays on the current page.

export const LISTEN_PATH = '/listen';

/** `raw` when it is a safe same-origin path, else `fallback`. */
export function safeRedirect<T extends string | null>(
	raw: string | null | undefined,
	fallback: T
): string | T {
	if (typeof raw !== 'string' || raw.length === 0 || raw.length > 2048) return fallback;
	if (!raw.startsWith('/') || raw.startsWith('//')) return fallback;
	// Browsers treat "\" like "/", so "/\evil.example" is protocol-relative too.
	// eslint-disable-next-line no-control-regex -- rejecting control characters is the point
	if (/[\\\u0000-\u001f\u007f]/.test(raw)) return fallback;
	// Normalize like the browser: dot segments ("/..//x", "/%2e%2e//x") can
	// otherwise collapse into a protocol-relative "//x".
	let url: URL;
	try {
		url = new URL(raw, PARSE_BASE);
	} catch {
		return fallback;
	}
	if (url.origin !== PARSE_ORIGIN) return fallback;
	const path = url.pathname;
	if (path.startsWith('//') || path.startsWith('/\\')) return fallback;
	// The admin has its own sign-in: listener redirects never go there.
	if (/^\/admin(\/|$)/i.test(path)) return fallback;
	return path + url.search + url.hash;
}

/** A host that can never be real: only used to normalize paths. */
const PARSE_BASE = 'https://redirect.invalid';
const PARSE_ORIGIN = new URL(PARSE_BASE).origin;

/** Where Clerk should land after sign-in or sign-up on this page. */
export function signInTarget(pathname: string, search: string): string {
	if (pathname === LISTEN_PATH) {
		const q = new URLSearchParams(search);
		return safeRedirect(q.get('redirect_url') ?? q.get('return_to'), LISTEN_PATH);
	}
	return safeRedirect(pathname + search, '/');
}

/**
 * A likely Clerk session, from its readable `__client_uat` cookie (also the
 * suffixed `__client_uat_<id>` form): set and not "0". Lets / skip loading
 * Clerk for visitors who were never signed in.
 */
export function hasClerkSession(cookie: string): boolean {
	for (const part of cookie.split(';')) {
		const eq = part.indexOf('=');
		if (eq < 0) continue;
		const name = part.slice(0, eq).trim();
		const value = part.slice(eq + 1).trim();
		if ((name === '__client_uat' || name.startsWith('__client_uat_')) && value && value !== '0') {
			return true;
		}
	}
	return false;
}

/** One or two letters for an avatar without an image. */
export function initials(
	name: string | null | undefined,
	email: string | null | undefined
): string {
	const words = (name ?? '').trim().split(/\s+/).filter(Boolean);
	if (words.length >= 2) return (words[0][0] + words[words.length - 1][0]).toUpperCase();
	if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase();
	const local = (email ?? '').split('@')[0];
	return local ? local.slice(0, 1).toUpperCase() : '?';
}
