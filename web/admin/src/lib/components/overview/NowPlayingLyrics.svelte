<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { MediaQuery } from 'svelte/reactivity';
	import OutLink from '../OutLink.svelte';
	import Skeleton from '../Skeleton.svelte';
	import LyricsLangPicker from '../lyrics/LyricsLangPicker.svelte';
	import LyricsLookupStatus from '../lyrics/LyricsLookupStatus.svelte';
	import TranslationStatus from '../lyrics/TranslationStatus.svelte';
	import { errorMessage } from '#lib/api.js';
	import { fastClock } from '#lib/clock.svelte.js';
	import { lyricsLang } from '#lib/lyricsLang.svelte.js';
	import { activeIndex, parseLrc } from '#lib/lrc.js';
	import type { Snapshot } from '#lib/now-playing.svelte.js';
	import { queries } from '#lib/queries.js';
	import { availableLangs, interleave, pickLang, translatedLines } from '#lib/translation.js';

	let {
		trackId,
		snap,
		playing,
		durationMs
	}: { trackId: string; snap: Snapshot; playing: boolean; durationMs: number } = $props();
	// A JS string: a leading space in markup would be dropped.
	const CREDIT_SEP = ' · ';

	const uid = $props.id();
	const headingId = `${uid}-np-lyrics`;

	// The song playing now: re-checked every 10 s while its lyrics or
	// translation are on their way.
	const lyrics = createQuery(() => queries.trackLyrics(trackId, true, true));
	const l = $derived(lyrics.data && lyrics.data.track_id === trackId ? lyrics.data : undefined);
	const lines = $derived(l?.status === 'found' ? parseLrc(l.synced) : []);
	const plain = $derived(!lines.length && l?.status === 'found' && l.plain ? l.plain : null);

	// Translation: one saved preference; a song without it shows the original.
	const tr = $derived(l?.translation ?? null);
	const available = $derived(availableLangs(tr));
	const lang = $derived(pickLang(lyricsLang.value, available));
	const trText = $derived(tr && lang !== 'orig' ? tr[lang] : null);
	const trLines = $derived(lines.length ? translatedLines(trText, lines.length) : null);
	const trPairs = $derived(plain && trText ? interleave(plain, trText) : null);

	// Same estimate as the progress bar, on a faster clock so lines change on time.
	const position = $derived.by(() => {
		const base = snap.np?.progress_ms ?? 0;
		const elapsed = playing ? Math.max(0, fastClock.now - snap.at) : 0;
		return Math.min(durationMs || Infinity, base + elapsed);
	});
	const active = $derived(lines.length ? activeIndex(lines, position) : -1);

	const reducedMotion = new MediaQuery('prefers-reduced-motion: reduce');
	let box = $state<HTMLDivElement>();
	let pausedUntil = 0;

	function userScrolled() {
		pausedUntil = Date.now() + 5000;
	}

	// Keep the active line centred inside the box only (never scroll the page),
	// also when translations make the lines taller.
	$effect(() => {
		const i = active;
		void trLines;
		if (!box || i < 0 || Date.now() < pausedUntil) return;
		const el = box.querySelector<HTMLElement>(`[data-i="${i}"]`);
		if (!el) return;
		box.scrollTo({
			top: el.offsetTop - box.clientHeight / 2 + el.clientHeight / 2,
			behavior: reducedMotion.current ? 'auto' : 'smooth'
		});
	});
</script>

<section aria-labelledby={headingId} class="mt-5 border-t border-rule pt-4">
	<div class="mb-2 flex flex-wrap items-center gap-2">
		<h3 id={headingId} class="eyebrow text-ink">Lyrics</h3>
		{#if lines.length}<span class="chip">Synced</span>{/if}
		<div class="ml-auto">
			<LyricsLangPicker {available} bind:value={lyricsLang.value} />
		</div>
	</div>

	{#if lyrics.isError && !l}
		<p class="text-sm text-ink-soft" role="alert">
			Could not load lyrics: <span class="font-mono break-words">{errorMessage(lyrics.error)}</span>
			<button
				type="button"
				class="ml-1 font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				onclick={() => lyrics.refetch()}>Try again</button
			>
		</p>
	{:else if !l}
		<Skeleton class="h-24" />
	{:else if lines.length}
		<!-- A scrollable box must be focusable so keyboard users can scroll it; the
		     listeners only pause auto-scroll while the user scrolls by hand. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<div
			bind:this={box}
			role="region"
			aria-label="Lyrics"
			tabindex="0"
			class="relative max-h-80 overflow-y-auto py-2 font-lyrics text-base leading-relaxed"
			onwheel={userScrolled}
			onpointerdown={userScrolled}
			ontouchmove={userScrolled}
			onkeydown={userScrolled}
		>
			{#each lines as line, i (i)}
				<p
					data-i={i}
					dir="auto"
					aria-current={i === active ? 'true' : undefined}
					class={[
						'lyric-line py-0.5',
						i === active ? 'font-semibold text-ink' : 'text-ink-soft opacity-70'
					]}
				>
					{#if line.text}{line.text}{:else}<span aria-hidden="true">♪</span>{/if}
					{#if trLines?.[i]}
						<span class="block text-[0.85em] font-normal opacity-75" {lang} dir="auto"
							>{trLines[i]}</span
						>
					{/if}
				</p>
			{/each}
		</div>
	{:else if plain}
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			role="region"
			aria-label="Lyrics"
			tabindex="0"
			class="max-h-80 overflow-y-auto font-lyrics text-sm leading-relaxed text-ink"
		>
			{#if trPairs}
				{#each trPairs as p, i (i)}
					<p class="min-h-[1lh]" dir="auto">
						{p.text}
						{#if p.tr}<span class="block opacity-75" {lang}>{p.tr}</span>{/if}
					</p>
				{/each}
			{:else}
				<div class="whitespace-pre-line">{plain}</div>
				{#if trText}
					<p class="eyebrow mt-4 mb-1 text-ink-soft" {lang}>
						{lang === 'vi' ? 'Bản dịch tiếng Việt' : 'English translation'}
					</p>
					<div class="whitespace-pre-line opacity-80" {lang}>{trText}</div>
				{/if}
			{/if}
		</div>
	{:else if l.status === 'found' || l.status === 'instrumental'}
		<span class="chip">Instrumental</span>
	{:else if l.status === 'not_found'}
		<p class="text-sm text-ink-soft">No lyrics found for this song.</p>
	{:else if l.status === 'error'}
		<p class="text-sm text-ink-soft">The lyrics lookup failed.</p>
	{:else}
		<p class="text-sm text-ink-soft">No lyrics yet.</p>
	{/if}

	{#if l}
		<LyricsLookupStatus lyrics={l} />
	{/if}

	{#if l?.status === 'found'}
		<TranslationStatus {trackId} translation={tr} />
	{/if}

	{#if l && (l.status === 'found' || l.status === 'instrumental')}
		<p class="mt-2 text-xs text-ink-soft">
			Lyrics from <OutLink
				href="https://lrclib.net"
				class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				>LRCLIB</OutLink
			>{#if trText}{CREDIT_SEP}Translated by Claude{/if}
		</p>
	{/if}
</section>

<style>
	@media (prefers-reduced-motion: no-preference) {
		.lyric-line {
			transition:
				color 200ms ease,
				opacity 200ms ease;
		}
	}
</style>
