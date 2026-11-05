<script lang="ts">
	import Segmented from '#lib/components/Segmented.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import ChartCard from '#lib/components/charts/ChartCard.svelte';
	import BarList from '#lib/components/charts/BarList.svelte';
	import AudioFeatureBars from '#lib/components/charts/AudioFeatureBars.svelte';
	import { api } from '#lib/api.js';
	import { artistNames, num, percent } from '#lib/format.js';
	import { Query } from '#lib/query.svelte.js';
	import type { AudioProfile, GenreStat, StatsRange, TopArtist, TopTrack } from '#lib/types.js';

	let range = $state<StatsRange>('30d');

	const tracks = new Query<TopTrack[]>();
	const artists = new Query<TopArtist[]>();
	const genres = new Query<GenreStat[]>();
	const profile = new Query<AudioProfile>();

	$effect(() => {
		const r = range;
		void tracks.run((signal) => api.topTracks(r, 10, { signal }).then((x) => x.data));
		void artists.run((signal) => api.topArtists(r, 10, { signal }).then((x) => x.data));
		void genres.run((signal) => api.genres(r, 10, { signal }).then((x) => x.data));
		void profile.run((signal) => api.audioProfile(r, { signal }).then((x) => x.data));
		return () => {
			tracks.abort();
			artists.abort();
			genres.abort();
			profile.abort();
		};
	});

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
		<ChartCard title="Top tracks" subtitle="By plays" refreshing={tracks.loading && !!tracks.data}>
			{#snippet chart()}
				{#if tracks.error && !tracks.data}
					<ErrorState message={tracks.error} onretry={() => tracks.refetch()} />
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
			refreshing={artists.loading && !!artists.data}
		>
			{#snippet chart()}
				{#if artists.error && !artists.data}
					<ErrorState message={artists.error} onretry={() => artists.refetch()} />
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
			refreshing={genres.loading && !!genres.data}
		>
			{#snippet chart()}
				{#if genres.error && !genres.data}
					<ErrorState message={genres.error} onretry={() => genres.refetch()} />
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
			refreshing={profile.loading && !!profile.data}
		>
			{#snippet chart()}
				{#if profile.error && !profile.data}
					<ErrorState message={profile.error} onretry={() => profile.refetch()} />
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
