<script lang="ts">
	import { onDestroy } from 'svelte';
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
	import { api } from '#lib/api.js';
	import { debounce } from '#lib/debounce.js';
	import { artistNames, duration } from '#lib/format.js';
	import type { PlayerControl } from '#lib/player-control.svelte.js';
	import { nextRepeat, repeatDisallowKey, repeatLabel } from '#lib/player.js';
	import type { PlaybackState } from '#lib/types.js';

	// The loaded track, seek bar, transport buttons and volume of the player.
	let { ctl, st }: { ctl: PlayerControl; st: PlaybackState } = $props();

	const item = $derived(st.item);
	const durationMs = $derived(item?.duration_ms ?? 0);
	const control = $derived(ctl.control);
	const disallows = $derived(st.disallows ?? {});
	const nextMode = $derived(nextRepeat(st.repeat_state));

	// Sliders: local while dragging or using the arrow keys, sent once the
	// changes stop for a moment.
	let seekDraft = $state<number | null>(null);
	const shownProgress = $derived(seekDraft ?? ctl.progress);
	const sendSeek = debounce(() => {
		if (seekDraft == null) return;
		const ms = Math.round(seekDraft);
		seekDraft = null;
		ctl.run(
			{
				label: 'seek',
				run: () => api.playerSeek(ms),
				patch: (s) => ({ ...s, progress_ms: ms })
			},
			true
		);
	}, 300);

	const device = $derived(st.device);
	let volumeDraft = $state<number | null>(null);
	const volume = $derived(volumeDraft ?? device?.volume_percent ?? 0);
	const VolumeIcon = $derived(volume === 0 ? VolumeX : volume < 50 ? Volume1 : Volume2);
	const sendVolume = debounce(() => {
		if (volumeDraft == null) return;
		const pct = Math.round(volumeDraft);
		volumeDraft = null;
		ctl.run(
			{
				label: 'change the volume',
				run: () => api.playerVolume(pct),
				patch: (s) => (s.device ? { ...s, device: { ...s.device, volume_percent: pct } } : s)
			},
			true
		);
	}, 300);
	onDestroy(() => {
		sendSeek.cancel();
		sendVolume.cancel();
	});

	function toggleShuffle() {
		const on = !st.shuffle_state;
		ctl.run({
			label: 'change shuffle',
			run: () => api.playerShuffle(on),
			patch: (s) => ({ ...s, shuffle_state: on })
		});
	}

	function cycleRepeat() {
		const mode = nextMode;
		ctl.run({
			label: 'change repeat',
			run: () => api.playerRepeat(mode),
			patch: (s) => ({ ...s, repeat_state: mode })
		});
	}

	const uid = $props.id();
</script>

<div class="flex min-w-0 items-center gap-4">
	<AlbumArt
		src={item?.cover_image_url}
		alt={item?.album_name ? `Cover of ${item.album_name}` : ''}
		size={88}
		rounded="rounded-[var(--baoopn-radius)]"
	/>
	<div class="min-w-0 flex-1">
		{#if item}
			<p class="truncate text-lg font-semibold text-ink" dir="auto" title={item.name}>
				{item.name}
			</p>
			<p class="truncate text-sm text-ink-soft" dir="auto">{artistNames(item.artists)}</p>
			{#if item.album_name}
				<p class="truncate font-mono text-xs text-ink-soft" dir="auto">{item.album_name}</p>
			{/if}
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
		onclick={ctl.previous}
	>
		<SkipBack class="size-5" aria-hidden="true" />
	</button>
	<button
		type="button"
		class="btn btn-primary btn-icon size-12 rounded-full"
		aria-label={st.is_playing ? 'Pause' : 'Play'}
		disabled={!control || (st.is_playing ? disallows.pausing : disallows.resuming)}
		onclick={ctl.togglePlay}
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
		onclick={ctl.next}
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
