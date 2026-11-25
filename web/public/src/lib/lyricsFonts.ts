// CJK faces for --font-lyrics, loaded only when the text on screen needs them.
// Their @font-face CSS is large (about 30 KB gzip per family, hundreds of
// unicode-range slices), so it stays out of the main stylesheet. Before a
// family arrives, the system font draws those characters.

const KANA = /[぀-ヿㇰ-ㇿｦ-ﾟ]/;
const HANGUL = /[ᄀ-ᇿ㄰-㆏ꥠ-꥿가-힯ힰ-퟿]/;
const HAN = /[㐀-䶿一-鿿豈-﫿]/;

const loaders = {
	sc: () => import('@fontsource-variable/noto-sans-sc'),
	tc: () => import('@fontsource-variable/noto-sans-tc'),
	jp: () => import('@fontsource-variable/noto-sans-jp'),
	kr: () => import('@fontsource-variable/noto-sans-kr')
} as const;

const started = new Set<keyof typeof loaders>();

function load(key: keyof typeof loaders) {
	if (started.has(key)) return;
	started.add(key);
	loaders[key]().catch(() => started.delete(key)); // try again next time
}

/** Load the CJK faces that these strings need (browser only, once each). */
export function loadLyricsFonts(...texts: (string | null | undefined)[]) {
	if (typeof document === 'undefined') return;
	const s = texts.filter(Boolean).join('\n');
	if (!s) return;
	// Japanese: the JP face also has the kanji, drawn the Japanese way.
	if (KANA.test(s)) load('jp');
	else if (HAN.test(s)) {
		load('sc');
		load('tc');
	}
	if (HANGUL.test(s)) load('kr');
}
