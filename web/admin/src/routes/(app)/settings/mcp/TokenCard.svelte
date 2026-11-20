<script lang="ts">
	import { Info, KeyRound, LoaderCircle, RotateCw, Trash2, TriangleAlert } from '@lucide/svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { dateTime } from '#lib/format.js';
	import type { McpToken } from '#lib/mcp.js';

	let {
		token,
		newToken,
		envTokenConfigured,
		generating,
		revoking,
		ongenerate,
		onrevoke
	}: {
		token: McpToken | null;
		/** The token just generated (shown once), else null. */
		newToken: string | null;
		envTokenConfigured: boolean;
		generating: boolean;
		/** A revoke request is in flight. */
		revoking: boolean;
		ongenerate: () => Promise<void>;
		onrevoke: () => Promise<void>;
	} = $props();

	let confirmRotate = $state(false);
	let confirmRevoke = $state(false);
	const busy = $derived(generating || revoking);

	function generate() {
		if (token) confirmRotate = true;
		else void ongenerate();
	}
</script>

<section class="card flex flex-col gap-4 p-6" aria-labelledby="mcp-token-h">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h2 id="mcp-token-h" class="font-semibold text-fg">Access token</h2>
		<div class="flex flex-wrap gap-2">
			{#if token}
				<button
					type="button"
					class="btn btn-secondary btn-sm"
					onclick={() => (confirmRevoke = true)}
					disabled={busy}
				>
					<Trash2 class="size-4" aria-hidden="true" /> Revoke
				</button>
			{/if}
			<button type="button" class="btn btn-primary btn-sm" onclick={generate} disabled={busy}>
				{#if generating}<LoaderCircle
						class="size-4 animate-spin"
						aria-hidden="true"
					/>{:else if token}<RotateCw class="size-4" aria-hidden="true" />{:else}<KeyRound
						class="size-4"
						aria-hidden="true"
					/>{/if}
				{token ? 'Rotate token' : 'Generate token'}
			</button>
		</div>
	</div>

	<p class="text-sm text-muted">
		Send it as <code class="font-mono">Authorization: Bearer &lt;token&gt;</code> to get the admin tools.
		Only its hash is stored.
	</p>

	{#if newToken}
		<div
			class="flex flex-col gap-2 rounded-xl bg-spot-butter p-3 text-sm text-on-spot"
			role="status"
		>
			<p class="flex gap-2">
				<TriangleAlert class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
				<span>Copy this token now. It will not be shown again.</span>
			</p>
			<div class="flex flex-wrap items-center gap-2">
				<input
					class="input min-w-0 flex-1 font-mono text-xs"
					readonly
					value={newToken}
					aria-label="New MCP access token"
					onfocus={(e) => e.currentTarget.select()}
				/>
				<CopyButton value={newToken} label="Copy access token" />
			</div>
		</div>
	{/if}

	{#if token}
		<dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[10rem_1fr]">
			<dt class="text-muted">Token</dt>
			<dd><code class="font-mono text-fg">{token.prefix}…</code></dd>
			<dt class="text-muted">Created</dt>
			<dd class="text-fg">{dateTime(token.created_at)}</dd>
			<dt class="text-muted">Last used</dt>
			<dd class="text-fg">
				{#if token.last_used_at}<RelTime value={token.last_used_at} showAbsolute />{:else}<span
						class="text-muted">Never</span
					>{/if}
			</dd>
		</dl>
	{:else}
		<p class="text-sm text-fg">No token. Without one, clients get the public tools only.</p>
	{/if}

	{#if envTokenConfigured}
		<p class="flex gap-2 rounded-xl bg-spot-sky p-3 text-sm text-on-spot">
			<Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span>
				The token in <code class="font-mono">MCP_ADMIN_TOKEN</code> also works as an admin token. Remove
				it from the environment to rely on this one alone.
			</span>
		</p>
	{/if}
</section>

<ConfirmDialog
	bind:open={confirmRotate}
	title="Rotate the access token?"
	message="A new token replaces the current one. Clients using the old token get 401 until you update them."
	confirmLabel="Rotate token"
	onconfirm={ongenerate}
/>
<ConfirmDialog
	bind:open={confirmRevoke}
	title="Revoke the access token?"
	message="Clients using it get 401. Clients without a token keep the public tools."
	confirmLabel="Revoke token"
	danger
	onconfirm={onrevoke}
/>
