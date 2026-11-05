<script lang="ts">
	import { Clock } from '@lucide/svelte';
	import { zone } from '#lib/zone.svelte.js';
</script>

{#if zone.corrected}
	<p
		class="inline-flex max-w-full items-center gap-1.5 rounded-full bg-spot-butter px-2.5 py-1 font-mono text-[0.6875rem] text-on-spot"
		role="status"
		title="Your browser's built-in time-zone data is older than the server's. Times use the server's current offset ({zone
			.spec.label})."
	>
		<Clock class="size-3.5 shrink-0" aria-hidden="true" />
		<span class="truncate">Times corrected for outdated browser time-zone data ({zone.iana})</span>
	</p>
{/if}
