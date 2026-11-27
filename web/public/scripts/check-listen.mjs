// Listen-along sync checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real helpers.
import assert from 'node:assert/strict';
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

console.log('check-listen: ok');
