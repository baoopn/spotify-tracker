// Browser helpers for the listen-along player: stored settings and the OS
// media controls (lock screen, headset buttons).

import type { NowPlaying } from '../types';

export const VOLUME_KEY = 'st-public:listen-volume';
export const MUTED_KEY = 'st-public:listen-muted';

/** No seeking or skipping from the OS: Bao's position decides. */
const NOOP_ACTIONS: MediaSessionAction[] = [
	'seekto',
	'seekbackward',
	'seekforward',
	'previoustrack',
	'nexttrack'
];

export function readStored(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

export function store(key: string, value: string): void {
	try {
		localStorage.setItem(key, value);
	} catch {
		// private mode or blocked storage: the setting lasts for this visit only
	}
}

/** OS play joins, OS pause leaves; everything else does nothing. */
export function setupMediaSession(onPlay: () => void, onPause: () => void): void {
	if (!('mediaSession' in navigator)) return;
	const ms = navigator.mediaSession;
	const set = (action: MediaSessionAction, fn: MediaSessionActionHandler | null) => {
		try {
			ms.setActionHandler(action, fn);
		} catch {
			// action not supported by this browser
		}
	};
	// No-op handlers, not null: null lets the browser fall back to its own seek.
	const noop = () => {};
	for (const action of NOOP_ACTIONS) set(action, noop);
	set('play', onPlay);
	set('pause', onPause);
}

export function setMediaMetadata(track: NowPlaying['track'] | undefined): void {
	if (!('mediaSession' in navigator) || typeof MediaMetadata === 'undefined') return;
	navigator.mediaSession.metadata = track
		? new MediaMetadata({
				title: track.name,
				artist: track.artists.map((a) => a.name).join(', '),
				album: track.album?.name ?? '',
				artwork: track.album?.cover_image_url ? [{ src: track.album.cover_image_url }] : []
			})
		: null;
}
