<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import LyricsList from './LyricsList.svelte';
	import { lyricClock } from '#lib/clock.svelte.js';
	import { artistNames } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import {
		currentIndex,
		lineKeys,
		rollLines,
		shouldRefetch,
		windowKey,
		type RefetchMemo,
		type Rolling
	} from '#lib/lyrics.js';
	import { estimatePositionMs } from '#lib/playback.js';
	import { nowLyricsQuery } from '#lib/queries.js';
	import type { NowPlaying } from '#lib/types.js';

	let { np }: { np: NowPlaying } = $props();

	const uid = $props.id();
	const headingId = `${uid}-now-lyrics`;
	const dialogId = `${uid}-lyrics-dialog`;
	const dialogTitleId = `${uid}-lyrics-dialog-title`;

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

	// A short rolling run: a few past lines plus the current server window,
	// never the whole song. Folded in once per new window, not on every tick.
	let roll = $state<Rolling | null>(null);
	$effect.pre(() => {
		const d = data;
		if (!d) return;
		roll = untrack(() => rollLines(roll, d, posMs ?? 0));
	});
	const lines = $derived(data && roll && roll.trackId === trackId ? roll.lines : []);
	const keys = $derived(lineKeys(lines));
	const active = $derived(lines.length && posMs != null ? currentIndex(lines, posMs) : -1);
	const shown = $derived(playing && !!data && lines.length > 0);

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

	// Expanded view: a native modal dialog (focus trap, Esc, focus return).
	let dialog = $state<HTMLDialogElement>();
	let open = $state(false);

	function expand() {
		if (!dialog || dialog.open) return;
		dialog.showModal();
		open = true;
	}
	function collapse() {
		dialog?.close();
	}
	function onBackdrop(e: MouseEvent) {
		// Clicks on the panel land on its children; only the backdrop hits the dialog.
		if (e.target === dialog) collapse();
	}

	// Playback stopped or the lyrics went away: close cleanly.
	$effect(() => {
		if (!shown && open) collapse();
	});
</script>

{#if shown && data}
	<section aria-labelledby={headingId} class="mt-6 border-t border-rule pt-4">
		<div class="mb-2 flex items-center gap-2">
			<h3 id={headingId} class="label text-ink">Lyrics</h3>
			<span class="chip label">Synced</span>
			<button
				type="button"
				class="ml-auto inline-flex size-9 items-center justify-center rounded-lg border border-rule text-ink-soft transition-colors hover:border-ink hover:text-ink"
				aria-label="Expand lyrics"
				title="Expand lyrics"
				aria-haspopup="dialog"
				aria-expanded={open}
				aria-controls={dialogId}
				onclick={expand}
			>
				<svg
					viewBox="0 0 24 24"
					class="size-4"
					fill="none"
					stroke="currentColor"
					stroke-width="1.75"
					stroke-linecap="round"
					stroke-linejoin="round"
					aria-hidden="true"
				>
					<path d="M15 4h5v5M9 20H4v-5M20 4l-6 6M4 20l6-6" />
				</svg>
			</button>
		</div>

		<!-- One scroller at a time; the empty box holds the card's height. -->
		{#if open}
			<div class="h-64" aria-hidden="true"></div>
		{:else}
			<LyricsList {lines} {keys} {active} class="h-64" />
		{/if}

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

<!-- Outside the {#if} so it can close itself (and hand focus back) before
     the card goes away. Content renders only while open. -->
<dialog
	bind:this={dialog}
	id={dialogId}
	aria-labelledby={dialogTitleId}
	class="lyrics-dialog"
	onclose={() => (open = false)}
	onclick={onBackdrop}
>
	{#if open && data && np.track}
		<div class="flex h-full flex-col bg-paper text-ink">
			<header class="flex items-start gap-3 border-b border-rule px-5 py-4">
				<div class="min-w-0 flex-1">
					<h2 id={dialogTitleId} class="truncate text-sm font-semibold" dir="auto">
						{np.track.name}
					</h2>
					<p class="truncate text-sm text-ink-soft" dir="auto">{artistNames(np.track.artists)}</p>
				</div>
				<button
					type="button"
					class="btn btn-secondary btn-icon"
					aria-label="Close lyrics"
					title="Close lyrics"
					onclick={collapse}
				>
					<svg
						viewBox="0 0 24 24"
						class="size-5"
						fill="none"
						stroke="currentColor"
						stroke-width="1.75"
						stroke-linecap="round"
						stroke-linejoin="round"
						aria-hidden="true"
					>
						<path d="M6 6l12 12M18 6L6 18" />
					</svg>
				</button>
			</header>
			<LyricsList {lines} {keys} {active} size="lg" class="min-h-0 flex-1 px-5 sm:px-8" />
			<p class="border-t border-rule px-5 py-3 text-xs text-ink-soft">
				<a
					href={data.credit.url}
					target="_blank"
					rel="noopener noreferrer"
					class="link font-semibold text-ink">{data.credit.text}</a
				>
			</p>
		</div>
	{/if}
</dialog>

<style>
	/* Full screen on phones; a large centred panel from sm up. */
	.lyrics-dialog {
		margin: 0;
		padding: 0;
		width: 100%;
		max-width: none;
		height: 100dvh;
		max-height: none;
		border: 0;
		background: var(--paper);
		color: var(--ink);
		overscroll-behavior: contain;
	}
	@media (min-width: 640px) {
		.lyrics-dialog {
			margin: auto;
			width: calc(100% - 3rem);
			max-width: 42rem;
			height: 80vh;
			border: 1px solid var(--ink);
			border-radius: 12px;
			box-shadow: 4px 4px 0 var(--print, var(--ink));
		}
	}
	.lyrics-dialog::backdrop {
		background: color-mix(in oklab, var(--ink) 45%, transparent);
	}
	/* A modal dialog does not stop the page behind it from scrolling. */
	:global(html:has(dialog.lyrics-dialog[open])) {
		overflow: hidden;
	}
</style>
