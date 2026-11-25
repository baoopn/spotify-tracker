<script lang="ts">
	import { tick } from 'svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import OutLink from '#lib/components/OutLink.svelte';
	import LyricsEditor from '#lib/components/lyrics/LyricsEditor.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import LyricsLangPicker from '#lib/components/lyrics/LyricsLangPicker.svelte';
	import LyricsLookupStatus from '#lib/components/lyrics/LyricsLookupStatus.svelte';
	import TranslationStatus from '#lib/components/lyrics/TranslationStatus.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { PenLine, Trash2 } from '@lucide/svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { lyricsLang } from '#lib/lyricsLang.svelte.js';
	import { parseLrc } from '#lib/lrc.js';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { scriptLang } from '#lib/scriptLang.js';
	import { availableLangs, interleave, pickLang, translatedLines } from '#lib/translation.js';

	let {
		trackId,
		durationMs = 0
	}: { trackId: string; /** Track length (ms), for the LRC checks. */ durationMs?: number } =
		$props();
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

	const manual = $derived(l?.source === 'manual');
	const hasLyrics = $derived(l?.status === 'found' || l?.status === 'instrumental');
	// The editor belongs to one track: another track closes it.
	let editingFor = $state<string | null>(null);
	const editing = $derived(editingFor === trackId);
	let confirmRemove = $state(false);
	let editButton: HTMLButtonElement | undefined = $state();

	async function closeEditor() {
		editingFor = null;
		// Back to the button that opened the editor, once it is rendered again.
		await tick();
		editButton?.focus();
	}

	const queryClient = useQueryClient();
	const remove = createMutation(() => ({
		mutationFn: () => api.clearManualLyrics(trackId),
		onSuccess: (data) => {
			queryClient.setQueryData(qk.tracks.lyrics(trackId, true), data);
			void queryClient.invalidateQueries({ queryKey: ['tracks', 'lyrics', trackId] });
			void queryClient.invalidateQueries({ queryKey: qk.lyrics.queue });
			// The API says whether a lookup will run (none while the fetcher is off).
			const note = data.message ?? '';
			if (data.lookup_enabled) toasts.success('Your lyrics were removed', note);
			else toasts.info('Your lyrics were removed', note);
		},
		onError: (err) => toasts.error('Could not remove the lyrics', errorMessage(err))
	}));

	async function removeLyrics() {
		try {
			await remove.mutateAsync();
		} catch {
			// onError shows a toast.
		}
	}
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
	{:else if editing}
		<LyricsEditor lyrics={l} {durationMs} onclose={closeEditor} />
	{:else if l.status === 'found' && l.plain}
		<!-- A scrollable box must be focusable so keyboard users can scroll it. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			role="region"
			aria-label="Lyrics"
			tabindex="0"
			class="max-h-80 overflow-y-auto rounded-xl bg-surface-2 p-3 font-lyrics text-sm leading-relaxed text-fg"
		>
			{#if trLines}
				{#each lines as line, i (i)}
					<p class="min-h-[1lh]" dir="auto" lang={scriptLang(line.text)}>
						{line.text}
						{#if trLines[i]}<span class="block text-muted" {lang}>{trLines[i]}</span>{/if}
					</p>
				{/each}
			{:else if trPairs}
				{#each trPairs as p, i (i)}
					<p class="min-h-[1lh]" dir="auto" lang={scriptLang(p.text)}>
						{p.text}
						{#if p.tr}<span class="block text-muted" {lang}>{p.tr}</span>{/if}
					</p>
				{/each}
			{:else}
				<div class="whitespace-pre-line" lang={scriptLang(l.plain)}>{l.plain}</div>
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

	{#if l && !editing}
		<LyricsLookupStatus lyrics={l} details />
		<div class="mt-2 flex flex-wrap items-center gap-2">
			<button
				bind:this={editButton}
				type="button"
				class="btn btn-secondary btn-sm"
				onclick={() => (editingFor = trackId)}
			>
				<PenLine class="size-4" aria-hidden="true" />
				{hasLyrics ? 'Edit lyrics' : 'Add your own lyrics'}
			</button>
			{#if manual}
				<button
					type="button"
					class="btn btn-ghost btn-sm"
					aria-haspopup="dialog"
					disabled={remove.isPending}
					onclick={() => (confirmRemove = true)}
				>
					<Trash2 class="size-4" aria-hidden="true" />
					Remove my lyrics
				</button>
			{/if}
		</div>
	{/if}

	{#if l?.status === 'found'}
		<TranslationStatus {trackId} translation={tr} details />
	{/if}

	{#if l && (l.status === 'found' || l.status === 'instrumental')}
		<p class="mt-2 text-xs text-muted">
			{#if manual}<span class="font-medium text-fg">Added by you</span>{#if l.manual_at}, <RelTime
						value={l.manual_at}
					/>{/if}{:else}Lyrics from <OutLink
					href="https://lrclib.net"
					class="font-medium text-primary hover:underline">LRCLIB</OutLink
				>{/if}{#if trText}{CREDIT_SEP}Translated by Claude{/if}
		</p>
	{/if}
</section>

<ConfirmDialog
	bind:open={confirmRemove}
	title="Remove your lyrics?"
	message="The lyrics you added for this song are deleted, and LRCLIB is asked for its lyrics again. This cannot be undone."
	confirmLabel="Remove my lyrics"
	danger
	onconfirm={removeLyrics}
/>
