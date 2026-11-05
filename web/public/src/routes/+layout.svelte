<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/outfit';
	import '@fontsource-variable/jetbrains-mono';
	import '../app.css';
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import ThemeSwitcher from '#lib/components/ThemeSwitcher.svelte';
	import { theme } from '#lib/theme.svelte.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	onMount(() => {
		const stopTheme = theme.start();
		const stopZone = zone.start();
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
	class="sr-only z-[60] rounded-[8px] border border-ink bg-ink px-3 py-2 text-paper focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
	>Skip to content</a
>

<div class="mx-auto flex min-h-dvh max-w-6xl flex-col px-4 sm:px-8">
	<header class="flex items-center justify-between gap-4 py-4 sm:py-6">
		<a href={resolve('/')} class="shrink-0 rounded-brand" aria-label="baoopn listening, home">
			<!-- Listening sub-brand: primary badge in light mode, inverse in dark (>= 120 px wide). -->
			<img
				src="/logo/listening-wordmark-primary.svg"
				alt="baoopn listening"
				width="320"
				height="132"
				class="h-[50px] w-auto sm:h-14 dark:hidden"
			/>
			<img
				src="/logo/listening-wordmark-inverse.svg"
				alt="baoopn listening"
				width="320"
				height="132"
				class="hidden h-[50px] w-auto sm:h-14 dark:block"
			/>
		</a>
		<ThemeSwitcher />
	</header>

	{@render children()}
</div>
