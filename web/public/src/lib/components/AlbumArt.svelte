<script lang="ts">
	import { coverUrl } from '#lib/format.js';

	let {
		src,
		alt = '',
		class: klass = 'size-12 rounded-lg',
		eager = false
	}: { src?: string | null; alt?: string; class?: string; eager?: boolean } = $props();

	const safe = $derived(coverUrl(src ?? undefined));
	let failedSrc = $state<string | null>(null);
	const failed = $derived(!!safe && failedSrc === safe);
</script>

{#if safe && !failed}
	<img
		src={safe}
		{alt}
		width="640"
		height="640"
		loading={eager ? 'eager' : 'lazy'}
		decoding="async"
		fetchpriority={eager ? 'high' : 'auto'}
		referrerpolicy="no-referrer"
		class={['aspect-square shrink-0 bg-paper-2 object-cover', klass]}
		onerror={() => (failedSrc = safe ?? null)}
	/>
{:else}
	<!-- No album art: fall back to the listening mark (sub-brand kit). -->
	<img
		src="/brand/mark_primary"
		alt={alt ? 'baoopn listening' : ''}
		width="200"
		height="200"
		class={['aspect-square shrink-0 object-cover', klass]}
	/>
{/if}
