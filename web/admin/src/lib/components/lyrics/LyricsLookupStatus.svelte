<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { RefreshCw } from '@lucide/svelte';
	import ConfirmDialog from '../ConfirmDialog.svelte';
	import RelTime from '../RelTime.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { TrackLyrics } from '#lib/types.js';

	let {
		lyrics,
		details = false,
		class: className = 'mt-2'
	}: {
		lyrics: TrackLyrics;
		/** Also show when the lyrics were last checked, and the last lookup's error. */
		details?: boolean;
		/** Classes for the wrapper (spacing). */
		class?: string;
	} = $props();

	const queryClient = useQueryClient();
	const requeue = createMutation(() => ({
		mutationFn: (replaceManual: boolean) => api.requeueLyrics(lyrics.track_id, { replaceManual }),
		onSuccess: (_d, replaceManual) => {
			void queryClient.invalidateQueries({ queryKey: ['tracks', 'lyrics', lyrics.track_id] });
			void queryClient.invalidateQueries({ queryKey: qk.lyrics.queue });
			toasts.success(
				'Lyrics lookup queued',
				replaceManual
					? 'LRCLIB lyrics will replace yours when found.'
					: 'It goes to the front of the queue.'
			);
		},
		onError: (err) => toasts.error('Could not queue the lookup', errorMessage(err))
	}));

	// Lyrics added by hand are kept unless a look-up is confirmed to replace them.
	const manual = $derived(lyrics.source === 'manual');
	let confirmOpen = $state(false);
	async function replace() {
		try {
			await requeue.mutateAsync(true);
		} catch {
			// onError already shows a toast; the dialog closes.
		}
	}

	const settled = $derived(!lyrics.looking_up && !lyrics.queued);
	// Stored lyrics whose fresh lookup failed: they stay, and it is retried.
	const relookupFailed = $derived(
		(lyrics.status === 'found' || lyrics.status === 'instrumental') && !!lyrics.next_attempt_at
	);
</script>

<div class={['flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-soft', className]}>
	<!-- One live region that stays mounted, so state changes are announced;
	     out of the flow while empty (still in the accessibility tree), so it adds no gap. -->
	<span
		role="status"
		aria-live="polite"
		class="inline-flex flex-wrap items-center gap-x-2 empty:absolute"
	>
		{#if lyrics.looking_up}
			<span class="chip">Looking up…</span>
		{:else if lyrics.queued}
			<span class="chip">Queued</span>{#if lyrics.replace_manual}<span
					>LRCLIB lyrics will replace yours when found</span
				>{/if}
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
			class="btn btn-ghost btn-sm"
			disabled={requeue.isPending}
			onclick={() => (manual ? (confirmOpen = true) : requeue.mutate(false))}
			aria-haspopup={manual ? 'dialog' : undefined}
			title={manual
				? 'Look the lyrics up on LRCLIB; what it finds replaces your lyrics.'
				: 'Fetch the lyrics from LRCLIB again, ahead of the queue. Changed lyrics are translated again.'}
		>
			<RefreshCw class="size-3.5" aria-hidden="true" />
			{requeue.isPending ? 'Queuing…' : 'Look up lyrics again'}
		</button>
	{/if}
</div>

{#if manual}
	<ConfirmDialog
		bind:open={confirmOpen}
		title="Replace your lyrics?"
		message="LRCLIB is asked for this song again. If it finds lyrics, they replace the lyrics you added. If it finds nothing, yours stay."
		confirmLabel="Look up and replace"
		danger
		onconfirm={replace}
	/>
{/if}
