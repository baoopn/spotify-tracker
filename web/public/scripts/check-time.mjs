// Time-handling checks, run by `pnpm check` (docs/admin-api.md "Time handling").
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import { clockTime, dateTime, relative, releaseYear, toDate } from '../src/lib/format.ts';
import {
	browserOffset,
	gmtLabel,
	offsetZone,
	parseLongOffset,
	resolveZone
} from '../src/lib/tz.ts';

// Release dates are calendar strings: slice the year, never parse as instants.
assert.equal(releaseYear('2020'), '2020');
assert.equal(releaseYear('2020-05'), '2020');
assert.equal(releaseYear('2020-01-01'), '2020');
assert.equal(releaseYear(' 1999-12-31 '), '1999');
assert.equal(releaseYear('soon'), null);
assert.equal(releaseYear(undefined), null);
assert.equal(toDate('2020-01-01'), null, 'date-only strings must not become instants');

// DST boundary in America/New_York: 2026-11-01 02:00 EDT -> 01:00 EST.
const NY = 'America/New_York';
const before = clockTime('2026-11-01T05:30:00Z', { zone: NY }); // 1:30 AM EDT
const after = clockTime('2026-11-01T06:30:00Z', { zone: NY }); // 1:30 AM EST (same wall time)
assert.match(before, /1:30:00/);
assert.match(after, /1:30:00/);
assert.notEqual(before, after, `zone name must differ across the DST change: ${before} / ${after}`);
assert.match(before, /EDT|GMT-4/);
assert.match(after, /EST|GMT-5/);

// Relative days come from calendar dates, not 24 h subtraction:
// exactly 24 h before Nov 1 23:30 EST is Nov 1 00:30 EDT, the SAME local day.
const now = Date.parse('2026-11-02T04:30:00Z'); // 2026-11-01 23:30 EST
assert.doesNotMatch(relative('2026-11-01T04:30:00Z', now, NY), /yesterday/);
// 25 h before is Oct 31 (the previous calendar day) -> "yesterday".
assert.match(relative('2026-11-01T03:30:00Z', now, NY), /yesterday/);
assert.equal(relative('2026-11-02T04:29:30Z', now, NY), 'just now');

// Stale browser tz data: a mocked mismatch must format at the server's fixed
// offset ("-06:00") and be labelled from the minutes ("GMT-6"), never by Intl.
assert.equal(parseLongOffset('GMT-06:00'), -360);
assert.equal(parseLongOffset('GMT+05:30'), 330);
assert.equal(parseLongOffset('GMT'), 0);
assert.equal(gmtLabel(-360), 'GMT-6');
assert.equal(gmtLabel(330), 'GMT+5:30');
assert.equal(gmtLabel(0), 'GMT');
const server = {
	tz: 'America/Denver',
	abbreviation: 'CST',
	offset_minutes: -360,
	offset: '-06:00',
	now: '2026-11-05T14:00:00-06:00',
	source: 'test'
};
const stale = resolveZone('America/Denver', server, -420); // browser ICU says UTC-7
assert.equal(stale.kind, 'corrected');
assert.ok(stale.kind === 'corrected');
assert.equal(stale.zone, '-06:00');
assert.equal(stale.label, 'GMT-6');
const fixed = { zone: stale.zone, label: stale.label };
assert.match(clockTime('2026-11-05T20:00:00Z', fixed), /^2:00:00\sPM GMT-6$/);
assert.match(dateTime('2026-11-05T20:00:00Z', fixed), /2:00:00.PM GMT-6$/);
// Agreement keeps the IANA zone; no zone at all is "unknown".
assert.deepEqual(resolveZone('America/Denver', server, -360), {
	kind: 'intl',
	zone: 'America/Denver'
});
assert.deepEqual(resolveZone(null, null, null), { kind: 'unknown' });
assert.equal(typeof browserOffset('America/Denver'), 'number');
assert.equal(offsetZone(-360), '-06:00');
assert.equal(offsetZone(330), '+05:30');
// Same offset but a stale name (browser "MDT", server "CST"): keep the IANA
// zone, label from the offset.
assert.deepEqual(resolveZone('America/Denver', server, -360, 'MDT'), {
	kind: 'corrected',
	zone: 'America/Denver',
	label: 'GMT-6',
	tz: 'America/Denver',
	reason: 'name'
});
assert.deepEqual(resolveZone('America/Denver', server, -360, 'CST'), {
	kind: 'intl',
	zone: 'America/Denver'
});
// Numeric server abbreviations ("+07") never trigger a relabel.
assert.equal(
	resolveZone(
		'Asia/Bangkok',
		{ ...server, tz: 'Asia/Bangkok', abbreviation: '+07', offset_minutes: 420, offset: '+07:00' },
		420,
		'GMT+7'
	).kind,
	'intl'
);

console.log('time checks: ok');
