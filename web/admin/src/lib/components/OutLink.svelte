<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { ClassValue } from 'svelte/elements';

	let {
		href,
		class: className,
		label,
		children
	}: { href: string; class?: ClassValue; label?: string; children: Snippet } = $props();
</script>

<!-- External (Spotify) URLs never go through the SvelteKit router. -->
<!-- eslint-disable svelte/no-navigation-without-resolve -->
<a {href} target="_blank" rel="noopener noreferrer external" class={className} aria-label={label}>
	{@render children()}
</a>
<!-- eslint-enable svelte/no-navigation-without-resolve -->
