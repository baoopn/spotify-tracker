<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import ArtistLinks from './ArtistLinks.svelte';
	import BrandIcon from './BrandIcon.svelte';
	import ExplicitBadge from './ExplicitBadge.svelte';
	import RelTime from './RelTime.svelte';
	import { spotifyUrl } from '#lib/format.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/api.js';
	import { live } from '#lib/live.svelte.js';
	import { historyQuery, loadStatus, nowPlayingQuery } from '#lib/queries.js';

	const SHOW = 5;

	const now = createQuery(() => nowPlayingQuery());
	// The history observer that owns the periodic safety-net refresh.
	const history = createQuery(() => historyQuery({ refresh: true }));
	const historyStatus = $derived(loadStatus(history));

	// Skip the newest entry when it is the track currently playing (shown in the hero).
	const items = $derived.by(() => {
		const np = now.data;
		const all = history.data ?? [];
		const [first, ...rest] = all;
		const dupe = !!first && !!np?.is_playing && np.track?.id === first.track.id;
		return (dupe ? rest : all).slice(0, SHOW);
	});

	const pad = (n: number) => String(n).padStart(2, '0');
</script>

<section aria-labelledby="recent-heading" aria-busy={historyStatus === 'loading'}>
	<div class="flex items-end justify-between gap-4 border-b border-ink pb-3">
		<h2
			id="recent-heading"
			class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
		>
			<BrandIcon name="track" class="size-6" />
			Recently played
		</h2>
		{#if historyStatus === 'ready' && items.length > 0}
			<p class="label text-ink-soft">{pad(items.length)} tracks</p>
		{/if}
	</div>

	{#if historyStatus === 'loading'}
		<ol aria-hidden="true">
			{#each [1, 2, 3, 4, 5] as i (i)}
				<li class="row">
					<span class="label w-7 text-ink-soft">{pad(i)}</span>
					<div class="skeleton size-11 shrink-0"></div>
					<div class="flex-1 space-y-2">
						<div class="skeleton h-4 w-1/2"></div>
						<div class="skeleton h-3 w-1/3"></div>
					</div>
				</li>
			{/each}
		</ol>
	{:else if historyStatus === 'error'}
		<div class="flex flex-wrap items-center gap-4 border-b border-rule py-6" role="alert">
			<p class="chip label [--chip:var(--spot-rose)]">
				<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
				Couldn't load
			</p>
			<p class="text-ink-soft">{errorMessage(history.error)}</p>
			<button type="button" class="btn btn-secondary" onclick={() => live.retry()}>Try again</button
			>
		</div>
	{:else if items.length === 0}
		<p class="border-b border-rule py-6 text-ink-soft">Nothing played yet.</p>
	{:else}
		<ol>
			{#each items as entry, i (entry.id)}
				{@const href = spotifyUrl(entry.track.external_url)}
				<li class="row">
					<span class="label w-7 shrink-0 text-ink-soft" aria-hidden="true">{pad(i + 1)}</span>
					<AlbumArt
						src={entry.track.album?.cover_image_url}
						alt={entry.track.album?.name ? `Album cover of ${entry.track.album.name}` : ''}
						class="size-11 rounded-[6px] border border-rule-strong"
					/>
					<div class="min-w-0 flex-1">
						<p class="flex items-center gap-1.5 font-semibold">
							<span class="truncate">
								{#if href}
									<a {href} target="_blank" rel="noopener noreferrer" class="link"
										>{entry.track.name}</a
									>
								{:else}
									{entry.track.name}
								{/if}
							</span>
							{#if entry.track.explicit}<ExplicitBadge />{/if}
						</p>
						<p class="truncate text-sm text-ink-soft">
							<ArtistLinks artists={entry.track.artists} />
						</p>
					</div>
					<div class="flex shrink-0 flex-col items-end gap-1 text-right">
						<RelTime value={entry.played_at} class="text-xs text-ink-soft" />
						{#if entry.replay_count > 1}
							<span
								class="chip label px-2! py-0! [--chip:var(--spot-select)]"
								title="Played {entry.replay_count} times in a row"
							>
								<span aria-hidden="true">×{entry.replay_count}</span>
								<span class="sr-only">played {entry.replay_count} times in a row</span>
							</span>
						{/if}
					</div>
				</li>
			{/each}
		</ol>
	{/if}
</section>

<style>
	/* Ruled tracklist rows, like the back of a sleeve. */
	.row {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		min-height: 4.25rem;
		padding-block: 0.6rem;
		border-bottom: 1px solid var(--rule);
	}
</style>
