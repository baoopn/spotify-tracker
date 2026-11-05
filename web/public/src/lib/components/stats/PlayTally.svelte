<script lang="ts">
	// Tally marks: groups of five (four strokes and a strike), up to four groups.
	let { plays }: { plays: number | null } = $props();

	const MAX = 20;
	const GROUP_W = 46;
	const shown = $derived(plays == null ? 0 : Math.min(MAX, Math.max(0, Math.floor(plays))));
	const groups = $derived(Array.from({ length: Math.max(1, Math.ceil(shown / 5)) }, (_, g) => g));

	// A little hand-drawn wobble, deterministic per stroke.
	const wobble = (g: number, s: number) => ((g * 7 + s * 13) % 5) * 0.6 - 1.2;

	const aria = $derived(
		plays == null
			? 'Play count unknown'
			: plays > MAX
				? `Tally of ${MAX} or more plays (${plays})`
				: `Tally of ${plays} ${plays === 1 ? 'play' : 'plays'}`
	);
</script>

<svg viewBox="0 0 {GROUP_W * 4} 48" class="w-full" role="img" aria-label={aria}>
	{#each groups as g (g)}
		{@const inGroup = Math.min(5, shown - g * 5)}
		{@const ox = g * GROUP_W + 6}
		{#if shown === 0}
			{#each [0, 1, 2, 3] as s (s)}
				<line
					x1={ox + s * 8}
					y1="10"
					x2={ox + s * 8}
					y2="40"
					class="stroke-rule-strong"
					stroke-width="2"
					stroke-dasharray="3 3"
					stroke-linecap="round"
				/>
			{/each}
		{:else}
			{#each Array.from({ length: Math.min(4, inGroup) }, (_, s) => s) as s (s)}
				<line
					x1={ox + s * 8 + wobble(g, s)}
					y1={9 + wobble(s, g) * 0.5}
					x2={ox + s * 8 - wobble(g, s) * 0.5}
					y2={40}
					class="stroke-ink"
					stroke-width="2.25"
					stroke-linecap="round"
				/>
			{/each}
			{#if inGroup === 5}
				<line
					x1={ox - 4}
					y1="34"
					x2={ox + 28}
					y2="14"
					class="stroke-ink"
					stroke-width="2.25"
					stroke-linecap="round"
				/>
			{/if}
		{/if}
	{/each}
</svg>
