// Shared helpers for the backup status (settings page and overview card).
// The backup container reports through the database; the API only reads it.

import { CircleAlert, CircleCheck, CirclePause, TriangleAlert } from '@lucide/svelte';
import type { Tone } from './components/Badge.svelte';
import { timeOfDay } from './time';
import type { BackupHealth, BackupStatus } from './types';

export const BACKUP_DOCS_URL = 'https://github.com/baoopn/spotify-tracker/blob/main/docs/backup.md';

/** "HH:MM", 24-hour. */
export const HHMM_RE = /^([01]\d|2[0-3]):[0-5]\d$/;

type HealthMeta = { tone: Tone; cls: string; icon: typeof CircleCheck; label: string };

const META: Record<BackupHealth, HealthMeta> = {
	ok: { tone: 'ok', cls: 'bg-spot-mint text-on-spot', icon: CircleCheck, label: 'Healthy' },
	paused: { tone: 'info', cls: 'bg-spot-sky text-on-spot', icon: CirclePause, label: 'Paused' },
	stale: { tone: 'danger', cls: 'bg-spot-rose text-on-spot', icon: CircleAlert, label: 'Stale' },
	failing: {
		tone: 'danger',
		cls: 'bg-spot-rose text-on-spot',
		icon: CircleAlert,
		label: 'Failing'
	},
	offline: {
		tone: 'warn',
		cls: 'bg-spot-butter text-on-spot',
		icon: TriangleAlert,
		label: 'Offline'
	},
	unconfigured: {
		tone: 'warn',
		cls: 'bg-spot-butter text-on-spot',
		icon: TriangleAlert,
		label: 'Not configured'
	},
	unknown: {
		tone: 'warn',
		cls: 'bg-spot-butter text-on-spot',
		icon: TriangleAlert,
		label: 'Unknown'
	}
};

export function healthMeta(h: BackupHealth | undefined): HealthMeta {
	return (h && META[h]) || META.unknown;
}

/** One line that says why the health is what it is. */
export function healthExplanation(s: BackupStatus): string {
	switch (s.health) {
		case 'ok':
			return 'Backups run on schedule.';
		case 'paused':
			return 'Scheduled backups are paused. "Back up now" still works.';
		case 'stale':
			return s.last_success
				? 'The last successful backup is older than expected.'
				: 'No backup has succeeded yet.';
		case 'failing':
			return 'The last backup failed and none has succeeded since.';
		case 'offline':
			return 'The backup service has not checked in for over 5 minutes.';
		case 'unconfigured':
			return 'The backup service runs, but its destination or encryption key is missing.';
		default:
			return 'The backup service is not running, or has never reported.';
	}
}

/** True while a request waits in the queue or a run is in progress (poll faster). */
export function backupBusy(s: BackupStatus | undefined): boolean {
	if (!s) return false;
	return !!s.pending_request || s.runs.some((r) => r.status === 'running');
}

/** The service can take a request (else the API answers 503). */
export function backupAvailable(s: BackupStatus | undefined): boolean {
	return !!s?.agent && s.agent.configured && s.health !== 'offline' && s.health !== 'unknown';
}

/** "age1qyqs…x7k2" for an age public key. */
export function shortKey(key: string): string {
	return key.length > 16 ? `${key.slice(0, 8)}…${key.slice(-4)}` : key;
}

/**
 * A daily "HH:MM" UTC time as the admin's wall time at its next occurrence,
 * e.g. "9:00 PM CST". The offset can change with DST, so use the next run.
 */
export function utcTimeLocal(hhmm: string, now = Date.now()): string | null {
	if (!HHMM_RE.test(hhmm)) return null;
	const [h, m] = hhmm.split(':').map(Number);
	const d = new Date(now);
	d.setUTCHours(h, m, 0, 0);
	if (d.getTime() < now) d.setUTCDate(d.getUTCDate() + 1);
	return timeOfDay(d);
}
