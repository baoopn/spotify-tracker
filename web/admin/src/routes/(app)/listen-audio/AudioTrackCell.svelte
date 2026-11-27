<script lang="ts">
	import AlbumArt from '#lib/components/AlbumArt.svelte';
	import type { AudioTrack } from '#lib/listenAudio.js';

	let { track, trackId }: { track: AudioTrack | null; trackId: string } = $props();

	const artists = $derived(track?.artists.map((a) => a.name).join(', ') || 'Unknown artist');
</script>

<div class="flex min-w-0 items-center gap-3">
	<AlbumArt src={track?.cover_image_url} size={40} />
	<div class="min-w-0">
		{#if track}
			<p class="truncate font-medium text-fg" title={track.name}>{track.name}</p>
			<p class="truncate text-xs text-muted" title={artists}>{artists}</p>
		{:else}
			<p class="truncate font-medium text-muted">Not in the catalog</p>
			<p class="truncate font-mono text-xs text-muted">{trackId}</p>
		{/if}
	</div>
</div>
