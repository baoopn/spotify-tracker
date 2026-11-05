<script lang="ts">
	import { onMount } from 'svelte';
	import { Check, LoaderCircle, Plus, Save, Send, Trash2, Undo2 } from '@lucide/svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import TagInput from '#lib/components/TagInput.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import { ApiError, api, errorMessage } from '#lib/api.js';
	import { dateTime } from '#lib/format.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { EmailSettings, EmailSettingsUpdate } from '#lib/types.js';

	const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
	const PRESET_DAYS = [30, 14, 7, 3, 1];

	let settings = $state.raw<EmailSettings | null>(null);
	let loadError = $state<string | null>(null);

	// form state
	let host = $state('');
	let port = $state<number | null>(587);
	let username = $state('');
	let password = $state('');
	let clearPassword = $state(false);
	let from = $state('');
	let alertTo = $state<string[]>([]);
	let days = $state<number[]>([]);
	let customDay = $state<number | null>(null);

	let saving = $state(false);
	let errors = $state<Record<string, string>>({});
	let testTo = $state('');
	let testing = $state(false);

	function fill(s: EmailSettings) {
		settings = s;
		host = s.host;
		port = s.port || 587;
		username = s.username;
		password = '';
		clearPassword = false;
		from = s.from;
		alertTo = [...s.alert_to];
		days = [...s.expiry_alert_days].sort((a, b) => b - a);
	}

	async function load() {
		loadError = null;
		try {
			fill(await api.emailSettings());
		} catch (err) {
			loadError = errorMessage(err);
		}
	}

	onMount(() => {
		void load();
	});

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		const anySet = !!(host.trim() || from.trim() || alertTo.length);
		if (anySet && !host.trim()) e.host = 'Required to send email';
		if (port == null || !Number.isInteger(port) || port < 1 || port > 65535)
			e.port = 'Must be a whole number between 1 and 65535';
		if (anySet && !from.trim()) e.from = 'Required to send email';
		else if (from.trim() && !EMAIL_RE.test(from.trim().replace(/^.*<([^>]+)>\s*$/, '$1')))
			e.from = 'Must be an email address (optionally "Name <email>")';
		if (anySet && alertTo.length === 0) e.alert_to = 'Add at least one recipient';
		const bad = alertTo.find((a) => !EMAIL_RE.test(a));
		if (bad) e.alert_to = `${bad} is not a valid email address`;
		if (days.some((d) => !Number.isInteger(d) || d < 1 || d > 365))
			e.expiry_alert_days = 'Days must be between 1 and 365';
		return e;
	}

	/** Map server field keys like `alert_to[1]` onto our form fields. */
	function mapServerFields(fields: Record<string, string>): Record<string, string> {
		const out: Record<string, string> = {};
		for (const [k, v] of Object.entries(fields)) {
			const key = k.replace(/\[\d+\]$/, '').replace(/\.\d+$/, '');
			out[key] = out[key] ? `${out[key]}; ${v}` : v;
		}
		return out;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		errors = validate();
		if (Object.keys(errors).length) {
			toasts.error('Please fix the highlighted fields');
			return;
		}
		const body: EmailSettingsUpdate = {
			host: host.trim(),
			port: port ?? 587,
			username: username.trim(),
			from: from.trim(),
			alert_to: alertTo,
			expiry_alert_days: [...days].sort((a, b) => b - a)
		};
		// Untouched password field = keep the stored one (field omitted).
		if (clearPassword) body.password = '';
		else if (password) body.password = password;

		saving = true;
		try {
			fill(await api.saveEmailSettings(body));
			errors = {};
			toasts.success('Email settings saved');
		} catch (err) {
			if (err instanceof ApiError && err.isValidation) {
				errors = mapServerFields(err.fields);
				toasts.error('The server rejected some fields', err.message);
			} else {
				toasts.error('Could not save settings', errorMessage(err));
			}
		} finally {
			saving = false;
		}
	}

	function toggleDay(d: number) {
		days = days.includes(d) ? days.filter((x) => x !== d) : [...days, d].sort((a, b) => b - a);
	}

	function addCustomDay() {
		const d = customDay;
		if (d == null || !Number.isInteger(d) || d < 1 || d > 365) {
			errors = { ...errors, expiry_alert_days: 'Days must be between 1 and 365' };
			return;
		}
		if (!days.includes(d)) days = [...days, d].sort((a, b) => b - a);
		customDay = null;
		const rest = { ...errors };
		delete rest.expiry_alert_days;
		errors = rest;
	}

	async function sendTest() {
		const to = testTo.trim();
		if (to && !EMAIL_RE.test(to)) {
			toasts.error('Invalid test recipient', `${to} is not a valid email address`);
			return;
		}
		testing = true;
		try {
			const res = await api.testEmail(to || undefined);
			if (res.ok)
				toasts.success('Test email sent', to ? `Sent to ${to}` : 'Sent to the alert recipients');
			else toasts.error('Test email failed', res.error);
		} catch (err) {
			toasts.error('Test email failed', errorMessage(err));
		} finally {
			testing = false;
		}
	}

	const allDays = $derived([...new Set([...PRESET_DAYS, ...days])].sort((a, b) => b - a));
</script>

<PageHeader
	title="Email alerts"
	eyebrow="Settings"
	description="Where token-expiry warnings go, and the SMTP server that sends them."
/>

{#if loadError && !settings}
	<div class="card"><ErrorState message={loadError} onretry={load} /></div>
{:else if !settings}
	<div class="card flex flex-col gap-4 p-6">
		{#each { length: 6 }, i (i)}<Skeleton class="h-10" />{/each}
	</div>
{:else}
	<div class="grid gap-4 lg:grid-cols-3">
		<form class="card flex flex-col gap-5 p-6 lg:col-span-2" onsubmit={save} novalidate>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<h2 class="font-semibold text-fg">SMTP server</h2>
				<div class="flex items-center gap-2 text-xs text-muted">
					{#if settings.enabled}<Badge tone="ok">Alerts enabled</Badge>{:else}<Badge tone="warn"
							>Alerts disabled</Badge
						>{/if}
					{#if settings.updated_at}<span>Updated {dateTime(settings.updated_at)}</span>{/if}
				</div>
			</div>

			<div class="grid gap-4 sm:grid-cols-[1fr_8rem]">
				<div>
					<label class="label" for="smtp-host">Host</label>
					<input
						id="smtp-host"
						class="input"
						bind:value={host}
						placeholder="smtp.example.com"
						autocomplete="off"
						aria-invalid={!!errors.host}
						aria-describedby={errors.host ? 'smtp-host-err' : undefined}
					/>
					{#if errors.host}<p id="smtp-host-err" class="field-error">{errors.host}</p>{/if}
				</div>
				<div>
					<label class="label" for="smtp-port">Port</label>
					<input
						id="smtp-port"
						class="input"
						type="number"
						min="1"
						max="65535"
						inputmode="numeric"
						bind:value={port}
						aria-invalid={!!errors.port}
						aria-describedby={errors.port ? 'smtp-port-err' : undefined}
					/>
					{#if errors.port}<p id="smtp-port-err" class="field-error">{errors.port}</p>{/if}
				</div>
			</div>

			<div class="grid gap-4 sm:grid-cols-2">
				<div>
					<label class="label" for="smtp-user">Username</label>
					<input
						id="smtp-user"
						class="input"
						bind:value={username}
						autocomplete="off"
						aria-invalid={!!errors.username}
					/>
					{#if errors.username}<p class="field-error">{errors.username}</p>{/if}
				</div>
				<div>
					<div class="flex items-center justify-between gap-2">
						<label class="label" for="smtp-pass">Password</label>
						{#if settings.password_set && !clearPassword}
							<button
								type="button"
								class="mb-1 inline-flex items-center gap-1 text-xs font-medium text-danger hover:underline"
								onclick={() => {
									clearPassword = true;
									password = '';
								}}
							>
								<Trash2 class="size-3" aria-hidden="true" /> Clear password
							</button>
						{/if}
					</div>
					{#if clearPassword}
						<div
							class="flex min-h-[2.375rem] items-center justify-between gap-2 rounded-[0.8rem] bg-spot-rose px-3 text-sm text-on-spot"
						>
							Stored password will be removed on save
							<button
								type="button"
								class="inline-flex items-center gap-1 text-xs font-medium hover:underline"
								onclick={() => (clearPassword = false)}
							>
								<Undo2 class="size-3" aria-hidden="true" /> Undo
							</button>
						</div>
					{:else}
						<input
							id="smtp-pass"
							class="input"
							type="password"
							bind:value={password}
							autocomplete="new-password"
							placeholder={settings.password_set ? '••••••••  (stored)' : 'Not set'}
							aria-describedby="smtp-pass-hint"
							aria-invalid={!!errors.password}
						/>
						<p id="smtp-pass-hint" class="hint">
							{settings.password_set
								? 'Leave blank to keep the stored password.'
								: 'Optional, for servers that require auth.'}
						</p>
					{/if}
					{#if errors.password}<p class="field-error">{errors.password}</p>{/if}
				</div>
			</div>

			<div>
				<label class="label" for="smtp-from">From</label>
				<input
					id="smtp-from"
					class="input"
					bind:value={from}
					placeholder="Spotify Tracker <alerts@example.com>"
					aria-invalid={!!errors.from}
					aria-describedby={errors.from ? 'smtp-from-err' : undefined}
				/>
				{#if errors.from}<p id="smtp-from-err" class="field-error">{errors.from}</p>{/if}
			</div>

			<div>
				<label class="label" for="smtp-to">Alert recipients</label>
				<TagInput
					id="smtp-to"
					bind:values={alertTo}
					placeholder="name@example.com, press Enter"
					invalid={!!errors.alert_to}
					describedby="smtp-to-hint"
					validate={(v) => (EMAIL_RE.test(v) ? null : `${v} is not a valid email address`)}
				/>
				<p id="smtp-to-hint" class="hint">Separate multiple addresses with Enter or commas.</p>
				{#if errors.alert_to}<p class="field-error">{errors.alert_to}</p>{/if}
			</div>

			<fieldset>
				<legend class="label">Token expiry alerts</legend>
				<p class="hint mt-0 mb-2">Send a warning when the Spotify token has this many days left.</p>
				<div class="flex flex-wrap items-center gap-2">
					{#each allDays as d (d)}
						{@const on = days.includes(d)}
						<button
							type="button"
							aria-pressed={on}
							class={[
								'inline-flex items-center gap-1 rounded-full border px-3 py-1 text-sm font-medium transition-colors',
								on
									? 'border-transparent bg-spot-select text-on-spot'
									: 'border-border-strong text-muted hover:text-fg'
							]}
							onclick={() => toggleDay(d)}
						>
							{#if on}<Check class="size-3.5" aria-hidden="true" />{/if}
							{d}
							{d === 1 ? 'day' : 'days'}
						</button>
					{/each}
					<span class="flex items-center gap-1">
						<input
							class="input w-20"
							type="number"
							min="1"
							max="365"
							placeholder="Days"
							aria-label="Custom number of days"
							bind:value={customDay}
							onkeydown={(e) => {
								if (e.key === 'Enter') {
									e.preventDefault();
									addCustomDay();
								}
							}}
						/>
						<button
							type="button"
							class="btn btn-secondary btn-sm btn-icon"
							aria-label="Add custom day"
							onclick={addCustomDay}
						>
							<Plus class="size-4" aria-hidden="true" />
						</button>
					</span>
				</div>
				{#if errors.expiry_alert_days}<p class="field-error">{errors.expiry_alert_days}</p>{/if}
			</fieldset>

			<div class="flex justify-end gap-2 border-t border-border pt-4">
				<button
					type="button"
					class="btn btn-ghost"
					onclick={() => settings && fill(settings)}
					disabled={saving}
				>
					Reset
				</button>
				<button type="submit" class="btn btn-primary" disabled={saving}>
					{#if saving}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Save
							class="size-4"
							aria-hidden="true"
						/>{/if}
					Save settings
				</button>
			</div>
		</form>

		<section class="card flex flex-col gap-3 self-start p-6" aria-labelledby="test-h">
			<h2 id="test-h" class="font-semibold text-fg">Send a test email</h2>
			<p class="text-sm text-muted">
				Uses the saved settings. Leave the recipient empty to send to all alert recipients.
			</p>
			<div>
				<label class="label" for="test-to">Recipient (optional)</label>
				<input
					id="test-to"
					class="input"
					type="email"
					bind:value={testTo}
					placeholder={settings.alert_to[0] ?? 'name@example.com'}
				/>
			</div>
			<button type="button" class="btn btn-secondary" onclick={sendTest} disabled={testing}>
				{#if testing}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Send
						class="size-4"
						aria-hidden="true"
					/>{/if}
				Send test email
			</button>
		</section>
	</div>
{/if}
