<script lang="ts">
	import { spotifyUrl } from '#lib/format.js';
	import type { Artist } from '#lib/types.js';

	let { artists, linkClass = '' }: { artists: Artist[]; linkClass?: string } = $props();
</script>

{#if artists.length === 0}
	<span>Unknown artist</span>
{:else}
	{#each artists as artist, i (artist.id || i)}
		{@const href = spotifyUrl(artist.external_url)}
		{#if href}
			<a {href} target="_blank" rel="noopener noreferrer" class={['link', linkClass]}
				>{artist.name}</a
			>{:else}<span>{artist.name}</span>{/if}{#if i < artists.length - 1},&nbsp;{/if}
	{/each}
{/if}
