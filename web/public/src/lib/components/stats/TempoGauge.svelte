<script lang="ts">
	// Semicircle gauge, 40-220 BPM, ticks every 20, needle at the value. The
	// pivot dot pulses at the song's BPM while it plays (static under reduced motion).
	let { bpm, playing = false }: { bpm: number | null; playing?: boolean } = $props();

	const MIN = 40;
	const MAX = 220;
	const CX = 100;
	const CY = 96;
	const R = 78;
	const TICKS = Array.from({ length: (MAX - MIN) / 20 + 1 }, (_, i) => MIN + i * 20);

	function at(v: number, r: number): [number, number] {
		const t = (Math.min(MAX, Math.max(MIN, v)) - MIN) / (MAX - MIN);
		const a = Math.PI * (1 - t);
		return [CX + Math.cos(a) * r, CY - Math.sin(a) * r];
	}
	const needle = $derived(bpm != null ? at(bpm, R - 14) : null);
	const label = $derived(
		bpm != null ? `Tempo gauge at ${Math.round(bpm)} BPM, on a 40 to 220 scale` : 'Tempo unknown'
	);
</script>

<svg viewBox="-12 0 224 112" class="w-full" role="img" aria-label={label}>
	<path
		d="M {CX - R} {CY} A {R} {R} 0 0 1 {CX + R} {CY}"
		fill="none"
		class={bpm != null ? 'stroke-ink' : 'stroke-rule-strong'}
		stroke-width="1.5"
	/>
	{#each TICKS as t (t)}
		{@const [x1, y1] = at(t, R)}
		{@const [x2, y2] = at(t, t % 40 === 0 ? R - 10 : R - 6)}
		<line
			{x1}
			{y1}
			{x2}
			{y2}
			class={bpm != null ? 'stroke-ink' : 'stroke-rule-strong'}
			stroke-width={t % 40 === 0 ? 1.5 : 1}
		/>
		{#if t === MIN || t === 120 || t === MAX}
			{@const [lx, ly] = at(t, R + 10)}
			<text
				x={lx}
				y={ly}
				text-anchor={t === MIN ? 'end' : t === MAX ? 'start' : 'middle'}
				class="fill-ink-soft font-mono text-[8px]">{t}</text
			>
		{/if}
	{/each}
	{#if needle}
		<line
			x1={CX}
			y1={CY}
			x2={needle[0]}
			y2={needle[1]}
			class="stroke-ink"
			stroke-width="2"
			stroke-linecap="round"
		/>
		<circle
			cx={CX}
			cy={CY}
			r="6"
			class={['pulse fill-spot-now stroke-ink', playing && 'on']}
			stroke-width="1.5"
			style:animation-duration="{60 / Math.max(1, bpm ?? 60)}s"
		/>
	{:else}
		<circle cx={CX} cy={CY} r="6" fill="none" class="stroke-rule-strong" stroke-width="1.5" />
	{/if}
</svg>

<style>
	.pulse {
		transform-box: fill-box;
		transform-origin: center;
	}
	.pulse.on {
		animation: beat linear infinite;
	}
	@keyframes beat {
		0% {
			transform: scale(1.4);
		}
		35%,
		100% {
			transform: scale(1);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.pulse.on {
			animation: none;
		}
	}
</style>
