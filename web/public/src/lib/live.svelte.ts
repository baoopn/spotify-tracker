// Live connection: the now-playing SSE stream (with reconnect backoff) writing
// into the TanStack Query cache, plus the server clock-skew estimate. Data and
// its load/error state live in the query cache (see queries.ts); this module
// only owns the stream, the connection status and the track-change invalidation.

import { hashKey, type QueryCacheNotifyEvent, type QueryClient } from '@tanstack/svelte-query';
import { API } from './api';
import { isAnalysing, queryKeys } from './queries';
import type { NowPlaying, TrackDetail } from './types';

/**
 * connecting:   first stream connection pending (bounded by CONNECT_TIMEOUT_MS)
 * live:         stream open
 * reconnecting: stream down, retry scheduled at `retryAt`, polling not yet confirmed
 * polling:      stream down or unsupported, but polling succeeds
 */
export type Connection = 'connecting' | 'live' | 'reconnecting' | 'polling';

/** The stream sends about every 3 s; treat it as down after this much silence. */
const STREAM_STALE_MS = 20_000;
const BACKOFF_MIN_MS = 1_000;
const BACKOFF_MAX_MS = 30_000;
/** A stream that neither opens nor errors this long counts as failed. */
const CONNECT_TIMEOUT_MS = 10_000;
/** The tracker records the play right around a track change; refresh again after this. */
const PLAY_RECORDED_DELAY_MS = 4_000;

const NOW_HASH = hashKey(queryKeys.nowPlaying);

function isNowPlaying(v: unknown): v is NowPlaying {
	return typeof v === 'object' && v !== null && 'is_playing' in v && 'fetched_at' in v;
}

class LiveState {
	/**
	 * Estimated client-minus-server clock skew (ms): the smallest recent gap
	 * between receiving a snapshot and its server `fetched_at`. A wrong system
	 * clock then cannot push progress or "x min ago" off.
	 */
	skewMs = $state(0);
	#skewSamples: number[] = [];

	connection = $state<Connection>('connecting');
	/** Client time (ms) of the next stream reconnect attempt while disconnected. */
	retryAt = $state<number | null>(null);

	/** Poll now playing over HTTP (the stream is down or unsupported). */
	get polling(): boolean {
		return this.connection === 'reconnecting' || this.connection === 'polling';
	}

	#client: QueryClient | null = null;
	#source: EventSource | null = null;
	#timers = new Set<ReturnType<typeof setTimeout>>();
	#staleTimer: ReturnType<typeof setTimeout> | null = null;
	#connectTimer: ReturnType<typeof setTimeout> | null = null;
	#everOpen = false;
	#attempt = 0;
	#running = false;
	/** Track id of the last snapshot seen; undefined before the first one. */
	#trackId: string | null | undefined = undefined;

	/** Start the stream, writing into `client`'s cache. Returns a cleanup function. */
	start(client: QueryClient): () => void {
		if (this.#running) return () => {};
		this.#running = true;
		this.#client = client;

		// Every snapshot, from the stream (setQueryData) or HTTP (fetch/poll),
		// lands here: sample the skew and react to track changes.
		const unsubscribe = client.getQueryCache().subscribe((ev) => this.#onCacheEvent(ev));

		if (typeof EventSource === 'undefined') {
			this.connection = 'polling';
		} else {
			this.#connect();
		}

		const onVisible = () => {
			if (document.visibilityState !== 'visible') return;
			if (!this.#source && typeof EventSource !== 'undefined') {
				this.#attempt = 0;
				this.#connect();
			}
		};
		document.addEventListener('visibilitychange', onVisible);

		return () => {
			document.removeEventListener('visibilitychange', onVisible);
			unsubscribe();
			this.#stop();
		};
	}

	/** Retry everything after an error (button in the UI). */
	retry(): void {
		this.#attempt = 0;
		const client = this.#client;
		if (client) {
			for (const queryKey of [queryKeys.nowPlaying, queryKeys.historyAll, queryKeys.playlistsAll]) {
				void client.refetchQueries({ queryKey, type: 'active' }, { cancelRefetch: false });
			}
		}
		if (this.#running && !this.#source && typeof EventSource !== 'undefined') this.#connect();
	}

	/** The server's "now" (ms since epoch) for a client instant, skew-corrected. */
	serverNow(clientNow: number = Date.now()): number {
		return clientNow - this.skewMs;
	}

	#onCacheEvent(ev: QueryCacheNotifyEvent): void {
		if (ev.type !== 'updated' || ev.action.type !== 'success') return;
		if (ev.query.queryHash !== NOW_HASH) return;
		const next = ev.query.state.data as NowPlaying | null | undefined;

		const fetched = next ? Date.parse(next.fetched_at) : NaN;
		if (!Number.isNaN(fetched)) {
			this.#skewSamples.push(Date.now() - fetched);
			if (this.#skewSamples.length > 20) this.#skewSamples.shift();
			this.skewMs = Math.min(...this.#skewSamples);
		}

		// Stream down but HTTP works: be honest that we are polling.
		if (!ev.action.manual && this.connection === 'reconnecting') this.connection = 'polling';

		const prevId = this.#trackId;
		const nextId = next?.track?.id ?? null;
		this.#trackId = nextId;
		if (this.#running && prevId !== undefined && prevId !== nextId) {
			this.#invalidatePlays();
			this.#later(() => this.#invalidatePlays(nextId), PLAY_RECORDED_DELAY_MS);
		}
	}

	/** A play was (about to be) recorded: refresh everything derived from plays. */
	#invalidatePlays(trackId?: string | null): void {
		const client = this.#client;
		if (!client) return;
		void client.invalidateQueries({ queryKey: queryKeys.historyAll });
		void client.invalidateQueries({ queryKey: queryKeys.playlistsAll });
		void client.invalidateQueries({ queryKey: queryKeys.statsAll });
		if (trackId) void client.invalidateQueries({ queryKey: queryKeys.track(trackId) });
	}

	/**
	 * A frame for the same track: nudge its detail while it is still being
	 * analysed (the 4 s poll covers the rest), so the stats show up promptly.
	 * Enriched details are left to their normal interval.
	 */
	#nudgeDetail(prevId: string | null | undefined, id: string | null): void {
		const client = this.#client;
		if (!client || !id || id !== prevId) return;
		const key = queryKeys.track(id);
		const detail = client.getQueryData<TrackDetail>(key);
		if (!isAnalysing(detail)) return;
		if (client.getQueryState(key)?.fetchStatus === 'fetching') return;
		void client.invalidateQueries({ queryKey: key });
	}

	#refetchNow(): void {
		void this.#client?.refetchQueries(
			{ queryKey: queryKeys.nowPlaying, type: 'active' },
			{ cancelRefetch: false }
		);
	}

	#connect(): void {
		this.#closeSource();
		// "Connecting…" only for the very first attempt; retries keep the
		// reconnecting/polling status (with its countdown) until the stream opens.
		if (!this.#everOpen && this.#attempt === 0) this.connection = 'connecting';
		this.retryAt = null;
		let es: EventSource;
		try {
			es = new EventSource(API.stream);
		} catch {
			this.connection = 'polling';
			return;
		}
		this.#source = es;
		// Never hang in a pending state (e.g. a proxy that buffers the stream).
		this.#connectTimer = setTimeout(() => {
			if (this.#source === es && this.connection !== 'live') this.#fail();
		}, CONNECT_TIMEOUT_MS);

		es.onopen = () => {
			this.#attempt = 0;
			this.#everOpen = true;
			this.#clearConnectTimer();
			// Leaving 'reconnecting'/'polling' also stops the HTTP poll (refetchInterval).
			this.connection = 'live';
			this.#armStale();
		};
		es.addEventListener('now-playing', (ev) => {
			this.connection = 'live';
			this.#armStale();
			let data: unknown;
			try {
				data = JSON.parse((ev as MessageEvent<string>).data);
			} catch {
				return; // ignore a malformed frame; the next one will follow shortly
			}
			const snapshot = isNowPlaying(data) ? data : null;
			const prevId = this.#trackId;
			this.#client?.setQueryData(queryKeys.nowPlaying, snapshot);
			this.#nudgeDetail(prevId, snapshot?.track?.id ?? null);
		});
		// Take over reconnection ourselves (EventSource gives up on HTTP errors
		// and retries too eagerly otherwise) and poll while disconnected.
		es.onerror = () => this.#fail();
	}

	#fail(): void {
		this.#closeSource();
		// 'reconnecting'/'polling' switch on the now-playing refetchInterval.
		if (this.connection !== 'polling') this.connection = 'reconnecting';
		this.#refetchNow();
		this.#scheduleReconnect();
	}

	#scheduleReconnect(): void {
		const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** this.#attempt);
		this.#attempt++;
		const delay = base / 2 + Math.random() * (base / 2);
		this.retryAt = Date.now() + delay;
		this.#later(() => {
			if (this.#running && !this.#source) this.#connect();
		}, delay);
	}

	#clearConnectTimer(): void {
		if (this.#connectTimer) clearTimeout(this.#connectTimer);
		this.#connectTimer = null;
	}

	#armStale(): void {
		if (this.#staleTimer) clearTimeout(this.#staleTimer);
		this.#staleTimer = setTimeout(() => {
			// A silent stream (e.g. a dead upstream behind a proxy) is a failure:
			// stop claiming "live", poll, and reconnect with backoff.
			if (this.#running && this.#source) this.#fail();
		}, STREAM_STALE_MS);
	}

	#closeSource(): void {
		this.#clearConnectTimer();
		if (this.#staleTimer) clearTimeout(this.#staleTimer);
		this.#staleTimer = null;
		this.#source?.close();
		this.#source = null;
	}

	#later(fn: () => void, ms: number): void {
		const id = setTimeout(() => {
			this.#timers.delete(id);
			fn();
		}, ms);
		this.#timers.add(id);
	}

	#stop(): void {
		this.#running = false;
		this.#closeSource();
		for (const id of this.#timers) clearTimeout(id);
		this.#timers.clear();
		this.#client = null;
		this.#trackId = undefined;
	}
}

export const live = new LiveState();
