// Listen-along sync checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	hasClerkParams,
	hasInviteTicket,
	inviteMode,
	stripClerkParams
} from '../src/lib/account/invite.ts';
import {
	DriftController,
	DriftState,
	HARD_SEEK_MS,
	JITTER_MS,
	RATE_MIN_INTERVAL_MS,
	SETTLE_MS,
	SOFT_START_MS,
	SOFT_STOP_MS,
	estimateAt,
	isOwnerAction,
	rateFor,
	smoothTarget
} from '../src/lib/listen/drift.ts';
import {
	hasClerkSession,
	initials,
	safeRedirect,
	signInTarget
} from '../src/lib/account/redirect.ts';
import {
	DEFAULT_VOLUME,
	DRIFT_MS,
	IDLE_MAX_AGE_MS,
	SEEK_COOLDOWN_MS,
	SEEK_LEAD_MS,
	URL_REFRESH_MARGIN_MS,
	audioNote,
	joinSeekMs,
	nextStatus,
	parseVolume,
	seekTarget,
	shouldRefreshUrl,
	syncAction,
	targetMs
} from '../src/lib/listen/sync.ts';
import {
	MAX_REPORT_MS,
	SEEK_JUMP_MS,
	chunkReport,
	playedDelta
} from '../src/lib/listen/progress.ts';
import { presenceState, presenceTrackId } from '../src/lib/listen/presence.ts';
import { dayKey, dayLabel, groupByDay, previousDay } from '../src/lib/listen/history.ts';

const T0 = Date.parse('2026-11-26T12:00:00Z');
/** @param {number} ms */
const at = (ms) => new Date(T0 + ms).toISOString();
const snap = (over = {}) => ({
	is_playing: true,
	progress_ms: 10_000,
	fetched_at: at(0),
	track: { id: '4uLU6hMCjMI75M1A2tKUQC' },
	...over
});

// targetMs: progress plus the time since fetched_at while playing.
assert.equal(targetMs(snap(), T0 + 1500, 200_000), 11_500);
assert.equal(targetMs(snap({ is_playing: false }), T0 + 5000, 200_000), 10_000, 'paused stays put');
assert.equal(targetMs(snap(), T0 + 500_000, 200_000), null, 'stale snapshot');
assert.equal(targetMs(snap(), T0 + IDLE_MAX_AGE_MS, 200_000), 70_000, 'exactly at the limit');
assert.equal(targetMs(snap(), T0 + IDLE_MAX_AGE_MS + 1, 200_000), null);
assert.equal(targetMs(snap({ track: null }), T0, 200_000), null, 'no track');
assert.equal(targetMs(snap({ progress_ms: null }), T0, 200_000), null, 'no progress');
assert.equal(targetMs(snap({ fetched_at: 'nope' }), T0, 200_000), null, 'bad timestamp');
assert.equal(targetMs(null, T0, 200_000), null);
assert.equal(targetMs(snap({ progress_ms: 199_000 }), T0 + 5000, 200_000), 200_000, 'clamped');
assert.equal(targetMs(snap(), T0 - 5000, 200_000), 10_000, 'client clock behind the server');

// seekTarget: a little ahead, inside the file.
assert.equal(seekTarget(10_000, 200_000), 10_000 + SEEK_LEAD_MS);
assert.equal(seekTarget(-1000, 200_000), 0);
assert.equal(seekTarget(199_900, 200_000), 199_750, 'stays before the end');
assert.equal(seekTarget(10_000, NaN), 10_000 + SEEK_LEAD_MS, 'unknown duration');
assert.equal(seekTarget(10_000, 0), 10_000 + SEEK_LEAD_MS);
assert.equal(seekTarget(500, 100), 0, 'tiny file');

// syncAction table.
/** @type {import('../src/lib/listen/sync.ts').SyncInput} */
const base = {
	joined: true,
	ownerPlaying: true,
	audioPaused: false,
	audioReady: true,
	audioMs: 10_000,
	targetMs: 10_000,
	audioDurationMs: 200_000,
	msSinceSeek: 10_000
};
/** @type {[string, Partial<import('../src/lib/listen/sync.ts').SyncInput>, object][]} */
const cases = [
	['in sync', {}, { kind: 'none' }],
	['small drift is fine', { audioMs: 10_000 + DRIFT_MS }, { kind: 'none' }],
	[
		'big drift seeks',
		{ audioMs: 10_000 + DRIFT_MS + 1 },
		{ kind: 'seek', toMs: 10_000 + SEEK_LEAD_MS }
	],
	['behind seeks too', { audioMs: 5000 }, { kind: 'seek', toMs: 10_000 + SEEK_LEAD_MS }],
	['seek cooldown', { audioMs: 30_000, msSinceSeek: SEEK_COOLDOWN_MS - 1 }, { kind: 'none' }],
	[
		'cooldown over',
		{ audioMs: 30_000, msSinceSeek: SEEK_COOLDOWN_MS },
		{ kind: 'seek', toMs: 10_000 + SEEK_LEAD_MS }
	],
	['not joined pauses', { joined: false }, { kind: 'pause' }],
	['not joined, already paused', { joined: false, audioPaused: true }, { kind: 'none' }],
	['no target pauses', { targetMs: null }, { kind: 'pause' }],
	['no target, paused', { targetMs: null, audioPaused: true }, { kind: 'none' }],
	['owner paused', { ownerPlaying: false }, { kind: 'pause' }],
	['owner paused, already paused', { ownerPlaying: false, audioPaused: true }, { kind: 'none' }],
	['past the file end', { targetMs: 200_000 }, { kind: 'ended' }],
	['past the end while paused', { targetMs: 250_000, audioPaused: true }, { kind: 'ended' }],
	[
		'paused and ready plays',
		{ audioPaused: true, audioMs: 0 },
		{ kind: 'play', toMs: 10_000 + SEEK_LEAD_MS }
	],
	['paused, not ready waits', { audioPaused: true, audioReady: false }, { kind: 'none' }],
	[
		'unknown duration still plays',
		{ audioPaused: true, audioDurationMs: NaN },
		{ kind: 'play', toMs: 10_000 + SEEK_LEAD_MS }
	]
];
for (const [name, over, want] of cases) {
	assert.deepEqual(syncAction({ ...base, ...over }), want, name);
}

// shouldRefreshUrl: under a minute left (or expired).
assert.equal(shouldRefreshUrl(T0 + URL_REFRESH_MARGIN_MS + 1, T0), false);
assert.equal(shouldRefreshUrl(T0 + URL_REFRESH_MARGIN_MS - 1, T0), true);
assert.equal(shouldRefreshUrl(T0 - 1, T0), true);
assert.equal(shouldRefreshUrl(null, T0), false);
assert.equal(shouldRefreshUrl(NaN, T0), false);

// audioNote: plain words per reason.
assert.equal(audioNote('not_found'), 'No audio for this song');
assert.equal(audioNote('storage_unconfigured'), "Audio isn't set up yet");
assert.equal(audioNote('unavailable'), 'Audio is unavailable right now');
assert.equal(audioNote('idle'), "Bao isn't listening right now");
assert.equal(audioNote(null), null);
const REASONS = /** @type {const} */ (['not_found', 'storage_unconfigured', 'unavailable', 'idle']);
for (const r of REASONS) {
	assert.ok(!audioNote(r)?.includes('—'), 'no em dashes');
}

// parseVolume: 0..1, default when missing or broken.
assert.equal(parseVolume(null), DEFAULT_VOLUME);
assert.equal(parseVolume(''), DEFAULT_VOLUME);
assert.equal(parseVolume('abc'), DEFAULT_VOLUME);
assert.equal(parseVolume('0.35'), 0.35);
assert.equal(parseVolume('0'), 0);
assert.equal(parseVolume('1'), 1);
assert.equal(parseVolume('7'), 1);
assert.equal(parseVolume('-2'), 0);
assert.equal(parseVolume('Infinity'), DEFAULT_VOLUME);

// Join after Stop streaming starts at Bao's live position, never where the
// listener stopped: the audio position plays no part.
assert.equal(joinSeekMs(95_000, 200_000), 95_000 + SEEK_LEAD_MS);
assert.equal(joinSeekMs(null, 200_000), null, 'nothing to follow');
assert.equal(joinSeekMs(199_900, 200_000), 199_750, 'stays inside the file');

// nextStatus: the status after a tick.
/** @type {import('../src/lib/listen/sync.ts').StatusInput} */
const st = {
	current: 'loading',
	hasTrack: true,
	targetMs: 10_000,
	ownerPlaying: true,
	joined: true,
	hasSource: true,
	paused: false,
	readyState: 4,
	driftMs: 100
};
assert.equal(nextStatus(st), 'in-sync');
assert.equal(nextStatus({ ...st, driftMs: DRIFT_MS }), 'loading', 'too far off');
assert.equal(nextStatus({ ...st, readyState: 2 }), 'loading', 'still buffering');
assert.equal(nextStatus({ ...st, ownerPlaying: false }), 'paused-by-owner');
assert.equal(nextStatus({ ...st, targetMs: null }), 'waiting');
assert.equal(nextStatus({ ...st, hasTrack: false }), 'waiting');
assert.equal(
	nextStatus({ ...st, current: 'no-audio' }),
	'no-audio',
	'sticks until the song changes'
);
assert.equal(nextStatus({ ...st, current: 'ended', paused: true }), 'ended');
// Not joined (also after Stop streaming): ready once the metadata is in.
assert.equal(nextStatus({ ...st, joined: false, paused: true, readyState: 1 }), 'in-sync');
assert.equal(nextStatus({ ...st, joined: false, paused: true, readyState: 0 }), 'preparing');
// Right after Stop streaming: not joined, metadata reloading, the last status
// was "in-sync" or "loading". Never "loading" (getting in sync) while not joined.
for (const current of /** @type {const} */ (['in-sync', 'loading'])) {
	for (const hasSource of [true, false]) {
		const s = nextStatus({ ...st, current, joined: false, paused: true, hasSource, readyState: 0 });
		assert.equal(s, 'preparing', `stopped: ${current}, source ${hasSource}`);
	}
}
assert.equal(nextStatus({ ...st, hasSource: false }), 'loading', 'joined, no source yet');

// playedDelta: currentTime steps count; seeks and steps back do not.
assert.equal(playedDelta(null, 10), 0, 'first reading');
assert.equal(playedDelta(10, 10.25), 250);
assert.equal(playedDelta(10, 10 + SEEK_JUMP_MS / 1000), SEEK_JUMP_MS, 'exactly 2 s counts');
assert.equal(playedDelta(10, 12.01), 0, 'a jump over 2 s is a seek');
assert.equal(playedDelta(10, 9), 0, 'back seek');
assert.equal(playedDelta(10, 10), 0);
assert.equal(playedDelta(NaN, 10), 0);
assert.equal(playedDelta(10, NaN), 0);

// chunkReport: whole ms, at most MAX_REPORT_MS per report, remainder kept.
assert.deepEqual(chunkReport(0), { chunks: [], rest: 0 });
assert.deepEqual(chunkReport(0.4), { chunks: [], rest: 0.4 });
assert.deepEqual(chunkReport(15_000), { chunks: [15_000], rest: 0 });
assert.deepEqual(chunkReport(MAX_REPORT_MS), { chunks: [MAX_REPORT_MS], rest: 0 });
assert.deepEqual(chunkReport(45_000), { chunks: [20_000, 20_000, 5000], rest: 0 });
{
	const r = chunkReport(1000.75);
	assert.deepEqual(r.chunks, [1000]);
	assert.ok(Math.abs(r.rest - 0.75) < 1e-9);
}
assert.deepEqual(chunkReport(NaN), { chunks: [], rest: 0 });
for (const c of chunkReport(123_456).chunks) assert.ok(c >= 1 && c <= MAX_REPORT_MS);

// History days: calendar days in the viewer's zone.
const NY = 'America/New_York';
assert.equal(dayKey(Date.parse('2026-11-26T03:00:00Z'), NY), '2026-11-25', 'still the 25th in NY');
assert.equal(dayKey(Date.parse('2026-11-26T03:00:00Z'), 'UTC'), '2026-11-26');
assert.equal(previousDay('2026-03-01'), '2026-02-28');
assert.equal(previousDay('2026-01-01'), '2025-12-31');
assert.equal(dayLabel('2026-11-26', '2026-11-26'), 'Today');
assert.equal(dayLabel('2026-11-25', '2026-11-26'), 'Yesterday');
assert.equal(dayLabel('2026-11-20', '2026-11-26'), 'Fri, Nov 20');
assert.equal(dayLabel('2025-11-20', '2026-11-26'), 'Thu, Nov 20, 2025');
{
	const now = Date.parse('2026-11-26T18:00:00Z');
	const plays = [
		{ id: 3, listened_at: '2026-11-26T17:00:00Z' },
		{ id: 2, listened_at: '2026-11-26T01:00:00Z' }, // Nov 25 evening in NY
		{ id: 1, listened_at: '2026-11-20T12:00:00Z' },
		{ id: 0, listened_at: 'bad' }
	];
	const g = groupByDay(plays, now, NY);
	assert.deepEqual(
		g.map((x) => [x.label, x.items.map((p) => p.id)]),
		[
			['Today', [3]],
			['Yesterday', [2]],
			['Fri, Nov 20', [1]]
		]
	);
	assert.deepEqual(
		groupByDay(plays, now, 'UTC').map((x) => x.items.length),
		[2, 1],
		'same day in UTC'
	);
	assert.deepEqual(groupByDay([], now, NY), []);
}

// presenceState: listening only when joined and in sync; hidden or ended is idle.
/** @type {[Parameters<typeof presenceState>[0], string][]} */
const presenceCases = [
	[{ joined: true, status: 'in-sync', hidden: false }, 'listening'],
	[{ joined: true, status: 'loading', hidden: false }, 'following'],
	[{ joined: true, status: 'paused-by-owner', hidden: false }, 'following'],
	[{ joined: false, status: 'in-sync', hidden: false }, 'following'],
	[{ joined: false, status: 'no-audio', hidden: false }, 'following'],
	[{ joined: true, status: 'in-sync', hidden: true }, 'idle'],
	[{ joined: true, status: 'ended', hidden: false }, 'idle']
];
for (const [input, want] of presenceCases) {
	assert.equal(presenceState(input), want, JSON.stringify(input));
}
assert.equal(presenceTrackId('4uLU6hMCjMI75M1A2tKUQC'), '4uLU6hMCjMI75M1A2tKUQC');
assert.equal(presenceTrackId('spotify:local:abc'), null);
assert.equal(presenceTrackId(null), null);

// safeRedirect: only same-origin relative paths (no open redirects).
assert.equal(safeRedirect('/listen', null), '/listen');
assert.equal(safeRedirect('/listen?x=1#a', null), '/listen?x=1#a');
assert.equal(safeRedirect('/', null), '/');
assert.equal(safeRedirect('//evil.example', null), null, 'protocol-relative');
assert.equal(safeRedirect('https://evil.example', null), null, 'absolute URL');
assert.equal(safeRedirect(String.raw`/\evil.example`, null), null, 'backslash');
assert.equal(safeRedirect(String.raw`/ok\x`, null), null, 'any backslash');
assert.equal(safeRedirect('', null), null, 'empty');
assert.equal(safeRedirect(null, '/'), '/');
assert.equal(safeRedirect(undefined, '/'), '/');
assert.equal(safeRedirect('listen', null), null, 'relative without slash');
assert.equal(safeRedirect('javascript:alert(1)', null), null);
assert.equal(safeRedirect('/a\nb', null), null, 'control characters');
assert.equal(safeRedirect('\t/listen', null), null);
// Dot segments that normalize into a protocol-relative "//evil.example".
assert.equal(safeRedirect('/..//evil.example', null), null, 'dot segment');
assert.equal(safeRedirect('/%2e%2e//evil.example', null), null, 'encoded dot segment');
assert.equal(safeRedirect('/a/../..//evil.example', null), null, 'nested dot segments');
assert.equal(safeRedirect('/%2E%2E//evil.example?x=1', null), null, 'upper-case encoding');
// The admin has its own sign-in.
assert.equal(safeRedirect('/admin', null), null, '/admin');
assert.equal(safeRedirect('/admin/listeners', null), null, '/admin/...');
assert.equal(safeRedirect('/ADMIN', null), null, 'any case');
assert.equal(safeRedirect('/listen/../admin', null), null, 'dot segment into /admin');
assert.equal(safeRedirect('/administrator', null), '/administrator', 'only the /admin tree');
// Normalized output.
assert.equal(safeRedirect('/a/./b/../listen?x=1#h', null), '/a/listen?x=1#h');
assert.equal(signInTarget('/listen', '?redirect_url=%2F..%2F%2Fevil.example'), '/listen');
assert.equal(signInTarget('/listen', '?redirect_url=/admin'), '/listen');

// signInTarget: /listen honours a safe redirect_url / return_to; else stays.
assert.equal(signInTarget('/listen', ''), '/listen');
assert.equal(signInTarget('/listen', '?redirect_url=%2Flisten%23history'), '/listen#history');
assert.equal(signInTarget('/listen', '?return_to=/'), '/');
assert.equal(signInTarget('/listen', '?redirect_url=https://evil.example'), '/listen');
assert.equal(signInTarget('/listen', '?redirect_url=//evil.example'), '/listen');
assert.equal(
	signInTarget('/listen', '?__clerk_ticket=t&__clerk_status=sign_up'),
	'/listen',
	'invitation link'
);
assert.equal(signInTarget('/', ''), '/');
assert.equal(signInTarget('/', '?tab=1'), '/?tab=1');

// hasClerkSession: the readable __client_uat cookie, set and not "0".
assert.equal(hasClerkSession(''), false);
assert.equal(hasClerkSession('__client_uat=0'), false);
assert.equal(hasClerkSession('__client_uat='), false);
assert.equal(hasClerkSession('a=1; __client_uat=1764200000; b=2'), true);
assert.equal(hasClerkSession('__client_uat_AbC12=1764200000'), true, 'suffixed name');
assert.equal(hasClerkSession('__client_uat_AbC12=0; x=1'), false);
assert.equal(hasClerkSession('x__client_uat=1'), false);

// initials for the avatar without a photo.
assert.equal(initials('Ada Lovelace', 'ada@example.com'), 'AL');
assert.equal(initials('Ada', null), 'AD');
assert.equal(initials('', 'ada@example.com'), 'A');
assert.equal(initials(null, null), '?');

// rateFor: behind speeds up, ahead slows down, at most ±5 %, 0.005 steps.
const near = (/** @type {number} */ x, /** @type {number} */ y, msg = '') =>
	assert.ok(Math.abs(x - y) < 1e-9, `${msg} ${x} != ${y}`);
near(rateFor(0), 1);
near(rateFor(-1000), 1.05, 'behind 1 s: 1 + 1000/4000 clamped to 1.05');
near(rateFor(-100), 1.025, 'behind 100 ms');
near(rateFor(100), 0.975, 'ahead 100 ms');
near(rateFor(200), 0.95);
near(rateFor(5000), 0.95, 'clamped ahead');
near(rateFor(-5000), 1.05, 'clamped behind');
near(rateFor(-30), 1.01, 'rounded to 0.005 (1.0075)');

// DriftState: hysteresis, hard seek, EMA reset, buffering.
{
	const d = new DriftState();
	let t = 0;
	/** @param {number | null} drift @param {boolean} [buffering] */
	const step = (drift, buffering = false) => d.step(drift, buffering, (t += RATE_MIN_INTERVAL_MS));
	assert.deepEqual(step(100), { seek: false, rate: 1 }, 'under the start threshold');
	assert.equal(d.correcting, false);
	// Drive the EMA above 120 ms: correcting starts and the rate moves.
	for (let i = 0; i < 10; i++) step(-400);
	assert.equal(d.correcting, true);
	assert.ok(d.rate > 1, 'behind: faster');
	// Between stop (40) and start (120): keeps correcting (hysteresis).
	for (let i = 0; i < 20; i++) step(-80);
	assert.equal(d.correcting, true, 'still correcting at 80 ms');
	for (let i = 0; i < 20; i++) step(-10);
	assert.equal(d.correcting, false, 'stops under 40 ms');
	assert.equal(d.rate, 1, 'back to exactly 1');
	assert.ok(SOFT_STOP_MS < SOFT_START_MS);

	// Hard seek over 1.5 s (smoothed): one measure of 6 s is enough.
	const h = new DriftState();
	assert.equal(h.step(HARD_SEEK_MS - 1, false, 0).seek, false, 'just under');
	h.reset();
	assert.equal(h.step(HARD_SEEK_MS + 1, false, 0).seek, true, 'just over');
	assert.equal(h.ema, null, 'EMA reset after the seek');
	assert.equal(h.step(6000, false, 1000).seek, true);

	// EMA smoothing: a single spike does not trigger a seek.
	const e = new DriftState();
	e.step(0, false, 0);
	assert.equal(e.step(3000, false, 500).seek, false, '0.3 * 3000 = 900 ms smoothed');

	// Buffering: no correction, rate 1, the drift is not counted.
	const b = new DriftState();
	for (let i = 0; i < 10; i++) b.step(-400, false, i * 500);
	assert.ok(b.rate > 1);
	assert.deepEqual(b.step(-400, true, 6000), { seek: false, rate: 1 });
	assert.equal(b.ema, null, 'not counted while buffering');
	assert.deepEqual(b.step(null, false, 7000), { seek: false, rate: 1 }, 'nothing to follow');

	// Rate changes at most every 500 ms.
	const r = new DriftState();
	r.step(-400, false, 0);
	const first = r.step(-400, false, 1000).rate;
	r.step(-1000, false, 1100);
	assert.equal(r.rate, first, 'no change within 500 ms');

	// Right after a seek: nothing counted (the seek lead must not be corrected).
	const s = new DriftState();
	assert.deepEqual(s.step(300, false, 0, SETTLE_MS - 1), { seek: false, rate: 1 });
	assert.equal(s.ema, null, 'not counted inside the settle window');
	// A big gap waits for the seek cooldown, then seeks.
	const cd = new DriftState();
	assert.equal(cd.step(5000, false, 0, SEEK_COOLDOWN_MS - 1).seek, false, 'in cooldown');
	cd.reset();
	assert.equal(cd.step(5000, false, 0, SEEK_COOLDOWN_MS).seek, true, 'cooldown over');
}

// smoothTarget: keep the running estimate for jitter under 250 ms.
{
	const id = '4uLU6hMCjMI75M1A2tKUQC';
	const e0 = smoothTarget(null, id, 10_000, true, 0);
	assert.equal(estimateAt(e0, 1000), 11_000);
	const e1 = smoothTarget(e0, id, 11_000 + JITTER_MS - 1, true, 1000);
	assert.equal(e1, e0, 'jitter kept out');
	const e2 = smoothTarget(e0, id, 30_000, true, 1000);
	assert.notEqual(e2, e0, 'Bao seeked: take the new value');
	assert.equal(estimateAt(e2, 1000), 30_000);
	assert.notEqual(smoothTarget(e0, 'other0track0id00000000', 11_000, true, 1000), e0, 'new song');
	const paused = smoothTarget(e0, id, 11_000, false, 1000);
	assert.notEqual(paused, e0, 'pause rebases');
	assert.equal(estimateAt(paused, 9000), 11_000, 'paused stays put');
}

// Owner actions: a new changed_at for the same song (Bao seeked, paused or
// resumed) skips the jitter filter and asks for a seek; the same changed_at
// with small jitter is filtered; null keeps the old behaviour.
{
	const id = '4uLU6hMCjMI75M1A2tKUQC';
	const T = Date.parse('2026-11-27T12:00:00Z');
	/** @param {number} progress @param {number} fetchedOffset @param {string | null | undefined} changed */
	const np = (progress, fetchedOffset, changed) => ({
		is_playing: true,
		progress_ms: progress,
		fetched_at: new Date(T + fetchedOffset).toISOString(),
		changed_at: changed,
		track: { id }
	});
	const A = '2026-11-27T11:59:00.000Z';
	const B = '2026-11-27T12:00:02.500Z';

	const e = smoothTarget(null, id, 10_000, true, 0, A);
	assert.equal(isOwnerAction(e, id, A), false, 'same changed_at');
	assert.equal(isOwnerAction(e, id, B), true, 'new changed_at');
	assert.equal(isOwnerAction(e, id, null), false, 'null now');
	assert.equal(
		isOwnerAction(smoothTarget(null, id, 10_000, true, 0, null), id, B),
		false,
		'null before'
	);
	assert.equal(
		isOwnerAction(e, 'other0track0id00000000', B),
		false,
		'another song is a track change'
	);
	assert.notEqual(smoothTarget(e, id, 10_100, true, 0, B), e, 'action skips the jitter filter');
	assert.equal(smoothTarget(e, id, 10_100, true, 0, A), e, 'same changed_at: jitter filtered');

	const c = new DriftController();
	assert.equal(c.target(np(10_000, 0, A), T, 200_000), 10_000);
	// 100 ms of poll jitter, same changed_at: the running estimate stays.
	assert.equal(c.target(np(11_100, 1000, A), T + 1000, 200_000), 11_000);
	assert.equal(c.seekPending, false);
	// Bao seeked (only 100 ms, under the jitter limit): taken at once, seek requested.
	assert.equal(c.target(np(10_900, 2000, B), T + 2000, 200_000), 10_900);
	assert.equal(c.seekPending, true, 'seek right away');

	// Null changed_at (an older API): only the jitter rule applies.
	const n = new DriftController();
	n.target(np(10_000, 0, null), T, 200_000);
	assert.equal(n.target(np(11_100, 1000, null), T + 1000, 200_000), 11_000);
	assert.equal(n.seekPending, false);

	// Only one place seeks: any seek (the player's too) clears the pending one.
	const C = '2026-11-27T12:00:05.000Z';
	assert.equal(c.target(np(20_000, 3000, C), T + 3000, 200_000), 20_000);
	assert.equal(c.seekPending, true);
	c.noteSeek();
	assert.equal(c.seekPending, false, 'cleared by the player seek');
}

// Invitation links: which Clerk screen, and stripping the params afterwards.
assert.equal(inviteMode('?__clerk_status=sign_up&__clerk_ticket=t'), 'sign_up');
assert.equal(inviteMode('?__clerk_status=complete&__clerk_ticket=t'), 'used');
assert.equal(inviteMode('?__clerk_status=sign_in'), 'used');
assert.equal(inviteMode(''), 'none');
assert.equal(inviteMode('?redirect_url=/listen'), 'none');
assert.equal(inviteMode('?__clerk_status=other'), 'none');
assert.equal(hasInviteTicket('?__clerk_status=sign_up&__clerk_ticket=t'), true);
assert.equal(hasInviteTicket('?__clerk_ticket='), false, 'empty ticket');
assert.equal(hasInviteTicket('?__clerk_status=sign_up'), false);
assert.equal(hasClerkParams('?a=1&__clerk_status=complete'), true);
assert.equal(hasClerkParams('?a=1'), false);
assert.equal(
	stripClerkParams('/listen', '?a=1&__clerk_status=sign_up&__clerk_ticket=t&b=2', '#history'),
	'/listen?a=1&b=2#history',
	'keeps other params and the hash'
);
assert.equal(
	stripClerkParams('/listen', '?__clerk_status=sign_up&__clerk_ticket=t', ''),
	'/listen',
	'no empty ?'
);
assert.equal(stripClerkParams('/listen', '', '#x'), '/listen#x');

// The <audio> attachment must not track state: Svelte re-runs (and tears
// down) an attachment when state it read changes, so a mute or volume change
// would stop the player.
const player = readFileSync(new URL('../src/lib/listen/player.svelte.ts', import.meta.url), 'utf8');
assert.match(
	player,
	/attach = \(el: HTMLAudioElement\) => untrack\(/,
	'listenPlayer.attach must run its body in untrack()'
);

console.log('check-listen: ok');
