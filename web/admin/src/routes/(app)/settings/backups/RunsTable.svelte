<script lang="ts">
	import { History } from '@lucide/svelte';
	import Badge, { type Tone } from '#lib/components/Badge.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { isInterrupted } from '#lib/backups.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { bytes, timeSpan } from '#lib/format.js';
	import type { BackupRun, BackupRunStatus, BackupTrigger } from '#lib/types.js';

	let { runs }: { runs: BackupRun[] } = $props();

	const STATUS: Record<BackupRunStatus, { tone: Tone; label: string }> = {
		running: { tone: 'info', label: 'Running' },
		success: { tone: 'ok', label: 'Success' },
		failed: { tone: 'danger', label: 'Failed' }
	};
	/** Still 'running' over 2 h after it started: the container stopped mid-run. */
	const INTERRUPTED = { tone: 'warn' as Tone, label: 'Interrupted' };
	const TRIGGER: Record<BackupTrigger, string> = {
		schedule: 'Scheduled',
		manual: 'Manual',
		start: 'On start',
		retry: 'Retry'
	};
</script>

<section class="card overflow-hidden" aria-labelledby="bk-runs-h">
	<h2 id="bk-runs-h" class="px-6 pt-6 pb-2 font-semibold text-fg">Recent runs</h2>
	{#if runs.length === 0}
		<EmptyState
			icon={History}
			title="No runs yet"
			message="Runs show up here once a backup starts."
		/>
	{:else}
		<div class="overflow-x-auto">
			<table class="data-table">
				<caption class="sr-only">Latest backup runs, newest first</caption>
				<thead>
					<tr>
						<th scope="col" class="w-28">Status</th>
						<th scope="col" class="w-28">Trigger</th>
						<th scope="col" class="w-40">Started</th>
						<th scope="col" class="w-24">Duration</th>
						<th scope="col" class="w-24">Size</th>
						<th scope="col">Name</th>
					</tr>
				</thead>
				<tbody>
					{#each runs as run (run.id)}
						{@const st = isInterrupted(run, relativeClock.now)
							? INTERRUPTED
							: (STATUS[run.status] ?? STATUS.failed)}
						<tr>
							<td><Badge tone={st.tone}>{st.label}</Badge></td>
							<td class="text-sm" title={run.requested_by ?? undefined}>
								{TRIGGER[run.trigger] ?? run.trigger}
							</td>
							<td class="text-sm"><RelTime value={run.started_at} /></td>
							<td class="num text-sm">{timeSpan(run.duration_ms)}</td>
							<td class="num text-sm">{bytes(run.size_bytes)}</td>
							<td class="max-w-md text-sm">
								{#if run.name}
									<span class="block truncate font-mono text-xs" title={run.name}>{run.name}</span>
								{/if}
								{#if run.error}
									<details class="text-xs">
										<summary class="cursor-pointer font-medium text-danger">Show error</summary>
										<p class="mt-1 font-mono break-words whitespace-pre-wrap text-ink">
											{run.error}
										</p>
									</details>
								{:else if !run.name}
									<span class="text-muted">–</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
