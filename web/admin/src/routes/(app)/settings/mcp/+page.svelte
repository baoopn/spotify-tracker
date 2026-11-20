<script lang="ts">
	import { RefreshCw } from '@lucide/svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import StatusCard from './StatusCard.svelte';
	import TokenCard from './TokenCard.svelte';
	import UsageCard from './UsageCard.svelte';
	import ToolsCard from './ToolsCard.svelte';

	const queryClient = useQueryClient();
	const query = createQuery(() => queries.mcp());
	const manual = new ManualRefetch(() => query.refetch());
	const s = $derived(query.data);

	/** The token from the last generate call; it lives only in this page's memory. */
	let newToken = $state<string | null>(null);

	const toggle = createMutation(() => ({
		mutationFn: (enabled: boolean) => api.saveMcpSettings({ admin_tools_enabled: enabled }),
		onSuccess: (saved) => {
			queryClient.setQueryData(qk.mcp, saved);
			toasts.success(saved.admin_tools_enabled ? 'Admin tools enabled' : 'Admin tools disabled');
		},
		onError: (err) => toasts.error('Could not save the setting', errorMessage(err))
	}));

	const generate = createMutation(() => ({
		mutationFn: () => api.generateMcpToken(),
		onSuccess: (res) => {
			newToken = res.token;
			toasts.success('Access token generated', 'Copy it now: it is shown only once.');
		},
		onError: (err) => toasts.error('Could not generate a token', errorMessage(err)),
		onSettled: () => queryClient.invalidateQueries({ queryKey: qk.mcp })
	}));

	const revoke = createMutation(() => ({
		mutationFn: () => api.revokeMcpToken(),
		onSuccess: (saved) => {
			newToken = null;
			queryClient.setQueryData(qk.mcp, saved);
			toasts.success('Access token revoked');
		},
		onError: (err) => toasts.error('Could not revoke the token', errorMessage(err))
	}));

	// ConfirmDialog awaits these; errors are already toasted by the mutations.
	async function onGenerate() {
		await generate.mutateAsync().catch(() => {});
	}
	async function onRevoke() {
		await revoke.mutateAsync().catch(() => {});
	}
</script>

<PageHeader
	title="MCP"
	eyebrow="Settings"
	description="The remote MCP endpoint that lets Claude read your listening data."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-ghost"
			onclick={() => manual.run()}
			disabled={manual.active}
		>
			<RefreshCw class={['size-4', manual.active && 'animate-spin']} aria-hidden="true" /> Refresh
		</button>
	{/snippet}
</PageHeader>

{#if query.isError && !s}
	<div class="card">
		<ErrorState message={errorMessage(query.error)} onretry={() => manual.run()} />
	</div>
{:else if !s}
	<div class="card flex flex-col gap-3 p-6">
		{#each { length: 6 }, i (i)}<Skeleton class="h-6" />{/each}
	</div>
{:else}
	<div class="grid gap-4 lg:grid-cols-2">
		<StatusCard settings={s} saving={toggle.isPending} ontoggle={(on) => toggle.mutate(on)} />
		<TokenCard
			token={s.token}
			{newToken}
			envTokenConfigured={s.env_token_configured}
			generating={generate.isPending}
			revoking={revoke.isPending}
			ongenerate={onGenerate}
			onrevoke={onRevoke}
		/>
	</div>
	<div class="mt-4 flex flex-col gap-4">
		<UsageCard endpoint={s.endpoint_url} token={newToken} />
		<ToolsCard
			publicTools={s.public_tools}
			adminTools={s.admin_tools}
			adminEnabled={s.admin_tools_enabled}
		/>
	</div>
{/if}
