// Listen-along audio status for the admin player views (pure; tested by
// scripts/check-audio-status.mjs).

/** The batch endpoint takes 1 to 50 Spotify track ids. */
export const STATUS_MAX_IDS = 50;
const TRACK_ID = /^[0-9A-Za-z]{22}$/;

/**
 * The ids for one status call: valid Spotify ids only (local files have
 * none), no repeats, in order (the current song first: the server checks the
 * bucket for it when no file is recorded yet), at most 50.
 */
export function statusIds(ids: readonly (string | null | undefined)[]): string[] {
	const out: string[] = [];
	for (const id of ids) {
		if (!id || !TRACK_ID.test(id) || out.includes(id)) continue;
		out.push(id);
		if (out.length === STATUS_MAX_IDS) break;
	}
	return out;
}
