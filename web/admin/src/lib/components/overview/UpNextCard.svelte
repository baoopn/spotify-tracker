<script lang="ts">
	import AlbumArt from '../AlbumArt.svelte';
	import { artistNames, duration } from '#lib/format.js';
	import type { UpNext } from '#lib/types.js';

	// The next tracks of the Spotify queue, prefetched (catalog, lyrics,
	// translation) before they play. Private: only shown here. A list read
	// for another song than the one playing now is stale and hidden.
	let { upNext, currentTrackId }: { upNext: UpNext | null; currentTrackId: string | null } =
		$props();

	const uid = $props.id();
	const items = $derived(
		upNext && currentTrackId && upNext.for_track_id === currentTrackId ? upNext.items : []
	);
</script>

{#if items.length}
	<section
		aria-labelledby="{uid}-up-next"
		class="mt-8 flex flex-col gap-3 border-t border-ink pt-4"
	>
		<h2 id="{uid}-up-next" class="eyebrow">Up next</h2>
		<ol class="flex flex-col divide-y divide-border">
			{#each items as item (item.position)}
				<li class="flex items-center gap-3 py-2 first:pt-0 last:pb-0">
					<AlbumArt src={item.cover_image_url} size={40} />
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-medium text-fg" dir="auto">{item.name}</p>
						<p class="truncate text-xs text-muted" dir="auto">
							{artistNames(item.artists)}{item.album_name ? ` · ${item.album_name}` : ''}
						</p>
					</div>
					<span class="shrink-0 font-mono text-xs text-muted">{duration(item.duration_ms)}</span>
				</li>
			{/each}
		</ol>
	</section>
{/if}
