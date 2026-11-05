<script lang="ts">
	import { MODES, PALETTES, theme } from '#lib/theme.svelte.js';
	import { zoneList } from '#lib/tz.js';
	import { zone } from '#lib/zone.svelte.js';

	// Searchable zone picker: an input bound to a datalist of every IANA zone.
	let zones = $state<string[]>([]);
	let query = $state('');
	let zoneError = $state(false);

	function loadZones() {
		if (zones.length === 0) zones = zoneList();
	}
	function chooseZone() {
		const tz = query.trim();
		if (!tz) return;
		if (!zones.includes(tz)) {
			zoneError = true;
			return;
		}
		zoneError = false;
		zone.setOverride(tz);
		query = '';
	}

	const ICONS: Record<string, string> = {
		light:
			'M12 3v1.5M12 19.5V21M4.6 4.6l1.1 1.1M18.3 18.3l1.1 1.1M3 12h1.5M19.5 12H21M4.6 19.4l1.1-1.1M18.3 5.7l1.1-1.1M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
		dark: 'M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z',
		system: 'M4 5h16v11H4zM9 20h6M12 16v4'
	};
</script>

<button
	type="button"
	popovertarget="theme-panel"
	class="btn btn-secondary btn-icon"
	aria-label="Appearance"
	title="Appearance"
>
	<svg
		viewBox="0 0 24 24"
		class="size-5"
		fill="none"
		stroke="currentColor"
		stroke-width="1.75"
		stroke-linecap="round"
		stroke-linejoin="round"
		aria-hidden="true"
	>
		<path d="M12 21a9 9 0 1 1 9-9c0 1.7-1.3 3-3 3h-1.8a1.7 1.7 0 0 0-1.2 2.9c.6.6.4 3.1-3 3.1Z" />
		<circle cx="7.5" cy="11" r="1.2" fill="currentColor" stroke="none" />
		<circle cx="10.5" cy="7" r="1.2" fill="currentColor" stroke="none" />
		<circle cx="15" cy="7.5" r="1.2" fill="currentColor" stroke="none" />
	</svg>
</button>

<div
	id="theme-panel"
	popover="auto"
	class="panel m-0 w-72 max-w-[calc(100vw-2rem)] rounded-brand border border-ink bg-paper-2 p-4 text-ink"
	aria-label="Appearance settings"
>
	<fieldset>
		<legend class="label mb-2 text-ink-soft">Spot ink</legend>
		<div class="flex flex-wrap gap-1">
			{#each PALETTES as p (p.id)}
				<label
					class="grid size-11 place-items-center rounded-full has-focus-visible:outline-2 has-focus-visible:outline-offset-1 has-focus-visible:outline-ink"
					title={p.label}
				>
					<input
						type="radio"
						name="palette"
						value={p.id}
						class="peer sr-only"
						checked={theme.palette === p.id}
						onchange={() => theme.setPalette(p.id)}
					/>
					<span
						class="swatch size-7 rounded-full border border-ink outline-offset-2 peer-checked:outline-2 peer-checked:outline-ink"
						data-palette={p.id}
						aria-hidden="true"
					></span>
					<span class="sr-only">{p.label}</span>
				</label>
			{/each}
		</div>
	</fieldset>

	<fieldset class="mt-4">
		<legend class="label mb-2 text-ink-soft">Paper</legend>
		<div class="grid grid-cols-3 overflow-hidden rounded-[8px] border border-ink">
			{#each MODES as m, i (m.id)}
				<label
					class={[
						'flex min-h-11 flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors duration-150 has-checked:bg-spot-now has-focus-visible:outline-2 has-focus-visible:-outline-offset-4 has-focus-visible:outline-ink',
						i > 0 && 'border-l border-ink'
					]}
				>
					<input
						type="radio"
						name="mode"
						value={m.id}
						class="sr-only"
						checked={theme.mode === m.id}
						onchange={() => theme.setMode(m.id)}
					/>
					<svg
						viewBox="0 0 24 24"
						class="size-4"
						fill="none"
						stroke="currentColor"
						stroke-width="1.75"
						stroke-linecap="round"
						stroke-linejoin="round"
						aria-hidden="true"><path d={ICONS[m.id]} /></svg
					>
					{m.label}
				</label>
			{/each}
		</div>
	</fieldset>

	<fieldset class="mt-4">
		<legend class="label mb-2 text-ink-soft">Time zone</legend>
		<p class="text-sm">
			{#if zone.override}
				{zone.override}
			{:else}
				Automatic ({zone.detected ?? 'unknown'})
			{/if}
		</p>
		{#if zone.resolution.kind === 'corrected'}
			<p class="mt-1 text-xs text-ink-soft" title={zone.correctionNote}>
				Corrected to {zone.resolution.label} from the server's tz data.
			</p>
		{/if}
		<form
			class="mt-2 flex gap-2"
			onsubmit={(e) => {
				e.preventDefault();
				chooseZone();
			}}
		>
			<label class="sr-only" for="tz-choose">Choose a time zone</label>
			<input
				id="tz-choose"
				type="search"
				list="tz-options"
				placeholder="Choose…"
				autocomplete="off"
				class="min-h-11 w-full min-w-0 rounded-[8px] border border-ink bg-paper px-2.5 text-sm text-ink"
				aria-invalid={zoneError}
				aria-describedby={zoneError ? 'tz-error' : undefined}
				bind:value={query}
				onfocus={loadZones}
				onchange={chooseZone}
			/>
			<datalist id="tz-options">
				{#each zones as z (z)}
					<option value={z}></option>
				{/each}
			</datalist>
			<button type="submit" class="btn btn-secondary shrink-0 px-3">Set</button>
		</form>
		{#if zoneError}
			<p id="tz-error" class="mt-1 text-xs text-ink-soft">
				Pick a zone from the list, e.g. America/Edmonton.
			</p>
		{/if}
		{#if zone.override}
			<button
				type="button"
				class="link mt-2 min-h-11 text-sm"
				onclick={() => zone.setOverride(null)}>Reset to automatic</button
			>
		{/if}
	</fieldset>
</div>

<style>
	/* Each palette previews as its dominant spot ink, flat. */
	.swatch {
		background: var(--spot-now);
	}
	/* The default set leads with two inks: a flat ring and a flat centre. */
	.swatch[data-palette='baoopn'] {
		background: var(--spot-select);
		box-shadow: inset 0 0 0 0.45rem var(--spot-now);
	}
	/* Anchored to the top-right corner, below the toggle button; print-offset depth. */
	.panel {
		position: fixed;
		inset: auto;
		top: 5rem;
		right: max(1rem, calc((100vw - 72rem) / 2 + 1.5rem));
		box-shadow: 4px 4px 0 var(--ink);
	}
	@media (min-width: 40rem) {
		.panel {
			top: 6.25rem;
		}
	}
</style>
