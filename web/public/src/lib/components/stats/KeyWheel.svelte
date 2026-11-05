<script lang="ts">
	// Circle of fifths: majors on the outer ring, relative minors inside. The
	// song's key is filled with the lead spot ink.
	import { FIFTHS_MAJOR, FIFTHS_MINOR, fifthsIndex, keyName, validKey } from '#lib/music.js';

	let { musicalKey, mode }: { musicalKey: number | null; mode: number | null } = $props();

	const C = 100;
	const R_OUT = 94;
	const R_MID = 64;
	const R_IN = 38;
	const STEP = (2 * Math.PI) / 12;

	const hit = $derived(
		validKey(musicalKey)
			? { index: fifthsIndex(musicalKey, mode), ring: mode === 0 ? 'inner' : 'outer' }
			: null
	);

	function polar(r: number, a: number): string {
		return `${C + Math.cos(a) * r} ${C + Math.sin(a) * r}`;
	}
	function wedge(i: number, r1: number, r2: number): string {
		const a1 = -Math.PI / 2 + (i - 0.5) * STEP;
		const a2 = a1 + STEP;
		return `M ${polar(r2, a1)} A ${r2} ${r2} 0 0 1 ${polar(r2, a2)} L ${polar(r1, a2)} A ${r1} ${r1} 0 0 0 ${polar(r1, a1)} Z`;
	}
	function labelAt(i: number, r: number): [number, number] {
		const a = -Math.PI / 2 + i * STEP;
		return [C + Math.cos(a) * r, C + Math.sin(a) * r];
	}
	const aria = $derived(
		hit ? `Circle of fifths with ${keyName(musicalKey, mode)} highlighted` : 'Key unknown'
	);
	const line = $derived(hit ? 'stroke-ink' : 'stroke-rule-strong');
</script>

<svg viewBox="0 0 200 200" class="mx-auto w-full max-w-44" role="img" aria-label={aria}>
	{#each FIFTHS_MAJOR as name, i (name)}
		{@const on = hit?.ring === 'outer' && hit.index === i}
		{@const [x, y] = labelAt(i, (R_OUT + R_MID) / 2)}
		<path
			d={wedge(i, R_MID, R_OUT)}
			class={[on ? 'fill-spot-now' : 'fill-paper', line]}
			stroke-width={on ? 1.5 : 0.75}
		/>
		<text
			{x}
			{y}
			text-anchor="middle"
			dominant-baseline="central"
			class={['text-[10px]', on ? 'fill-on-spot font-semibold' : 'fill-ink']}>{name}</text
		>
	{/each}
	{#each FIFTHS_MINOR as name, i (name)}
		{@const on = hit?.ring === 'inner' && hit.index === i}
		{@const [x, y] = labelAt(i, (R_MID + R_IN) / 2)}
		<path
			d={wedge(i, R_IN, R_MID)}
			class={[on ? 'fill-spot-now' : 'fill-paper-2', line]}
			stroke-width={on ? 1.5 : 0.75}
		/>
		<text
			{x}
			{y}
			text-anchor="middle"
			dominant-baseline="central"
			class={['text-[7.5px]', on ? 'fill-on-spot font-semibold' : 'fill-ink-soft']}>{name}</text
		>
	{/each}
	<circle cx={C} cy={C} r={R_IN} class={['fill-paper', line]} stroke-width="0.75" />
</svg>
