<script lang="ts">
	let { name, src, size = 36 }: { name: string; src?: string | null; size?: number } = $props();

	let failedSrc = $state<string | null>(null);
	const showImage = $derived(!!src && failedSrc !== src);
	const initial = $derived(name.trim().slice(0, 1).toUpperCase() || '?');
</script>

{#if showImage}
	<img
		{src}
		alt=""
		width={size}
		height={size}
		loading="lazy"
		decoding="async"
		referrerpolicy="no-referrer"
		class="shrink-0 rounded-full border border-rule-strong bg-paper-4 object-cover"
		style:width="{size}px"
		style:height="{size}px"
		onerror={() => (failedSrc = src ?? null)}
	/>
{:else}
	<span
		class="grid shrink-0 place-items-center rounded-full bg-spot-select text-sm font-semibold text-on-spot"
		style:width="{size}px"
		style:height="{size}px"
		aria-hidden="true">{initial}</span
	>
{/if}
