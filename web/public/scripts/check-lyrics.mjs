// Now-playing lyrics helper checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import {
	PREFETCH_MS,
	REFETCH_THROTTLE_MS,
	SAME_WINDOW_RETRY_MS,
	SEEK_BACK_MS,
	KEEP_BEFORE,
	currentIndex,
	lineKeys,
	mergeLines,
	needsRefetch,
	rollLines,
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

/** @param {[number, string][]} pairs */
const ln = (pairs) => pairs.map(([start_ms, text]) => ({ start_ms, text }));
/** @param {{ lines: { text: string }[] }} r */
const texts = (r) => r.lines.map((l) => l.text);

// mergeLines: sorted union; the larger same-start group wins, newer on a tie.
assert.deepEqual(mergeLines([], []), []);
assert.deepEqual(
	mergeLines(
		ln([
			[1, 'a'],
			[2, 'b']
		]),
		ln([
			[2, 'B'],
			[3, 'c']
		])
	).map((l) => l.text),
	['a', 'B', 'c'],
	'newer wins a tie'
);
assert.deepEqual(
	mergeLines(
		ln([
			[2, 'b'],
			[3, 'c']
		]),
		ln([
			[1, 'a'],
			[2, 'b']
		])
	).map((l) => l.text),
	['a', 'b', 'c'],
	'sorted'
);
{
	const full = ln([
		[1, 'a'],
		[5, 'x'],
		[5, 'y'],
		[5, 'z']
	]);
	const cut = ln([
		[5, 'y'],
		[5, 'z'],
		[9, 'n']
	]);
	assert.deepEqual(
		mergeLines(full, cut).map((l) => l.text),
		['a', 'x', 'y', 'z', 'n'],
		'a window cut mid-group keeps the whole group'
	);
}

// rollLines: rolling run, never the whole song.
{
	const w1 = {
		track_id: 't',
		until_ms: 5000,
		lines: ln([
			[1000, 'l1'],
			[2000, 'l2'],
			[3000, 'l3'],
			[4000, 'l4']
		])
	};
	let r = rollLines(null, w1, 1500);
	assert.deepEqual(texts(r), ['l1', 'l2', 'l3', 'l4']);
	assert.equal(r.until_ms, 5000);

	// Adjacent window (starts at prev until_ms): merges, past lines trimmed.
	const w2 = {
		track_id: 't',
		until_ms: 10_000,
		lines: ln([
			[5000, 'l5'],
			[6000, 'l6'],
			[7000, 'l7'],
			[8000, 'l8'],
			[9000, 'l9']
		])
	};
	r = rollLines(r, w2, 7500);
	assert.deepEqual(
		texts(r),
		['l3', 'l4', 'l5', 'l6', 'l7', 'l8', 'l9'],
		`${KEEP_BEFORE} lines kept before l7`
	);

	// Overlapping window: merges.
	const w3 = {
		track_id: 't',
		until_ms: 11_000,
		lines: ln([
			[9000, 'l9'],
			[10_000, 'l10']
		])
	};
	r = rollLines(r, w3, 9500);
	assert.deepEqual(texts(r), ['l5', 'l6', 'l7', 'l8', 'l9', 'l10']);

	// Gap ahead (seek forward): replaced, no hole.
	const far = {
		track_id: 't',
		until_ms: 61_000,
		lines: ln([
			[59_000, 'f1'],
			[60_000, 'f2']
		])
	};
	assert.deepEqual(texts(rollLines(r, far, 60_500)), ['f1', 'f2']);

	// Seek back before the kept lines: replaced.
	const back = {
		track_id: 't',
		until_ms: 3000,
		lines: ln([
			[1000, 'l1'],
			[2000, 'l2']
		])
	};
	assert.deepEqual(texts(rollLines(r, back, 1200)), ['l1', 'l2']);

	// New track: replaced.
	const other = { track_id: 'u', until_ms: null, lines: ln([[9000, 'o1']]) };
	assert.deepEqual(texts(rollLines(r, other, 9500)), ['o1']);

	// Before the first line: nothing trimmed.
	assert.deepEqual(texts(rollLines(null, w1, 0)), ['l1', 'l2', 'l3', 'l4']);

	// Long play stays short: never more than KEEP_BEFORE past lines.
	/** @type {import("../src/lib/lyrics.ts").Rolling | null} */
	let run = null;
	for (let s = 0; s < 200; s++) {
		const lines = ln(Array.from({ length: 8 }, (_, i) => [(s + i) * 1000, `L${s + i}`]));
		run = rollLines(run, { track_id: 't', until_ms: (s + 8) * 1000, lines }, s * 1000 + 500);
		assert.ok(currentIndex(run.lines, s * 1000 + 500) <= KEEP_BEFORE, `trimmed at step ${s}`);
	}
	assert.ok(run && run.lines.length <= KEEP_BEFORE + 8);
}

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

console.log('lyrics checks passed');
