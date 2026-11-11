import { api, errorMessage } from './api';
import { isSpotifyAuthorizeUrl } from './links';
import { daysUntil } from './time.js';
import { toasts } from './toast.svelte';
import type { SpotifyStatus } from './types';

/** Ask the API for a Spotify authorize URL and leave the SPA to go there. */
export async function startSpotifyAuthorize(): Promise<void> {
	try {
		const { url } = await api.spotifyAuthorize();
		// Only ever leave for Spotify's accounts host, whatever the API returned.
		if (!isSpotifyAuthorizeUrl(url)) {
			toasts.error('Could not start Spotify authorization', 'Unexpected authorize URL');
			return;
		}
		window.location.href = url;
	} catch (err) {
		toasts.error('Could not start Spotify authorization', errorMessage(err));
	}
}

export type TokenHealth = 'ok' | 'warn' | 'danger' | 'unknown';

/**
 * Whole days until the token expires, from the expiry instant (floored), so it is
 * correct across DST changes and in every zone. Falls back to the server's count.
 */
export function tokenDaysLeft(
	s: SpotifyStatus | null | undefined,
	now = Date.now()
): number | null {
	if (!s) return null;
	return daysUntil(s.expires_at, now) ?? s.days_left;
}

/** ≤ 3 days (or disconnected / errored) is critical, ≤ 14 days is a warning. */
export function tokenHealth(s: SpotifyStatus | null | undefined): TokenHealth {
	if (!s) return 'unknown';
	if (!s.connected) return 'danger';
	if (s.last_error) return 'danger';
	const days = tokenDaysLeft(s);
	if (days == null) return 'unknown';
	if (days <= 3) return 'danger';
	if (days <= 14) return 'warn';
	return 'ok';
}
