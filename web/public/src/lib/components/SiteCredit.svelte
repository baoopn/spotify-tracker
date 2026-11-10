<script lang="ts">
	import { onMount } from 'svelte';
	import { fetchCredit, type Credit } from '#lib/credit.js';
	import { SITE_URL } from '#lib/links.js';
	import { theme } from '#lib/theme.svelte.js';

	const LOGO_SIZE = 24;

	// Prerendered fallback ("by baoopn"); the live monogram is added once it is known to be good.
	let credit = $state<Credit | null>(null);
	// Logo URLs that failed to load; another variant may still work.
	let failed = $state<string[]>([]);

	const logo = $derived(
		credit && (theme.resolved === 'dark' ? credit.logo.dark : credit.logo.light)
	);
	const showLogo = $derived(logo !== null && !failed.includes(logo.url));

	onMount(() => {
		const ctrl = new AbortController();
		fetchCredit(ctrl.signal).then((c) => {
			if (!ctrl.signal.aborted) credit = c;
		});
		return () => ctrl.abort();
	});
</script>

<!-- "by [monogram] baoopn"; the text names the link, so the mark is decorative. -->
<a
	href={credit?.url ?? SITE_URL}
	rel="noopener"
	class="link flex min-h-11 items-center gap-2 rounded-[8px] text-sm font-medium text-ink"
>
	by
	{#if logo && showLogo}
		<img
			src={logo.url}
			alt=""
			width={LOGO_SIZE}
			height={LOGO_SIZE}
			decoding="async"
			class="size-6 rounded-[5px]"
			onerror={() => (failed = [...failed, logo.url])}
		/>
	{/if}
	baoopn
</a>
