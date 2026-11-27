<script lang="ts">
	import { Radio } from '@lucide/svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import Badge, { type Tone } from '#lib/components/Badge.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { countryFlag, countryName } from '#lib/country.js';
	import { titleCase } from '#lib/format.js';
	import { liveListenersQuery, type PresenceState } from '#lib/listeners.js';

	const live = createQuery(() => liveListenersQuery());
	const rows = $derived(live.data ?? []);

	const tones: Record<PresenceState, Tone> = {
		listening: 'ok',
		following: 'info',
		idle: 'neutral'
	};
	const labels: Record<PresenceState, string> = {
		listening: 'Listening',
		following: 'Following',
		idle: 'Idle'
	};
</script>

<section class="card overflow-hidden" aria-labelledby="live-h">
	<div class="flex flex-wrap items-center justify-between gap-2 px-6 pt-6 pb-4">
		<h2 id="live-h" class="flex items-center gap-2 font-semibold text-fg">
			<Radio class="size-4" aria-hidden="true" /> Listening now
		</h2>
		{#if rows.length > 0}
			<p class="text-sm text-muted">{rows.length} on the page</p>
		{/if}
	</div>

	{#if live.isPending}
		<div class="flex flex-col gap-3 px-6 pb-6" aria-busy="true">
			{#each { length: 2 }, i (i)}<Skeleton class="h-10" />{/each}
		</div>
	{:else if live.isError && rows.length === 0}
		<ErrorState message={errorMessage(live.error)} onretry={() => live.refetch()} />
	{:else if rows.length === 0}
		<EmptyState icon={Radio} title="Nobody is listening right now." />
	{:else}
		<ul class="flex flex-col border-t border-border">
			{#each rows as p (p.listener_id)}
				{@const flag = countryFlag(p.country)}
				<li
					class="grid gap-x-4 gap-y-1 border-b border-border px-6 py-3 last:border-b-0 sm:grid-cols-[minmax(0,14rem)_6.5rem_minmax(0,1fr)_auto] sm:items-center"
				>
					<span class="truncate text-sm font-medium text-fg" title={p.email}>{p.email}</span>
					<span><Badge tone={tones[p.state]}>{labels[p.state]}</Badge></span>
					<div class="min-w-0 text-sm">
						{#if p.track}
							<p class="truncate text-fg" title={p.track.name}>{p.track.name}</p>
							<p class="truncate text-xs text-muted">
								{p.track.artists.map((a) => a.name).join(', ') || 'Unknown artist'}
							</p>
						{:else}
							<p class="text-muted">No song</p>
						{/if}
						<p class="truncate text-xs text-muted">
							{#if flag}<span aria-hidden="true">{flag}</span>{/if}
							{countryName(p.country)} · {p.device ? titleCase(p.device) : 'Unknown'} ·
							{p.browser || 'Unknown'}
						</p>
					</div>
					<span class="text-xs text-muted sm:text-right">Since <RelTime value={p.since} /></span>
				</li>
			{/each}
		</ul>
	{/if}
</section>
