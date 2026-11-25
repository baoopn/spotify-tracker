// Now-playing lyrics helper checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import {
	availableLangs,
	currentIndex,
	interleave,
	lineKeys,
	lyricsState,
	pickLang
} from '../src/lib/lyrics.ts';
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

// Translations: only done and present ones. A language is null only when
// the song is all in it, so a mixed song shows its main language's too.
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
	'en',
	'vi'
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

// lyricsState: one answer for the hero's layout toggle and the lyrics panel.
const idle = { isPending: false, isFetching: false, isError: false };
const first = { isPending: true, isFetching: true, isError: false };
const refetching = { isPending: false, isFetching: true, isError: false };
const failedQ = { isPending: false, isFetching: false, isError: true };
const retrying = { isPending: false, isFetching: true, isError: true };
/**
 * @param {Partial<import('../src/lib/types.ts').NowLyrics>} over
 * @returns {import('../src/lib/types.ts').NowLyrics}
 */
const nl = (over) => ({
	track_id: 't1',
	status: 'found',
	synced: true,
	instrumental: false,
	lines: lines([1000]),
	plain: null,
	credit: { text: 'c', url: 'u' },
	translation: null,
	...over
});
assert.equal(lyricsState(undefined, first, 't1'), 'loading', 'first fetch');
assert.equal(lyricsState(undefined, idle, null), 'none', 'no track');
assert.equal(lyricsState(nl({}), idle, 't1'), 'readable', 'synced lines');
assert.equal(
	lyricsState(nl({ synced: false, lines: [], plain: 'a\nb' }), idle, 't1'),
	'readable',
	'plain text'
);
assert.equal(
	lyricsState(nl({ synced: true, lines: [], plain: 'a\nb' }), idle, 't1'),
	'readable',
	'synced but no lines: the plain text is readable'
);
assert.equal(lyricsState(nl({ lines: [], plain: null }), idle, 't1'), 'none', 'found, nothing');
assert.equal(lyricsState(nl({ status: 'pending', lines: [] }), idle, 't1'), 'looking');
assert.equal(lyricsState(nl({ status: 'instrumental', lines: [] }), idle, 't1'), 'instrumental');
assert.equal(lyricsState(null, idle, 't1'), 'none', 'server has no lyrics');
// Song change: the server still answers for the previous song between polls.
assert.equal(lyricsState(nl({ track_id: 't0' }), idle, 't1'), 'loading', 'old song, between polls');
assert.equal(lyricsState(nl({ track_id: 't0' }), refetching, 't1'), 'loading', 'old song, polling');
assert.equal(lyricsState(undefined, failedQ, 't1'), 'failed');
assert.equal(lyricsState(nl({ track_id: 't0' }), failedQ, 't1'), 'failed', 'old song, then error');
assert.equal(lyricsState(undefined, retrying, 't1'), 'loading', 'retry running');
assert.equal(lyricsState(nl({}), failedQ, 't1'), 'readable', 'a failed refetch keeps the lyrics');

console.log('lyrics checks passed');
