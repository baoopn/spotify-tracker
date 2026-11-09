<script lang="ts">
	import { DatabaseBackup, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { ApiError, api, errorMessage } from '#lib/api.js';
	import { backupAvailable } from '#lib/backups.js';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import StatusCard from './StatusCard.svelte';
	import SettingsForm from './SettingsForm.svelte';
	import RunsTable from './RunsTable.svelte';
	import InventoryList from './InventoryList.svelte';

	const queryClient = useQueryClient();
	// Polls every 5 s while a request is queued or a run is in progress, else 60 s.
	const query = createQuery(() => queries.backups());
	const manual = new ManualRefetch(() => query.refetch());
	const s = $derived(query.data);

	const available = $derived(backupAvailable(s));
	const running = $derived(s?.runs.some((r) => r.status === 'running') ?? false);
	const pending = $derived(s?.pending_request ?? null);

	function requestError(title: string, err: unknown) {
		if (err instanceof ApiError && err.code === 'already_pending')
			toasts.info('Already queued', 'A backup request is waiting to be picked up.');
		else if (err instanceof ApiError && err.code === 'backup_unavailable')
			toasts.error(title, 'The backup service is offline or not configured.');
		else toasts.error(title, errorMessage(err));
	}

	// The backup service claims requests within a minute; the status then polls fast.
	const run = createMutation(() => ({
		mutationFn: () => api.runBackup(),
		onSuccess: () => toasts.success('Backup queued', 'It starts within a minute.'),
		onError: (err) => requestError('Could not start a backup', err),
		onSettled: () => queryClient.invalidateQueries({ queryKey: qk.backups })
	}));
	const refresh = createMutation(() => ({
		mutationFn: () => api.refreshBackups(),
		onSuccess: () => toasts.success('Refresh queued', 'The list updates within a minute.'),
		onError: (err) => requestError('Could not refresh the list', err),
		onSettled: () => queryClient.invalidateQueries({ queryKey: qk.backups })
	}));

	const runLabel = $derived(
		running ? 'Running…' : pending?.kind === 'backup' || run.isPending ? 'Queued…' : 'Back up now'
	);
	const runBusy = $derived(running || pending?.kind === 'backup' || run.isPending);
	const refreshBusy = $derived(pending?.kind === 'refresh' || refresh.isPending);
</script>

<PageHeader
	title="Backups"
	eyebrow="Settings"
	description="Encrypted off-site database backups, as reported by the backup service."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary"
			onclick={() => refresh.mutate()}
			disabled={!s || !available || !!pending || refresh.isPending}
		>
			<RefreshCw class={['size-4', refreshBusy && 'animate-spin']} aria-hidden="true" />
			{refreshBusy ? 'Refreshing…' : 'Refresh list'}
		</button>
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => run.mutate()}
			disabled={!s || !available || !!pending || runBusy}
		>
			{#if runBusy}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<DatabaseBackup class="size-4" aria-hidden="true" />{/if}
			{runLabel}
		</button>
	{/snippet}
</PageHeader>

{#if query.isError && !s}
	<div class="card">
		<ErrorState message={errorMessage(query.error)} onretry={() => manual.run()} />
	</div>
{:else if !s}
	<div class="grid gap-4 lg:grid-cols-3">
		<div class="card flex flex-col gap-3 p-6 lg:col-span-2">
			{#each { length: 6 }, i (i)}<Skeleton class="h-6" />{/each}
		</div>
		<div class="card flex flex-col gap-4 p-6">
			{#each { length: 4 }, i (i)}<Skeleton class="h-10" />{/each}
		</div>
	</div>
{:else}
	<div class="grid items-start gap-4 lg:grid-cols-3">
		<div class="lg:col-span-2"><StatusCard status={s} /></div>
		<SettingsForm settings={s.settings} />
	</div>
	<div class="mt-4 flex flex-col gap-4">
		<RunsTable runs={s.runs} />
		<InventoryList inventory={s.inventory} />
	</div>
{/if}
