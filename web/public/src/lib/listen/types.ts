// Listen-along API (/api/v1/listen/*) response shapes. Every body is `{ data: ... }`.

/** GET /api/v1/listen/config (no auth). */
export type ListenConfig = {
	enabled: boolean;
	clerk_publishable_key: string | null;
	/** Cloudflare Turnstile on Request an invite; null (or absent on an older API) when off. */
	turnstile_site_key?: string | null;
};

/** GET /api/v1/listen/me (Clerk session token). */
export type ListenMe = {
	email: string;
	name: string;
	image_url: string;
	/** An admin passes without a listeners row. */
	admin: boolean;
};

/**
 * Why there is no audio URL:
 * idle                  nothing is playing (or the snapshot is stale / past the end)
 * not_found             no file for this song
 * storage_unconfigured  listen along is on, but S3 is not set up
 * unavailable           storage did not answer; try again in 30 s
 */
export type ListenAudioReason = 'idle' | 'not_found' | 'storage_unconfigured' | 'unavailable';

/** GET /api/v1/listen/audio (Clerk session token). `reason` is null exactly when `url` is set. */
export type ListenAudio = {
	track_id: string | null;
	url: string | null;
	/** RFC 3339. */
	expires_at: string | null;
	content_type: string | null;
	reason: ListenAudioReason | null;
};

/** One counted play in a listener's history (GET /api/v1/listen/history). */
export type ListenPlay = {
	id: number;
	/** When the play started (RFC 3339). */
	listened_at: string;
	last_heard_at: string;
	heard_ms: number;
	track: {
		id: string;
		name: string;
		duration_ms: number;
		explicit: boolean;
		external_url?: string;
		artists: { id: string; name: string }[];
		album_name: string;
		cover_image_url: string;
	};
};

/** A page of history, newest first; `next_cursor` is null on the last page. */
export type ListenHistoryPage = { data: ListenPlay[]; next_cursor: string | null };

/** POST /api/v1/listen/progress: audio time actually played since the last report. */
export type ListenProgress = { track_id: string; heard_ms: number };

/**
 * POST /api/v1/listen/presence.
 * listening  joined and hearing the audio in sync
 * following  on the page and allowed, but not hearing audio
 * idle       tab hidden, or the session ended
 */
export type PresenceState = 'listening' | 'following' | 'idle';
export type ListenPresence = { state: PresenceState; track_id: string | null };

/** POST /api/v1/listen/requests (no sign-in). `website` is a honeypot: always empty. */
export type ListenRequestInput = {
	name: string;
	email: string;
	message?: string;
	website?: string;
	/** The Turnstile token, when the config has a site key. */
	turnstile_token?: string;
};
