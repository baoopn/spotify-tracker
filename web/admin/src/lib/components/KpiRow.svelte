<script lang="ts" module>
	export type Kpi = {
		label: string;
		value: string;
		/** Short context line, e.g. "since 4 Mar 2024". */
		sub?: string;
		/**
		 * Live indicator before the value: `true` = active (pulsing dot),
		 * `false` = idle (static muted dot), omitted = no dot.
		 */
		live?: boolean;
	};
</script>

<script lang="ts">
	let {
		items,
		loading = false,
		label,
		columns = 6
	}: {
		items: Kpi[];
		loading?: boolean;
		/** Accessible name of the group. */
		label: string;
		/** 6 (default): 2 / 3 / 6 per row. 4: 2 / 2 / 4 per row. */
		columns?: 4 | 6;
	} = $props();
</script>

<!-- Liner-note credits: large numbers on paper, separated by hairline rules. -->
<dl
	aria-label={label}
	aria-busy={loading}
	class={[
		'grid grid-cols-2 border-y border-rule',
		columns === 4 ? 'four lg:grid-cols-4' : 'sm:grid-cols-3 xl:grid-cols-6'
	]}
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
					<span class="inline-flex items-center gap-2.5">
						{#if k.live !== undefined}
							<span class={['live-dot', k.live && 'on']} aria-hidden="true"></span>
						{/if}
						{k.value}
					</span>
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
		dl:not(.four) .kpi:nth-child(3n + 1) {
			border-left: 0;
			padding-left: 0;
		}
		dl:not(.four) .kpi:nth-child(n + 4) {
			border-top: 1px solid var(--rule);
		}
	}
	@media (min-width: 640px) and (max-width: 1023px) {
		.four .kpi:nth-child(2n + 1) {
			border-left: 0;
			padding-left: 0;
		}
		.four .kpi:nth-child(n + 3) {
			border-top: 1px solid var(--rule);
		}
	}

	/* Spot-ink dot with an ink rim; pulses only while active (reduced motion stops it). */
	.live-dot {
		width: 0.625rem;
		height: 0.625rem;
		flex: none;
		border-radius: 999px;
		border: 1px solid var(--ink);
		background: var(--paper-4);
	}
	.live-dot.on {
		background: var(--spot-mint);
		animation: ink-pulse 1.6s ease-in-out infinite;
	}
</style>
