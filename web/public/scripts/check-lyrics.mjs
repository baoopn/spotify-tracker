// Now-playing lyrics helper checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import { availableLangs, currentIndex, interleave, lineKeys, pickLang } from '../src/lib/lyrics.ts';
import { estimatePositionMs } from '../src/lib/playback.ts';

/** @param {number[]} starts */
const lines = (starts) => starts.map((start_ms, i) => ({ start_ms, text: `line ${i}` }));

// currentIndex: last line started at or before the position, -1 before the first.
const L = lines([1000, 2000, 2000, 5000]);
assert.equal(currentIndex([], 0), -1);
assert.equal(currentIndex(L, 0), -1);
assert.equal(currentIndex(L, 999), -1);
assert.equal(currentIndex(L, 1000), 0);
assert.equal(currentIndex(L, 1999), 0);
assert.equal(currentIndex(L, 2000), 2, 'ties: the later line in order wins');
assert.equal(currentIndex(L, 4999), 2);
assert.equal(currentIndex(L, 5000), 3);
assert.equal(currentIndex(L, 9e9), 3);
const many = lines(Array.from({ length: 101 }, (_, i) => i * 100));
for (let p = -50; p < 10_200; p += 37) {
	const want = p < 0 ? -1 : Math.min(100, Math.floor(p / 100));
	assert.equal(currentIndex(many, p), want, `binary search at ${p}`);
}

// estimatePositionMs: progress plus elapsed while playing, clamped.
const at = '2026-11-24T10:00:00.000Z';
const t0 = Date.parse(at);
/**
 * @param {boolean} is_playing
 * @param {number | null} progress_ms
 */
const np = (is_playing, progress_ms) => ({ is_playing, progress_ms, fetched_at: at });
assert.equal(estimatePositionMs(null, t0, 1000), null);
assert.equal(estimatePositionMs(np(true, null), t0, 1000), null);
assert.equal(estimatePositionMs(np(true, 5000), t0 + 2500, 200_000), 7500);
assert.equal(estimatePositionMs(np(false, 5000), t0 + 2500, 200_000), 5000, 'paused: no elapsed');
assert.equal(estimatePositionMs(np(true, 5000), t0 - 9000, 200_000), 5000, 'clock behind: >= base');
assert.equal(estimatePositionMs(np(true, 199_000), t0 + 9000, 200_000), 200_000, 'clamped to end');
assert.equal(estimatePositionMs(np(true, 5000), t0 + 1000, 0), 6000, 'unknown duration: no cap');
assert.equal(estimatePositionMs(np(true, -50), t0, 1000), 0, 'never negative');
assert.equal(
	estimatePositionMs({ is_playing: true, progress_ms: 10, fetched_at: 'bad' }, t0, 1000),
	10,
	'unparsable fetched_at: no elapsed'
);

/** @param {[number, string][]} pairs */
const ln = (pairs) => pairs.map(([start_ms, text]) => ({ start_ms, text }));

// lineKeys: start plus place in the same-start group.
assert.deepEqual(
	lineKeys(
		ln([
			[1, 'a'],
			[5, 'x'],
			[5, 'y'],
			[9, 'n']
		])
	),
	['1:0', '5:0', '5:1', '9:0']
);
assert.deepEqual(lineKeys([]), []);

// Translations: only done ones, never the song's own language.
const txt = { lines: ['a'], plain: null };
assert.deepEqual(availableLangs(null), []);
assert.deepEqual(
	availableLangs({ status: 'pending', source_language: null, en: null, vi: null }),
	[]
);
assert.deepEqual(availableLangs({ status: 'done', source_language: 'ko', en: txt, vi: txt }), [
	'en',
	'vi'
]);
assert.deepEqual(availableLangs({ status: 'done', source_language: 'vi', en: txt, vi: txt }), [
	'en'
]);
assert.deepEqual(availableLangs({ status: 'done', source_language: 'en', en: null, vi: txt }), [
	'vi'
]);
assert.equal(pickLang('vi', ['en']), 'orig', 'missing language falls back to the original');
assert.equal(pickLang('en', ['en', 'vi']), 'en');
assert.equal(pickLang('orig', ['en']), 'orig');
assert.deepEqual(interleave('một\n\nhai', 'one\n\ntwo'), [
	{ text: 'một', tr: 'one' },
	{ text: '', tr: '' },
	{ text: 'hai', tr: 'two' }
]);
assert.equal(interleave('a\nb', 'one line'), null, 'different shapes: show below');

console.log('lyrics checks passed');
