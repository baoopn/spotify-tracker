<script lang="ts">
	import Badge from '#lib/components/Badge.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import type { McpSettings } from '#lib/mcp.js';

	let {
		settings,
		saving,
		ontoggle
	}: {
		settings: McpSettings;
		saving: boolean;
		ontoggle: (enabled: boolean) => void;
	} = $props();

	const on = $derived(settings.admin_tools_enabled);
</script>

<section class="card flex flex-col gap-4 p-6" aria-labelledby="mcp-status-h">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h2 id="mcp-status-h" class="font-semibold text-fg">Status</h2>
		{#if settings.enabled}<Badge tone="ok">Enabled</Badge>{:else}<Badge tone="danger"
				>Disabled</Badge
			>{/if}
	</div>

	<div>
		<p class="label">Endpoint</p>
		<div class="flex flex-wrap items-center gap-2">
			<code
				class="min-w-0 flex-1 basis-48 rounded-xl bg-surface-2 px-3 py-2 font-mono text-xs break-all text-fg"
				>{settings.endpoint_url}</code
			>
			<CopyButton value={settings.endpoint_url} label="Copy endpoint URL" />
		</div>
		{#if !settings.enabled}
			<p class="hint">
				Set <code class="font-mono">MCP_ENABLED=true</code> to serve this endpoint.
			</p>
		{/if}
	</div>

	<div>
		<p class="label">Rate limit</p>
		<p class="text-sm text-fg">
			{settings.rate_limit_per_minute > 0
				? `${settings.rate_limit_per_minute} requests per minute per IP`
				: 'Off'}
		</p>
	</div>

	<div>
		<button
			type="button"
			role="switch"
			aria-checked={on}
			aria-describedby="mcp-admin-hint"
			disabled={saving}
			class="inline-flex min-h-9 items-center gap-2 rounded-lg text-sm text-ink-soft hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-60"
			onclick={() => ontoggle(!on)}
		>
			<span
				class={[
					'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-ink transition-colors duration-150',
					on ? 'bg-spot-sky' : 'bg-paper-4'
				]}
				aria-hidden="true"
			>
				<span
					class={[
						'size-3.5 rounded-full bg-ink transition-transform duration-150',
						on ? 'translate-x-4.5' : 'translate-x-0.5'
					]}
				></span>
			</span>
			Admin tools
		</button>
		<p id="mcp-admin-hint" class="hint">
			{on
				? 'Clients with an admin token also get the read-only admin tools.'
				: 'Off: clients get the public tools only, and a wrong token is still rejected. If the database is unreachable, the MCP_ADMIN_TOKEN env token gets the admin tools anyway.'}
		</p>
	</div>
</section>
