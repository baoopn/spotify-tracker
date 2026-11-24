<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { MediaQuery } from 'svelte/reactivity';
	import { lyricClock } from '#lib/clock.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import {
		currentIndex,
		mergeLines,
		shouldRefetch,
		windowKey,
		type RefetchMemo
	} from '#lib/lyrics.js';
	import { estimatePositionMs } from '#lib/playback.js';
	import { nowLyricsQuery } from '#lib/queries.js';
	import type { LyricLine, NowPlaying } from '#lib/types.js';

	let { np }: { np: NowPlaying } = $props();

	const uid = $props.id();
	const headingId = `${uid}-now-lyrics`;

	const trackId = $derived(np.track?.id ?? null);
	const playing = $derived(!!trackId && np.is_playing);
	const durationMs = $derived(np.track?.duration_ms ?? 0);

	const query = createQuery(() => nowLyricsQuery(trackId, playing));
	// Only lines for the track on screen: a late response for the previous
	// song (or a null "no synced lyrics") shows nothing.
	const data = $derived(
		query.data && query.data.track_id === trackId && query.data.lines.length > 0 ? query.data : null
	);

	// The server sends a short window at a time; keep every line seen for
	// this track so the list grows as the song plays. `seen` is a plain
	// (non-reactive) cache; `lines` recomputes only when a new window arrives,
	// and a new track starts from an empty list.
	let seen: { trackId: string | null; lines: LyricLine[] } = { trackId: null, lines: [] };
	const lines = $derived.by(() => {
		if (!data) return [];
		const prev = seen.trackId === data.track_id ? seen.lines : [];
		seen = { trackId: data.track_id, lines: mergeLines(prev, data.lines) };
		return seen.lines;
	});

	// The stream frames (every few seconds) reset the base; the 250 ms clock
	// moves the highlight in between.
	const posMs = $derived(
		playing ? estimatePositionMs(np, live.serverNow(lyricClock.now), durationMs) : null
	);
	const active = $derived(lines.length && posMs != null ? currentIndex(lines, posMs) : -1);

	// Refetch when the window runs out or playback seeks back; see shouldRefetch.
	const memo: RefetchMemo = { key: null, at: 0 };
	$effect(() => {
		if (!data || posMs == null || query.isFetching) return;
		const state = {
			isError: query.isError,
			hidden: document.visibilityState === 'hidden',
			nowMs: Date.now()
		};
		if (!shouldRefetch(data, posMs, state, memo)) return;
		memo.key = windowKey(data);
		memo.at = state.nowMs;
		void query.refetch();
	});

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

{#if playing && data && lines.length}
	<section aria-labelledby={headingId} class="mt-6 border-t border-rule pt-4">
		<div class="mb-2 flex items-center gap-2">
			<h3 id={headingId} class="label text-ink">Lyrics</h3>
			<span class="chip label">Synced</span>
		</div>

		<!-- A scrollable box must be focusable so keyboard users can scroll it; the
		     listeners only pause auto-scroll while the user scrolls by hand.
		     No aria-live: a line that changes every few seconds is too noisy. -->
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

		<p class="mt-2 text-xs text-ink-soft">
			<a
				href={data.credit.url}
				target="_blank"
				rel="noopener noreferrer"
				class="link font-semibold text-ink">{data.credit.text}</a
			>
		</p>
	</section>
{/if}

<style>
	@media (prefers-reduced-motion: no-preference) {
		.lyric-line {
			transition:
				color 200ms ease,
				opacity 200ms ease;
		}
	}
</style>
