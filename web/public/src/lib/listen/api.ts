// Listen-along API calls. Same error type as the public API (#lib/api), plus
// an optional Clerk bearer token. Same origin; never cached.

import { ApiError } from '../api';
import type { ListenAudio, ListenConfig, ListenMe } from './types';

export const LISTEN_API = {
	config: '/api/v1/listen/config',
	me: '/api/v1/listen/me',
	audio: '/api/v1/listen/audio'
} as const;

type ErrorBody = { error?: { code?: string; message?: string } };

async function getData<T>(path: string, token?: string | null, signal?: AbortSignal): Promise<T> {
	const headers: Record<string, string> = { accept: 'application/json' };
	if (token) headers.authorization = `Bearer ${token}`;
	let res: Response;
	try {
		res = await fetch(path, { headers, cache: 'no-store', signal });
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
	if (body === null || typeof body !== 'object' || !('data' in body)) {
		throw new ApiError(res.status, 'bad_response', 'Unexpected response from the server.');
	}
	return (body as { data: T }).data;
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
