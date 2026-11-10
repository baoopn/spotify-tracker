<script lang="ts">
	import { onMount } from 'svelte';
	import { FALLBACK, fetchCredit, type Credit } from '#lib/credit.js';
	import { SITE_URL } from '#lib/links.js';
	import { theme } from '#lib/theme.svelte.js';

	// Prerendered fallback ("by baoopn"); the live credit replaces it once it is known to be good.
	let credit = $state<Credit | null>(null);
	// Logo URLs that failed to load; another variant may still work.
	let failed = $state<string[]>([]);

	const logo = $derived(
		credit && (theme.resolved === 'dark' ? credit.logo.dark : credit.logo.light)
	);
	const showLogo = $derived(logo !== null && !failed.includes(logo.url));
	// Live text only with a logo to show; otherwise the static fallback.
	const prefix = $derived(credit && showLogo ? credit.prefix : FALLBACK.prefix);
	const suffix = $derived(credit && showLogo ? credit.suffix : FALLBACK.suffix);

	onMount(() => {
		const ctrl = new AbortController();
		fetchCredit(ctrl.signal).then((c) => {
			if (!ctrl.signal.aborted) credit = c;
		});
		return () => ctrl.abort();
	});
</script>

<!-- "<prefix> <mark> <suffix>"; the text names the link, so the mark is decorative unless it is alone. -->
<a
	href={credit?.url ?? SITE_URL}
	rel="noopener"
	class="link flex min-h-11 items-center gap-2 rounded-[8px] text-sm font-medium text-ink"
>
	{#if prefix}<span>{prefix}</span>{/if}
	{#if credit && logo && showLogo}
		<img
			src={logo.url}
			alt={prefix || suffix ? '' : credit.alt}
			width={logo.width}
			height={logo.height}
			decoding="async"
			class={['h-6 w-auto', credit.rounded && 'rounded-[5px]']}
			onerror={() => (failed = [...failed, logo.url])}
		/>
	{/if}
	{#if suffix}<span>{suffix}</span>{/if}
</a>
