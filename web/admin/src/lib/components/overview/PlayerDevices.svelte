<script lang="ts">
	import { Check, RefreshCw } from '@lucide/svelte';
	import Skeleton from '../Skeleton.svelte';
	import { deviceIcon, deviceTypeLabel } from '#lib/player.js';
	import type { PlayerDevice } from '#lib/types.js';

	// Spotify Connect devices; picking one moves playback there.
	let {
		devices,
		activeId,
		disabled,
		refreshing,
		onpick,
		onrefresh
	}: {
		/** undefined while loading; null when Spotify did not list them. */
		devices: PlayerDevice[] | null | undefined;
		activeId: string | null;
		disabled: boolean;
		refreshing: boolean;
		onpick: (id: string) => void;
		onrefresh: () => void;
	} = $props();

	const uid = $props.id();
</script>

<div class="flex flex-col gap-2">
	<div class="flex items-center justify-between gap-2">
		<h3 id="{uid}-devices" class="eyebrow">Devices</h3>
		<button
			type="button"
			class="btn btn-ghost btn-sm btn-icon"
			aria-label="Refresh devices"
			title="Refresh devices"
			onclick={onrefresh}
			disabled={refreshing}
		>
			<RefreshCw class={['size-4', refreshing && 'animate-spin']} aria-hidden="true" />
		</button>
	</div>
	{#if devices === undefined}
		<Skeleton class="h-12" />
	{:else if devices === null}
		<p class="text-sm text-ink-soft">Spotify did not list the devices. Try refreshing.</p>
	{:else if devices.length === 0}
		<p class="text-sm text-ink-soft">
			No devices found. Open Spotify on a phone, computer or speaker, then refresh.
		</p>
	{:else}
		<ul class="flex flex-col gap-1" aria-labelledby="{uid}-devices">
			{#each devices as d (d.id)}
				{@const Icon = deviceIcon(d.type)}
				{@const active = d.id === activeId}
				<li>
					<!-- The active device stays focusable (aria-disabled), so focus
					     is not lost when it becomes active. -->
					<button
						type="button"
						class={[
							'flex w-full min-w-0 items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors duration-150',
							active
								? 'cursor-default border-ink bg-paper-3'
								: 'border-transparent hover:bg-paper-3 disabled:hover:bg-transparent',
							(disabled || d.is_restricted) && !active && 'opacity-60'
						]}
						aria-current={active ? 'true' : undefined}
						aria-disabled={active ? 'true' : undefined}
						disabled={!active && (disabled || d.is_restricted)}
						onclick={() => {
							if (!active) onpick(d.id);
						}}
					>
						<Icon class="size-5 shrink-0 text-ink" aria-hidden="true" />
						<span class="min-w-0 flex-1">
							<span class="block truncate text-sm font-medium text-ink">{d.name}</span>
							<span class="block truncate text-xs text-ink-soft">
								{deviceTypeLabel(d.type)}{d.is_restricted
									? ' · cannot be controlled'
									: ''}{d.is_private_session ? ' · private session' : ''}
							</span>
						</span>
						{#if active}
							<span class="inline-flex shrink-0 items-center gap-1 font-mono text-xs text-ink">
								<Check class="size-3.5" aria-hidden="true" /> Playing here
							</span>
						{:else if !d.is_restricted}
							<span class="sr-only">Play on this device</span>
						{/if}
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</div>
