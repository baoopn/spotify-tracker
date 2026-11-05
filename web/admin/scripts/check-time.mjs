// Tiny checks for src/lib/time.ts: release dates stay calendar values, and
// formatting is DST-safe. Run by `pnpm check` (node strips the TS types).
import assert from 'node:assert/strict';

// Run as if the browser were in a negative-offset zone, where naive date-only
// parsing goes wrong (new Date('2020-01-01') is Dec 31, 2019 locally).
process.env.TZ = 'America/Los_Angeles';

const t = await import('../src/lib/time.ts');

// Intl may use U+202F / U+00A0 before AM/PM depending on ICU; compare loosely.
const norm = (s) => s.replace(/\s/g, ' ');
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

check('releaseYear slices the year from every precision', () => {
	assert.equal(t.releaseYear('2020'), '2020');
	assert.equal(t.releaseYear('2020-05'), '2020');
	assert.equal(t.releaseYear('2020-01-01'), '2020');
	assert.equal(t.releaseYear(''), null);
	assert.equal(t.releaseYear('soon'), null);
});

check('formatReleaseDate never shifts the day in a negative-offset zone', () => {
	const naive = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium' }).format(
		new Date('2020-01-01')
	);
	assert.equal(naive, 'Dec 31, 2019', 'sanity: naive parsing is wrong here');
	assert.equal(t.formatReleaseDate('2020-01-01', 'en-US'), 'Jan 1, 2020');
	assert.equal(t.formatReleaseDate('2020-05', 'en-US'), 'May 2020');
	assert.equal(t.formatReleaseDate('2020', 'en-US'), '2020');
});

check('date-only strings are not instants', () => {
	assert.equal(t.parseInstant('2020-05-01'), null);
	assert.equal(t.parseInstant('2020'), null);
	assert.ok(t.parseInstant('2026-11-01T05:30:00Z'));
});

check('America/New_York 2026-11-01: the repeated 1:30 AM carries its zone', () => {
	const opts = { locale: 'en-US', timeZone: 'America/New_York' };
	assert.equal(norm(t.dateTime('2026-11-01T05:30:00Z', opts)), 'Nov 1, 2026, 1:30 AM EDT');
	assert.equal(norm(t.dateTime('2026-11-01T06:30:00Z', opts)), 'Nov 1, 2026, 1:30 AM EST');
});

check('daily buckets use server instants, not +86 400 000 ms', () => {
	const opts = { locale: 'en-US', timeZone: 'America/New_York' };
	const nov1 = '2026-11-01T04:00:00Z'; // local midnight EDT
	const nov2 = '2026-11-02T05:00:00Z'; // local midnight EST (25 h later)
	assert.equal(t.bucketLabel(nov1, 'day', opts), 'Nov 1');
	assert.equal(t.bucketLabel(nov2, 'day', opts), 'Nov 2');
	const naive = new Date(Date.parse(nov1) + 86_400_000);
	assert.equal(t.bucketLabel(naive, 'day', opts), 'Nov 1', 'sanity: adding a day lands on Nov 1');
});

check('hourly labels follow the local wall clock (incl. :30 offsets)', () => {
	process.env.TZ = 'Asia/Kolkata';
	try {
		assert.equal(
			norm(t.bucketLabel('2026-11-01T12:00:00Z', 'hour', { locale: 'en-US' })),
			'5:30 PM'
		);
	} finally {
		process.env.TZ = 'America/Los_Angeles';
	}
});

check('days until expiry is floored from instants', () => {
	const now = Date.parse('2026-11-01T12:00:00Z');
	assert.equal(t.daysUntil('2026-11-10T00:00:00Z', now), 8);
	assert.equal(t.daysUntil('2026-11-01T11:00:00Z', now), -1);
	assert.equal(t.daysUntil(null, now), null);
});

check('utcDateTime labels UTC explicitly', () => {
	assert.equal(norm(t.utcDateTime('2027-05-04T09:12:00Z', 'en-US')), 'May 4, 2027, 9:12 AM UTC');
});

check('relative times come from instant differences', () => {
	const now = Date.parse('2026-11-01T06:30:00Z');
	assert.equal(t.relative('2026-11-01T06:27:00Z', now, 'en-US'), '3 minutes ago');
	assert.equal(t.relative('2026-11-01T05:30:00Z', now, 'en-US'), '1 hour ago');
});

check('stale browser tz data: server offset wins, labelled GMT-6', () => {
	const at = new Date('2026-11-05T18:00:00Z');
	// Simulate stale browser data: Intl says UTC-7 for America/Denver (MST in
	// November), while the mocked server answer says -6 for the same zone.
	const tz = 'America/Denver';
	assert.equal(t.intlOffsetMinutes(tz, at), -420, 'sanity: Intl reports -7 for the test zone');
	const server = { tz, offset_minutes: -360, offset: '-06:00' };
	const spec = t.resolveZone(tz, server, at);
	assert.equal(spec.corrected, true);
	assert.equal(spec.label, 'GMT-6');
	assert.ok(['-06:00', 'Etc/GMT+6'].includes(spec.timeZone), `fixed offset, got ${spec.timeZone}`);
	t.setZoneProvider(() => spec);
	try {
		assert.equal(norm(t.dateTime(at, { locale: 'en-US' })), 'Nov 5, 2026, 12:00 PM GMT-6');
		assert.equal(norm(t.timeOfDay(at, { locale: 'en-US' })), '12:00 PM GMT-6');
		// a daily bucket at the server's local midnight (06:00Z) keeps its calendar day
		assert.equal(
			t.bucketLabel('2026-11-05T06:00:00Z', 'day', { locale: 'en-US', timeZone: tz }),
			'Nov 5'
		);
	} finally {
		t.setZoneProvider(() => ({ iana: null }));
	}
});

check('server agrees or is unreachable: plain Intl with the IANA zone', () => {
	const at = new Date('2026-11-05T18:00:00Z');
	const ok = t.resolveZone(
		'America/Denver',
		{ tz: 'America/Denver', offset_minutes: -420, offset: '-07:00' },
		at
	);
	assert.deepEqual([ok.corrected, ok.timeZone, ok.label], [false, 'America/Denver', undefined]);
	const down = t.resolveZone('America/Denver', null, at);
	assert.deepEqual([down.corrected, down.timeZone], [false, 'America/Denver']);
	assert.deepEqual(t.resolveZone(null, null, at), { iana: null, corrected: false });
	assert.equal(t.offsetLabel(-360), 'GMT-6');
	assert.equal(t.offsetLabel(330), 'GMT+5:30');
	assert.equal(t.offsetLabel(0), 'GMT');
});

if (process.exitCode) {
	console.error('time checks failed');
} else {
	console.log(`${passed} time checks passed (DST, release dates, zones)`);
}
