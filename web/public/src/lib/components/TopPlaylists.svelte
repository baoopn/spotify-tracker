<script lang="ts">
	// "Top 50 playlists": the auto-generated playlist of each completed week,
	// month and year, with a Spotify embed for the newest one of the chosen kind.
	import BrandIcon from './BrandIcon.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { periodRange, spotifyUrl } from '#lib/format.js';
	import { topPlaylistsQuery } from '#lib/queries.js';
	import { theme } from '#lib/theme.svelte.js';
	import type { TopPeriod, TopPlaylist } from '#lib/types.js';

	const PERIODS: { id: TopPeriod; label: string }[] = [
		{ id: 'week', label: 'Week' },
		{ id: 'month', label: 'Month' },
		{ id: 'year', label: 'Year' }
	];

	// Optional section: keeps the last good data on errors; with none, it stays hidden.
	const query = createQuery(() => topPlaylistsQuery());
	const data = $derived(query.data);
	const timeZone = $derived(data?.time_zone || 'UTC');

	/** Kinds with at least one published playlist, in week, month, year order. */
	const available = $derived(PERIODS.filter((p) => (data?.periods?.[p.id]?.length ?? 0) > 0));

	let chosen = $state<TopPeriod | null>(null);
	const period = $derived(available.find((p) => p.id === chosen)?.id ?? available[0]?.id ?? null);
	const entries = $derived<TopPlaylist[]>(period ? (data?.periods[period] ?? []) : []);
	const latest = $derived(entries[0] ?? null);
	const earlier = $derived(entries.slice(1));

	const nf = new Intl.NumberFormat(undefined);
	function count(n: number, one: string, many: string): string {
		return `${nf.format(n)} ${n === 1 ? one : many}`;
	}

	/** Embed URL only for a plain Spotify id; dark mode gets Spotify's dark player. */
	function embedSrc(id: string): string | null {
		if (!/^[A-Za-z0-9]+$/.test(id)) return null;
		const dark = theme.resolved === 'dark' ? '&theme=0' : '';
		return `https://open.spotify.com/embed/playlist/${id}?utm_source=generator${dark}`;
	}
	const embed = $derived(latest ? embedSrc(latest.spotify_id) : null);
	const latestHref = $derived(spotifyUrl(latest?.spotify_url));
</script>

{#if query.isPending}
	<section aria-labelledby="top-heading" aria-busy="true">
		<div class="border-b border-ink pb-3">
			<h2
				id="top-heading"
				class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
			>
				<BrandIcon name="playlist" class="size-6" />
				Top 50 playlists
			</h2>
		</div>
		<div class="top mt-6">
			<div class="space-y-3">
				<div class="skeleton h-11 w-full max-w-xs"></div>
				<div class="skeleton h-7 w-3/4"></div>
				<div class="skeleton h-4 w-1/2"></div>
			</div>
			<div class="skeleton h-[352px] w-full rounded-brand"></div>
		</div>
	</section>
{:else if latest && period}
	<section aria-labelledby="top-heading" aria-busy="false">
		<div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-1 border-b border-ink pb-3">
			<h2
				id="top-heading"
				class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
			>
				<BrandIcon name="playlist" class="size-6" />
				Top 50 playlists
			</h2>
			<p class="label text-ink-soft">Auto-generated when each week, month and year ends</p>
		</div>

		<div class="top mt-6">
			<div class="min-w-0">
				<!-- Segmented control: native radios give arrow-key navigation for free. -->
				<fieldset>
					<legend class="sr-only">Playlist period</legend>
					<div
						class="inline-grid overflow-hidden rounded-[8px] border border-ink"
						style:grid-template-columns="repeat({available.length}, minmax(5.5rem, 1fr))"
					>
						{#each available as p, i (p.id)}
							<label
								class={[
									'flex min-h-11 items-center justify-center px-4 py-1.5 text-sm font-medium transition-colors duration-150 has-checked:bg-spot-now has-checked:text-on-spot has-focus-visible:outline-2 has-focus-visible:-outline-offset-4 has-focus-visible:outline-ink',
									i > 0 && 'border-l border-ink'
								]}
							>
								<input
									type="radio"
									name="top-period"
									value={p.id}
									class="sr-only"
									checked={period === p.id}
									onchange={() => (chosen = p.id)}
								/>
								{p.label}
							</label>
						{/each}
					</div>
				</fieldset>

				<h3 class="mt-6 font-display text-xl leading-snug font-semibold break-words">
					{latest.name}
				</h3>
				<p class="mt-1 text-ink-soft">
					<time datetime={latest.period_start}
						>{periodRange(
							latest.period_start,
							latest.period_end,
							latest.time_zone || timeZone
						)}</time
					>
				</p>
				<p class="label mt-2 flex flex-wrap gap-x-2 text-ink-soft">
					<span class="whitespace-nowrap">{count(latest.track_count, 'track', 'tracks')}</span>
					<span aria-hidden="true">·</span>
					<span class="whitespace-nowrap">{count(latest.total_plays, 'play', 'plays')}</span>
				</p>
				{#if latestHref}
					<a
						href={latestHref}
						target="_blank"
						rel="noopener noreferrer"
						class="btn btn-primary mt-5"
					>
						Open in Spotify
						<span class="sr-only">(opens in a new tab)</span>
					</a>
				{/if}

				{#if earlier.length > 0}
					<h4 class="label mt-8 border-b border-rule pb-2 text-ink-soft">Earlier</h4>
					<ul class="divide-y divide-rule">
						{#each earlier as item (item.period_start)}
							{@const href = spotifyUrl(item.spotify_url)}
							{@const range = periodRange(
								item.period_start,
								item.period_end,
								item.time_zone || timeZone
							)}
							<li class="flex min-h-11 flex-wrap items-baseline justify-between gap-x-4 py-2">
								{#if href}
									<a {href} target="_blank" rel="noopener noreferrer" class="link font-medium"
										>{item.label}</a
									>
								{:else}
									<span class="font-medium">{item.label}</span>
								{/if}
								<span class="label text-ink-soft">{range}</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<div class="min-w-0">
				{#if embed}
					<iframe
						src={embed}
						title="Spotify player: {latest.name}"
						width="100%"
						height="352"
						loading="lazy"
						allow="autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture"
						class="block w-full rounded-brand border-0"
					></iframe>
				{/if}
			</div>
		</div>
	</section>
{/if}

<style>
	/* Details beside the player on wide screens, stacked on phones. */
	.top {
		display: grid;
		gap: 2rem;
	}
	@media (min-width: 64rem) {
		.top {
			grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
			align-items: start;
		}
	}
</style>
