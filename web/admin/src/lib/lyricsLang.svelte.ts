// Which lyrics translation the admin shows (original, English or
// Vietnamese): one saved preference for the overview card and the track page.

import type { LyricsLang } from './translation';

const KEY = 'st-admin:lyrics-lang';

function load(): LyricsLang {
	try {
		const v = localStorage.getItem(KEY);
		return v === 'en' || v === 'vi' ? v : 'orig';
	} catch {
		return 'orig';
	}
}

class LyricsLangState {
	#value = $state<LyricsLang>('orig');
	#loaded = false;

	get value(): LyricsLang {
		if (!this.#loaded && typeof window !== 'undefined') {
			this.#loaded = true;
			this.#value = load();
		}
		return this.#value;
	}

	set value(v: LyricsLang) {
		this.#loaded = true;
		this.#value = v;
		try {
			localStorage.setItem(KEY, v);
		} catch {
			// storage unavailable: keep the in-memory choice
		}
	}
}

export const lyricsLang = new LyricsLangState();
