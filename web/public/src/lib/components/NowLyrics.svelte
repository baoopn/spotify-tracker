<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import LyricsLangToggle from './LyricsLangToggle.svelte';
	import LyricsList from './LyricsList.svelte';
	import { lyricClock } from '#lib/clock.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import {
		availableLangs,
		creditHref,
		currentIndex,
		interleave,
		lineKeys,
		lyricsState,
		lyricsView,
		pickLang
	} from '#lib/lyrics.js';
	import { lyricsLang } from '#lib/lyricsLang.svelte.js';
	import { estimatePositionMs } from '#lib/playback.js';
	import { nowLyricsQuery } from '#lib/queries.js';
	import type { NowPlaying } from '#lib/types.js';

	let {
		np,
		focus = false
	}: {
		np: NowPlaying;
		/** Lyrics focus layout: a tall box with large type. */
		focus?: boolean;
	} = $props();
	// A JS string: a leading space in markup would be dropped.
	const CREDIT_SEP = ' · ';

	const uid = $props.id();
	const headingId = `${uid}-now-lyrics`;

	const trackId = $derived(np.track?.id ?? null);
	const playing = $derived(!!trackId && np.is_playing);
	const durationMs = $derived(np.track?.duration_ms ?? 0);

	// The full lyrics, fetched once per track. Only for the track on screen:
	// a late response for the previous song shows nothing.
	const query = createQuery(() => nowLyricsQuery(trackId, playing));
	// Same helper as the hero's layout toggle, so the two never disagree.
	const state = $derived(playing ? lyricsState(query.data, query, trackId) : 'none');
	const data = $derived(query.data && query.data.track_id === trackId ? query.data : null);

	const found = $derived(data?.status === 'found' ? data : null);
	const view = $derived(found ? lyricsView(found) : { lines: [], plain: null });
	const lines = $derived(view.lines);
	const keys = $derived(lineKeys(lines));
	const plain = $derived(view.plain);
	const instrumental = $derived(state === 'instrumental');
	/** The lookup is still to come: the server says so, ask again soon. */
	const looking = $derived(state === 'looking');
	/** No answer for this song yet (first fetch, or the previous song's data). */
	const loading = $derived(state === 'loading');
	const failed = $derived(state === 'failed');

	// Translation: one shared preference; a song without it shows the original.
	const tr = $derived(found?.translation ?? null);
	const available = $derived(availableLangs(tr));
	const lang = $derived(pickLang(lyricsLang.value, available));
	const trText = $derived(tr && lang !== 'orig' ? tr[lang] : null);
	const trLines = $derived(trText && lines.length ? trText.lines : null);
	const trPlain = $derived(trText && plain ? trText.plain : null);
	const translating = $derived(tr?.status === 'pending');

	// The stream frames (every few seconds) reset the base; the 250 ms clock
	// moves the highlight in between. Only needed for synced lines.
	const posMs = $derived(
		playing && lines.length
			? estimatePositionMs(np, live.serverNow(lyricClock.now), durationMs)
			: null
	);
	const active = $derived(lines.length && posMs != null ? currentIndex(lines, posMs) : -1);

	/** Something to read: synced lines or plain text. */
	const readable = $derived(state === 'readable');
	const shown = $derived(state !== 'none');

	/** Every state holds the same box, so nothing jumps when lyrics arrive. */
	// svh, not dvh: the box keeps its height when the phone toolbar hides.
	// 55svh on phones leaves room to scroll the page around it.
	const box = $derived(focus ? 'h-[55svh] lg:h-[70svh]' : 'h-64');
</script>

{#if shown}
	<section
		aria-labelledby={headingId}
		aria-busy={loading || looking}
		class={focus ? '' : 'mt-6 border-t border-rule pt-4'}
	>
		<div class="mb-2 flex min-h-9 flex-wrap items-center gap-2">
			<h3 id={headingId} class="label text-ink">Lyrics</h3>
			{#if lines.length}
				<span class="chip label">Synced</span>
			{:else if plain}
				<span class="chip label">Plain</span>
			{:else if instrumental}
				<span class="chip label">Instrumental</span>
			{/if}
			{#if translating}
				<span class="chip label" title="The translation is on its way">Translating…</span>
			{/if}
			{#if readable}
				<div class="ml-auto">
					<LyricsLangToggle {available} bind:value={lyricsLang.value} compact={!focus} />
				</div>
			{/if}
		</div>

		<!-- Every waiting state holds the same box as the lyrics, so nothing
		     jumps when they arrive. -->
		{#if loading}
			<div class={['skeleton rounded-lg', box]} aria-hidden="true"></div>
			<p class="sr-only">Loading lyrics</p>
		{:else if failed}
			<p class={['flex items-center justify-center gap-2 text-sm text-ink-soft', box]} role="alert">
				Couldn&rsquo;t load lyrics.
				<button type="button" class="link font-semibold text-ink" onclick={() => query.refetch()}
					>Retry</button
				>
			</p>
		{:else if looking}
			<p
				class={[
					'flex items-center justify-center text-ink-soft',
					focus ? 'text-base' : 'text-sm',
					box
				]}
				role="status"
			>
				Looking for lyrics…
			</p>
		{:else if instrumental}
			<!-- No words to show; the chip says it all. -->
		{:else if lines.length}
			<LyricsList
				{lines}
				{keys}
				{active}
				size={focus ? 'lg' : 'sm'}
				translation={trLines}
				translationLang={lang}
				class={box}
			/>
		{:else if plain}
			<!-- A new box per song, so the next song starts at the top. -->
			{#key trackId}
				{@render plainText(
					plain,
					trPlain,
					focus
						? [box, 'overscroll-contain py-4 text-xl leading-relaxed sm:text-2xl']
						: [box, 'py-2 text-base leading-relaxed']
				)}
			{/key}
		{/if}

		{#if found}
			{@const href = creditHref(found.credit)}
			<p class="mt-3 text-xs text-ink-soft">
				{#if href}<a
						{href}
						target="_blank"
						rel="noopener noreferrer"
						class="link font-semibold text-ink">{found.credit.text}</a
					>{:else}<span class="font-semibold text-ink">{found.credit.text}</span
					>{/if}{#if trText && tr}{CREDIT_SEP}{tr.credit.text}{/if}
			</p>
		{/if}
	</section>
{/if}

{#snippet plainText(text: string, translated: string | null, cls: (string | false)[])}
	{@const pairs = translated ? interleave(text, translated) : null}
	<!-- A scrollable box must be focusable so keyboard users can scroll it. -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div
		role="region"
		aria-label="Lyrics, scrollable"
		tabindex="0"
		dir="auto"
		class={['overflow-y-auto break-words text-ink', ...cls]}
	>
		{#if pairs}
			{#each pairs as p, i (i)}
				<p class="min-h-[1lh]">
					{p.text}
					{#if p.tr}<span class="block text-[0.85em] opacity-75" {lang} dir="auto">{p.tr}</span
						>{/if}
				</p>
			{/each}
		{:else}
			<div class="whitespace-pre-line">{text}</div>
			{#if translated}
				<h4 class="label mt-4 mb-1 text-ink-soft" {lang}>
					{lang === 'vi' ? 'Bản dịch tiếng Việt' : 'English translation'}
				</h4>
				<div class="whitespace-pre-line opacity-80" {lang}>{translated}</div>
			{/if}
		{/if}
	</div>
{/snippet}
