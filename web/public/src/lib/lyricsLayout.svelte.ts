// How the now-playing hero is laid out: the standard card, or a lyrics focus
// view where the lyrics take the main area. Kept across songs and visits; a
// song without lyrics shows the standard card without changing the choice.

export type LyricsLayout = 'standard' | 'lyrics';

const KEY = 'st-public:lyrics-layout';

function load(): LyricsLayout {
	try {
		return localStorage.getItem(KEY) === 'lyrics' ? 'lyrics' : 'standard';
	} catch {
		return 'standard';
	}
}

class LyricsLayoutState {
	// Read once at start (browser only), never inside a reactive read: writing
	// state from a getter would fail when first read inside a $derived.
	#value = $state<LyricsLayout>(typeof window === 'undefined' ? 'standard' : load());

	get value(): LyricsLayout {
		return this.#value;
	}

	set value(v: LyricsLayout) {
		this.#value = v;
		try {
			localStorage.setItem(KEY, v);
		} catch {
			// storage unavailable: keep the in-memory choice
		}
	}
}

export const lyricsLayout = new LyricsLayoutState();
