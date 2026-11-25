<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import ArtistLinks from './ArtistLinks.svelte';
	import BrandIcon from './BrandIcon.svelte';
	import ExplicitBadge from './ExplicitBadge.svelte';
	import LayoutToggle from './LayoutToggle.svelte';
	import NowLyrics from './NowLyrics.svelte';
	import RelTime from './RelTime.svelte';
	import { estimatePositionMs } from '#lib/playback.js';
	import { tick } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import { frameClock, secondClock } from '#lib/clock.svelte.js';
	import { artistNames, duration, releaseYear, spotifyUrl, toDate } from '#lib/format.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { live } from '#lib/live.svelte.js';
	import { lyricsLayout, type LyricsLayout } from '#lib/lyricsLayout.svelte.js';
	import { historyQuery, loadStatus, nowLyricsQuery, nowPlayingQuery } from '#lib/queries.js';

	/** A paused track this old counts as "not listening". */
	const PAUSED_STALE_MS = 15 * 60_000;

	const now = createQuery(() => nowPlayingQuery());
	const history = createQuery(() => historyQuery());
	const nowStatus = $derived(loadStatus(now));
	const np = $derived(now.data ?? null);
	const stale = $derived.by(() => {
		if (!np?.track || np.is_playing) return false;
		const at = toDate(np.fetched_at)?.getTime() ?? 0;
		return live.serverNow() - at > PAUSED_STALE_MS;
	});
	const track = $derived(np?.track && !stale ? np.track : null);
	const playing = $derived(!!track && !!np?.is_playing);
	const durationMs = $derived(track?.duration_ms ?? 0);

	// Interpolated between server updates from the server's fetched_at instant
	// (skew-corrected). While playing it reads a per-frame clock so the bar
	// glides; with reduced motion it steps once a second instead. Clamped to
	// [0, duration] so a wrong system clock can't push it off either end.
	const reducedMotion = new MediaQuery('prefers-reduced-motion: reduce');
	const progressMs = $derived.by(() => {
		if (!track || !np) return null;
		const now = playing ? (reducedMotion.current ? secondClock.now : frameClock.now) : 0;
		return estimatePositionMs({ ...np, is_playing: playing }, live.serverNow(now), durationMs);
	});
	const ratio = $derived(progressMs != null && durationMs > 0 ? progressMs / durationMs : 0);

	const trackUrl = $derived(spotifyUrl(track?.external_url));
	const albumUrl = $derived(spotifyUrl(track?.album?.external_url));
	const year = $derived(releaseYear(track?.album?.release_date));
	const cover = $derived(track?.album?.cover_image_url);

	// Lyrics focus layout: offered while the song has lyrics to read, or they
	// are on their way. Same query as NowLyrics (shared cache, one request).
	const lyrics = createQuery(() => nowLyricsQuery(track?.id ?? null, playing));
	const ld = $derived(lyrics.data && lyrics.data.track_id === track?.id ? lyrics.data : null);
	const canFocus = $derived(
		playing &&
			(ld
				? ld.status === 'pending' ||
					(ld.status === 'found' && (ld.synced ? ld.lines.length > 0 : !!ld.plain))
				: lyrics.isPending && !lyrics.isError)
	);
	// No lyrics: the standard card, without changing the saved choice.
	const focus = $derived(canFocus && lyricsLayout.value === 'lyrics');

	// Keep the pressed toggle where it was on screen (the layouts differ in
	// height), and keep keyboard focus on it.
	async function setLayout(v: LyricsLayout, from: HTMLElement) {
		const before = from.getBoundingClientRect().top;
		lyricsLayout.value = v;
		await tick();
		const group = document.querySelector<HTMLElement>('[data-layout-toggle]');
		if (!group) return;
		window.scrollBy({ top: group.getBoundingClientRect().top - before, behavior: 'instant' });
		group.querySelector<HTMLElement>('[aria-pressed="true"]')?.focus({ preventScroll: true });
	}

	// Last played, for the idle state: the newest history entry, or the stale paused track.
	const lastPlayed = $derived(
		history.data?.[0]
			? { track: history.data[0].track, at: history.data[0].played_at }
			: np?.track
				? { track: np.track, at: np.fetched_at }
				: null
	);

	const announcement = $derived(
		track
			? `${playing ? 'Now playing' : 'Paused'}: ${track.name} by ${artistNames(track.artists)}`
			: nowStatus === 'ready'
				? 'Not listening right now'
				: ''
	);
</script>

{#snippet credit(label: string)}
	<dt class="label text-ink-soft">{label}</dt>
{/snippet}

{#snippet statusRow()}
	<div class="flex flex-wrap items-center justify-between gap-3">
		<p class={['chip label', !playing && '[--chip:var(--spot-butter)]']}>
			<BrandIcon name={playing ? 'now-playing' : 'paused'} tone="navy" class="size-4" />
			{playing ? 'Now playing' : 'Paused'}
		</p>
		{#if canFocus}
			<LayoutToggle value={focus ? 'lyrics' : 'standard'} onchange={setLayout} />
		{/if}
	</div>
{/snippet}

{#snippet coverArt(cls: string)}
	{#if track}
		{#if albumUrl}
			<a
				href={albumUrl}
				target="_blank"
				rel="noopener noreferrer"
				class="block shrink-0 rounded-brand"
				aria-label="Album: {track.album?.name ?? track.name}, on Spotify"
			>
				<AlbumArt
					src={cover}
					alt="Album cover of {track.album?.name ?? track.name}"
					class={cls}
					eager
				/>
			</a>
		{:else}
			<AlbumArt
				src={cover}
				alt="Album cover of {track.album?.name ?? track.name}"
				class={cls}
				eager
			/>
		{/if}
	{/if}
{/snippet}

{#snippet title(cls: string)}
	{#if track}
		<h3
			class={[
				'font-display leading-[1.05] font-semibold tracking-[-0.02em] text-balance break-words',
				cls
			]}
		>
			{#if trackUrl}
				<a href={trackUrl} target="_blank" rel="noopener noreferrer" class="link">{track.name}</a>
			{:else}
				{track.name}
			{/if}
			{#if track.explicit}<span class="ml-1 inline-block align-middle"><ExplicitBadge /></span>{/if}
		</h3>
	{/if}
{/snippet}

{#snippet progress()}
	<!-- Thin ink progress line with a dot. -->
	<div
		class="@container relative h-3"
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
		<!-- Zero-width anchor moved with transform (compositor only); the bar is
		     a size container, so 100cqw is the bar's width and the anchor never
		     extends past it (only the dot's half-width can). -->
		<div
			class="knob pointer-events-none absolute inset-y-0 left-0 w-0"
			style:transform="translateX(calc({ratio} * 100cqw))"
		>
			<div
				class="absolute top-1/2 -left-1.5 size-3 -translate-y-1/2 rounded-full border-2 border-paper bg-ink"
			></div>
		</div>
	</div>
	<div
		class="mt-2 flex justify-between font-mono text-xs text-ink-soft tabular-nums"
		aria-hidden="true"
	>
		<span>{duration(progressMs)}</span>
		<span>{duration(durationMs)}</span>
	</div>
{/snippet}

{#snippet spotifyButton(cls: string)}
	{#if trackUrl}
		<a href={trackUrl} target="_blank" rel="noopener noreferrer" class={['btn btn-primary', cls]}>
			<svg viewBox="0 0 24 24" class="size-5" fill="currentColor" aria-hidden="true">
				<path
					d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm4.6 14.4a.6.6 0 0 1-.9.2c-2.4-1.5-5.3-1.8-8.8-1a.6.6 0 1 1-.3-1.2c3.8-.9 7.1-.5 9.8 1.1.3.2.4.6.2.9Zm1.2-2.7a.8.8 0 0 1-1 .3c-2.7-1.7-6.9-2.2-10.2-1.2a.8.8 0 1 1-.4-1.5c3.7-1.1 8.4-.6 11.5 1.4.3.2.4.7.1 1Zm.1-2.8C14.6 9 9.4 8.8 6.3 9.7a.9.9 0 1 1-.5-1.8c3.5-1.1 9.3-.9 13 1.3a.9.9 0 0 1-.9 1.6Z"
				/>
			</svg>
			Listen on Spotify
		</a>
	{/if}
{/snippet}

<section
	aria-labelledby="now-heading"
	aria-busy={nowStatus === 'loading'}
	class={[
		'transition-opacity duration-200',
		// Brand guideline: dim slightly when nothing is playing.
		nowStatus === 'ready' && !playing && 'opacity-85'
	]}
>
	<h2 id="now-heading" class="sr-only">Now playing</h2>
	<p class="sr-only" aria-live="polite" aria-atomic="true">{announcement}</p>

	{#if nowStatus === 'loading'}
		<div class="hero">
			<div class="skeleton aspect-square w-full rounded-brand"></div>
			<div class="space-y-4">
				<div class="skeleton h-6 w-32 rounded-full"></div>
				<div class="skeleton h-12 w-4/5"></div>
				<div class="skeleton h-5 w-1/2"></div>
				<div class="skeleton mt-8 h-px w-full"></div>
			</div>
		</div>
	{:else if nowStatus === 'error'}
		<div class="border-y border-rule py-10" role="alert">
			<p class="chip label [--chip:var(--spot-rose)]">
				<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
				Couldn't load
			</p>
			<p class="mt-4 font-display text-2xl font-semibold tracking-tight">
				What's playing didn't come through.
			</p>
			<p class="mt-1 text-ink-soft">{errorMessage(now.error)}</p>
			<button type="button" class="btn btn-secondary mt-6" onclick={() => live.retry()}
				>Try again</button
			>
		</div>
	{:else if track && focus && np}
		<!-- Lyrics focus: a compact, sticky now-playing column; the lyrics take
		     the main area. Stacks on phones (header row, then the lyrics). -->
		<div class="focus-hero">
			<div class="min-w-0 md:sticky md:top-6">
				{@render statusRow()}
				<div class="mt-4 flex items-center gap-4 md:block">
					{@render coverArt(
						'print size-20 rounded-[12px] [--print:var(--spot-now)] sm:size-24 md:size-auto md:w-full md:max-w-[16rem] md:rounded-brand'
					)}
					<div class="min-w-0 md:mt-5">
						{@render title('text-xl sm:text-2xl md:text-3xl')}
						<p class="mt-1 truncate font-medium md:mt-2 md:text-lg md:whitespace-normal">
							<ArtistLinks artists={track.artists} />
						</p>
					</div>
				</div>
				<div class="mt-4 md:mt-6">{@render progress()}</div>
				{@render spotifyButton('mt-6 hidden md:inline-flex')}
			</div>
			<div class="min-w-0">
				<NowLyrics {np} focus />
			</div>
		</div>
	{:else if track}
		<div class="hero">
			{@render coverArt('print w-full rounded-brand [--print:var(--spot-now)]')}

			<div class="min-w-0">
				{@render statusRow()}

				{@render title('mt-5 text-4xl sm:text-5xl lg:text-6xl')}

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

				<div class="mt-6">{@render progress()}</div>

				{#if playing && np}<NowLyrics {np} />{/if}

				{@render spotifyButton('mt-7')}
			</div>
		</div>
	{:else}
		<div class="hero">
			<img
				src="/brand/mark_primary"
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
	/* Lyrics focus: a narrow now-playing column next to the lyrics. */
	.focus-hero {
		display: grid;
		gap: 1.5rem;
	}
	@media (min-width: 48rem) {
		.focus-hero {
			grid-template-columns: minmax(0, 4fr) minmax(0, 8fr);
			gap: 3rem;
			align-items: start;
		}
	}
	/* Moved every frame from script: no CSS transition (it would lag behind and
	   restart on each update), just a compositor-only transform. */
	.fill,
	.knob {
		will-change: transform;
	}
</style>
