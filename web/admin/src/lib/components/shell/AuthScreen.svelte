<script lang="ts">
	import BrandLogo from '../BrandLogo.svelte';
	import { CircleAlert, LoaderCircle, LogOut, RefreshCw, ShieldX } from '@lucide/svelte';
	import { auth } from '#lib/auth.svelte.js';

	let { kind }: { kind: 'loading' | 'forbidden' | 'unauthorized' | 'error' } = $props();

	const email = $derived(auth.clerk?.user?.primaryEmailAddress?.emailAddress ?? null);
</script>

<svelte:head>
	<title>Listening admin</title>
</svelte:head>

<main class="grid min-h-dvh place-items-center p-4">
	{#if kind === 'loading'}
		<div class="flex flex-col items-center gap-4 text-muted" role="status" aria-live="polite">
			<BrandLogo kind="listening-mark" class="size-14" />
			<span class="eyebrow flex items-center gap-2">
				<LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Loading dashboard…
			</span>
		</div>
	{:else}
		<div class="card-print w-full max-w-md p-8">
			<div
				class={[
					'inline-flex items-center gap-2 rounded-full px-3 py-1 text-on-spot',
					kind === 'forbidden' ? 'bg-spot-butter' : 'bg-spot-rose'
				]}
			>
				{#if kind === 'forbidden'}
					<ShieldX class="size-4" aria-hidden="true" /><span class="eyebrow text-on-spot">403</span>
				{:else}
					<CircleAlert class="size-4" aria-hidden="true" /><span class="eyebrow text-on-spot"
						>Error</span
					>
				{/if}
			</div>

			{#if kind === 'forbidden'}
				<h1 class="mt-5 text-3xl font-semibold text-ink">Access denied</h1>
				<p class="mt-2 text-sm text-muted">
					This account is not an admin{#if email}&nbsp;(<span class="font-medium text-fg"
							>{email}</span
						>){/if}. Ask the operator to add it to the admin allow-list, or sign in with a different
					account.
				</p>
			{:else if kind === 'unauthorized'}
				<h1 class="mt-5 text-3xl font-semibold text-ink">Session not accepted</h1>
				<p class="mt-2 text-sm text-muted">
					You are signed in, but the API rejected the session token. Check that the server's Clerk
					configuration matches this instance.
				</p>
				{#if auth.error}<p class="mt-2 font-mono text-xs break-words text-muted">
						{auth.error}
					</p>{/if}
			{:else}
				<h1 class="mt-5 text-3xl font-semibold text-ink">Could not start the dashboard</h1>
				<p class="mt-2 text-sm break-words text-muted">
					{auth.error ?? 'Unknown error'}
				</p>
			{/if}

			<div class="mt-6 flex flex-wrap gap-2">
				{#if kind === 'error'}
					<button type="button" class="btn btn-primary" onclick={() => auth.retry()}>
						<RefreshCw class="size-4" aria-hidden="true" /> Try again
					</button>
				{:else}
					<button type="button" class="btn btn-secondary" onclick={() => auth.refresh()}>
						<RefreshCw class="size-4" aria-hidden="true" /> Retry
					</button>
				{/if}
				{#if auth.clerk?.user}
					<button type="button" class="btn btn-primary" onclick={() => auth.signOut()}>
						<LogOut class="size-4" aria-hidden="true" /> Sign out
					</button>
				{/if}
			</div>
		</div>
	{/if}
</main>
