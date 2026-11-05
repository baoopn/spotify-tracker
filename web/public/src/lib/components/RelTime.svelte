<script lang="ts">
	import { relativeClock } from '#lib/clock.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import { zone } from '#lib/zone.svelte.js';

	let { value, class: klass = '' }: { value: string | null | undefined; class?: string } = $props();

	// Instant difference against the skew-corrected server clock, re-rendered on a 20 s tick.
	// Calendar days are taken in the effective (possibly corrected) zone.
	const rel = $derived(zone.relative(value, live.serverNow(relativeClock.now)));
	const abs = $derived(
		zone.correctionNote ? `${zone.dateTime(value)} (${zone.correctionNote})` : zone.dateTime(value)
	);
</script>

{#if value}
	<time datetime={value} title={abs} class={['font-mono whitespace-nowrap', klass]}>{rel}</time>
{/if}
