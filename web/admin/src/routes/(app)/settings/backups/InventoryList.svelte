<script lang="ts">
	import { Archive, Info } from '@lucide/svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import OutLink from '#lib/components/OutLink.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { BACKUP_DOCS_URL } from '#lib/backups.js';
	import { bytes, num } from '#lib/format.js';
	import type { BackupObject } from '#lib/types.js';

	let { inventory }: { inventory: BackupObject[] } = $props();

	const total = $derived(inventory.reduce((sum, o) => sum + o.size_bytes, 0));
</script>

<section class="card overflow-hidden" aria-labelledby="bk-inv-h">
	<div class="flex flex-wrap items-baseline justify-between gap-2 px-6 pt-6 pb-2">
		<h2 id="bk-inv-h" class="font-semibold text-fg">Stored backups</h2>
		<p class="text-sm text-muted">
			{num(inventory.length)}
			{inventory.length === 1 ? 'backup' : 'backups'} · {bytes(total)}
		</p>
	</div>

	{#if inventory.length === 0}
		<EmptyState
			icon={Archive}
			title="No stored backups listed"
			message="The list is refreshed after each backup, or with “Refresh list”."
		/>
	{:else}
		<div class="overflow-x-auto">
			<table class="data-table">
				<caption class="sr-only">Backups in the destination, newest first</caption>
				<thead>
					<tr>
						<th scope="col">Name</th>
						<th scope="col" class="w-40">Created</th>
						<th scope="col" class="w-24">Size</th>
					</tr>
				</thead>
				<tbody>
					{#each inventory as obj (obj.name)}
						<tr>
							<td class="max-w-md truncate font-mono text-xs" title={obj.name}>{obj.name}</td>
							<td class="text-sm"><RelTime value={obj.created_at} /></td>
							<td class="num text-sm">{bytes(obj.size_bytes)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}

	<p class="m-6 mt-4 flex gap-2 rounded-xl bg-spot-sky p-3 text-sm text-on-spot">
		<Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
		<span>
			Restore needs the private key, which is never stored here. See
			<OutLink href={BACKUP_DOCS_URL} class="font-medium underline">docs/backup.md</OutLink>.
		</span>
	</p>
</section>
