<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { ClassValue } from 'svelte/elements';
	import { httpsHref } from '../links';

	let {
		href,
		class: className,
		label,
		children
	}: { href: string; class?: ClassValue; label?: string; children: Snippet } = $props();

	// Only https: links render as links; anything else (javascript:, data:, ...) is plain text.
	const safe = $derived(httpsHref(href));
</script>

{#if safe}
	<!-- External (Spotify) URLs never go through the SvelteKit router. -->
	<!-- eslint-disable svelte/no-navigation-without-resolve -->
	<a
		href={safe}
		target="_blank"
		rel="noopener noreferrer external"
		class={className}
		aria-label={label}
	>
		{@render children()}
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{:else}
	<span class={className}>{@render children()}</span>
{/if}
