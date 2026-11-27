<script lang="ts">
	import { CircleAlert, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import KpiRow, { type Kpi } from '#lib/components/KpiRow.svelte';
	import { ApiError, errorMessage } from '#lib/api.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { bytes, dateTime, num, relative } from '#lib/format.js';
	import { audioKeys, listenAudioApi, type AudioSummary } from '#lib/listenAudio.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';

	let { summary }: { summary: AudioSummary | null } = $props();

	const queryClient = useQueryClient();

	function every(s: number): string {
		if (s <= 0) return 'Off';
		if (s % 3600 === 0) return `Every ${s / 3600} h`;
		if (s % 60 === 0) return `Every ${s / 60} min`;
		return `Every ${s} s`;
	}

	const items = $derived<Kpi[]>(
		summary
			? [
					{
						label: 'Files',
						value: num(summary.files),
						sub: `${num(summary.tracks_in_catalog)} in the catalog`
					},
					{ label: 'Total size', value: bytes(summary.total_bytes) },
					{
						label: 'Last sync',
						value: summary.last_sync_at
							? relative(summary.last_sync_at, relativeClock.now)
							: 'Never',
						sub: summary.last_sync_at ? dateTime(summary.last_sync_at) : undefined
					},
					{ label: 'Schedule', value: every(summary.sync_interval_s) }
				]
			: ['Files', 'Total size', 'Last sync', 'Schedule'].map((label) => ({ label, value: '' }))
	);

	const sync = createMutation(() => ({
		mutationFn: () => listenAudioApi.sync(),
		onSuccess: (r) =>
			toasts.success(
				'Sync done',
				`${num(r.added)} added, ${num(r.removed)} removed, ${num(r.files)} files in all.`
			),
		onError: (err) => {
			if (err instanceof ApiError && err.code === 'sync_running') {
				toasts.info('A sync is already running', 'The list updates when it is done.');
			} else if (err instanceof ApiError && err.status === 429) {
				toasts.info('Synced moments ago', 'Wait half a minute, then try again.');
			} else {
				toasts.error('Could not sync', errorMessage(err));
			}
		},
		onSettled: () =>
			Promise.all([
				queryClient.invalidateQueries({ queryKey: audioKeys.all }),
				queryClient.invalidateQueries({ queryKey: qk.tracks.all })
			])
	}));
</script>

<section class="mb-6" aria-label="Audio files summary">
	<div class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0 flex-1 basis-[36rem]">
			<KpiRow label="Audio file totals" {items} loading={!summary} columns={4} />
		</div>
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => sync.mutate()}
			disabled={sync.isPending || !summary}
		>
			{#if sync.isPending}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<RefreshCw class="size-4" aria-hidden="true" />{/if}
			Sync now
		</button>
	</div>
	{#if summary?.last_error}
		<p class="mt-3 flex gap-2 rounded-xl bg-spot-rose p-3 text-sm text-on-spot" role="alert">
			<CircleAlert class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span class="min-w-0 break-words">
				The last sync failed: <span class="font-mono text-xs">{summary.last_error}</span>
			</span>
		</p>
	{/if}
</section>
