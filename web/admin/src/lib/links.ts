// URL guards for values that come from the API and end up in an href or in
// window.location. Kept free of imports so scripts/check-links.mjs can load it.

/** The URL when it is an https: link, else null (blocks javascript:, data:, http:, ...). */
export function httpsHref(url: string | null | undefined): string | null {
	return typeof url === 'string' && url.startsWith('https://') ? url : null;
}

const SPOTIFY_AUTHORIZE_ORIGIN = 'https://accounts.spotify.com/';

/** True when the URL points at Spotify's accounts host, the only place the connect flow may go. */
export function isSpotifyAuthorizeUrl(url: unknown): url is string {
	return typeof url === 'string' && url.startsWith(SPOTIFY_AUTHORIZE_ORIGIN);
}
