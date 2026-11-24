// Tiny checks for src/lib/lrc.ts (same fixtures as internal/lrc on the server).
// Run by `pnpm check` (node strips the TS types).
import assert from 'node:assert/strict';

const { parseLrc, activeIndex } = await import('../src/lib/lrc.ts');

let passed = 0;
function check(name, fn) {
	try {
		fn();
		passed++;
	} catch (err) {
		console.error(`FAIL ${name}\n  ${err.message}`);
		process.exitCode = 1;
	}
}
const starts = (lines) => lines.map((l) => l.startMs);

check('several time tags on one line', () => {
	const l = parseLrc('[00:01.00][00:30.00] chorus');
	assert.deepEqual(l, [
		{ startMs: 1000, text: 'chorus' },
		{ startMs: 30000, text: 'chorus' }
	]);
});

check('fraction widths and mm:ss:ff', () => {
	assert.deepEqual(
		starts(parseLrc('[00:01.5]a\n[00:01.05]b\n[00:01.005]c\n[00:02]d\n[00:03:25]e')),
		[1005, 1050, 1500, 2000, 3250]
	);
});

check('three-digit minutes', () => {
	assert.deepEqual(starts(parseLrc('[100:00.00] long')), [6_000_000]);
});

check('offset tag shifts and clamps at 0', () => {
	assert.deepEqual(starts(parseLrc('[offset:+500]\n[00:00.20]a\n[00:02.00]b')), [0, 1500]);
	assert.deepEqual(starts(parseLrc('[00:02.00]b\n[offset:-500]')), [2500]);
});

check('metadata and untimed lines dropped, blank timed lines kept', () => {
	const l = parseLrc(
		'[ar:Someone]\n[ti:Song]\n[length:03:20]\nplain words\n[00:05.00]\n[00:06.00] x'
	);
	assert.deepEqual(l, [
		{ startMs: 5000, text: '' },
		{ startMs: 6000, text: 'x' }
	]);
});

check('unsorted input sorts stably; CRLF and word tags', () => {
	const l = parseLrc('[00:09.00]late\r\n[00:01.00]<00:01.00>early <00:01.50>word\r\n[00:09.00]tie');
	assert.deepEqual(l, [
		{ startMs: 1000, text: 'early word' },
		{ startMs: 9000, text: 'late' },
		{ startMs: 9000, text: 'tie' }
	]);
});

check('garbage and empty input give no lines', () => {
	assert.deepEqual(parseLrc('[99:99.99] x\n[ab:cd] y'), []);
	assert.deepEqual(parseLrc(''), []);
	assert.deepEqual(parseLrc(null), []);
});

// Shared with internal/lrc/lrc_test.go: keep the two in sync.
check('invalid second tag dropped', () => {
	assert.deepEqual(parseLrc('[00:01.00][00:75.00] x'), [{ startMs: 1000, text: 'x' }]);
});

check('byte order mark', () => {
	assert.deepEqual(parseLrc('\uFEFF[00:01.00] a\n[00:02.00] b'), [
		{ startMs: 1000, text: 'a' },
		{ startMs: 2000, text: 'b' }
	]);
});

check('offset any case', () => {
	assert.deepEqual(parseLrc('[OFFSET:+500]\n[00:01.00] a'), [{ startMs: 500, text: 'a' }]);
});

check('minutes cap', () => {
	assert.deepEqual(parseLrc('[1000:00.00] x\n[999:59.99] y'), [{ startMs: 59_999_990, text: 'y' }]);
});

check('activeIndex', () => {
	const l = parseLrc('[00:01.00]a\n[00:02.00]b\n[00:03.00]c');
	assert.equal(activeIndex(l, 0), -1);
	assert.equal(activeIndex(l, 1000), 0);
	assert.equal(activeIndex(l, 2500), 1);
	assert.equal(activeIndex(l, 99_000), 2);
	assert.equal(activeIndex([], 5), -1);
});

if (!process.exitCode) console.log(`${passed} lrc checks passed`);
