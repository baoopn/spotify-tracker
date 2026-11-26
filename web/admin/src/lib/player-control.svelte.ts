// Commands for the owner's Spotify player, shared by the Player page and the
// Overview's mini controls. Create one per component (it uses the query
// client and registers a cleanup), during component init.
//
// The state is polled every 5 s while the tab is visible (queries.ts).
// Commands update the cache at once (optimistic) and hold the polls while
// they run and for HOLD_MS after: Spotify's GET /me/player lags 0.5–2 s
// behind a command, so an earlier poll would undo the optimistic state.
// Releasing the hold re-enables (and so refetches) the player queries.
// The hold is module-wide: it outlives the component that sent the command,
// so opening the Player page right after a click on the Overview still waits.

import { onDestroy } from 'svelte';
import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
import { api, errorMessage } from './api';
import { secondClock } from './clock.svelte';
import { qk, queries } from './queries';
import { toasts } from './toast.svelte';
import type { PlaybackState, PlayerStateView } from './types';

const HOLD_MS = 1500;

// Commands in flight (any instance) and the cool-down after the last one.
const hold = $state({ pending: 0, cooling: false });
let coolTimer: ReturnType<typeof setTimeout> | undefined;

function holdStart() {
	hold.pending++;
	clearTimeout(coolTimer);
	hold.cooling = false;
}

function holdEnd() {
	hold.pending = Math.max(0, hold.pending - 1);
	clearTimeout(coolTimer);
	hold.cooling = true;
	coolTimer = setTimeout(() => (hold.cooling = false), HOLD_MS);
}

/** True while a player command runs and for HOLD_MS after: polls are paused. */
export function playerHolding(): boolean {
	return hold.pending > 0 || hold.cooling;
}

/** A pause sent from here that the now-playing stream may not show yet. */
export type LocalPause = { trackId: string; progressMs: number } | null;

export type PlayerCmd = {
	/** "pause", "skip to the next track": used in the error toast. */
	label: string;
	run: () => Promise<unknown>;
	patch?: (s: PlaybackState) => PlaybackState;
	done?: string;
};

export class PlayerControl {
	#destroyed = false;
	// A slider change made while a command runs waits for it (latest wins).
	#queued: PlayerCmd | null = null;
	#cmd;
	/** The playback state query (GET /admin/api/player?parts=state). */
	readonly query;

	constructor() {
		const queryClient = useQueryClient();
		this.query = createQuery(() => ({ ...queries.playerState(), enabled: !playerHolding() }));
		this.#cmd = createMutation(() => ({
			mutationFn: (c: PlayerCmd) => c.run(),
			onMutate: async (c: PlayerCmd) => {
				holdStart();
				await queryClient.cancelQueries({ queryKey: qk.player.state, exact: true });
				const prev = queryClient.getQueryData<PlayerStateView>(qk.player.state);
				if (prev?.state && c.patch) {
					queryClient.setQueryData<PlayerStateView>(qk.player.state, {
						...prev,
						state: c.patch(prev.state)
					});
				}
				return { prev };
			},
			onError: (err, c, result) => {
				if (result?.prev) queryClient.setQueryData(qk.player.state, result.prev);
				toasts.error(`Could not ${c.label}`, errorMessage(err));
			},
			onSuccess: (_d, c) => {
				if (c.done) toasts.success(c.done);
			},
			onSettled: () => {
				// Ends this command's share of the shared hold (its cool-down
				// timer is module-wide, so it runs after an unmount too).
				holdEnd();
				const next = this.#queued;
				this.#queued = null;
				// After unmount: no queued slider change is sent.
				if (next && !this.#destroyed) {
					setTimeout(() => {
						if (!this.#destroyed) this.#cmd.mutate(next);
					});
				}
			}
		}));
		onDestroy(() => {
			this.#destroyed = true;
			this.#queued = null;
		});
	}

	/** True while a command runs (any instance) and for HOLD_MS after. */
	get holding(): boolean {
		return playerHolding();
	}
	get view(): PlayerStateView | undefined {
		return this.query.data;
	}
	get state(): PlaybackState | null {
		return this.query.data?.state ?? null;
	}
	/** The token has the playback scope (the state may still be null). */
	get canControl(): boolean {
		return !!this.query.data?.can_control;
	}
	/** Commands can be sent: the scope is granted and a device is active. */
	get control(): boolean {
		return this.canControl && !!this.state;
	}
	get busy(): boolean {
		return this.#cmd.isPending;
	}
	/** Where the track is now: ticks locally between polls while playing. */
	get progress(): number {
		const st = this.state;
		const item = st?.item;
		if (!st || !item) return 0;
		const elapsed = st.is_playing ? Math.max(0, secondClock.now - this.query.dataUpdatedAt) : 0;
		return Math.min(item.duration_ms, st.progress_ms + elapsed);
	}

	/**
	 * Runs a command. Controls stay enabled (and focused) while one runs: a
	 * click then is ignored, while a slider change (wait) is sent after it.
	 */
	run = (c: PlayerCmd, wait = false): void => {
		if (this.#cmd.isPending) {
			if (wait) this.#queued = c;
			return;
		}
		this.#cmd.mutate(c);
	};

	togglePlay = (): void => {
		const st = this.state;
		if (!st) return;
		if (st.is_playing) {
			const at = this.progress;
			this.run({
				label: 'pause',
				run: () => api.playerPause(),
				patch: (s) => ({ ...s, is_playing: false, progress_ms: at })
			});
		} else {
			this.run({
				label: 'resume playback',
				run: () => api.playerPlay(),
				patch: (s) => ({ ...s, is_playing: true })
			});
		}
	};

	previous = (): void => this.run({ label: 'go back', run: () => api.playerPrevious() });

	next = (): void => this.run({ label: 'skip to the next track', run: () => api.playerNext() });
}
