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
/** Lines kept on screen before the active one. */
export const KEEP_BEFORE = 4;

/** What the public card shows: a short rolling run, never the whole song. */
export type Rolling = { trackId: string; lines: LyricLine[]; until_ms: number | null };

type LyricsWindow = { track_id: string; until_ms: number | null; lines: readonly LyricLine[] };

/** Lines grouped by start_ms, in order. */
function groups(lines: readonly LyricLine[]): Map<number, LyricLine[]> {
	const m = new Map<number, LyricLine[]>();
	for (const l of lines) {
		const g = m.get(l.start_ms);
		if (g) g.push(l);
		else m.set(l.start_ms, [l]);
	}
	return m;
}

/**
 * Merge two runs by start_ms, sorted. Lines that share a start form a
 * group; the larger group wins (a window can start mid-group), the newer
 * one on a tie.
 */
export function mergeLines(prev: readonly LyricLine[], next: readonly LyricLine[]): LyricLine[] {
	const out = groups(prev);
	for (const [k, g] of groups(next)) {
		const old = out.get(k);
		if (!old || g.length >= old.length) out.set(k, g);
	}
	return [...out.keys()].sort((a, b) => a - b).flatMap((k) => out.get(k)!);
}

/**
 * Fold a new server window into the rolling run. A new track, or a window
 * that does not touch the kept lines (a gap ahead, or a seek back before
 * them), replaces the run; otherwise the two merge. Past lines are trimmed
 * to KEEP_BEFORE before the line playing at `posMs`.
 */
export function rollLines(
	prev: Rolling | null,
	win: LyricsWindow,
	posMs: number,
	keepBefore = KEEP_BEFORE
): Rolling {
	const first = win.lines[0]?.start_ms;
	const touches =
		prev != null &&
		prev.trackId === win.track_id &&
		prev.lines.length > 0 &&
		first != null &&
		first >= prev.lines[0].start_ms &&
		(prev.until_ms == null || first <= prev.until_ms);
	const lines = touches ? mergeLines(prev.lines, win.lines) : [...win.lines];
	const idx = currentIndex(lines, posMs);
	const from = Math.max(0, idx - keepBefore);
	return { trackId: win.track_id, lines: lines.slice(from), until_ms: win.until_ms };
}

/** Stable keys: start_ms plus the line's place in its same-start group. */
export function lineKeys(lines: readonly LyricLine[]): string[] {
	let prevStart = NaN;
	let n = 0;
	return lines.map((l) => {
		n = l.start_ms === prevStart ? n + 1 : 0;
		prevStart = l.start_ms;
		return `${l.start_ms}:${n}`;
	});
}
