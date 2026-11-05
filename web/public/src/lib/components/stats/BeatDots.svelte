<script lang="ts">
	// N beat dots for N/4, the downbeat accented. While the song plays they light
	// up in time with the tempo (Web Animations; static under reduced motion).
	import { prefersReducedMotion } from 'svelte/motion';

	let {
		beats,
		bpm,
		playing = false
	}: { beats: number | null; bpm: number | null; playing?: boolean } = $props();

	const n = $derived(beats != null && beats >= 1 && beats <= 12 ? Math.round(beats) : null);
	const count = $derived(n ?? 4);
	const W = 200;
	const cx = (i: number) => (W / (count + 1)) * (i + 1);

	// Attachment per "lit" dot: re-runs (and cancels the old animation) whenever
	// the beat count, tempo, playing state or motion preference changes.
	function lightUp(i: number) {
		return (el: SVGCircleElement) => {
			if (!n || !bpm || !playing || prefersReducedMotion.current || i >= n) return;
			const beat = 60_000 / bpm;
			const on = 1 / n;
			const anim = el.animate(
				[
					{ opacity: 1, offset: 0 },
					{ opacity: 1, offset: on * 0.6 },
					{ opacity: 0, offset: on },
					{ opacity: 0, offset: 1 }
				],
				{ duration: beat * n, delay: beat * i, iterations: Infinity }
			);
			return () => anim.cancel();
		};
	}

	const aria = $derived(n ? `${n} beats per bar, first beat accented` : 'Time signature unknown');
</script>

<svg viewBox="0 0 {W} 60" class="w-full" role="img" aria-label={aria}>
	{#each Array.from({ length: count }, (_, i) => i) as i (i)}
		{@const r = i === 0 ? 13 : 9}
		<circle
			cx={cx(i)}
			cy="30"
			{r}
			class={[
				n ? (i === 0 ? 'fill-ink' : 'fill-paper') : 'fill-none',
				n ? 'stroke-ink' : 'stroke-rule-strong'
			]}
			stroke-width="1.5"
			stroke-dasharray={n ? undefined : '3 3'}
		/>
		<!-- the "lit" layer, animated in time -->
		<circle {@attach lightUp(i)} cx={cx(i)} cy="30" r={r - 3} class="fill-spot-now" opacity="0" />
	{/each}
</svg>
