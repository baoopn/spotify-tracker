// Query keys and option factories for every server read in the admin SPA.
// Keys are hierarchical so a mutation can invalidate a whole family
// (e.g. `qk.tracks.all` hits every tracks list page and every track detail).

import { infiniteQueryOptions, keepPreviousData, queryOptions } from '@tanstack/svelte-query';
import { api } from './api';
import type {
	AnalyticsRange,
	ArtistSort,
	AudioProfile,
	EnrichmentStatus,
	GenreStat,
	RecentPlaylist,
	SortOrder,
	StatsRange,
	TopArtist,
	TopTrack,
	TrackSort
} from './types';

/** Polling cadence per kind of data (paused while the tab is hidden). */
export const REFRESH = {
	/** Overview counters, Spotify status, recent playlists. */
	live: 15_000,
	/** Paginated data lists and the track drawer. */
	list: 30_000,
	/** Analytics and listening insights. */
	analytics: 60_000
} as const;

export type TracksParams = {
	q: string;
	status: EnrichmentStatus | '';
	sort: TrackSort;
	order: SortOrder;
	limit: number;
	offset: number;
};
export type ArtistsParams = {
	q: string;
	sort: ArtistSort;
	order: SortOrder;
	limit: number;
	offset: number;
};
export type PlaylistsParams = { limit: number; offset: number };

export const qk = {
	overview: ['overview'] as const,
	spotify: ['spotify', 'status'] as const,
	emailSettings: ['settings', 'email'] as const,
	analytics: (range: AnalyticsRange, tz: string | null) => ['analytics', range, tz] as const,
	stats: {
		all: ['stats'] as const,
		topTracks: (range: StatsRange, limit: number) => ['stats', 'top-tracks', range, limit] as const,
		topArtists: (range: StatsRange, limit: number) =>
			['stats', 'top-artists', range, limit] as const,
		genres: (range: StatsRange, limit: number) => ['stats', 'genres', range, limit] as const,
		audioProfile: (range: StatsRange) => ['stats', 'audio-profile', range] as const
	},
	tracks: {
		all: ['tracks'] as const,
		lists: ['tracks', 'list'] as const,
		list: (p: TracksParams) => ['tracks', 'list', p] as const,
		detail: (id: string) => ['tracks', 'detail', id] as const
	},
	artists: {
		all: ['artists'] as const,
		list: (p: ArtistsParams) => ['artists', 'list', p] as const
	},
	playlists: {
		all: ['playlists'] as const,
		lists: ['playlists', 'list'] as const,
		list: (p: PlaylistsParams) => ['playlists', 'list', p] as const,
		recent: (limit: number) => ['playlists', 'recent', limit] as const
	},
	history: (q: string, limit: number) => ['history', { q, limit }] as const,
	audit: (limit: number) => ['audit', { limit }] as const
};

export const queries = {
	overview: () =>
		queryOptions({
			queryKey: qk.overview,
			queryFn: ({ signal }) => api.overview({ signal }),
			refetchInterval: REFRESH.live
		}),

	spotify: () =>
		queryOptions({
			queryKey: qk.spotify,
			queryFn: ({ signal }) => api.spotify({ signal }),
			refetchInterval: REFRESH.live
		}),

	/** A form page: refetched on mount/focus, but not polled (the form is filled once). */
	emailSettings: () =>
		queryOptions({
			queryKey: qk.emailSettings,
			queryFn: ({ signal }) => api.emailSettings({ signal })
		}),

	recentPlaylists: (limit = 5) =>
		queryOptions({
			queryKey: qk.playlists.recent(limit),
			queryFn: ({ signal }): Promise<RecentPlaylist[]> =>
				api.recentPlaylists(limit, { signal }).then((r) => r.data),
			refetchInterval: REFRESH.live
		}),

	analytics: (range: AnalyticsRange, tz: string | null) =>
		queryOptions({
			queryKey: qk.analytics(range, tz),
			queryFn: ({ signal }) => api.analytics(range, tz, { signal }),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.analytics
		}),

	topTracks: (range: StatsRange, limit = 10) =>
		queryOptions({
			queryKey: qk.stats.topTracks(range, limit),
			// Explicit return types: `.then` + keepPreviousData confuse TData inference.
			queryFn: ({ signal }): Promise<TopTrack[]> =>
				api.topTracks(range, limit, { signal }).then((r) => r.data),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.analytics
		}),

	topArtists: (range: StatsRange, limit = 10) =>
		queryOptions({
			queryKey: qk.stats.topArtists(range, limit),
			queryFn: ({ signal }): Promise<TopArtist[]> =>
				api.topArtists(range, limit, { signal }).then((r) => r.data),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.analytics
		}),

	genres: (range: StatsRange, limit = 10) =>
		queryOptions({
			queryKey: qk.stats.genres(range, limit),
			queryFn: ({ signal }): Promise<GenreStat[]> =>
				api.genres(range, limit, { signal }).then((r) => r.data),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.analytics
		}),

	audioProfile: (range: StatsRange) =>
		queryOptions({
			queryKey: qk.stats.audioProfile(range),
			queryFn: ({ signal }): Promise<AudioProfile> =>
				api.audioProfile(range, { signal }).then((r) => r.data),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.analytics
		}),

	tracks: (p: TracksParams) =>
		queryOptions({
			queryKey: qk.tracks.list(p),
			queryFn: ({ signal }) => api.tracks(p, { signal }),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.list
		}),

	/** Disabled (and so not polled) while no track is selected. */
	track: (id: string | null) =>
		queryOptions({
			queryKey: qk.tracks.detail(id ?? ''),
			queryFn: ({ signal }) => api.track(id ?? '', { signal }),
			enabled: !!id,
			refetchInterval: REFRESH.list
		}),

	artists: (p: ArtistsParams) =>
		queryOptions({
			queryKey: qk.artists.list(p),
			queryFn: ({ signal }) => api.artists(p, { signal }),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.list
		}),

	playlists: (p: PlaylistsParams) =>
		queryOptions({
			queryKey: qk.playlists.list(p),
			queryFn: ({ signal }) => api.playlists(p, { signal }),
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.list
		}),

	/** Cursor-paged; a refetch re-reads every loaded page from the newest one. */
	history: (q: string, limit = 50) =>
		infiniteQueryOptions({
			queryKey: qk.history(q, limit),
			queryFn: ({ signal, pageParam }) => api.history({ limit, q, cursor: pageParam }, { signal }),
			initialPageParam: null as string | null,
			getNextPageParam: (last) => last.next_cursor || null,
			placeholderData: keepPreviousData,
			refetchInterval: REFRESH.list
		}),

	audit: (limit = 50) =>
		infiniteQueryOptions({
			queryKey: qk.audit(limit),
			queryFn: ({ signal, pageParam }) => api.audit({ limit, cursor: pageParam }, { signal }),
			initialPageParam: null as string | null,
			getNextPageParam: (last) => last.next_cursor || null,
			refetchInterval: REFRESH.list
		})
};
