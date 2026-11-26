// The Overview health strip: one status per Status-page section, computed
// from data the Overview already loads (pure, no runes).

import { healthMeta } from './backups';
import { compact, relative } from './format';
import { tokenDaysLeft, tokenHealth } from './spotify';
import { hasCap } from './translation';
import type {
	BackupStatus,
	EnrichmentCounts,
	LyricsQueue,
	ServiceHealth,
	SpotifyStatus,
	TranslationQueue
} from './types';

/** ok, warning, error; off = turned off; unknown = no data (yet). */
export type HealthLevel = 'ok' | 'warn' | 'error' | 'off' | 'unknown';

/** Status-page section ids the chips link to. */
export type StatusSection =
	'spotify-token' | 'services' | 'backups' | 'lyrics-queue' | 'translations' | 'enrichment';

export type HealthChip = {
	id: StatusSection;
	label: string;
	level: HealthLevel;
	/** Short value, e.g. "179 days left". */
	value: string;
	/** Status word for the accessible name, e.g. "healthy". */
	status: string;
};

const WORD: Record<HealthLevel, string> = {
	ok: 'healthy',
	warn: 'needs attention',
	error: 'action needed',
	off: 'off',
	unknown: 'unknown'
};

function chip(
	id: StatusSection,
	label: string,
	level: HealthLevel,
	value: string,
	status = WORD[level]
): HealthChip {
	return { id, label, level, value, status };
}

/** Same thresholds as the Spotify token card (tokenHealth). */
export function spotifyChip(s: SpotifyStatus | undefined, now = Date.now()): HealthChip {
	const label = 'Spotify token';
	if (!s) return chip('spotify-token', label, 'unknown', 'Unavailable');
	const h = tokenHealth(s);
	const days = tokenDaysLeft(s, now);
	const level: HealthLevel =
		h === 'ok' ? 'ok' : h === 'warn' ? 'warn' : h === 'danger' ? 'error' : 'unknown';
	let value: string;
	if (!s.connected) value = 'Not connected';
	else if (s.last_error) value = 'Refresh failed';
	else if (days != null) value = `${days} ${days === 1 ? 'day' : 'days'} left`;
	else value = 'Connected';
	return chip('spotify-token', label, level, value);
}

/** Down → error, degraded → warning. */
export function servicesChip(list: ServiceHealth[] | undefined): HealthChip {
	const label = 'Services';
	if (!list) return chip('services', label, 'unknown', 'Unavailable');
	if (list.length === 0) return chip('services', label, 'warn', 'None reported');
	const ok = list.filter((s) => s.status === 'ok').length;
	const level: HealthLevel = list.some((s) => s.status === 'down')
		? 'error'
		: ok < list.length
			? 'warn'
			: 'ok';
	return chip('services', label, level, `${ok}/${list.length} healthy`);
}

/** The server's backup health (stale / failing → error, offline etc. → warning). */
export function backupsChip(s: BackupStatus | undefined, now = Date.now()): HealthChip {
	const label = 'Backups';
	if (!s) return chip('backups', label, 'unknown', 'Unavailable');
	const m = healthMeta(s.health);
	const level: HealthLevel =
		m.tone === 'ok' ? 'ok' : m.tone === 'danger' ? 'error' : m.tone === 'info' ? 'off' : 'warn';
	const last = s.last_success;
	const value = last ? relative(last.finished_at ?? last.started_at, now) : 'No backup yet';
	return chip('backups', label, level, value, m.label.toLowerCase());
}

/** Failed lookups (retried later) → warning. */
export function lyricsChip(s: LyricsQueue | undefined): HealthChip {
	const label = 'Lyrics queue';
	if (!s) return chip('lyrics-queue', label, 'unknown', 'Unavailable');
	if (!s.enabled) return chip('lyrics-queue', label, 'off', 'Off');
	const waiting = s.queued + s.in_progress;
	const parts = [`${compact(waiting)} queued`];
	if (s.error) parts.push(`${compact(s.error)} failed`);
	return chip('lyrics-queue', label, s.error > 0 ? 'warn' : 'ok', parts.join(' · '));
}

/** The daily or monthly cap reached → warning. */
export function translationsChip(s: TranslationQueue | undefined): HealthChip {
	const label = 'Translations';
	if (!s) return chip('translations', label, 'unknown', 'Unavailable');
	if (!s.enabled) return chip('translations', label, 'off', 'Off');
	const dayFull = hasCap(s.daily_cap) && s.calls_today >= s.daily_cap;
	const monthFull = hasCap(s.monthly_cap) && s.calls_month >= s.monthly_cap;
	const today = hasCap(s.daily_cap)
		? `${compact(s.calls_today)}/${compact(s.daily_cap)} today`
		: `${compact(s.calls_today)} today`;
	const queued = s.queued + s.in_progress;
	const value = queued ? `${today} · ${compact(queued)} queued` : today;
	if (dayFull || monthFull) {
		return chip(
			'translations',
			label,
			'warn',
			value,
			monthFull ? 'monthly cap reached' : 'daily cap reached'
		);
	}
	return chip('translations', label, 'ok', value);
}

/** Share of tracks enriched; failed enrichments → warning. */
export function enrichmentChip(c: EnrichmentCounts | undefined): HealthChip {
	const label = 'Enrichment';
	if (!c) return chip('enrichment', label, 'unknown', 'Unavailable');
	const total = (c.enriched ?? 0) + (c.pending ?? 0) + (c.unavailable ?? 0) + (c.failed ?? 0);
	if (total === 0) return chip('enrichment', label, 'ok', 'No tracks yet');
	// Floored, so 100% means every track.
	const value = `${Math.floor(((c.enriched ?? 0) / total) * 100)}% enriched`;
	if ((c.failed ?? 0) > 0) {
		return chip('enrichment', label, 'warn', `${value} · ${compact(c.failed)} failed`);
	}
	return chip('enrichment', label, 'ok', value);
}
