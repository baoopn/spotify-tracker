<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import RelTime from '../RelTime.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { compact } from '#lib/format.js';
	import { queries } from '#lib/queries.js';

	// The LRCLIB lookup queue: the song playing now, upcoming songs and
	// re-queues first, then the rest of the library.
	const query = createQuery(() => queries.lyricsQueue());
	const s = $derived(query.data);
	const busy = $derived(!!s && (s.queued > 0 || s.in_progress > 0));
	// Joined in JS: separators in markup lose their spaces around {#if} blocks.
	const SEP = ' · ';
	const results = $derived(
		s
			? [
					`${compact(s.found)} found`,
					`${compact(s.instrumental)} instrumental`,
					`${compact(s.not_found)} not found`,
					s.error ? `${compact(s.error)} failed` : ''
				]
					.filter(Boolean)
					.join(SEP)
			: ''
	);
	const waiting = $derived(
		s
			? [
					s.in_progress ? `${compact(s.in_progress)} looking up now` : '',
					`${compact(s.queued)} queued` +
						(s.requested ? ` (${compact(s.requested)} re-queued)` : ''),
					s.backlog ? `${compact(s.backlog)} never looked up` : ''
				]
					.filter(Boolean)
					.join(SEP)
			: ''
	);
</script>

<section class="flex flex-col gap-3 border-t border-ink pt-4" aria-labelledby="lq-title">
	<div class="flex items-center justify-between gap-2">
		<h2 id="lq-title" class="eyebrow">Lyrics queue</h2>
		{#if s && !s.enabled}
			<span class="chip">Off</span>
		{:else if busy && s}
			<span class="chip" role="status">{compact(s.queued + s.in_progress)} to look up</span>
		{/if}
	</div>

	{#if s}
		<div class="text-sm">
			<p class="text-fg">{results}</p>
			<p class="text-xs text-muted">{waiting}</p>
			<p class="text-xs text-muted">
				Last lookup <RelTime value={s.last_lookup_at} />{#if s.next_retry_at}
					· next retry <RelTime value={s.next_retry_at} />{/if}
			</p>
			{#if !s.enabled}
				<p class="text-xs text-muted">The lyrics fetcher is off (LYRICS_ENABLED=false).</p>
			{/if}
			<a
				href={resolve('data/tracks?lyrics=missing')}
				class="mt-2 inline-block text-sm font-medium text-primary hover:underline"
			>
				Find songs without lyrics
			</a>
		</div>
	{:else if query.isError}
		<p class="text-sm text-muted">The lyrics queue is unavailable.</p>
	{:else}
		<Skeleton class="h-4 w-2/3" /><Skeleton class="h-3 w-1/2" />
	{/if}
</section>
