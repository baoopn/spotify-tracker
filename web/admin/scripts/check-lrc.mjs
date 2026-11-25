// Tiny checks for src/lib/lrc.ts (same fixtures as internal/lrc on the server).
// Run by `pnpm check` (node strips the TS types).
import assert from 'node:assert/strict';

const {
	parseLrc,
	activeIndex,
	normalizeLyrics,
	validateLrc,
	plainFromLrc,
	markerOnly,
	lrcSourceLines,
	formatLrcTime
} = await import('../src/lib/lrc.ts');

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

// Translations (src/lib/translation.ts).
const {
	availableLangs,
	pickLang,
	translatedLines,
	interleave,
	translationNote,
	estimateCost,
	capRatio,
	hasCap
} = await import('../src/lib/translation.ts');
const tr = (over) => ({
	status: 'done',
	stale: false,
	kind: 'synced',
	source_language: 'vi',
	languages: ['vi'],
	en: 'one\n\nthree',
	vi: null,
	model: 'm',
	waiting: false,
	attempts: 0,
	last_error: '',
	translated_at: null,
	next_attempt_at: null,
	...over
});

check('translation: languages shown', () => {
	assert.deepEqual(availableLangs(null), []);
	assert.deepEqual(availableLangs(tr()), ['en']);
	assert.deepEqual(availableLangs(tr({ source_language: 'ko', vi: 'x' })), ['en', 'vi']);
	assert.deepEqual(
		availableLangs(tr({ languages: ['vi', 'en'], vi: 'x' })),
		['en', 'vi'],
		'mixed song: its main language is shown too'
	);
	assert.deepEqual(availableLangs(tr({ stale: true })), [], 'stale: being redone');
	assert.deepEqual(availableLangs(tr({ status: 'pending' })), []);
	assert.equal(pickLang('vi', ['en']), 'orig');
	assert.equal(pickLang('en', ['en']), 'en');
});

check('translation: synced lines line up with the parsed LRC', () => {
	const lines = parseLrc('[00:01.00] một\n[00:02.00]\n[00:03.00] ba');
	assert.deepEqual(translatedLines(tr().en, lines.length), ['one', '', 'three']);
	assert.equal(translatedLines('one\nthree', lines.length), null, 'count mismatch: hide');
	assert.equal(translatedLines(null, 3), null);
});

check('translation: plain interleave and notes', () => {
	assert.deepEqual(interleave('a\nb', 'x\ny'), [
		{ text: 'a', tr: 'x' },
		{ text: 'b', tr: 'y' }
	]);
	assert.equal(interleave('a\nb', 'x'), null);
	assert.equal(translationNote(tr({ status: 'pending', waiting: true })), null);
	assert.match(translationNote(tr({ status: 'pending' })), /cap/);
	assert.match(translationNote(tr({ status: 'refused' })), /declined/);
	assert.match(translationNote(tr({ status: 'not_queued' })), /when they play/);
	assert.match(translationNote(tr({ status: 'error', next_attempt_at: 'x' })), /tried again later/);
	assert.match(translationNote(tr({ status: 'error' })), /given up/);
});

// Manual lyrics: same fixtures and messages as internal/lrc/validate_test.go.
check('normalizeLyrics: BOM, CRLF, trailing spaces, blank edges', () => {
	const bom = String.fromCharCode(0xfeff);
	assert.equal(
		normalizeLyrics(`${bom}\r\n[00:01.00] a  \r\n[00:02.00] b\t\r[00:03.00] c\0\n\n`),
		'[00:01.00] a\n[00:02.00] b\n[00:03.00] c'
	);
});

check('validateLrc: valid text and repeated choruses', () => {
	assert.deepEqual(
		validateLrc('[ar:Artist]\n[00:01.00] a\n\n[00:02.50] b\n[00:03.00]', 200_000),
		[]
	);
	assert.deepEqual(validateLrc('[00:10.00] a\n[00:05.00][01:00.00] chorus\n[00:20.00] b'), []);
	assert.deepEqual(validateLrc('[03:24.00] a', 200_000), [], 'within the slack');
	assert.deepEqual(validateLrc('[offset:+20000]\n[03:35.00] a', 200_000), [], 'offset');
});

check('validateLrc: per-line errors', () => {
	assert.deepEqual(validateLrc('[00:01.00] a\n[00:75.00] b'), [
		{
			line: 2,
			message: 'invalid timestamp [00:75.00]: seconds must be below 60 and minutes at most 999'
		}
	]);
	assert.deepEqual(validateLrc('[00:01.00] a\n[1.5] b\n[00:02.1234] c'), [
		{ line: 2, message: 'invalid timestamp [1.5]: use [mm:ss.xx]' },
		{ line: 3, message: 'invalid timestamp [00:02.1234]: use [mm:ss.xx]' }
	]);
	const untimed = 'no timestamp: start the line with [mm:ss.xx], or remove it';
	assert.deepEqual(validateLrc('[00:01.00] a\nplain words\n[Chorus]'), [
		{ line: 2, message: untimed },
		{ line: 3, message: untimed }
	]);
	assert.deepEqual(validateLrc('[00:05.00] a\n[00:03.00] b\n[00:06.00] c'), [
		{ line: 2, message: 'out of order: 0:03.00 comes before 0:05.00 on line 1' }
	]);
	assert.deepEqual(validateLrc('[00:01.00] a\n[03:30.00] b', 200_000), [
		{ line: 2, message: '3:30.00 is past the end of the song (3:20.00)' }
	]);
	assert.deepEqual(validateLrc('[ar:x]'), [
		{ line: 0, message: 'no timed lines: synced lyrics need [mm:ss.xx] timestamps' }
	]);
	assert.equal(validateLrc('x\n'.repeat(200)).length, 50, 'errors are capped');
	assert.equal(validateLrc('[00:01.00] a\n'.repeat(2001))[0].line, 0, 'too many lines');
});

check('plainFromLrc: tags removed, markers become one stanza break', () => {
	const lrc =
		'[ar:Artist]\n[offset:+100]\n[00:01.00] one <00:01.50>two\n[00:02.00]\n[00:02.50] ♪\n' +
		'[00:03.00][00:30.00] chorus\n[00:04.00] [Instrumental]\n[00:05.00] last';
	assert.equal(plainFromLrc(lrc), 'one two\n\nchorus\n\nlast');
	assert.equal(plainFromLrc('[00:01.00] ♪\n[00:02.00]'), '');
	assert.ok(markerOnly('[Instrumental] ♪ ...'));
	assert.ok(!markerOnly('la la'));
});

check('lrcSourceLines: the editor preview rows', () => {
	assert.deepEqual(
		lrcSourceLines('[ti:T]\n[00:01.00][00:30.00] a\n\n[00:99.00] b\nwords').map((l) => [
			l.line,
			l.kind,
			l.starts
		]),
		[
			[1, 'meta', []],
			[2, 'timed', [1000, 30000]],
			[3, 'blank', []],
			[4, 'bad', []],
			[5, 'bad', []]
		]
	);
	assert.equal(formatLrcTime(61_230), '1:01.23');
});

check('translation: cost estimate and cap share', () => {
	// claude-sonnet-5-5: $2 in / $10 out per million tokens.
	assert.equal(estimateCost('claude-sonnet-5-5', { input: 1_000_000, output: 100_000 }), 3);
	assert.equal(estimateCost('claude-sonnet-5-5', { input: 0, output: 0 }), 0);
	assert.equal(estimateCost('some-other-model', { input: 1, output: 1 }), null, 'unknown price');
	assert.equal(estimateCost(undefined, { input: 1, output: 1 }), null, 'older API: no model');
	assert.equal(capRatio(40, 1000), 0.04);
	assert.equal(capRatio(120, 100), 1, 'clamped');
	assert.equal(capRatio(5, 0), 0, 'no cap');
});

check('translation: a cap of 0 or less is no cap (no meter)', () => {
	assert.equal(hasCap(100), true);
	assert.equal(hasCap(0), false);
	assert.equal(hasCap(-1), false);
	assert.equal(capRatio(5, -1), 0);
});

if (!process.exitCode) console.log(`${passed} lrc checks passed`);
