<script lang="ts">
	import BrandIcon from '../BrandIcon.svelte';
	import { listenPlayer, type ListenStatus } from '#lib/listen/player.svelte.js';

	const p = listenPlayer;

	const labels: Record<ListenStatus, string> = {
		waiting: 'Waiting for a song',
		loading: 'Getting in sync',
		preparing: 'Loading',
		'in-sync': 'In sync',
		'paused-by-owner': 'Bao paused',
		'no-audio': 'No audio',
		'cant-play': "Can't play here",
		ended: 'Song ended',
		'access-ended': 'Access ended',
		off: 'Off'
	};
	const spots: Record<ListenStatus, string> = {
		waiting: '--spot-butter',
		loading: '--spot-sky',
		preparing: '--spot-sky',
		'in-sync': '--spot-mint',
		'paused-by-owner': '--spot-butter',
		'no-audio': '--spot-rose',
		'cant-play': '--spot-rose',
		ended: '--spot-lilac',
		'access-ended': '--spot-rose',
		off: '--spot-butter'
	};

	const inSync = $derived(p.status === 'in-sync');
	// Not joined, "Getting in sync" never shows (the player sets "loading" at a
	// track change before the next tick turns it into "preparing").
	const label = $derived(
		p.joined
			? labels[p.status]
			: p.ready && inSync
				? 'Ready'
				: p.status === 'loading'
					? labels.preparing
					: labels[p.status]
	);
	// Join only makes sense when this song has audio to play.
	const showJoin = $derived(
		!p.joined && !['access-ended', 'off', 'no-audio', 'cant-play'].includes(p.status)
	);
	const percent = $derived(Math.round(p.volume * 100));
	const hint = $derived(
		p.note ??
			(p.joined
				? null
				: p.stopped
					? 'Stopped. Join again to hear Bao live.'
					: p.ready
						? 'Press Join to hear what Bao hears.'
						: null)
	);
</script>

<section
	class="print mb-10 rounded-brand bg-paper-2 p-4 [--print:var(--spot-now)] sm:p-5"
	aria-label="Listen along"
>
	<div class="flex flex-wrap items-center gap-x-4 gap-y-3">
		<p class="chip label" style:--chip="var({spots[p.status]})" aria-live="polite">
			<BrandIcon
				name={p.joined && inSync ? 'now-playing' : 'headphones'}
				tone="navy"
				class="size-4"
			/>
			{label}
		</p>

		{#if p.joined || showJoin}
			<!-- One button for Join and Stop streaming, so keyboard focus stays on it.
			     Join is enabled once the audio is loaded, so play() runs inside the
			     click (iOS). Stop is not a pause: Join later starts at Bao's live spot. -->
			<button
				type="button"
				class={[
					'btn disabled:cursor-not-allowed disabled:opacity-60',
					p.joined ? 'btn-secondary' : 'btn-primary'
				]}
				onclick={() => (p.joined ? p.stop() : p.join())}
				disabled={!p.joined && !p.ready}
			>
				{#if p.joined}
					<!-- lucide "square" -->
					<svg
						viewBox="0 0 24 24"
						class="size-4"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						stroke-linecap="round"
						stroke-linejoin="round"
						aria-hidden="true"
					>
						<rect x="3" y="3" width="18" height="18" rx="2" />
					</svg>
					Stop streaming
				{:else}
					Join
				{/if}
			</button>
		{/if}

		<div class="flex min-w-0 flex-1 basis-48 items-center gap-3">
			<button
				type="button"
				class="btn btn-secondary btn-icon shrink-0"
				aria-pressed={p.muted}
				aria-label={p.muted ? 'Unmute' : 'Mute'}
				title={p.muted ? 'Unmute' : 'Mute'}
				onclick={() => p.toggleMute()}
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
					<path d="M11 5 6 9H3v6h3l5 4V5Z" />
					{#if p.muted}
						<path d="m22 9-6 6M16 9l6 6" />
					{:else}
						<path d="M15.5 8.5a5 5 0 0 1 0 7M18.5 5.5a9 9 0 0 1 0 13" />
					{/if}
				</svg>
			</button>
			{#if p.volumeSupported}
				<input
					type="range"
					min="0"
					max="1"
					step="0.05"
					value={p.volume}
					oninput={(e) => p.setVolume(e.currentTarget.valueAsNumber)}
					aria-label="Volume"
					aria-valuetext="{percent}%"
					class="volume min-w-0 flex-1 sm:max-w-56"
				/>
			{/if}
		</div>
	</div>

	{#if hint}
		<p class="mt-3 text-sm text-ink-soft" role="status">{hint}</p>
	{/if}
</section>

<style>
	/* Ink track and thumb on paper, like the hero's progress line. */
	.volume {
		appearance: none;
		height: 2.75rem;
		background: transparent;
		accent-color: var(--ink);
	}
	.volume::-webkit-slider-runnable-track {
		height: 2px;
		background: var(--ink);
		border-radius: 1px;
	}
	.volume::-moz-range-track {
		height: 2px;
		background: var(--ink);
		border-radius: 1px;
	}
	.volume::-webkit-slider-thumb {
		appearance: none;
		margin-top: -7px;
		width: 16px;
		height: 16px;
		border-radius: 999px;
		border: 2px solid var(--paper-2);
		background: var(--ink);
	}
	.volume::-moz-range-thumb {
		width: 12px;
		height: 12px;
		border-radius: 999px;
		border: 2px solid var(--paper-2);
		background: var(--ink);
	}
</style>
