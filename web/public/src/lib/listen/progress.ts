// Listen-along history: how much of the song the listener actually heard.
// The pure helpers are tested by scripts/check-listen.mjs; ProgressTracker
// wires them to the <audio> element and reports to POST /listen/progress.

/** Report the heard time this often while the audio plays. */
export const FLUSH_MS = 15_000;
/** The server takes at most this much per report. */
export const MAX_REPORT_MS = 20_000;
/** A currentTime jump larger than this between two timeupdates is a seek, not playback. */
export const SEEK_JUMP_MS = 2000;

/** Played ms between two currentTime readings (seconds); 0 for a seek or a step back. */
export function playedDelta(prevSec: number | null, nowSec: number): number {
	if (prevSec == null || !Number.isFinite(prevSec) || !Number.isFinite(nowSec)) return 0;
	const ms = (nowSec - prevSec) * 1000;
	return ms > 0 && ms <= SEEK_JUMP_MS ? ms : 0;
}

/** Whole-ms reports of at most MAX_REPORT_MS each, plus the sub-ms remainder kept for later. */
export function chunkReport(pendingMs: number): { chunks: number[]; rest: number } {
	if (!Number.isFinite(pendingMs) || pendingMs < 1) {
		return { chunks: [], rest: Math.max(0, pendingMs || 0) };
	}
	let whole = Math.floor(pendingMs);
	const rest = pendingMs - whole;
	const chunks: number[] = [];
	while (whole > 0) {
		const c = Math.min(MAX_REPORT_MS, whole);
		chunks.push(c);
		whole -= c;
	}
	return { chunks, rest };
}

export type SendProgress = (trackId: string, heardMs: number, keepalive: boolean) => Promise<void>;

/**
 * Sums the audio time actually played for one track at a time and flushes it
 * every FLUSH_MS, on pause, on page hide, and when told to (track change,
 * leaving). `counting()` returns the track id while time counts (joined and
 * in sync), else null. Muted audio still counts.
 */
export class ProgressTracker {
	#send: SendProgress;
	#trackId: string | null = null;
	#pending = 0;
	#last: number | null = null;

	constructor(send: SendProgress) {
		this.#send = send;
	}

	attach(el: HTMLAudioElement, counting: () => string | null): () => void {
		const onTime = () => {
			const id = counting();
			const t = el.currentTime;
			if (!id || el.paused || el.seeking || el.ended) {
				this.#last = null;
				return;
			}
			if (id !== this.#trackId) {
				this.flush();
				this.#trackId = id;
				this.#last = null;
			}
			this.#pending += playedDelta(this.#last, t);
			this.#last = t;
		};
		const onBreak = () => {
			this.#last = null;
			this.flush();
		};
		const onSeeking = () => (this.#last = null);
		const onHide = () => this.flush(true);
		const onVisibility = () => {
			if (document.visibilityState === 'hidden') this.flush(true);
		};
		el.addEventListener('timeupdate', onTime);
		el.addEventListener('seeking', onSeeking);
		el.addEventListener('pause', onBreak);
		el.addEventListener('ended', onBreak);
		window.addEventListener('pagehide', onHide);
		document.addEventListener('visibilitychange', onVisibility);
		const timer = setInterval(() => this.flush(), FLUSH_MS);
		return () => {
			clearInterval(timer);
			el.removeEventListener('timeupdate', onTime);
			el.removeEventListener('seeking', onSeeking);
			el.removeEventListener('pause', onBreak);
			el.removeEventListener('ended', onBreak);
			window.removeEventListener('pagehide', onHide);
			document.removeEventListener('visibilitychange', onVisibility);
			this.flush(true);
		};
	}

	/** Send what was heard so far (errors are dropped: history is best effort). */
	flush(keepalive = false): void {
		const id = this.#trackId;
		const { chunks, rest } = chunkReport(this.#pending);
		this.#pending = rest;
		if (!id) return;
		for (const ms of chunks) void this.#send(id, ms, keepalive).catch(() => {});
	}
}
