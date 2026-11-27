// Listen-along API calls. Same error type as the public API (#lib/api), plus
// an optional Clerk bearer token. Same origin; never cached.

import { ApiError } from '../api';
import type {
	ListenAudio,
	ListenConfig,
	ListenHistoryPage,
	ListenMe,
	ListenProgress
} from './types';

export const LISTEN_API = {
	config: '/api/v1/listen/config',
	me: '/api/v1/listen/me',
	audio: '/api/v1/listen/audio',
	progress: '/api/v1/listen/progress',
	history: '/api/v1/listen/history'
} as const;

export const HISTORY_LIMIT = 50;

type ErrorBody = { error?: { code?: string; message?: string } };

type SendOptions = { method?: 'GET' | 'POST'; body?: unknown; keepalive?: boolean };

async function call(
	path: string,
	token: string | null | undefined,
	signal: AbortSignal | undefined,
	opts: SendOptions = {}
): Promise<unknown> {
	const headers: Record<string, string> = { accept: 'application/json' };
	if (token) headers.authorization = `Bearer ${token}`;
	if (opts.body !== undefined) headers['content-type'] = 'application/json';
	let res: Response;
	try {
		res = await fetch(path, {
			method: opts.method ?? 'GET',
			headers,
			cache: 'no-store',
			body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
			keepalive: opts.keepalive,
			signal
		});
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
		const e = (body as ErrorBody | null)?.error;
		if (e && typeof e.message === 'string') {
			throw new ApiError(res.status, e.code ?? 'http_error', e.message);
		}
		throw new ApiError(res.status, 'http_error', `Request failed (${res.status}).`);
	}
	if (res.status === 204) return null;
	if (body === null || typeof body !== 'object' || !('data' in body)) {
		throw new ApiError(res.status, 'bad_response', 'Unexpected response from the server.');
	}
	return body;
}

async function getData<T>(path: string, token?: string | null, signal?: AbortSignal): Promise<T> {
	return ((await call(path, token, signal)) as { data: T }).data;
}

export function fetchListenConfig(signal?: AbortSignal): Promise<ListenConfig> {
	return getData<ListenConfig>(LISTEN_API.config, null, signal);
}

export function fetchListenMe(token: string | null, signal?: AbortSignal): Promise<ListenMe> {
	return getData<ListenMe>(LISTEN_API.me, token, signal);
}

export function fetchListenAudio(token: string | null, signal?: AbortSignal): Promise<ListenAudio> {
	return getData<ListenAudio>(LISTEN_API.audio, token, signal);
}

/** Report played audio time (a delta, 1..20000 ms). `keepalive` survives page hide. */
export async function postListenProgress(
	token: string | null,
	body: ListenProgress,
	keepalive = false
): Promise<void> {
	await call(LISTEN_API.progress, token, undefined, { method: 'POST', body, keepalive });
}

/** The caller's own counted plays, newest first. */
export async function fetchListenHistory(
	token: string | null,
	cursor: string | null,
	signal?: AbortSignal
): Promise<ListenHistoryPage> {
	const q = new URLSearchParams({ limit: String(HISTORY_LIMIT) });
	if (cursor) q.set('cursor', cursor);
	const page = (await call(`${LISTEN_API.history}?${q}`, token, signal)) as ListenHistoryPage;
	return { data: page.data ?? [], next_cursor: page.next_cursor ?? null };
}
