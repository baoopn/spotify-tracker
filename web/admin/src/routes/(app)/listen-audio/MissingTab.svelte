<script lang="ts">
	import { PartyPopper, SearchX } from '@lucide/svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import type { MissingAudioList } from '#lib/listenAudio.js';
	import AudioTrackCell from './AudioTrackCell.svelte';

	let {
		data,
		error,
		dimmed,
		q,
		offset,
		limit,
		onsearch,
		onpage,
		onretry
	}: {
		data: MissingAudioList | undefined;
		error: unknown;
		dimmed: boolean;
		q: string;
		offset: number;
		limit: number;
		onsearch: (q: string) => void;
		onpage: (offset: number) => void;
		onretry: () => void;
	} = $props();
</script>

<div class="mb-4 flex flex-wrap items-center gap-3">
	<SearchInput
		value={q}
		placeholder="Search song or artist"
		label="Search missing songs"
		{onsearch}
	/>
</div>

<p class="mb-4 text-sm text-muted">
	Your most-played songs with no file yet. The track id is the file name to upload.
</p>

<div class="overflow-hidden">
	{#if error && !data}
		<ErrorState message={errorMessage(error)} {onretry} />
	{:else if !data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 6 }, i (i)}<Skeleton class="h-10" />{/each}
		</div>
	{:else if data.data.length === 0}
		{#if q}
			<EmptyState icon={SearchX} title="No songs match" message="Try a different search." />
		{:else}
			<EmptyState
				icon={PartyPopper}
				title="Nothing missing"
				message="Every song you have played has an audio file."
			/>
		{/if}
	{:else}
		<div class={['overflow-x-auto transition-opacity', dimmed && 'opacity-60']}>
			<table class="data-table">
				<caption class="sr-only">Most-played songs without an audio file</caption>
				<thead>
					<tr>
						<th scope="col">Track</th>
						<th scope="col" class="w-24 text-right">Your plays</th>
						<th scope="col" class="w-36">Last played</th>
						<th scope="col" class="w-72">File name</th>
					</tr>
				</thead>
				<tbody>
					{#each data.data as m (m.track.id)}
						<tr>
							<td class="max-w-0 min-w-64">
								<AudioTrackCell track={m.track} trackId={m.track.id} />
							</td>
							<td class="text-right font-medium tabular-nums">{num(m.owner_plays)}</td>
							<td><RelTime value={m.last_played_at} /></td>
							<td>
								<div class="flex items-center gap-2">
									<code class="truncate font-mono text-xs text-fg">{m.track.id}</code>
									<CopyButton value={m.track.id} label="Copy the track id of {m.track.name}" />
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pagination {offset} {limit} total={data.total} disabled={dimmed} onchange={onpage} />
	{/if}
</div>
