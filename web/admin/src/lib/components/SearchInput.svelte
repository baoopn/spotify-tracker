<script lang="ts">
	import { Search, X } from '@lucide/svelte';
	import { debounce } from '#lib/debounce.js';

	let {
		value = '',
		placeholder = 'Search…',
		label = 'Search',
		delay = 300,
		onsearch
	}: {
		value?: string;
		placeholder?: string;
		label?: string;
		delay?: number;
		onsearch: (q: string) => void;
	} = $props();

	// Local draft, seeded from the prop; the parent owns the committed value.
	// svelte-ignore state_referenced_locally
	let draft = $state(value);

	// svelte-ignore state_referenced_locally
	const emit = debounce((q: string) => onsearch(q.trim()), delay);

	function oninput() {
		emit(draft);
	}

	function clear() {
		emit.cancel();
		draft = '';
		onsearch('');
	}
</script>

<div class="relative w-full sm:w-72">
	<Search
		class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted"
		aria-hidden="true"
	/>
	<input
		type="search"
		class="input pr-9 pl-9"
		{placeholder}
		aria-label={label}
		bind:value={draft}
		{oninput}
		onkeydown={(e) => {
			if (e.key === 'Enter') {
				emit.cancel();
				onsearch(draft.trim());
			}
		}}
	/>
	{#if draft}
		<button
			type="button"
			class="absolute top-1/2 right-2 grid size-6 -translate-y-1/2 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-fg"
			aria-label="Clear search"
			onclick={clear}
		>
			<X class="size-4" aria-hidden="true" />
		</button>
	{/if}
</div>

<style>
	input[type='search']::-webkit-search-cancel-button {
		display: none;
	}
</style>
