// LRC ("[mm:ss.xx] line") parsing for the admin now-playing lyrics.
// Mirrors internal/lrc on the server: several time tags per line, [offset:],
// metadata tags ignored, blank timed lines kept as instrumental gaps, an invalid
// time tag (seconds >= 60, minutes > 999) consumed and dropped, [offset:] in any
// case, U+FEFF removed. Keep the fixtures in scripts/check-lrc.mjs and
// internal/lrc/lrc_test.go in sync.

export type LrcLine = { startMs: number; text: string };

const TIME_TAG = /^\[(\d+):(\d{1,2})(?:[.:](\d{1,3}))?\]/;
const MAX_MINUTES = 999;
const OFFSET_TAG = /^\s*\[offset:\s*([+-]?\d+)\s*\]\s*$/i;
const WORD_TAG = /<\d+:\d{1,2}(?:[.:]\d{1,3})?>/g;

function fractionMs(frac: string | undefined): number {
	if (!frac) return 0;
	// Scale by digit count: ".5" = 500 ms, ".05" = 50 ms, ".005" = 5 ms.
	return Math.round(Number(frac) * 10 ** (3 - frac.length));
}

/** Parses LRC text into timed lines sorted by start time (file order on ties). */
export function parseLrc(text: string | null | undefined): LrcLine[] {
	if (!text) return [];
	const raw = text
		.replace(/\uFEFF/g, '')
		.split('\n')
		.map((l) => l.replace(/\r$/, ''));

	let offset = 0;
	for (const l of raw) {
		const m = OFFSET_TAG.exec(l);
		if (m) offset = Number(m[1]);
	}

	const out: { startMs: number; text: string; order: number }[] = [];
	let order = 0;
	for (const l of raw) {
		let rest = l.trimStart();
		const times: number[] = [];
		for (;;) {
			const m = TIME_TAG.exec(rest);
			if (!m) break;
			rest = rest.slice(m[0].length);
			const minutes = Number(m[1]);
			const seconds = Number(m[2]);
			if (minutes > MAX_MINUTES || seconds >= 60) continue; // invalid tag: dropped
			const ms = minutes * 60_000 + seconds * 1000 + fractionMs(m[3]);
			times.push(Math.max(0, ms - offset));
		}
		if (times.length === 0) continue; // metadata or untimed line
		const lineText = rest.replace(WORD_TAG, '').trim();
		for (const t of times) out.push({ startMs: t, text: lineText, order: order++ });
	}
	out.sort((a, b) => a.startMs - b.startMs || a.order - b.order);
	return out.map(({ startMs, text }) => ({ startMs, text }));
}

/** Index of the last line that has started at posMs, or -1 before the first line. */
export function activeIndex(lines: readonly LrcLine[], posMs: number): number {
	let lo = 0;
	let hi = lines.length - 1;
	let ans = -1;
	while (lo <= hi) {
		const mid = (lo + hi) >> 1;
		if (lines[mid].startMs <= posMs) {
			ans = mid;
			lo = mid + 1;
		} else {
			hi = mid - 1;
		}
	}
	return ans;
}

// ---------- manual lyrics (mirrors internal/lrc/validate.go) ----------

/** One problem in LRC text; `line` is 1-based (0 = the text as a whole). */
export type LrcLineError = { line: number; message: string };

/** Errors returned at most, timed lines allowed, and the end-of-song slack (as on the server). */
export const LRC_MAX_ERRORS = 50;
export const LRC_MAX_LINES = 2000;
export const LRC_SLACK_MS = 5000;

const BOM = String.fromCharCode(0xfeff);
const LEAD_TAG = /^\[([^\]]*)\]/;
const META_TAG = /^\[[A-Za-z#][\w-]*:[^\]]*\]$/;
const OFFSET_LINE = /^\[offset:\s*([+-]?\d+)\s*\]$/i;

/** Removes byte order marks and NUL bytes, makes line breaks "\n", cuts trailing spaces and blank edges. */
export function normalizeLyrics(text: string): string {
	return text
		.split(BOM)
		.join('')
		.replace(/\0/g, '')
		.replace(/\r\n?/g, '\n')
		.split('\n')
		.map((l) => l.replace(/[ \t]+$/, ''))
		.join('\n')
		.replace(/^\n+|\n+$/g, '');
}

/** m:ss.xx, like the server's messages. */
export function formatLrcTime(ms: number): string {
	const m = Math.floor(ms / 60_000);
	const s = Math.floor(ms / 1000) % 60;
	const cs = Math.floor((ms % 1000) / 10);
	return `${m}:${String(s).padStart(2, '0')}.${String(cs).padStart(2, '0')}`;
}

/** One source line of LRC text as the editor preview shows it. */
export type LrcSourceLine = {
	/** 1-based line number. */
	line: number;
	kind: 'timed' | 'meta' | 'blank' | 'bad';
	starts: number[];
	text: string;
};

/** Splits LRC text into source lines with their time tags (invalid tags make the line `bad`). */
export function lrcSourceLines(text: string): LrcSourceLine[] {
	return text.split('\n').map((raw, i) => {
		let s = raw.trim();
		const line = i + 1;
		if (s === '') return { line, kind: 'blank', starts: [], text: '' };
		if (OFFSET_LINE.test(s)) return { line, kind: 'meta', starts: [], text: s };
		const starts: number[] = [];
		let bad = false;
		for (;;) {
			const lead = LEAD_TAG.exec(s);
			if (!lead) break;
			const m = TIME_TAG.exec(s);
			if (m && m[0].length === lead[0].length) {
				const minutes = Number(m[1]);
				const seconds = Number(m[2]);
				if (minutes > MAX_MINUTES || seconds >= 60) bad = true;
				else starts.push(minutes * 60_000 + seconds * 1000 + fractionMs(m[3]));
				s = s.slice(m[0].length);
				continue;
			}
			if (/^\d/.test(lead[1])) bad = true;
			break;
		}
		if (bad) return { line, kind: 'bad', starts, text: s.trim() };
		if (starts.length === 0 && META_TAG.test(s)) return { line, kind: 'meta', starts, text: s };
		if (starts.length === 0) return { line, kind: 'bad', starts, text: s };
		return { line, kind: 'timed', starts, text: s.replace(WORD_TAG, '').trim() };
	});
}

/**
 * Checks hand-written LRC text (normalized) line by line, with the server's
 * rules and messages: valid time tags, lines with one tag in time order,
 * nothing past the end of the song (durationMs + slack, after [offset:]),
 * at least one and at most LRC_MAX_LINES timed lines.
 */
export function validateLrc(text: string, durationMs = 0, slackMs = LRC_SLACK_MS): LrcLineError[] {
	const errs: LrcLineError[] = [];
	const add = (line: number, message: string) => {
		if (errs.length < LRC_MAX_ERRORS) errs.push({ line, message });
	};
	let offset = 0;
	for (const raw of text.split('\n')) {
		const m = OFFSET_LINE.exec(raw.trim());
		if (m) offset = Number(m[1]);
	}
	let timed = 0;
	let prev = -1;
	let prevLine = 0;
	for (const raw of text.split('\n').map((l, i) => ({ l, n: i + 1 }))) {
		let s = raw.l.trim();
		const n = raw.n;
		if (s === '' || OFFSET_LINE.test(s)) continue;
		const starts: number[] = [];
		let bad = false;
		for (;;) {
			const lead = LEAD_TAG.exec(s);
			if (!lead) break;
			const m = TIME_TAG.exec(s);
			if (m && m[0].length === lead[0].length) {
				const minutes = Number(m[1]);
				const seconds = Number(m[2]);
				if (minutes > MAX_MINUTES || seconds >= 60) {
					add(
						n,
						`invalid timestamp [${lead[1]}]: seconds must be below 60 and minutes at most ${MAX_MINUTES}`
					);
					bad = true;
				} else {
					starts.push(minutes * 60_000 + seconds * 1000 + fractionMs(m[3]));
				}
				s = s.slice(m[0].length);
				continue;
			}
			if (/^\d/.test(lead[1])) {
				add(n, `invalid timestamp [${lead[1]}]: use [mm:ss.xx]`);
				bad = true;
			}
			break;
		}
		if (bad) continue;
		if (starts.length === 0) {
			if (!META_TAG.test(s)) add(n, 'no timestamp: start the line with [mm:ss.xx], or remove it');
			continue;
		}
		timed += starts.length;
		if (starts.length === 1) {
			if (prev >= 0 && starts[0] < prev) {
				add(
					n,
					`out of order: ${formatLrcTime(starts[0])} comes before ${formatLrcTime(prev)} on line ${prevLine}`
				);
			} else {
				prev = starts[0];
				prevLine = n;
			}
		}
		if (durationMs > 0) {
			for (const ms of starts) {
				const at = Math.max(0, ms - offset);
				if (at > durationMs + slackMs) {
					add(n, `${formatLrcTime(at)} is past the end of the song (${formatLrcTime(durationMs)})`);
					break;
				}
			}
		}
	}
	if (timed > LRC_MAX_LINES) add(0, `too many timed lines (${timed}; at most ${LRC_MAX_LINES})`);
	if (timed === 0 && errs.length === 0)
		add(0, 'no timed lines: synced lyrics need [mm:ss.xx] timestamps');
	return errs;
}

/** Text with no lyrics: only symbols ("♪", "...") and the word "instrumental". */
export function markerOnly(s: string): boolean {
	return (
		s
			.toLowerCase()
			.replace(/[^\p{L}\p{Nd}]/gu, '')
			.replaceAll('instrumental', '') === ''
	);
}

/**
 * Plain lyrics derived from LRC text, as the server derives them: metadata
 * dropped, time and word tags removed, word-less lines become one stanza break.
 */
export function plainFromLrc(synced: string): string {
	const out: string[] = [];
	let blank = false;
	for (const raw of synced.split('\n')) {
		let s = raw.replace(/\r$/, '').trim();
		if (OFFSET_LINE.test(s) || META_TAG.test(s)) continue;
		for (;;) {
			const m = TIME_TAG.exec(s);
			if (!m) break;
			s = s.slice(m[0].length).trim();
		}
		s = s.replace(WORD_TAG, '').trim();
		if (markerOnly(s)) {
			blank = out.length > 0;
			continue;
		}
		if (blank) {
			out.push('');
			blank = false;
		}
		out.push(s);
	}
	return out.join('\n');
}
