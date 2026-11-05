<script lang="ts" module>
	export type Kpi = {
		label: string;
		value: string;
		/** Short context line, e.g. "since 4 Mar 2024". */
		sub?: string;
	};
</script>

<script lang="ts">
	let {
		items,
		loading = false,
		label
	}: {
		items: Kpi[];
		loading?: boolean;
		/** Accessible name of the group. */
		label: string;
	} = $props();
</script>

<!-- Liner-note credits: large numbers on paper, separated by hairline rules. -->
<dl
	aria-label={label}
	aria-busy={loading}
	class="grid grid-cols-2 border-y border-rule sm:grid-cols-3 xl:grid-cols-6"
>
	{#each items as k (k.label)}
		<div class="kpi flex flex-col gap-1 px-4 py-5 first:pl-0">
			<dt class="eyebrow">{k.label}</dt>
			<dd
				class="order-first font-display text-3xl leading-tight font-semibold tracking-tight text-ink"
			>
				{#if loading}
					<span class="skeleton block h-8 w-20" aria-hidden="true"></span>
					<span class="sr-only">Loading</span>
				{:else}
					{k.value}
				{/if}
			</dd>
			{#if k.sub && !loading}<dd class="font-mono text-xs text-ink-soft">{k.sub}</dd>{/if}
		</div>
	{/each}
</dl>

<style>
	.kpi + .kpi {
		border-left: 1px solid var(--rule);
	}
	@media (max-width: 639px) {
		.kpi:nth-child(2n + 1) {
			border-left: 0;
			padding-left: 0;
		}
		.kpi:nth-child(n + 3) {
			border-top: 1px solid var(--rule);
		}
	}
	@media (min-width: 640px) and (max-width: 1279px) {
		.kpi:nth-child(3n + 1) {
			border-left: 0;
			padding-left: 0;
		}
		.kpi:nth-child(n + 4) {
			border-top: 1px solid var(--rule);
		}
	}
</style>
