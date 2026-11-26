<script lang="ts">
	import { RefreshCw } from '@lucide/svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import SpotifyTokenCard from '#lib/components/status/SpotifyTokenCard.svelte';
	import ServiceList from '#lib/components/status/ServiceList.svelte';
	import BackupsCard from '#lib/components/status/BackupsCard.svelte';
	import LyricsQueueCard from '#lib/components/status/LyricsQueueCard.svelte';
	import TranslationQueueCard from '#lib/components/status/TranslationQueueCard.svelte';
	import EnrichmentProgress from '#lib/components/status/EnrichmentProgress.svelte';
	import { errorMessage } from '#lib/api.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import { qk, queries } from '#lib/queries.js';

	// The overview query carries the token, services and enrichment; the
	// queue and backup cards run their own queries (shared with the Overview
	// health strip and the settings pages).
	const overview = createQuery(() => queries.overview());
	const queryClient = useQueryClient();
	const manual = new ManualRefetch(() =>
		Promise.all([
			overview.refetch(),
			...[qk.backups, qk.lyrics.queue, qk.lyrics.translationQueue].map((queryKey) =>
				queryClient.refetchQueries({ queryKey, exact: true })
			)
		])
	);
	const d = $derived(overview.data);
</script>

<PageHeader
	title="Status"
	eyebrow="System"
	description="The Spotify token, services, backups, lyrics and enrichment."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={() => manual.run()}
			disabled={manual.active}
		>
			<RefreshCw class={['size-4', manual.active && 'animate-spin']} aria-hidden="true" />
			Refresh
		</button>
	{/snippet}
</PageHeader>

{#snippet loading()}
	<div class="flex flex-col gap-3 border-t border-ink pt-4" aria-hidden="true">
		<Skeleton class="h-4 w-24" /><Skeleton class="h-9 w-32" /><Skeleton class="h-2 w-full" />
	</div>
{/snippet}

{#if overview.isError && !d}
	<div class="card-print mb-10">
		<ErrorState message={errorMessage(overview.error)} onretry={() => manual.run()} />
	</div>
{/if}

<div class="grid items-start gap-x-10 gap-y-10 lg:grid-cols-2">
	<div id="spotify-token" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		{#if d}
			<SpotifyTokenCard status={d.spotify} />
		{:else if !overview.isError}{@render loading()}{/if}
	</div>
	<div id="services" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		{#if d}
			<div class="border-t border-ink pt-4"><ServiceList services={d.services} /></div>
		{:else if !overview.isError}{@render loading()}{/if}
	</div>
	<div id="backups" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		<BackupsCard />
	</div>
	<div id="lyrics-queue" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		<LyricsQueueCard />
	</div>
	<div id="translations" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		<TranslationQueueCard />
	</div>
	<div id="enrichment" class="scroll-mt-[calc(5rem+env(safe-area-inset-top,0px))]">
		{#if d}
			<div class="border-t border-ink pt-4"><EnrichmentProgress counts={d.enrichment} /></div>
		{:else if !overview.isError}{@render loading()}{/if}
	</div>
</div>
