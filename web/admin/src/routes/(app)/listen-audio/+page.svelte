<script lang="ts">
	import { FileAudio, ListX, RefreshCw } from '@lucide/svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Segmented from '#lib/components/Segmented.svelte';
	import { audioFilesQuery, missingAudioQuery } from '#lib/listenAudio.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import AudioSummary from './AudioSummary.svelte';
	import MissingTab from './MissingTab.svelte';
	import UploadedTab from './UploadedTab.svelte';

	const LIMIT = 25;

	let tab = $state<'uploaded' | 'missing'>('uploaded');
	let q = $state('');
	let offset = $state(0);
	let missingOffset = $state(0);

	// The files query also carries the summary, so it stays on in both tabs.
	const files = createQuery(() => audioFilesQuery({ q, limit: LIMIT, offset }));
	const missing = createQuery(() => ({
		...missingAudioQuery({ limit: LIMIT, offset: missingOffset }),
		enabled: tab === 'missing'
	}));
	const manual = new ManualRefetch(() =>
		Promise.all([files.refetch(), tab === 'missing' ? missing.refetch() : null])
	);
</script>

<PageHeader
	title="Audio files"
	eyebrow="Listen"
	description="The songs listeners can hear at /listen, synced from the bucket."
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

<AudioSummary summary={files.data?.summary ?? null} />

<div class="mb-4">
	<Segmented
		bind:value={tab}
		label="Audio files view"
		options={[
			{ value: 'uploaded', label: 'Uploaded', icon: FileAudio },
			{ value: 'missing', label: 'Missing', icon: ListX }
		]}
	/>
</div>

{#if tab === 'uploaded'}
	<UploadedTab
		data={files.data}
		error={files.isError ? files.error : null}
		dimmed={files.isPlaceholderData || manual.active}
		{q}
		{offset}
		limit={LIMIT}
		onsearch={(v) => {
			q = v;
			offset = 0;
		}}
		onpage={(o) => (offset = o)}
		onretry={() => manual.run()}
	/>
{:else}
	<MissingTab
		data={missing.data}
		error={missing.isError ? missing.error : null}
		dimmed={missing.isPlaceholderData || manual.active}
		offset={missingOffset}
		limit={LIMIT}
		onpage={(o) => (missingOffset = o)}
		onretry={() => manual.run()}
	/>
{/if}
