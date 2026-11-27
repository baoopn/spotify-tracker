<script lang="ts">
	import type { Snippet } from 'svelte';
	import BrandIcon from '../BrandIcon.svelte';
	import ClerkMount from './ClerkMount.svelte';
	import RequestInvite from './RequestInvite.svelte';
	import { listenAuth } from '#lib/listen/auth.svelte.js';
	import { browser } from '$app/env';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import {
		hasClerkParams,
		hasInviteTicket,
		inviteMode,
		stripClerkParams
	} from '#lib/account/invite.js';

	let { children }: { children: Snippet } = $props();

	const status = $derived(listenAuth.status);
	// page.url follows goto and replaceState, so these update without a reload.
	// Not during prerender: the static shell has no query string.
	const search = $derived(browser ? page.url.search : '');
	const mode = $derived(inviteMode(search));
	const ticket = $derived(hasInviteTicket(search));
	const signedIn = $derived(!!listenAuth.profile);

	/** Drop the __clerk_* params (a reload must never retry a used ticket). */
	function stripInvite() {
		const u = page.url;
		// eslint-disable-next-line svelte/no-navigation-without-resolve -- same page, params stripped
		replaceState(stripClerkParams(u.pathname, u.search, u.hash), {});
	}

	// Signed in on this page (the sign-up or sign-in just finished): strip the
	// params. An invite opened while already signed in keeps them for the notice.
	const justSignedIn = $derived(listenAuth.everSignedOut && signedIn && hasClerkParams(search));
	$effect(() => {
		if (justSignedIn) stripInvite();
	});
</script>

{#snippet chip(text: string, spot: string)}
	<p class="chip label" style:--chip="var({spot})">
		<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
		{text}
	</p>
{/snippet}

{#if ticket && signedIn && status !== 'signed-out' && !listenAuth.everSignedOut}
	<!-- An invite link opened while signed in: Clerk only accepts it signed out. -->
	<section class="border-t border-ink pt-8" aria-labelledby="listen-invite-h">
		{@render chip("You're invited", '--spot-lilac')}
		<h2
			id="listen-invite-h"
			class="mt-5 font-display text-3xl font-semibold tracking-tight sm:text-4xl"
		>
			This link is an invite to create a new account
		</h2>
		<p class="mt-2 break-words text-ink-soft">
			{#if listenAuth.email}You're signed in as <span class="font-medium text-ink"
					>{listenAuth.email}</span
				>.{/if}
			Sign out to accept it.
		</p>
		<div class="mt-6 flex flex-wrap items-center gap-x-5 gap-y-3">
			<!-- Sign-out stays on this URL, query intact, so the sign-up mounts. -->
			<button type="button" class="btn btn-primary" onclick={() => listenAuth.signOut()}>
				Sign out and accept
			</button>
			<button
				type="button"
				class="link flex min-h-11 items-center text-sm font-medium text-ink"
				onclick={stripInvite}
			>
				Keep using this account
			</button>
		</div>
	</section>
{:else if status === 'allowed'}
	{@render children()}
{:else if status === 'signed-out' && listenAuth.clerk}
	<section
		class="grid items-start gap-10 border-t border-ink pt-8 lg:grid-cols-[minmax(0,1fr)_auto]"
		aria-labelledby="listen-signin-h"
	>
		<div class="max-w-md">
			{@render chip(mode === 'sign_up' ? "You're invited" : 'Invite only', '--spot-lilac')}
			<h2
				id="listen-signin-h"
				class="mt-5 font-display text-4xl leading-[1.05] font-semibold tracking-[-0.02em] sm:text-5xl"
			>
				Hear what Bao hears
			</h2>
			{#if mode === 'used'}
				<p class="chip label mt-4 [--chip:var(--spot-butter)]" role="status">
					<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
					Invite used
				</p>
				<p class="mt-2 text-ink-soft">This invite was already used. Sign in instead.</p>
			{/if}
			<p class="mt-3 flex items-center gap-2 text-ink-soft">
				<BrandIcon name="headphones" class="size-5 shrink-0" />
				{mode === 'sign_up'
					? 'Create your account with the email your invite went to.'
					: 'Sign in with the email your invite went to.'}
			</p>
			{#if !ticket}<RequestInvite />{/if}
		</div>
		<div class="min-w-0">
			<ClerkMount clerk={listenAuth.clerk} />
		</div>
	</section>
{:else if status === 'disabled'}
	<section class="border-t border-ink pt-8" role="status">
		{@render chip('Off', '--spot-butter')}
		<h2 class="mt-5 font-display text-3xl font-semibold tracking-tight sm:text-4xl">
			Listen along isn't available.
		</h2>
	</section>
{:else if status === 'forbidden'}
	<!-- No alert role on the section: it also holds the request form. -->
	<section class="border-t border-ink pt-8" aria-labelledby="listen-forbidden-h">
		{@render chip('No access', '--spot-rose')}
		<h2
			id="listen-forbidden-h"
			class="mt-5 font-display text-3xl font-semibold tracking-tight sm:text-4xl"
		>
			This account isn't invited
		</h2>
		<p class="mt-2 break-words text-ink-soft">
			{#if listenAuth.email}You're signed in as <span class="font-medium text-ink"
					>{listenAuth.email}</span
				>.{/if}
			Ask Bao for an invite, or sign in with another account.
		</p>
		<div class="mt-6 flex flex-wrap gap-3">
			<button type="button" class="btn btn-primary" onclick={() => listenAuth.signOut()}>
				Sign out
			</button>
			<button type="button" class="btn btn-secondary" onclick={() => listenAuth.retry()}>
				Check again
			</button>
		</div>
		<RequestInvite email={listenAuth.email ?? ''} />
	</section>
{:else if status === 'error'}
	<section class="border-t border-ink pt-8" role="alert">
		{@render chip("Couldn't load", '--spot-rose')}
		<h2 class="mt-5 font-display text-3xl font-semibold tracking-tight sm:text-4xl">
			Something went wrong
		</h2>
		{#if listenAuth.error}<p class="mt-2 break-words text-ink-soft">{listenAuth.error}</p>{/if}
		<button type="button" class="btn btn-secondary mt-6" onclick={() => listenAuth.retry()}>
			Retry
		</button>
	</section>
{:else}
	<section class="border-t border-ink pt-8" aria-busy="true">
		<p class="label text-ink-soft" role="status">Checking your access…</p>
		<div class="mt-5 space-y-4">
			<div class="skeleton h-10 w-2/3 max-w-md"></div>
			<div class="skeleton h-5 w-1/2 max-w-sm"></div>
		</div>
	</section>
{/if}
