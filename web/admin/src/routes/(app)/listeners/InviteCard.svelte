<script lang="ts">
	import { LoaderCircle, UserRoundPlus } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import { ApiError, errorMessage } from '#lib/api.js';
	import { listenersApi, listenersKey } from '#lib/listeners.js';
	import { toasts } from '#lib/toast.svelte.js';

	let { listenUrl }: { listenUrl: string } = $props();

	const queryClient = useQueryClient();
	let email = $state('');
	let fieldError = $state<string | null>(null);

	const invite = createMutation(() => ({
		mutationFn: (address: string) => listenersApi.invite(address),
		onSuccess: (res) => {
			email = '';
			fieldError = null;
			toasts.success(
				res.invitation_sent ? 'Invitation sent' : 'Access added',
				res.invitation_sent
					? `${res.data.email} gets an email with a sign-up link.`
					: `${res.data.email} already has an account. They can sign in at /listen.`
			);
		},
		onError: (err) => {
			if (err instanceof ApiError && (err.status === 422 || err.status === 409)) {
				fieldError = err.fields.email ?? err.message;
			} else {
				toasts.error('Could not send the invitation', errorMessage(err));
			}
		},
		onSettled: () => queryClient.invalidateQueries({ queryKey: listenersKey })
	}));

	function submit(e: SubmitEvent) {
		e.preventDefault();
		const address = email.trim();
		if (!address) {
			fieldError = 'Enter an email address.';
			return;
		}
		fieldError = null;
		invite.mutate(address);
	}
</script>

<section class="card flex flex-col gap-4 p-6" aria-labelledby="invite-h">
	<h2 id="invite-h" class="font-semibold text-fg">Invite someone</h2>
	<p class="text-sm text-muted">
		They get an email with a link to make an account. Then they can listen along at the page below.
	</p>

	<form class="flex flex-col gap-2" onsubmit={submit} novalidate>
		<label class="label" for="invite-email">Email</label>
		<div class="flex flex-wrap items-start gap-2">
			<input
				id="invite-email"
				class="input min-w-0 flex-1 basis-56"
				type="email"
				autocomplete="off"
				spellcheck="false"
				placeholder="friend@example.com"
				bind:value={email}
				oninput={() => (fieldError = null)}
				aria-invalid={!!fieldError}
				aria-describedby={fieldError ? 'invite-email-err' : undefined}
				disabled={invite.isPending}
			/>
			<button type="submit" class="btn btn-primary" disabled={invite.isPending}>
				{#if invite.isPending}<LoaderCircle
						class="size-4 animate-spin"
						aria-hidden="true"
					/>{:else}<UserRoundPlus class="size-4" aria-hidden="true" />{/if}
				Invite
			</button>
		</div>
		{#if fieldError}<p id="invite-email-err" class="field-error">{fieldError}</p>{/if}
	</form>

	<div>
		<p class="label">Listen page</p>
		<div class="flex flex-wrap items-center gap-2">
			<code
				class="min-w-0 flex-1 basis-48 rounded-xl bg-surface-2 px-3 py-2 font-mono text-xs break-all text-fg"
				>{listenUrl}</code
			>
			<CopyButton value={listenUrl} label="Copy listen page URL" />
		</div>
	</div>
</section>
