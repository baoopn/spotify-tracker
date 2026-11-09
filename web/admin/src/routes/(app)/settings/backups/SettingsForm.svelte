<script lang="ts">
	import { untrack } from 'svelte';
	import { LoaderCircle, RotateCcw, Save } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import Badge from '#lib/components/Badge.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import { ApiError, api, errorMessage } from '#lib/api.js';
	import { HHMM_RE, utcTimeLocal } from '#lib/backups.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { qk } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import type { BackupSettings, BackupSettingsUpdate, BackupStatus } from '#lib/types.js';

	let { settings }: { settings: BackupSettings } = $props();

	const queryClient = useQueryClient();

	// Filled once from the first load: the status polls, and a poll must never
	// overwrite what is being typed (Reset re-reads the latest values).
	const initial = untrack(() => settings);
	let time = $state(initial.time);
	let keepDays = $state<number | null>(initial.keep_days);
	let keepMin = $state<number | null>(initial.keep_min);
	let paused = $state(initial.paused);
	let errors = $state<Record<string, string>>({});

	function fill(s: BackupSettings) {
		time = s.time;
		keepDays = s.keep_days;
		keepMin = s.keep_min;
		paused = s.paused;
		errors = {};
	}

	const localTime = $derived(utcTimeLocal(time.trim(), relativeClock.now));

	function validate(): Record<string, string> {
		const e: Record<string, string> = {};
		if (!HHMM_RE.test(time.trim())) e.time = 'Use 24-hour HH:MM, e.g. 03:00';
		if (keepDays == null || !Number.isInteger(keepDays) || keepDays < 0 || keepDays > 3650)
			e.keep_days = 'Must be a whole number between 0 and 3650';
		if (keepMin == null || !Number.isInteger(keepMin) || keepMin < 1 || keepMin > 1000)
			e.keep_min = 'Must be a whole number between 1 and 1000';
		return e;
	}

	type Saved = { data?: BackupSettings; note?: string } | undefined;

	/** Put the server's answer into the cache and the form (PUT and DELETE share it). */
	async function applySaved(res: Saved, title: string) {
		toasts.success(title, res?.note ?? 'The backup service applies them within a minute.');
		let saved = res?.data;
		if (saved && typeof saved.time === 'string') {
			const next = saved;
			queryClient.setQueryData<BackupStatus>(qk.backups, (old) =>
				old ? { ...old, settings: next } : old
			);
			void queryClient.invalidateQueries({ queryKey: qk.backups });
		} else {
			// Unexpected body: re-read the status and fill from that.
			await queryClient.refetchQueries({ queryKey: qk.backups });
			saved = queryClient.getQueryData<BackupStatus>(qk.backups)?.settings;
		}
		if (saved) fill(saved);
	}

	const save = createMutation(() => ({
		mutationFn: (body: BackupSettingsUpdate) => api.saveBackupSettings(body),
		onSuccess: (res) => applySaved(res, 'Backup settings saved'),
		onError: (err) => {
			if (err instanceof ApiError && err.isValidation) {
				errors = { ...err.fields };
				toasts.error('The server rejected some fields', err.message);
			} else {
				toasts.error('Could not save settings', errorMessage(err));
			}
		}
	}));

	let confirmDefaults = $state(false);
	const reset = createMutation(() => ({
		mutationFn: () => api.resetBackupSettings(),
		onSuccess: (res) => applySaved(res, 'Back to default settings'),
		onError: (err) => toasts.error('Could not reset settings', errorMessage(err))
	}));

	async function useDefaults() {
		try {
			await reset.mutateAsync();
		} catch {
			// toast shown in onError; close the dialog anyway
		}
	}

	const saving = $derived(save.isPending || reset.isPending);

	function submit(e: SubmitEvent) {
		e.preventDefault();
		errors = validate();
		if (Object.keys(errors).length) {
			toasts.error('Please fix the highlighted fields');
			return;
		}
		save.mutate({
			time: time.trim(),
			keep_days: keepDays ?? 0,
			keep_min: keepMin ?? 1,
			paused
		});
	}
</script>

<form class="card flex flex-col gap-5 p-6" onsubmit={submit} novalidate aria-labelledby="bk-set-h">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h2 id="bk-set-h" class="font-semibold text-fg">Schedule and retention</h2>
		{#if settings.source === 'admin'}<Badge tone="accent">Custom</Badge>{:else}<Badge tone="neutral"
				>Defaults</Badge
			>{/if}
	</div>

	<div>
		<label class="label" for="bk-time">Daily backup time (UTC)</label>
		<input
			id="bk-time"
			class="input w-32 font-mono"
			bind:value={time}
			inputmode="numeric"
			placeholder="03:00"
			maxlength="5"
			autocomplete="off"
			aria-invalid={!!errors.time}
			aria-describedby={errors.time ? 'bk-time-err' : 'bk-time-hint'}
		/>
		{#if errors.time}
			<p id="bk-time-err" class="field-error">{errors.time}</p>
		{:else}
			<p id="bk-time-hint" class="hint">
				{localTime ? `= ${localTime} your time` : '24-hour HH:MM'}
			</p>
		{/if}
		<p class="hint">
			A new time applies from its next occurrence, so the next backup may come up to a day later.
		</p>
	</div>

	<div class="grid gap-4 sm:grid-cols-2">
		<div>
			<label class="label" for="bk-days">Keep days</label>
			<input
				id="bk-days"
				class="input"
				type="number"
				min="0"
				max="3650"
				inputmode="numeric"
				bind:value={keepDays}
				aria-invalid={!!errors.keep_days}
				aria-describedby={errors.keep_days ? 'bk-days-err' : 'bk-days-hint'}
			/>
			{#if errors.keep_days}
				<p id="bk-days-err" class="field-error">{errors.keep_days}</p>
			{:else}
				<p id="bk-days-hint" class="hint">Older backups are deleted. 0 keeps all.</p>
			{/if}
		</div>
		<div>
			<label class="label" for="bk-min">Keep at least</label>
			<input
				id="bk-min"
				class="input"
				type="number"
				min="1"
				max="1000"
				inputmode="numeric"
				bind:value={keepMin}
				aria-invalid={!!errors.keep_min}
				aria-describedby={errors.keep_min ? 'bk-min-err' : 'bk-min-hint'}
			/>
			{#if errors.keep_min}
				<p id="bk-min-err" class="field-error">{errors.keep_min}</p>
			{:else}
				<p id="bk-min-hint" class="hint">Newest backups kept whatever their age.</p>
			{/if}
		</div>
	</div>

	<div>
		<button
			type="button"
			role="switch"
			aria-checked={paused}
			aria-describedby="bk-paused-hint"
			class="inline-flex min-h-9 items-center gap-2 rounded-lg text-sm text-ink-soft hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
			onclick={() => (paused = !paused)}
		>
			<span
				class={[
					'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-ink transition-colors duration-150',
					paused ? 'bg-spot-sky' : 'bg-paper-4'
				]}
				aria-hidden="true"
			>
				<span
					class={[
						'size-3.5 rounded-full bg-ink transition-transform duration-150',
						paused ? 'translate-x-4.5' : 'translate-x-0.5'
					]}
				></span>
			</span>
			Pause scheduled backups
		</button>
		<p id="bk-paused-hint" class="hint">"Back up now" still works while paused.</p>
		{#if errors.paused}<p class="field-error">{errors.paused}</p>{/if}
	</div>

	<div class="flex flex-wrap justify-end gap-2 border-t border-border pt-4">
		<button
			type="button"
			class="btn btn-secondary mr-auto"
			onclick={() => (confirmDefaults = true)}
			disabled={saving || settings.source !== 'admin'}
			title={settings.source === 'admin' ? undefined : 'Already using the defaults'}
		>
			{#if reset.isPending}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<RotateCcw class="size-4" aria-hidden="true" />{/if}
			Use defaults
		</button>
		<button type="button" class="btn btn-ghost" onclick={() => fill(settings)} disabled={saving}>
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

<ConfirmDialog
	bind:open={confirmDefaults}
	title="Use default backup settings?"
	message="This removes the time, retention and pause set here. The backup service goes back to its environment or built-in values within a minute."
	confirmLabel="Use defaults"
	onconfirm={useDefaults}
/>
