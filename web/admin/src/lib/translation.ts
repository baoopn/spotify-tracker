// Lyrics translation helpers for the admin views (pure, no runes).

import type { TrackTranslation } from './types';

/** A lyrics view: the original words or a translation. */
export type LyricsLang = 'orig' | 'en' | 'vi';

/**
 * Translations that can be shown: done, current and present. A language is
 * null only when the song is all in it, so a mixed song keeps both.
 */
export function availableLangs(t: TrackTranslation | null | undefined): ('en' | 'vi')[] {
	if (!t || t.status !== 'done' || t.stale) return [];
	return (['en', 'vi'] as const).filter((l) => t[l] != null);
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
	if (!t || t.waiting) return null;
	if (t.status === 'pending' || t.stale)
		return 'Queued; waiting for the daily or monthly cap, or a play.';
	switch (t.status) {
		case 'not_queued':
			return 'Not translated yet: songs are translated when they play.';
		case 'skipped':
			return 'Not translated: nothing to translate, or too long.';
		case 'refused':
			return 'Claude declined to translate these lyrics.';
		case 'error':
			return t.next_attempt_at
				? 'Translation failed. It will be tried again later.'
				: 'Translation failed too often and was given up.';
	}
	return null;
}

/**
 * Claude prices in US dollars per million tokens (docs/deployment.md). Add a
 * row when TRANSLATE_MODEL changes; an unknown model shows no estimate.
 */
export const MODEL_PRICES: Readonly<Record<string, { input: number; output: number }>> = {
	'claude-sonnet-5-5': { input: 2, output: 10 }
};

/** Estimated US dollars for the tokens, or null when the model has no known price. */
export function estimateCost(
	model: string | null | undefined,
	tokens: { input: number; output: number } | null | undefined
): number | null {
	const price = model ? MODEL_PRICES[model] : undefined;
	if (!price || !tokens) return null;
	return (tokens.input * price.input + tokens.output * price.output) / 1_000_000;
}

/** A cap of 0 or less means no cap (TRANSLATE_DAILY_CAP / TRANSLATE_MONTHLY_CAP). */
export function hasCap(cap: number): boolean {
	return cap > 0;
}

/** Share of a cap used, 0..1 (0 when there is no cap). */
export function capRatio(used: number, cap: number): number {
	return hasCap(cap) ? Math.max(0, Math.min(1, used / cap)) : 0;
}
