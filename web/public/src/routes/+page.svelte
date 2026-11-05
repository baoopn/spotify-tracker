<script lang="ts">
	import { onMount } from 'svelte';
	import NowPlayingHero from '#lib/components/NowPlayingHero.svelte';
	import RecentList from '#lib/components/RecentList.svelte';
	import RecentPlaylists from '#lib/components/RecentPlaylists.svelte';
	import SongStats from '#lib/components/stats/SongStats.svelte';
	import { secondClock } from '#lib/clock.svelte.js';
	import { zone } from '#lib/zone.svelte.js';
	import { live } from '#lib/live.svelte.js';

	onMount(() => live.start());

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
		{#if live.lastUpdated}
			<p>
				Last updated
				<time
					datetime={live.lastUpdated.toISOString()}
					title={zone.correctionNote ?? zone.dateTime(live.lastUpdated)}
					>{zone.clockTime(live.lastUpdated)}</time
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

	<!-- Master brand only as the link back to the main site (monogram, 24 px). -->
	<a
		href="https://baoopn.com"
		class="link flex min-h-11 items-center gap-2 rounded-[8px] text-sm font-medium text-ink"
	>
		by
		<img
			src="/logo/baoopn-monogram-primary.svg"
			alt=""
			width="200"
			height="200"
			class="size-6 rounded-[5px] dark:hidden"
		/>
		<img
			src="/logo/baoopn-monogram-inverse.svg"
			alt=""
			width="200"
			height="200"
			class="hidden size-6 rounded-[5px] dark:block"
		/>
		baoopn
	</a>
</footer>
