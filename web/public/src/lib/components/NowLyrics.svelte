<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { tick } from 'svelte';
	import LyricsList from './LyricsList.svelte';
	import { lyricClock } from '#lib/clock.svelte.js';
	import { artistNames } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { currentIndex, lineKeys } from '#lib/lyrics.js';
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

	// The full lyrics, fetched once per track. Only for the track on screen:
	// a late response for the previous song shows nothing.
	const query = createQuery(() => nowLyricsQuery(trackId, playing));
	const data = $derived(query.data && query.data.track_id === trackId ? query.data : null);

	const lines = $derived(data?.synced ? data.lines : []);
	const keys = $derived(lineKeys(lines));
	const plain = $derived(!lines.length && data?.plain ? data.plain : null);
	const instrumental = $derived(!lines.length && !plain && !!data?.instrumental);

	// The stream frames (every few seconds) reset the base; the 250 ms clock
	// moves the highlight in between. Only needed for synced lines.
	const posMs = $derived(
		playing && lines.length
			? estimatePositionMs(np, live.serverNow(lyricClock.now), durationMs)
			: null
	);
	const active = $derived(lines.length && posMs != null ? currentIndex(lines, posMs) : -1);

	/** Something to read: synced lines or plain text (instrumental has no expand). */
	const readable = $derived(playing && (lines.length > 0 || plain != null));
	const shown = $derived(readable || (playing && instrumental));

	// Expanded view: a native modal dialog (focus trap, Esc, focus return).
	let dialog = $state<HTMLDialogElement>();
	let open = $state(false);

	// Render the content first so the close button gets focus and
	// aria-labelledby resolves when the dialog opens.
	async function expand() {
		if (!dialog || dialog.open) return;
		open = true;
		await tick();
		if (dialog && !dialog.open && readable) dialog.showModal();
		else open = false;
	}
	function collapse() {
		// close() fires onclose, which clears `open`; clear it directly if the
		// dialog never opened (e.g. lyrics vanished during the tick).
		if (dialog?.open) dialog.close();
		else open = false;
	}
	// Clicks on the panel land on its children; only the backdrop hits the
	// dialog itself. Both ends must be on it, so a text selection dragged
	// out of the panel does not close it.
	let downOnBackdrop = false;
	function onBackdropDown(e: PointerEvent) {
		downOnBackdrop = e.target === dialog;
	}
	function onBackdrop(e: MouseEvent) {
		if (downOnBackdrop && e.target === dialog) collapse();
		downOnBackdrop = false;
	}

	// Playback stopped or the lyrics went away: close cleanly.
	$effect(() => {
		if (!readable && open) collapse();
	});
</script>

{#if shown && data}
	<section aria-labelledby={headingId} class="mt-6 border-t border-rule pt-4">
		<div class="mb-2 flex items-center gap-2">
			<h3 id={headingId} class="label text-ink">Lyrics</h3>
			{#if lines.length}
				<span class="chip label">Synced</span>
			{:else if plain}
				<span class="chip label">Plain</span>
			{:else if instrumental}
				<span class="chip label">Instrumental</span>
			{/if}
			{#if readable}
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
			{/if}
		</div>

		<!-- One scroller at a time; the empty box holds the card's height. -->
		{#if instrumental}
			<!-- No words to show; the chip says it all. -->
		{:else if open}
			<div class="h-64" aria-hidden="true"></div>
		{:else if lines.length}
			<LyricsList {lines} {keys} {active} class="h-64" />
		{:else if plain}
			<!-- A new box per song, so the next song starts at the top. -->
			{#key trackId}
				{@render plainText(plain, 'h-64 py-2 text-base leading-relaxed')}
			{/key}
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

{#snippet plainText(text: string, cls: string)}
	<!-- A scrollable box must be focusable so keyboard users can scroll it. -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div
		role="region"
		aria-label="Lyrics, scrollable"
		tabindex="0"
		dir="auto"
		class={['overflow-y-auto break-words whitespace-pre-line text-ink', cls]}
	>
		{text}
	</div>
{/snippet}

<!-- Outside the {#if} so it can close itself when the card goes away. Focus
     returns to the Expand button; if the card is gone, the browser drops it
     back to the page. Content renders only while open. -->
<dialog
	bind:this={dialog}
	id={dialogId}
	aria-labelledby={dialogTitleId}
	class="lyrics-dialog"
	onclose={() => (open = false)}
	onpointerdown={onBackdropDown}
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
			{#if lines.length}
				<LyricsList {lines} {keys} {active} size="lg" class="min-h-0 flex-1 px-5 sm:px-8" />
			{:else if plain}
				{#key trackId}
					{@render plainText(
						plain,
						'min-h-0 flex-1 overscroll-contain px-5 py-6 text-xl leading-relaxed sm:px-8 sm:text-2xl'
					)}
				{/key}
			{/if}
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
			height: 80dvh;
			border: 1px solid var(--ink);
			border-radius: 12px;
			box-shadow: 4px 4px 0 var(--print-color, var(--ink));
		}
	}
	.lyrics-dialog::backdrop {
		/* A literal scrim: --ink turns light in dark mode. */
		background: rgb(10 8 20 / 0.55);
	}
	/* A modal dialog does not stop the page behind it from scrolling. */
	:global(html:has(dialog.lyrics-dialog[open])) {
		overflow: hidden;
	}
</style>
