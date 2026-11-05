// Formatting helpers. Large standalone numbers use compact notation; tables
// use full numbers with grouping.

const nf = new Intl.NumberFormat(undefined);
const nfCompact = new Intl.NumberFormat(undefined, {
	notation: 'compact',
	maximumFractionDigits: 1
});
const nf1 = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const pct = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 2 });

export function num(n: number | null | undefined): string {
	return n == null || Number.isNaN(n) ? '–' : nf.format(n);
}

export function compact(n: number | null | undefined): string {
	if (n == null || Number.isNaN(n)) return '–';
	return Math.abs(n) < 10_000 ? nf.format(n) : nfCompact.format(n);
}

export function decimal(n: number | null | undefined): string {
	return n == null || Number.isNaN(n) ? '–' : nf1.format(n);
}

export function percent(ratio: number | null | undefined): string {
	return ratio == null || Number.isNaN(ratio) ? '–' : pct.format(ratio);
}

export function ms(n: number | null | undefined): string {
	if (n == null || Number.isNaN(n)) return '–';
	if (n >= 1000) return `${nf1.format(n / 1000)} s`;
	return `${nf.format(Math.round(n))} ms`;
}

export function hours(msTotal: number | null | undefined): string {
	if (msTotal == null) return '–';
	const h = msTotal / 3_600_000;
	return h < 10 ? `${nf1.format(h)} h` : `${compact(Math.round(h))} h`;
}

export function duration(msTotal: number | null | undefined): string {
	if (msTotal == null || msTotal < 0) return '–:––';
	const total = Math.floor(msTotal / 1000);
	const m = Math.floor(total / 60);
	const s = total % 60;
	return `${m}:${s.toString().padStart(2, '0')}`;
}

// Dates and times live in time.ts (zone-aware, DST-safe); re-exported for convenience.
export {
	date,
	dateTime,
	daysUntil,
	formatReleaseDate,
	inDays,
	relative,
	releaseYear,
	utcDateTime
} from './time.js';

export function artistNames(artists: { name: string }[] | undefined): string {
	return artists?.map((a) => a.name).join(', ') || 'Unknown artist';
}

export function titleCase(s: string): string {
	return s.charAt(0).toUpperCase() + s.slice(1).replaceAll('_', ' ');
}

const KEYS = ['C', 'C♯/D♭', 'D', 'D♯/E♭', 'E', 'F', 'F♯/G♭', 'G', 'G♯/A♭', 'A', 'A♯/B♭', 'B'];

export function musicalKey(key: number | null, mode: number | null): string {
	if (key == null || key < 0 || key > 11) return '–';
	return `${KEYS[key]} ${mode === 1 ? 'major' : mode === 0 ? 'minor' : ''}`.trim();
}
