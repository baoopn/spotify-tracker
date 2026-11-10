<script lang="ts" module>
	export type ListeningIconName =
		'headphones' | 'track' | 'playlist' | 'now-playing' | 'now-playing-static' | 'paused';
</script>

<script lang="ts">
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

	// Brand-kit files via /brand/<slot> (302 to the baoopn brand API's media URL,
	// or the static copy). Slot names use underscores: now-playing -> now_playing.
	const slug = $derived(name.replaceAll('-', '_'));
	const navy = $derived(`/brand/icon_${slug}_navy`);
	const cream = $derived(`/brand/icon_${slug}_cream`);
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
