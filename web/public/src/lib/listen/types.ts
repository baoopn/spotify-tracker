// Listen-along API (/api/v1/listen/*) response shapes. Every body is `{ data: ... }`.

/** GET /api/v1/listen/config (no auth). */
export type ListenConfig = {
	enabled: boolean;
	clerk_publishable_key: string | null;
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
