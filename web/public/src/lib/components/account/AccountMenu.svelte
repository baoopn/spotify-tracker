<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { resolve } from '$app/paths';
	import { initials } from '#lib/account/redirect.js';
	import { listenAuth } from '#lib/listen/auth.svelte.js';

	// Header account control. Renders nothing while listen along is off, and
	// loads Clerk on / only for a likely session or a Sign in click.
	onMount(() => {
		void listenAuth.peek();
	});

	const menuId = $props.id();
	let open = $state(false);
	let signingIn = $state(false);
	let button = $state<HTMLButtonElement | null>(null);
	let menu = $state<HTMLDivElement | null>(null);

	const profile = $derived(listenAuth.profile);
	const status = $derived(listenAuth.status);
	/** Clerk is loading for a likely session: hold the spot without a Sign in flash. */
	const pending = $derived(
		!profile && (status === 'idle' || status === 'loading' || status === 'checking')
	);
	const label = $derived(profile?.name || profile?.email || 'Account');

	async function signIn() {
		signingIn = true;
		try {
			await listenAuth.openSignIn();
		} finally {
			signingIn = false;
		}
	}

	function items(): HTMLElement[] {
		return menu ? [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')] : [];
	}

	async function show() {
		open = true;
		await tick();
		items()[0]?.focus();
	}

	function close(refocus = true) {
		open = false;
		if (refocus) button?.focus();
	}

	function onMenuKey(e: KeyboardEvent) {
		const list = items();
		const i = list.indexOf(document.activeElement as HTMLElement);
		const go = (n: number) => {
			e.preventDefault();
			list[(n + list.length) % list.length]?.focus();
		};
		if (e.key === 'ArrowDown') go(i + 1);
		else if (e.key === 'ArrowUp') go(i - 1);
		else if (e.key === 'Home') go(0);
		else if (e.key === 'End') go(list.length - 1);
		else if (e.key === 'Escape') {
			e.preventDefault();
			close();
		} else if (e.key === 'Tab') close(false);
	}

	function onButtonKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			void show();
		}
	}

	function onWindowPointer(e: PointerEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (!menu?.contains(t) && !button?.contains(t)) close(false);
	}

	function run(fn: () => unknown) {
		close();
		void fn();
	}
</script>

<svelte:window onpointerdown={onWindowPointer} />

{#if listenAuth.enabled}
	{#if profile}
		<div class="relative">
			<button
				bind:this={button}
				type="button"
				class="avatar btn btn-secondary btn-icon overflow-hidden p-0!"
				aria-haspopup="menu"
				aria-expanded={open}
				aria-controls={open ? menuId : undefined}
				aria-label="Account: {label}"
				title={label}
				onclick={() => (open ? close() : show())}
				onkeydown={onButtonKey}
			>
				{#if profile.imageUrl}
					<img src={profile.imageUrl} alt="" class="size-full object-cover" />
				{:else}
					<span class="font-display text-sm font-semibold" aria-hidden="true"
						>{initials(profile.name, profile.email)}</span
					>
				{/if}
			</button>

			{#if open}
				<div
					bind:this={menu}
					id={menuId}
					role="menu"
					tabindex="-1"
					aria-label="Account"
					class="print absolute top-[calc(100%+0.5rem)] right-0 z-40 w-64 max-w-[calc(100vw-2rem)] rounded-brand bg-paper-2 p-1.5"
					onkeydown={onMenuKey}
				>
					<div class="border-b border-rule px-3 pt-2 pb-2.5" role="presentation">
						{#if profile.name}<p class="truncate font-semibold">{profile.name}</p>{/if}
						<p class="truncate text-sm text-ink-soft">{profile.email}</p>
					</div>
					{#if status === 'allowed'}
						<a role="menuitem" class="item" href={resolve('/listen')} onclick={() => close(false)}
							>Listen along</a
						>
					{:else if status === 'forbidden'}
						<a
							role="menuitem"
							class="item"
							href="{resolve('/listen')}#request"
							onclick={() => close(false)}>Request an invite</a
						>
					{:else if status === 'error'}
						<p class="px-3 py-2 text-sm text-ink-soft" role="presentation">
							Couldn't check your access.
						</p>
					{/if}
					<button
						type="button"
						role="menuitem"
						class="item"
						onclick={() => run(() => listenAuth.openUserProfile())}>Manage profile</button
					>
					<button
						type="button"
						role="menuitem"
						class="item"
						onclick={() => run(() => listenAuth.signOut())}>Sign out</button
					>
				</div>
			{/if}
		</div>
	{:else if pending}
		<span class="skeleton block size-11 rounded-full!" aria-hidden="true"></span>
	{:else}
		<button type="button" class="btn btn-secondary" onclick={signIn} disabled={signingIn}>
			{signingIn ? 'Loading…' : 'Sign in'}
		</button>
	{/if}
{/if}

<style>
	.avatar {
		border-radius: 999px;
	}
	.item {
		display: flex;
		align-items: center;
		width: 100%;
		min-height: 2.75rem;
		padding: 0.5rem 0.75rem;
		border-radius: 8px;
		font-size: 0.9375rem;
		font-weight: 500;
		text-align: left;
		color: var(--ink);
	}
	.item:hover,
	.item:focus-visible {
		background: var(--paper-3);
	}
</style>
