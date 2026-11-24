// Playback position estimate between now-playing updates (pure, no runes).

/** The fields of a now-playing snapshot the estimate needs. */
type Snapshot = { is_playing: boolean; progress_ms: number | null; fetched_at: string };

/**
 * Position in ms at `serverNowMs`: progress at the snapshot plus the time
 * elapsed since its fetched_at while playing. Clamped to [0, duration] (when
 * the duration is known) so a wrong clock can't push it off either end.
 * Null without a progress value.
 */
export function estimatePositionMs(
	np: Snapshot | null | undefined,
	serverNowMs: number,
	durationMs: number
): number | null {
	if (!np || np.progress_ms == null) return null;
	const fetched = Date.parse(np.fetched_at);
	const elapsed =
		np.is_playing && Number.isFinite(fetched) ? Math.max(0, serverNowMs - fetched) : 0;
	const p = np.progress_ms + elapsed;
	return Math.max(0, durationMs > 0 ? Math.min(p, durationMs) : p);
}
