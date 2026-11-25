<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { LoaderCircle, Upload } from '@lucide/svelte';
	import { ApiError, api, errorMessage } from '#lib/api.js';
	import {
		formatLrcTime,
		lrcSourceLines,
		markerOnly,
		normalizeLyrics,
		plainFromLrc,
		validateLrc,
		type LrcLineError
	} from '#lib/lrc.js';
	import { qk } from '#lib/queries.js';
	import { scriptLang } from '#lib/scriptLang.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { ManualLyricsInput, TrackLyrics } from '#lib/types.js';

	let {
		lyrics,
		durationMs = 0,
		onclose
	}: {
		lyrics: TrackLyrics;
		/** Track length; synced lines may not start past it (plus a few seconds). */
		durationMs?: number;
		onclose: (saved: boolean) => void;
	} = $props();

	type Tab = 'synced' | 'plain';
	const uid = $props.id();
	const MAX_FILE = 200_000;
	const SYNCED_PLACEHOLDER = ['[00:12.30] First line', '[00:15.80] Second line'].join('\n');
	const TABS: { v: Tab; label: string }[] = [
		{ v: 'synced', label: 'Synced (LRC)' },
		{ v: 'plain', label: 'Plain' }
	];

	// Prefilled once from the current lyrics, LRCLIB's plain text included: saving
	// them unedited sends the same texts, so the lyrics hash (and a translation)
	// stays as it is. Plain text that belongs to the synced text (LRCLIB's, or
	// derived from it) is re-derived when only the synced text is edited.
	const initial = (() => {
		const synced = lyrics.status === 'found' ? (lyrics.synced ?? '') : '';
		const plain = lyrics.status === 'found' ? (lyrics.plain ?? '') : '';
		return {
			synced,
			plain,
			plainFollowsSynced:
				!!synced && (lyrics.source !== 'manual' || plain === plainFromLrc(synced)),
			instrumental: lyrics.status === 'instrumental',
			tab: (plain && !synced ? 'plain' : 'synced') as Tab
		};
	})();

	let tab = $state<Tab>(initial.tab);
	let synced = $state(initial.synced);
	let plain = $state(initial.plain);
	let instrumental = $state(initial.instrumental);
	let serverLines = $state<LrcLineError[]>([]);
	let formError = $state('');
	let tabButtons: HTMLButtonElement[] = $state([]);
	let syncedArea: HTMLTextAreaElement | undefined = $state();
	let plainArea: HTMLTextAreaElement | undefined = $state();
	let fileInput: HTMLInputElement | undefined = $state();
	let summary: HTMLElement | undefined = $state();

	const normSynced = $derived(normalizeLyrics(synced));
	const normPlain = $derived(normalizeLyrics(plain));
	const rows = $derived(normSynced ? lrcSourceLines(normSynced) : []);
	const liveErrors = $derived(
		normSynced && !instrumental ? validateLrc(normSynced, durationMs) : []
	);
	// Server errors replace the live ones until the text changes.
	const errors = $derived(serverLines.length ? serverLines : liveErrors);
	const errorsByLine = $derived(
		errors.reduce((m, e) => {
			if (e.line > 0) m.set(e.line, [...(m.get(e.line) ?? []), e]);
			return m;
		}, new Map<number, LrcLineError[]>())
	);
	// Announced (alert) only on a save attempt or a server answer, not while typing.
	let announce = $state('');
	const derivedPlain = $derived(normSynced && !liveErrors.length ? plainFromLrc(normSynced) : '');
	const empty = $derived(!normSynced && !normPlain);

	const queryClient = useQueryClient();
	const save = createMutation(() => ({
		mutationFn: (body: ManualLyricsInput) => api.setLyrics(lyrics.track_id, body),
		onSuccess: (data) => {
			queryClient.setQueryData(qk.tracks.lyrics(lyrics.track_id, true), data);
			void queryClient.invalidateQueries({ queryKey: ['tracks', 'lyrics', lyrics.track_id] });
			void queryClient.invalidateQueries({ queryKey: qk.lyrics.queue });
			toasts.success('Lyrics saved', 'Your lyrics are used from now on.');
			onclose(true);
		},
		onError: (err) => {
			if (err instanceof ApiError && err.lines.length) {
				serverLines = err.lines;
				tab = 'synced';
				announce = problems(err.lines.length);
				void focusSummary();
				return;
			}
			formError = errorMessage(err);
		}
	}));

	async function focusSummary() {
		await tick();
		summary?.focus();
	}

	function problems(n: number): string {
		return `${n === 1 ? '1 problem' : `${n} problems`} in the synced lyrics`;
	}

	function select(t: Tab, focusTab = false) {
		tab = t;
		if (focusTab) tabButtons[t === 'synced' ? 0 : 1]?.focus();
	}

	function onTabKey(e: KeyboardEvent) {
		if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) {
			e.preventDefault();
			const next: Tab =
				e.key === 'Home'
					? 'synced'
					: e.key === 'End'
						? 'plain'
						: tab === 'synced'
							? 'plain'
							: 'synced';
			select(next, true);
		}
	}

	function submit(e: SubmitEvent) {
		e.preventDefault();
		formError = '';
		announce = '';
		serverLines = [];
		if (instrumental) {
			save.mutate({ instrumental: true });
			return;
		}
		if (empty) {
			formError = 'Add synced or plain lyrics, or mark the song instrumental.';
			return;
		}
		if (liveErrors.length) {
			tab = 'synced';
			announce = `${problems(liveErrors.length)}. Fix them to save.`;
			void focusSummary();
			return;
		}
		const words = normPlain || derivedPlain;
		if (markerOnly(words)) {
			formError = 'The lyrics hold no words: mark the song instrumental instead.';
			return;
		}
		// The synced text changed and the plain text was not touched: plain text
		// that belonged to the old synced text is derived again from the new one.
		const rederive =
			initial.plainFollowsSynced &&
			plain === initial.plain &&
			normSynced !== normalizeLyrics(initial.synced);
		save.mutate({
			...(normSynced ? { synced: normSynced } : {}),
			...(normPlain && !rederive ? { plain: normPlain } : {})
		});
	}

	async function importFile(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		if (file.size > MAX_FILE) {
			toasts.error('File too large', `An .lrc file is at most ${MAX_FILE / 1000} kB.`);
			return;
		}
		try {
			synced = normalizeLyrics(await file.text());
			serverLines = [];
			tab = 'synced';
			syncedArea?.focus();
		} catch (err) {
			toasts.error('Could not read the file', errorMessage(err));
		}
	}

	function editSynced(e: Event) {
		synced = (e.currentTarget as HTMLTextAreaElement).value;
		serverLines = [];
	}

	/** Moves the caret of the synced text to a line (from the error list). */
	function goToLine(line: number) {
		const area = syncedArea;
		if (!area) return;
		const start =
			area.value
				.split('\n')
				.slice(0, line - 1)
				.join('\n').length + (line > 1 ? 1 : 0);
		area.focus();
		area.setSelectionRange(start, start);
	}

	// Focus the editor's first field once, when it opens. untrack: the attachment
	// must not re-run (and steal focus) when the tab changes.
	function autofocus(node: HTMLElement) {
		untrack(() => {
			(tab === 'synced' ? syncedArea : plainArea)?.focus({ preventScroll: true });
			node.scrollIntoView({ block: 'nearest' });
		});
	}

	const errorsId = `${uid}-errors`;
	const syncedHint = `${uid}-synced-hint`;
	const plainHint = `${uid}-plain-hint`;
</script>

<form
	class="mt-1 rounded-xl border border-rule-strong bg-surface-2 p-3"
	aria-label="Edit lyrics"
	onsubmit={submit}
	{@attach autofocus}
>
	<div class="mb-3 flex flex-wrap items-center gap-2">
		<div
			role="tablist"
			aria-label="Lyrics format"
			class="inline-flex items-center gap-0.5 rounded-lg border border-rule-strong bg-paper-2 p-0.5"
		>
			{#each TABS as t, i (t.v)}
				{@const active = tab === t.v}
				<button
					bind:this={tabButtons[i]}
					type="button"
					role="tab"
					id="{uid}-tab-{t.v}"
					aria-selected={active}
					aria-controls="{uid}-panel-{t.v}"
					tabindex={active ? 0 : -1}
					class={[
						'inline-flex min-h-9 items-center rounded-md px-3 py-1.5 font-mono text-xs font-medium tracking-[0.04em] uppercase transition-colors duration-150',
						active ? 'bg-spot-select text-on-spot' : 'text-ink-soft hover:bg-paper-3 hover:text-ink'
					]}
					onclick={() => select(t.v)}
					onkeydown={onTabKey}
				>
					{t.label}
					{#if t.v === 'synced' && errors.length && !instrumental}
						<span class="ml-1.5 text-danger" aria-hidden="true">●</span><span class="sr-only"
							>, has problems</span
						>
					{/if}
				</button>
			{/each}
		</div>
		<label class="ml-auto inline-flex min-h-9 cursor-pointer items-center gap-2 text-sm text-fg">
			<input type="checkbox" class="size-4 accent-[var(--ink)]" bind:checked={instrumental} />
			Instrumental
		</label>
	</div>

	<div
		role="tabpanel"
		id="{uid}-panel-synced"
		aria-labelledby="{uid}-tab-synced"
		hidden={tab !== 'synced'}
	>
		<div class="mb-1 flex flex-wrap items-end justify-between gap-2">
			<label class="label mb-0" for="{uid}-synced">Synced lyrics (LRC)</label>
			<button
				type="button"
				class="btn btn-ghost btn-sm"
				disabled={instrumental}
				onclick={() => fileInput?.click()}
			>
				<Upload class="size-4" aria-hidden="true" />
				Import .lrc file
			</button>
			<input
				bind:this={fileInput}
				type="file"
				accept=".lrc,.txt,text/plain"
				class="sr-only"
				tabindex="-1"
				aria-hidden="true"
				onchange={importFile}
			/>
		</div>
		<textarea
			bind:this={syncedArea}
			id="{uid}-synced"
			class="input min-h-48 font-mono text-sm leading-relaxed"
			spellcheck="false"
			autocomplete="off"
			placeholder={SYNCED_PLACEHOLDER}
			disabled={instrumental}
			aria-invalid={errors.length > 0 && !instrumental}
			aria-describedby="{syncedHint}{errors.length ? ` ${errorsId}` : ''}"
			value={synced}
			oninput={editSynced}></textarea>
		<p id={syncedHint} class="hint">
			One line per lyric, each starting with [mm:ss.xx]. Lines may not start after the song ends{durationMs
				? ` (${formatLrcTime(durationMs)})`
				: ''}.
		</p>

		{#if !instrumental && errors.length}
			<div
				bind:this={summary}
				id={errorsId}
				tabindex="-1"
				class="mt-2 rounded-lg border border-danger px-3 py-2 text-sm text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
			>
				<p class="font-semibold text-danger">
					{problems(errors.length)}
				</p>
				<ul class="mt-1 space-y-0.5">
					{#each errors as e, i (i)}
						<li>
							{#if e.line > 0}
								<button
									type="button"
									class="font-mono font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
									onclick={() => goToLine(e.line)}>Line {e.line}</button
								>: {e.message}
							{:else}
								{e.message}
							{/if}
						</li>
					{/each}
				</ul>
			</div>
		{/if}

		{#if !instrumental && rows.length}
			<h4 class="mt-3 mb-1 text-xs font-semibold text-muted" id="{uid}-preview">Preview</h4>
			<ol
				aria-labelledby="{uid}-preview"
				class="max-h-72 overflow-y-auto rounded-lg bg-paper-2 py-1 font-lyrics text-sm"
			>
				{#each rows as r (r.line)}
					{@const lineErrors = errorsByLine.get(r.line)}
					{#if r.kind !== 'blank'}
						<li
							class={[
								'grid grid-cols-[2rem_4rem_minmax(0,1fr)] gap-x-2 px-2 py-0.5',
								lineErrors && 'bg-danger/10'
							]}
						>
							<span class="text-right font-mono text-xs text-muted tabular-nums">{r.line}</span>
							<span class="font-mono text-xs text-ink-soft tabular-nums">
								{#if r.starts.length}{r.starts
										.map(formatLrcTime)
										.join(' ')}{:else if r.kind === 'meta'}meta{:else}–{/if}
							</span>
							<span
								class={['min-w-0 break-words', r.kind === 'meta' ? 'text-muted' : 'text-fg']}
								dir="auto"
								lang={scriptLang(r.text)}>{r.text || '♪'}</span
							>
							{#each lineErrors ?? [] as e, i (i)}
								<span class="col-start-3 text-xs font-medium text-danger">{e.message}</span>
							{/each}
						</li>
					{/if}
				{/each}
			</ol>
		{/if}
	</div>

	<div
		role="tabpanel"
		id="{uid}-panel-plain"
		aria-labelledby="{uid}-tab-plain"
		hidden={tab !== 'plain'}
	>
		<label class="label" for="{uid}-plain">Plain lyrics</label>
		<textarea
			bind:this={plainArea}
			id="{uid}-plain"
			class="input min-h-48 text-sm leading-relaxed"
			disabled={instrumental}
			aria-describedby={plainHint}
			placeholder={derivedPlain || 'First line\nSecond line'}
			bind:value={plain}></textarea>
		<p id={plainHint} class="hint">
			{#if normSynced && initial.plainFollowsSynced && plain === initial.plain}
				This text comes from the synced lyrics. If you edit only those, it is made from them again.
			{:else if normSynced}
				Leave it empty to use the synced lyrics without timestamps.
			{:else}
				Without synced lyrics, the page shows this text.
			{/if}
		</p>
	</div>

	{#if instrumental}
		<p class="mt-2 text-sm text-muted">The song is saved as instrumental, with no lyrics.</p>
	{/if}

	<p class="sr-only" role="alert">{announce}</p>

	{#if formError}
		<p class="field-error" role="alert">{formError}</p>
	{/if}

	<div class="mt-3 flex flex-wrap justify-end gap-2">
		<button
			type="button"
			class="btn btn-secondary"
			disabled={save.isPending}
			onclick={() => onclose(false)}
		>
			Cancel
		</button>
		<button type="submit" class="btn btn-primary" disabled={save.isPending}>
			{#if save.isPending}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{/if}
			Save lyrics
		</button>
	</div>
</form>
