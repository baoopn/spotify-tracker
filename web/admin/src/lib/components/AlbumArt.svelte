<script lang="ts">
	import { Disc3 } from '@lucide/svelte';

	let {
		src,
		alt = '',
		size = 40,
		rounded = 'rounded-md'
	}: { src?: string | null; alt?: string; size?: number; rounded?: string } = $props();

	let failedSrc = $state<string | null>(null);
	const failed = $derived(!!src && failedSrc === src);
	// Album art is printed with a thin ink border (a softer rule on small thumbnails).
	const frame = $derived(size >= 64 ? 'border border-ink' : 'border border-rule-strong');
</script>

{#if src && !failed}
	<img
		{src}
		{alt}
		width={size}
		height={size}
		loading="lazy"
		decoding="async"
		referrerpolicy="no-referrer"
		class={['shrink-0 bg-paper-4 object-cover', frame, rounded]}
		style:width="{size}px"
		style:height="{size}px"
		onerror={() => (failedSrc = src ?? null)}
	/>
{:else}
	<div
		class={['grid shrink-0 place-items-center bg-paper-4 text-ink-soft', frame, rounded]}
		style:width="{size}px"
		style:height="{size}px"
		role={alt ? 'img' : undefined}
		aria-label={alt || undefined}
		aria-hidden={alt ? undefined : true}
	>
		<Disc3 class="size-1/2" aria-hidden="true" />
	</div>
{/if}
