<script lang="ts">
	import type { LyricsLayout } from '#lib/lyricsLayout.svelte.js';

	let {
		value,
		onchange
	}: {
		value: LyricsLayout;
		/** Called with the button that was pressed, so the page can keep it in place. */
		onchange: (v: LyricsLayout, from: HTMLElement) => void;
	} = $props();

	const options = [
		{ id: 'standard', label: 'Standard' },
		{ id: 'lyrics', label: 'Lyrics' }
	] as const;
</script>

<div
	role="group"
	aria-label="View layout"
	data-layout-toggle
	class="inline-flex rounded-lg border border-rule p-0.5 text-xs"
>
	{#each options as o (o.id)}
		<button
			type="button"
			aria-pressed={value === o.id}
			class={[
				'inline-flex items-center gap-1.5 rounded-md px-2 py-1 font-semibold transition-colors',
				value === o.id ? 'bg-ink text-paper' : 'text-ink-soft hover:text-ink'
			]}
			onclick={(e) => value !== o.id && onchange(o.id, e.currentTarget)}
		>
			<svg
				viewBox="0 0 24 24"
				class="size-3.5"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
				aria-hidden="true"
			>
				{#if o.id === 'standard'}
					<rect x="3" y="4" width="8" height="8" rx="1.5" />
					<path d="M14 6h7M14 10h5M3 16h18M3 20h12" />
				{:else}
					<path d="M4 6h16M4 10h11M4 14h16M4 18h9" />
				{/if}
			</svg>
			{o.label}
		</button>
	{/each}
</div>
