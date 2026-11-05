<script lang="ts">
	import BrandLogo from '#lib/components/BrandLogo.svelte';
	import { page } from '$app/state';
	import AuthScreen from '#lib/components/shell/AuthScreen.svelte';
	import ModePicker from '#lib/components/ModePicker.svelte';
	import { auth, navigateTo, safeNext } from '#lib/auth.svelte.js';
	import ZoneNotice from '#lib/components/ZoneNotice.svelte';
	import { secondClock } from '#lib/clock.svelte.js';
	import { timeOfDay } from '#lib/time.js';
	import { zone } from '#lib/zone.svelte.js';

	const next = $derived(safeNext(page.url.searchParams.get('next')));

	// Already an admin? Go where we were heading.
	$effect(() => {
		if (auth.status === 'signed-in') void navigateTo(next, true);
	});

	/** Mounts Clerk's <SignIn /> into the element; unmounts on cleanup. */
	function clerkSignIn(node: HTMLDivElement) {
		const clerk = auth.clerk;
		if (!clerk) return;
		const redirect = new URL(next, window.location.origin).href;
		clerk.mountSignIn(node, {
			routing: 'hash',
			forceRedirectUrl: redirect,
			fallbackRedirectUrl: redirect
		});
		return () => clerk.unmountSignIn(node);
	}
</script>

<svelte:head>
	<title>Sign in · Listening admin</title>
</svelte:head>

{#if auth.status === 'forbidden' || auth.status === 'unauthorized' || auth.status === 'error'}
	<AuthScreen kind={auth.status} />
{:else if auth.status === 'signed-out'}
	<main
		id="main"
		class="mx-auto grid min-h-dvh max-w-5xl content-center items-center gap-10 p-6 lg:grid-cols-[1fr_auto]"
	>
		<div class="flex flex-col items-start gap-5">
			<BrandLogo kind="listening-mark" class="size-14" />
			<div>
				<p class="eyebrow">listening.baoopn.com · admin</p>
				<h1 class="mt-2 text-5xl leading-none font-semibold text-ink">
					The back of<br />the sleeve.
				</h1>
				<p class="mt-4 max-w-sm text-ink-soft">
					Plays, tracks and the plumbing behind what Bao is listening to. Admins only.
				</p>
			</div>
			{#if page.url.searchParams.get('next')}
				<p class="rounded-lg bg-spot-sky px-3 py-2 text-sm text-on-spot" role="status">
					Your session ended. Sign in again to continue where you left off.
				</p>
			{/if}
			<ModePicker size="sm" />
			<footer class="flex flex-col items-start gap-2 border-t border-rule pt-3">
				<p class="font-mono text-xs text-ink-soft">
					Local time {timeOfDay(new Date(secondClock.now))} · {zone.iana ?? 'zone unknown (UTC)'}
				</p>
				<ZoneNotice />
			</footer>
		</div>

		<div {@attach clerkSignIn} class="min-h-[22rem]"></div>
	</main>
{:else}
	<AuthScreen kind="loading" />
{/if}
