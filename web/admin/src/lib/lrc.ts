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
