<script lang="ts">
	import { Check } from '@lucide/svelte';
	import { PALETTES, theme, type PaletteId } from '#lib/theme.svelte.js';

	let { size = 'md' }: { size?: 'sm' | 'md' } = $props();

	let buttons: HTMLButtonElement[] = $state([]);

	function onkeydown(e: KeyboardEvent, index: number) {
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (index + 1) % PALETTES.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp')
			next = (index - 1 + PALETTES.length) % PALETTES.length;
		if (next < 0) return;
		e.preventDefault();
		theme.setPalette(PALETTES[next].id as PaletteId);
		buttons[next]?.focus();
	}
</script>

<div
	role="radiogroup"
	aria-label="Colour palette"
	class={size === 'sm'
		? 'flex flex-wrap gap-2'
		: 'grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5'}
>
	{#each PALETTES as p, i (p.id)}
		{@const active = theme.palette === p.id}
		<button
			bind:this={buttons[i]}
			type="button"
			role="radio"
			aria-checked={active}
			aria-label={p.label}
			title={p.label}
			tabindex={active ? 0 : -1}
			data-palette={p.id}
			onclick={() => theme.setPalette(p.id)}
			onkeydown={(e) => onkeydown(e, i)}
			class={[
				'group relative cursor-pointer transition-colors duration-150',
				size === 'sm'
					? 'grid size-10 place-items-center rounded-full border'
					: 'flex flex-col gap-3 rounded-[var(--baoopn-radius)] border bg-paper-2 p-3 text-left',
				active ? 'border-ink' : 'border-rule hover:border-rule-strong'
			]}
		>
			{#if size === 'sm'}
				<span class="size-7 rounded-full bg-spot-select" aria-hidden="true"></span>
				{#if active}
					<Check class="absolute size-4 text-on-spot" aria-hidden="true" />
				{/if}
			{:else}
				<!-- Three flat spot swatches: now playing, selection, charts -->
				<span class="flex gap-1.5" aria-hidden="true">
					<span class="h-8 flex-1 rounded-md bg-spot-now"></span>
					<span class="h-8 flex-1 rounded-md bg-spot-select"></span>
					<span class="h-8 flex-1 rounded-md bg-spot-chart"></span>
				</span>
				<span class="flex items-center justify-between text-sm font-semibold text-ink">
					{p.label}
					{#if active}
						<span class="grid size-5 place-items-center rounded-full bg-ink text-paper">
							<Check class="size-3.5" aria-hidden="true" />
						</span>
					{/if}
				</span>
			{/if}
		</button>
	{/each}
</div>
