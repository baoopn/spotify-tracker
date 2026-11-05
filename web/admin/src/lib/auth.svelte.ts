// Clerk-based admin authentication.
//
// Boot sequence: GET /admin/api/config (public) -> load Clerk with the
// publishable key -> if a Clerk session exists, GET /admin/api/auth/me to ask
// the API whether this Clerk user is an allowed admin.
//
// Clerk (`@clerk/clerk-js` + the bundled `@clerk/ui`) is imported lazily so the
// main bundle stays small and nothing is fetched from a CDN at runtime.

import type { Clerk } from '@clerk/clerk-js';
import { goto } from '$app/navigation';
import { ApiError, api, configureAuth, errorMessage } from './api';
import type { AdminUser } from './types';

export type AuthStatus =
	| 'idle'
	| 'loading'
	| 'signed-out'
	| 'checking'
	| 'signed-in'
	/** Clerk session exists but the API rejected its token (401). */
	| 'unauthorized'
	/** Valid Clerk user that is not on the admin allow-list (403). */
	| 'forbidden'
	| 'error';

/** Navigate within the SPA when same-origin, otherwise do a full page load. */
export async function navigateTo(to: string, replaceState = false): Promise<void> {
	// eslint-disable-next-line svelte/prefer-svelte-reactivity -- one-off parse, not state
	const url = new URL(to, window.location.href);
	if (url.origin !== window.location.origin) {
		window.location.href = url.href;
		return;
	}
	// eslint-disable-next-line svelte/no-navigation-without-resolve -- `to` is already an absolute app path
	await goto(url.pathname + url.search + url.hash, { replaceState });
}

/** Clerk appearance built from our CSS tokens, so it follows palette + mode automatically. */
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

class AuthState {
	status = $state<AuthStatus>('idle');
	user = $state<AdminUser | null>(null);
	error = $state<string | null>(null);
	version = $state('');
	clerk = $state.raw<Clerk | null>(null);

	#initPromise: Promise<void> | null = null;
	#clerkUserId: string | null = null;

	constructor() {
		configureAuth(
			() => this.getToken(),
			(status) => {
				if (status === 403) {
					this.status = 'forbidden';
				} else {
					this.status = this.clerk?.session ? 'unauthorized' : 'signed-out';
				}
			}
		);
	}

	init(): Promise<void> {
		this.#initPromise ??= this.#boot();
		return this.#initPromise;
	}

	/** Retry after a boot error. */
	retry(): Promise<void> {
		this.#initPromise = null;
		this.error = null;
		return this.init();
	}

	async #boot(): Promise<void> {
		this.status = 'loading';
		try {
			const cfg = await api.config();
			this.version = cfg.version;
			if (!cfg.clerk_publishable_key) {
				throw new Error('The API did not return a Clerk publishable key.');
			}
			const [{ Clerk }, { ui }] = await Promise.all([
				import('@clerk/clerk-js'),
				import('@clerk/ui')
			]);
			const clerk = new Clerk(cfg.clerk_publishable_key);
			await clerk.load({
				ui,
				appearance: clerkAppearance(),
				signInUrl: '/admin/login',
				afterSignOutUrl: '/admin/login',
				routerPush: (to: string) => navigateTo(to),
				routerReplace: (to: string) => navigateTo(to, true)
			});
			this.clerk = clerk;
			this.#clerkUserId = clerk.user?.id ?? null;
			clerk.addListener(({ user }) => {
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

	/** Re-evaluate the API-side admin check for the current Clerk user. */
	async refresh(): Promise<void> {
		const clerk = this.clerk;
		if (!clerk?.user || !clerk.session) {
			this.user = null;
			this.status = 'signed-out';
			return;
		}
		if (this.status !== 'signed-in') this.status = 'checking';
		try {
			const { user } = await api.me({ silent: true });
			this.user = user;
			this.status = 'signed-in';
			this.error = null;
		} catch (err) {
			this.user = null;
			if (err instanceof ApiError && err.status === 403) {
				this.status = 'forbidden';
			} else if (err instanceof ApiError && err.status === 401) {
				this.status = 'unauthorized';
				this.error = err.message;
			} else {
				this.status = 'error';
				this.error = errorMessage(err);
			}
		}
	}

	async getToken(): Promise<string | null> {
		const session = this.clerk?.session;
		if (!session) return null;
		try {
			return await session.getToken();
		} catch {
			return null;
		}
	}

	async signOut(): Promise<void> {
		this.user = null;
		if (this.clerk) {
			await this.clerk.signOut({ redirectUrl: '/admin/login' });
		}
		this.status = 'signed-out';
	}
}

export const auth = new AuthState();

/** The current in-app location (including base), used for `?next=`. */
export function currentPath(): string {
	return window.location.pathname + window.location.search;
}

/** Only allow redirects back into the admin app. */
export function safeNext(next: string | null | undefined): string {
	if (!next || !next.startsWith('/admin') || next.startsWith('//') || next.includes('\\')) {
		return '/admin';
	}
	if (next.startsWith('/admin/login')) return '/admin';
	return next;
}
