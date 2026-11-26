<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { ListPlus, Play } from '@lucide/svelte';
	import AlbumArt from '../AlbumArt.svelte';
	import SearchInput from '../SearchInput.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { artistNames, duration } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import type { PlayerTrack } from '#lib/types.js';

	// The next tracks of the Spotify queue and a search to add more. The Web
	// API cannot remove or reorder queue items, so neither is offered.
	let {
		queue,
		disabled,
		onplay,
		onplayfrom,
		onqueue
	}: {
		/** undefined while loading; null when Spotify refused the queue. */
		queue: PlayerTrack[] | null | undefined;
		disabled: boolean;
		/** Play a search result on its own. */
		onplay: (t: PlayerTrack) => void;
		/** Play the queue from position i on. */
		onplayfrom: (i: number) => void;
		onqueue: (t: PlayerTrack) => void;
	} = $props();

	const uid = $props.id();
	let q = $state('');
	const search = createQuery(() => queries.playerSearch(q));
</script>

{#snippet row(t: PlayerTrack, index: number | null)}
	<li class="flex min-w-0 items-center gap-3 py-2">
		<AlbumArt src={t.cover_image_url} size={40} />
		<div class="min-w-0 flex-1">
			<p class="truncate text-sm font-medium text-ink" dir="auto" title={t.name}>{t.name}</p>
			<p class="truncate text-xs text-ink-soft" dir="auto">
				{artistNames(t.artists)}<span class="font-mono">{` · ${duration(t.duration_ms)}`}</span>
			</p>
		</div>
		<div class="flex shrink-0 items-center gap-1">
			{#if index === null}
				<button
					type="button"
					class="btn btn-ghost btn-sm btn-icon"
					aria-label="Add “{t.name}” to the queue"
					title="Add to queue"
					{disabled}
					onclick={() => onqueue(t)}
				>
					<ListPlus class="size-4" aria-hidden="true" />
				</button>
			{/if}
			<button
				type="button"
				class="btn btn-ghost btn-sm btn-icon"
				aria-label="Play “{t.name}” now"
				title="Play now"
				disabled={disabled || (index !== null && !t.uri.startsWith('spotify:track:'))}
				onclick={() => (index === null ? onplay(t) : onplayfrom(index))}
			>
				<Play class="size-4" aria-hidden="true" />
			</button>
		</div>
	</li>
{/snippet}

<div class="flex flex-col gap-2 border-t border-ink pt-4">
	<h2 id="{uid}-add" class="eyebrow">Add to queue</h2>
	<SearchInput
		label="Search Spotify for a track"
		placeholder="Search for a song…"
		maxlength={200}
		onsearch={(s) => (q = s)}
	/>
	{#if q}
		{#if search.isPending}
			<div class="flex flex-col gap-2 py-2" aria-busy="true">
				<Skeleton class="h-10" /><Skeleton class="h-10" />
			</div>
		{:else if search.isError}
			<p class="text-sm text-ink-soft">Search failed: {errorMessage(search.error)}</p>
		{:else if search.data?.length}
			<ul class="flex flex-col divide-y divide-rule" aria-labelledby="{uid}-add">
				{#each search.data as t (t.id)}
					{@render row(t, null)}
				{/each}
			</ul>
		{:else}
			<p class="text-sm text-ink-soft">No tracks match “{q}”.</p>
		{/if}
	{/if}
</div>

<div class="flex flex-col gap-2 border-t border-ink pt-4">
	<h2 id="{uid}-next" class="eyebrow">Up next</h2>
	{#if queue === undefined}
		<div class="flex flex-col gap-2"><Skeleton class="h-10" /><Skeleton class="h-10" /></div>
	{:else if queue === null}
		<p class="text-sm text-ink-soft">The queue is unavailable right now.</p>
	{:else if queue.length === 0}
		<p class="text-sm text-ink-soft">The queue is empty.</p>
	{:else}
		<ol class="flex flex-col divide-y divide-rule" aria-labelledby="{uid}-next">
			<!-- The queue can hold the same track twice (repeat): key by position. -->
			{#each queue as t, i (i)}
				{@render row(t, i)}
			{/each}
		</ol>
	{/if}
</div>
