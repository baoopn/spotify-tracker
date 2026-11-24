<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import RelTime from '../RelTime.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { translationNote } from '#lib/translation.js';
	import type { TrackTranslation } from '#lib/types.js';

	let {
		trackId,
		translation,
		details = false
	}: {
		trackId: string;
		translation: TrackTranslation | null;
		/** Also show when and by which model it was translated, and the last error. */
		details?: boolean;
	} = $props();

	const queryClient = useQueryClient();
	const requeue = createMutation(() => ({
		mutationFn: () => api.requeueTranslation(trackId),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['tracks', 'lyrics', trackId] });
			void queryClient.invalidateQueries({ queryKey: qk.lyrics.translationQueue });
			toasts.success('Translation queued', 'It goes to the front of the queue.');
		},
		onError: (err) => toasts.error('Could not queue the translation', errorMessage(err))
	}));

	const t = $derived(translation);
	const waiting = $derived(!!t && (t.status === 'pending' || t.stale));
	const note = $derived(translationNote(t));
</script>

{#if t}
	<div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-soft">
		{#if waiting}
			<span class="chip" role="status">Translating…</span>
		{:else if note}
			<span>{note}</span>
		{:else if t.status === 'done' && details}
			<span>
				Translated <RelTime value={t.translated_at} />{#if t.model}
					by {t.model}{/if}
			</span>
		{/if}
		{#if details && t.last_error && t.status !== 'done'}
			<span class="font-mono break-words">{t.last_error}</span>
		{/if}
		<button
			type="button"
			class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink disabled:opacity-50"
			disabled={requeue.isPending}
			onclick={() => requeue.mutate()}
			title="Translate again, ahead of the queue (uses Claude credit)"
		>
			{requeue.isPending ? 'Queuing…' : 'Re-queue'}
		</button>
	</div>
{/if}
