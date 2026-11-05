<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import BrandIcon from './BrandIcon.svelte';
	import RelTime from './RelTime.svelte';
	import { spotifyUrl } from '#lib/format.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { recentPlaylistsQuery } from '#lib/queries.js';

	// Optional section: keeps the last good list on errors; with none, it stays hidden.
	const query = createQuery(() => recentPlaylistsQuery({ refresh: true }));
	const playlists = $derived(query.data ?? []);

	const nf = new Intl.NumberFormat(undefined);

	function tracksLabel(n: number | null): string | null {
		if (n == null) return null;
		return `${nf.format(n)} ${n === 1 ? 'track' : 'tracks'}`;
	}
</script>

<!-- Optional section: rendered only once there is at least one playlist. -->
{#if playlists.length > 0}
	<section aria-labelledby="playlists-heading">
		<div class="border-b border-ink pb-3">
			<h2
				id="playlists-heading"
				class="flex items-center gap-2.5 font-display text-2xl font-semibold tracking-[-0.02em]"
			>
				<BrandIcon name="playlist" class="size-6" />
				Recently played playlists
			</h2>
		</div>

		<!-- A row of square sleeves. -->
		<ul class="mt-6 grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 lg:grid-cols-5">
			{#each playlists as item (item.playlist.id)}
				{@const pl = item.playlist}
				{@const href = spotifyUrl(pl.external_url)}
				{@const name = pl.name || 'Unavailable playlist'}
				{@const tracks = tracksLabel(pl.tracks_total)}
				<li class="min-w-0">
					<svelte:element
						this={href ? 'a' : 'div'}
						{href}
						target={href ? '_blank' : undefined}
						rel={href ? 'noopener noreferrer' : undefined}
						class="sleeve group block rounded-brand"
					>
						<AlbumArt
							src={pl.cover_image_url}
							alt=""
							class="cover w-full rounded-brand border border-ink"
						/>
						<p
							class="mt-3 line-clamp-2 leading-snug font-semibold break-words group-hover:underline"
							title={name}
						>
							{name}
						</p>
					</svelte:element>
					{#if pl.owner_name}
						<p class="mt-0.5 truncate text-sm text-ink-soft">by {pl.owner_name}</p>
					{/if}
					<p class="label mt-1.5 flex flex-wrap gap-x-2 text-ink-soft">
						{#if tracks}<span class="whitespace-nowrap">{tracks}</span>{/if}
						<span class="whitespace-nowrap">played <RelTime value={item.last_played} /></span>
					</p>
				</li>
			{/each}
		</ul>
	</section>
{/if}

<style>
	/* Hover/focus lifts the sleeve off the page with a spot-ink print offset. */
	.sleeve :global(.cover) {
		transition:
			transform 180ms ease-out,
			box-shadow 180ms ease-out;
	}
	.sleeve:hover :global(.cover),
	.sleeve:focus-visible :global(.cover) {
		transform: translate(-2px, -2px);
		box-shadow: 4px 4px 0 var(--spot-select);
	}
</style>
