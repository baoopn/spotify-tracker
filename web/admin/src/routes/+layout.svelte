<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/outfit';
	import '@fontsource-variable/jetbrains-mono';
	// Lyrics font (--font-lyrics): only the unicode-range slices in use are fetched.
	import '@fontsource-variable/noto-sans';
	import '@fontsource-variable/noto-sans-thai';
	import '../app.css';
	import { onMount } from 'svelte';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import Toasts from '#lib/components/Toasts.svelte';
	import { auth } from '#lib/auth.svelte.js';
	import { queryClient } from '#lib/query-client.js';
	import { theme } from '#lib/theme.svelte.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	onMount(() => {
		const stopTheme = theme.start();
		const stopZone = zone.start();
		void auth.init();
		// The CJK faces' CSS is large (~120 KB gzip in all), so it loads after
		// the first paint instead of blocking it.
		void Promise.all([
			import('@fontsource-variable/noto-sans-sc'),
			import('@fontsource-variable/noto-sans-tc'),
			import('@fontsource-variable/noto-sans-jp'),
			import('@fontsource-variable/noto-sans-kr')
		]).catch(() => {});
		return () => {
			stopTheme();
			stopZone();
		};
	});

	// Keep <html data-palette/data-theme> in sync (system mode follows the OS live).
	$effect(() => {
		theme.apply();
	});
</script>

<a
	href="#main"
	class="sr-only z-50 rounded-lg bg-primary px-3 py-2 text-primary-fg focus:not-sr-only focus:fixed focus:top-[calc(0.75rem+env(safe-area-inset-top,0px))] focus:left-[max(0.75rem,env(safe-area-inset-left,0px))]"
	>Skip to content</a
>

<QueryClientProvider client={queryClient}>
	{@render children()}
</QueryClientProvider>

<Toasts />
