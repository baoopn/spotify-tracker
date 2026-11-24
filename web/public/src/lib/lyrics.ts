// Synced-lyrics window helpers (pure, no runes).

import type { LyricLine } from './types';

/** Refetch this long before the window runs out, so the next lines are ready. */
export const PREFETCH_MS = 4_000;
/** A position this far before the window start means a seek back. */
export const SEEK_BACK_MS = 1_500;

/** Index of the line playing at `posMs` (last line started at or before it), or -1. */
export function currentIndex(lines: readonly LyricLine[], posMs: number): number {
	let lo = 0;
	let hi = lines.length - 1;
	let found = -1;
	while (lo <= hi) {
		const mid = (lo + hi) >> 1;
		if (lines[mid].start_ms <= posMs) {
			found = mid;
			lo = mid + 1;
		} else {
			hi = mid - 1;
		}
	}
	return found;
}

/**
 * True when the window no longer covers `posMs`: it is about to run out
 * (more lines follow), or playback jumped back before its start.
 */
export function needsRefetch(
	data: { from_ms: number; until_ms: number | null },
	posMs: number
): boolean {
	if (data.until_ms != null && posMs >= data.until_ms - PREFETCH_MS) return true;
	return posMs < data.from_ms - SEEK_BACK_MS;
}

/** At most one effect-driven refetch per this many ms. */
export const REFETCH_THROTTLE_MS = 3_000;
/** A window that came back unchanged is asked again only after this long. */
export const SAME_WINDOW_RETRY_MS = 15_000;

type Window = { track_id: string; from_ms: number; until_ms: number | null };

/** Identifies one window: a refetch that returns the same key got nothing new. */
export function windowKey(data: Window): string {
	return `${data.track_id}:${data.from_ms}:${data.until_ms ?? ''}`;
}

/** What the last effect-driven refetch was for. */
export type RefetchMemo = { key: string | null; at: number };

/**
 * Whether the component should refetch the window now. Never while the
 * query is failing (TanStack retries with backoff) or the tab is hidden;
 * once per distinct window (a window that comes back unchanged is asked
 * again only after SAME_WINDOW_RETRY_MS), and at most every
 * REFETCH_THROTTLE_MS.
 */
export function shouldRefetch(
	data: Window,
	posMs: number,
	state: { isError: boolean; hidden: boolean; nowMs: number },
	memo: RefetchMemo
): boolean {
	if (state.isError || state.hidden) return false;
	if (!needsRefetch(data, posMs)) return false;
	const since = state.nowMs - memo.at;
	if (since < REFETCH_THROTTLE_MS) return false;
	return memo.key !== windowKey(data) || since >= SAME_WINDOW_RETRY_MS;
}

/** Most lines kept per track while windows pile up on the client. */
export const MAX_LINES = 500;

/**
 * Merge a new window into the lines seen so far for the same track: keyed
 * by start_ms, sorted, the newer window winning on the same start. Keeps
 * at most MAX_LINES, dropping the earliest first.
 */
export function mergeLines(prev: readonly LyricLine[], next: readonly LyricLine[]): LyricLine[] {
	const byStart = new Map<number, LyricLine[]>();
	for (const l of prev) byStart.set(l.start_ms, [...(byStart.get(l.start_ms) ?? []), l]);
	// A new window replaces every line it has at that start (keeps duplicates in order).
	const fresh = new Map<number, LyricLine[]>();
	for (const l of next) fresh.set(l.start_ms, [...(fresh.get(l.start_ms) ?? []), l]);
	for (const [k, v] of fresh) byStart.set(k, v);
	const out = [...byStart.keys()].sort((a, b) => a - b).flatMap((k) => byStart.get(k)!);
	return out.length > MAX_LINES ? out.slice(out.length - MAX_LINES) : out;
}
