// Listen-along sign-in (mirrors web/admin/src/lib/auth.svelte.ts).
//
// Boot: GET /listen/config -> disabled, or load Clerk with the publishable key
// -> with a Clerk session, GET /listen/me asks the API whether this person is
// invited. Clerk (`@clerk/clerk-js` + `@clerk/ui`) is imported lazily, so the
// `/` bundle never includes it. /listen boots in full (init); other pages only
// peek(): Clerk loads there when a session cookie exists, or on Sign in.
// Redirect rules: see ../account/redirect.ts.

import type { Clerk } from '@clerk/clerk-js';
import { goto } from '$app/navigation';
import { ApiError, errorMessage } from '../api';
import { fetchListenConfig, fetchListenMe } from './api';
import type { ListenMe } from './types';
import { LISTEN_PATH, hasClerkSession, safeRedirect, signInTarget } from '../account/redirect';

export { LISTEN_PATH };

/** The signed-in person, for the account menu (Clerk's user is not reactive). */
export type AccountProfile = { name: string; email: string; imageUrl: string | null };

/** Hide account deletion in Clerk's profile: listeners keep their account. */
const PROFILE_APPEARANCE = { elements: { profileSection__danger: { display: 'none' } } };

/** The current page as a same-origin path (where sign-out stays). */
function herePath(): string {
	return safeRedirect(window.location.pathname + window.location.search, '/');
}

export type ListenAuthStatus =
	| 'idle'
	| 'loading'
	/** LISTEN_ENABLED is off. */
	| 'disabled'
	| 'signed-out'
	| 'checking'
	| 'allowed'
	/** Signed in, but not invited (or banned, or no verified email). */
	| 'forbidden'
	| 'error';

/** Result of an access re-check while listening. */
export type AccessCheck = 'ok' | 'forbidden' | 'disabled' | 'signed-out' | 'error';

/** Clerk appearance from the shared CSS tokens, so it follows palette and mode. */
export function clerkAppearance() {
	return {
		variables: {
			colorPrimary: 'var(--primary)',
			colorPrimaryForeground: 'var(--paper)',
			colorDanger: 'var(--danger)',
			colorSuccess: 'var(--ok)',
			colorWarning: 'var(--warn)',
			colorNeutral: 'var(--text)',
			colorForeground: 'var(--text)',
			colorMuted: 'var(--surface-2)',
			colorMutedForeground: 'var(--muted)',
			colorBackground: 'var(--paper-2)',
			colorInput: 'var(--surface)',
			colorInputForeground: 'var(--text)',
			colorRing: 'var(--ring)',
			colorBorder: 'var(--border-strong)',
			colorShadow: 'transparent',
			borderRadius: '8px',
			fontFamily: 'inherit',
			fontFamilyButtons: 'inherit'
		},
		elements: {
			cardBox: {
				boxShadow: 'var(--print-shadow)',
				border: '1px solid var(--ink)',
				borderRadius: 'var(--baoopn-radius)'
			}
		}
	};
}

/** Navigate within the site when same-origin, otherwise do a full page load. */
async function navigateTo(to: string, replaceState = false): Promise<void> {
	const url = new URL(to, window.location.href);
	if (url.origin !== window.location.origin) {
		window.location.href = url.href;
		return;
	}
	// eslint-disable-next-line svelte/no-navigation-without-resolve -- `to` is already an absolute site path
	await goto(url.pathname + url.search + url.hash, { replaceState });
}

class ListenAuth {
	status = $state<ListenAuthStatus>('idle');
	me = $state<ListenMe | null>(null);
	error = $state<string | null>(null);
	clerk = $state.raw<Clerk | null>(null);
	/** Listen along is on (null until /listen/config answered). */
	enabled = $state<boolean | null>(null);
	profile = $state<AccountProfile | null>(null);

	#configPromise: Promise<string | null> | null = null;
	/** Sign in was started from the account menu: move on to /listen once allowed. */
	#afterSignIn = false;
	#initPromise: Promise<void> | null = null;
	/** The last token Clerk gave us, for sends that cannot wait (page hide). */
	#lastToken: string | null = null;
	#clerkUserId: string | null = null;

	/** The signed-in email (also when the API said no). */
	get email(): string | null {
		return this.me?.email ?? this.clerk?.user?.primaryEmailAddress?.emailAddress ?? null;
	}

	init(): Promise<void> {
		this.#initPromise ??= this.#boot();
		return this.#initPromise;
	}

	/**
	 * Light boot for pages other than /listen: only the config, and Clerk only
	 * when a Clerk session cookie says someone is likely signed in.
	 */
	async peek(): Promise<void> {
		if (this.#initPromise) return this.#initPromise;
		try {
			if ((await this.#config()) === null) return;
		} catch {
			this.enabled = false; // no account control without the config
			return;
		}
		if (this.#initPromise) return this.#initPromise;
		if (hasClerkSession(document.cookie)) return this.init();
		if (this.status === 'idle') this.status = 'signed-out';
	}

	/** Sign in in a Clerk modal; invited listeners then move on to /listen. */
	async openSignIn(): Promise<void> {
		this.#afterSignIn = true;
		if (this.status === 'error' && !this.clerk) await this.retry();
		else await this.init();
		const clerk = this.clerk;
		if (!clerk) return;
		if (clerk.user) return this.refresh();
		const to = signInTarget(window.location.pathname, window.location.search);
		clerk.openSignIn({
			forceRedirectUrl: to,
			fallbackRedirectUrl: to,
			signUpForceRedirectUrl: to,
			signUpFallbackRedirectUrl: to
		});
	}

	/** Clerk's profile (name, photo, emails, devices), without account deletion. */
	openUserProfile(): void {
		this.clerk?.openUserProfile({ appearance: PROFILE_APPEARANCE });
	}

	/** The publishable key, or null when listen along is off. Fetched once. */
	#config(): Promise<string | null> {
		this.#configPromise ??= fetchListenConfig().then((cfg) => {
			this.enabled = cfg.enabled;
			if (!cfg.enabled) {
				this.status = 'disabled';
				return null;
			}
			if (!cfg.clerk_publishable_key) {
				throw new Error("Sign-in isn't set up on the server yet.");
			}
			return cfg.clerk_publishable_key;
		});
		// A failed request may be retried later.
		this.#configPromise.catch(() => (this.#configPromise = null));
		return this.#configPromise;
	}

	#syncProfile(): void {
		const u = this.clerk?.user;
		this.profile = u
			? {
					name: u.fullName ?? u.username ?? '',
					email: u.primaryEmailAddress?.emailAddress ?? '',
					imageUrl: u.hasImage ? u.imageUrl : null
				}
			: null;
	}

	/** Retry after a boot or check error. */
	retry(): Promise<void> {
		if (this.clerk) return this.refresh();
		this.#initPromise = null;
		this.error = null;
		return this.init();
	}

	async #boot(): Promise<void> {
		this.status = 'loading';
		try {
			const key = await this.#config();
			if (key === null) return;
			const [{ Clerk }, { ui }] = await Promise.all([
				import('@clerk/clerk-js'),
				import('@clerk/ui')
			]);
			const clerk = new Clerk(key);
			await clerk.load({
				ui,
				appearance: clerkAppearance(),
				signInUrl: LISTEN_PATH,
				signUpUrl: LISTEN_PATH,
				// Clerk never sends anyone to a hosted page or to /admin.
				signInForceRedirectUrl: signInTarget(window.location.pathname, window.location.search),
				signUpForceRedirectUrl: signInTarget(window.location.pathname, window.location.search),
				afterSignOutUrl: herePath(),
				routerPush: (to: string) => navigateTo(to),
				routerReplace: (to: string) => navigateTo(to, true)
			});
			this.clerk = clerk;
			this.#clerkUserId = clerk.user?.id ?? null;
			this.#syncProfile();
			clerk.addListener(({ user }) => {
				this.#syncProfile(); // also after profile edits (name, photo)
				const id = user?.id ?? null;
				if (id !== this.#clerkUserId) {
					this.#clerkUserId = id;
					void this.refresh();
				}
			});
			await this.refresh();
		} catch (err) {
			this.status = 'error';
			this.error = errorMessage(err);
		}
	}

	/** Ask the API whether the current Clerk user may listen along. */
	async refresh(): Promise<void> {
		if (!this.clerk?.user || !this.clerk.session) {
			this.me = null;
			this.status = 'signed-out';
			return;
		}
		if (this.status !== 'allowed') this.status = 'checking';
		const res = await this.#me();
		const moveOn = this.#afterSignIn;
		this.#afterSignIn = false;
		if (res.ok) {
			this.me = res.me;
			this.status = 'allowed';
			this.error = null;
			if (moveOn && window.location.pathname !== LISTEN_PATH) void navigateTo(LISTEN_PATH);
			return;
		}
		this.me = null;
		const err = res.err;
		if (err instanceof ApiError && err.status === 403) {
			this.status = 'forbidden';
			this.error = err.message;
		} else if (err instanceof ApiError && err.status === 401) {
			await this.#dropSession();
		} else if (err instanceof ApiError && err.status === 404) {
			this.status = 'disabled';
		} else {
			this.status = 'error';
			this.error = errorMessage(err);
		}
	}

	/** Re-check access while listening; does not change the gate on 403. */
	async check(): Promise<AccessCheck> {
		if (!this.clerk?.session) {
			this.status = 'signed-out';
			return 'signed-out';
		}
		const res = await this.#me();
		if (res.ok) return 'ok';
		if (res.err instanceof ApiError && res.err.status === 403) return 'forbidden';
		if (res.err instanceof ApiError && res.err.status === 404) return 'disabled';
		if (res.err instanceof ApiError && res.err.status === 401) {
			await this.#dropSession();
			return 'signed-out';
		}
		return 'error';
	}

	/** GET /listen/me; on 401, once more with a fresh token. */
	async #me(): Promise<{ ok: true; me: ListenMe } | { ok: false; err: unknown }> {
		try {
			return { ok: true, me: await fetchListenMe(await this.getToken()) };
		} catch (err) {
			if (!(err instanceof ApiError && err.status === 401)) return { ok: false, err };
		}
		try {
			return { ok: true, me: await fetchListenMe(await this.getToken(true)) };
		} catch (err) {
			return { ok: false, err };
		}
	}

	/** The API refused the session even with a fresh token: sign in again. */
	async #dropSession(): Promise<void> {
		this.me = null;
		this.#lastToken = null;
		try {
			await this.clerk?.signOut();
		} catch {
			// the sign-in form shows either way
		}
		this.status = 'signed-out';
	}

	/** A Clerk session token; `fresh` skips Clerk's token cache. */
	async getToken(fresh = false): Promise<string | null> {
		const session = this.clerk?.session;
		if (!session) return null;
		try {
			const token = await session.getToken(fresh ? { skipCache: true } : undefined);
			this.#lastToken = token;
			return token;
		} catch {
			return null;
		}
	}

	/**
	 * Run `fn` with a token. `now` (a keepalive send on page hide) uses the
	 * last token at once: awaiting Clerk could outlive the page.
	 */
	withToken<T>(now: boolean, fn: (token: string | null) => Promise<T>): Promise<T> {
		if (now) return fn(this.#lastToken);
		return this.getToken().then(fn);
	}

	async signOut(): Promise<void> {
		this.me = null;
		this.profile = null;
		this.#lastToken = null;
		// Sign-out stays on the current page (/ or /listen).
		if (this.clerk) await this.clerk.signOut({ redirectUrl: herePath() });
		this.status = 'signed-out';
	}
}

export const listenAuth = new ListenAuth();
