<script lang="ts">
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
	import { artistNames, duration } from '#lib/format.js';
	import { nextRepeat, repeatLabel } from '#lib/player.js';
	import { qk, queries } from '#lib/queries.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { PlaybackState, PlayerTrack, PlayerView } from '#lib/types.js';

	// The owner's Spotify player: transport, device and queue. Polled every
	// 3 s while visible; commands update the cache at once (optimistic) and
	// refetch shortly after, once Spotify has caught up.
	const queryClient = useQueryClient();
	const query = createQuery(() => queries.player());
	const v = $derived(query.data);
	const st = $derived(v?.state ?? null);
	const item = $derived(st?.item ?? null);
	const canControl = $derived(!!v?.can_control);
	// The refresh button spins for its own refetch only, not for the polls.
	const manual = new ManualRefetch(() => query.refetch());

	type Cmd = {
		/** "pause", "skip to the next track": used in the error toast. */
		label: string;
		run: () => Promise<unknown>;
		patch?: (s: PlaybackState) => PlaybackState;
		done?: string;
	};

	let settleTimer: ReturnType<typeof setTimeout> | undefined;
	const cmd = createMutation(() => ({
		mutationFn: (c: Cmd) => c.run(),
		onMutate: async (c: Cmd) => {
			await queryClient.cancelQueries({ queryKey: qk.player });
			const prev = queryClient.getQueryData<PlayerView>(qk.player);
			if (prev?.state && c.patch) {
				queryClient.setQueryData<PlayerView>(qk.player, { ...prev, state: c.patch(prev.state) });
			}
			return { prev };
		},
		onError: (err, c, result) => {
			if (result?.prev) queryClient.setQueryData(qk.player, result.prev);
			toasts.error(`Could not ${c.label}`, errorMessage(err));
		},
		onSuccess: (_d, c) => {
			if (c.done) toasts.success(c.done);
		},
		onSettled: () => {
			clearTimeout(settleTimer);
			settleTimer = setTimeout(
				() => queryClient.invalidateQueries({ queryKey: qk.player, exact: true }),
				500
			);
		}
	}));
	$effect(() => () => clearTimeout(settleTimer));
	const busy = $derived(cmd.isPending);
	const run = (c: Cmd) => cmd.mutate(c);

	// Progress ticks locally between polls.
	const durationMs = $derived(item?.duration_ms ?? 0);
	const progress = $derived.by(() => {
		if (!st) return 0;
		const elapsed = st.is_playing ? Math.max(0, secondClock.now - query.dataUpdatedAt) : 0;
		return Math.min(durationMs || Infinity, st.progress_ms + elapsed);
	});
	// While dragging, the bar follows the pointer instead of the clock.
	let seekDraft = $state<number | null>(null);
	const shownProgress = $derived(seekDraft ?? progress);

	function seek() {
		if (seekDraft == null) return;
		const ms = Math.round(seekDraft);
		seekDraft = null;
		run({
			label: 'seek',
			run: () => api.playerSeek(ms),
			patch: (s) => ({ ...s, progress_ms: ms })
		});
	}

	const disallows = $derived(st?.disallows ?? {});
	const control = $derived(canControl && !!st && !busy);

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

	// Volume: the slider is local while dragging and sent on release.
	const device = $derived(st?.device ?? null);
	let volumeDraft = $state<number | null>(null);
	const volume = $derived(volumeDraft ?? device?.volume_percent ?? 0);
	const VolumeIcon = $derived(volume === 0 ? VolumeX : volume < 50 ? Volume1 : Volume2);

	function setVolume() {
		if (volumeDraft == null) return;
		const pct = Math.round(volumeDraft);
		volumeDraft = null;
		run({
			label: 'change the volume',
			run: () => api.playerVolume(pct),
			patch: (s) => (s.device ? { ...s, device: { ...s.device, volume_percent: pct } } : s)
		});
	}

	function playTrack(t: PlayerTrack) {
		run({
			label: `play “${t.name}”`,
			run: () => api.playerPlay({ uris: [t.uri] }),
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

	const settingsHref = resolve('settings/spotify');
</script>

<section class="mt-8 flex flex-col gap-4 border-t border-ink pt-4" aria-labelledby="pl-title">
	<div class="flex items-center justify-between gap-2">
		<h2 id="pl-title" class="eyebrow">Player</h2>
		{#if device}
			<span class="truncate font-mono text-xs text-ink-soft" title={device.name}>
				on {device.name}
			</span>
		{/if}
	</div>

	{#if !v}
		{#if query.isError}
			<p class="text-sm text-muted">
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
					onchange={seek}
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
					onclick={() => {
						const on = !st.shuffle_state;
						run({
							label: 'change shuffle',
							run: () => api.playerShuffle(on),
							patch: (s) => ({ ...s, shuffle_state: on })
						});
					}}
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
					aria-label="Repeat: {repeatLabel[st.repeat_state]}"
					title={repeatLabel[st.repeat_state]}
					disabled={!control ||
						disallows.toggling_repeat_context ||
						disallows.toggling_repeat_track}
					onclick={() => {
						const mode = nextRepeat(st.repeat_state);
						run({
							label: 'change repeat',
							run: () => api.playerRepeat(mode),
							patch: (s) => ({ ...s, repeat_state: mode })
						});
					}}
				>
					{#if st.repeat_state === 'track'}
						<Repeat1 class="size-5" aria-hidden="true" />
					{:else}
						<Repeat class="size-5" aria-hidden="true" />
					{/if}
				</button>
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
						onchange={setVolume}
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
			devices={v.devices}
			activeId={device?.id ?? null}
			disabled={!canControl || busy}
			refreshing={manual.active}
			onpick={transfer}
			onrefresh={() => manual.run()}
		/>

		{#if st}
			<PlayerQueue
				queue={v.queue}
				disabled={!canControl || busy}
				onplay={playTrack}
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
