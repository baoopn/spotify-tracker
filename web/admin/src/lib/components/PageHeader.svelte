<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		description,
		eyebrow,
		actions
	}: {
		title: string;
		/** One-line mono summary under the title. */
		description?: string;
		/** Small credit line above the title, e.g. the section name. */
		eyebrow?: string;
		actions?: Snippet;
	} = $props();
</script>

<svelte:head>
	<title>{title} · baoopn listening admin</title>
</svelte:head>

<header
	class="mb-8 flex flex-wrap items-end justify-between gap-x-6 gap-y-4 border-b border-ink pb-5"
>
	<div class="min-w-0">
		{#if eyebrow}<p class="eyebrow mb-2">{eyebrow}</p>{/if}
		<h1 class="text-4xl leading-none font-semibold text-ink sm:text-5xl">{title}</h1>
		{#if description}
			<p class="mt-3 font-mono text-sm text-ink-soft">{description}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>
	{/if}
</header>
