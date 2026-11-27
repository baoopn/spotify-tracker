// Listen-along invitations (/admin/api/listeners): types, calls and the query.

import { queryOptions } from '@tanstack/svelte-query';
import { ADMIN_API, request } from './api';

export type ListenerStatus = 'invited' | 'active' | 'revoked';

export type Listener = {
	id: number;
	email: string;
	status: ListenerStatus;
	clerk_user_id: string | null;
	/** The email already has a Clerk account (no invitation was sent). */
	has_account: boolean;
	invited_by: string;
	invited_at: string;
	accepted_at: string | null;
	revoked_at: string | null;
	last_seen_at: string | null;
};

/** GET /listeners: active, then invited, then revoked (each newest first). */
export type ListenersList = {
	data: Listener[];
	/** LISTEN_ENABLED: false means /listen says it isn't available. */
	listen_enabled: boolean;
	/** S3 storage is set up for the audio files. */
	audio_configured: boolean;
	listen_url: string;
};

export type InviteResult = { data: Listener; invitation_sent: boolean };
export type ResendResult = { data: Listener };
export type ClerkCleanup = 'ok' | 'failed' | 'skipped';
export type RevokeResult = { data: Listener; clerk_cleanup: ClerkCleanup };

const BASE = `${ADMIN_API}/listeners`;

export const listenersApi = {
	list: ({ signal }: { signal?: AbortSignal } = {}) => request<ListenersList>(BASE, { signal }),
	invite: (email: string) => request<InviteResult>(BASE, { method: 'POST', body: { email } }),
	resend: (id: number) => request<ResendResult>(`${BASE}/${id}/resend`, { method: 'POST' }),
	revoke: (id: number) => request<RevokeResult>(`${BASE}/${id}/revoke`, { method: 'POST' })
};

export const listenersKey = ['listeners'] as const;

export function listenersQuery() {
	return queryOptions({
		queryKey: listenersKey,
		queryFn: ({ signal }) => listenersApi.list({ signal }),
		refetchInterval: 60_000
	});
}

/** Resend only makes sense for a pending invitation to someone without an account. */
export function canResend(l: Listener): boolean {
	return l.status === 'invited' && !l.has_account;
}

/** One counted play in a listener's history (GET /listeners/{id}/history). */
export type ListenPlay = {
	id: number;
	/** When the play started. */
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

export type ListenHistoryPage = { data: ListenPlay[]; next_cursor: string | null };

export const HISTORY_LIMIT = 50;

/** A listener's counted plays, newest first (read-only, also for revoked listeners). */
export function listenerHistory(
	id: number,
	cursor: string | null,
	{ signal }: { signal?: AbortSignal } = {}
): Promise<ListenHistoryPage> {
	return request<ListenHistoryPage>(`${BASE}/${id}/history`, {
		query: { limit: HISTORY_LIMIT, cursor },
		signal
	});
}
