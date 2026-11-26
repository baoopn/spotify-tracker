<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { Languages } from '@lucide/svelte';
	import RelTime from '../RelTime.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { translationNote } from '#lib/translation.js';
	import type { TrackTranslation } from '#lib/types.js';

	let {
		trackId,
		translation,
		details = false,
		class: className = 'mt-2'
	}: {
		trackId: string;
		translation: TrackTranslation | null;
		/** Also show when and by which model it was translated, and the last error. */
		details?: boolean;
		/** Classes for the wrapper (spacing). */
		class?: string;
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
	const waiting = $derived(!!t && t.waiting);
	const note = $derived(translationNote(t));
</script>

{#if t}
	<div class={['flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-soft', className]}>
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
			class="btn btn-ghost btn-sm"
			disabled={requeue.isPending}
			onclick={() => requeue.mutate()}
			title="Translate again, ahead of the queue (uses Claude credit)"
		>
			<Languages class="size-3.5" aria-hidden="true" />
			{requeue.isPending ? 'Queuing…' : 'Translate again'}
		</button>
	</div>
{/if}
