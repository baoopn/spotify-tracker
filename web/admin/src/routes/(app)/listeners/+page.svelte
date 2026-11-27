<script lang="ts">
	import { Info, RefreshCw, TriangleAlert } from '@lucide/svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { errorMessage } from '#lib/api.js';
	import { listenersQuery } from '#lib/listeners.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import InviteCard from './InviteCard.svelte';
	import ListenersTable from './ListenersTable.svelte';
	import ListeningNow from './ListeningNow.svelte';

	const query = createQuery(() => listenersQuery());
	const manual = new ManualRefetch(() => query.refetch());
	const s = $derived(query.data);
</script>

<PageHeader
	title="Listeners"
	eyebrow="Listen"
	description="People you invited to listen along with you at /listen."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-ghost"
			onclick={() => manual.run()}
			disabled={manual.active}
		>
			<RefreshCw class={['size-4', manual.active && 'animate-spin']} aria-hidden="true" /> Refresh
		</button>
	{/snippet}
</PageHeader>

{#if query.isError && !s}
	<div class="card">
		<ErrorState message={errorMessage(query.error)} onretry={() => manual.run()} />
	</div>
{:else if !s}
	<div class="card flex flex-col gap-3 p-6">
		{#each { length: 6 }, i (i)}<Skeleton class="h-6" />{/each}
	</div>
{:else}
	<div class="mb-4"><ListeningNow /></div>

	{#if !s.listen_enabled || !s.audio_configured}
		<div class="mb-4 flex flex-col gap-2">
			{#if !s.listen_enabled}
				<p class="flex gap-2 rounded-xl bg-spot-butter p-3 text-sm text-on-spot" role="status">
					<TriangleAlert class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
					<span>
						Listen along is off. Set <code class="font-mono">LISTEN_ENABLED=true</code> to open the listen
						page. You can still invite people now.
					</span>
				</p>
			{/if}
			{#if !s.audio_configured}
				<p class="flex gap-2 rounded-xl bg-spot-sky p-3 text-sm text-on-spot" role="status">
					<Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
					<span>
						Audio storage isn't set up. Listeners see the song and lyrics, but hear nothing until
						the
						<code class="font-mono">S3_*</code> settings are in place.
					</span>
				</p>
			{/if}
		</div>
	{/if}

	<div class="grid items-start gap-4 2xl:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
		<InviteCard listenUrl={s.listen_url} />
		<ListenersTable listeners={s.data} />
	</div>
{/if}
