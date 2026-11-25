<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import RelTime from '../RelTime.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { TrackLyrics } from '#lib/types.js';

	let {
		lyrics,
		details = false
	}: {
		lyrics: TrackLyrics;
		/** Also show when the lyrics were last checked, and the last error. */
		details?: boolean;
	} = $props();

	const queryClient = useQueryClient();
	const requeue = createMutation(() => ({
		mutationFn: () => api.requeueLyrics(lyrics.track_id),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ['tracks', 'lyrics', lyrics.track_id] });
			void queryClient.invalidateQueries({ queryKey: qk.lyrics.queue });
			toasts.success('Lyrics lookup queued', 'It goes to the front of the queue.');
		},
		onError: (err) => toasts.error('Could not queue the lookup', errorMessage(err))
	}));

	const l = $derived(lyrics);
	const settled = $derived(!l.looking_up && !l.queued);
</script>

<div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-soft">
	{#if l.looking_up}
		<span class="chip" role="status">Looking up…</span>
	{:else if l.queued}
		<span class="chip" role="status">Queued</span>
	{:else if l.status === 'not_found'}
		<span>
			Not found{#if l.next_attempt_at}, next try <RelTime value={l.next_attempt_at} />{/if}
		</span>
	{:else if l.status === 'error'}
		<span>
			Lookup failed{#if l.next_attempt_at}, next try <RelTime value={l.next_attempt_at} />{/if}
		</span>
	{:else if details && l.fetched_at}
		<span>Checked <RelTime value={l.fetched_at} /></span>
	{/if}
	{#if details && settled && l.last_error}
		<span class="font-mono break-words">{l.last_error}</span>
	{/if}
	<button
		type="button"
		class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink disabled:opacity-50"
		disabled={requeue.isPending}
		onclick={() => requeue.mutate()}
		title="Fetch the lyrics from LRCLIB again, ahead of the queue. Changed lyrics are translated again."
	>
		{requeue.isPending ? 'Queuing…' : 'Look up again'}
	</button>
</div>
