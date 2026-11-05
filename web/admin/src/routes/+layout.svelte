<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/outfit';
	import '@fontsource-variable/jetbrains-mono';
	import '../app.css';
	import { onMount } from 'svelte';
	import Toasts from '#lib/components/Toasts.svelte';
	import { auth } from '#lib/auth.svelte.js';
	import { theme } from '#lib/theme.svelte.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	onMount(() => {
		const stopTheme = theme.start();
		const stopZone = zone.start();
		void auth.init();
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
	class="sr-only z-50 rounded-lg bg-primary px-3 py-2 text-primary-fg focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
	>Skip to content</a
>

{@render children()}

<Toasts />
