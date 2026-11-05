// Live listening state: now playing over SSE (with reconnect backoff and a
// polling fallback) plus the recent history, refreshed on track changes.

import { API, errorMessage, fetchHistory, fetchNowPlaying, fetchRecentPlaylists } from './api';
import type { HistoryEntry, NowPlaying, RecentPlaylist } from './types';

export type LoadStatus = 'loading' | 'ready' | 'error';
/**
 * connecting:   first stream connection pending (bounded by CONNECT_TIMEOUT_MS)
 * live:         stream open
 * reconnecting: stream down, retry scheduled at `retryAt`, polling not yet confirmed
 * polling:      stream down or unsupported, but polling succeeds
 */
export type Connection = 'connecting' | 'live' | 'reconnecting' | 'polling';

const HISTORY_LIMIT = 6;
const PLAYLIST_LIMIT = 5;
const POLL_MS = 10_000;
const HISTORY_REFRESH_MS = 60_000;
/** The stream sends about every 3 s; treat it as down after this much silence. */
const STREAM_STALE_MS = 20_000;
const BACKOFF_MIN_MS = 1_000;
const BACKOFF_MAX_MS = 30_000;
/** A stream that neither opens nor errors this long counts as failed. */
const CONNECT_TIMEOUT_MS = 10_000;

function isNowPlaying(v: unknown): v is NowPlaying {
	return typeof v === 'object' && v !== null && 'is_playing' in v && 'fetched_at' in v;
}

class LiveState {
	nowPlaying = $state<NowPlaying | null>(null);
	nowStatus = $state<LoadStatus>('loading');
	nowError = $state<string | null>(null);
	/**
	 * Estimated client-minus-server clock skew (ms): the smallest recent gap
	 * between receiving a snapshot and its server `fetched_at`. A wrong system
	 * clock then cannot push progress or "x min ago" off.
	 */
	skewMs = $state(0);
	#skewSamples: number[] = [];

	history = $state<HistoryEntry[]>([]);
	historyStatus = $state<LoadStatus>('loading');
	historyError = $state<string | null>(null);

	/** Secondary section: stays empty (hidden) until loaded, and on errors. */
	playlists = $state<RecentPlaylist[]>([]);
	playlistsStatus = $state<LoadStatus>('loading');

	connection = $state<Connection>('connecting');
	lastUpdated = $state<Date | null>(null);
	/** Client time (ms) of the next stream reconnect attempt while disconnected. */
	retryAt = $state<number | null>(null);

	#source: EventSource | null = null;
	#timers = new Set<ReturnType<typeof setTimeout>>();
	#pollTimer: ReturnType<typeof setInterval> | null = null;
	#historyTimer: ReturnType<typeof setInterval> | null = null;
	#staleTimer: ReturnType<typeof setTimeout> | null = null;
	#connectTimer: ReturnType<typeof setTimeout> | null = null;
	#everOpen = false;
	#attempt = 0;
	#running = false;
	#historyAbort: AbortController | null = null;
	#playlistsAbort: AbortController | null = null;

	/** Start live updates. Returns a cleanup function. */
	start(): () => void {
		if (this.#running) return () => {};
		this.#running = true;

		void this.refreshNow();
		void this.refreshHistory();
		this.#historyTimer = setInterval(() => void this.refreshHistory(), HISTORY_REFRESH_MS);

		if (typeof EventSource === 'undefined') {
			this.#startPolling();
			this.connection = 'polling';
		} else {
			this.#connect();
		}

		const onVisible = () => {
			if (document.visibilityState !== 'visible') return;
			void this.refreshHistory();
			if (!this.#source && typeof EventSource !== 'undefined') {
				this.#attempt = 0;
				this.#connect();
			}
		};
		document.addEventListener('visibilitychange', onVisible);

		return () => {
			document.removeEventListener('visibilitychange', onVisible);
			this.#stop();
		};
	}

	/** Retry everything after an error (button in the UI). */
	retry(): void {
		this.#attempt = 0;
		void this.refreshNow();
		void this.refreshHistory();
		if (this.#running && !this.#source && typeof EventSource !== 'undefined') this.#connect();
	}

	async refreshNow(): Promise<void> {
		try {
			this.#accept(await fetchNowPlaying());
			// Stream down but HTTP works: be honest that we are polling.
			if (this.connection === 'reconnecting') this.connection = 'polling';
		} catch (err) {
			// Keep showing the last good value; only surface the error with no data.
			if (this.nowStatus !== 'ready') {
				this.nowStatus = 'error';
				this.nowError = errorMessage(err);
			}
		}
	}

	async refreshHistory(): Promise<void> {
		void this.refreshPlaylists();
		this.#historyAbort?.abort();
		const ctrl = new AbortController();
		this.#historyAbort = ctrl;
		try {
			const items = await fetchHistory(HISTORY_LIMIT, ctrl.signal);
			if (ctrl.signal.aborted) return;
			this.history = items;
			this.historyStatus = 'ready';
			this.historyError = null;
		} catch (err) {
			if (ctrl.signal.aborted) return;
			if (this.historyStatus !== 'ready') {
				this.historyStatus = 'error';
				this.historyError = errorMessage(err);
			}
		}
	}

	/** Called with every history refresh (load, track change, every 60 s, tab visible). */
	async refreshPlaylists(): Promise<void> {
		this.#playlistsAbort?.abort();
		const ctrl = new AbortController();
		this.#playlistsAbort = ctrl;
		try {
			const items = await fetchRecentPlaylists(PLAYLIST_LIMIT, ctrl.signal);
			if (ctrl.signal.aborted) return;
			this.playlists = items;
			this.playlistsStatus = 'ready';
		} catch {
			// Optional section: keep the last good list; with none, it stays hidden.
			if (!ctrl.signal.aborted && this.playlistsStatus !== 'ready') this.playlistsStatus = 'error';
		}
	}

	/** The server's "now" (ms since epoch) for a client instant, skew-corrected. */
	serverNow(clientNow: number = Date.now()): number {
		return clientNow - this.skewMs;
	}

	#accept(next: NowPlaying | null): void {
		const prev = this.nowPlaying;
		const prevId = prev?.track?.id ?? null;
		const nextId = next?.track?.id ?? null;

		const fetched = next ? Date.parse(next.fetched_at) : NaN;
		if (!Number.isNaN(fetched)) {
			this.#skewSamples.push(Date.now() - fetched);
			if (this.#skewSamples.length > 20) this.#skewSamples.shift();
			this.skewMs = Math.min(...this.#skewSamples);
		}
		this.nowPlaying = next;
		this.nowStatus = 'ready';
		this.nowError = null;
		this.lastUpdated = new Date();

		if (this.#running && this.historyStatus !== 'loading' && prevId !== nextId) {
			// The tracker records the play right around the change; refresh twice.
			void this.refreshHistory();
			this.#later(() => void this.refreshHistory(), 4_000);
		}
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
			this.#startPolling();
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
			this.connection = 'live';
			this.#stopPolling();
			this.#armStale();
		};
		es.addEventListener('now-playing', (ev) => {
			this.connection = 'live';
			this.#armStale();
			try {
				const data: unknown = JSON.parse((ev as MessageEvent<string>).data);
				this.#accept(isNowPlaying(data) ? data : null);
			} catch {
				// ignore a malformed frame; the next one will follow shortly
			}
		});
		// Take over reconnection ourselves (EventSource gives up on HTTP errors
		// and retries too eagerly otherwise) and poll while disconnected.
		es.onerror = () => this.#fail();
	}

	#fail(): void {
		this.#closeSource();
		if (this.connection !== 'polling') this.connection = 'reconnecting';
		this.#startPolling();
		void this.refreshNow();
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

	#startPolling(): void {
		if (this.#pollTimer) return;
		this.#pollTimer = setInterval(() => void this.refreshNow(), POLL_MS);
	}

	#stopPolling(): void {
		if (this.#pollTimer) clearInterval(this.#pollTimer);
		this.#pollTimer = null;
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
		this.#stopPolling();
		if (this.#historyTimer) clearInterval(this.#historyTimer);
		this.#historyTimer = null;
		for (const id of this.#timers) clearTimeout(id);
		this.#timers.clear();
		this.#historyAbort?.abort();
		this.#playlistsAbort?.abort();
	}
}

export const live = new LiveState();
