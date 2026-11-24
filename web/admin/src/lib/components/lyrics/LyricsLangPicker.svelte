<script lang="ts">
	import Segmented from '../Segmented.svelte';
	import type { LyricsLang } from '#lib/translation.js';

	let {
		available,
		value = $bindable()
	}: {
		/** Translations this song has; nothing renders when empty. */
		available: readonly ('en' | 'vi')[];
		value: LyricsLang;
	} = $props();

	const LABELS = { en: 'English', vi: 'Tiếng Việt' } as const;
	const options = $derived([
		{ value: 'orig' as LyricsLang, label: 'Original' },
		...available.map((l) => ({ value: l as LyricsLang, label: LABELS[l] }))
	]);
</script>

{#if available.length}
	<Segmented {options} bind:value label="Lyrics translation" size="sm" />
{/if}
