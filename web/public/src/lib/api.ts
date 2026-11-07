import type {
	ErrorResponse,
	HistoryEntry,
	ItemResponse,
	ListResponse,
	AudioProfile,
	NowPlaying,
	RecentPlaylist,
	StatsRange,
	TopArtist,
	TopTrack,
	TopPlaylists,
	TrackDetail
} from './types';

export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

function isErrorResponse(v: unknown): v is ErrorResponse {
	return (
		typeof v === 'object' &&
		v !== null &&
		'error' in v &&
		typeof (v as ErrorResponse).error?.message === 'string'
	);
}

async function getJson<T>(path: string, signal?: AbortSignal): Promise<T> {
	let res: Response;
	try {
		res = await fetch(path, { headers: { accept: 'application/json' }, signal });
	} catch (err) {
		if (err instanceof DOMException && err.name === 'AbortError') throw err;
		throw new ApiError(0, 'network', 'Could not reach the server.');
	}
	let body: unknown = null;
	try {
		body = await res.json();
	} catch {
		// non-JSON body (proxy error page, empty response)
	}
	if (!res.ok) {
		if (isErrorResponse(body)) throw new ApiError(res.status, body.error.code, body.error.message);
		throw new ApiError(res.status, 'http_error', `Request failed (${res.status}).`);
	}
	if (body === null || typeof body !== 'object' || !('data' in body)) {
		throw new ApiError(res.status, 'bad_response', 'Unexpected response from the server.');
	}
	return body as T;
}

export const API = {
	nowPlaying: '/api/v1/now-playing',
	stream: '/api/v1/now-playing/stream',
	history: (limit: number) => `/api/v1/history?limit=${limit}`,
	recentPlaylists: (limit: number) => `/api/v1/playlists/recent?limit=${limit}`,
	topPlaylists: (limit: number) => `/api/v1/playlists/top?limit=${limit}`,
	track: (id: string) => `/api/v1/tracks/${encodeURIComponent(id)}`,
	audioProfile: (range: string) => `/api/v1/stats/audio-profile?range=${range}`,
	topTracks: (range: StatsRange, limit: number) =>
		`/api/v1/stats/top-tracks?range=${range}&limit=${limit}`,
	topArtists: (range: StatsRange, limit: number) =>
		`/api/v1/stats/top-artists?range=${range}&limit=${limit}`
} as const;

export async function fetchNowPlaying(signal?: AbortSignal): Promise<NowPlaying | null> {
	return (await getJson<ItemResponse<NowPlaying | null>>(API.nowPlaying, signal)).data;
}

export async function fetchHistory(limit: number, signal?: AbortSignal): Promise<HistoryEntry[]> {
	return (await getJson<ListResponse<HistoryEntry>>(API.history(limit), signal)).data ?? [];
}

export async function fetchRecentPlaylists(
	limit: number,
	signal?: AbortSignal
): Promise<RecentPlaylist[]> {
	return (
		(await getJson<ListResponse<RecentPlaylist>>(API.recentPlaylists(limit), signal)).data ?? []
	);
}

export async function fetchTopPlaylists(
	limit: number,
	signal?: AbortSignal
): Promise<TopPlaylists> {
	return (await getJson<ItemResponse<TopPlaylists>>(API.topPlaylists(limit), signal)).data;
}

export async function fetchTrackDetail(id: string, signal?: AbortSignal): Promise<TrackDetail> {
	return (await getJson<ItemResponse<TrackDetail>>(API.track(id), signal)).data;
}

export async function fetchAudioProfile(
	range = '30d',
	signal?: AbortSignal
): Promise<AudioProfile | null> {
	return (await getJson<ItemResponse<AudioProfile | null>>(API.audioProfile(range), signal)).data;
}

export async function fetchTopTracks(
	range: StatsRange,
	limit: number,
	signal?: AbortSignal
): Promise<TopTrack[]> {
	return (await getJson<ListResponse<TopTrack>>(API.topTracks(range, limit), signal)).data ?? [];
}

export async function fetchTopArtists(
	range: StatsRange,
	limit: number,
	signal?: AbortSignal
): Promise<TopArtist[]> {
	return (await getJson<ListResponse<TopArtist>>(API.topArtists(range, limit), signal)).data ?? [];
}

export function errorMessage(err: unknown): string {
	if (err instanceof ApiError) return err.message;
	if (err instanceof Error) return err.message;
	return 'Something went wrong.';
}
