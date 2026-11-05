<script lang="ts">
	import { ArrowDown, ArrowUp, Music, RotateCcw } from '@lucide/svelte';
	import { page } from '$app/state';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import EnrichmentBadge from '#lib/components/EnrichmentBadge.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import TrackDrawer from './TrackDrawer.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { Query } from '#lib/query.svelte.js';
	import { toasts } from '#lib/toast.svelte.js';
	import {
		ENRICHMENT_STATUSES,
		type EnrichmentStatus,
		type OffsetList,
		type SortOrder,
		type TrackRow,
		type TrackSort
	} from '#lib/types.js';

	const LIMIT = 25;

	function initialStatus(): EnrichmentStatus | '' {
		const s = page.url.searchParams.get('status');
		return ENRICHMENT_STATUSES.includes(s as EnrichmentStatus) ? (s as EnrichmentStatus) : '';
	}

	let q = $state('');
	let status = $state<EnrichmentStatus | ''>(initialStatus());
	let sort = $state<TrackSort>('plays');
	let order = $state<SortOrder>('desc');
	let offset = $state(0);
	let selected = $state<string | null>(null);
	let confirmRetry = $state(false);

	const list = new Query<OffsetList<TrackRow>>();

	$effect(() => {
		const params = { q, status, sort, order, limit: LIMIT, offset };
		void list.run((signal) => api.tracks(params, { signal }));
		return () => list.abort();
	});

	function setSort(s: TrackSort) {
		if (sort === s) order = order === 'desc' ? 'asc' : 'desc';
		else {
			sort = s;
			order = s === 'name' ? 'asc' : 'desc';
		}
		offset = 0;
	}

	async function retryFailed() {
		try {
			const { requeued } = await api.retryFailed();
			toasts.success(
				'Failed tracks requeued',
				`${num(requeued)} ${requeued === 1 ? 'track' : 'tracks'} queued for enrichment.`
			);
			await list.refetch();
		} catch (err) {
			toasts.error('Could not requeue failed tracks', errorMessage(err));
		}
	}

	const columns: { key: TrackSort | null; label: string; cls?: string }[] = [
		{ key: 'name', label: 'Track' },
		{ key: 'plays', label: 'Plays', cls: 'w-24 text-right' },
		{ key: 'last_played', label: 'Last played', cls: 'w-40' },
		{ key: null, label: 'Enrichment', cls: 'w-36' },
		{ key: 'created', label: 'Added', cls: 'w-36' }
	];
</script>

<PageHeader
	title="Tracks"
	eyebrow="Data"
	description="Every song in the library, with play counts and audio-feature status."
>
	{#snippet actions()}
		<button type="button" class="btn btn-secondary" onclick={() => (confirmRetry = true)}>
			<RotateCcw class="size-4" aria-hidden="true" /> Retry all failed
		</button>
	{/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap items-center gap-3">
	<SearchInput
		value={q}
		placeholder="Search tracks"
		label="Search tracks"
		onsearch={(v) => {
			q = v;
			offset = 0;
		}}
	/>
	<label class="flex items-center gap-2 text-sm text-muted">
		Status
		<select
			class="input w-auto"
			bind:value={status}
			onchange={() => (offset = 0)}
			aria-label="Filter by enrichment status"
		>
			<option value="">All</option>
			{#each ENRICHMENT_STATUSES as s (s)}
				<option value={s}>{s[0].toUpperCase() + s.slice(1)}</option>
			{/each}
		</select>
	</label>
</div>

<div class="overflow-hidden">
	{#if list.error && !list.data}
		<ErrorState message={list.error} onretry={() => list.refetch()} />
	{:else if !list.data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 8 }, i (i)}
				<div class="flex items-center gap-3">
					<Skeleton class="size-10" /><Skeleton class="h-4 flex-1" /><Skeleton class="h-4 w-24" />
				</div>
			{/each}
		</div>
	{:else if list.data.data.length === 0}
		<EmptyState
			icon={Music}
			title="No tracks found"
			message={q || status
				? 'Try a different search or filter.'
				: 'No tracks have been recorded yet.'}
		/>
	{:else}
		<div class={['overflow-x-auto transition-opacity', list.loading && 'opacity-60']}>
			<table class="data-table">
				<caption class="sr-only">
					Tracks, sorted by {sort}
					{order === 'asc' ? 'ascending' : 'descending'}. Select a row for details.
				</caption>
				<thead>
					<tr>
						{#each columns as col (col.label)}
							<th
								scope="col"
								class={col.cls}
								aria-sort={col.key && sort === col.key
									? order === 'asc'
										? 'ascending'
										: 'descending'
									: undefined}
							>
								{#if col.key}
									{@const key = col.key}
									<button
										type="button"
										class="inline-flex items-center gap-1 rounded hover:text-fg"
										onclick={() => setSort(key)}
									>
										{col.label}
										{#if sort === key}
											{#if order === 'asc'}<ArrowUp
													class="size-3.5"
													aria-hidden="true"
												/>{:else}<ArrowDown class="size-3.5" aria-hidden="true" />{/if}
										{/if}
									</button>
								{:else}
									{col.label}
								{/if}
							</th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each list.data.data as row (row.track.id)}
						<tr class="row-link" onclick={() => (selected = row.track.id)}>
							<td class="max-w-0 min-w-64">
								<button
									type="button"
									class="block w-full rounded-lg text-left"
									aria-label="Show details for {row.track.name}"
									onclick={(e) => {
										e.stopPropagation();
										selected = row.track.id;
									}}
								>
									<TrackCell track={row.track} />
								</button>
							</td>
							<td class="text-right font-medium tabular-nums">{num(row.plays)}</td>
							<td><RelTime value={row.last_played} /></td>
							<td><EnrichmentBadge status={row.enrichment_status} /></td>
							<td><RelTime value={row.created_at} /></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pagination
			{offset}
			limit={LIMIT}
			total={list.data.total}
			disabled={list.loading}
			onchange={(o) => (offset = o)}
		/>
	{/if}
</div>

<TrackDrawer bind:trackId={selected} onchanged={() => list.refetch()} />

<ConfirmDialog
	bind:open={confirmRetry}
	title="Retry all failed tracks?"
	message="Every track whose enrichment failed is reset to pending and queued again. This may take a while and uses provider quota."
	confirmLabel="Retry failed"
	onconfirm={retryFailed}
/>
