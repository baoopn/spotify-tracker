// Guess the CJK language of a lyric line or title from its script, so the
// element can carry a lang attribute: screen readers read it right, and the
// :lang() font stacks (app.css) put the matching Noto face first. Han glyphs
// differ by region (直, 骨 and 海 are drawn one way in Japan, another in
// China), so a Japanese line must not fall to the Chinese face.

export type CjkLang = 'ja' | 'ko' | 'zh-Hans' | 'zh-Hant';

// Hiragana, katakana (and phonetic extensions, half-width forms, kana supplements).
const KANA = /[\u3040-\u30ff\u31f0-\u31ff\uff66-\uff9f\u{1b000}-\u{1b16f}]/u;
// Hangul jamo, compatibility jamo, extended jamo, syllables, half-width forms.
const HANGUL = /[\u1100-\u11ff\u3130-\u318f\ua960-\ua97f\uac00-\ud7af\ud7b0-\ud7ff\uffa0-\uffdc]/u;
// CJK ideographs: Ext A, URO, compatibility, and Ext B and later (planes 2-3).
const HAN = /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\u{20000}-\u{323af}]/u;

// Common characters written differently in the two scripts; a rough count is
// enough to tell a Traditional lyric from a Simplified one.
const HANT = new Set(
	'們這個說來時會為國過對學見還沒發點開關體從問電話讓愛夢聽歲願顆經東樂風雲聲飛憶處無與誰淚憐離遠邊總'
);
const HANS = new Set(
	'们这个说来时会为国过对学见还没发点开关体从问电话让爱梦听岁愿颗经东乐风云声飞忆处无与谁泪怜离远边总'
);

/** The CJK language a string is written in, or undefined for other scripts. */
export function scriptLang(text: string | null | undefined): CjkLang | undefined {
	if (!text) return undefined;
	if (KANA.test(text)) return 'ja';
	if (HANGUL.test(text)) return 'ko';
	if (!HAN.test(text)) return undefined;
	let hant = 0;
	let hans = 0;
	for (const ch of text) {
		if (HANT.has(ch)) hant++;
		else if (HANS.has(ch)) hans++;
	}
	return hant > hans ? 'zh-Hant' : 'zh-Hans';
}
