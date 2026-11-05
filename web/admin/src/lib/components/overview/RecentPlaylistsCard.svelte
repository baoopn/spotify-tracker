<script lang="ts">
	import { onMount } from 'svelte';
	import { ListMusic } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import AlbumArt from '../AlbumArt.svelte';
	import RelTime from '../RelTime.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { api } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { Query } from '#lib/query.svelte.js';
	import type { RecentPlaylist } from '#lib/types.js';

	const recent = new Query<RecentPlaylist[]>();

	onMount(() => {
		void recent.run((signal) => api.recentPlaylists(5, { signal }).then((r) => r.data));
		return () => recent.abort();
	});
</script>

<section aria-labelledby="rp-title">
	<div class="mb-3 flex items-baseline justify-between gap-2">
		<h2 id="rp-title" class="eyebrow">Recent playlists</h2>
		<a href={resolve('data/playlists')} class="text-sm font-medium text-primary hover:underline"
			>View all</a
		>
	</div>

	{#if recent.error && !recent.data}
		<p class="text-sm text-muted" role="alert">Could not load playlists: {recent.error}</p>
	{:else if !recent.data}
		<div class="flex flex-col gap-3" aria-busy="true">
			{#each { length: 3 }, i (i)}<Skeleton class="h-10" />{/each}
		</div>
	{:else if recent.data.length === 0}
		<p class="flex items-center gap-2 text-sm text-muted">
			<ListMusic class="size-4" aria-hidden="true" /> No public playlists played recently.
		</p>
	{:else}
		<ul class="flex flex-col divide-y divide-border">
			{#each recent.data as row (row.playlist.id)}
				<li class="flex items-center gap-3 py-2 first:pt-0 last:pb-0">
					<AlbumArt src={row.playlist.cover_image_url} size={40} />
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-medium text-fg">
							{row.playlist.available && row.playlist.name
								? row.playlist.name
								: 'Spotify mix / unavailable'}
						</p>
						<p class="truncate text-xs text-muted">
							{row.playlist.owner_name ?? ''}{row.playlist.owner_name ? ' · ' : ''}{num(row.plays)}
							{row.plays === 1 ? 'play' : 'plays'}
						</p>
					</div>
					<span class="shrink-0 text-xs text-muted"><RelTime value={row.last_played} /></span>
				</li>
			{/each}
		</ul>
	{/if}
</section>
