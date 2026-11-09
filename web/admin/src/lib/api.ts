// Typed client for the admin API (/admin/api) and the public read API (/api/v1).
// Every /admin/api call except /config carries a fresh Clerk session token.

import type {
	AdminConfig,
	AdminTopPlaylist,
	AdminTopPlaylistDetail,
	Analytics,
	AnalyticsRange,
	ArtistRow,
	ArtistSort,
	AudioProfile,
	AuditEntry,
	CursorList,
	EmailSettings,
	EmailSettingsUpdate,
	EnrichmentStatus,
	GenreStat,
	HistoryEntry,
	MeResponse,
	NowPlaying,
	OffsetList,
	OkResult,
	Overview,
	PageAnalytics,
	SortOrder,
	SpotifyAuthorizeResponse,
	SpotifyStatus,
	SpotifyTestResponse,
	StatsRange,
	TopArtist,
	TopPlaylistPeriod,
	TopPlaylistsList,
	TopTrack,
	TrackAdminDetail,
	TrackRow,
	TrackSort,
	Playlist,
	RecentPlaylist,
	ServerZone,
	V1Item,
	V1List
} from './types';

export const ADMIN_API = '/admin/api';
export const PUBLIC_API = '/api/v1';

export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly fields: Record<string, string>;

	constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
		this.fields = fields ?? {};
	}

	get isValidation(): boolean {
		return this.status === 422 || Object.keys(this.fields).length > 0;
	}
}

export function errorMessage(err: unknown): string {
	if (err instanceof ApiError) return err.message;
	if (err instanceof Error) return err.message;
	return 'Something went wrong';
}

// ---------- auth hooks (wired up by auth.svelte.ts, avoids an import cycle) ----------

type TokenProvider = () => Promise<string | null>;
type AuthFailureHandler = (status: 401 | 403, err: ApiError) => void;

let tokenProvider: TokenProvider = async () => null;
let onAuthFailure: AuthFailureHandler = () => {};

export function configureAuth(provider: TokenProvider, handler: AuthFailureHandler): void {
	tokenProvider = provider;
	onAuthFailure = handler;
}

// ---------- core request ----------

type Query = Record<string, string | number | boolean | null | undefined>;

type RequestOptions = {
	method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
	query?: Query;
	body?: unknown;
	signal?: AbortSignal;
	/** Attach the Clerk bearer token (default true for /admin/api). */
	auth?: boolean;
	/** Don't trigger the global 401/403 handler (used by the auth bootstrap). */
	silentAuthErrors?: boolean;
};

function buildUrl(path: string, query?: Query): string {
	if (!query) return path;
	const params = new URLSearchParams();
	for (const [k, v] of Object.entries(query)) {
		if (v === undefined || v === null || v === '') continue;
		params.set(k, String(v));
	}
	const qs = params.toString();
	return qs ? `${path}?${qs}` : path;
}

async function parseError(res: Response): Promise<ApiError> {
	let code = `http_${res.status}`;
	let message = res.statusText || `Request failed (${res.status})`;
	let fields: Record<string, string> | undefined;
	try {
		const body = (await res.json()) as {
			error?: { code?: string; message?: string; fields?: Record<string, string> };
		};
		if (body?.error) {
			code = body.error.code ?? code;
			message = body.error.message ?? message;
			fields = body.error.fields;
		}
	} catch {
		// non-JSON error body (proxy error page etc.)
	}
	if (res.status === 429 && !fields) {
		message = message || 'Too many requests, slow down a little.';
	}
	return new ApiError(res.status, code, message, fields);
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
	const method = opts.method ?? 'GET';
	const useAuth = opts.auth ?? path.startsWith(ADMIN_API);
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
	if (useAuth) {
		const token = await tokenProvider();
		if (token) headers.Authorization = `Bearer ${token}`;
	}

	let res: Response;
	try {
		res = await fetch(buildUrl(path, opts.query), {
			method,
			headers,
			credentials: 'same-origin',
			body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
			signal: opts.signal
		});
	} catch (err) {
		if (err instanceof DOMException && err.name === 'AbortError') throw err;
		throw new ApiError(0, 'network_error', 'Cannot reach the API. Is the server running?');
	}

	if (!res.ok) {
		const err = await parseError(res);
		if ((res.status === 401 || res.status === 403) && useAuth && !opts.silentAuthErrors) {
			onAuthFailure(res.status, err);
		}
		throw err;
	}
	if (res.status === 204) return undefined as T;
	const text = await res.text();
	return (text ? JSON.parse(text) : undefined) as T;
}

// ---------- endpoints ----------

/** `{start}` is the row's period_start exactly as returned, URL-encoded. */
function topPlaylistPath(period: TopPlaylistPeriod, start: string): string {
	return `${ADMIN_API}/top-playlists/${period}/${encodeURIComponent(start)}`;
}

type Signal = { signal?: AbortSignal };

export const api = {
	// auth
	config: () => request<AdminConfig>(`${ADMIN_API}/config`, { auth: false }),
	me: (opts: { silent?: boolean } = {}) =>
		request<MeResponse>(`${ADMIN_API}/auth/me`, { silentAuthErrors: opts.silent }),

	// overview
	overview: ({ signal }: Signal = {}) => request<Overview>(`${ADMIN_API}/overview`, { signal }),

	// spotify
	spotify: ({ signal }: Signal = {}) => request<SpotifyStatus>(`${ADMIN_API}/spotify`, { signal }),
	spotifyAuthorize: () =>
		request<SpotifyAuthorizeResponse>(`${ADMIN_API}/spotify/authorize`, { method: 'POST' }),
	spotifyTest: () => request<SpotifyTestResponse>(`${ADMIN_API}/spotify/test`, { method: 'POST' }),

	// email
	emailSettings: ({ signal }: Signal = {}) =>
		request<EmailSettings>(`${ADMIN_API}/settings/email`, { signal }),
	saveEmailSettings: (body: EmailSettingsUpdate) =>
		request<EmailSettings>(`${ADMIN_API}/settings/email`, { method: 'PUT', body }),
	testEmail: (to?: string) =>
		request<OkResult>(`${ADMIN_API}/settings/email/test`, {
			method: 'POST',
			body: to ? { to } : {}
		}),

	// data
	history: (q: { limit?: number; cursor?: string | null; q?: string }, { signal }: Signal = {}) =>
		request<CursorList<HistoryEntry>>(`${ADMIN_API}/history`, { query: q, signal }),
	tracks: (
		q: {
			q?: string;
			status?: EnrichmentStatus | '';
			sort?: TrackSort;
			order?: SortOrder;
			limit?: number;
			offset?: number;
		},
		{ signal }: Signal = {}
	) => request<OffsetList<TrackRow>>(`${ADMIN_API}/tracks`, { query: q, signal }),
	track: (id: string, { signal }: Signal = {}) =>
		request<TrackAdminDetail>(`${ADMIN_API}/tracks/${encodeURIComponent(id)}`, { signal }),
	enrichTrack: (id: string) =>
		request<{ queued: boolean }>(`${ADMIN_API}/tracks/${encodeURIComponent(id)}/enrich`, {
			method: 'POST'
		}),
	retryFailed: () =>
		request<{ requeued: number }>(`${ADMIN_API}/enrichment/retry-failed`, { method: 'POST' }),
	artists: (
		q: { q?: string; sort?: ArtistSort; order?: SortOrder; limit?: number; offset?: number },
		{ signal }: Signal = {}
	) => request<OffsetList<ArtistRow>>(`${ADMIN_API}/artists`, { query: q, signal }),

	// analytics
	/** `tz`: the viewer's IANA zone, so 30d buckets are their calendar days; null = UTC. */
	analytics: (range: AnalyticsRange, tz: string | null, { signal }: Signal = {}) =>
		request<Analytics>(`${ADMIN_API}/analytics`, { query: { range, tz }, signal }),
	/** Public-page visitors; same `range` / `tz` semantics as `analytics`. */
	pageAnalytics: (range: AnalyticsRange, tz: string | null, { signal }: Signal = {}) =>
		request<PageAnalytics>(`${ADMIN_API}/analytics/pages`, { query: { range, tz }, signal }),

	// audit
	audit: (q: { limit?: number; cursor?: string | null }, { signal }: Signal = {}) =>
		request<CursorList<AuditEntry>>(`${ADMIN_API}/audit`, { query: q, signal }),

	// playlists
	playlists: (q: { limit?: number; offset?: number }, { signal }: Signal = {}) =>
		request<OffsetList<RecentPlaylist>>(`${ADMIN_API}/playlists`, { query: q, signal }),
	refreshPlaylist: (id: string) =>
		request<Playlist>(`${ADMIN_API}/playlists/${encodeURIComponent(id)}/refresh`, {
			method: 'POST'
		}),
	// top 50 playlists
	topPlaylists: (
		q: { period?: TopPlaylistPeriod | ''; limit?: number; offset?: number },
		{ signal }: Signal = {}
	) => request<TopPlaylistsList>(`${ADMIN_API}/top-playlists`, { query: q, signal }),
	topPlaylist: (period: TopPlaylistPeriod, start: string, { signal }: Signal = {}) =>
		request<V1Item<AdminTopPlaylistDetail>>(topPlaylistPath(period, start), { signal }).then(
			(r) => r.data
		),
	runTopPlaylists: () =>
		request<TopPlaylistsList>(`${ADMIN_API}/top-playlists/run`, { method: 'POST' }),
	/** Check published playlists against Spotify now and restore missing ones. */
	verifyTopPlaylists: () =>
		request<TopPlaylistsList>(`${ADMIN_API}/top-playlists/verify`, { method: 'POST' }),
	/** `date`: YYYY-MM-DD anywhere inside the (completed) period, in the configured zone. */
	generateTopPlaylist: (period: TopPlaylistPeriod, date: string) =>
		request<V1Item<AdminTopPlaylist>>(`${ADMIN_API}/top-playlists/generate`, {
			method: 'POST',
			body: { period, date }
		}).then((r) => r.data),
	regenerateTopPlaylist: (period: TopPlaylistPeriod, start: string) =>
		request<V1Item<AdminTopPlaylist>>(`${topPlaylistPath(period, start)}/regenerate`, {
			method: 'POST'
		}).then((r) => r.data),
	setTopPlaylistHidden: (period: TopPlaylistPeriod, start: string, hidden: boolean) =>
		request<V1Item<AdminTopPlaylist>>(topPlaylistPath(period, start), {
			method: 'PATCH',
			body: { hidden }
		}).then((r) => r.data),

	/** The server's current view of a zone (its tz data can be newer than the browser's). */
	timezone: (tz: string, { signal }: Signal = {}) =>
		request<V1Item<ServerZone>>(`${PUBLIC_API}/timezone`, { query: { tz }, signal }),
	recentPlaylists: (limit = 5, { signal }: Signal = {}) =>
		request<V1List<RecentPlaylist>>(`${PUBLIC_API}/playlists/recent`, {
			query: { limit },
			signal
		}),

	// public stats (/api/v1, no auth)
	nowPlaying: () => request<V1Item<NowPlaying | null>>(`${PUBLIC_API}/now-playing`),
	topTracks: (range: StatsRange, limit = 10, { signal }: Signal = {}) =>
		request<V1List<TopTrack>>(`${PUBLIC_API}/stats/top-tracks`, {
			query: { range, limit },
			signal
		}),
	topArtists: (range: StatsRange, limit = 10, { signal }: Signal = {}) =>
		request<V1List<TopArtist>>(`${PUBLIC_API}/stats/top-artists`, {
			query: { range, limit },
			signal
		}),
	genres: (range: StatsRange, limit = 10, { signal }: Signal = {}) =>
		request<V1List<GenreStat>>(`${PUBLIC_API}/stats/genres`, {
			query: { range, limit },
			signal
		}),
	audioProfile: (range: StatsRange, { signal }: Signal = {}) =>
		request<V1Item<AudioProfile>>(`${PUBLIC_API}/stats/audio-profile`, {
			query: { range },
			signal
		})
};

export const NOW_PLAYING_STREAM = `${PUBLIC_API}/now-playing/stream`;
