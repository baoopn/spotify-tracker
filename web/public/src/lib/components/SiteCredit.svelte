<script lang="ts">
	import { onMount } from 'svelte';
	import { fetchCredit, type Credit } from '#lib/credit.js';
	import { SITE_URL } from '#lib/links.js';
	import { theme } from '#lib/theme.svelte.js';

	const LOGO_HEIGHT = 20;

	// Prerendered fallback; the live credit replaces it only once it is known to be good.
	let credit = $state<Credit | null>(null);
	// Logo URLs that failed to load; another variant may still work.
	let failed = $state<string[]>([]);

	const logo = $derived(
		credit && (theme.resolved === 'dark' ? credit.logo.dark : credit.logo.light)
	);
	// Width from the aspect ratio, clamped so odd sizes can't reserve silly space.
	const logoWidth = $derived(
		logo ? Math.min(Math.max(1, Math.round((LOGO_HEIGHT * logo.width) / logo.height)), 200) : 0
	);

	onMount(() => {
		const ctrl = new AbortController();
		fetchCredit(ctrl.signal).then((c) => {
			if (!ctrl.signal.aborted) credit = c;
		});
		return () => ctrl.abort();
	});
</script>

<p class="flex flex-wrap items-center gap-x-2">
	{#if credit && logo && !failed.includes(logo.url)}
		<span class="text-sm text-ink-soft">{credit.text}</span>
		<a
			href={credit.url}
			rel="noopener"
			class="link flex min-h-11 items-center gap-2 rounded-[8px] text-sm font-medium text-ink"
		>
			<img
				src={logo.url}
				alt={credit.alt}
				width={logoWidth}
				height={LOGO_HEIGHT}
				decoding="async"
				class="h-5 w-auto"
				onerror={() => (failed = [...failed, logo.url])}
			/>
		</a>
	{:else}
		<span class="text-sm text-ink-soft">Designed &amp; built by</span>
		<a
			href={SITE_URL}
			class="link flex min-h-11 items-center gap-2 rounded-[8px] text-sm font-medium text-ink"
			>baoopn</a
		>
	{/if}
</p>
