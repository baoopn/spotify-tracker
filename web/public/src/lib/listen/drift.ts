// Smooth drift handling for listen along (pure parts tested by
// scripts/check-listen.mjs). Small gaps close by nudging playbackRate (at
// most ±5 %, inaudible); only a big gap still seeks.
//
// - Measured every MEASURE_MS while joined and playing; drift = audio - target
//   (positive: ahead), smoothed with an EMA that resets on every seek.
// - |drift| > HARD_SEEK_MS: seek. SOFT_START_MS..HARD_SEEK_MS: adjust the
//   rate, until the gap is under SOFT_STOP_MS (hysteresis), then exactly 1.
// - Never while buffering (readyState, seeking, or "waiting" without a later
//   "playing"); after such a stall the next measure seeks once.
// - Snapshot jitter: a new now-playing snapshot that moves the target by less
//   than JITTER_MS keeps the running estimate.
// - Owner actions: a new `changed_at` for the same song (Bao seeked, paused or
//   resumed) skips the jitter filter, resets the EMA and seeks at once.

import { estimatePositionMs } from '../playback.ts';
import { IDLE_MAX_AGE_MS, seekTarget, type SyncSnapshot } from './sync.ts';

export const MEASURE_MS = 250;
export const HARD_SEEK_MS = 1500;
export const SOFT_START_MS = 120;
export const SOFT_STOP_MS = 40;
export const EMA_ALPHA = 0.3;
export const RATE_SPAN = 0.05;
const RATE_DIVISOR_MS = 4000;
const RATE_STEP = 0.005;
export const RATE_MIN_INTERVAL_MS = 500;
export const JITTER_MS = 250;
const HAVE_FUTURE_DATA = 3;

/** Behind (negative drift) speeds up, ahead slows down; ±5 %, in 0.005 steps. */
export function rateFor(driftMs: number): number {
	const adj = Math.min(RATE_SPAN, Math.max(-RATE_SPAN, driftMs / RATE_DIVISOR_MS));
	return Math.round((1 - adj) / RATE_STEP) * RATE_STEP;
}

export type DriftStep = { seek: boolean; rate: number };

/** The controller state machine (no DOM). */
export class DriftState {
	ema: number | null = null;
	correcting = false;
	rate = 1;
	#rateAt = -Infinity;

	/** After any seek or track change. */
	reset(): void {
		this.ema = null;
		this.correcting = false;
	}

	/**
	 * One measure. `drift` null means nothing to follow (not joined, paused,
	 * no target): back to rate 1 at once.
	 */
	step(driftMs: number | null, buffering: boolean, nowMs: number): DriftStep {
		if (driftMs == null || buffering || !Number.isFinite(driftMs)) {
			this.reset();
			return this.#setRate(1, nowMs, true);
		}
		this.ema = this.ema == null ? driftMs : this.ema + EMA_ALPHA * (driftMs - this.ema);
		const gap = Math.abs(this.ema);
		if (gap > HARD_SEEK_MS) {
			this.reset();
			return { ...this.#setRate(1, nowMs, true), seek: true };
		}
		if (!this.correcting && gap > SOFT_START_MS) this.correcting = true;
		else if (this.correcting && gap < SOFT_STOP_MS) this.correcting = false;
		return this.#setRate(this.correcting ? rateFor(this.ema) : 1, nowMs, false);
	}

	#setRate(rate: number, nowMs: number, force: boolean): DriftStep {
		if (rate !== this.rate && (force || nowMs - this.#rateAt >= RATE_MIN_INTERVAL_MS)) {
			this.rate = rate;
			this.#rateAt = nowMs;
		}
		return { seek: false, rate: this.rate };
	}
}

/** A running target estimate: the position at `atMs`, moving while playing. */
export type TargetEstimate = {
	trackId: string;
	ms: number;
	atMs: number;
	playing: boolean;
	/** The snapshot's `changed_at` (null when unknown). */
	changedAt: string | null;
};

/**
 * Bao acted on the same song (seek, pause, resume): `changed_at` moved. Null on
 * either side (unknown, or an older API) is never an action.
 */
export function isOwnerAction(
	prev: TargetEstimate | null,
	trackId: string,
	changedAt: string | null | undefined
): boolean {
	return (
		!!prev &&
		prev.trackId === trackId &&
		prev.changedAt != null &&
		changedAt != null &&
		prev.changedAt !== changedAt
	);
}

export function estimateAt(e: TargetEstimate, nowMs: number): number {
	return e.ms + (e.playing ? Math.max(0, nowMs - e.atMs) : 0);
}

/**
 * Fold a freshly computed target into the running estimate: keep the
 * estimate when the new value is within JITTER_MS (poll jitter), take the
 * new one on a bigger jump (Bao seeked), a track change or a play/pause.
 */
export function smoothTarget(
	prev: TargetEstimate | null,
	trackId: string,
	targetMs: number,
	playing: boolean,
	nowMs: number,
	changedAt: string | null = null
): TargetEstimate {
	if (
		prev &&
		!isOwnerAction(prev, trackId, changedAt) &&
		prev.trackId === trackId &&
		prev.playing === playing &&
		Math.abs(estimateAt(prev, nowMs) - targetMs) < JITTER_MS
	) {
		return prev;
	}
	return { trackId, ms: targetMs, atMs: nowMs, playing, changedAt };
}

/**
 * Wires DriftState to the <audio>: measures every MEASURE_MS, sets the rate
 * (pitch kept), seeks on a big gap or after a stall, and filters snapshot
 * jitter for the target the player uses.
 */
export class DriftController {
	#state = new DriftState();
	#est: TargetEstimate | null = null;
	#stalled = false;
	#reseek = false;
	/** Bao acted (a new changed_at): seek on the next measure. */
	#ownerAction = false;

	/** Bao acted on this song: the next measure seeks (also for tests). */
	get seekPending(): boolean {
		return this.#ownerAction;
	}

	/** The owner's position now, jitter-filtered (null: nothing to follow). */
	target(np: SyncSnapshot | null, serverNowMs: number, durationMs: number): number | null {
		const id = np?.track?.id;
		const fetched = np ? Date.parse(np.fetched_at) : NaN;
		if (!np || !id || np.progress_ms == null || !(serverNowMs - fetched <= IDLE_MAX_AGE_MS)) {
			this.#est = null;
			return null;
		}
		const fresh = estimatePositionMs(np, serverNowMs, durationMs);
		if (fresh == null) return null;
		const changedAt = np.changed_at ?? null;
		if (isOwnerAction(this.#est, id, changedAt)) {
			this.#ownerAction = true;
			this.#state.reset();
		}
		this.#est = smoothTarget(this.#est, id, fresh, np.is_playing, serverNowMs, changedAt);
		const at = estimateAt(this.#est, serverNowMs);
		return durationMs > 0 ? Math.min(at, durationMs) : at;
	}

	/**
	 * `follow()` gives the target while joined and Bao plays (else null);
	 * `onSeek` runs after the controller moved currentTime.
	 */
	attach(el: HTMLAudioElement, follow: () => number | null, onSeek: () => void): () => void {
		const keepPitch = el as HTMLAudioElement & { webkitPreservesPitch?: boolean };
		el.preservesPitch = true;
		if ('webkitPreservesPitch' in keepPitch) keepPitch.webkitPreservesPitch = true;
		const onReset = () => this.#state.reset();
		const onWaiting = () => (this.#stalled = true);
		const onPlaying = () => {
			if (this.#stalled) this.#reseek = true; // a stall that recovered
			this.#stalled = false;
		};
		const onEmptied = () => {
			this.#stalled = false;
			this.#reseek = false;
			this.#ownerAction = false;
			this.#state.reset();
		};
		const events: [string, () => void][] = [
			['seeking', onReset],
			['waiting', onWaiting],
			['playing', onPlaying],
			['emptied', onEmptied]
		];
		for (const [t, fn] of events) el.addEventListener(t, fn);
		const timer = setInterval(() => {
			const target = el.paused ? null : follow();
			const buffering = el.readyState < HAVE_FUTURE_DATA || el.seeking || this.#stalled;
			if (target != null && (this.#ownerAction || (!buffering && this.#reseek))) {
				this.#ownerAction = false;
				this.#reseek = false;
				this.#seek(el, target, onSeek);
				return;
			}
			const r = this.#state.step(
				target == null ? null : el.currentTime * 1000 - target,
				buffering,
				Date.now()
			);
			if (el.playbackRate !== r.rate) el.playbackRate = r.rate;
			if (r.seek && target != null) this.#seek(el, target, onSeek);
		}, MEASURE_MS);
		return () => {
			clearInterval(timer);
			for (const [t, fn] of events) el.removeEventListener(t, fn);
			el.playbackRate = 1;
		};
	}

	#seek(el: HTMLAudioElement, target: number, onSeek: () => void): void {
		el.currentTime = seekTarget(target, el.duration * 1000) / 1000;
		onSeek();
	}
}
