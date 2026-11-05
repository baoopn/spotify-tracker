<script lang="ts">
	import type { Snippet } from 'svelte';
	import { ChartLine, Table } from '@lucide/svelte';

	let {
		title,
		subtitle,
		refreshing = false,
		chart,
		table,
		actions
	}: {
		title: string;
		subtitle?: string;
		/** Hold the previous render at reduced opacity while refetching. */
		refreshing?: boolean;
		chart: Snippet;
		/** Accessible table twin of the chart. */
		table?: Snippet;
		actions?: Snippet;
	} = $props();

	let showTable = $state(false);
	const headingId = $props.id();
</script>

<section class="card flex min-w-0 flex-col p-5" aria-labelledby={headingId}>
	<div class="mb-4 flex flex-wrap items-start justify-between gap-2">
		<div class="min-w-0">
			<h2 id={headingId} class="text-xl font-semibold text-ink">{title}</h2>
			{#if subtitle}<p class="mt-1 font-mono text-xs text-ink-soft">{subtitle}</p>{/if}
		</div>
		<div class="flex items-center gap-1">
			{@render actions?.()}
			{#if table}
				<button
					type="button"
					class="btn btn-ghost btn-sm"
					aria-pressed={showTable}
					onclick={() => (showTable = !showTable)}
				>
					{#if showTable}
						<ChartLine class="size-4" aria-hidden="true" /> Chart
					{:else}
						<Table class="size-4" aria-hidden="true" /> Table
					{/if}
				</button>
			{/if}
		</div>
	</div>
	<div class={['min-w-0 transition-opacity', refreshing && 'opacity-60']} aria-busy={refreshing}>
		{#if showTable && table}
			<div class="max-h-96 overflow-auto rounded-lg border border-rule">
				{@render table()}
			</div>
		{:else}
			{@render chart()}
		{/if}
	</div>
</section>
