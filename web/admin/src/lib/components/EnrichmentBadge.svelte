<script lang="ts">
	import Badge from './Badge.svelte';
	import type { EnrichmentStatus } from '#lib/types.js';

	let { status }: { status: EnrichmentStatus } = $props();

	const map = {
		pending: { tone: 'warn', label: 'Pending' },
		enriched: { tone: 'ok', label: 'Enriched' },
		unavailable: { tone: 'plain', label: 'Unavailable' },
		failed: { tone: 'danger', label: 'Failed' }
	} as const;

	const item = $derived(map[status] ?? map.pending);
</script>

<Badge tone={item.tone}>{item.label}</Badge>
