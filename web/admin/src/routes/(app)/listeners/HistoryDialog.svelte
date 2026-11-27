<script lang="ts">
	import { LoaderCircle } from '@lucide/svelte';
	import { createInfiniteQuery } from '@tanstack/svelte-query';
	import AlbumArt from '#lib/components/AlbumArt.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { duration } from '#lib/format.js';
	import {
		listenerHistory,
		listenersKey,
		type ListenHistoryPage,
		type Listener
	} from '#lib/listeners.js';

	let { open = $bindable(false), listener }: { open?: boolean; listener: Listener | null } =
		$props();

	const id = $derived(listener?.id ?? null);
	const history = createInfiniteQuery(() => ({
		queryKey: [...listenersKey, id, 'history'] as const,
		queryFn: ({ pageParam, signal }): Promise<ListenHistoryPage> =>
			listenerHistory(id ?? 0, pageParam, { signal }),
		initialPageParam: null as string | null,
		getNextPageParam: (last: ListenHistoryPage) => last.next_cursor,
		enabled: open && id != null
	}));
	const plays = $derived(history.data?.pages.flatMap((p) => p.data) ?? []);
</script>

<Dialog
	bind:open
	variant="drawer"
	title="History"
	description={listener ? `What ${listener.email} heard at /listen.` : undefined}
>
	{#if history.isPending}
		<div class="flex flex-col gap-3" aria-busy="true">
			{#each { length: 5 }, i (i)}<Skeleton class="h-12" />{/each}
		</div>
	{:else if history.isError && plays.length === 0}
		<ErrorState message={errorMessage(history.error)} onretry={() => history.refetch()} />
	{:else if plays.length === 0}
		<EmptyState
			title="No plays yet"
			message="Songs they hear for 30 seconds or more show up here."
		/>
	{:else}
		<ol class="flex flex-col">
			{#each plays as play (play.id)}
				<li class="flex items-center gap-3 border-b border-rule py-2.5 last:border-b-0">
					<AlbumArt src={play.track.cover_image_url} alt="" size={40} />
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-medium text-fg" title={play.track.name}>
							{play.track.name}
						</p>
						<p class="truncate text-xs text-muted">
							{play.track.artists.map((a) => a.name).join(', ') || 'Unknown artist'}
						</p>
					</div>
					<div class="flex shrink-0 flex-col items-end gap-0.5 text-xs text-muted">
						<RelTime value={play.listened_at} />
						<span class="font-mono tabular-nums">{duration(play.heard_ms)} heard</span>
					</div>
				</li>
			{/each}
		</ol>
		{#if history.hasNextPage}
			<button
				type="button"
				class="btn btn-secondary mt-4"
				onclick={() => history.fetchNextPage()}
				disabled={history.isFetchingNextPage}
			>
				{#if history.isFetchingNextPage}<LoaderCircle
						class="size-4 animate-spin"
						aria-hidden="true"
					/>{/if}
				Load more
			</button>
		{/if}
		{#if history.isError}
			<p class="mt-3 text-sm text-danger" role="alert">{errorMessage(history.error)}</p>
		{/if}
	{/if}
</Dialog>
