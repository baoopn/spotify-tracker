// Which lyrics translation to show (original, English or Vietnamese). One
// preference for both layouts (standard and lyrics focus), kept across songs and
// visits; a song that lacks the chosen language shows the original.

import type { LyricsLang } from './lyrics';

const KEY = 'st-public:lyrics-lang';

function load(): LyricsLang {
	try {
		const v = localStorage.getItem(KEY);
		return v === 'en' || v === 'vi' ? v : 'orig';
	} catch {
		return 'orig';
	}
}

class LyricsLangState {
	// Read once at start (browser only), never inside a reactive read: writing
	// state from a getter would fail when first read inside a $derived.
	#value = $state<LyricsLang>(typeof window === 'undefined' ? 'orig' : load());

	get value(): LyricsLang {
		return this.#value;
	}

	set value(v: LyricsLang) {
		this.#value = v;
		try {
			localStorage.setItem(KEY, v);
		} catch {
			// storage unavailable: keep the in-memory choice
		}
	}
}

export const lyricsLang = new LyricsLangState();
