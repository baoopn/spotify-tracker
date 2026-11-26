<script lang="ts">
	import { onDestroy } from 'svelte';
	import { resolve } from '$app/paths';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import {
		Pause,
		Play,
		Repeat,
		Repeat1,
		Shuffle,
		SkipBack,
		SkipForward,
		Volume1,
		Volume2,
		VolumeX
	} from '@lucide/svelte';
	import AlbumArt from '../AlbumArt.svelte';
	import Skeleton from '../Skeleton.svelte';
	import PlayerDevices from './PlayerDevices.svelte';
	import PlayerQueue from './PlayerQueue.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { secondClock } from '#lib/clock.svelte.js';
	import { debounce } from '#lib/debounce.js';
	import { artistNames, duration } from '#lib/format.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import { nextRepeat, repeatDisallowKey, repeatLabel } from '#lib/player.js';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { PlaybackState, PlayerStateView, PlayerTrack } from '#lib/types.js';

	// The owner's Spotify player: transport, device and queue. The state is
	// polled every 5 s, the devices and queue every 25 s. Commands update the
	// cache at once (optimistic) and hold the polls while they run and for
	// HOLD_MS after: Spotify's GET /me/player lags 0.5–2 s behind a command,
	// so an earlier poll would undo the optimistic state. Releasing the hold
	// refetches both queries.
	const HOLD_MS = 1500;
	let holding = $state(false);
	let holdTimer: ReturnType<typeof setTimeout> | undefined;

	const queryClient = useQueryClient();
	const query = createQuery(() => ({ ...queries.playerState(), enabled: !holding }));
	const extras = createQuery(() => ({ ...queries.playerExtras(), enabled: !holding }));
	const v = $derived(query.data);
	const st = $derived(v?.state ?? null);
	const item = $derived(st?.item ?? null);
	const canControl = $derived(!!v?.can_control);
	// The refresh button spins for its own refetch only, not for the polls.
	const manual = new ManualRefetch(() => Promise.all([query.refetch(), extras.refetch()]));

	type Cmd = {
		/** "pause", "skip to the next track": used in the error toast. */
		label: string;
		run: () => Promise<unknown>;
		patch?: (s: PlaybackState) => PlaybackState;
		done?: string;
	};

	// A slider change made while a command runs waits for it (latest wins).
	let queued: Cmd | null = null;
	const cmd = createMutation(() => ({
		mutationFn: (c: Cmd) => c.run(),
		onMutate: async (c: Cmd) => {
			holding = true;
			clearTimeout(holdTimer);
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
			const next = queued;
			queued = null;
			if (next) {
				setTimeout(() => cmd.mutate(next));
				return;
			}
			clearTimeout(holdTimer);
			holdTimer = setTimeout(() => (holding = false), HOLD_MS);
		}
	}));
	const busy = $derived(cmd.isPending);

	/**
	 * Runs a command. Controls stay enabled (and focused) while one runs: a
	 * click then is ignored, while a slider change (wait) is sent after it.
	 */
	function run(c: Cmd, wait = false) {
		if (cmd.isPending) {
			if (wait) queued = c;
			return;
		}
		cmd.mutate(c);
	}

	// A new track can change the queue and the devices: refresh them.
	let lastItemId: string | null | undefined;
	$effect(() => {
		const id = item?.id ?? null;
		if (lastItemId !== undefined && id !== lastItemId) {
			queryClient.invalidateQueries({ queryKey: qk.player.extras, exact: true });
		}
		lastItemId = id;
	});

	// Progress ticks locally between polls (not without a track).
	const durationMs = $derived(item?.duration_ms ?? 0);
	const progress = $derived.by(() => {
		if (!st || !item) return 0;
		const elapsed = st.is_playing ? Math.max(0, secondClock.now - query.dataUpdatedAt) : 0;
		return Math.min(durationMs, st.progress_ms + elapsed);
	});

	// Sliders: local while dragging or using the arrow keys, sent once the
	// changes stop for a moment.
	let seekDraft = $state<number | null>(null);
	const shownProgress = $derived(seekDraft ?? progress);
	const sendSeek = debounce(() => {
		if (seekDraft == null) return;
		const ms = Math.round(seekDraft);
		seekDraft = null;
		run(
			{
				label: 'seek',
				run: () => api.playerSeek(ms),
				patch: (s) => ({ ...s, progress_ms: ms })
			},
			true
		);
	}, 300);

	const device = $derived(st?.device ?? null);
	let volumeDraft = $state<number | null>(null);
	const volume = $derived(volumeDraft ?? device?.volume_percent ?? 0);
	const VolumeIcon = $derived(volume === 0 ? VolumeX : volume < 50 ? Volume1 : Volume2);
	const sendVolume = debounce(() => {
		if (volumeDraft == null) return;
		const pct = Math.round(volumeDraft);
		volumeDraft = null;
		run(
			{
				label: 'change the volume',
				run: () => api.playerVolume(pct),
				patch: (s) => (s.device ? { ...s, device: { ...s.device, volume_percent: pct } } : s)
			},
			true
		);
	}, 300);
	onDestroy(() => {
		clearTimeout(holdTimer);
		sendSeek.cancel();
		sendVolume.cancel();
	});

	const disallows = $derived(st?.disallows ?? {});
	const control = $derived(canControl && !!st);
	const nextMode = $derived(st ? nextRepeat(st.repeat_state) : 'off');

	function togglePlay() {
		if (!st) return;
		if (st.is_playing) {
			const at = progress;
			run({
				label: 'pause',
				run: () => api.playerPause(),
				patch: (s) => ({ ...s, is_playing: false, progress_ms: at })
			});
		} else {
			run({
				label: 'resume playback',
				run: () => api.playerPlay(),
				patch: (s) => ({ ...s, is_playing: true })
			});
		}
	}

	function toggleShuffle() {
		if (!st) return;
		const on = !st.shuffle_state;
		run({
			label: 'change shuffle',
			run: () => api.playerShuffle(on),
			patch: (s) => ({ ...s, shuffle_state: on })
		});
	}

	function cycleRepeat() {
		const mode = nextMode;
		run({
			label: 'change repeat',
			run: () => api.playerRepeat(mode),
			patch: (s) => ({ ...s, repeat_state: mode })
		});
	}

	/** Search result: play just this track. */
	function playTrack(t: PlayerTrack) {
		run({
			label: `play “${t.name}”`,
			run: () => api.playerPlay({ uris: [t.uri] }),
			patch: (s) => ({ ...s, is_playing: true, progress_ms: 0, item: t })
		});
	}

	/** Queue row: play from it and keep the rest of the queue after it. */
	function playFromQueue(i: number) {
		const rest = (extras.data?.queue ?? [])
			.slice(i)
			.filter((t) => t.uri.startsWith('spotify:track:'));
		const t = rest[0];
		if (!t) return;
		run({
			label: `play “${t.name}”`,
			run: () => api.playerPlay({ uris: rest.slice(0, 100).map((x) => x.uri) }),
			patch: (s) => ({ ...s, is_playing: true, progress_ms: 0, item: t })
		});
	}

	function queueTrack(t: PlayerTrack) {
		run({
			label: `queue “${t.name}”`,
			run: () => api.playerQueue(t.uri),
			done: `Queued “${t.name}”`
		});
	}

	function transfer(id: string) {
		// Keep the playing state; with nothing active, start playing there.
		const play = st ? st.is_playing : true;
		run({ label: 'switch the device', run: () => api.playerTransfer(id, play) });
	}

	const uid = $props.id();
	const settingsHref = resolve('settings/spotify');
</script>

<section
	class="mt-8 flex flex-col gap-4 border-t border-ink pt-4"
	aria-labelledby="{uid}-title"
	aria-busy={busy}
>
	<div class="flex items-center justify-between gap-2">
		<h2 id="{uid}-title" class="eyebrow">Player</h2>
		{#if device}
			<span class="truncate font-mono text-xs text-ink-soft" title={device.name}>
				on {device.name}
			</span>
		{/if}
	</div>

	{#if !v}
		{#if query.isError}
			<p class="text-sm text-ink-soft">
				The player is unavailable: {errorMessage(query.error)}
			</p>
		{:else}
			<div class="flex items-center gap-3">
				<Skeleton class="size-14" />
				<div class="flex flex-1 flex-col gap-2">
					<Skeleton class="h-4 w-1/2" /><Skeleton class="h-3 w-1/3" />
				</div>
			</div>
			<Skeleton class="h-2 w-full" />
			<div class="flex justify-center gap-3">
				<Skeleton class="size-10" /><Skeleton class="size-12" /><Skeleton class="size-10" />
			</div>
		{/if}
	{:else if !v.connected}
		<p class="text-sm text-ink-soft">
			Spotify is not connected. <a
				href={settingsHref}
				class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				>Connect it in Settings</a
			>.
		</p>
	{:else}
		{#if !canControl}
			<div class="card flex flex-col gap-1 p-3 text-sm" role="note">
				<p class="font-semibold text-ink">Playback control is not authorized</p>
				<p class="text-ink-soft">
					The Spotify token was granted before playback control existed. <a
						href={settingsHref}
						class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
						>Re-authorize Spotify</a
					> to use these controls.
				</p>
			</div>
		{/if}

		{#if st}
			<div class="flex min-w-0 items-center gap-3">
				<AlbumArt
					src={item?.cover_image_url}
					alt={item?.album_name ? `Cover of ${item.album_name}` : ''}
					size={56}
				/>
				<div class="min-w-0 flex-1">
					{#if item}
						<p class="truncate font-medium text-ink" dir="auto" title={item.name}>{item.name}</p>
						<p class="truncate text-sm text-ink-soft" dir="auto">{artistNames(item.artists)}</p>
					{:else}
						<p class="font-medium text-ink">
							{st.currently_playing_type === 'episode'
								? 'Playing a podcast episode'
								: st.currently_playing_type === 'ad'
									? 'An ad is playing'
									: 'Nothing loaded'}
						</p>
					{/if}
				</div>
			</div>

			<div class="flex flex-col gap-1">
				<input
					type="range"
					class="slider w-full"
					min="0"
					max={Math.max(1, durationMs)}
					step="1000"
					value={shownProgress}
					disabled={!control || !item || disallows.seeking}
					aria-label="Seek"
					aria-valuetext="{duration(shownProgress)} of {duration(durationMs)}"
					oninput={(e) => (seekDraft = Number(e.currentTarget.value))}
					onchange={() => sendSeek()}
				/>
				<div class="flex justify-between font-mono text-xs text-ink-soft">
					<span>{duration(shownProgress)}</span>
					<span>{duration(durationMs)}</span>
				</div>
			</div>

			<div class="flex flex-wrap items-center justify-center gap-2">
				<button
					type="button"
					class={['btn btn-ghost btn-icon', st.shuffle_state && 'toggle-on']}
					aria-label="Shuffle"
					aria-pressed={st.shuffle_state}
					title={st.shuffle_state ? 'Shuffle is on' : 'Shuffle is off'}
					disabled={!control || disallows.toggling_shuffle}
					onclick={toggleShuffle}
				>
					<Shuffle class="size-5" aria-hidden="true" />
				</button>
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					aria-label="Previous track"
					disabled={!control || disallows.skipping_prev}
					onclick={() => run({ label: 'go back', run: () => api.playerPrevious() })}
				>
					<SkipBack class="size-5" aria-hidden="true" />
				</button>
				<button
					type="button"
					class="btn btn-primary btn-icon size-12 rounded-full"
					aria-label={st.is_playing ? 'Pause' : 'Play'}
					disabled={!control || (st.is_playing ? disallows.pausing : disallows.resuming)}
					onclick={togglePlay}
				>
					{#if st.is_playing}
						<Pause class="size-5" aria-hidden="true" />
					{:else}
						<Play class="size-5" aria-hidden="true" />
					{/if}
				</button>
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					aria-label="Next track"
					disabled={!control || disallows.skipping_next}
					onclick={() => run({ label: 'skip to the next track', run: () => api.playerNext() })}
				>
					<SkipForward class="size-5" aria-hidden="true" />
				</button>
				<button
					type="button"
					class={['btn btn-ghost btn-icon', st.repeat_state !== 'off' && 'toggle-on']}
					aria-label="Repeat"
					aria-describedby="{uid}-repeat"
					title={repeatLabel[st.repeat_state]}
					disabled={!control || disallows[repeatDisallowKey(nextMode)]}
					onclick={cycleRepeat}
				>
					{#if st.repeat_state === 'track'}
						<Repeat1 class="size-5" aria-hidden="true" />
					{:else}
						<Repeat class="size-5" aria-hidden="true" />
					{/if}
				</button>
				<span id="{uid}-repeat" class="sr-only">{repeatLabel[st.repeat_state]}</span>
			</div>

			{#if device?.supports_volume}
				<div class="flex items-center gap-3">
					<VolumeIcon class="size-4 shrink-0 text-ink-soft" aria-hidden="true" />
					<input
						type="range"
						class="slider min-w-0 flex-1"
						min="0"
						max="100"
						step="1"
						value={volume}
						disabled={!control}
						aria-label="Volume"
						aria-valuetext="{volume}%"
						oninput={(e) => (volumeDraft = Number(e.currentTarget.value))}
						onchange={() => sendVolume()}
					/>
					<span class="w-9 shrink-0 text-right font-mono text-xs text-ink-soft">{volume}%</span>
				</div>
			{/if}
		{:else}
			<p class="text-sm text-ink-soft">
				No device is playing right now. Pick a device to start playback.
			</p>
		{/if}

		<PlayerDevices
			devices={extras.data ? extras.data.devices : undefined}
			activeId={device?.id ?? null}
			disabled={!canControl}
			refreshing={manual.active}
			onpick={transfer}
			onrefresh={() => manual.run()}
		/>

		{#if st}
			<PlayerQueue
				queue={extras.data ? extras.data.queue : undefined}
				disabled={!canControl}
				onplay={playTrack}
				onplayfrom={playFromQueue}
				onqueue={queueTrack}
			/>
		{/if}
	{/if}
</section>

<style>
	.slider {
		accent-color: var(--ink);
		height: 1.25rem;
		cursor: pointer;
	}
	.slider:disabled {
		cursor: not-allowed;
		opacity: 0.5;
	}
	.toggle-on {
		background: var(--spot-select);
		color: var(--on-spot);
	}
</style>
