// Pure time-zone helpers (no runes), shared by the zone store and the checks.
// See docs/admin-api.md "Stale browser time-zone data".

export type ServerZone = {
	tz: string;
	abbreviation: string;
	offset_minutes: number;
	offset: string;
	now: string;
	source: string;
};

/** Parse Intl's `longOffset` zone name: "GMT-06:00" -> -360, "GMT+05:30" -> 330, "GMT" -> 0. */
export function parseLongOffset(name: string | undefined): number | null {
	if (name == null) return null;
	if (name === 'GMT' || name === 'UTC') return 0;
	const m = /^(?:GMT|UTC)([+-−])(\d{1,2})(?::?(\d{2}))?$/.exec(name.trim());
	if (!m) return null;
	const sign = m[1] === '+' ? 1 : -1;
	return sign * (Number(m[2]) * 60 + Number(m[3] ?? 0));
}

/** The browser's own (possibly stale) UTC offset in minutes for a zone at an instant. */
export function browserOffset(zone: string, at: number = Date.now()): number | null {
	try {
		const parts = new Intl.DateTimeFormat('en-US', {
			timeZone: zone,
			timeZoneName: 'longOffset'
		}).formatToParts(at);
		return parseLongOffset(parts.find((p) => p.type === 'timeZoneName')?.value);
	} catch {
		return null; // the browser doesn't know this zone at all
	}
}

/** The browser's short zone name ("MDT", "GMT+7") for a zone at an instant, if any. */
export function browserShortName(zone: string, at: number = Date.now()): string | null {
	try {
		const parts = new Intl.DateTimeFormat('en-US', {
			timeZone: zone,
			timeZoneName: 'short'
		}).formatToParts(at);
		return parts.find((p) => p.type === 'timeZoneName')?.value ?? null;
	} catch {
		return null;
	}
}

/** "GMT-6", "GMT+5:30", "GMT" built from minutes, never from Intl's names. */
export function gmtLabel(offsetMinutes: number): string {
	if (offsetMinutes === 0) return 'GMT';
	const sign = offsetMinutes < 0 ? '-' : '+';
	const abs = Math.abs(offsetMinutes);
	const h = Math.floor(abs / 60);
	const m = abs % 60;
	return `GMT${sign}${h}${m ? `:${String(m).padStart(2, '0')}` : ''}`;
}

/** "+05:30" / "-06:00": a fixed-offset zone Intl accepts as `timeZone`. */
export function offsetZone(offsetMinutes: number): string {
	const sign = offsetMinutes < 0 ? '-' : '+';
	const abs = Math.abs(offsetMinutes);
	return `${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
}

export type Resolution =
	| { kind: 'intl'; zone: string } // browser data agrees (or no server answer): use the IANA zone
	// Stale browser data. Offset differs: format at the server's fixed offset.
	// Only the name differs (e.g. "MDT" vs the server's "CST" at the same
	// UTC-6): keep the IANA zone but label it from the offset.
	| { kind: 'corrected'; zone: string; label: string; tz: string; reason: 'offset' | 'name' }
	| { kind: 'unknown' }; // no zone at all: browser default offset

/** Decide how to format, given the zone and the server's answer (if any). */
export function resolveZone(
	zone: string | null,
	server: ServerZone | null,
	browserMinutes: number | null,
	browserName: string | null = null
): Resolution {
	if (!zone) return { kind: 'unknown' };
	if (!server || server.tz !== zone) return { kind: 'intl', zone };
	const label = gmtLabel(server.offset_minutes);
	if (browserMinutes !== server.offset_minutes) {
		return {
			kind: 'corrected',
			zone: server.offset || offsetZone(server.offset_minutes),
			label,
			tz: zone,
			reason: 'offset'
		};
	}
	// Same offset. A different *letter* abbreviation (browser "MDT", server
	// "CST") means the browser's name is stale; numeric server names ("+07")
	// just mean the zone has no abbreviation, and Intl's "GMT+7" is fine.
	const serverHasName = /^[A-Za-z]{2,5}$/.test(server.abbreviation);
	if (serverHasName && browserName && browserName !== server.abbreviation) {
		return { kind: 'corrected', zone, label, tz: zone, reason: 'name' };
	}
	return { kind: 'intl', zone };
}

/** Every zone the picker offers: Intl's list, plus zones it may omit. */
export function zoneList(): string[] {
	let list: string[];
	try {
		list = Intl.supportedValuesOf('timeZone');
	} catch {
		list = [];
	}
	return [...new Set([...list, 'America/Edmonton', 'UTC'])].sort();
}
