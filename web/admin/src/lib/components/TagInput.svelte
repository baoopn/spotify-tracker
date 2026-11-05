<script lang="ts">
	import { X } from '@lucide/svelte';

	let {
		values = $bindable([]),
		id,
		placeholder = 'Type and press Enter',
		invalid = false,
		validate,
		describedby
	}: {
		values?: string[];
		id: string;
		placeholder?: string;
		invalid?: boolean;
		/** Return an error message to reject a tag. */
		validate?: (v: string) => string | null;
		describedby?: string;
	} = $props();

	let draft = $state('');
	let error = $state<string | null>(null);
	let input: HTMLInputElement | undefined = $state();

	function commit(): boolean {
		const parts = draft
			.split(/[,;\s]+/)
			.map((s) => s.trim())
			.filter(Boolean);
		if (parts.length === 0) return false;
		for (const p of parts) {
			const msg = validate?.(p) ?? null;
			if (msg) {
				error = msg;
				return false;
			}
		}
		const next = [...values];
		for (const p of parts) if (!next.includes(p)) next.push(p);
		values = next;
		draft = '';
		error = null;
		return true;
	}

	function remove(v: string) {
		values = values.filter((x) => x !== v);
		input?.focus();
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			if (draft.trim()) {
				e.preventDefault();
				commit();
			}
		} else if (e.key === 'Backspace' && !draft && values.length) {
			values = values.slice(0, -1);
		}
	}
</script>

<div
	class={[
		'input flex min-h-[2.375rem] flex-wrap items-center gap-1.5 py-1.5',
		(invalid || error) && 'border-danger'
	]}
>
	{#each values as v (v)}
		<span class="chip">
			{v}
			<button
				type="button"
				class="-mr-1 grid size-4 place-items-center rounded-full hover:bg-spot-select"
				aria-label="Remove {v}"
				onclick={() => remove(v)}
			>
				<X class="size-3" aria-hidden="true" />
			</button>
		</span>
	{/each}
	<input
		bind:this={input}
		{id}
		class="min-w-[10rem] flex-1 bg-transparent text-sm text-fg outline-none"
		{placeholder}
		bind:value={draft}
		{onkeydown}
		onblur={() => commit()}
		aria-invalid={invalid || !!error}
		aria-describedby={describedby}
		autocomplete="off"
	/>
</div>
{#if error}<p class="field-error" role="alert">{error}</p>{/if}
