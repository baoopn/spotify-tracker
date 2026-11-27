<script lang="ts">
	import { LoaderCircle, Send, UserRoundX } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import Badge, { type Tone } from '#lib/components/Badge.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { errorMessage } from '#lib/api.js';
	import {
		canResend,
		listenersApi,
		listenersKey,
		type Listener,
		type ListenerStatus
	} from '#lib/listeners.js';
	import { toasts } from '#lib/toast.svelte.js';

	let { listeners }: { listeners: Listener[] } = $props();

	const queryClient = useQueryClient();
	const tones: Record<ListenerStatus, Tone> = { active: 'ok', invited: 'warn', revoked: 'plain' };
	const labels: Record<ListenerStatus, string> = {
		active: 'Active',
		invited: 'Invited',
		revoked: 'Revoked'
	};

	let confirmOpen = $state(false);
	let target = $state<Listener | null>(null);

	const resend = createMutation(() => ({
		mutationFn: (id: number) => listenersApi.resend(id),
		onSuccess: (res) => toasts.success('Invitation sent again', res.data.email),
		onError: (err) => toasts.error('Could not resend', errorMessage(err)),
		onSettled: () => queryClient.invalidateQueries({ queryKey: listenersKey })
	}));

	const revoke = createMutation(() => ({
		mutationFn: (id: number) => listenersApi.revoke(id),
		onSuccess: (res) => {
			if (res.clerk_cleanup === 'failed') {
				toasts.warning(
					'Access revoked',
					`${res.data.email} can no longer listen, but cleaning up in Clerk failed. Check the user in Clerk.`
				);
			} else {
				toasts.success('Access revoked', res.data.email);
			}
		},
		onError: (err) => toasts.error('Could not revoke', errorMessage(err)),
		onSettled: () => queryClient.invalidateQueries({ queryKey: listenersKey })
	}));

	function askRevoke(l: Listener) {
		target = l;
		confirmOpen = true;
	}

	// ConfirmDialog awaits this; errors are already toasted by the mutation.
	async function onRevoke() {
		if (target) await revoke.mutateAsync(target.id).catch(() => {});
	}
</script>

<section class="card overflow-hidden" aria-labelledby="listeners-h">
	<div class="flex flex-wrap items-center justify-between gap-2 px-6 pt-6 pb-4">
		<h2 id="listeners-h" class="font-semibold text-fg">People</h2>
		<p class="text-sm text-muted">{listeners.length} total</p>
	</div>

	{#if listeners.length === 0}
		<EmptyState title="No one yet" message="Invite someone to listen along with you." />
	{:else}
		<div class="overflow-x-auto">
			<table class="data-table">
				<caption class="sr-only">Listeners</caption>
				<thead>
					<tr>
						<th scope="col">Email</th>
						<th scope="col" class="w-28">Status</th>
						<th scope="col" class="w-40">Invited</th>
						<th scope="col" class="w-40">Last seen</th>
						<th scope="col" class="w-56"><span class="sr-only">Actions</span></th>
					</tr>
				</thead>
				<tbody>
					{#each listeners as l (l.id)}
						<tr class={l.status === 'revoked' ? 'text-muted' : undefined}>
							<td class="max-w-72">
								<span class="block truncate font-medium" title={l.email}>{l.email}</span>
								{#if l.has_account && l.status === 'invited'}
									<span class="text-xs text-muted">Has an account, not signed in yet</span>
								{:else if l.invited_by}
									<span class="block truncate text-xs text-muted" title={l.invited_by}
										>By {l.invited_by}</span
									>
								{/if}
							</td>
							<td><Badge tone={tones[l.status]}>{labels[l.status]}</Badge></td>
							<td><RelTime value={l.invited_at} /></td>
							<td>
								{#if l.status === 'revoked'}
									<span class="text-xs">Revoked</span> <RelTime value={l.revoked_at} />
								{:else if l.last_seen_at}
									<RelTime value={l.last_seen_at} />
								{:else}
									<span class="text-muted">Never</span>
								{/if}
							</td>
							<td>
								{#if l.status !== 'revoked'}
									<div class="flex flex-wrap justify-end gap-2">
										{#if canResend(l)}
											<button
												type="button"
												class="btn btn-secondary btn-sm"
												onclick={() => resend.mutate(l.id)}
												disabled={resend.isPending}
												aria-label="Resend the invitation to {l.email}"
											>
												{#if resend.isPending && resend.variables === l.id}<LoaderCircle
														class="size-4 animate-spin"
														aria-hidden="true"
													/>{:else}<Send class="size-4" aria-hidden="true" />{/if}
												Resend
											</button>
										{/if}
										<button
											type="button"
											class="btn btn-secondary btn-sm"
											onclick={() => askRevoke(l)}
											disabled={revoke.isPending}
											aria-label="Revoke access for {l.email}"
										>
											<UserRoundX class="size-4" aria-hidden="true" /> Revoke
										</button>
									</div>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

<ConfirmDialog
	bind:open={confirmOpen}
	title="Revoke access?"
	message={target
		? `${target.email} stops hearing audio within a minute and can't sign in to listen again. You can invite them again later.`
		: ''}
	confirmLabel="Revoke access"
	danger
	onconfirm={onRevoke}
/>
