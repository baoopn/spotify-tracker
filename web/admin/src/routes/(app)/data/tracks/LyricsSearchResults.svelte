<script lang="ts">
	import { FileText } from '@lucide/svelte';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { errorMessage } from '#lib/api.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { lyricsQueryOk, queries } from '#lib/queries.js';

	const LIMIT = 20;

	let { q, onselect }: { q: string; onselect: (id: string) => void } = $props();

	const tooShort = $derived(!lyricsQueryOk(q));
	const results = createQuery(() => queries.searchLyrics(q, LIMIT));
</script>

{#if tooShort}
	<EmptyState
		icon={FileText}
		title="Search lyrics"
		message="Type at least 2 characters, including a letter or digit, to find songs by their words."
	/>
{:else if results.isError && !results.data}
	<ErrorState message={errorMessage(results.error)} onretry={() => results.refetch()} />
{:else if !results.data}
	<div class="flex flex-col gap-3 p-4" aria-busy="true">
		{#each { length: 5 }, i (i)}
			<div class="flex items-center gap-3">
				<Skeleton class="size-10" /><Skeleton class="h-4 flex-1" />
			</div>
		{/each}
	</div>
{:else if results.data.length === 0}
	<EmptyState icon={FileText} title="No lyrics match" message="Try other words or fewer of them." />
{:else}
	<ul
		class={['flex flex-col gap-1 transition-opacity', results.isPlaceholderData && 'opacity-60']}
		aria-label="Lyrics matches"
	>
		{#each results.data as m (m.track.id)}
			<li>
				<button
					type="button"
					class="flex w-full flex-col gap-1.5 rounded-xl px-3 py-2 text-left hover:bg-surface-2"
					aria-label="Show details for {m.track.name}"
					onclick={() => onselect(m.track.id)}
				>
					<TrackCell track={m.track} />
					{#if m.snippet}
						<span class="line-clamp-2 text-sm break-words text-muted">{m.snippet}</span>
					{/if}
					{#if m.phrase_match || m.has_synced}
						<span class="flex flex-wrap gap-1.5">
							{#if m.phrase_match}<span class="chip">Exact phrase</span>{/if}
							{#if m.has_synced}<span class="chip">Synced</span>{/if}
						</span>
					{/if}
				</button>
			</li>
		{/each}
	</ul>
{/if}
