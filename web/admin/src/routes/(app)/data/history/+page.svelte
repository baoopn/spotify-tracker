<script lang="ts">
	import { History, LoaderCircle, Repeat } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { createInfiniteQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { duration } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	const LIMIT = 50;

	let q = $state('');
	// Polling re-reads every loaded page from the newest one, so new plays appear on top.
	const plays = createInfiniteQuery(() => queries.history(q, LIMIT));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => plays.refetch());
	const rows = $derived(plays.data?.pages.flatMap((p) => p.data) ?? []);
	const error = $derived(plays.error ? errorMessage(plays.error) : null);
</script>

<PageHeader
	title="History"
	eyebrow="Data"
	description="Every play, newest first. Back-to-back repeats are folded into one row."
>
	{#snippet actions()}
		<SearchInput
			value={q}
			placeholder="Search track, artist, album or Spotify ID/link"
			label="Search history"
			onsearch={(v) => (q = v)}
		/>
	{/snippet}
</PageHeader>

<div class="overflow-hidden">
	{#if error && rows.length === 0}
		<ErrorState message={error} onretry={() => manual.run()} />
	{:else if !plays.data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 8 }, i (i)}
				<div class="flex items-center gap-3">
					<Skeleton class="size-10" />
					<div class="flex flex-1 flex-col gap-1.5">
						<Skeleton class="h-4 w-1/3" /><Skeleton class="h-3 w-1/4" />
					</div>
					<Skeleton class="h-4 w-20" />
				</div>
			{/each}
		</div>
	{:else if rows.length === 0}
		<EmptyState
			icon={History}
			title={q ? 'No plays match your search' : 'No plays recorded yet'}
			message={q ? `Nothing found for “${q}”.` : 'Plays appear here once the poller records them.'}
		/>
	{:else}
		<div
			class={[
				'overflow-x-auto transition-opacity',
				(plays.isPlaceholderData || manual.active) && 'opacity-60'
			]}
		>
			<table class="data-table">
				<caption class="sr-only">Listening history</caption>
				<thead>
					<tr>
						<th scope="col" class="tl-index">No.</th>
						<th scope="col">Track</th>
						<th scope="col" class="w-24">Length</th>
						<th scope="col" class="w-28">Replays</th>
						<th scope="col" class="w-48">Played</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as row, i (row.id)}
						<tr>
							<td class="tl-index">{String(i + 1).padStart(2, '0')}</td>
							<td class="max-w-0 min-w-64"><TrackCell track={row.track} /></td>
							<td class="text-muted tabular-nums">{duration(row.track.duration_ms)}</td>
							<td>
								{#if row.replay_count > 1}
									<span
										class="chip"
										title="Played {row.replay_count} times in a row"
										aria-label="{row.replay_count} replays"
									>
										<Repeat class="size-3" aria-hidden="true" />
										{row.replay_count}×
									</span>
								{:else}
									<span class="text-muted">–</span>
								{/if}
							</td>
							<td><RelTime value={row.played_at} showAbsolute /></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="flex items-center justify-center border-t border-border p-3">
			{#if error}
				<p class="mr-3 text-sm text-danger" role="alert">{error}</p>
			{/if}
			{#if plays.hasNextPage}
				<button
					type="button"
					class="btn btn-secondary"
					onclick={() => plays.fetchNextPage()}
					disabled={plays.isFetchingNextPage}
				>
					{#if plays.isFetchingNextPage}<LoaderCircle
							class="size-4 animate-spin"
							aria-hidden="true"
						/>{/if}
					Load more
				</button>
			{:else}
				<p class="text-sm text-muted">You've reached the beginning of the history.</p>
			{/if}
		</div>
	{/if}
</div>
