<script lang="ts">
	import { FileAudio, FileX } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import type { AudioStatusEntry } from '#lib/listenAudio.js';

	// Whether listeners can hear this song. Nothing when listen along is off.
	// `compact` (queue rows): a small "Audio" pill when there is a file, and
	// only screen-reader text when there is none.
	let {
		status,
		enabled,
		compact = false
	}: {
		/** undefined while loading or unknown. */
		status: AudioStatusEntry | undefined;
		/** listen_enabled from the status call. */
		enabled: boolean;
		compact?: boolean;
	} = $props();
</script>

{#if enabled && status}
	{#if compact}
		{#if status.available}
			<span
				class="inline-flex shrink-0 items-center gap-1 rounded-full bg-spot-mint px-1.5 py-px font-mono text-[0.625rem] tracking-[0.06em] text-on-spot uppercase"
				title="Listen-along audio file uploaded"
			>
				<FileAudio class="size-3" aria-hidden="true" /> Audio
			</span>
		{:else}
			<span class="sr-only">No listen-along audio file</span>
		{/if}
	{:else if status.available}
		<span
			class="inline-flex max-w-full items-center gap-1.5 rounded-full bg-spot-mint px-2.5 py-0.5 text-xs font-medium text-on-spot"
			title={status.ext ? `Uploaded as .${status.ext}` : undefined}
		>
			<FileAudio class="size-3.5 shrink-0" aria-hidden="true" />
			<span class="truncate">Listen along: audio ready</span>
		</span>
	{:else}
		<a
			href="{resolve('listen-audio')}?tab=missing"
			class="inline-flex max-w-full items-center gap-1.5 rounded-full bg-spot-butter px-2.5 py-0.5 text-xs font-medium text-on-spot underline-offset-2 hover:underline"
			title="Listeners hear nothing for this song. Open the missing files."
		>
			<FileX class="size-3.5 shrink-0" aria-hidden="true" />
			<span class="truncate">No audio file</span>
		</a>
	{/if}
{/if}
