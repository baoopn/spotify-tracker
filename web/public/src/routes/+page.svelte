<script lang="ts">
	import { onMount } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import NowPlayingHero from '#lib/components/NowPlayingHero.svelte';
	import RecentList from '#lib/components/RecentList.svelte';
	import RecentPlaylists from '#lib/components/RecentPlaylists.svelte';
	import SiteCredit from '#lib/components/SiteCredit.svelte';
	import SongStats from '#lib/components/stats/SongStats.svelte';
	import TopPlaylists from '#lib/components/TopPlaylists.svelte';
	import TopTracks from '#lib/components/TopTracks.svelte';
	import { secondClock } from '#lib/clock.svelte.js';
	import { zone } from '#lib/zone.svelte.js';
	import { live } from '#lib/live.svelte.js';
	import { NOW_POLL_MS, nowPlayingQuery } from '#lib/queries.js';
	import { CONTACT_URL } from '#lib/links.js';

	const queryClient = useQueryClient();
	onMount(() => live.start(queryClient));

	// The one now-playing observer that polls, and only while the stream is down.
	const now = createQuery(() => ({
		...nowPlayingQuery(),
		refetchInterval: live.polling ? NOW_POLL_MS : false
	}));
	const lastUpdated = $derived(now.dataUpdatedAt > 0 ? new Date(now.dataUpdatedAt) : null);

	// Honest connection status; the countdown reads the 1 s clock only while waiting.
	const status = $derived.by(() => {
		switch (live.connection) {
			case 'live':
				return 'Updated live';
			case 'connecting':
				return 'Connecting…';
			case 'polling':
				return 'Polling every 10 s';
			case 'reconnecting': {
				const secs = live.retryAt ? Math.ceil((live.retryAt - secondClock.now) / 1000) : 0;
				return secs > 0 ? `Reconnecting in ${secs} s` : 'Reconnecting…';
			}
		}
	});
</script>

<svelte:head>
	<title>What Bao is listening to</title>
</svelte:head>

<main id="main" class="flex-1 pb-16">
	<h1 class="label mb-8 flex items-center gap-3 text-ink-soft sm:mb-10">
		What Bao is listening to
		<span class="h-px flex-1 bg-rule" aria-hidden="true"></span>
	</h1>

	<div class="space-y-16 sm:space-y-20">
		<NowPlayingHero />
		<SongStats />
		<TopTracks />
		<TopPlaylists />
		<RecentList />
		<RecentPlaylists />
	</div>
</main>

<footer
	class="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 border-t border-ink py-5 text-ink-soft"
>
	<div class="label flex flex-wrap items-center gap-x-4 gap-y-1">
		<p class="flex items-center gap-2">
			<span
				class={[
					'size-2.5 rounded-full border border-ink',
					live.connection === 'live'
						? 'bg-spot-mint'
						: live.connection === 'reconnecting'
							? 'bg-spot-butter'
							: 'bg-paper-2'
				]}
				aria-hidden="true"
			></span>
			{status}
		</p>
		{#if lastUpdated}
			<p>
				Last updated
				<time
					datetime={lastUpdated.toISOString()}
					title={zone.correctionNote ?? zone.dateTime(lastUpdated)}
					>{zone.clockTime(lastUpdated)}</time
				>
			</p>
		{/if}
		{#if zone.resolution.kind === 'unknown'}
			<!-- Opens the appearance popover, where the time zone can be set. -->
			<button type="button" popovertarget="theme-panel" class="link label text-ink-soft"
				>Time zone unknown — set it</button
			>
		{/if}
	</div>

	<div class="flex flex-wrap items-center gap-x-5">
		<a
			href={CONTACT_URL}
			class="link flex min-h-11 items-center rounded-[8px] text-sm font-medium text-ink">Contact</a
		>
		<SiteCredit />
	</div>
</footer>
