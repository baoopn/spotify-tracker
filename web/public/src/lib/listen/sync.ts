// Listen-along sync rules (pure, no runes; tested by scripts/check-listen.mjs).
// The owner's Spotify position is the truth; the local <audio> follows it.

import { estimatePositionMs } from '../playback.ts';
import type { ListenAudioReason } from './types';

/** Re-seek when the audio is this far off the owner's position. */
export const DRIFT_MS = 2000;
/** Seek this far ahead, to cover the time the seek itself takes. */
export const SEEK_LEAD_MS = 300;
/** Never seek again sooner than this after a seek (let the audio settle). */
export const SEEK_COOLDOWN_MS = 3000;
/** How often the player compares positions. */
export const CHECK_MS = 1000;
/** A snapshot older than this means the owner is not listening. */
export const IDLE_MAX_AGE_MS = 60_000;
/** Fetch a new URL when the current one has less than this left. */
export const URL_REFRESH_MARGIN_MS = 60_000;
/** Ask again after an "unavailable" answer. */
export const UNAVAILABLE_RETRY_MS = 30_000;
/** Re-check access this often while joined. */
export const ACCESS_CHECK_MS = 60_000;
/** The server may still answer for the previous song right after a change. */
export const TRACK_MISMATCH_RETRY_MS = 1500;
export const TRACK_MISMATCH_RETRIES = 3;
/** Stay this far before the file's end when seeking. */
const END_GUARD_MS = 250;
export const DEFAULT_VOLUME = 0.8;

/** The fields of a now-playing snapshot the sync needs. */
export type SyncSnapshot = {
	is_playing: boolean;
	progress_ms: number | null;
	fetched_at: string;
	/** The last owner action (play, pause, seek, skip); null or absent when unknown. */
	changed_at?: string | null;
	track: { id: string } | null;
};

/**
 * The owner's position (ms) at `serverNowMs`, or null when there is nothing
 * to follow: no track, no progress, or a snapshot older than IDLE_MAX_AGE_MS.
 */
export function targetMs(
	np: SyncSnapshot | null | undefined,
	serverNowMs: number,
	durationMs: number
): number | null {
	if (!np?.track || np.progress_ms == null) return null;
	const fetched = Date.parse(np.fetched_at);
	if (!Number.isFinite(fetched) || serverNowMs - fetched > IDLE_MAX_AGE_MS) return null;
	return estimatePositionMs(np, serverNowMs, durationMs);
}

export type SyncInput = {
	joined: boolean;
	ownerPlaying: boolean;
	audioPaused: boolean;
	/** The audio has metadata and a source (readyState >= HAVE_METADATA). */
	audioReady: boolean;
	audioMs: number;
	targetMs: number | null;
	/** The file's duration in ms; 0 or NaN when unknown. */
	audioDurationMs: number;
	msSinceSeek: number;
};

export type SyncAction =
	| { kind: 'none' }
	| { kind: 'pause' }
	| { kind: 'ended' }
	| { kind: 'play'; toMs: number }
	| { kind: 'seek'; toMs: number };

const NONE: SyncAction = { kind: 'none' };
const PAUSE: SyncAction = { kind: 'pause' };

/** What the player should do now. Pause only when the audio is playing. */
export function syncAction(s: SyncInput): SyncAction {
	if (!s.joined || s.targetMs == null || !s.ownerPlaying) return s.audioPaused ? NONE : PAUSE;
	const dur = Number.isFinite(s.audioDurationMs) ? s.audioDurationMs : 0;
	if (dur > 0 && s.targetMs >= dur) return { kind: 'ended' };
	if (s.audioPaused) {
		return s.audioReady ? { kind: 'play', toMs: seekTarget(s.targetMs, dur) } : NONE;
	}
	if (Math.abs(s.audioMs - s.targetMs) > DRIFT_MS && s.msSinceSeek >= SEEK_COOLDOWN_MS) {
		return { kind: 'seek', toMs: seekTarget(s.targetMs, dur) };
	}
	return NONE;
}

/** Where to seek: a little ahead of the owner, inside the file. */
export function seekTarget(target: number, audioDurationMs: number): number {
	const ahead = target + SEEK_LEAD_MS;
	const max =
		Number.isFinite(audioDurationMs) && audioDurationMs > 0
			? Math.max(0, audioDurationMs - END_GUARD_MS)
			: Infinity;
	return Math.min(Math.max(0, ahead), max);
}

/** The URL expires soon (or has expired): fetch a new one before playing. */
export function shouldRefreshUrl(expiresAtMs: number | null, nowMs: number): boolean {
	if (expiresAtMs == null || !Number.isFinite(expiresAtMs)) return false;
	return expiresAtMs - nowMs < URL_REFRESH_MARGIN_MS;
}

/** The note shown for an audio answer without a URL. */
export function audioNote(reason: ListenAudioReason | null | undefined): string | null {
	switch (reason) {
		case 'not_found':
			return 'No audio for this song';
		case 'storage_unconfigured':
			return "Audio isn't set up yet";
		case 'unavailable':
			return 'Audio is unavailable right now';
		case 'idle':
			return "Bao isn't listening right now";
		default:
			return null;
	}
}

/** A stored volume: a number in 0..1, DEFAULT_VOLUME when missing or invalid. */
export function parseVolume(raw: string | null | undefined): number {
	if (raw == null || raw.trim() === '') return DEFAULT_VOLUME;
	const v = Number(raw);
	if (!Number.isFinite(v)) return DEFAULT_VOLUME;
	return Math.min(1, Math.max(0, v));
}

export type ListenStatus =
	| 'waiting'
	| 'loading'
	/** Not joined, and the audio's metadata is still loading (Join waits for it). */
	| 'preparing'
	| 'in-sync'
	| 'paused-by-owner'
	| 'no-audio'
	| 'cant-play'
	| 'ended'
	| 'access-ended'
	/** Listen along was switched off on the server. */
	| 'off';

/** HTMLMediaElement.readyState values. */
const HAVE_METADATA = 1;
const HAVE_FUTURE_DATA = 3;

export type StatusInput = {
	current: ListenStatus;
	hasTrack: boolean;
	targetMs: number | null;
	ownerPlaying: boolean;
	joined: boolean;
	hasSource: boolean;
	paused: boolean;
	readyState: number;
	driftMs: number;
};

/**
 * The status after a sync tick. Not joined (also right after Stop streaming)
 * it is never "loading" (getting in sync): "in-sync" means ready to join, and
 * "preparing" means the metadata is still on its way.
 */
export function nextStatus(s: StatusInput): ListenStatus {
	if (s.current === 'no-audio' || s.current === 'cant-play') return s.current;
	if (!s.hasTrack || s.targetMs == null) return 'waiting';
	if (s.current === 'ended' && s.paused) return 'ended';
	if (!s.ownerPlaying) return 'paused-by-owner';
	if (!s.joined) return s.hasSource && s.readyState >= HAVE_METADATA ? 'in-sync' : 'preparing';
	if (!s.hasSource) return 'loading';
	return !s.paused && s.readyState >= HAVE_FUTURE_DATA && s.driftMs < DRIFT_MS
		? 'in-sync'
		: 'loading';
}

/**
 * Where Join starts: always Bao's live position, never where the listener
 * stopped (stopping is not a pause). Null when there is nothing to follow.
 */
export function joinSeekMs(target: number | null, audioDurationMs: number): number | null {
	return target == null ? null : seekTarget(target, audioDurationMs);
}
