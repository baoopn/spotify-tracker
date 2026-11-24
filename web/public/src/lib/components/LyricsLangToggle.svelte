<script lang="ts">
	import type { LyricsLang } from '#lib/lyrics.js';

	let {
		available,
		value = $bindable(),
		compact = false
	}: {
		/** Translations this song has; nothing renders when empty. */
		available: readonly ('en' | 'vi')[];
		value: LyricsLang;
		/** Short labels (Orig / EN / VI) for narrow places. */
		compact?: boolean;
	} = $props();

	const options = $derived([
		{
			id: 'orig' as const,
			label: compact ? 'Orig' : 'Original',
			full: 'Original',
			lang: undefined
		},
		...available.map((l) =>
			l === 'en'
				? { id: l, label: compact ? 'EN' : 'English', full: 'English', lang: 'en' }
				: { id: l, label: compact ? 'VI' : 'Tiếng Việt', full: 'Tiếng Việt', lang: 'vi' }
		)
	]);
</script>

{#if available.length}
	<div
		role="group"
		aria-label="Lyrics translation"
		class="inline-flex rounded-lg border border-rule p-0.5 text-xs"
	>
		{#each options as o (o.id)}
			<button
				type="button"
				lang={o.lang}
				aria-pressed={value === o.id}
				aria-label={compact ? o.full : undefined}
				class={[
					'rounded-md px-2 py-1 font-semibold transition-colors',
					value === o.id ? 'bg-ink text-paper' : 'text-ink-soft hover:text-ink'
				]}
				onclick={() => (value = o.id)}>{o.label}</button
			>
		{/each}
	</div>
{/if}
