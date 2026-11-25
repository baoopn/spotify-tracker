// CJK faces for --font-lyrics, loaded only when the text on screen needs them.
// Their @font-face CSS is large (about 30 KB gzip per family, hundreds of
// unicode-range slices), so it stays out of the main stylesheet. Before a
// family arrives, the system font draws those characters.
import { scriptLang, type CjkLang } from './scriptLang.js';

const loaders: Record<CjkLang, () => Promise<unknown>> = {
	ja: () => import('@fontsource-variable/noto-sans-jp'),
	ko: () => import('@fontsource-variable/noto-sans-kr'),
	'zh-Hans': () => import('@fontsource-variable/noto-sans-sc'),
	'zh-Hant': () => import('@fontsource-variable/noto-sans-tc')
};

const started = new Set<CjkLang>();

function load(lang: CjkLang) {
	if (started.has(lang)) return;
	started.add(lang);
	loaders[lang]().catch(() => started.delete(lang)); // try again next time
}

/**
 * Load the face for each string's language (browser only, once each). Pass
 * lines one by one: each line gets its own lang, so a song mixing Chinese and
 * Korean lines loads both faces.
 */
export function loadLyricsFonts(...texts: (string | null | undefined)[]) {
	if (typeof document === 'undefined') return;
	for (const t of texts) {
		const lang = scriptLang(t);
		if (lang) load(lang);
	}
}
