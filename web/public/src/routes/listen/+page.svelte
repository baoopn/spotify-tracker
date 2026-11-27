<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { resolve } from '$app/paths';
	import NowPlayingHero from '#lib/components/NowPlayingHero.svelte';
	import SiteCredit from '#lib/components/SiteCredit.svelte';
	import ListenControls from '#lib/components/listen/ListenControls.svelte';
	import ListenGate from '#lib/components/listen/ListenGate.svelte';
	import ListenHistory from '#lib/components/listen/ListenHistory.svelte';
	import { live } from '#lib/live.svelte.js';
	import { listenAuth } from '#lib/listen/auth.svelte.js';
	import { listenPlayer } from '#lib/listen/player.svelte.js';
	import { postListenPresence } from '#lib/listen/api.js';
	import { PresencePinger, presenceState, presenceTrackId } from '#lib/listen/presence.js';
	import { NOW_POLL_MS, nowPlayingQuery } from '#lib/queries.js';

	const queryClient = useQueryClient();
	onMount(() => live.start(queryClient));
	onMount(() => {
		void listenAuth.init();
	});

	// The one now-playing observer that polls, and only while the stream is down.
	const now = createQuery(() => ({
		...nowPlayingQuery(),
		refetchInterval: live.polling ? NOW_POLL_MS : false
	}));

	// Every snapshot (SSE or poll) drives the player; audio only once allowed.
	$effect(() => {
		const np = now.data ?? null;
		const allowed = listenAuth.status === 'allowed';
		untrack(() => listenPlayer.update(np, allowed));
	});

	// "Listening now" in the admin: on load, on every change and every 30 s.
	const presence = new PresencePinger((body, keepalive) =>
		listenAuth.withToken(keepalive, (t) => postListenPresence(t, body, keepalive))
	);
	onMount(() => presence.start());
	const trackId = $derived(now.data?.track?.id ?? null);
	const presenceActive = $derived(
		listenAuth.status === 'allowed' &&
			listenPlayer.status !== 'access-ended' &&
			listenPlayer.status !== 'off'
	);
	const presenceBody = $derived({
		state: presenceState({
			joined: listenPlayer.joined,
			status: listenPlayer.status,
			hidden: false
		}),
		track_id: presenceTrackId(trackId)
	});
	// Sending is a side effect: only the network call happens here.
	$effect(() => {
		const body = presenceBody;
		const active = presenceActive;
		untrack(() => (active ? presence.update(body) : presence.stop()));
	});
</script>

<svelte:head>
	<title>Listen along with Bao</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main id="main" class="flex-1 pb-16">
	<h1 class="label mb-8 flex items-center gap-3 text-ink-soft sm:mb-10">
		Listen along with Bao
		<span class="h-px flex-1 bg-rule" aria-hidden="true"></span>
	</h1>

	<ListenGate>
		<ListenControls />
		<NowPlayingHero />
		<ListenHistory {trackId} />
	</ListenGate>
</main>

<!-- Always in the DOM, so one element keeps the autoplay permission across songs. -->
<audio {@attach listenPlayer.attach} preload="auto"></audio>

<footer
	class="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 border-t border-ink py-5 text-ink-soft"
>
	<a href={resolve('/')} class="link flex min-h-11 items-center text-sm font-medium text-ink"
		>See everything Bao plays</a
	>
	<SiteCredit />
</footer>
