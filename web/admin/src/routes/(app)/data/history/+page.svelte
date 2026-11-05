<script lang="ts">
	import { History, LoaderCircle, Repeat } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { duration } from '#lib/format.js';
	import type { HistoryEntry } from '#lib/types.js';

	const LIMIT = 50;

	let q = $state('');
	let rows = $state.raw<HistoryEntry[]>([]);
	let cursor = $state<string | null>(null);
	let loading = $state(false);
	let loaded = $state(false);
	let error = $state<string | null>(null);
	let ctrl: AbortController | null = null;

	async function load(reset: boolean) {
		ctrl?.abort();
		const c = new AbortController();
		ctrl = c;
		loading = true;
		error = null;
		try {
			const res = await api.history(
				{ limit: LIMIT, q, cursor: reset ? undefined : cursor },
				{ signal: c.signal }
			);
			if (c.signal.aborted) return;
			rows = reset ? res.data : [...rows, ...res.data];
			cursor = res.next_cursor || null;
			loaded = true;
		} catch (err) {
			if (c.signal.aborted) return;
			error = errorMessage(err);
		} finally {
			if (ctrl === c) loading = false;
		}
	}

	$effect(() => {
		void q;
		void load(true);
		return () => ctrl?.abort();
	});
</script>

<PageHeader
	title="History"
	eyebrow="Data"
	description="Every play, newest first. Back-to-back repeats are folded into one row."
>
	{#snippet actions()}
		<SearchInput
			value={q}
			placeholder="Search track, artist or album"
			label="Search history"
			onsearch={(v) => (q = v)}
		/>
	{/snippet}
</PageHeader>

<div class="overflow-hidden">
	{#if error && rows.length === 0}
		<ErrorState message={error} onretry={() => load(true)} />
	{:else if !loaded}
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
		<div class={['overflow-x-auto transition-opacity', loading && 'opacity-60']}>
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
			{#if cursor}
				<button
					type="button"
					class="btn btn-secondary"
					onclick={() => load(false)}
					disabled={loading}
				>
					{#if loading}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{/if}
					Load more
				</button>
			{:else}
				<p class="text-sm text-muted">You've reached the beginning of the history.</p>
			{/if}
		</div>
	{/if}
</div>
