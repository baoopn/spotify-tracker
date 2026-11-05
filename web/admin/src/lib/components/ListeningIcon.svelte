<script lang="ts" module>
	export type ListeningIconName =
		'headphones' | 'track' | 'playlist' | 'now-playing' | 'now-playing-static' | 'paused';
</script>

<script lang="ts">
	import { asset } from '$app/paths';

	let {
		name,
		alt = '',
		mode = 'img',
		class: className = 'size-4'
	}: {
		name: ListeningIconName;
		/** Empty = decorative. */
		alt?: string;
		/**
		 * `img`: the navy / cream file swapped by theme (keeps SMIL animation).
		 * `mask`: the navy file as a CSS mask filled with `currentColor`, so the glyph
		 * follows surrounding text colour like the lucide icons it sits next to.
		 */
		mode?: 'img' | 'mask';
		class?: string;
	} = $props();

	const navy = $derived(asset(`brand/listening/${name}-navy.svg`));
	const cream = $derived(asset(`brand/listening/${name}-cream.svg`));
</script>

{#if mode === 'mask'}
	<span
		class={['listening-mask inline-block shrink-0', className]}
		style:--icon-url="url({navy})"
		role={alt ? 'img' : undefined}
		aria-label={alt || undefined}
		aria-hidden={alt ? undefined : true}
	></span>
{:else}
	<img
		src={navy}
		{alt}
		width="200"
		height="200"
		class={['inline-block shrink-0 dark:hidden', className]}
	/>
	<img
		src={cream}
		{alt}
		width="200"
		height="200"
		class={['hidden shrink-0 dark:inline-block', className]}
	/>
{/if}

<style>
	.listening-mask {
		background-color: currentColor;
		mask: var(--icon-url) center / contain no-repeat;
	}
</style>
