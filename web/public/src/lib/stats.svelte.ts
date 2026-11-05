// Song stats: audio features of the track on show (now playing, or the last
// played one when idle) plus the 30-day play-weighted audio profile.

import { errorMessage, fetchAudioProfile, fetchTrackDetail } from './api';
import type { AudioProfile, TrackDetail } from './types';

const PROFILE_REFRESH_MS = 10 * 60_000;

export type DetailStatus = 'idle' | 'loading' | 'ready' | 'error';

class SongStatsState {
	detail = $state<TrackDetail | null>(null);
	status = $state<DetailStatus>('idle');
	error = $state<string | null>(null);
	profile = $state<AudioProfile | null>(null);

	#id: string | null = null;
	#abort: AbortController | null = null;
	#profileTimer: ReturnType<typeof setInterval> | null = null;

	/** Start the 30-day profile refresh. Returns a cleanup function. */
	start(): () => void {
		void this.refreshProfile();
		this.#profileTimer = setInterval(() => void this.refreshProfile(), PROFILE_REFRESH_MS);
		return () => {
			if (this.#profileTimer) clearInterval(this.#profileTimer);
			this.#profileTimer = null;
			this.#abort?.abort();
		};
	}

	/** Load the detail for a track id; a no-op when it is already loaded or loading. */
	load(id: string | null, force = false): void {
		if (!force && id === this.#id) return;
		this.#id = id;
		this.#abort?.abort();
		if (!id) {
			this.detail = null;
			this.status = 'idle';
			return;
		}
		const ctrl = new AbortController();
		this.#abort = ctrl;
		// Keep the previous detail on screen only if it is the same track.
		if (this.detail?.id !== id) this.status = 'loading';
		fetchTrackDetail(id, ctrl.signal)
			.then((d) => {
				if (ctrl.signal.aborted) return;
				this.detail = d;
				this.status = 'ready';
				this.error = null;
			})
			.catch((err: unknown) => {
				if (ctrl.signal.aborted) return;
				this.status = 'error';
				this.error = errorMessage(err);
			});
	}

	retry(): void {
		this.load(this.#id, true);
	}

	async refreshProfile(): Promise<void> {
		try {
			this.profile = await fetchAudioProfile('30d');
		} catch {
			// Optional overlay: keep the last value, or none.
		}
	}
}

export const songStats = new SongStatsState();
