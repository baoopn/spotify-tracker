<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import type { TopPlaylistPeriod } from '#lib/types.js';

	let { period, start }: { period: TopPlaylistPeriod; start: string } = $props();

	// Mounted only while the row is expanded, so the snapshot is fetched on demand.
	const detail = createQuery(() => queries.topPlaylist(period, start));
</script>

<div>
	<h3 class="eyebrow mb-2">Stored tracks</h3>
	{#if detail.isError && !detail.data}
		<ErrorState message={errorMessage(detail.error)} onretry={() => detail.refetch()} />
	{:else if !detail.data}
		<div class="flex flex-col gap-2" aria-busy="true">
			{#each { length: 4 }, i (i)}
				<div class="flex items-center gap-3">
					<Skeleton class="size-10" /><Skeleton class="h-4 flex-1" /><Skeleton class="h-4 w-12" />
				</div>
			{/each}
		</div>
	{:else if detail.data.tracks.length === 0}
		<p class="text-sm text-muted">No stored snapshot for this period.</p>
	{:else}
		<ol class="flex max-h-96 flex-col overflow-y-auto rounded-lg border border-rule">
			{#each detail.data.tracks as t (t.position)}
				<li
					class="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-3 border-b border-rule px-3 py-2 last:border-b-0"
				>
					<span class="text-right font-mono text-xs text-ink-soft tabular-nums">{t.position}</span>
					<TrackCell track={t.track} />
					<span class="font-mono text-xs whitespace-nowrap text-ink-soft tabular-nums">
						{num(t.plays)}
						{t.plays === 1 ? 'play' : 'plays'}
					</span>
				</li>
			{/each}
		</ol>
	{/if}
</div>
