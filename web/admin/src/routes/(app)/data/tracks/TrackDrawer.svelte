<script lang="ts">
	import OutLink from '#lib/components/OutLink.svelte';
	import { ExternalLink, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import AlbumArt from '#lib/components/AlbumArt.svelte';
	import EnrichmentBadge from '#lib/components/EnrichmentBadge.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import AudioFeatureBars from '#lib/components/charts/AudioFeatureBars.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import {
		artistNames,
		copyrightLine,
		dateTime,
		duration,
		formatReleaseDate,
		musicalKey,
		num,
		titleCase,
		trackPosition
	} from '#lib/format.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';

	let { trackId = $bindable(null) }: { trackId?: string | null } = $props();

	const queryClient = useQueryClient();
	// Enabled (and polled) only while the drawer is open.
	const detail = createQuery(() => queries.track(trackId));

	const t = $derived(detail.data && detail.data.id === trackId ? detail.data : undefined);
	const e = $derived(t?.enrichment ?? null);

	// Release rows; each one shows only when its data is present.
	const position = $derived(trackPosition(t?.track_number, t?.album?.total_tracks));
	const copyrights = $derived(t?.album?.copyrights?.filter((c) => c.text.trim()) ?? []);
	const hasRelease = $derived(
		!!position ||
			t?.album?.total_tracks != null ||
			t?.disc_number != null ||
			!!t?.album?.release_date ||
			!!t?.album?.upc ||
			copyrights.length > 0
	);
	const artistGenres = $derived(t?.artists.filter((a) => a.genres?.length) ?? []);
	const multiArtist = $derived((t?.artists.length ?? 0) > 1);

	const enrich = createMutation(() => ({
		mutationFn: (id: string) => api.enrichTrack(id),
		onSuccess: () => {
			toasts.success('Re-enrichment queued', 'The track was reset to pending.');
			// Detail + every tracks list, and the overview's enrichment counts.
			return Promise.all([
				queryClient.invalidateQueries({ queryKey: qk.tracks.all }),
				queryClient.invalidateQueries({ queryKey: qk.overview })
			]);
		},
		onError: (err) => toasts.error('Could not queue enrichment', errorMessage(err))
	}));

	function reenrich() {
		if (trackId) enrich.mutate(trackId);
	}
</script>

<Dialog
	bind:open={
		() => !!trackId,
		(v) => {
			if (!v) trackId = null;
		}
	}
	variant="drawer"
	title={t?.name ?? 'Track'}
>
	{#if detail.isError && !t}
		<ErrorState message={errorMessage(detail.error)} onretry={() => detail.refetch()} />
	{:else if !t}
		<div class="flex flex-col gap-4" aria-busy="true">
			<div class="flex gap-4">
				<Skeleton class="size-24 rounded-2xl" /><Skeleton class="h-24 flex-1" />
			</div>
			<Skeleton class="h-40" /><Skeleton class="h-24" />
		</div>
	{:else}
		<div class="flex flex-col gap-6">
			<div class="flex gap-4">
				<AlbumArt
					src={t.album?.cover_image_url}
					alt={t.album?.name ? `Cover of ${t.album.name}` : ''}
					size={96}
					rounded="rounded-2xl"
				/>
				<div class="min-w-0">
					<p class="text-sm text-muted">{artistNames(t.artists)}</p>
					{#if t.album}<p class="text-sm text-muted">
							{t.album.name}{#if t.album.release_date}&nbsp;· {formatReleaseDate(
									t.album.release_date
								)}{/if}
						</p>{/if}
					<div class="mt-2 flex flex-wrap gap-2">
						<EnrichmentBadge status={e?.status ?? 'pending'} />
						{#if t.explicit}<span class="chip">Explicit</span>{/if}
						{#if t.album?.album_type}<span class="chip">{titleCase(t.album.album_type)}</span>{/if}
					</div>
					{#if t.external_url}
						<OutLink
							href={t.external_url}
							class="mt-2 inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline"
							>Open in Spotify <ExternalLink class="size-3.5" aria-hidden="true" /></OutLink
						>
					{/if}
				</div>
			</div>

			<dl class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
				<div class="rounded-xl bg-surface-2 px-3 py-2">
					<dt class="text-xs text-muted">Plays</dt>
					<dd class="font-semibold text-fg">{num(t.plays)}</dd>
				</div>
				<div class="rounded-xl bg-surface-2 px-3 py-2">
					<dt class="text-xs text-muted">Length</dt>
					<dd class="font-semibold text-fg">{duration(t.duration_ms)}</dd>
				</div>
				<div class="rounded-xl bg-surface-2 px-3 py-2">
					<dt class="text-xs text-muted">Last played</dt>
					<dd class="font-semibold text-fg"><RelTime value={t.last_played} /></dd>
				</div>
				<div class="rounded-xl bg-surface-2 px-3 py-2">
					<dt class="text-xs text-muted">Added</dt>
					<dd class="font-semibold text-fg"><RelTime value={t.created_at} /></dd>
				</div>
			</dl>

			<section aria-labelledby="rel-h">
				<h3 id="rel-h" class="mb-2 text-sm font-semibold text-fg">Release</h3>
				{#if hasRelease}
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
						{#if position}
							<dt class="text-muted">Track</dt>
							<dd class="text-fg">{position}</dd>
						{:else if t.album?.total_tracks != null}
							<dt class="text-muted">Tracks on release</dt>
							<dd class="text-fg">{t.album.total_tracks}</dd>
						{/if}
						{#if t.disc_number != null}
							<dt class="text-muted">Disc</dt>
							<dd class="text-fg">{t.disc_number}</dd>
						{/if}
						{#if t.album?.release_date}
							<dt class="text-muted">Released</dt>
							<dd class="text-fg">{formatReleaseDate(t.album.release_date)}</dd>
						{/if}
						{#if t.album?.upc}
							<dt class="text-muted">UPC</dt>
							<dd class="font-mono text-fg">{t.album.upc}</dd>
						{/if}
						{#if copyrights.length}
							<dt class="text-muted">Copyrights</dt>
							<dd class="min-w-0">
								{#each copyrights as c, i (i)}
									<p class="text-xs break-words text-muted">{copyrightLine(c)}</p>
								{/each}
							</dd>
						{/if}
					</dl>
				{:else}
					<p class="text-sm text-muted">No release details yet.</p>
				{/if}
			</section>

			<section aria-labelledby="feat-h">
				<h3 id="feat-h" class="mb-3 text-sm font-semibold text-fg">Audio features</h3>
				{#if e?.features}
					<AudioFeatureBars features={e.features} label="Audio features of {t.name}" />
					<dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
						<div class="rounded-xl bg-surface-2 px-3 py-2">
							<dt class="text-xs text-muted">Key</dt>
							<dd class="font-semibold text-fg">{musicalKey(e.features.key, e.features.mode)}</dd>
						</div>
						<div class="rounded-xl bg-surface-2 px-3 py-2">
							<dt class="text-xs text-muted">Time signature</dt>
							<dd class="font-semibold text-fg">
								{e.features.time_signature != null ? `${e.features.time_signature}/4` : '–'}
							</dd>
						</div>
					</dl>
				{:else}
					<p class="text-sm text-muted">No audio features yet.</p>
				{/if}
			</section>

			<section aria-labelledby="genre-h">
				<h3 id="genre-h" class="mb-2 text-sm font-semibold text-fg">Genres</h3>
				{#if e?.genres?.length}
					<ul class="flex flex-col gap-2">
						{#each e.genres as g (g.root)}
							<li>
								<span class="chip">{g.root}</span>
								{#if g.sub.length}
									<span class="ml-1 text-xs text-muted">{g.sub.join(', ')}</span>
								{/if}
							</li>
						{/each}
					</ul>
				{:else}
					<p class="text-sm text-muted">No genres yet.</p>
				{/if}
				{#if artistGenres.length}
					<h4 class="mt-4 mb-2 text-xs font-semibold text-muted">Spotify artist genres</h4>
					<ul class="flex flex-col gap-2">
						{#each artistGenres as a (a.id)}
							<li class="flex flex-wrap items-center gap-1.5">
								{#if multiArtist}<span class="text-xs text-muted">{a.name}:</span>{/if}
								{#each a.genres ?? [] as g (g)}
									<span class="chip">{g}</span>
								{/each}
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<section aria-labelledby="enr-h">
				<h3 id="enr-h" class="mb-2 text-sm font-semibold text-fg">Enrichment</h3>
				{#if e}
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
						<dt class="text-muted">Source</dt>
						<dd class="text-fg">{e.source ?? '–'}</dd>
						<dt class="text-muted">Attempts</dt>
						<dd class="text-fg">{num(e.attempts)}</dd>
						<dt class="text-muted">Enriched</dt>
						<dd class="text-fg">{dateTime(e.enriched_at)}</dd>
						<dt class="text-muted">Next attempt</dt>
						<dd class="text-fg">{dateTime(e.next_attempt_at)}</dd>
						{#if t.isrc}
							<dt class="text-muted">ISRC</dt>
							<dd class="font-mono text-fg">{t.isrc}</dd>
						{/if}
					</dl>
					{#if e.last_error}
						<p class="mt-3 rounded-xl bg-spot-rose px-3 py-2 text-sm break-words text-on-spot">
							<span class="font-medium">Last error:</span>
							<span class="font-mono">{e.last_error}</span>
						</p>
					{/if}
				{:else}
					<p class="text-sm text-muted">This track has not been processed yet.</p>
				{/if}
			</section>
		</div>
	{/if}

	{#snippet footer()}
		<button type="button" class="btn btn-secondary" onclick={() => (trackId = null)}>Close</button>
		<button
			type="button"
			class="btn btn-primary"
			onclick={reenrich}
			disabled={enrich.isPending || !t}
		>
			{#if enrich.isPending}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<RefreshCw class="size-4" aria-hidden="true" />{/if}
			Re-enrich
		</button>
	{/snippet}
</Dialog>
