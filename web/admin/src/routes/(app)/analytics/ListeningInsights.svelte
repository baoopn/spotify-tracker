<script lang="ts">
	import Segmented from '#lib/components/Segmented.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import ChartCard from '#lib/components/charts/ChartCard.svelte';
	import BarList from '#lib/components/charts/BarList.svelte';
	import AudioFeatureBars from '#lib/components/charts/AudioFeatureBars.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { artistNames, num, percent } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import type { StatsRange } from '#lib/types.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	let range = $state<StatsRange>('30d');

	const tracks = createQuery(() => queries.topTracks(range, 10));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const tracksManual = new ManualRefetch(() => tracks.refetch());
	const artists = createQuery(() => queries.topArtists(range, 10));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const artistsManual = new ManualRefetch(() => artists.refetch());
	const genres = createQuery(() => queries.genres(range, 10));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const genresManual = new ManualRefetch(() => genres.refetch());
	const profile = createQuery(() => queries.audioProfile(range));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const profileManual = new ManualRefetch(() => profile.refetch());

	const ranges = [
		{ value: '7d', label: '7d' },
		{ value: '30d', label: '30d' },
		{ value: '90d', label: '90d' },
		{ value: '365d', label: '1y' },
		{ value: 'all', label: 'All' }
	] satisfies { value: StatsRange; label: string }[];
</script>

{#snippet loadingBars()}
	<div class="flex flex-col gap-3">
		{#each { length: 5 }, i (i)}<Skeleton class="h-8" />{/each}
	</div>
{/snippet}

<section aria-labelledby="listen-h" class="mt-10">
	<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
		<h2 id="listen-h" class="text-lg font-semibold text-fg">Listening insights</h2>
		<Segmented options={ranges} bind:value={range} label="Listening time range" />
	</div>

	<div class="grid gap-4 lg:grid-cols-2">
		<ChartCard
			title="Top tracks"
			subtitle="By plays"
			refreshing={(tracks.isPlaceholderData || tracksManual.active) && !!tracks.data}
		>
			{#snippet chart()}
				{#if tracks.isError && !tracks.data}
					<ErrorState message={errorMessage(tracks.error)} onretry={() => tracksManual.run()} />
				{:else if !tracks.data}
					{@render loadingBars()}
				{:else if tracks.data.length === 0}
					<p class="text-sm text-muted">No plays in this range.</p>
				{:else}
					<BarList
						label="Top tracks by plays"
						format={num}
						items={tracks.data.map((t) => ({
							id: t.track.id,
							label: t.track.name,
							sub: artistNames(t.track.artists),
							value: t.plays
						}))}
					/>
				{/if}
			{/snippet}
		</ChartCard>

		<ChartCard
			title="Top artists"
			subtitle="By plays"
			refreshing={(artists.isPlaceholderData || artistsManual.active) && !!artists.data}
		>
			{#snippet chart()}
				{#if artists.isError && !artists.data}
					<ErrorState message={errorMessage(artists.error)} onretry={() => artistsManual.run()} />
				{:else if !artists.data}
					{@render loadingBars()}
				{:else if artists.data.length === 0}
					<p class="text-sm text-muted">No plays in this range.</p>
				{:else}
					<BarList
						label="Top artists by plays"
						format={num}
						items={artists.data.map((a) => ({
							id: a.artist.id,
							label: a.artist.name,
							value: a.plays
						}))}
					/>
				{/if}
			{/snippet}
		</ChartCard>

		<ChartCard
			title="Genres"
			subtitle="Share of plays"
			refreshing={(genres.isPlaceholderData || genresManual.active) && !!genres.data}
		>
			{#snippet chart()}
				{#if genres.isError && !genres.data}
					<ErrorState message={errorMessage(genres.error)} onretry={() => genresManual.run()} />
				{:else if !genres.data}
					{@render loadingBars()}
				{:else if genres.data.length === 0}
					<p class="text-sm text-muted">No genre data yet.</p>
				{:else}
					<BarList
						label="Genres by share of plays"
						format={(n) => percent(n)}
						items={genres.data.map((g) => ({
							id: g.genre,
							label: g.genre,
							sub: g.sub
								.slice(0, 3)
								.map((s) => s.genre)
								.join(', '),
							value: g.share
						}))}
					/>
				{/if}
			{/snippet}
		</ChartCard>

		<ChartCard
			title="Audio profile"
			subtitle={profile.data
				? `Play-weighted average of ${num(profile.data.sample_tracks)} tracks`
				: 'Play-weighted average'}
			refreshing={(profile.isPlaceholderData || profileManual.active) && !!profile.data}
		>
			{#snippet chart()}
				{#if profile.isError && !profile.data}
					<ErrorState message={errorMessage(profile.error)} onretry={() => profileManual.run()} />
				{:else if !profile.data}
					{@render loadingBars()}
				{:else if profile.data.sample_tracks === 0}
					<p class="text-sm text-muted">No enriched tracks in this range.</p>
				{:else}
					<AudioFeatureBars features={profile.data} label="Average audio features" />
				{/if}
			{/snippet}
		</ChartCard>
	</div>
</section>
