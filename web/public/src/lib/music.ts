// Music-theory helpers for the song stats illustrations.

/** Caption names by pitch class (0 = C). */
export const KEY_NAMES = [
	'C',
	'C♯/D♭',
	'D',
	'E♭',
	'E',
	'F',
	'F♯',
	'G',
	'A♭',
	'A',
	'B♭',
	'B'
] as const;

/** Circle of fifths, clockwise from the top: major names and their relative minors. */
export const FIFTHS_MAJOR = ['C', 'G', 'D', 'A', 'E', 'B', 'F♯', 'D♭', 'A♭', 'E♭', 'B♭', 'F'];
export const FIFTHS_MINOR = [
	'Am',
	'Em',
	'Bm',
	'F♯m',
	'C♯m',
	'G♯m',
	'E♭m',
	'B♭m',
	'Fm',
	'Cm',
	'Gm',
	'Dm'
];

export function validKey(key: number | null | undefined): key is number {
	return key != null && Number.isInteger(key) && key >= 0 && key <= 11;
}

/** "A♭ major", "C♯/D♭ minor", or "—" when there is no key. */
export function keyName(key: number | null | undefined, mode: number | null | undefined): string {
	if (!validKey(key)) return '—';
	const m = mode === 1 ? ' major' : mode === 0 ? ' minor' : '';
	return `${KEY_NAMES[key]}${m}`;
}

/** Segment index on the circle of fifths: majors by their tonic, minors via their relative major. */
export function fifthsIndex(key: number, mode: number | null | undefined): number {
	const tonic = mode === 0 ? (key + 3) % 12 : key;
	return (tonic * 7) % 12;
}
