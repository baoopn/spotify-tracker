// Synced-lyrics helpers (pure, no runes).

import type { LyricLine } from './types';

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
