<script lang="ts" module>
	export type RadarAxis = { label: string; value: number | null; avg: number | null };
</script>

<script lang="ts">
	// Hand-made radar ("polygon") chart for 0..1 audio features. Two series,
	// told apart by more than colour: this song = flat lilac fill + solid ink
	// stroke + vertex dots; 30-day average = dashed ink-soft outline, no fill.
	import { cubicOut } from 'svelte/easing';
	import { Tween, prefersReducedMotion } from 'svelte/motion';

	let { axes, songLabel }: { axes: RadarAxis[]; songLabel: string } = $props();

	const SIZE = 320;
	const C = SIZE / 2;
	const R = 108;
	const RINGS = [0.25, 0.5, 0.75, 1];

	const clamp = (v: number | null) =>
		v == null || Number.isNaN(v) ? 0 : Math.min(1, Math.max(0, v));
	const fmt = (v: number | null) => (v == null ? '—' : v.toFixed(2));

	// Morph the shape on track change (300 ms ease-out; instant under reduced motion).
	const shape = Tween.of(() => axes.map((a) => clamp(a.value)), {
		duration: () => (prefersReducedMotion.current ? 0 : 300),
		easing: cubicOut
	});
	const hasAvg = $derived(axes.some((a) => a.avg != null));

	function point(i: number, r: number): [number, number] {
		const angle = -Math.PI / 2 + (i * 2 * Math.PI) / axes.length;
		return [C + Math.cos(angle) * R * r, C + Math.sin(angle) * R * r];
	}
	const pts = (vals: number[]) => vals.map((v, i) => point(i, v).join(',')).join(' ');

	const songPts = $derived(pts(shape.current));
	const avgPts = $derived(pts(axes.map((a) => clamp(a.avg))));

	let active = $state<number | null>(null);
	const tip = $derived.by(() => {
		if (active == null || !axes[active]) return null;
		const [x, y] = point(active, shape.current[active] ?? 0);
		return { x: (x / SIZE) * 100, y: (y / SIZE) * 100, axis: axes[active] };
	});
</script>

<figure class="m-0">
	<div class="relative mx-auto aspect-square w-full max-w-[22rem]">
		<svg
			viewBox="0 0 {SIZE} {SIZE}"
			class="size-full overflow-visible"
			role="group"
			aria-label="Audio features of {songLabel} compared with the 30-day average"
		>
			<!-- grid rings and spokes: hairlines -->
			{#each RINGS as ring (ring)}
				<polygon
					points={pts(axes.map(() => ring))}
					fill="none"
					class="stroke-rule"
					stroke-width="1"
				/>
			{/each}
			{#each axes as axis, i (axis.label)}
				{@const [x, y] = point(i, 1)}
				<line x1={C} y1={C} x2={x} y2={y} class="stroke-rule" stroke-width="1" />
			{/each}

			{#if hasAvg}
				<polygon
					points={avgPts}
					fill="none"
					class="stroke-ink-soft"
					stroke-width="1.5"
					stroke-dasharray="5 4"
					stroke-linejoin="round"
				/>
			{/if}
			<polygon
				points={songPts}
				class="fill-spot-lilac stroke-ink"
				fill-opacity="0.7"
				stroke-width="2"
				stroke-linejoin="round"
			/>

			<!-- axis labels: mono small caps -->
			{#each axes as axis, i (axis.label)}
				{@const [x, y] = point(i, 1.2)}
				<text
					{x}
					{y}
					text-anchor={Math.abs(x - C) < 4 ? 'middle' : x > C ? 'start' : 'end'}
					dominant-baseline="middle"
					class="fill-ink-soft font-mono text-[10.5px] tracking-[0.08em] uppercase"
					>{axis.label}</text
				>
			{/each}

			<!-- vertices: keyboard focusable, value on hover/focus; hit target larger than the mark -->
			{#each axes as axis, i (axis.label)}
				{@const [x, y] = point(i, shape.current[i] ?? 0)}
				<!-- Data points are focusable so keyboard users get the same value readout
				     as hover (the table below is the full text alternative). -->
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<g
					role="img"
					tabindex="0"
					aria-label="{axis.label}: this song {fmt(axis.value)}, 30-day average {fmt(axis.avg)}"
					class="vertex outline-none"
					onpointerenter={() => (active = i)}
					onpointerleave={() => (active = null)}
					onfocus={() => (active = i)}
					onblur={() => (active = null)}
				>
					<circle cx={x} cy={y} r="14" fill="transparent" />
					{#if active === i}
						<!-- focus/hover ring -->
						<circle cx={x} cy={y} r="10" fill="none" class="stroke-ink" stroke-width="1.5" />
					{/if}
					<circle
						cx={x}
						cy={y}
						r={active === i ? 6 : 4.5}
						class="dot fill-ink stroke-paper"
						stroke-width="2"
					/>
				</g>
			{/each}
		</svg>

		{#if tip}
			<div
				class="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-[calc(100%+12px)] rounded-[8px] border border-ink bg-paper-2 px-2.5 py-1.5 whitespace-nowrap shadow-[2px_2px_0_var(--ink)]"
				style:left="{tip.x}%"
				style:top="{tip.y}%"
				aria-hidden="true"
			>
				<p class="label text-ink-soft">{tip.axis.label}</p>
				<p class="font-mono text-sm tabular-nums">
					{fmt(tip.axis.value)}{#if hasAvg}<span class="text-ink-soft"
							>&nbsp;· avg {fmt(tip.axis.avg)}</span
						>{/if}
				</p>
			</div>
		{/if}
	</div>

	<figcaption class="mt-4 flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-sm">
		<span class="flex items-center gap-2">
			<svg viewBox="0 0 16 16" class="size-4" aria-hidden="true"
				><rect
					x="1"
					y="1"
					width="14"
					height="14"
					rx="3"
					class="fill-spot-lilac stroke-ink"
					fill-opacity="0.7"
					stroke-width="1.5"
				/></svg
			>
			This song
		</span>
		{#if hasAvg}
			<span class="flex items-center gap-2 text-ink-soft">
				<svg viewBox="0 0 24 8" class="h-2 w-6" aria-hidden="true"
					><line
						x1="1"
						y1="4"
						x2="23"
						y2="4"
						class="stroke-ink-soft"
						stroke-width="1.5"
						stroke-dasharray="5 4"
					/></svg
				>
				Your 30-day average
			</span>
		{/if}
	</figcaption>
</figure>

<details class="mt-3 text-sm">
	<summary class="label mx-auto w-fit cursor-pointer py-2 text-ink-soft">Show as table</summary>
	<table class="mt-2 w-full border-collapse font-mono text-xs tabular-nums">
		<thead>
			<tr class="border-b border-ink text-left">
				<th scope="col" class="label py-1.5 font-medium">Feature</th>
				<th scope="col" class="label py-1.5 text-right font-medium">This song</th>
				<th scope="col" class="label py-1.5 text-right font-medium">30-day avg</th>
			</tr>
		</thead>
		<tbody>
			{#each axes as axis (axis.label)}
				<tr class="border-b border-rule">
					<th scope="row" class="py-1.5 text-left font-normal">{axis.label}</th>
					<td class="py-1.5 text-right">{fmt(axis.value)}</td>
					<td class="py-1.5 text-right text-ink-soft">{fmt(axis.avg)}</td>
				</tr>
			{/each}
		</tbody>
	</table>
</details>

<style>
	.dot {
		transition: r 150ms ease-out;
	}
</style>
