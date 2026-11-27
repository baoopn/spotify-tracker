// Listen-along audio files (/admin/api/listen/audio): the record of files in
// the bucket (synced from S3), the most-played songs without one, and the
// Tracks page audio filter.

import { keepPreviousData, queryOptions } from '@tanstack/svelte-query';
import { ADMIN_API, request } from './api';

/** The track behind a file (null when the file's track is not in the catalog yet). */
export type AudioTrack = {
	id: string;
	name: string;
	duration_ms: number;
	artists: { id: string; name: string }[];
	album_name: string;
	cover_image_url: string;
};

export type AudioFile = {
	/** Also the file name in the bucket: `{track_id}.{ext}`. */
	track_id: string;
	ext: string;
	size_bytes: number;
	content_type: string | null;
	last_modified: string | null;
	first_seen_at: string;
	track: AudioTrack | null;
	/** The owner's plays of the track. */
	owner_plays: number;
};

export type AudioSummary = {
	files: number;
	total_bytes: number;
	tracks_in_catalog: number;
	last_sync_at: string | null;
	last_error: string | null;
	/** 0 = the scheduled sync is off. */
	sync_interval_s: number;
};

export type AudioFilesList = { data: AudioFile[]; total: number; summary: AudioSummary };

export type MissingAudio = {
	track: AudioTrack;
	owner_plays: number;
	last_played_at: string | null;
};
export type MissingAudioList = { data: MissingAudio[]; total: number };

export type AudioSyncResult = { files: number; added: number; removed: number; took_ms: number };

/** GET /admin/api/tracks `audio=` filter. */
export type AudioFilter = 'has' | 'missing';
export const AUDIO_FILTERS: readonly AudioFilter[] = ['has', 'missing'];
export const AUDIO_FILTER_LABELS: Record<AudioFilter, string> = {
	has: 'Has audio',
	missing: 'No audio'
};

const BASE = `${ADMIN_API}/listen/audio`;
type Signal = { signal?: AbortSignal };
export type AudioListParams = { q: string; limit: number; offset: number };
export type MissingParams = { limit: number; offset: number };

export const listenAudioApi = {
	files: (p: AudioListParams, { signal }: Signal = {}) =>
		request<AudioFilesList>(BASE, { query: p, signal }),
	missing: (p: MissingParams, { signal }: Signal = {}) =>
		request<MissingAudioList>(`${BASE}/missing`, { query: p, signal }),
	sync: () => request<AudioSyncResult>(`${BASE}/sync`, { method: 'POST' })
};

export const audioKeys = {
	all: ['listen-audio'] as const,
	files: (p: AudioListParams) => ['listen-audio', 'files', p] as const,
	missing: (p: MissingParams) => ['listen-audio', 'missing', p] as const
};

export function audioFilesQuery(p: AudioListParams) {
	return queryOptions({
		queryKey: audioKeys.files(p),
		queryFn: ({ signal }) => listenAudioApi.files(p, { signal }),
		placeholderData: keepPreviousData,
		refetchInterval: 60_000
	});
}

export function missingAudioQuery(p: MissingParams) {
	return queryOptions({
		queryKey: audioKeys.missing(p),
		queryFn: ({ signal }) => listenAudioApi.missing(p, { signal }),
		placeholderData: keepPreviousData
	});
}

/** GET /admin/api/overview `listen_storage` (null when listen along or S3 is off). */
export type ListenStorage = {
	files: number;
	total_bytes: number;
	/** Distinct tracks in the owner's listening history. */
	tracks_played: number;
	/** Of those, the ones with an audio file. */
	tracks_with_audio: number;
	plays_30d: number;
	plays_30d_with_audio: number;
	last_sync_at: string | null;
	last_error: string | null;
	sync_interval_s: number;
	/** Files first seen in the last 7 days. */
	added_7d: number;
};

/** Whole percent of `part` in `total` (0 when there is no total). */
export function percent(part: number, total: number): number {
	return total > 0 ? Math.round((Math.min(part, total) / total) * 100) : 0;
}
