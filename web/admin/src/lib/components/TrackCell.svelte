<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import { artistNames } from '#lib/format.js';
	import type { TrackView } from '#lib/types.js';

	let { track, size = 40 }: { track: TrackView; size?: number } = $props();

	const artists = $derived(artistNames(track.artists));
</script>

<div class="flex min-w-0 items-center gap-3">
	<AlbumArt src={track.album?.cover_image_url} {size} />
	<div class="min-w-0">
		<p class="truncate font-medium text-fg" title={track.name}>
			{track.name}
			{#if track.explicit}
				<span
					class="ml-1 rounded bg-surface-3 px-1 py-px align-middle text-[10px] font-semibold text-muted"
					title="Explicit">E</span
				>
			{/if}
		</p>
		<p class="truncate text-xs text-muted" title={artists}>
			{artists}
			{#if track.album?.name}
				<span aria-hidden="true">·</span> {track.album.name}
			{/if}
		</p>
	</div>
</div>
