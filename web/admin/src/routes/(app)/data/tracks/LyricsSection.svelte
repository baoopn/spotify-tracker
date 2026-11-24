<script lang="ts">
	import OutLink from '#lib/components/OutLink.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { queries } from '#lib/queries.js';

	let { trackId }: { trackId: string } = $props();
	const uid = $props.id();
	const headingId = `${uid}-lyrics`;

	// Lyrics are large: kept fresh for a minute, and re-asked every minute only
	// while the lookup is still pending.
	const lyrics = createQuery(() => queries.trackLyrics(trackId));
	const l = $derived(lyrics.data && lyrics.data.track_id === trackId ? lyrics.data : undefined);
</script>

<section aria-labelledby={headingId}>
	<div class="mb-2 flex items-center gap-2">
		<h3 id={headingId} class="text-sm font-semibold text-fg">Lyrics</h3>
		{#if l?.has_synced}<span class="chip">Synced</span>{/if}
	</div>

	{#if lyrics.isError && !l}
		<p class="text-sm text-muted" role="alert">
			Could not load lyrics: <span class="font-mono break-words">{errorMessage(lyrics.error)}</span>
			<button
				type="button"
				class="ml-1 font-medium text-primary hover:underline"
				onclick={() => lyrics.refetch()}>Try again</button
			>
		</p>
	{:else if !l}
		<Skeleton class="h-24" />
	{:else if l.status === 'found' && l.plain}
		<!-- A scrollable box must be focusable so keyboard users can scroll it. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			role="region"
			aria-label="Lyrics"
			tabindex="0"
			class="max-h-80 overflow-y-auto rounded-xl bg-surface-2 p-3 text-sm leading-relaxed whitespace-pre-line text-fg"
		>
			{l.plain}
		</div>
	{:else if l.status === 'found' || l.status === 'instrumental'}
		<span class="chip">Instrumental</span>
	{:else if l.status === 'not_found'}
		<p class="text-sm text-fg">No lyrics found.</p>
		<p class="mt-1 text-xs text-muted">
			Checked <RelTime value={l.fetched_at} />
			{#if l.next_attempt_at}· next try <RelTime value={l.next_attempt_at} />{/if}
		</p>
	{:else if l.status === 'error'}
		<p class="text-sm text-fg">
			Lookup failed.
			{#if l.next_attempt_at}Next try <RelTime value={l.next_attempt_at} />{/if}
		</p>
		{#if l.last_error}
			<p class="mt-1 font-mono text-xs break-words text-muted">{l.last_error}</p>
		{/if}
	{:else}
		<p class="text-sm text-muted">Not fetched yet. Lyrics are looked up in the background.</p>
	{/if}

	{#if l && (l.status === 'found' || l.status === 'instrumental')}
		<p class="mt-2 text-xs text-muted">
			Lyrics from <OutLink
				href="https://lrclib.net"
				class="font-medium text-primary hover:underline">LRCLIB</OutLink
			>
		</p>
	{/if}
</section>
