<script lang="ts">
	// One 100%-wide proportion bar split by root genre (the API gives no weights,
	// so roots share it equally), one spot ink per segment in a fixed order, a
	// 2px paper gap between fills, and a mono legend with sub-genre chips.
	let { genres }: { genres: { root: string; sub: string[] }[] } = $props();

	const INKS = [
		'fill-spot-peach',
		'fill-spot-mint',
		'fill-spot-lilac',
		'fill-spot-butter',
		'fill-spot-sky',
		'fill-spot-rose'
	];
	const SWATCH = [
		'bg-spot-peach',
		'bg-spot-mint',
		'bg-spot-lilac',
		'bg-spot-butter',
		'bg-spot-sky',
		'bg-spot-rose'
	];

	const W = 200;
	const GAP = 2;
	const roots = $derived(genres.slice(0, INKS.length));
	const segW = $derived(roots.length ? (W - GAP * (roots.length - 1)) / roots.length : W);
	const aria = $derived(
		roots.length ? `Genres: ${roots.map((g) => g.root).join(', ')}` : 'No genres yet'
	);
	const subs = (g: { root: string; sub: string[] }) =>
		g.sub.filter((s) => s.toLowerCase() !== g.root.toLowerCase());
</script>

<svg viewBox="0 0 {W} 20" class="w-full" role="img" aria-label={aria} preserveAspectRatio="none">
	{#if roots.length === 0}
		<rect
			x="0.5"
			y="0.5"
			width={W - 1}
			height="19"
			rx="4"
			fill="none"
			class="stroke-rule-strong"
			stroke-dasharray="4 3"
		/>
	{:else}
		{#each roots as g, i (g.root)}
			<rect
				x={i * (segW + GAP) + 0.5}
				y="0.5"
				width={segW - 1}
				height="19"
				rx="4"
				class={[INKS[i], 'stroke-ink']}
				stroke-width="1"
				vector-effect="non-scaling-stroke"
			/>
		{/each}
	{/if}
</svg>

{#if roots.length}
	<ul class="mt-3 space-y-2">
		{#each roots as g, i (g.root)}
			<li class="flex flex-wrap items-center gap-x-2 gap-y-1">
				<span
					class={['size-3 shrink-0 rounded-[3px] border border-ink', SWATCH[i]]}
					aria-hidden="true"
				></span>
				<span class="label">{g.root}</span>
				{#each subs(g) as sub (sub)}
					<span
						class="rounded-full border border-rule-strong px-2 py-px font-mono text-[0.6875rem] text-ink-soft"
						>{sub}</span
					>
				{/each}
			</li>
		{/each}
	</ul>
{/if}
