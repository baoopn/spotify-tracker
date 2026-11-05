import { createSubscriber } from 'svelte/reactivity';
import { NOW_PLAYING_STREAM } from './api.js';
import type { NowPlaying } from './types.js';

export type Snapshot = { np: NowPlaying | null; at: number };

/** Pair a now-playing payload with the instant its progress refers to. */
export function snapshot(np: NowPlaying | null): Snapshot {
	const now = Date.now();
	const fetched = np ? Date.parse(np.fetched_at) : NaN;
	// Interpolate from when Spotify was polled; fall back to "now" on clock skew.
	const at = Number.isFinite(fetched) && fetched <= now ? fetched : now;
	return { np, at };
}

/**
 * Live now-playing via Server-Sent Events (`event: now-playing`). The
 * EventSource is only open while something in the UI reads `latest`/`status`.
 */
export class NowPlayingStream {
	#latest = $state<Snapshot | null>(null);
	#status = $state<'connecting' | 'live' | 'reconnecting'>('connecting');
	#subscribe: () => void;

	constructor(url = NOW_PLAYING_STREAM) {
		this.#subscribe = createSubscriber(() => {
			const es = new EventSource(url);
			es.addEventListener('open', () => (this.#status = 'live'));
			es.addEventListener('error', () => (this.#status = 'reconnecting'));
			es.addEventListener('now-playing', (e) => {
				try {
					this.#latest = snapshot(
						JSON.parse((e as MessageEvent<string>).data) as NowPlaying | null
					);
					this.#status = 'live';
				} catch {
					// ignore malformed frames
				}
			});
			return () => {
				es.close();
				this.#status = 'connecting';
			};
		});
	}

	/** Most recent pushed snapshot, or null before the first event. */
	get latest(): Snapshot | null {
		this.#subscribe();
		return this.#latest;
	}

	get status() {
		this.#subscribe();
		return this.#status;
	}
}
