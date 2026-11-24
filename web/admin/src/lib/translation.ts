// Lyrics translation helpers for the admin views (pure, no runes).

import type { TrackTranslation } from './types';

/** A lyrics view: the original words or a translation. */
export type LyricsLang = 'orig' | 'en' | 'vi';

/** Translations that can be shown: done, current, not the song's own language. */
export function availableLangs(t: TrackTranslation | null | undefined): ('en' | 'vi')[] {
	if (!t || t.status !== 'done' || t.stale) return [];
	return (['en', 'vi'] as const).filter((l) => t[l] != null && t.source_language !== l);
}

/** The language to show: the preference when the song has it, else the original. */
export function pickLang(pref: LyricsLang, available: readonly ('en' | 'vi')[]): LyricsLang {
	return pref !== 'orig' && available.includes(pref) ? pref : 'orig';
}

/**
 * The translated lines of synced lyrics, one per parsed LRC line, or null
 * when the counts differ (the parser and the stored translation disagree).
 */
export function translatedLines(text: string | null, lineCount: number): string[] | null {
	if (text == null || lineCount === 0) return null;
	const out = text.split('\n');
	return out.length === lineCount ? out : null;
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

/** One line on why there is no translation to show, or null. */
export function translationNote(t: TrackTranslation | null | undefined): string | null {
	if (!t) return null;
	if (t.stale || t.status === 'pending') return null;
	switch (t.status) {
		case 'skipped':
			return 'Not translated: nothing to translate, or too long.';
		case 'refused':
			return 'Claude declined to translate these lyrics.';
		case 'error':
			return t.next_attempt_at
				? 'Translation failed. It will be tried again.'
				: 'Translation failed too often and was given up.';
	}
	return null;
}
