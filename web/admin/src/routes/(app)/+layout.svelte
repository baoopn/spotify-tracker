<script lang="ts">
	import Sidebar from '#lib/components/shell/Sidebar.svelte';
	import TopBar from '#lib/components/shell/TopBar.svelte';
	import AuthScreen from '#lib/components/shell/AuthScreen.svelte';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { auth, currentPath, navigateTo } from '#lib/auth.svelte.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	let sidebarOpen = $state(false);
	const queryClient = useQueryClient();

	// Queries only exist while signed in (children render behind the gate below);
	// drop cached admin data as soon as the session is gone or rejected.
	$effect(() => {
		if (auth.status === 'signed-in' || auth.status === 'checking') return;
		queryClient.clear();
	});

	// Auth guard: no Clerk session -> sign-in page, remembering where we were.
	$effect(() => {
		if (auth.status !== 'signed-out') return;
		if (auth.clerk?.user) {
			// Clerk just signed in but our status hasn't caught up yet.
			void auth.refresh();
			return;
		}
		void navigateTo(`/admin/login?next=${encodeURIComponent(currentPath())}`, true);
	});
</script>

{#if auth.status === 'signed-in'}
	<div class="flex min-h-dvh">
		<Sidebar bind:open={sidebarOpen} />
		<div class="flex min-w-0 flex-1 flex-col">
			<TopBar bind:sidebarOpen />
			<main id="main" class="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6 lg:px-8">
				{@render children()}
			</main>
		</div>
	</div>
{:else if auth.status === 'forbidden' || auth.status === 'unauthorized' || auth.status === 'error'}
	<AuthScreen kind={auth.status} />
{:else}
	<AuthScreen kind="loading" />
{/if}
