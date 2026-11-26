<script lang="ts">
	import { resolve } from '$app/paths';
	import { ArrowRight, Pause, Play, SkipBack, SkipForward } from '@lucide/svelte';
	import Skeleton from '../Skeleton.svelte';
	import { PlayerControl } from '#lib/player-control.svelte.js';

	// Previous / play-pause / next under the now-playing card, and a link to
	// the full Player page. Polls only the playback state (not the devices or
	// the queue); the buttons appear only when they can work.
	const ctl = new PlayerControl();
	const st = $derived(ctl.state);
	const disallows = $derived(st?.disallows ?? {});
	const device = $derived(st?.device ?? null);
</script>

<div class="flex min-h-12 flex-wrap items-center gap-x-4 gap-y-2 pt-3" aria-busy={ctl.busy}>
	{#if !ctl.view && !ctl.query.isError}
		<div class="flex items-center gap-2" aria-hidden="true">
			<Skeleton class="size-10" /><Skeleton class="size-12 rounded-full" /><Skeleton
				class="size-10"
			/>
		</div>
	{:else if st && ctl.control}
		<div class="flex min-w-0 items-center gap-3">
			<div class="flex items-center gap-1" role="group" aria-label="Playback">
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					aria-label="Previous track"
					disabled={disallows.skipping_prev}
					onclick={ctl.previous}
				>
					<SkipBack class="size-5" aria-hidden="true" />
				</button>
				<button
					type="button"
					class="btn btn-primary btn-icon size-12 rounded-full"
					aria-label={st.is_playing ? 'Pause' : 'Play'}
					disabled={st.is_playing ? disallows.pausing : disallows.resuming}
					onclick={ctl.togglePlay}
				>
					{#if st.is_playing}
						<Pause class="size-5" aria-hidden="true" />
					{:else}
						<Play class="size-5" aria-hidden="true" />
					{/if}
				</button>
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					aria-label="Next track"
					disabled={disallows.skipping_next}
					onclick={ctl.next}
				>
					<SkipForward class="size-5" aria-hidden="true" />
				</button>
			</div>
			{#if device}
				<span class="truncate font-mono text-xs text-ink-soft" title={device.name}>
					on {device.name}
				</span>
			{/if}
		</div>
	{/if}
	<a href={resolve('player')} class="btn btn-secondary btn-sm ml-auto">
		Open player <ArrowRight class="size-4" aria-hidden="true" />
	</a>
</div>
