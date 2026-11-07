<script lang="ts">
	// "My top 5": most played tracks and artists over the last 7, 30 or 365
	// days, both driven by one range control.
	import AlbumArt from './AlbumArt.svelte';
	import ArtistLinks from './ArtistLinks.svelte';
	import BrandIcon from './BrandIcon.svelte';
	import ExplicitBadge from './ExplicitBadge.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { spotifyUrl } from '#lib/format.js';
	import { loadStatus, TOP_LIMIT, topArtistsQuery, topTracksQuery } from '#lib/queries.js';
	import type { StatsRange } from '#lib/types.js';

	const RANGES: { id: StatsRange; label: string; long: string }[] = [
		{ id: '7d', label: '7D', long: 'Last 7 days' },
		{ id: '30d', label: '30D', long: 'Last 30 days' },
		{ id: '365d', label: '365D', long: 'Last 365 days' }
	];

	let range = $state<StatsRange>('7d');
	const rangeLabel = $derived(RANGES.find((r) => r.id === range)?.long ?? '');

	// Both keep the previous range on screen while the next one loads.
	const tracks = createQuery(() => topTracksQuery(range));
	const artists = createQuery(() => topArtistsQuery(range));
	const trackStatus = $derived(loadStatus(tracks));
	const artistStatus = $derived(loadStatus(artists));
	// Optional section: hidden only when both first loads fail. Once any range
	// has loaded, later failures show per-column notes and keep the control.
	let seen = false;
	const everLoaded = $derived.by(() => {
		if (tracks.data !== undefined || artists.data !== undefined) seen = true;
		return seen;
	});
	const hidden = $derived(!everLoaded && trackStatus === 'error' && artistStatus === 'error');
	const busy = $derived(trackStatus === 'loading' || artistStatus === 'loading');

	const nf = new Intl.NumberFormat(undefined);
	const plays = (n: number) => `${nf.format(n)} ${n === 1 ? 'play' : 'plays'}`;
	const pad = (n: number) => String(n).padStart(2, '0');
	const skeletonRows = Array.from({ length: TOP_LIMIT }, (_, i) => i + 1);
</script>

{#snippet skeleton()}
	<ol aria-hidden="true">
		{#each skeletonRows as i (i)}
			<li class="row">
				<span class="label w-7 shrink-0 text-ink-soft">{pad(i)}</span>
				<div class="skeleton size-11 shrink-0"></div>
				<div class="flex-1 space-y-2">
					<div class="skeleton h-4 w-1/2"></div>
					<div class="skeleton h-3 w-1/3"></div>
				</div>
			</li>
		{/each}
	</ol>
{/snippet}

{#snippet note(text: string)}
	<p class="border-b border-rule py-6 text-ink-soft">{text}</p>
{/snippet}

{#if !hidden}
	<section aria-labelledby="top5-heading" aria-busy={busy}>
		<div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-1 border-b border-ink pb-3">
			<h2
				id="top5-heading"
				class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
			>
				<BrandIcon name="headphones" class="size-6" />
				My top 5
			</h2>
			<p class="label text-ink-soft" data-testid="top5-range">{rangeLabel}</p>
		</div>

		<!-- Segmented control: native radios give arrow-key navigation for free. -->
		<fieldset class="mt-6">
			<legend class="sr-only">Top 5 range</legend>
			<div
				class="inline-grid overflow-hidden rounded-[8px] border border-ink"
				style:grid-template-columns="repeat({RANGES.length}, minmax(4.5rem, 1fr))"
			>
				{#each RANGES as r, i (r.id)}
					<label
						class={[
							'flex min-h-11 items-center justify-center px-4 py-1.5 text-sm font-medium transition-colors duration-150 has-checked:bg-spot-now has-checked:text-on-spot has-focus-visible:outline-2 has-focus-visible:-outline-offset-4 has-focus-visible:outline-ink',
							i > 0 && 'border-l border-ink'
						]}
					>
						<input type="radio" name="top5-range" value={r.id} class="sr-only" bind:group={range} />
						{r.label}
						<span class="sr-only">{r.long}</span>
					</label>
				{/each}
			</div>
		</fieldset>

		<div class="cols mt-6">
			<div class="min-w-0" aria-busy={trackStatus === 'loading'}>
				<h3 id="top5-tracks" class="label border-b border-ink pb-2 text-ink-soft">Tracks</h3>
				{#if trackStatus === 'loading'}
					{@render skeleton()}
				{:else if trackStatus === 'error'}
					{@render note("Couldn't load tracks.")}
				{:else if (tracks.data ?? []).length === 0}
					{@render note('No plays in this range yet.')}
				{:else}
					<ol
						aria-labelledby="top5-tracks"
						class={['transition-opacity', tracks.isPlaceholderData && 'opacity-60']}
					>
						{#each tracks.data ?? [] as entry, i (entry.track.id)}
							{@const href = spotifyUrl(entry.track.external_url)}
							<li class="row">
								<span class="label w-7 shrink-0 text-ink-soft" aria-hidden="true">{pad(i + 1)}</span
								>
								<AlbumArt
									src={entry.track.album?.cover_image_url}
									alt={entry.track.album?.name ? `Album cover of ${entry.track.album.name}` : ''}
									class="size-11 rounded-[6px] border border-rule-strong"
								/>
								<div class="min-w-0 flex-1">
									<p class="flex items-center gap-1.5 font-semibold">
										<span class="truncate">
											{#if href}
												<a {href} target="_blank" rel="noopener noreferrer" class="link"
													>{entry.track.name}</a
												>
											{:else}
												{entry.track.name}
											{/if}
										</span>
										{#if entry.track.explicit}<ExplicitBadge />{/if}
									</p>
									<p class="truncate text-sm text-ink-soft">
										<ArtistLinks artists={entry.track.artists} />
									</p>
								</div>
								<span class="shrink-0 text-xs whitespace-nowrap text-ink-soft"
									>{plays(entry.plays)}</span
								>
							</li>
						{/each}
					</ol>
				{/if}
			</div>

			<div class="min-w-0" aria-busy={artistStatus === 'loading'}>
				<h3 id="top5-artists" class="label border-b border-ink pb-2 text-ink-soft">Artists</h3>
				{#if artistStatus === 'loading'}
					{@render skeleton()}
				{:else if artistStatus === 'error'}
					{@render note("Couldn't load artists.")}
				{:else if (artists.data ?? []).length === 0}
					{@render note('No plays in this range yet.')}
				{:else}
					<ol
						aria-labelledby="top5-artists"
						class={['transition-opacity', artists.isPlaceholderData && 'opacity-60']}
					>
						{#each artists.data ?? [] as entry, i (entry.artist.id || i)}
							{@const href = spotifyUrl(entry.artist.external_url)}
							<li class="row">
								<span class="label w-7 shrink-0 text-ink-soft" aria-hidden="true">{pad(i + 1)}</span
								>
								<AlbumArt
									src={entry.artist.image_url}
									class="size-11 rounded-full border border-rule-strong"
								/>
								<p class="min-w-0 flex-1 truncate font-semibold">
									{#if href}
										<a {href} target="_blank" rel="noopener noreferrer" class="link"
											>{entry.artist.name}</a
										>
									{:else}
										{entry.artist.name}
									{/if}
								</p>
								<span class="shrink-0 text-xs whitespace-nowrap text-ink-soft"
									>{plays(entry.plays)}</span
								>
							</li>
						{/each}
					</ol>
				{/if}
			</div>
		</div>
	</section>
{/if}

<style>
	/* Tracks beside artists on wide screens, stacked (tracks first) on phones. */
	.cols {
		display: grid;
		gap: 2rem;
	}
	@media (min-width: 48rem) {
		.cols {
			grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
			align-items: start;
		}
	}
	/* Ruled tracklist rows, like RecentList. */
	.row {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		min-height: 4.25rem;
		padding-block: 0.6rem;
		border-bottom: 1px solid var(--rule);
	}
</style>
