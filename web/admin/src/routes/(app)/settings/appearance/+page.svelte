<script lang="ts">
	import { Check, Monitor, Moon, Sun } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import PalettePicker from '#lib/components/PalettePicker.svelte';
	import TimeZonePicker from './TimeZonePicker.svelte';
	import { MODES, PALETTES, theme, type Mode } from '#lib/theme.svelte.js';

	const icons = { light: Sun, dark: Moon, system: Monitor } as const;

	const paletteLabel = $derived(PALETTES.find((p) => p.id === theme.palette)?.label ?? '');

	function previewTheme(m: Mode): 'light' | 'dark' {
		return m === 'system' ? (theme.systemDark ? 'dark' : 'light') : m;
	}
</script>

<PageHeader
	title="Appearance"
	eyebrow="Settings"
	description="Spot ink, paper and time zone. Saved in this browser only."
/>

<div class="flex flex-col gap-6">
	<section class="card p-6" aria-labelledby="pal-h">
		<h2 id="pal-h" class="font-semibold text-fg">Palette</h2>
		<p class="mt-1 mb-4 text-sm text-muted">Soft accent colours used across the dashboard.</p>
		<PalettePicker />
	</section>

	<section class="card p-6" aria-labelledby="mode-h">
		<h2 id="mode-h" class="font-semibold text-fg">Mode</h2>
		<p class="mt-1 mb-4 text-sm text-muted">
			System follows your operating system and switches live.
		</p>
		<div role="group" aria-labelledby="mode-h" class="grid gap-3 sm:grid-cols-3">
			{#each MODES as m (m.id)}
				{@const active = theme.mode === m.id}
				{@const Icon = icons[m.id]}
				<button
					type="button"
					aria-pressed={active}
					class={[
						'flex flex-col gap-3 rounded-2xl border-2 p-3 text-left transition-colors',
						active ? 'border-primary' : 'border-border hover:border-border-strong'
					]}
					onclick={() => theme.setMode(m.id)}
				>
					<!-- Live preview of the current palette in this mode -->
					<div
						data-theme={previewTheme(m.id)}
						data-palette={theme.palette}
						class="pointer-events-none flex h-28 gap-2 overflow-hidden rounded-xl border border-border bg-bg p-2"
						aria-hidden="true"
					>
						<div class="flex w-1/4 flex-col gap-1.5 rounded-lg bg-surface p-1.5">
							<span class="h-2 rounded bg-spot-select"></span>
							<span class="h-2 rounded bg-surface-3"></span>
							<span class="h-2 rounded bg-surface-3"></span>
						</div>
						<div class="flex flex-1 flex-col gap-2">
							<div class="flex flex-1 flex-col justify-between rounded-lg bg-surface p-2">
								<span class="h-2 w-1/2 rounded bg-fg/70"></span>
								<span class="h-1.5 w-3/4 rounded bg-paper-4/50"></span>
								<span class="h-1.5 w-full rounded-full bg-spot-select">
									<span class="block h-full w-2/3 rounded-full bg-primary"></span>
								</span>
							</div>
							<div class="flex gap-1.5">
								<span class="h-4 w-12 rounded-md bg-primary"></span>
								<span class="h-4 w-10 rounded-md bg-spot-mint"></span>
								<span class="h-4 w-10 rounded-md bg-spot-butter"></span>
							</div>
						</div>
					</div>
					<span class="flex items-center justify-between text-sm font-medium text-fg">
						<span class="flex items-center gap-2"
							><Icon class="size-4" aria-hidden="true" />{m.label}</span
						>
						{#if active}
							<span class="grid size-5 place-items-center rounded-full bg-primary text-primary-fg">
								<Check class="size-3.5" aria-hidden="true" />
							</span>
						{/if}
					</span>
				</button>
			{/each}
		</div>
	</section>

	<section class="card p-6" aria-labelledby="tz-h">
		<h2 id="tz-h" class="font-semibold text-fg">Time zone</h2>
		<p class="mt-1 mb-4 text-sm text-muted">
			All times are shown in this zone, checked against the server's up-to-date rules.
		</p>
		<TimeZonePicker />
	</section>

	<p class="text-sm text-muted">
		Currently <span class="font-medium text-fg">{paletteLabel}</span>,
		<span class="font-medium text-fg">{theme.resolved}</span> mode{theme.mode === 'system'
			? ' (following the system)'
			: ''}.
	</p>
</div>
