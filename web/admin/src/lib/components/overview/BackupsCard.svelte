<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import RelTime from '../RelTime.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { healthExplanation, healthMeta } from '#lib/backups.js';
	import { bytes } from '#lib/format.js';
	import { queries } from '#lib/queries.js';

	// Shares the cache (and polling) with the Backups settings page.
	const query = createQuery(() => queries.backups());
	const s = $derived(query.data);
	const hm = $derived(healthMeta(s?.health));
	const last = $derived(s?.last_success ?? null);
</script>

<section class="flex flex-col gap-3 border-t border-ink pt-4" aria-labelledby="bk-title">
	<div class="flex items-center justify-between gap-2">
		<h2 id="bk-title" class="eyebrow">Backups</h2>
		{#if s}
			<span
				class={[
					'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
					hm.cls
				]}
			>
				<hm.icon class="size-3.5" aria-hidden="true" />
				{hm.label}
			</span>
		{/if}
	</div>

	{#if s}
		<div class="text-sm">
			<p class="text-fg">
				{#if last}
					Last backup <RelTime value={last.finished_at ?? last.started_at} /> · {bytes(
						last.size_bytes
					)}
				{:else}
					No successful backup yet
				{/if}
			</p>
			<p class="text-xs text-muted">{healthExplanation(s)}</p>
		</div>
	{:else if query.isError}
		<p class="text-sm text-muted">Backup status is unavailable.</p>
	{:else}
		<Skeleton class="h-4 w-2/3" /><Skeleton class="h-3 w-1/2" />
	{/if}

	<div><a href={resolve('settings/backups')} class="btn btn-ghost btn-sm">Details</a></div>
</section>
