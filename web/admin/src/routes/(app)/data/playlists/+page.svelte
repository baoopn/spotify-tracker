<script lang="ts">
	import { ExternalLink, ListMusic, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import AlbumArt from '#lib/components/AlbumArt.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import OutLink from '#lib/components/OutLink.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { Query } from '#lib/query.svelte.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { OffsetList, Playlist, RecentPlaylist } from '#lib/types.js';

	const LIMIT = 25;

	let offset = $state(0);
	let refreshing = $state<string | null>(null);
	const list = new Query<OffsetList<RecentPlaylist>>();

	$effect(() => {
		const params = { limit: LIMIT, offset };
		void list.run((signal) => api.playlists(params, { signal }));
		return () => list.abort();
	});

	function label(p: Playlist): string {
		return p.available && p.name ? p.name : 'Spotify mix / unavailable';
	}

	async function refresh(p: Playlist) {
		refreshing = p.id;
		try {
			const updated = await api.refreshPlaylist(p.id);
			if (list.data) {
				list.data = {
					...list.data,
					data: list.data.data.map((r) =>
						r.playlist.id === updated.id ? { ...r, playlist: updated } : r
					)
				};
			}
			toasts.success(
				'Playlist metadata refreshed',
				updated.available ? label(updated) : 'Spotify reports this playlist as unavailable.'
			);
		} catch (err) {
			toasts.error('Could not refresh playlist', errorMessage(err));
		} finally {
			refreshing = null;
		}
	}
</script>

<PageHeader
	title="Playlists"
	eyebrow="Data"
	description="Every playlist played from, newest first, private and Spotify mixes included."
/>

<div class="overflow-hidden">
	{#if list.error && !list.data}
		<ErrorState message={list.error} onretry={() => list.refetch()} />
	{:else if !list.data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 6 }, i (i)}
				<div class="flex items-center gap-3">
					<Skeleton class="size-12" /><Skeleton class="h-4 flex-1" /><Skeleton class="h-4 w-24" />
				</div>
			{/each}
		</div>
	{:else if list.data.data.length === 0}
		<EmptyState
			icon={ListMusic}
			title="No playlists yet"
			message="Playlists appear here once you play something from one."
		/>
	{:else}
		<div class={['overflow-x-auto transition-opacity', list.loading && 'opacity-60']}>
			<table class="data-table">
				<caption class="sr-only">Playlists played from</caption>
				<thead>
					<tr>
						<th scope="col" class="w-16"><span class="sr-only">Cover</span></th>
						<th scope="col">Playlist</th>
						<th scope="col">Owner</th>
						<th scope="col">Visibility</th>
						<th scope="col" class="text-right">Tracks</th>
						<th scope="col" class="text-right">Plays</th>
						<th scope="col" class="w-40">Last played</th>
						<th scope="col" class="w-44"><span class="sr-only">Actions</span></th>
					</tr>
				</thead>
				<tbody>
					{#each list.data.data as row (row.playlist.id)}
						{@const p = row.playlist}
						<tr>
							<td><AlbumArt src={p.available ? p.cover_image_url : null} size={48} /></td>
							<td class="max-w-72">
								{#if p.available && p.name && p.external_url}
									<OutLink
										href={p.external_url}
										class="inline-flex max-w-full items-center gap-1 font-medium text-fg hover:text-primary hover:underline"
										><span class="truncate">{p.name}</span>
										<ExternalLink class="size-3.5 shrink-0" aria-hidden="true" /></OutLink
									>
								{:else if p.available && p.name}
									<span class="block truncate font-medium text-fg">{p.name}</span>
								{:else}
									<span class="text-muted italic">Spotify mix / unavailable</span>
								{/if}
								{#if p.available && p.description}
									<p class="truncate text-xs text-muted" title={p.description}>
										{p.description}
									</p>
								{/if}
							</td>
							<td class="max-w-48 truncate" title={p.owner_name ?? p.owner_id}>
								{p.owner_name || p.owner_id || '–'}
							</td>
							<td>
								<div class="flex flex-wrap gap-1">
									{#if p.public === true}<Badge>Public</Badge>{/if}
									{#if p.public === false}<Badge>Private</Badge>{/if}
									{#if p.collaborative}<Badge>Collaborative</Badge>{/if}
									{#if p.public === null && !p.collaborative}<span class="text-muted">–</span>{/if}
								</div>
							</td>
							<td class="text-right tabular-nums">{num(p.tracks_total)}</td>
							<td class="text-right font-medium tabular-nums">{num(row.plays)}</td>
							<td><RelTime value={row.last_played} /></td>
							<td class="text-right">
								<button
									type="button"
									class="btn btn-secondary btn-sm"
									onclick={() => refresh(p)}
									disabled={refreshing === p.id}
									aria-label="Refresh metadata for {label(p)}"
								>
									{#if refreshing === p.id}
										<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
									{:else}
										<RefreshCw class="size-4" aria-hidden="true" />
									{/if}
									Refresh metadata
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pagination
			{offset}
			limit={LIMIT}
			total={list.data.total}
			disabled={list.loading}
			onchange={(o) => (offset = o)}
		/>
	{/if}
</div>
