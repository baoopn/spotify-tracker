<script lang="ts">
	import { Inbox, LoaderCircle, UserRoundPlus, X } from '@lucide/svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import Badge from '#lib/components/Badge.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import { errorMessage } from '#lib/api.js';
	import { countryFlag, countryName } from '#lib/country.js';
	import { listenersKey, requestsApi, requestsQuery, type JoinRequest } from '#lib/listeners.js';
	import { toasts } from '#lib/toast.svelte.js';

	/** Pending requests, from the listeners list (decides whether the card shows). */
	let { pending }: { pending: number } = $props();

	const queryClient = useQueryClient();
	let showDismissed = $state(false);
	let expanded = $state<Record<number, boolean>>({});
	let confirmOpen = $state(false);
	let target = $state<JoinRequest | null>(null);

	const visible = $derived(pending > 0 || showDismissed);
	const pendingList = createQuery(() => ({ ...requestsQuery('pending'), enabled: visible }));
	const dismissedList = createQuery(() => ({
		...requestsQuery('dismissed'),
		enabled: showDismissed
	}));

	const refresh = () => queryClient.invalidateQueries({ queryKey: listenersKey });

	const invite = createMutation(() => ({
		mutationFn: (id: number) => requestsApi.invite(id),
		onSuccess: (res) =>
			toasts.success(
				res.invitation_sent ? 'Invitation sent' : 'Access added',
				res.invitation_sent
					? `${res.data.email} gets an email with a sign-up link.`
					: `${res.data.email} already has an account. They can sign in at /listen.`
			),
		onError: (err) => toasts.error('Could not invite', errorMessage(err)),
		onSettled: refresh
	}));

	const dismiss = createMutation(() => ({
		mutationFn: (id: number) => requestsApi.dismiss(id),
		onSuccess: (res) => toasts.success('Request dismissed', res.request.email),
		onError: (err) => toasts.error('Could not dismiss', errorMessage(err)),
		onSettled: refresh
	}));

	function askDismiss(r: JoinRequest) {
		target = r;
		confirmOpen = true;
	}

	// ConfirmDialog awaits this; errors are already toasted by the mutation.
	async function onDismiss() {
		if (target) await dismiss.mutateAsync(target.id).catch(() => {});
	}
</script>

{#snippet row(r: JoinRequest)}
	{@const flag = countryFlag(r.country)}
	<li class="flex flex-col gap-2 border-b border-border px-6 py-4 last:border-b-0">
		<div class="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
			<div class="min-w-0 flex-1 basis-56">
				<p class="truncate font-medium text-fg" title={r.name}>{r.name}</p>
				<p class="truncate text-sm text-muted" title={r.email}>{r.email}</p>
				<p class="mt-0.5 text-xs text-muted">
					{#if flag}<span aria-hidden="true">{flag}</span>{/if}
					{countryName(r.country)} · <RelTime value={r.created_at} />
				</p>
			</div>
			{#if r.status === 'pending'}
				<div class="flex flex-wrap gap-2">
					<button
						type="button"
						class="btn btn-primary btn-sm"
						onclick={() => invite.mutate(r.id)}
						disabled={invite.isPending}
						aria-label="Invite {r.email}"
					>
						{#if invite.isPending && invite.variables === r.id}<LoaderCircle
								class="size-4 animate-spin"
								aria-hidden="true"
							/>{:else}<UserRoundPlus class="size-4" aria-hidden="true" />{/if}
						Invite
					</button>
					<button
						type="button"
						class="btn btn-secondary btn-sm"
						onclick={() => askDismiss(r)}
						disabled={dismiss.isPending}
						aria-label="Dismiss the request from {r.email}"
					>
						<X class="size-4" aria-hidden="true" /> Dismiss
					</button>
				</div>
			{:else}
				<Badge tone="plain">{r.status === 'invited' ? 'Invited' : 'Dismissed'}</Badge>
			{/if}
		</div>
		{#if r.already_listener}
			<p class="text-xs text-muted">Already a listener</p>
		{/if}
		{#if r.message}
			<div class="text-sm text-fg">
				<p class={['break-words whitespace-pre-line', !expanded[r.id] && 'line-clamp-2']}>
					{r.message}
				</p>
				{#if r.message.length > 140 || r.message.includes('\n')}
					<button
						type="button"
						class="mt-1 text-xs font-medium text-muted underline-offset-2 hover:text-fg hover:underline"
						aria-expanded={!!expanded[r.id]}
						onclick={() => (expanded[r.id] = !expanded[r.id])}
					>
						{expanded[r.id] ? 'Show less' : 'Show more'}
					</button>
				{/if}
			</div>
		{/if}
	</li>
{/snippet}

{#if visible}
	<section class="card overflow-hidden" aria-labelledby="requests-h">
		<div class="flex flex-wrap items-center justify-between gap-2 px-6 pt-6 pb-4">
			<h2 id="requests-h" class="flex items-center gap-2 font-semibold text-fg">
				<Inbox class="size-4" aria-hidden="true" /> Requests
				{#if pending > 0}<Badge tone="warn">{pending} waiting</Badge>{/if}
			</h2>
			<label class="flex items-center gap-2 text-sm text-muted">
				<input type="checkbox" bind:checked={showDismissed} />
				Show dismissed
			</label>
		</div>

		{#if pendingList.isPending}
			<div class="flex flex-col gap-3 px-6 pb-6" aria-busy="true">
				{#each { length: 2 }, i (i)}<Skeleton class="h-14" />{/each}
			</div>
		{:else if pendingList.isError && !pendingList.data}
			<ErrorState message={errorMessage(pendingList.error)} onretry={() => pendingList.refetch()} />
		{:else if (pendingList.data?.data.length ?? 0) === 0}
			<p class="border-t border-border px-6 py-4 text-sm text-muted">No requests waiting.</p>
		{:else}
			<ul class="border-t border-border">
				{#each pendingList.data?.data ?? [] as r (r.id)}{@render row(r)}{/each}
			</ul>
		{/if}

		{#if showDismissed}
			<h3 class="eyebrow border-t border-border px-6 pt-4 pb-2">Dismissed</h3>
			{#if dismissedList.isPending}
				<div class="px-6 pb-6"><Skeleton class="h-14" /></div>
			{:else if dismissedList.isError && !dismissedList.data}
				<ErrorState
					title="Could not load dismissed requests"
					message={errorMessage(dismissedList.error)}
					onretry={() => dismissedList.refetch()}
				/>
			{:else if (dismissedList.data?.data.length ?? 0) === 0}
				<p class="px-6 pb-4 text-sm text-muted">No dismissed requests.</p>
			{:else}
				<ul>
					{#each dismissedList.data?.data ?? [] as r (r.id)}{@render row(r)}{/each}
				</ul>
			{/if}
		{/if}
	</section>
{:else}
	<button
		type="button"
		class="text-sm text-muted underline-offset-2 hover:text-fg hover:underline"
		onclick={() => (showDismissed = true)}
	>
		Show dismissed requests
	</button>
{/if}

<ConfirmDialog
	bind:open={confirmOpen}
	title="Dismiss this request?"
	message={target ? `${target.name} (${target.email}) is not told. They can ask again later.` : ''}
	confirmLabel="Dismiss"
	danger
	onconfirm={onDismiss}
/>
