// Synced-lyrics helpers (pure, no runes).

import type { LyricLine, NowLyrics } from './types';

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

/**
 * The translations that can be shown: done and present. A language is null
 * only when the song is all in it, so a mixed song keeps both, even its main
 * language.
 */
export function availableLangs(t: Translation | null | undefined): ('en' | 'vi')[] {
	if (!t || t.status !== 'done') return [];
	return (['en', 'vi'] as const).filter((l) => t[l] != null);
}

/** The language to show: the preference when the song has it, else the original. */
export function pickLang(pref: LyricsLang, available: readonly ('en' | 'vi')[]): LyricsLang {
	return pref !== 'orig' && available.includes(pref) ? pref : 'orig';
}

/**
 * A translated title or album name in the language shown, or null (the
 * original, no translation, or the name stays as written).
 */
export function nameIn(
	name: { en: string | null; vi: string | null } | null | undefined,
	lang: LyricsLang
): string | null {
	if (!name || lang === 'orig') return null;
	return name[lang]?.trim() || null;
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

/**
 * Where the lyrics of the track on screen stand:
 * - loading: no answer for this track yet (first fetch, or the server still
 *   answers for the previous song and will be asked again)
 * - looking: the server is still looking them up
 * - readable: synced lines or plain text to show
 * - instrumental: no words
 * - failed: the request failed and no retry is running
 * - none: no lyrics for this song (or no track)
 */
export type LyricsState = 'loading' | 'looking' | 'readable' | 'instrumental' | 'failed' | 'none';

/** Lyrics data the page can render: synced lines, else plain text. */
export function lyricsView(d: NowLyrics): { lines: NowLyrics['lines']; plain: string | null } {
	const lines = d.status === 'found' && d.synced ? d.lines : [];
	const plain = d.status === 'found' && !lines.length && d.plain ? d.plain : null;
	return { lines, plain };
}

/**
 * One answer for the hero and the lyrics panel, so they never disagree.
 * `data` is the query's data: undefined before the first answer, null when
 * the server has no lyrics. Data for another track counts as loading.
 */
export function lyricsState(
	data: NowLyrics | null | undefined,
	q: { isPending: boolean; isFetching: boolean; isError: boolean },
	trackId: string | null
): LyricsState {
	if (!trackId) return 'none';
	if (data === null) return 'none';
	if (data && data.track_id === trackId) {
		if (data.status === 'pending') return 'looking';
		if (data.status === 'instrumental') return 'instrumental';
		const v = lyricsView(data);
		return v.lines.length || v.plain != null ? 'readable' : 'none';
	}
	// Nothing for this track yet: wait, unless the request failed for good.
	return q.isError && !q.isFetching ? 'failed' : 'loading';
}

/**
 * The link of a lyrics credit: only an https URL. Lyrics the owner added by
 * hand have none (`url: null`), so the credit is plain text.
 */
export function creditHref(credit: NowLyrics['credit']): string | null {
	return credit.url && /^https:\/\//i.test(credit.url) ? credit.url : null;
}
