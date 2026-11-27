<script lang="ts">
	import { CircleAlert } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import { bytes, num } from '#lib/format.js';
	import { percent, type ListenStorage } from '#lib/listenAudio.js';
	import RelTime from '../RelTime.svelte';

	let { storage }: { storage: ListenStorage } = $props();

	const coverage = $derived(percent(storage.tracks_with_audio, storage.tracks_played));
	const recent = $derived(percent(storage.plays_30d_with_audio, storage.plays_30d));
</script>

<section aria-labelledby="ls-title">
	<div class="mb-3 flex items-baseline justify-between gap-2">
		<h2 id="ls-title" class="eyebrow">Listen-along storage</h2>
		<a href={resolve('listen-audio')} class="text-sm font-medium text-primary hover:underline"
			>Audio files</a
		>
	</div>

	<div class="card grid gap-x-6 gap-y-5 p-5 sm:grid-cols-2 lg:grid-cols-4">
		<div class="min-w-0">
			<p class="eyebrow">Files</p>
			<p class="mt-1 font-display text-3xl leading-tight font-semibold text-ink tabular-nums">
				{num(storage.files)}
			</p>
			<p class="font-mono text-xs text-ink-soft">
				{bytes(storage.total_bytes)} ·
				<span class={storage.added_7d > 0 ? 'text-ok' : undefined}
					>+{num(storage.added_7d)} this week</span
				>
			</p>
		</div>

		<div class="min-w-0">
			<p class="eyebrow">Coverage</p>
			<p class="mt-1 font-display text-3xl leading-tight font-semibold text-ink tabular-nums">
				{coverage}%
			</p>
			<div
				class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-paper-4"
				role="progressbar"
				aria-label="Songs you have played that have audio"
				aria-valuemin={0}
				aria-valuemax={100}
				aria-valuenow={coverage}
			>
				<div class="h-full rounded-full bg-spot-mint" style:width="{coverage}%"></div>
			</div>
			<p class="mt-1.5 text-xs text-ink-soft">
				{num(storage.tracks_with_audio)} of {num(storage.tracks_played)} songs you have played
			</p>
		</div>

		<div class="min-w-0">
			<p class="eyebrow">Last 30 days</p>
			<p class="mt-1 font-display text-3xl leading-tight font-semibold text-ink tabular-nums">
				{recent}%
			</p>
			<p class="text-xs text-ink-soft">
				of your plays in the last 30 days had audio ({num(storage.plays_30d_with_audio)} of
				{num(storage.plays_30d)})
			</p>
		</div>

		<div class="min-w-0">
			<p class="eyebrow">Last sync</p>
			<p class="mt-1 text-lg font-semibold text-ink">
				{#if storage.last_sync_at}<RelTime value={storage.last_sync_at} />{:else}Never{/if}
			</p>
			{#if storage.last_error}
				<p
					class="mt-1 inline-flex max-w-full items-center gap-1.5 rounded-full bg-spot-rose px-2.5 py-0.5 text-xs text-on-spot"
					title={storage.last_error}
				>
					<CircleAlert class="size-3.5 shrink-0" aria-hidden="true" />
					<span class="truncate">Sync failed</span>
				</p>
				<p class="sr-only">{storage.last_error}</p>
			{:else}
				<p class="text-xs text-ink-soft">
					{storage.sync_interval_s > 0
						? `Every ${Math.round(storage.sync_interval_s / 60)} min`
						: 'Scheduled sync is off'}
				</p>
			{/if}
		</div>
	</div>
</section>
