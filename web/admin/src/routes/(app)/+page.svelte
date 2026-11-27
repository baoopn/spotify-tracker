<script lang="ts">
	import { ArrowRight, RefreshCw } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import KpiRow from '#lib/components/KpiRow.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import NowPlayingCard from '#lib/components/overview/NowPlayingCard.svelte';
	import NowPlayingControls from '#lib/components/overview/NowPlayingControls.svelte';
	import HealthStrip from '#lib/components/overview/HealthStrip.svelte';
	import RecentPlaylistsCard from '#lib/components/overview/RecentPlaylistsCard.svelte';
	import ListenStorageCard from '#lib/components/overview/ListenStorageCard.svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { compact, date, hours, num } from '#lib/format.js';
	import { qk, queries } from '#lib/queries.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import type { LocalPause } from '#lib/player-control.svelte.js';

	// Counters/health poll (see queries.ts); now-playing is live via SSE.
	const overview = createQuery(() => queries.overview());
	const queryClient = useQueryClient();
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	// Also refreshes the health strip's queue and backup queries.
	const manual = new ManualRefetch(() =>
		Promise.all([
			overview.refetch(),
			...[qk.backups, qk.lyrics.queue, qk.lyrics.translationQueue].map((queryKey) =>
				queryClient.refetchQueries({ queryKey, exact: true })
			)
		])
	);

	const d = $derived(overview.data);
	// A pause sent from the mini controls: the card stops its clock until the stream catches up.
	let localPause = $state<LocalPause>(null);
</script>

<PageHeader
	title="Overview"
	eyebrow="baoopn listening"
	description={d
		? `${compact(d.summary_7d.total_plays)} plays this week · ${hours(d.summary_7d.listening_ms)} of music`
		: 'What Bao is listening to right now, at a glance.'}
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
	{#if d}
		<NowPlayingCard initial={d.now_playing} paused={localPause}>
			{#snippet controls()}<NowPlayingControls bind:paused={localPause} />{/snippet}
		</NowPlayingCard>
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

	<div class="mt-10">
		<HealthStrip overview={d} />
	</div>

	{#if d && d.lyrics_missing > 0}
		<a
			href={resolve('data/tracks?lyrics=missing')}
			class="card mt-4 flex items-center justify-between gap-3 p-3 transition-colors duration-150 hover:border-ink hover:bg-paper-3"
		>
			<span class="flex min-w-0 flex-col">
				<span class="eyebrow">Lyrics</span>
				<span class="text-sm font-medium text-ink">
					{num(d.lyrics_missing)}
					{d.lyrics_missing === 1 ? 'song' : 'songs'} without lyrics
				</span>
			</span>
			<ArrowRight class="size-4 shrink-0 text-ink-soft" aria-hidden="true" />
		</a>
	{/if}

	{#if d?.listen_storage}
		<div class="mt-10">
			<ListenStorageCard storage={d.listen_storage} />
		</div>
	{/if}

	<div class="mt-10">
		<RecentPlaylistsCard />
	</div>

	{#if d?.version}
		<p class="eyebrow mt-12 border-t border-rule pt-4">API {d.version}</p>
	{/if}
{/if}
