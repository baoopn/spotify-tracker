<script lang="ts">
	import Badge from '#lib/components/Badge.svelte';
	import type { McpTool } from '#lib/mcp.js';

	let {
		publicTools,
		adminTools,
		adminEnabled
	}: { publicTools: McpTool[]; adminTools: McpTool[]; adminEnabled: boolean } = $props();
</script>

{#snippet list(id: string, title: string, tools: McpTool[], note: string)}
	<div class="flex flex-col gap-3">
		<div class="flex flex-wrap items-center gap-2">
			<h3 {id} class="text-sm font-semibold text-fg">{title}</h3>
			<span class="text-xs text-muted">{note}</span>
		</div>
		<ul class="flex flex-col gap-2" aria-labelledby={id}>
			{#each tools as tool (tool.name)}
				<li class="text-sm">
					<code class="font-mono text-fg">{tool.name}</code>
					<p class="text-muted">{tool.description}</p>
				</li>
			{/each}
		</ul>
	</div>
{/snippet}

<section class="card flex flex-col gap-4 p-6" aria-labelledby="mcp-tools-h">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h2 id="mcp-tools-h" class="font-semibold text-fg">Tools</h2>
		{#if !adminEnabled}<Badge tone="warn">Admin tools off</Badge>{/if}
	</div>
	<p class="text-sm text-muted">All tools are read-only.</p>
	<div class="grid gap-6 lg:grid-cols-2">
		{@render list('mcp-public-h', 'Public', publicTools, 'every client')}
		{@render list('mcp-admin-h', 'Admin', adminTools, 'with an admin token')}
	</div>
</section>
