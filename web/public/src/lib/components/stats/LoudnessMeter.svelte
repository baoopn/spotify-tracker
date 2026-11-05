<script lang="ts">
	// Segmented VU meter, -30..0 dB (quieter values clamp to -30). Blocks up to
	// the value are filled: mint to -14, butter to -8, peach to -3, rose above.
	let { db }: { db: number | null } = $props();

	const MIN = -30;
	const STEP = 2;
	const BLOCKS = Array.from({ length: -MIN / STEP }, (_, i) => MIN + i * STEP);
	const W = 200;
	const GAP = 2;
	const BW = (W - GAP * (BLOCKS.length - 1)) / BLOCKS.length;
	const x = (v: number) => ((v - MIN) / -MIN) * W;

	const level = $derived(db == null ? null : Math.min(0, Math.max(MIN, db)));
	function ink(start: number): string {
		if (start < -14) return 'fill-spot-mint';
		if (start < -8) return 'fill-spot-butter';
		if (start < -3) return 'fill-spot-peach';
		return 'fill-spot-rose';
	}
	const aria = $derived(
		db == null
			? 'Loudness unknown'
			: `Loudness meter at ${db.toFixed(1)} dB on a -30 to 0 dB scale; streaming target about -14`
	);
</script>

<svg viewBox="0 -16 {W} 54" class="w-full" role="img" aria-label={aria}>
	{#each BLOCKS as start, i (start)}
		{@const filled = level != null && level > start}
		<rect
			x={i * (BW + GAP)}
			y="0"
			width={BW}
			height="18"
			rx="2"
			class={[filled ? ink(start) : 'fill-none', filled ? 'stroke-ink' : 'stroke-rule-strong']}
			stroke-width="1"
		/>
	{/each}
	<!-- streaming target tick -->
	<line x1={x(-14)} y1="-5" x2={x(-14)} y2="23" class="stroke-ink" stroke-width="1.5" />
	<text
		x={x(-14)}
		y="-8"
		text-anchor="middle"
		class="fill-ink-soft font-mono text-[7px] tracking-[0.06em] uppercase">streaming target</text
	>
	{#each [-30, -20, -10, 0] as t (t)}
		<text
			x={x(t)}
			y="34"
			text-anchor={t === -30 ? 'start' : t === 0 ? 'end' : 'middle'}
			class="fill-ink-soft font-mono text-[7.5px]">{t}</text
		>
	{/each}
</svg>
