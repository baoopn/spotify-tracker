// Now-playing lyrics helper checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import {
	PREFETCH_MS,
	REFETCH_THROTTLE_MS,
	SAME_WINDOW_RETRY_MS,
	SEEK_BACK_MS,
	MAX_LINES,
	currentIndex,
	mergeLines,
	needsRefetch,
	shouldRefetch,
	windowKey
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

// needsRefetch: about to run out, or a seek back before the window.
const w = { from_ms: 10_000, until_ms: 40_000 };
assert.equal(needsRefetch(w, 10_000), false);
assert.equal(needsRefetch(w, 40_000 - PREFETCH_MS - 1), false);
assert.equal(needsRefetch(w, 40_000 - PREFETCH_MS), true);
assert.equal(needsRefetch(w, 50_000), true, 'seek forward past the window');
assert.equal(needsRefetch(w, 10_000 - SEEK_BACK_MS), false);
assert.equal(needsRefetch(w, 10_000 - SEEK_BACK_MS - 1), true, 'seek back');
assert.equal(needsRefetch({ from_ms: 0, until_ms: null }, 0), false);
assert.equal(needsRefetch({ from_ms: 200_000, until_ms: null }, 999_999), false, 'last lines');

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

// shouldRefetch: never while failing or hidden; once per distinct window,
// a same window again only after SAME_WINDOW_RETRY_MS; throttled.
{
	const w = { track_id: 't1', from_ms: 10_000, until_ms: 40_000 };
	const due = 40_000 - PREFETCH_MS; // needsRefetch is true from here
	const ok = { isError: false, hidden: false, nowMs: 100_000 };
	const fresh = { key: null, at: 0 };
	assert.equal(shouldRefetch(w, due - 1, ok, fresh), false, 'window still covers the position');
	assert.equal(shouldRefetch(w, due, ok, fresh), true, 'window running out');
	assert.equal(
		shouldRefetch(w, due, { ...ok, isError: true }, fresh),
		false,
		'failing: leave it to retries'
	);
	assert.equal(shouldRefetch(w, due, { ...ok, hidden: true }, fresh), false, 'hidden tab');
	const asked = { key: windowKey(w), at: 100_000 };
	assert.equal(
		shouldRefetch(w, due, { ...ok, nowMs: 100_000 + REFETCH_THROTTLE_MS }, asked),
		false,
		'same window came back: no loop'
	);
	assert.equal(
		shouldRefetch(w, due, { ...ok, nowMs: 100_000 + SAME_WINDOW_RETRY_MS }, asked),
		true,
		'same window asked again after the long retry'
	);
	const next = { ...w, from_ms: 36_000, until_ms: 70_000 };
	assert.equal(
		shouldRefetch(next, 70_000 - PREFETCH_MS, { ...ok, nowMs: 100_000 + 1_000 }, asked),
		false,
		'throttled even for a new window'
	);
	assert.equal(
		shouldRefetch(
			next,
			70_000 - PREFETCH_MS,
			{ ...ok, nowMs: 100_000 + REFETCH_THROTTLE_MS },
			asked
		),
		true,
		'a new window gets its one refetch'
	);
	assert.notEqual(windowKey(w), windowKey({ ...w, track_id: 't2' }), 'key includes the track');
	assert.equal(windowKey({ ...w, until_ms: null }), 't1:10000:');
}

// mergeLines: union by start_ms, sorted, newer window wins, capped.
{
	const a = [
		{ start_ms: 1000, text: 'a' },
		{ start_ms: 2000, text: 'b' }
	];
	const b = [
		{ start_ms: 2000, text: 'B' },
		{ start_ms: 3000, text: 'c' }
	];
	assert.deepEqual(mergeLines([], []), []);
	assert.deepEqual(mergeLines(a, []), a);
	assert.deepEqual(
		mergeLines(a, b).map((l) => l.text),
		['a', 'B', 'c'],
		'newer window wins on the same start'
	);
	assert.deepEqual(
		mergeLines(b, a).map((l) => l.text),
		['a', 'b', 'c'],
		'out-of-order window still sorts'
	);
	const dup = [
		{ start_ms: 5000, text: 'x' },
		{ start_ms: 5000, text: 'y' }
	];
	assert.deepEqual(
		mergeLines(a, dup).map((l) => l.text),
		['a', 'b', 'x', 'y'],
		'same-start lines in one window stay in order'
	);
	assert.deepEqual(
		mergeLines(mergeLines(a, b), b).map((l) => l.text),
		['a', 'B', 'c'],
		'idempotent'
	);
	const big = Array.from({ length: MAX_LINES + 20 }, (_, i) => ({ start_ms: i, text: `${i}` }));
	const capped = mergeLines([], big);
	assert.equal(capped.length, MAX_LINES);
	assert.equal(capped[0].start_ms, 20, 'drops the earliest');
}

console.log('lyrics checks passed');
