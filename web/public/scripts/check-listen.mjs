// Listen-along sync checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	DEFAULT_VOLUME,
	DRIFT_MS,
	IDLE_MAX_AGE_MS,
	SEEK_COOLDOWN_MS,
	SEEK_LEAD_MS,
	URL_REFRESH_MARGIN_MS,
	audioNote,
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
