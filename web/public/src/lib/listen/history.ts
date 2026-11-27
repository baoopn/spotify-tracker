// Listen-along history: group plays by local calendar day (pure; tested by
// scripts/check-listen.mjs).

const keyFormats = new Map<string, Intl.DateTimeFormat>();

/** "YYYY-MM-DD" of an instant in `zone` (the viewer's zone when undefined). */
export function dayKey(ms: number, zone?: string): string {
	const id = zone ?? '';
	let f = keyFormats.get(id);
	if (!f) {
		f = new Intl.DateTimeFormat('en-CA', {
			timeZone: zone,
			year: 'numeric',
			month: '2-digit',
			day: '2-digit'
		});
		keyFormats.set(id, f);
	}
	const parts = f.formatToParts(ms);
	const get = (t: string) => parts.find((p) => p.type === t)?.value ?? '';
	return `${get('year')}-${get('month')}-${get('day')}`;
}

/** The day before a "YYYY-MM-DD" key (calendar maths, so DST never matters). */
export function previousDay(key: string): string {
	const [y, m, d] = key.split('-').map(Number);
	return new Date(Date.UTC(y, m - 1, d - 1)).toISOString().slice(0, 10);
}

/** "Today", "Yesterday", "Tue, Nov 24" or, in another year, "Tue, Nov 24, 2025". */
export function dayLabel(key: string, todayKey: string): string {
	if (key === todayKey) return 'Today';
	if (key === previousDay(todayKey)) return 'Yesterday';
	const [y, m, d] = key.split('-').map(Number);
	const sameYear = key.slice(0, 4) === todayKey.slice(0, 4);
	return new Intl.DateTimeFormat('en-US', {
		timeZone: 'UTC',
		weekday: 'short',
		month: 'short',
		day: 'numeric',
		year: sameYear ? undefined : 'numeric'
	}).format(Date.UTC(y, m - 1, d));
}

export type DayGroup<T> = { key: string; label: string; items: T[] };

/** Consecutive plays (newest first) grouped by local day, in order. */
export function groupByDay<T extends { listened_at: string }>(
	plays: T[],
	nowMs: number,
	zone?: string
): DayGroup<T>[] {
	const today = dayKey(nowMs, zone);
	const groups: DayGroup<T>[] = [];
	for (const p of plays) {
		const ms = Date.parse(p.listened_at);
		if (!Number.isFinite(ms)) continue;
		const key = dayKey(ms, zone);
		const last = groups.at(-1);
		if (last?.key === key) last.items.push(p);
		else groups.push({ key, label: dayLabel(key, today), items: [p] });
	}
	return groups;
}
