<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import ArtistLinks from './ArtistLinks.svelte';
	import BrandIcon from './BrandIcon.svelte';
	import ExplicitBadge from './ExplicitBadge.svelte';
	import RelTime from './RelTime.svelte';
	import { secondClock } from '#lib/clock.svelte.js';
	import { artistNames, duration, releaseYear, spotifyUrl, toDate } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';

	/** A paused track this old counts as "not listening". */
	const PAUSED_STALE_MS = 15 * 60_000;

	const np = $derived(live.nowPlaying);
	const stale = $derived.by(() => {
		if (!np?.track || np.is_playing) return false;
		const at = toDate(np.fetched_at)?.getTime() ?? 0;
		return live.serverNow() - at > PAUSED_STALE_MS;
	});
	const track = $derived(np?.track && !stale ? np.track : null);
	const playing = $derived(!!track && !!np?.is_playing);
	const durationMs = $derived(track?.duration_ms ?? 0);

	// Interpolated between server updates from the server's fetched_at instant
	// (skew-corrected), reading the 1 s clock only while playing. Clamped to
	// [0, duration] so a wrong system clock can't push it off either end.
	const progressMs = $derived.by(() => {
		if (!track || np?.progress_ms == null) return null;
		const base = np.progress_ms;
		const fetched = toDate(np.fetched_at)?.getTime();
		const elapsed =
			playing && fetched != null ? Math.max(0, live.serverNow(secondClock.now) - fetched) : 0;
		const p = base + elapsed;
		return Math.max(0, durationMs > 0 ? Math.min(p, durationMs) : p);
	});
	const ratio = $derived(progressMs != null && durationMs > 0 ? progressMs / durationMs : 0);

	const trackUrl = $derived(spotifyUrl(track?.external_url));
	const albumUrl = $derived(spotifyUrl(track?.album?.external_url));
	const year = $derived(releaseYear(track?.album?.release_date));
	const cover = $derived(track?.album?.cover_image_url);

	// Last played, for the idle state: the newest history entry, or the stale paused track.
	const lastPlayed = $derived(
		live.history[0]
			? { track: live.history[0].track, at: live.history[0].played_at }
			: np?.track
				? { track: np.track, at: np.fetched_at }
				: null
	);

	const announcement = $derived(
		track
			? `${playing ? 'Now playing' : 'Paused'}: ${track.name} by ${artistNames(track.artists)}`
			: live.nowStatus === 'ready'
				? 'Not listening right now'
				: ''
	);
</script>

{#snippet credit(label: string)}
	<dt class="label text-ink-soft">{label}</dt>
{/snippet}

<section
	aria-labelledby="now-heading"
	aria-busy={live.nowStatus === 'loading'}
	class={[
		'transition-opacity duration-200',
		// Brand guideline: dim slightly when nothing is playing.
		live.nowStatus === 'ready' && !playing && 'opacity-85'
	]}
>
	<h2 id="now-heading" class="sr-only">Now playing</h2>
	<p class="sr-only" aria-live="polite" aria-atomic="true">{announcement}</p>

	{#if live.nowStatus === 'loading'}
		<div class="hero">
			<div class="skeleton aspect-square w-full rounded-brand"></div>
			<div class="space-y-4">
				<div class="skeleton h-6 w-32 rounded-full"></div>
				<div class="skeleton h-12 w-4/5"></div>
				<div class="skeleton h-5 w-1/2"></div>
				<div class="skeleton mt-8 h-px w-full"></div>
			</div>
		</div>
	{:else if live.nowStatus === 'error'}
		<div class="border-y border-rule py-10" role="alert">
			<p class="chip label [--chip:var(--spot-rose)]">
				<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
				Couldn't load
			</p>
			<p class="mt-4 font-display text-2xl font-semibold tracking-tight">
				What's playing didn't come through.
			</p>
			<p class="mt-1 text-ink-soft">{live.nowError}</p>
			<button type="button" class="btn btn-secondary mt-6" onclick={() => live.retry()}
				>Try again</button
			>
		</div>
	{:else if track}
		<div class="hero">
			{#if albumUrl}
				<a
					href={albumUrl}
					target="_blank"
					rel="noopener noreferrer"
					class="block rounded-brand"
					aria-label="Album: {track.album?.name ?? track.name}, on Spotify"
				>
					<AlbumArt
						src={cover}
						alt="Album cover of {track.album?.name ?? track.name}"
						class="print w-full rounded-brand [--print:var(--spot-now)]"
						eager
					/>
				</a>
			{:else}
				<AlbumArt
					src={cover}
					alt="Album cover of {track.album?.name ?? track.name}"
					class="print w-full rounded-brand [--print:var(--spot-now)]"
					eager
				/>
			{/if}

			<div class="min-w-0">
				<p class={['chip label', !playing && '[--chip:var(--spot-butter)]']}>
					<BrandIcon name={playing ? 'now-playing' : 'paused'} tone="navy" class="size-4" />
					{playing ? 'Now playing' : 'Paused'}
				</p>

				<h3
					class="mt-5 font-display text-4xl leading-[1.05] font-semibold tracking-[-0.02em] text-balance break-words sm:text-5xl lg:text-6xl"
				>
					{#if trackUrl}
						<a href={trackUrl} target="_blank" rel="noopener noreferrer" class="link"
							>{track.name}</a
						>
					{:else}
						{track.name}
					{/if}
					{#if track.explicit}<span class="ml-1 inline-block align-middle"><ExplicitBadge /></span
						>{/if}
				</h3>

				<p class="mt-3 text-lg font-medium break-words sm:text-xl">
					<ArtistLinks artists={track.artists} />
				</p>

				<dl
					class="mt-6 grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 border-t border-rule pt-4 text-sm"
				>
					{#if track.album}
						{@render credit('Album')}
						<dd class="min-w-0 break-words">
							{#if albumUrl}
								<a href={albumUrl} target="_blank" rel="noopener noreferrer" class="link"
									>{track.album.name}</a
								>
							{:else}
								{track.album.name}
							{/if}
						</dd>
					{/if}
					{#if year}
						{@render credit('Year')}
						<dd class="font-mono tabular-nums">{year}</dd>
					{/if}
					{@render credit('Length')}
					<dd class="font-mono tabular-nums">{duration(durationMs)}</dd>
				</dl>

				<!-- Thin ink progress line with a dot. -->
				<div class="mt-6">
					<div
						class="relative h-3"
						role="progressbar"
						aria-label="Playback progress"
						aria-valuemin={0}
						aria-valuemax={Math.round(durationMs / 1000)}
						aria-valuenow={progressMs == null ? undefined : Math.round(progressMs / 1000)}
						aria-valuetext={progressMs == null
							? undefined
							: `${duration(progressMs)} of ${duration(durationMs)}`}
					>
						<div class="absolute inset-x-0 top-1/2 h-px bg-rule-strong"></div>
						<div
							class="fill absolute top-1/2 left-0 h-0.5 w-full origin-left bg-ink"
							style:transform="translateY(-50%) scaleX({ratio})"
						></div>
						<div
							class="dot absolute top-1/2 size-3 rounded-full border-2 border-paper bg-ink"
							style:left="calc({ratio * 100}% - 0.375rem)"
						></div>
					</div>
					<div
						class="mt-2 flex justify-between font-mono text-xs text-ink-soft tabular-nums"
						aria-hidden="true"
					>
						<span>{duration(progressMs)}</span>
						<span>{duration(durationMs)}</span>
					</div>
				</div>

				{#if trackUrl}
					<a href={trackUrl} target="_blank" rel="noopener noreferrer" class="btn btn-primary mt-7">
						<svg viewBox="0 0 24 24" class="size-5" fill="currentColor" aria-hidden="true">
							<path
								d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm4.6 14.4a.6.6 0 0 1-.9.2c-2.4-1.5-5.3-1.8-8.8-1a.6.6 0 1 1-.3-1.2c3.8-.9 7.1-.5 9.8 1.1.3.2.4.6.2.9Zm1.2-2.7a.8.8 0 0 1-1 .3c-2.7-1.7-6.9-2.2-10.2-1.2a.8.8 0 1 1-.4-1.5c3.7-1.1 8.4-.6 11.5 1.4.3.2.4.7.1 1Zm.1-2.8C14.6 9 9.4 8.8 6.3 9.7a.9.9 0 1 1-.5-1.8c3.5-1.1 9.3-.9 13 1.3a.9.9 0 0 1-.9 1.6Z"
							/>
						</svg>
						Listen on Spotify
					</a>
				{/if}
			</div>
		</div>
	{:else}
		<div class="hero">
			<img
				src="/logo/listening-mark-primary.svg"
				alt=""
				width="200"
				height="200"
				class="print aspect-square w-full rounded-brand [--print:var(--spot-now)]"
			/>
			<div class="min-w-0">
				<p class="chip label [--chip:var(--spot-butter)]">
					<BrandIcon name="paused" tone="navy" class="size-4" />
					Off air
				</p>
				<h3
					class="mt-5 font-display text-4xl leading-[1.05] font-semibold tracking-[-0.02em] sm:text-5xl"
				>
					Not listening right now
				</h3>
				<p class="mt-3 flex items-center gap-2 text-ink-soft">
					<BrandIcon name="headphones" class="size-5" />
					Headphones are off. Here's what played last.
				</p>
				{#if lastPlayed}
					{@const lpUrl = spotifyUrl(lastPlayed.track.external_url)}
					<div class="mt-6 flex items-center gap-4 border-t border-rule pt-4">
						<AlbumArt
							src={lastPlayed.track.album?.cover_image_url}
							class="size-16 rounded-[8px] border border-ink"
						/>
						<div class="min-w-0 flex-1">
							<p class="label text-ink-soft">Last played · <RelTime value={lastPlayed.at} /></p>
							<p class="mt-1 truncate font-semibold">
								{#if lpUrl}
									<a href={lpUrl} target="_blank" rel="noopener noreferrer" class="link"
										>{lastPlayed.track.name}</a
									>
								{:else}
									{lastPlayed.track.name}
								{/if}
							</p>
							<p class="truncate text-sm text-ink-soft">{artistNames(lastPlayed.track.artists)}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	{/if}
</section>

<style>
	/* Asymmetric editorial grid: art left, credits right; stacks on mobile. */
	.hero {
		display: grid;
		gap: 2rem;
		align-items: center;
	}
	.hero > :global(:first-child) {
		width: 100%;
		max-width: 22rem;
	}
	@media (min-width: 48rem) {
		.hero {
			grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
			gap: 3.5rem;
		}
		.hero > :global(:first-child) {
			max-width: none;
		}
	}
	.fill,
	.dot {
		transition:
			transform 1s linear,
			left 1s linear;
	}
</style>
