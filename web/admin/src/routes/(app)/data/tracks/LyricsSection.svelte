<script lang="ts">
	import OutLink from '#lib/components/OutLink.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import LyricsLangPicker from '#lib/components/lyrics/LyricsLangPicker.svelte';
	import LyricsLookupStatus from '#lib/components/lyrics/LyricsLookupStatus.svelte';
	import TranslationStatus from '#lib/components/lyrics/TranslationStatus.svelte';
	import { errorMessage } from '#lib/api.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { lyricsLang } from '#lib/lyricsLang.svelte.js';
	import { parseLrc } from '#lib/lrc.js';
	import { queries } from '#lib/queries.js';
	import { availableLangs, interleave, pickLang, translatedLines } from '#lib/translation.js';

	let { trackId }: { trackId: string } = $props();
	// A JS string: a leading space in markup would be dropped.
	const CREDIT_SEP = ' · ';
	const uid = $props.id();
	const headingId = `${uid}-lyrics`;

	// Lyrics are large: kept fresh for a minute, and re-asked every minute only
	// while the lookup or the translation is on its way. The synced text lines
	// the translation up with each LRC line.
	const lyrics = createQuery(() => queries.trackLyrics(trackId, true));
	const l = $derived(lyrics.data && lyrics.data.track_id === trackId ? lyrics.data : undefined);

	const tr = $derived(l?.status === 'found' ? l.translation : null);
	const available = $derived(availableLangs(tr));
	const lang = $derived(pickLang(lyricsLang.value, available));
	const trText = $derived(tr && lang !== 'orig' ? tr[lang] : null);
	// Synced lines are shown only with a translation that lines up with them.
	const lines = $derived(trText && tr?.kind === 'synced' && l ? parseLrc(l.synced) : []);
	const trLines = $derived(translatedLines(trText, lines.length));
	const trPairs = $derived(!trLines && l?.plain && trText ? interleave(l.plain, trText) : null);
</script>

<section aria-labelledby={headingId}>
	<div class="mb-2 flex flex-wrap items-center gap-2">
		<h3 id={headingId} class="text-sm font-semibold text-fg">Lyrics</h3>
		{#if l?.has_synced}<span class="chip">Synced</span>{/if}
		<div class="ml-auto">
			<LyricsLangPicker {available} bind:value={lyricsLang.value} />
		</div>
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
			class="max-h-80 overflow-y-auto rounded-xl bg-surface-2 p-3 text-sm leading-relaxed text-fg"
		>
			{#if trLines}
				{#each lines as line, i (i)}
					<p class="min-h-[1lh]" dir="auto">
						{line.text}
						{#if trLines[i]}<span class="block text-muted" {lang}>{trLines[i]}</span>{/if}
					</p>
				{/each}
			{:else if trPairs}
				{#each trPairs as p, i (i)}
					<p class="min-h-[1lh]" dir="auto">
						{p.text}
						{#if p.tr}<span class="block text-muted" {lang}>{p.tr}</span>{/if}
					</p>
				{/each}
			{:else}
				<div class="whitespace-pre-line">{l.plain}</div>
				{#if trText}
					<p class="mt-4 mb-1 text-xs font-semibold text-muted" {lang}>
						{lang === 'vi' ? 'Bản dịch tiếng Việt' : 'English translation'}
					</p>
					<div class="whitespace-pre-line text-muted" {lang}>{trText}</div>
				{/if}
			{/if}
		</div>
	{:else if l.status === 'found' || l.status === 'instrumental'}
		<span class="chip">Instrumental</span>
	{:else if l.status === 'not_found'}
		<p class="text-sm text-fg">No lyrics found.</p>
	{:else if l.status === 'error'}
		<p class="text-sm text-fg">The lyrics lookup failed.</p>
	{:else}
		<p class="text-sm text-muted">Not fetched yet. Lyrics are looked up in the background.</p>
	{/if}

	{#if l}
		<LyricsLookupStatus lyrics={l} details />
	{/if}

	{#if l?.status === 'found'}
		<TranslationStatus {trackId} translation={tr} details />
	{/if}

	{#if l && (l.status === 'found' || l.status === 'instrumental')}
		<p class="mt-2 text-xs text-muted">
			Lyrics from <OutLink
				href="https://lrclib.net"
				class="font-medium text-primary hover:underline">LRCLIB</OutLink
			>{#if trText}{CREDIT_SEP}Translated by Claude{/if}
		</p>
	{/if}
</section>
