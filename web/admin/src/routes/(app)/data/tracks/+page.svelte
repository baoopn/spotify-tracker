<script lang="ts">
	import { ArrowDown, ArrowUp, FileAudio, Music, RotateCcw } from '@lucide/svelte';
	import { page } from '$app/state';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import TrackCell from '#lib/components/TrackCell.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import EnrichmentBadge from '#lib/components/EnrichmentBadge.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import Segmented from '#lib/components/Segmented.svelte';
	import TrackDrawer from './TrackDrawer.svelte';
	import LyricsSearchResults from './LyricsSearchResults.svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { api, errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { qk, queries } from '#lib/queries.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { AUDIO_FILTERS, AUDIO_FILTER_LABELS, type AudioFilter } from '#lib/listenAudio.js';
	import {
		ENRICHMENT_STATUSES,
		LYRICS_FILTERS,
		type EnrichmentStatus,
		type LyricsFilter,
		type SortOrder,
		type TrackRow,
		type TrackSort
	} from '#lib/types.js';

	const LIMIT = 25;

	function initialStatus(): EnrichmentStatus | '' {
		const s = page.url.searchParams.get('status');
		return ENRICHMENT_STATUSES.includes(s as EnrichmentStatus) ? (s as EnrichmentStatus) : '';
	}

	function initialLyrics(): LyricsFilter | '' {
		const s = page.url.searchParams.get('lyrics');
		return LYRICS_FILTERS.includes(s as LyricsFilter) ? (s as LyricsFilter) : '';
	}

	function initialAudio(): AudioFilter | '' {
		const s = page.url.searchParams.get('audio');
		return AUDIO_FILTERS.includes(s as AudioFilter) ? (s as AudioFilter) : '';
	}

	const LYRICS_OPTIONS: Record<LyricsFilter, string> = {
		missing: 'Missing',
		unsynced: 'Plain only (no timing)',
		found: 'Found',
		instrumental: 'Instrumental'
	};

	/** The lyrics chip of a row; muted text for states with no lyrics. */
	function lyricsState(row: TrackRow): { label: string; chip: boolean } {
		switch (row.lyrics_status) {
			case 'found':
				return { label: row.lyrics_synced ? 'Synced' : 'Plain', chip: true };
			case 'instrumental':
				return { label: 'Instrumental', chip: true };
			case 'not_found':
				return { label: 'Not found', chip: false };
			case 'error':
				return { label: 'Failed', chip: false };
			default:
				return { label: 'Not looked up', chip: false };
		}
	}

	let q = $state('');
	/** Search by track names (the paginated table) or by lyric text. */
	let mode = $state<'names' | 'lyrics'>('names');
	let lyricsQ = $state('');
	let status = $state<EnrichmentStatus | ''>(initialStatus());
	let lyrics = $state<LyricsFilter | ''>(initialLyrics());
	let audio = $state<AudioFilter | ''>(initialAudio());
	let sort = $state<TrackSort>('plays');
	let order = $state<SortOrder>('desc');
	let offset = $state(0);
	let selected = $state<string | null>(null);
	let confirmRetry = $state(false);

	const queryClient = useQueryClient();
	// Only the names table polls; it is hidden in lyrics mode.
	const list = createQuery(() => ({
		...queries.tracks({ q, status, lyrics, audio, sort, order, limit: LIMIT, offset }),
		enabled: mode === 'names'
	}));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => list.refetch());

	const emptyMessage = $derived.by(() => {
		if (lyrics === 'missing' && !q && !status)
			return 'Every track has lyrics, or is marked instrumental.';
		if (lyrics)
			return `No tracks match the lyrics filter “${LYRICS_OPTIONS[lyrics]}”. Try another filter or search.`;
		if (audio === 'missing' && !q && !status && !lyrics) return 'Every track has an audio file.';
		if (q || status || audio) return 'Try a different search or filter.';
		return 'No tracks have been recorded yet.';
	});

	function setSort(s: TrackSort) {
		if (sort === s) order = order === 'desc' ? 'asc' : 'desc';
		else {
			sort = s;
			order = s === 'name' ? 'asc' : 'desc';
		}
		offset = 0;
	}

	const retryFailed = createMutation(() => ({
		mutationFn: () => api.retryFailed(),
		onSuccess: ({ requeued }) => {
			toasts.success(
				'Failed tracks requeued',
				`${num(requeued)} ${requeued === 1 ? 'track' : 'tracks'} queued for enrichment.`
			);
			// Returned so the confirm dialog stays busy until the lists are fresh.
			return Promise.all([
				queryClient.invalidateQueries({ queryKey: qk.tracks.all }),
				queryClient.invalidateQueries({ queryKey: qk.overview })
			]);
		},
		onError: (err) => toasts.error('Could not requeue failed tracks', errorMessage(err))
	}));

	async function confirmRetryFailed() {
		try {
			await retryFailed.mutateAsync();
		} catch {
			// already reported by onError
		}
	}

	const columns: { key: TrackSort | null; label: string; cls?: string }[] = [
		{ key: 'name', label: 'Track' },
		{ key: 'plays', label: 'Plays', cls: 'w-24 text-right' },
		{ key: 'last_played', label: 'Last played', cls: 'w-40' },
		{ key: null, label: 'Enrichment', cls: 'w-36' },
		{ key: null, label: 'Lyrics', cls: 'w-32' },
		{ key: 'created', label: 'Added', cls: 'w-36' }
	];
</script>

<PageHeader
	title="Tracks"
	eyebrow="Library"
	description="Every song in the library, with play counts and audio-feature status."
>
	{#snippet actions()}
		<button type="button" class="btn btn-secondary" onclick={() => (confirmRetry = true)}>
			<RotateCcw class="size-4" aria-hidden="true" /> Retry all failed
		</button>
	{/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap items-center gap-3">
	<Segmented
		label="Search by"
		size="sm"
		bind:value={mode}
		options={[
			{ value: 'names', label: 'Names' },
			{ value: 'lyrics', label: 'Lyrics' }
		]}
	/>
	{#if mode === 'lyrics'}
		<SearchInput
			value={lyricsQ}
			placeholder="Words from the lyrics"
			label="Search lyrics"
			maxlength={200}
			onsearch={(v) => (lyricsQ = v)}
		/>
		<p class="text-xs text-muted">Accent-insensitive. Use "quotes" for an exact phrase.</p>
	{:else}
		<SearchInput
			value={q}
			placeholder="Search tracks, ISRC or Spotify ID/link"
			label="Search tracks"
			onsearch={(v) => {
				q = v;
				offset = 0;
			}}
		/>
		<label class="flex items-center gap-2 text-sm text-muted">
			Status
			<select
				class="input w-auto"
				bind:value={status}
				onchange={() => (offset = 0)}
				aria-label="Filter by enrichment status"
			>
				<option value="">All</option>
				{#each ENRICHMENT_STATUSES as s (s)}
					<option value={s}>{s[0].toUpperCase() + s.slice(1)}</option>
				{/each}
			</select>
		</label>
		<label class="flex items-center gap-2 text-sm text-muted">
			Lyrics
			<select
				class="input w-auto"
				bind:value={lyrics}
				onchange={() => (offset = 0)}
				aria-label="Filter by lyrics"
			>
				<option value="">All</option>
				{#each LYRICS_FILTERS as f (f)}
					<option value={f}>{LYRICS_OPTIONS[f]}</option>
				{/each}
			</select>
		</label>
		<label class="flex items-center gap-2 text-sm text-muted">
			Audio
			<select
				class="input w-auto"
				bind:value={audio}
				onchange={() => (offset = 0)}
				aria-label="Filter by audio file"
			>
				<option value="">All</option>
				{#each AUDIO_FILTERS as f (f)}
					<option value={f}>{AUDIO_FILTER_LABELS[f]}</option>
				{/each}
			</select>
		</label>
	{/if}
</div>

{#if mode === 'lyrics'}
	<LyricsSearchResults q={lyricsQ} onselect={(id) => (selected = id)} />
{:else}
	<div class="overflow-hidden">
		{#if list.isError && !list.data}
			<ErrorState message={errorMessage(list.error)} onretry={() => manual.run()} />
		{:else if !list.data}
			<div class="flex flex-col gap-3 p-4" aria-busy="true">
				{#each { length: 8 }, i (i)}
					<div class="flex items-center gap-3">
						<Skeleton class="size-10" /><Skeleton class="h-4 flex-1" /><Skeleton class="h-4 w-24" />
					</div>
				{/each}
			</div>
		{:else if list.data.data.length === 0}
			<EmptyState icon={Music} title="No tracks found" message={emptyMessage} />
		{:else}
			<div
				class={[
					'overflow-x-auto transition-opacity',
					(list.isPlaceholderData || manual.active) && 'opacity-60'
				]}
			>
				<table class="data-table">
					<caption class="sr-only">
						Tracks, sorted by {sort}
						{order === 'asc' ? 'ascending' : 'descending'}. Select a row for details.
					</caption>
					<thead>
						<tr>
							{#each columns as col (col.label)}
								<th
									scope="col"
									class={col.cls}
									aria-sort={col.key && sort === col.key
										? order === 'asc'
											? 'ascending'
											: 'descending'
										: undefined}
								>
									{#if col.key}
										{@const key = col.key}
										<button
											type="button"
											class="inline-flex items-center gap-1 rounded hover:text-fg"
											onclick={() => setSort(key)}
										>
											{col.label}
											{#if sort === key}
												{#if order === 'asc'}<ArrowUp
														class="size-3.5"
														aria-hidden="true"
													/>{:else}<ArrowDown class="size-3.5" aria-hidden="true" />{/if}
											{/if}
										</button>
									{:else}
										{col.label}
									{/if}
								</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each list.data.data as row (row.track.id)}
							{@const ly = lyricsState(row)}
							<tr class="row-link" onclick={() => (selected = row.track.id)}>
								<td class="max-w-0 min-w-64">
									<button
										type="button"
										class="block w-full rounded-lg text-left"
										aria-label="Show details for {row.track.name}"
										onclick={(e) => {
											e.stopPropagation();
											selected = row.track.id;
										}}
									>
										<span class="flex min-w-0 items-center gap-2">
											<span class="min-w-0 flex-1"><TrackCell track={row.track} /></span>
											{#if row.has_audio}
												<span
													class="inline-flex shrink-0 items-center gap-1 rounded-full bg-spot-sky px-2 py-0.5 font-mono text-[0.6875rem] tracking-[0.06em] text-on-spot uppercase"
													title="A listen-along audio file is uploaded"
												>
													<FileAudio class="size-3.5" aria-hidden="true" /> Audio
												</span>
											{/if}
										</span>
									</button>
								</td>
								<td class="text-right font-medium tabular-nums">{num(row.plays)}</td>
								<td><RelTime value={row.last_played} /></td>
								<td><EnrichmentBadge status={row.enrichment_status} /></td>
								<td>
									{#if ly.chip}
										<span class="chip whitespace-nowrap">{ly.label}</span>
									{:else}
										<span class="text-xs whitespace-nowrap text-muted">{ly.label}</span>
									{/if}
								</td>
								<td><RelTime value={row.created_at} /></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<Pagination
				{offset}
				limit={LIMIT}
				total={list.data.total}
				disabled={list.isPlaceholderData}
				onchange={(o) => (offset = o)}
			/>
		{/if}
	</div>
{/if}

<TrackDrawer bind:trackId={selected} />

<ConfirmDialog
	bind:open={confirmRetry}
	title="Retry all failed tracks?"
	message="Every track whose enrichment failed is reset to pending and queued again. This may take a while and uses provider quota."
	confirmLabel="Retry failed"
	onconfirm={confirmRetryFailed}
/>
