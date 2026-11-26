<script lang="ts">
	import { Ban, CircleCheck, CircleX, Clock } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import { num, percent } from '#lib/format.js';
	import type { EnrichmentCounts, EnrichmentStatus } from '#lib/types.js';

	let { counts }: { counts: EnrichmentCounts } = $props();

	// Status colours (they mean state), always paired with an icon + label.
	const parts = [
		{ key: 'enriched', label: 'Enriched', color: 'bg-spot-mint', icon: CircleCheck },
		{ key: 'pending', label: 'Pending', color: 'bg-spot-sky', icon: Clock },
		{ key: 'unavailable', label: 'Unavailable', color: 'bg-paper-4', icon: Ban },
		{ key: 'failed', label: 'Failed', color: 'bg-spot-rose', icon: CircleX }
	] as const satisfies readonly {
		key: EnrichmentStatus;
		label: string;
		color: string;
		icon: unknown;
	}[];

	const total = $derived(parts.reduce((a, p) => a + (counts[p.key] ?? 0), 0));
	const summary = $derived(parts.map((p) => `${p.label} ${num(counts[p.key] ?? 0)}`).join(', '));
</script>

<section aria-labelledby="enr-title">
	<div class="mb-3 flex items-baseline justify-between gap-2">
		<h2 id="enr-title" class="eyebrow">Enrichment</h2>
		<span class="font-mono text-xs text-ink-soft">{num(total)} tracks</span>
	</div>

	<div class="flex h-2.5 w-full gap-0.5" role="img" aria-label="Enrichment status: {summary}">
		{#if total === 0}
			<div class="h-full flex-1 rounded-full bg-surface-3"></div>
		{:else}
			{#each parts as p (p.key)}
				{#if (counts[p.key] ?? 0) > 0}
					<div
						class={['h-full rounded-full', p.color]}
						style:flex-grow={counts[p.key]}
						style:flex-basis="0"
						style:min-width="4px"
					></div>
				{/if}
			{/each}
		{/if}
	</div>

	<dl class="mt-4 grid grid-cols-2 gap-x-4 gap-y-2">
		{#each parts as p (p.key)}
			<div class="flex items-center justify-between gap-2 text-sm">
				<dt class="flex items-center gap-1.5 text-muted">
					<span class={['size-2 rounded-full', p.color]} aria-hidden="true"></span>
					<p.icon class="size-3.5" aria-hidden="true" />
					{p.label}
				</dt>
				<dd class="font-medium text-fg tabular-nums">
					{num(counts[p.key] ?? 0)}
					<span class="text-xs font-normal text-muted"
						>{total > 0 ? percent((counts[p.key] ?? 0) / total) : ''}</span
					>
				</dd>
			</div>
		{/each}
	</dl>

	{#if (counts.failed ?? 0) > 0}
		<a
			href={resolve('data/tracks?status=failed')}
			class="mt-3 inline-block text-sm font-medium text-primary hover:underline"
		>
			Review failed tracks
		</a>
	{/if}
</section>
