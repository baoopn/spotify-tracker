<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { MediaQuery } from 'svelte/reactivity';
	import OutLink from '../OutLink.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { fastClock } from '#lib/clock.svelte.js';
	import { activeIndex, parseLrc } from '#lib/lrc.js';
	import type { Snapshot } from '#lib/now-playing.svelte.js';
	import { queries } from '#lib/queries.js';

	let {
		trackId,
		snap,
		playing,
		durationMs
	}: { trackId: string; snap: Snapshot; playing: boolean; durationMs: number } = $props();

	const uid = $props.id();
	const headingId = `${uid}-np-lyrics`;

	const lyrics = createQuery(() => queries.trackLyrics(trackId, true));
	const l = $derived(lyrics.data && lyrics.data.track_id === trackId ? lyrics.data : undefined);
	const lines = $derived(l?.status === 'found' ? parseLrc(l.synced) : []);

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

	// Keep the active line centred inside the box only (never scroll the page).
	$effect(() => {
		const i = active;
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
	<div class="mb-2 flex items-center gap-2">
		<h3 id={headingId} class="eyebrow text-ink">Lyrics</h3>
		{#if lines.length}<span class="chip">Synced</span>{/if}
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
			class="relative max-h-80 overflow-y-auto py-2 text-base leading-relaxed"
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
				</p>
			{/each}
		</div>
	{:else if l.status === 'found' && l.plain}
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			role="region"
			aria-label="Lyrics"
			tabindex="0"
			class="max-h-80 overflow-y-auto text-sm leading-relaxed whitespace-pre-line text-ink"
		>
			{l.plain}
		</div>
	{:else if l.status === 'found' || l.status === 'instrumental'}
		<span class="chip">Instrumental</span>
	{:else if l.status === 'not_found'}
		<p class="text-sm text-ink-soft">No lyrics found for this song.</p>
	{:else if l.status === 'error'}
		<p class="text-sm text-ink-soft">Lyrics lookup failed. It will be tried again.</p>
	{:else}
		<p class="text-sm text-ink-soft">Not fetched yet. Lyrics are looked up in the background.</p>
	{/if}

	{#if l && (l.status === 'found' || l.status === 'instrumental')}
		<p class="mt-2 text-xs text-ink-soft">
			Lyrics from <OutLink
				href="https://lrclib.net"
				class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				>LRCLIB</OutLink
			>
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
