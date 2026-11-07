// TanStack Query: one client for the app, query keys and option factories.
// Everything is "always fresh": data is stale at once and refetched on mount,
// focus and reconnect, while the last good data stays on screen meanwhile.
// Now playing is pushed over SSE into this cache (see live.svelte.ts).

import { browser } from '$app/env';
import { QueryClient, keepPreviousData, queryOptions, skipToken } from '@tanstack/svelte-query';
import {
	ApiError,
	fetchAudioProfile,
	fetchHistory,
	fetchNowPlaying,
	fetchRecentPlaylists,
	fetchTopArtists,
	fetchTopTracks,
	fetchTopPlaylists,
	fetchTrackDetail
} from './api';
import type { NowPlaying, StatsRange, TopArtist, TopTrack, TrackDetail } from './types';

export const HISTORY_LIMIT = 6;
export const PLAYLIST_LIMIT = 5;
/** Older entries per period (besides the newest one). */
export const TOP_PLAYLIST_LIMIT = 6;
export const PROFILE_RANGE = '30d';
/** Entries in each "My top 5" list. */
export const TOP_LIMIT = 5;

/** Now-playing poll while the stream is down (SSE is the live source otherwise). */
export const NOW_POLL_MS = 10_000;
/** Safety nets beside the SSE-driven invalidation on track changes. */
const HISTORY_REFRESH_MS = 30_000;
const PLAYLISTS_REFRESH_MS = 30_000;
/** New top playlists appear at most once per period end. */
const TOP_PLAYLISTS_REFRESH_MS = 5 * 60_000;
const PROFILE_REFRESH_MS = 60_000;
const TRACK_REFRESH_MS = 60_000;
const TOP_REFRESH_MS = 60_000;
/** Fast poll while the enricher is still analysing the track on show. */
const TRACK_PENDING_MS = 4_000;

export const queryKeys = {
	nowPlaying: ['now-playing'] as const,
	history: (limit: number) => ['history', limit] as const,
	historyAll: ['history'] as const,
	recentPlaylists: (limit: number) => ['playlists', 'recent', limit] as const,
	topPlaylists: (limit: number) => ['playlists', 'top', limit] as const,
	playlistsAll: ['playlists'] as const,
	track: (id: string | null) => ['track', id] as const,
	audioProfile: (range: string) => ['stats', 'audio-profile', range] as const,
	topTracks: (range: StatsRange, limit: number) => ['stats', 'top-tracks', range, limit] as const,
	topArtists: (range: StatsRange, limit: number) => ['stats', 'top-artists', range, limit] as const,
	statsAll: ['stats'] as const
};

export function createQueryClient(): QueryClient {
	return new QueryClient({
		defaultOptions: {
			queries: {
				// Static prerendered shell: never fetch during the build.
				enabled: browser,
				staleTime: 0,
				refetchOnMount: 'always',
				refetchOnWindowFocus: 'always',
				refetchOnReconnect: 'always',
				refetchIntervalInBackground: false,
				retry: (failureCount, err) =>
					failureCount < 2 && !(err instanceof ApiError && err.status === 404)
			}
		}
	});
}

/**
 * Options for the periodic safety-net refresh. Give `refresh: true` to exactly
 * one observer per query, so the interval timers don't multiply.
 */
type RefreshOpt = { refresh?: boolean };

export function nowPlayingQuery() {
	return queryOptions({
		queryKey: queryKeys.nowPlaying,
		queryFn: async ({ signal, client }): Promise<NowPlaying | null> => {
			const next = await fetchNowPlaying(signal);
			// An SSE frame may have landed while this request was in flight:
			// never replace a newer snapshot with an older one.
			const cached = client.getQueryData<NowPlaying | null>(queryKeys.nowPlaying);
			if (cached && next && Date.parse(cached.fetched_at) > Date.parse(next.fetched_at)) {
				return cached;
			}
			return next;
		}
	});
}

export function historyQuery({ refresh = false }: RefreshOpt = {}) {
	return queryOptions({
		queryKey: queryKeys.history(HISTORY_LIMIT),
		queryFn: ({ signal }) => fetchHistory(HISTORY_LIMIT, signal),
		refetchInterval: refresh ? HISTORY_REFRESH_MS : false
	});
}

export function recentPlaylistsQuery({ refresh = false }: RefreshOpt = {}) {
	return queryOptions({
		queryKey: queryKeys.recentPlaylists(PLAYLIST_LIMIT),
		queryFn: ({ signal }) => fetchRecentPlaylists(PLAYLIST_LIMIT, signal),
		refetchInterval: refresh ? PLAYLISTS_REFRESH_MS : false
	});
}

export function topPlaylistsQuery() {
	return queryOptions({
		queryKey: queryKeys.topPlaylists(TOP_PLAYLIST_LIMIT),
		queryFn: ({ signal }) => fetchTopPlaylists(TOP_PLAYLIST_LIMIT, signal),
		refetchInterval: TOP_PLAYLISTS_REFRESH_MS
	});
}

/** Track detail for the song on show; idle (no request) without an id. */
export function trackDetailQuery(id: string | null) {
	return queryOptions({
		queryKey: queryKeys.track(id),
		queryFn: id ? ({ signal }) => fetchTrackDetail(id, signal) : skipToken,
		// Poll fast until the audio features land, then back to the normal interval.
		refetchInterval: (q) => (isAnalysing(q.state.data) ? TRACK_PENDING_MS : TRACK_REFRESH_MS)
	});
}

/** Loaded, but the enricher has not finished: features missing or still pending. */
export function isAnalysing(d: TrackDetail | undefined): boolean {
	return !!d && (!d.features || d.features.status === 'pending');
}

export function audioProfileQuery(range = PROFILE_RANGE) {
	return queryOptions({
		queryKey: queryKeys.audioProfile(range),
		queryFn: ({ signal }) => fetchAudioProfile(range, signal),
		refetchInterval: PROFILE_REFRESH_MS
	});
}

// Top lists: keep the previous range on screen while the next one loads.
// Explicit return types: keepPreviousData confuses TData inference.
export function topTracksQuery(range: StatsRange) {
	return queryOptions({
		queryKey: queryKeys.topTracks(range, TOP_LIMIT),
		queryFn: ({ signal }): Promise<TopTrack[]> => fetchTopTracks(range, TOP_LIMIT, signal),
		placeholderData: keepPreviousData,
		refetchInterval: TOP_REFRESH_MS
	});
}

export function topArtistsQuery(range: StatsRange) {
	return queryOptions({
		queryKey: queryKeys.topArtists(range, TOP_LIMIT),
		queryFn: ({ signal }): Promise<TopArtist[]> => fetchTopArtists(range, TOP_LIMIT, signal),
		placeholderData: keepPreviousData,
		refetchInterval: TOP_REFRESH_MS
	});
}

export type LoadStatus = 'loading' | 'ready' | 'error';

/**
 * Screen status of a query: any data (even from before a failed refetch)
 * is "ready", so errors only surface with nothing to show.
 */
export function loadStatus(q: { data: unknown; isError: boolean }): LoadStatus {
	if (q.data !== undefined) return 'ready';
	return q.isError ? 'error' : 'loading';
}
