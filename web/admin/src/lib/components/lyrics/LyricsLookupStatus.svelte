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
		/** Also show when the lyrics were last checked, and the last lookup's error. */
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

	const settled = $derived(!lyrics.looking_up && !lyrics.queued);
	// Stored lyrics whose fresh lookup failed: they stay, and it is retried.
	const relookupFailed = $derived(
		(lyrics.status === 'found' || lyrics.status === 'instrumental') && !!lyrics.next_attempt_at
	);
</script>

<div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-soft">
	<!-- One live region that stays mounted, so state changes are announced. -->
	<span role="status" aria-live="polite" class="inline-flex flex-wrap items-center gap-x-2">
		{#if lyrics.looking_up}
			<span class="chip">Looking up…</span>
		{:else if lyrics.queued}
			<span class="chip">Queued</span>
		{:else if lyrics.status === 'not_found'}
			<span>
				Not found{#if lyrics.next_attempt_at}, next try <RelTime
						value={lyrics.next_attempt_at}
					/>{/if}
			</span>
		{:else if lyrics.status === 'error' || relookupFailed}
			<span>
				{relookupFailed ? 'Fresh lookup failed' : 'Lookup failed'}{#if lyrics.next_attempt_at}, next
					try <RelTime value={lyrics.next_attempt_at} />{/if}
			</span>
		{:else if details && lyrics.fetched_at}
			<span>Checked <RelTime value={lyrics.fetched_at} /></span>
		{/if}
	</span>
	{#if details && settled && lyrics.last_error}
		<span class="font-mono break-words">{lyrics.last_error}</span>
	{/if}
	{#if lyrics.lookup_enabled}
		<button
			type="button"
			class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink disabled:opacity-50"
			disabled={requeue.isPending}
			onclick={() => requeue.mutate()}
			title="Fetch the lyrics from LRCLIB again, ahead of the queue. Changed lyrics are translated again."
		>
			{requeue.isPending ? 'Queuing…' : 'Look up again'}
		</button>
	{/if}
</div>
