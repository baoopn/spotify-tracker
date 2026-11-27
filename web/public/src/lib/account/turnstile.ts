// Cloudflare Turnstile for "Request an invite". The script loads once, on
// demand (only when the form opens and the server has a site key).
// turnstileTheme is pure and tested by scripts/check-listen.mjs.

const SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';

export type TurnstileTheme = 'light' | 'dark';

type RenderOptions = {
	sitekey: string;
	theme?: TurnstileTheme | 'auto';
	size?: 'normal' | 'flexible' | 'compact';
	action?: string;
	callback?: (token: string) => void;
	'expired-callback'?: () => void;
	'error-callback'?: () => void;
	'timeout-callback'?: () => void;
};

export type TurnstileApi = {
	render: (el: HTMLElement, opts: RenderOptions) => string | undefined;
	reset: (id?: string) => void;
	remove: (id?: string) => void;
};

declare global {
	interface Window {
		turnstile?: TurnstileApi;
	}
}

/** The widget theme for the site's resolved theme (`<html data-theme>`). */
export function turnstileTheme(dataTheme: string | null | undefined): TurnstileTheme {
	return dataTheme === 'dark' ? 'dark' : 'light';
}

let loading: Promise<TurnstileApi> | null = null;

/** Load Cloudflare's script once; resolves with window.turnstile. */
export function loadTurnstile(): Promise<TurnstileApi> {
	if (window.turnstile) return Promise.resolve(window.turnstile);
	loading ??= new Promise<TurnstileApi>((resolve, reject) => {
		const s = document.createElement('script');
		s.src = SCRIPT_URL;
		s.async = true;
		s.defer = true;
		s.onload = () => (window.turnstile ? resolve(window.turnstile) : reject(new Error('no api')));
		s.onerror = () => reject(new Error('The security check could not load.'));
		document.head.append(s);
	}).catch((err: unknown) => {
		loading = null; // a later open may retry
		throw err;
	});
	return loading;
}
