// Country display shared by page stats and "Listening now": codes are ISO
// 3166-1 alpha-2 from Cloudflare's CF-IPCountry.

let regionNames: Intl.DisplayNames | null = null;
try {
	regionNames = new Intl.DisplayNames(['en'], { type: 'region' });
} catch {
	regionNames = null;
}

/** "Germany" for "DE"; "Unknown" when empty. */
export function countryName(code: string | null | undefined): string {
	if (!code) return 'Unknown';
	try {
		return regionNames?.of(code.toUpperCase()) ?? code;
	} catch {
		return code;
	}
}

/** The flag emoji for a two-letter code (regional indicator letters), or "". */
export function countryFlag(code: string | null | undefined): string {
	if (!code || !/^[A-Za-z]{2}$/.test(code)) return '';
	const base = 0x1f1e6 - 65;
	return String.fromCodePoint(...[...code.toUpperCase()].map((c) => base + c.charCodeAt(0)));
}
