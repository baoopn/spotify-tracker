<script lang="ts">
	import { relativeClock } from '#lib/clock.svelte.js';
	import { dateTime, relative } from '#lib/format.js';

	let {
		value,
		showAbsolute = false
	}: { value: string | null | undefined; showAbsolute?: boolean } = $props();

	const rel = $derived(relative(value, relativeClock.now));
	const abs = $derived(dateTime(value));
</script>

{#if value}
	<time datetime={value} title={abs} class="whitespace-nowrap">
		{rel}
		{#if showAbsolute}<span class="block text-xs text-muted">{abs}</span>{/if}
	</time>
{:else}
	<span class="text-muted">–</span>
{/if}
