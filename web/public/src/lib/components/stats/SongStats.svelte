<script lang="ts">
	// "Song stats" liner notes: radar for the 0..1 features, then one small
	// illustration per remaining stat, each captioned with a big Outfit value.
	import { onMount } from 'svelte';
	import BeatDots from './BeatDots.svelte';
	import GenreBar from './GenreBar.svelte';
	import KeyWheel from './KeyWheel.svelte';
	import LoudnessMeter from './LoudnessMeter.svelte';
	import PlayTally from './PlayTally.svelte';
	import RadarChart, { type RadarAxis } from './RadarChart.svelte';
	import TempoGauge from './TempoGauge.svelte';
	import { live } from '#lib/live.svelte.js';
	import { keyName } from '#lib/music.js';
	import { songStats } from '#lib/stats.svelte.js';
	import type { AudioFeatureValues, AudioProfile } from '#lib/types.js';

	onMount(() => songStats.start());

	// The track on show: now playing, else the last played one.
	const trackId = $derived(live.nowPlaying?.track?.id ?? live.history[0]?.track.id ?? null);
	$effect(() => {
		songStats.load(trackId);
	});

	const d = $derived(songStats.detail?.id === trackId ? songStats.detail : null);
	const f = $derived<AudioFeatureValues | null>(d?.features?.features ?? null);
	const status = $derived(d?.features?.status ?? null);
	const playing = $derived(!!live.nowPlaying?.is_playing && live.nowPlaying.track?.id === d?.id);

	const AXES: { label: string; key: keyof AudioFeatureValues & keyof AudioProfile }[] = [
		{ label: 'Energy', key: 'energy' },
		{ label: 'Dance', key: 'danceability' },
		{ label: 'Mood', key: 'valence' },
		{ label: 'Acoustic', key: 'acousticness' },
		{ label: 'Instrumental', key: 'instrumentalness' },
		{ label: 'Speech', key: 'speechiness' },
		{ label: 'Live', key: 'liveness' }
	];
	const axes = $derived<RadarAxis[]>(
		AXES.map((a) => ({
			label: a.label,
			value: f?.[a.key] ?? null,
			avg: songStats.profile?.[a.key] ?? null
		}))
	);
	const radarState = $derived.by(() => {
		if (!d?.features || status === 'pending') return 'pending';
		if (status === 'unavailable' || status === 'failed') return 'none';
		return axes.every((a) => a.value == null) ? 'none' : 'ready';
	});

	const bpm = $derived(f?.tempo ?? null);
	const loud = $derived(f?.loudness ?? null);
	const beats = $derived(f?.time_signature ?? null);
	const plays = $derived(d?.plays ?? null);
	const genres = $derived(d?.features?.genres ?? []);
	const source = $derived(
		d?.features?.source ? d.features.source[0].toUpperCase() + d.features.source.slice(1) : null
	);
</script>

{#snippet cell(label: string, value: string, unit: string, caption?: string)}
	<div class="flex items-baseline gap-2">
		<p class="font-display text-4xl leading-none font-semibold tracking-[-0.02em] tabular-nums">
			{value}
		</p>
		{#if unit}<p class="label text-ink-soft">{unit}</p>{/if}
	</div>
	<p class="label mt-1 text-ink-soft">
		{label}{#if caption}<span> · {caption}</span>{/if}
	</p>
{/snippet}

{#if trackId}
	<section aria-labelledby="stats-heading" aria-busy={songStats.status === 'loading'}>
		<div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-1 border-b border-ink pb-3">
			<h2 id="stats-heading" class="font-display text-2xl font-semibold tracking-[-0.02em]">
				Song stats
			</h2>
			{#if d}<p class="label max-w-full truncate text-ink-soft">{d.name}</p>{/if}
		</div>

		{#if songStats.status === 'error' && !d}
			<div class="flex flex-wrap items-center gap-4 border-b border-rule py-6" role="alert">
				<p class="text-ink-soft">Couldn't load the stats for this track.</p>
				<button type="button" class="btn btn-secondary" onclick={() => songStats.retry()}
					>Try again</button
				>
			</div>
		{:else}
			<div class="stats mt-8">
				<!-- radar -->
				<div class="min-w-0">
					{#if !d || radarState === 'pending'}
						<p class="label mb-4 text-ink-soft">{d ? 'Analysing this track…' : 'Loading…'}</p>
						<div class="skeleton mx-auto aspect-square w-full max-w-[22rem] rounded-full"></div>
					{:else if radarState === 'none'}
						<div
							class="grid aspect-square w-full max-w-[22rem] place-items-center rounded-full border border-dashed border-rule-strong"
						>
							<p class="max-w-[14rem] text-center text-ink-soft">No audio data for this track</p>
						</div>
					{:else}
						<RadarChart {axes} songLabel={d.name} />
					{/if}
				</div>

				<!-- illustrated stats: 2 x 3 on wide screens, hairline rules between cells -->
				<div class="cells">
					<div class="cell">
						<TempoGauge {bpm} {playing} />
						{@render cell(
							'Tempo',
							bpm != null ? String(Math.round(bpm)) : '—',
							bpm != null ? 'BPM' : ''
						)}
					</div>
					<div class="cell">
						<KeyWheel musicalKey={f?.key ?? null} mode={f?.mode ?? null} />
						{@render cell('Key', keyName(f?.key, f?.mode), '')}
					</div>
					<div class="cell">
						<LoudnessMeter db={loud} />
						{@render cell(
							'Loudness',
							loud != null ? loud.toFixed(1) : '—',
							loud != null ? 'dB' : ''
						)}
					</div>
					<div class="cell">
						<BeatDots {beats} {bpm} {playing} />
						{@render cell('Time signature', beats != null ? `${beats}/4` : '—', '')}
					</div>
					<div class="cell">
						<PlayTally {plays} />
						{@render cell(
							'Plays',
							plays == null ? '—' : plays > 20 ? '20+' : `${plays}×`,
							'',
							plays != null && plays > 20 ? `played ${plays}×` : undefined
						)}
					</div>
					<div class="cell">
						<p class="label mb-3 text-ink-soft">Genres</p>
						<GenreBar {genres} />
						{#if genres.length === 0}
							<p class="mt-3 font-display text-4xl leading-none font-semibold">—</p>
						{/if}
					</div>
				</div>
			</div>
			{#if source}
				<p class="label mt-4 text-right text-ink-soft">Audio data: {source}</p>
			{/if}
		{/if}
	</section>
{/if}

<style>
	.stats {
		display: grid;
		gap: 2.5rem;
	}
	@media (min-width: 64rem) {
		.stats {
			grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
			align-items: start;
		}
	}
	/* Hairline rules between equal-height cells (the 1px gap shows the rule colour). */
	.cells {
		display: grid;
		gap: 1px;
		background: var(--rule);
		border-block: 1px solid var(--rule);
	}
	@media (min-width: 40rem) {
		.cells {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
	}
	.cell {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		gap: 1rem;
		min-height: 13rem;
		padding: 1.25rem 1rem;
		background: var(--paper);
	}
	/* Keep the illustrations modest on a single wide mobile column. */
	.cell > :global(svg) {
		width: 100%;
		max-width: 17rem;
	}
</style>
