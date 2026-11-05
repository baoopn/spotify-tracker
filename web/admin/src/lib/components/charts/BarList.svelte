<script lang="ts" module>
	export type BarItem = {
		id: string;
		label: string;
		sub?: string;
		value: number;
	};
</script>

<script lang="ts">
	import { compact, percent } from '#lib/format.js';

	let {
		items,
		label,
		max,
		format = compact,
		color = 'var(--spot-chart)',
		showShare = false
	}: {
		items: BarItem[];
		/** Accessible name for the list. */
		label: string;
		/** Domain max; defaults to the largest value. */
		max?: number;
		format?: (n: number) => string;
		color?: string;
		/** Show share of the total in the hover readout. */
		showShare?: boolean;
	} = $props();

	const domainMax = $derived(max ?? Math.max(0, ...items.map((i) => i.value)));
	const total = $derived(items.reduce((a, b) => a + b.value, 0));
	let active = $state<string | null>(null);
</script>

<ul class="flex flex-col gap-2.5" aria-label={label}>
	{#each items as item (item.id)}
		{@const ratio = domainMax > 0 ? Math.max(0, Math.min(1, item.value / domainMax)) : 0}
		<li
			class="group relative rounded-lg"
			onpointerenter={() => (active = item.id)}
			onpointerleave={() => (active = null)}
		>
			<div class="mb-1 flex items-baseline justify-between gap-3 text-sm">
				<span class="min-w-0 truncate text-ink" title={item.label}>
					{item.label}
					{#if item.sub}<span class="text-xs text-ink-soft"> · {item.sub}</span>{/if}
				</span>
			</div>
			<div class="flex items-center gap-2">
				<div class="relative h-3.5 flex-1 border-l border-ink">
					<div
						class={[
							'absolute inset-y-0 left-0 rounded-r-[4px] border-r-2 border-ink transition-[filter] duration-150',
							active === item.id && 'brightness-110'
						]}
						style:width="{Math.max(ratio * 100, item.value > 0 ? 1 : 0)}%"
						style:background={color}
					></div>
				</div>
				<span class="w-14 shrink-0 text-right font-mono text-xs font-medium text-ink tabular-nums"
					>{format(item.value)}</span
				>
			</div>
			{#if active === item.id && showShare && total > 0}
				<div
					class="card-print pointer-events-none absolute -top-1 right-16 z-10 -translate-y-full px-2.5 py-1.5 text-xs"
					role="tooltip"
				>
					<span class="font-mono font-semibold text-ink tabular-nums"
						>{percent(item.value / total)}</span
					>
					<span class="text-ink-soft"> of listed</span>
				</div>
			{/if}
		</li>
	{/each}
</ul>
