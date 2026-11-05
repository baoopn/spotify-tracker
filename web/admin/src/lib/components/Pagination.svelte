<script lang="ts">
	import { ChevronLeft, ChevronRight } from '@lucide/svelte';
	import { num } from '#lib/format.js';

	let {
		offset,
		limit,
		total,
		disabled = false,
		onchange
	}: {
		offset: number;
		limit: number;
		total: number;
		disabled?: boolean;
		onchange: (offset: number) => void;
	} = $props();

	const page = $derived(Math.floor(offset / limit) + 1);
	const pages = $derived(Math.max(1, Math.ceil(total / limit)));
	const from = $derived(total === 0 ? 0 : offset + 1);
	const to = $derived(Math.min(offset + limit, total));
</script>

<nav
	class="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3 text-sm"
	aria-label="Pagination"
>
	<p class="text-muted">
		{#if total > 0}
			Showing <span class="font-medium text-fg">{num(from)}–{num(to)}</span> of
			<span class="font-medium text-fg">{num(total)}</span>
		{:else}
			No results
		{/if}
	</p>
	<div class="flex items-center gap-2">
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			disabled={disabled || page <= 1}
			onclick={() => onchange(Math.max(0, offset - limit))}
		>
			<ChevronLeft class="size-4" aria-hidden="true" /> Previous
		</button>
		<span class="text-muted tabular-nums" aria-live="polite">Page {page} / {pages}</span>
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			disabled={disabled || page >= pages}
			onclick={() => onchange(offset + limit)}
		>
			Next <ChevronRight class="size-4" aria-hidden="true" />
		</button>
	</div>
</nav>
