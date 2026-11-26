<script lang="ts">
	import { nameIn, type LyricsLang } from '#lib/translation.js';
	import type { TrackTranslation } from '#lib/types.js';

	// The song title and album name in the viewer's chosen language (the raw
	// preference, also when the lyrics have no translation into it), as small
	// muted lines. Nothing for 'orig' or no name (the original stays primary).
	let {
		translation,
		lang
	}: { translation: TrackTranslation | null | undefined; lang: LyricsLang } = $props();

	const names = $derived(
		[
			{ label: 'Title', text: nameIn(translation, 'title', lang) },
			{ label: 'Album', text: nameIn(translation, 'album', lang) }
		].filter((n) => n.text)
	);
</script>

{#if names.length}
	<dl class="mb-2 grid grid-cols-[auto_1fr] gap-x-3 text-xs text-muted">
		{#each names as n (n.label)}
			<dt class="font-medium">{n.label}</dt>
			<dd class="min-w-0 break-words" {lang} dir="auto">{n.text}</dd>
		{/each}
	</dl>
{/if}
