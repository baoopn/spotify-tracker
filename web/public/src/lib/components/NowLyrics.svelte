<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { lyricClock } from '#lib/clock.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import { currentIndex, shouldRefetch, windowKey, type RefetchMemo } from '#lib/lyrics.js';
	import { estimatePositionMs } from '#lib/playback.js';
	import { nowLyricsQuery } from '#lib/queries.js';
	import type { NowPlaying } from '#lib/types.js';

	let { np }: { np: NowPlaying } = $props();

	const trackId = $derived(np.track?.id ?? null);
	const playing = $derived(!!trackId && np.is_playing);
	const durationMs = $derived(np.track?.duration_ms ?? 0);

	const query = createQuery(() => nowLyricsQuery(trackId, playing));
	// Only lines for the track on screen: a late response for the previous
	// song (or a null "no synced lyrics") shows nothing.
	const data = $derived(
		query.data && query.data.track_id === trackId && query.data.lines.length > 0 ? query.data : null
	);

	// The stream frames (every few seconds) reset the base; the 250 ms clock
	// moves the highlight in between.
	const posMs = $derived(
		playing ? estimatePositionMs(np, live.serverNow(lyricClock.now), durationMs) : null
	);

	const idx = $derived(data && posMs != null ? currentIndex(data.lines, posMs) : -1);
	const prev = $derived(data && idx > 0 ? data.lines[idx - 1].text : null);
	const current = $derived(data && idx >= 0 ? data.lines[idx].text : '');
	const next = $derived(data && idx + 1 < data.lines.length ? data.lines[idx + 1].text : null);

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
</script>

{#if playing && data}
	<section aria-label="Lyrics" class="mt-6 border-t border-rule pt-4">
		<!-- aria-live off: a line that changes every few seconds is too noisy to
		     announce; the current line can still be read on demand. -->
		<div class="lines" aria-live="off">
			<p class="row text-sm text-ink-soft" dir="auto">{prev ?? ''}</p>
			{#key idx}
				<p
					class="row current enter font-display text-xl font-semibold tracking-tight text-ink sm:text-2xl"
					dir="auto"
				>
					{#if current}{current}{:else}<span aria-hidden="true">♪</span>{/if}
				</p>
			{/key}
			<p class="row text-sm text-ink-soft" dir="auto">{next ?? ''}</p>
		</div>
		<p class="mt-3 text-xs text-ink-soft">
			<a href={data.credit.url} target="_blank" rel="noopener noreferrer" class="link"
				>{data.credit.text}</a
			>
		</p>
	</section>
{/if}

<style>
	/* Fixed row heights so a line change never shifts the layout. */
	.row {
		display: -webkit-box;
		-webkit-box-orient: vertical;
		-webkit-line-clamp: 1;
		line-clamp: 1;
		overflow: hidden;
		min-height: 1.5em;
		overflow-wrap: anywhere;
	}
	.current {
		-webkit-line-clamp: 2;
		line-clamp: 2;
		min-height: 2.5em;
		line-height: 1.25;
		margin-block: 0.25rem;
	}
	@media (prefers-reduced-motion: no-preference) {
		.enter {
			animation: line-in 220ms ease-out;
		}
	}
	@keyframes line-in {
		from {
			opacity: 0;
			transform: translateY(0.35em);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}
</style>
