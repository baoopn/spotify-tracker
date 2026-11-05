<script lang="ts">
	import { RefreshCw } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import KpiRow from '#lib/components/KpiRow.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import NowPlayingCard from '#lib/components/overview/NowPlayingCard.svelte';
	import SpotifyTokenCard from '#lib/components/overview/SpotifyTokenCard.svelte';
	import ServiceList from '#lib/components/overview/ServiceList.svelte';
	import EnrichmentProgress from '#lib/components/overview/EnrichmentProgress.svelte';
	import RecentPlaylistsCard from '#lib/components/overview/RecentPlaylistsCard.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { compact, date, hours } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	// Counters/health poll (see queries.ts); now-playing is live via SSE.
	const overview = createQuery(() => queries.overview());
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => overview.refetch());

	const d = $derived(overview.data);
</script>

<PageHeader
	title="Overview"
	eyebrow="baoopn listening"
	description={d
		? `${compact(d.summary_7d.total_plays)} plays this week · ${hours(d.summary_7d.listening_ms)} of music`
		: 'What Bao is listening to, and whether the machinery behind it is healthy.'}
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={() => manual.run()}
			disabled={manual.active}
		>
			<RefreshCw class={['size-4', manual.active && 'animate-spin']} aria-hidden="true" />
			Refresh
		</button>
	{/snippet}
</PageHeader>

{#if overview.isError && !d}
	<div class="card-print">
		<ErrorState message={errorMessage(overview.error)} onretry={() => manual.run()} />
	</div>
{:else}
	<div class="grid items-start gap-8 lg:grid-cols-3">
		<div class="lg:col-span-2">
			{#if d}
				<NowPlayingCard initial={d.now_playing} />
			{:else}
				<div class="card-print flex gap-5 p-5">
					<Skeleton class="size-32" />
					<div class="flex flex-1 flex-col gap-2 pt-2">
						<Skeleton class="h-4 w-24" />
						<Skeleton class="h-6 w-2/3" />
						<Skeleton class="h-4 w-1/2" />
						<Skeleton class="mt-4 h-2 w-full" />
					</div>
				</div>
			{/if}
		</div>
		{#if d}
			<SpotifyTokenCard status={d.spotify} />
		{:else}
			<div class="flex flex-col gap-3">
				<Skeleton class="h-4 w-24" /><Skeleton class="h-9 w-32" /><Skeleton class="h-2 w-full" />
			</div>
		{/if}
	</div>

	<section class="mt-10" aria-labelledby="totals-h">
		<h2 id="totals-h" class="eyebrow mb-3">Listening totals</h2>
		<KpiRow
			label="Listening totals"
			loading={!d}
			items={[
				{
					label: 'Plays, all time',
					value: compact(d?.summary_all.total_plays),
					sub: d?.summary_all.first_played_at
						? `since ${date(d.summary_all.first_played_at)}`
						: undefined
				},
				{ label: 'Plays, 7 days', value: compact(d?.summary_7d.total_plays) },
				{
					label: 'Tracks',
					value: compact(d?.summary_all.unique_tracks),
					sub: d ? `${compact(d.summary_7d.unique_tracks)} this week` : undefined
				},
				{
					label: 'Artists',
					value: compact(d?.summary_all.unique_artists),
					sub: d ? `${compact(d.summary_7d.unique_artists)} this week` : undefined
				},
				{
					label: 'Hours listened',
					value: hours(d?.summary_all.listening_ms),
					sub: d ? `${hours(d.summary_7d.listening_ms)} this week` : undefined
				},
				{ label: 'Enriched', value: compact(d?.summary_all.enriched_tracks) }
			]}
		/>
	</section>

	<div class="ruled-cols mt-10 grid gap-y-10 lg:grid-cols-3">
		{#if d}
			<EnrichmentProgress counts={d.enrichment} />
			<ServiceList services={d.services} />
		{:else}
			<div class="flex flex-col gap-3 lg:px-6">
				<Skeleton class="h-4 w-32" /><Skeleton class="h-2.5 w-full" /><Skeleton class="h-16" />
			</div>
			<div class="flex flex-col gap-3 lg:px-6">
				<Skeleton class="h-4 w-32" /><Skeleton class="h-8" /><Skeleton class="h-8" /><Skeleton
					class="h-8"
				/>
			</div>
		{/if}
		<RecentPlaylistsCard />
	</div>

	{#if d?.version}
		<p class="eyebrow mt-12 border-t border-rule pt-4">API {d.version}</p>
	{/if}
{/if}
