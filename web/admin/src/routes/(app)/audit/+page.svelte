<script lang="ts">
	import { LoaderCircle, ScrollText } from '@lucide/svelte';
	import { createInfiniteQuery } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Badge, { type Tone } from '#lib/components/Badge.svelte';
	import { errorMessage } from '#lib/api.js';
	import { queries } from '#lib/queries.js';
	import type { AuditEntry } from '#lib/types.js';

	const LIMIT = 50;

	// Polling re-reads every loaded page from the newest one, so new entries appear on top.
	const audit = createInfiniteQuery(() => queries.audit(LIMIT));
	const rows = $derived(audit.data?.pages.flatMap((p) => p.data) ?? []);
	const error = $derived(audit.error ? errorMessage(audit.error) : null);

	function tone(action: string): Tone {
		if (action.endsWith('failed')) return 'danger';
		if (action.startsWith('auth.')) return 'info';
		if (action.startsWith('spotify.')) return 'ok';
		if (action.startsWith('settings.')) return 'accent';
		return 'neutral';
	}

	function detailText(d: AuditEntry['detail']): string {
		if (d == null || d === '') return '–';
		return typeof d === 'string' ? d : JSON.stringify(d);
	}
</script>

<PageHeader
	title="Audit log"
	eyebrow="Settings"
	description="Sign-ins, re-authorisations and settings changes, newest first."
/>

<div class="overflow-hidden">
	{#if error && rows.length === 0}
		<ErrorState message={error} onretry={() => audit.refetch()} />
	{:else if !audit.data}
		<div class="flex flex-col gap-3 p-4" aria-busy="true">
			{#each { length: 8 }, i (i)}
				<Skeleton class="h-7" />
			{/each}
		</div>
	{:else if rows.length === 0}
		<EmptyState icon={ScrollText} title="No audit entries yet" />
	{:else}
		<div class="overflow-x-auto">
			<table class="data-table">
				<caption class="sr-only">Audit log</caption>
				<thead>
					<tr>
						<th scope="col" class="w-48">When</th>
						<th scope="col" class="w-52">Action</th>
						<th scope="col">Actor</th>
						<th scope="col">Detail</th>
						<th scope="col" class="w-36">IP</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as row (row.id)}
						<tr>
							<td><RelTime value={row.at} showAbsolute /></td>
							<td
								><Badge tone={tone(row.action)}><span class="font-mono">{row.action}</span></Badge
								></td
							>
							<td class="max-w-56 truncate" title={row.actor}>{row.actor || '–'}</td>
							<td class="max-w-md text-muted">
								<span class="line-clamp-2 break-words" title={detailText(row.detail)}
									>{detailText(row.detail)}</span
								>
							</td>
							<td class="font-mono text-xs text-muted">{row.ip ?? '–'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="flex items-center justify-center gap-3 border-t border-border p-3">
			{#if error}<p class="text-sm text-danger" role="alert">{error}</p>{/if}
			{#if audit.hasNextPage}
				<button
					type="button"
					class="btn btn-secondary"
					onclick={() => audit.fetchNextPage()}
					disabled={audit.isFetchingNextPage}
				>
					{#if audit.isFetchingNextPage}<LoaderCircle
							class="size-4 animate-spin"
							aria-hidden="true"
						/>{/if}
					Load more
				</button>
			{:else}
				<p class="text-sm text-muted">No older entries.</p>
			{/if}
		</div>
	{/if}
</div>
