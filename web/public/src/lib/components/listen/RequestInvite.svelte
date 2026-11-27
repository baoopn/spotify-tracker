<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { ListenApiError, postListenRequest } from '#lib/listen/api.js';
	import { errorMessage } from '#lib/api.js';
	import { listenAuth } from '#lib/listen/auth.svelte.js';
	import { loadTurnstile, turnstileTheme, type TurnstileApi } from '#lib/account/turnstile.js';

	/** Prefill (the signed-in email in the "not invited" state). */
	let { email: initialEmail = '' }: { email?: string } = $props();

	const MESSAGE_MAX = 500;
	const formId = $props.id();

	let open = $state(false);
	let sent = $state(false);
	let busy = $state(false);
	let name = $state('');
	// Seeded once from the prop; the visitor owns it after that.
	let email = $state('');
	let message = $state('');
	/** Honeypot: people never see or fill it. */
	let website = $state('');
	let fields = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);

	const left = $derived(MESSAGE_MAX - [...message].length);

	function show() {
		if (!email) email = initialEmail;
		open = true;
	}

	// The account menu links here as /listen#request: open the form at once.
	let anchor = $state<HTMLDivElement | null>(null);
	onMount(async () => {
		if (window.location.hash !== '#request') return;
		show();
		await tick();
		anchor?.scrollIntoView({ block: 'start' });
		anchor?.querySelector<HTMLInputElement>('input')?.focus();
	});

	// Cloudflare Turnstile, when the server has a site key: Send waits for a token.
	const siteKey = $derived(listenAuth.turnstileSiteKey);
	let token = $state<string | null>(null);
	let checkError = $state<string | null>(null);
	let widget: { api: TurnstileApi; id: string } | null = null;

	function turnstile(key: string) {
		return (node: HTMLDivElement) => {
			let gone = false;
			loadTurnstile().then(
				(api) => {
					if (gone) return;
					const id = api.render(node, {
						sitekey: key,
						theme: turnstileTheme(document.documentElement.dataset.theme),
						size: 'flexible',
						action: 'listen-request',
						callback: (t) => {
							token = t;
							checkError = null;
						},
						'expired-callback': () => resetCheck(),
						'error-callback': () => (token = null),
						'timeout-callback': () => resetCheck()
					});
					if (id) widget = { api, id };
				},
				(err: unknown) => (checkError = errorMessage(err))
			);
			return () => {
				gone = true;
				if (widget) widget.api.remove(widget.id);
				widget = null;
				token = null;
			};
		};
	}

	/** A token is single use: a fresh check after each submit or expiry. */
	function resetCheck() {
		token = null;
		if (widget) widget.api.reset(widget.id);
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		const errs: Record<string, string> = {};
		if (!name.trim()) errs.name = 'Enter your name.';
		if (!email.trim()) errs.email = 'Enter your email.';
		if (left < 0) errs.message = `Keep it under ${MESSAGE_MAX} characters.`;
		fields = errs;
		formError = null;
		if (Object.keys(errs).length > 0) return;
		busy = true;
		try {
			await postListenRequest({
				name: name.trim(),
				email: email.trim(),
				message: message.trim() || undefined,
				website,
				turnstile_token: siteKey ? (token ?? '') : undefined
			});
			sent = true;
		} catch (err) {
			if (err instanceof ListenApiError && err.status === 422) {
				fields = err.fields;
				if (Object.keys(err.fields).length === 0) formError = err.message;
			} else if (err instanceof ListenApiError && err.status === 429) {
				formError = 'Too many requests. Try again later.';
			} else {
				formError = errorMessage(err);
			}
		} finally {
			busy = false;
			if (siteKey && !sent) resetCheck();
		}
	}
</script>

<div id="request" bind:this={anchor} class="scroll-mt-6">
	{#if sent}
		<p class="chip label mt-6 [--chip:var(--spot-mint)]" role="status">
			<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
			Request sent
		</p>
		<p class="mt-3 max-w-md text-ink-soft" role="status">
			Thanks! If Bao adds you, an invite will arrive by email.
		</p>
	{:else if !open}
		<div class="mt-6">
			<p class="text-sm text-ink-soft">Not invited yet?</p>
			<button type="button" class="btn btn-secondary mt-2" onclick={show}>Request an invite</button>
		</div>
	{:else}
		<form
			class="print mt-6 max-w-md rounded-brand bg-paper-2 p-5"
			onsubmit={submit}
			novalidate
			aria-labelledby="{formId}-h"
		>
			<h3 id="{formId}-h" class="font-display text-xl font-semibold tracking-tight">
				Request an invite
			</h3>
			<p class="mt-1 text-sm text-ink-soft">
				Tell Bao who you are (how you know each other, for example), so you can be added.
			</p>

			<div class="mt-4 flex flex-col gap-4">
				<div>
					<label class="label text-ink-soft" for="{formId}-name">Name</label>
					<input
						id="{formId}-name"
						class="field"
						bind:value={name}
						maxlength="80"
						autocomplete="name"
						required
						aria-invalid={!!fields.name}
						aria-describedby={fields.name ? `${formId}-name-err` : undefined}
					/>
					{#if fields.name}<p id="{formId}-name-err" class="err">{fields.name}</p>{/if}
				</div>
				<div>
					<label class="label text-ink-soft" for="{formId}-email">Email</label>
					<input
						id="{formId}-email"
						class="field"
						type="email"
						bind:value={email}
						maxlength="254"
						autocomplete="email"
						spellcheck="false"
						required
						aria-invalid={!!fields.email}
						aria-describedby={fields.email ? `${formId}-email-err` : undefined}
					/>
					{#if fields.email}<p id="{formId}-email-err" class="err">{fields.email}</p>{/if}
				</div>
				<div>
					<div class="flex items-baseline justify-between gap-3">
						<label class="label text-ink-soft" for="{formId}-msg">Message (optional)</label>
						<span
							id="{formId}-msg-count"
							class={['font-mono text-xs tabular-nums', left < 0 ? 'err-text' : 'text-ink-soft']}
							>{left}</span
						>
					</div>
					<textarea
						id="{formId}-msg"
						class="field min-h-24 resize-y"
						bind:value={message}
						rows="3"
						aria-invalid={!!fields.message}
						aria-describedby="{formId}-msg-count{fields.message ? ` ${formId}-msg-err` : ''}"
					></textarea>
					{#if fields.message}<p id="{formId}-msg-err" class="err">{fields.message}</p>{/if}
				</div>
				<!-- Honeypot for bots: hidden from people and screen readers. -->
				<div class="hp" aria-hidden="true">
					<label for="{formId}-website">Website</label>
					<input
						id="{formId}-website"
						name="website"
						tabindex="-1"
						autocomplete="off"
						bind:value={website}
					/>
				</div>
			</div>

			{#if siteKey}
				<div class="mt-4">
					<div {@attach turnstile(siteKey)} class="min-h-[65px]"></div>
					{#if fields.turnstile || checkError}
						<p class="err" role="alert">{fields.turnstile ?? checkError}</p>
					{/if}
				</div>
			{/if}

			{#if formError}<p class="err mt-4" role="alert">{formError}</p>{/if}

			<div class="mt-5 flex flex-wrap gap-3">
				<button type="submit" class="btn btn-primary" disabled={busy || (!!siteKey && !token)}>
					{busy ? 'Sending…' : 'Send request'}
				</button>
				<button
					type="button"
					class="btn btn-secondary"
					onclick={() => (open = false)}
					disabled={busy}
				>
					Cancel
				</button>
			</div>
		</form>
	{/if}
</div>

<style>
	.field {
		display: block;
		width: 100%;
		margin-top: 0.375rem;
		min-height: 2.75rem;
		padding: 0.55rem 0.75rem;
		border: 1px solid var(--ink);
		border-radius: 8px;
		background: var(--paper);
		color: var(--ink);
		font-size: 1rem;
	}
	.field[aria-invalid='true'] {
		border-color: var(--danger);
	}
	.err-text {
		color: var(--danger);
	}
	.err {
		margin-top: 0.375rem;
		font-size: 0.875rem;
		color: var(--danger);
	}
	.hp {
		position: absolute;
		left: -10000px;
		width: 1px;
		height: 1px;
		overflow: hidden;
	}
</style>
