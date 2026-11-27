<script lang="ts">
	import { FileAudio } from '@lucide/svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import SearchInput from '#lib/components/SearchInput.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { bytes, num } from '#lib/format.js';
	import type { AudioFilesList } from '#lib/listenAudio.js';
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
		data: AudioFilesList | undefined;
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
		label="Search audio files"
		{onsearch}
	/>
</div>

<div class="overflow-hidden">
	{#if error && !data}
		<ErrorState message={errorMessage(error)} {onretry} />
	{:else if !data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 6 }, i (i)}<Skeleton class="h-10" />{/each}
		</div>
	{:else if data.data.length === 0}
		<EmptyState
			icon={FileAudio}
			title={q ? 'No files match' : 'No audio files yet'}
			message={q
				? 'Try a different search.'
				: 'Upload files named after the Spotify track id, like 4uLU6hMCjMI75M1A2tKUQC.mp3, then sync.'}
		/>
	{:else}
		<div class={['overflow-x-auto transition-opacity', dimmed && 'opacity-60']}>
			<table class="data-table">
				<caption class="sr-only">Uploaded audio files, newest first</caption>
				<thead>
					<tr>
						<th scope="col">Track</th>
						<th scope="col" class="w-20">Type</th>
						<th scope="col" class="w-24 text-right">Size</th>
						<th scope="col" class="w-36">Uploaded</th>
						<th scope="col" class="w-28 text-right">Your plays</th>
					</tr>
				</thead>
				<tbody>
					{#each data.data as f (f.track_id)}
						<tr>
							<td class="max-w-0 min-w-64">
								<AudioTrackCell track={f.track} trackId={f.track_id} />
							</td>
							<td><span class="chip font-mono uppercase">{f.ext}</span></td>
							<td class="text-right font-mono text-xs tabular-nums">{bytes(f.size_bytes)}</td>
							<td><RelTime value={f.last_modified ?? f.first_seen_at} /></td>
							<td class="text-right font-medium tabular-nums">{num(f.owner_plays)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pagination {offset} {limit} total={data.total} disabled={dimmed} onchange={onpage} />
	{/if}
</div>
