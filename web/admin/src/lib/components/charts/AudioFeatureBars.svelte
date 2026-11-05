<script lang="ts">
	import BarList, { type BarItem } from './BarList.svelte';
	import { decimal, percent } from '#lib/format.js';

	type Features = {
		acousticness?: number | null;
		danceability?: number | null;
		energy?: number | null;
		instrumentalness?: number | null;
		liveness?: number | null;
		speechiness?: number | null;
		valence?: number | null;
		tempo?: number | null;
		loudness?: number | null;
	};

	let { features, label = 'Audio features' }: { features: Features; label?: string } = $props();

	const KEYS = [
		['energy', 'Energy'],
		['danceability', 'Danceability'],
		['valence', 'Valence (positivity)'],
		['acousticness', 'Acousticness'],
		['instrumentalness', 'Instrumentalness'],
		['liveness', 'Liveness'],
		['speechiness', 'Speechiness']
	] as const;

	const items = $derived<BarItem[]>(
		KEYS.filter(([k]) => features[k] != null).map(([k, l]) => ({
			id: k,
			label: l,
			value: features[k] ?? 0
		}))
	);
</script>

{#if items.length}
	<BarList {items} {label} max={1} format={(n) => percent(n)} />
{/if}
{#if features.tempo != null || features.loudness != null}
	<dl class="mt-4 grid grid-cols-2 gap-3">
		{#if features.tempo != null}
			<div class="rounded-xl bg-surface-2 px-3 py-2">
				<dt class="text-xs text-muted">Tempo</dt>
				<dd class="text-lg font-semibold text-fg">{decimal(features.tempo)} BPM</dd>
			</div>
		{/if}
		{#if features.loudness != null}
			<div class="rounded-xl bg-surface-2 px-3 py-2">
				<dt class="text-xs text-muted">Loudness</dt>
				<dd class="text-lg font-semibold text-fg">{decimal(features.loudness)} dB</dd>
			</div>
		{/if}
	</dl>
{/if}
{#if !items.length && features.tempo == null && features.loudness == null}
	<p class="text-sm text-muted">No audio features available.</p>
{/if}
