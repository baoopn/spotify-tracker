<script lang="ts">
	// "Top 50 playlists": the kept playlists of the chosen kind (week, month or year)
	// as a pick list beside a Spotify embed of the picked one. The newest can be live:
	// the running period's playlist, updated while it runs.
	import BrandIcon from './BrandIcon.svelte';
	import RelTime from './RelTime.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { liveRange, periodRange, spotifyUrl } from '#lib/format.js';
	import { topPlaylistsQuery } from '#lib/queries.js';
	import { theme } from '#lib/theme.svelte.js';
	import type { TopPeriod, TopPlaylist } from '#lib/types.js';

	const PERIODS: { id: TopPeriod; label: string }[] = [
		{ id: 'week', label: 'Week' },
		{ id: 'month', label: 'Month' },
		{ id: 'year', label: 'Year' }
	];

	const SO_FAR: Record<TopPeriod, string> = {
		week: 'This week so far',
		month: 'This month so far',
		year: 'This year so far'
	};

	// Optional section: keeps the last good data on errors; with none, it stays hidden.
	const query = createQuery(() => topPlaylistsQuery());
	const data = $derived(query.data);
	const timeZone = $derived(data?.time_zone || 'UTC');

	/** Kinds with at least one published playlist, in week, month, year order. */
	const available = $derived(PERIODS.filter((p) => (data?.periods?.[p.id]?.length ?? 0) > 0));

	let chosen = $state<TopPeriod | null>(null);
	const period = $derived(available.find((p) => p.id === chosen)?.id ?? available[0]?.id ?? null);
	const entries = $derived<TopPlaylist[]>(period ? (data?.periods[period] ?? []) : []);

	// The picked entry by period start; falls back to the newest when it is gone.
	let picked = $state<string | null>(null);
	const selected = $derived(entries.find((e) => e.period_start === picked) ?? entries[0] ?? null);

	function chooseKind(id: TopPeriod) {
		chosen = id;
		picked = null; // a new kind starts at its newest playlist
	}

	/** Defensive: a "live" entry whose period has ended is shown as a finished one. */
	function isLive(e: TopPlaylist): boolean {
		return e.live && Date.parse(e.period_end) > Date.now();
	}

	function rangeOf(e: TopPlaylist): string {
		const tz = e.time_zone || timeZone;
		return isLive(e)
			? liveRange(e.period_start, tz)
			: periodRange(e.period_start, e.period_end, tz);
	}

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
	const embed = $derived(selected ? embedSrc(selected.spotify_id) : null);
	const selectedHref = $derived(spotifyUrl(selected?.spotify_url));
</script>

{#snippet heading()}
	<div class="border-b border-ink pb-3">
		<h2
			id="top-heading"
			class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
		>
			<BrandIcon name="playlist" class="size-6" />
			Top 50 playlists
		</h2>
	</div>
{/snippet}

{#if query.isPending}
	<section aria-labelledby="top-heading" aria-busy="true">
		{@render heading()}
		<div class="top mt-6">
			<div class="space-y-2">
				<div class="skeleton h-14 w-full"></div>
				<div class="skeleton h-14 w-full"></div>
				<div class="skeleton h-14 w-full"></div>
			</div>
			<div class="skeleton h-[352px] w-full rounded-brand"></div>
		</div>
	</section>
{:else if selected && period}
	<section aria-labelledby="top-heading" aria-busy="false">
		{@render heading()}

		<div class="top mt-6">
			<div class="min-w-0">
				{#if available.length > 1}
					<!-- Segmented control: native radios give arrow-key navigation for free. -->
					<fieldset class="mb-5">
						<legend class="sr-only">Playlist period</legend>
						<div
							class="inline-grid max-w-full overflow-hidden rounded-[8px] border border-ink"
							style:grid-template-columns="repeat({available.length}, minmax(4.5rem, 1fr))"
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
										onchange={() => chooseKind(p.id)}
									/>
									{p.label}
								</label>
							{/each}
						</div>
					</fieldset>
				{/if}

				<!-- Pick list: each kept playlist of the kind; the picked one plays beside it. -->
				<fieldset>
					<legend class="sr-only">Playlist</legend>
					<div class="overflow-hidden rounded-[8px] border border-ink">
						{#each entries as item, i (item.period_start)}
							{@const live = isLive(item)}
							<label
								class={[
									'group flex min-h-11 cursor-pointer flex-col justify-center gap-0.5 px-4 py-2.5 transition-colors duration-150 has-checked:bg-spot-now has-checked:text-on-spot has-focus-visible:outline-2 has-focus-visible:-outline-offset-4 has-focus-visible:outline-ink',
									i > 0 && 'border-t border-ink'
								]}
							>
								<input
									type="radio"
									name="top-entry"
									value={item.period_start}
									class="sr-only"
									checked={selected.period_start === item.period_start}
									onchange={() => (picked = item.period_start)}
								/>
								<span class="flex min-w-0 items-center gap-2 font-medium break-words">
									{#if live}
										<span
											class="size-2 shrink-0 rounded-full border border-ink bg-spot-mint"
											aria-hidden="true"
										></span>
										{SO_FAR[item.period]}
									{:else}
										{item.label}
									{/if}
								</span>
								<span
									class="flex flex-wrap gap-x-2 text-sm text-ink-soft group-has-checked:text-on-spot"
								>
									<time datetime={item.period_start}>{rangeOf(item)}</time>
									<span aria-hidden="true">·</span>
									<span class="whitespace-nowrap">{count(item.track_count, 'track', 'tracks')}</span
									>
									{#if live && item.refreshed_at}
										<span aria-hidden="true">·</span>
										<span class="whitespace-nowrap"
											>updated <RelTime value={item.refreshed_at} /></span
										>
									{/if}
								</span>
							</label>
						{/each}
					</div>
				</fieldset>
			</div>

			<div class="min-w-0">
				{#if embed}
					{#key selected.spotify_id}
						<iframe
							src={embed}
							title="Spotify player: {selected.name}"
							width="100%"
							height="352"
							loading="lazy"
							allow="autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture"
							class="block w-full rounded-brand border-0"
						></iframe>
					{/key}
				{:else if selectedHref}
					<a href={selectedHref} target="_blank" rel="noopener noreferrer" class="link font-medium">
						Open in Spotify
						<span class="sr-only">(opens in a new tab)</span>
					</a>
				{/if}
			</div>
		</div>
	</section>
{/if}

<style>
	/* Pick list beside the player on wide screens, stacked on phones (list first). */
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
