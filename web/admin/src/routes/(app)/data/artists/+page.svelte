<script lang="ts">
	import OutLink from '#lib/components/OutLink.svelte';
	import ArtistAvatar from '#lib/components/ArtistAvatar.svelte';
	import { ArrowDown, ArrowUp, ExternalLink, MicVocal } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import type { ArtistSort, SortOrder } from '#lib/types.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	const LIMIT = 25;

	let q = $state('');
	let sort = $state<ArtistSort>('plays');
	let order = $state<SortOrder>('desc');
	let offset = $state(0);

	const list = createQuery(() => queries.artists({ q, sort, order, limit: LIMIT, offset }));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => list.refetch());

	function setSort(s: ArtistSort) {
		if (sort === s) order = order === 'desc' ? 'asc' : 'desc';
		else {
			sort = s;
			order = s === 'name' ? 'asc' : 'desc';
		}
		offset = 0;
	}

	const columns: { key: ArtistSort | null; label: string; cls?: string }[] = [
		{ key: 'name', label: 'Artist' },
		{ key: 'plays', label: 'Plays', cls: 'w-28 text-right' },
		{ key: 'tracks', label: 'Tracks', cls: 'w-28 text-right' },
		{ key: null, label: 'Last played', cls: 'w-44' }
	];
</script>

<PageHeader
	title="Artists"
	eyebrow="Data"
	description="Who gets played most, and how many of their songs are in the library."
>
	{#snippet actions()}
		<SearchInput
			value={q}
			placeholder="Search artists"
			label="Search artists"
			onsearch={(v) => {
				q = v;
				offset = 0;
			}}
		/>
	{/snippet}
</PageHeader>

<div class="overflow-hidden">
	{#if list.isError && !list.data}
		<ErrorState message={errorMessage(list.error)} onretry={() => manual.run()} />
	{:else if !list.data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 8 }, i (i)}
				<Skeleton class="h-8" />
			{/each}
		</div>
	{:else if list.data.data.length === 0}
		<EmptyState
			icon={MicVocal}
			title="No artists found"
			message={q ? 'Try a different search.' : 'No artists have been recorded yet.'}
		/>
	{:else}
		<div
			class={[
				'overflow-x-auto transition-opacity',
				(list.isPlaceholderData || manual.active) && 'opacity-60'
			]}
		>
			<table class="data-table">
				<caption class="sr-only">
					Artists, sorted by {sort}
					{order === 'asc' ? 'ascending' : 'descending'}
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
					{#each list.data.data as row (row.artist.id)}
						<tr>
							<td>
								<div class="flex items-center gap-3">
									<ArtistAvatar name={row.artist.name} src={row.artist.image_url} />
									<span class="font-medium text-fg">{row.artist.name}</span>
									{#if row.artist.external_url}
										<OutLink
											href={row.artist.external_url}
											class="text-muted hover:text-primary"
											label="Open {row.artist.name} in Spotify"
											><ExternalLink class="size-3.5" aria-hidden="true" /></OutLink
										>
									{/if}
								</div>
							</td>
							<td class="text-right font-medium tabular-nums">{num(row.plays)}</td>
							<td class="text-right tabular-nums">{num(row.tracks)}</td>
							<td><RelTime value={row.last_played} /></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pagination
			{offset}
			limit={LIMIT}
			total={list.data.total}
			disabled={list.isPlaceholderData}
			onchange={(o) => (offset = o)}
		/>
	{/if}
</div>
