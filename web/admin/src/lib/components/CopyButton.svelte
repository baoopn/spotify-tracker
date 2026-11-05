<script lang="ts">
	import { Check, Copy } from '@lucide/svelte';
	import { toasts } from '#lib/toast.svelte.js';

	let { value, label = 'Copy' }: { value: string; label?: string } = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
			copied = true;
			clearTimeout(timer);
			timer = setTimeout(() => (copied = false), 1600);
		} catch {
			toasts.error('Copy failed', 'Your browser blocked clipboard access.');
		}
	}
</script>

<button type="button" class="btn btn-secondary btn-sm" onclick={copy} aria-label={label}>
	{#if copied}
		<Check class="size-4 text-ok" aria-hidden="true" /> Copied
	{:else}
		<Copy class="size-4" aria-hidden="true" /> Copy
	{/if}
</button>
<span class="sr-only" aria-live="polite">{copied ? 'Copied to clipboard' : ''}</span>
