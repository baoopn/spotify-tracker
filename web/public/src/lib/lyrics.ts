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

/** A lyrics view: the original words or a translation. */
export type LyricsLang = 'orig' | 'en' | 'vi';

type Translation = {
	status: string;
	source_language: string | null;
	en: { lines: string[]; plain: string | null } | null;
	vi: { lines: string[]; plain: string | null } | null;
};

/** The translations that can be shown: done, not the song's own language. */
export function availableLangs(t: Translation | null | undefined): ('en' | 'vi')[] {
	if (!t || t.status !== 'done') return [];
	return (['en', 'vi'] as const).filter((l) => t[l] != null && t.source_language !== l);
}

/** The language to show: the preference when the song has it, else the original. */
export function pickLang(pref: LyricsLang, available: readonly ('en' | 'vi')[]): LyricsLang {
	return pref !== 'orig' && available.includes(pref) ? pref : 'orig';
}

/**
 * Plain lyrics with their translation, line by line when both have the same
 * number of lines (null otherwise: show the translation below instead).
 */
export function interleave(
	original: string,
	translation: string
): { text: string; tr: string }[] | null {
	const a = original.split('\n');
	const b = translation.split('\n');
	if (a.length !== b.length) return null;
	return a.map((text, i) => ({ text, tr: b[i] }));
}
