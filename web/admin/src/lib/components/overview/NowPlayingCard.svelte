<script lang="ts">
	import OutLink from '../OutLink.svelte';
	import { ExternalLink } from '@lucide/svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import ListeningIcon from '../ListeningIcon.svelte';
	import AlbumArt from '../AlbumArt.svelte';
	import RelTime from '../RelTime.svelte';
	import { secondClock } from '#lib/clock.svelte.js';
	import { artistNames, duration, releaseYear } from '#lib/format.js';
	import { NowPlayingStream, snapshot } from '#lib/now-playing.svelte.js';
	import type { NowPlaying } from '#lib/types.js';

	let { initial }: { initial: NowPlaying | null } = $props();

	// Seeded from the overview response; live SSE events take over once they arrive.
	const stream = new NowPlayingStream();
	const snap = $derived(stream.latest ?? snapshot(initial));
	const live = $derived(stream.status);

	const track = $derived(snap.np?.track ?? null);
	const playing = $derived(!!snap.np?.is_playing && !!track);
	const durationMs = $derived(track?.duration_ms ?? 0);
	const progress = $derived.by(() => {
		const base = snap.np?.progress_ms ?? 0;
		const elapsed = playing ? Math.max(0, secondClock.now - snap.at) : 0;
		return Math.min(durationMs || Infinity, base + elapsed);
	});
	const ratio = $derived(durationMs > 0 ? Math.min(1, progress / durationMs) : 0);

	const reducedMotion = new MediaQuery('prefers-reduced-motion: reduce');
	const statusIcon = $derived(
		playing ? (reducedMotion.current ? 'now-playing-static' : 'now-playing') : 'paused'
	);
</script>

<section
	class={['card-print np p-5 transition-opacity duration-200 sm:p-6', !playing && 'opacity-85']}
	aria-labelledby="np-title"
>
	<div class="flex items-center justify-between gap-3">
		<h2 id="np-title" class="eyebrow flex items-center gap-2 text-ink">
			<ListeningIcon name={statusIcon} class="size-4" />
			{playing ? 'Now playing' : track ? 'Paused' : 'Now playing'}
		</h2>
		<span class="eyebrow" title="Live updates via Server-Sent Events" aria-live="polite">
			{live === 'live' ? 'Live' : live === 'connecting' ? 'Connecting…' : 'Reconnecting…'}
		</span>
	</div>

	{#if track}
		<div class="mt-5 flex flex-col gap-5 sm:flex-row sm:items-end">
			<AlbumArt
				src={track.album?.cover_image_url}
				alt={track.album?.name ? `Cover of ${track.album.name}` : ''}
				size={136}
				rounded="rounded-[var(--baoopn-radius)]"
			/>
			<div class="min-w-0 flex-1">
				<p
					class="font-display text-3xl leading-tight font-semibold tracking-tight text-ink"
					title={track.name}
				>
					<span class="line-clamp-2">{track.name}</span>
				</p>
				<p class="mt-1 truncate text-base text-ink">{artistNames(track.artists)}</p>
				{#if track.album?.name}
					<p class="truncate font-mono text-xs text-ink-soft">
						{track.album.name}{#if track.album.release_date}
							· {releaseYear(track.album.release_date)}{/if}
					</p>
				{/if}

				<!-- Thin ink progress line with a dot -->
				<div
					class="relative mt-5 h-3"
					role="progressbar"
					aria-label="Playback progress"
					aria-valuemin={0}
					aria-valuemax={Math.round(durationMs / 1000)}
					aria-valuenow={Math.round(progress / 1000)}
					aria-valuetext="{duration(progress)} of {duration(durationMs)}"
				>
					<span class="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-rule-strong"></span>
					<span
						class="progress-fill absolute top-1/2 left-0 h-0.5 -translate-y-1/2 bg-ink"
						style:width="{ratio * 100}%"
					></span>
					<span
						class="progress-fill absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-paper-2 bg-ink"
						style:left="{ratio * 100}%"
					></span>
				</div>
				<div class="mt-1 flex justify-between font-mono text-xs text-ink-soft">
					<span>{duration(progress)}</span>
					<span>{duration(durationMs)}</span>
				</div>
			</div>
		</div>
		<div class="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-rule pt-3">
			{#if snap.np?.fetched_at}
				<p class="font-mono text-xs text-ink-soft">
					polled <RelTime value={snap.np.fetched_at} />
				</p>
			{/if}
			{#if track.external_url}
				<OutLink
					href={track.external_url}
					class="inline-flex items-center gap-1 text-sm font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
					>Open in Spotify <ExternalLink class="size-3.5" aria-hidden="true" /></OutLink
				>
			{/if}
		</div>
	{:else}
		<div class="mt-5 flex items-center gap-5">
			<AlbumArt size={88} rounded="rounded-[var(--baoopn-radius)]" />
			<div>
				<p class="font-display text-2xl font-semibold text-ink">Nothing on right now</p>
				<p class="text-sm text-ink-soft">This updates by itself when Bao presses play.</p>
			</div>
		</div>
	{/if}
</section>

<style>
	.np {
		box-shadow: var(--print-shadow-spot);
	}
	.progress-fill {
		transition:
			width 1s linear,
			left 1s linear;
	}
</style>
