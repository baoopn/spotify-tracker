<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import Skeleton from '../Skeleton.svelte';
	import NowPlayingLyrics from '../overview/NowPlayingLyrics.svelte';
	import PlayerDevices from './PlayerDevices.svelte';
	import PlayerQueue from './PlayerQueue.svelte';
	import PlayerTransport from './PlayerTransport.svelte';
	import AudioStatusBadge from '../AudioStatusBadge.svelte';
	import { statusIds } from '#lib/audioStatus.js';
	import { audioStatusQuery } from '#lib/listenAudio.js';
	import { api, errorMessage } from '#lib/api.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import { PlayerControl } from '#lib/player-control.svelte.js';
	import type { Snapshot } from '#lib/now-playing.svelte.js';
	import { qk, queries } from '#lib/queries.js';
	import type { PlayerTrack } from '#lib/types.js';

	// The owner's Spotify player: transport, devices, queue and search. The
	// state is polled every 5 s, the devices and queue every 25 s; both hold
	// while a command runs (see player-control.svelte.ts).
	const queryClient = useQueryClient();
	const ctl = new PlayerControl();
	const extras = createQuery(() => ({ ...queries.playerExtras(), enabled: !ctl.holding }));
	const v = $derived(ctl.view);
	const st = $derived(ctl.state);
	const item = $derived(st?.item ?? null);
	const canControl = $derived(ctl.canControl);
	// Listen-along audio for the song and the queue: one batched call, refetched
	// when either changes (the key holds the ids) and every 60 s.
	const audioIds = $derived(statusIds([item?.id, ...(extras.data?.queue ?? []).map((t) => t.id)]));
	const audio = createQuery(() => audioStatusQuery(audioIds));
	// The refresh button spins for its own refetch only, not for the polls.
	const manual = new ManualRefetch(() => Promise.all([ctl.query.refetch(), extras.refetch()]));

	// A new track can change the queue and the devices: refresh them.
	let lastItemId: string | null | undefined;
	$effect(() => {
		const id = item?.id ?? null;
		if (lastItemId !== undefined && id !== lastItemId) {
			queryClient.invalidateQueries({ queryKey: qk.player.extras, exact: true });
		}
		lastItemId = id;
	});

	const device = $derived(st?.device ?? null);

	// The lyrics follow the player's own poll: its progress at the instant the
	// state was fetched (or patched by a command).
	const lyricsSnap = $derived<Snapshot>({
		np: st
			? {
					is_playing: st.is_playing,
					progress_ms: st.progress_ms,
					fetched_at: new Date(ctl.query.dataUpdatedAt).toISOString(),
					track: null
				}
			: null,
		at: ctl.query.dataUpdatedAt
	});

	/** Search result: play just this track. */
	function playTrack(t: PlayerTrack) {
		ctl.run({
			label: `play “${t.name}”`,
			run: () => api.playerPlay({ uris: [t.uri] }),
			patch: (s) => ({ ...s, is_playing: true, progress_ms: 0, item: t })
		});
	}

	/** Queue row: play from it and keep the rest of the queue after it. */
	function playFromQueue(i: number) {
		const rest = (extras.data?.queue ?? [])
			.slice(i)
			.filter((t) => t.uri.startsWith('spotify:track:'));
		const t = rest[0];
		if (!t) return;
		ctl.run({
			label: `play “${t.name}”`,
			run: () => api.playerPlay({ uris: rest.slice(0, 100).map((x) => x.uri) }),
			patch: (s) => ({ ...s, is_playing: true, progress_ms: 0, item: t })
		});
	}

	function queueTrack(t: PlayerTrack) {
		ctl.run({
			label: `queue “${t.name}”`,
			run: () => api.playerQueue(t.uri),
			done: `Queued “${t.name}”`
		});
	}

	function transfer(id: string) {
		// Keep the playing state; with nothing active, start playing there.
		const play = st ? st.is_playing : true;
		ctl.run({ label: 'switch the device', run: () => api.playerTransfer(id, play) });
	}

	const uid = $props.id();
	const settingsHref = resolve('settings/spotify');
</script>

{#if !v}
	{#if ctl.query.isError}
		<div class="card-print p-5">
			<p class="text-sm text-ink-soft">
				The player is unavailable: {errorMessage(ctl.query.error)}
			</p>
		</div>
	{:else}
		<div class="grid items-start gap-8 lg:grid-cols-2" aria-busy="true">
			<div class="card-print flex flex-col gap-4 p-5">
				<div class="flex items-center gap-4">
					<Skeleton class="size-22" />
					<div class="flex flex-1 flex-col gap-2">
						<Skeleton class="h-5 w-1/2" /><Skeleton class="h-3 w-1/3" />
					</div>
				</div>
				<Skeleton class="h-2 w-full" />
				<div class="flex justify-center gap-3">
					<Skeleton class="size-10" /><Skeleton class="size-12" /><Skeleton class="size-10" />
				</div>
			</div>
			<div class="flex flex-col gap-3">
				<Skeleton class="h-4 w-24" /><Skeleton class="h-10" /><Skeleton class="h-10" /><Skeleton
					class="h-10"
				/>
			</div>
		</div>
	{/if}
{:else if !v.connected}
	<div class="card-print p-5">
		<p class="text-sm text-ink-soft">
			Spotify is not connected. <a
				href={settingsHref}
				class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				>Connect it in Settings</a
			>.
		</p>
	</div>
{:else}
	{#if !canControl}
		<div class="card mb-6 flex flex-col gap-1 p-3 text-sm" role="note">
			<p class="font-semibold text-ink">Playback control is not authorized</p>
			<p class="text-ink-soft">
				The Spotify token was granted before playback control existed. <a
					href={settingsHref}
					class="font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
					>Re-authorize Spotify</a
				> to use these controls.
			</p>
		</div>
	{/if}

	<div class="grid items-start gap-8 lg:grid-cols-2">
		<div class="flex min-w-0 flex-col gap-8">
			<section
				class="card-print flex flex-col gap-4 p-5 sm:p-6"
				aria-labelledby="{uid}-title"
				aria-busy={ctl.busy}
			>
				<div class="flex items-center justify-between gap-2">
					<h2 id="{uid}-title" class="eyebrow">Playing</h2>
					{#if device}
						<span class="truncate font-mono text-xs text-ink-soft" title={device.name}>
							on {device.name}
						</span>
					{/if}
				</div>
				{#if st}
					<PlayerTransport {ctl} {st} />
					{#if item && audio.data?.listen_enabled}
						<div><AudioStatusBadge status={audio.data.data[item.id]} enabled /></div>
					{/if}
					{#if item}
						<NowPlayingLyrics
							trackId={item.id}
							snap={lyricsSnap}
							playing={st.is_playing}
							durationMs={item.duration_ms}
						/>
					{/if}
				{:else}
					<p class="text-sm text-ink-soft">
						No device is playing right now. Pick a device to start playback.
					</p>
				{/if}
			</section>

			<PlayerDevices
				devices={extras.data ? extras.data.devices : undefined}
				activeId={device?.id ?? null}
				disabled={!canControl}
				refreshing={manual.active}
				onpick={transfer}
				onrefresh={() => manual.run()}
			/>
		</div>

		<div class="flex min-w-0 flex-col gap-8">
			{#if st}
				<PlayerQueue
					queue={extras.data ? extras.data.queue : undefined}
					disabled={!canControl}
					onplay={playTrack}
					onplayfrom={playFromQueue}
					onqueue={queueTrack}
					audio={audio.data}
				/>
			{:else}
				<p class="text-sm text-ink-soft">The queue and search appear once a device plays.</p>
			{/if}
		</div>
	</div>
{/if}
