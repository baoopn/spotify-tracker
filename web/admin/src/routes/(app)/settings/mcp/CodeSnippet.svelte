<script lang="ts">
	import CopyButton from '#lib/components/CopyButton.svelte';

	let { code, label }: { code: string; label: string } = $props();
</script>

<div class="flex items-start gap-2">
	<pre
		class="min-w-0 flex-1 overflow-x-auto rounded-xl bg-surface-2 px-3 py-2 font-mono text-xs whitespace-pre-wrap text-fg"><code
			>{code}</code
		></pre>
	<div class="shrink-0"><CopyButton value={code} {label} /></div>
</div>
