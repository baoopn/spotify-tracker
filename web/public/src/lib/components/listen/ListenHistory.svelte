<script lang="ts">
	import { createInfiniteQuery } from '@tanstack/svelte-query';
	import AlbumArt from '../AlbumArt.svelte';
	import BrandIcon from '../BrandIcon.svelte';
	import ExplicitBadge from '../ExplicitBadge.svelte';
	import RelTime from '../RelTime.svelte';
	import { errorMessage } from '#lib/api.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { artistNames, spotifyUrl } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { fetchListenHistory } from '#lib/listen/api.js';
	import { listenAuth } from '#lib/listen/auth.svelte.js';
	import { groupByDay } from '#lib/listen/history.js';
	import { listenPlayer } from '#lib/listen/player.svelte.js';
	import type { ListenHistoryPage } from '#lib/listen/types.js';
	import { zone } from '#lib/zone.svelte.js';

	/** The song playing now; history is re-read a little after it changes. */
	let { trackId }: { trackId: string | null } = $props();

	/** A play is counted after the song changes: give the last report time to land. */
	const AFTER_CHANGE_MS = 4000;
	const REFRESH_MS = 60_000;

	const history = createInfiniteQuery(() => ({
		queryKey: ['listen-history', listenAuth.me?.email ?? null] as const,
		queryFn: async ({ pageParam, signal }): Promise<ListenHistoryPage> =>
			fetchListenHistory(await listenAuth.getToken(), pageParam, signal),
		initialPageParam: null as string | null,
		getNextPageParam: (last: ListenHistoryPage) => last.next_cursor,
		refetchInterval: listenPlayer.joined ? REFRESH_MS : false,
		refetchOnWindowFocus: false
	}));

	const plays = $derived(history.data?.pages.flatMap((p) => p.data) ?? []);
	const groups = $derived(groupByDay(plays, live.serverNow(relativeClock.now), zone.format.zone));

	// Re-read after each song change (the finished song may just have counted).
	let seen: string | null | undefined;
	$effect(() => {
		const id = trackId;
		if (seen === undefined || id === seen) {
			seen = id;
			return;
		}
		seen = id;
		const t = setTimeout(() => void history.refetch(), AFTER_CHANGE_MS);
		return () => clearTimeout(t);
	});
</script>

<section aria-labelledby="listen-history-h" aria-busy={history.isPending} class="mt-16 sm:mt-20">
	<div class="flex items-end justify-between gap-4 border-b border-ink pb-3">
		<h2
			id="listen-history-h"
			class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
		>
			<BrandIcon name="headphones" class="size-6" />
			Your history
		</h2>
		{#if plays.length > 0}
			<p class="label text-ink-soft">
				{plays.length}{history.hasNextPage ? '+' : ''}
				{plays.length === 1 ? 'song' : 'songs'}
			</p>
		{/if}
	</div>

	{#if history.isPending}
		<ol aria-hidden="true">
			{#each [1, 2, 3] as i (i)}
				<li class="row">
					<div class="skeleton size-11 shrink-0"></div>
					<div class="flex-1 space-y-2">
						<div class="skeleton h-4 w-1/2"></div>
						<div class="skeleton h-3 w-1/3"></div>
					</div>
				</li>
			{/each}
		</ol>
	{:else if history.isError && plays.length === 0}
		<div class="flex flex-wrap items-center gap-4 border-b border-rule py-6" role="alert">
			<p class="chip label [--chip:var(--spot-rose)]">
				<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
				Couldn't load
			</p>
			<p class="text-ink-soft">{errorMessage(history.error)}</p>
			<button type="button" class="btn btn-secondary" onclick={() => history.refetch()}>
				Try again
			</button>
		</div>
	{:else if plays.length === 0}
		<p class="border-b border-rule py-6 text-ink-soft">
			Songs you hear here will show up in your history.
		</p>
	{:else}
		{#each groups as group (group.key)}
			<h3 class="label mt-6 text-ink-soft">{group.label}</h3>
			<ol class="mt-1 border-t border-rule">
				{#each group.items as play (play.id)}
					{@const href = spotifyUrl(play.track.external_url)}
					<li class="row">
						<AlbumArt
							src={play.track.cover_image_url}
							alt={play.track.album_name ? `Album cover of ${play.track.album_name}` : ''}
							class="size-11 rounded-[6px] border border-rule-strong"
						/>
						<div class="min-w-0 flex-1">
							<p class="flex items-center gap-1.5 font-semibold">
								<span class="truncate font-lyrics">
									{#if href}
										<a {href} target="_blank" rel="noopener noreferrer" class="link"
											>{play.track.name}</a
										>
									{:else}
										{play.track.name}
									{/if}
								</span>
								{#if play.track.explicit}<ExplicitBadge />{/if}
							</p>
							<p class="truncate font-lyrics text-sm text-ink-soft">
								{artistNames(play.track.artists)}
							</p>
						</div>
						<RelTime value={play.listened_at} class="shrink-0 text-xs text-ink-soft" />
					</li>
				{/each}
			</ol>
		{/each}

		{#if history.hasNextPage}
			<div class="mt-6">
				<button
					type="button"
					class="btn btn-secondary"
					onclick={() => history.fetchNextPage()}
					disabled={history.isFetchingNextPage}
				>
					{history.isFetchingNextPage ? 'Loading…' : 'Load more'}
				</button>
			</div>
		{/if}
		{#if history.isError}
			<p class="mt-3 text-sm text-ink-soft" role="alert">{errorMessage(history.error)}</p>
		{/if}
	{/if}
</section>

<style>
	/* Ruled rows, like the tracklist on /. */
	.row {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		min-height: 4.25rem;
		padding-block: 0.6rem;
		border-bottom: 1px solid var(--rule);
	}
</style>
