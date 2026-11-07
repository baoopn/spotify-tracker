<script lang="ts">
	import { untrack } from 'svelte';
	import { LoaderCircle } from '@lucide/svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { ApiError, errorMessage } from '#lib/api.js';
	import { calendarDateIn } from '#lib/format.js';
	import type { TopPlaylistPeriod, TopPlaylistsStatus } from '#lib/types.js';

	let {
		open = $bindable(false),
		status,
		blocked = false,
		ongenerate
	}: {
		open?: boolean;
		status: TopPlaylistsStatus;
		/** Another generator action (Run now / Regenerate) is in flight. */
		blocked?: boolean;
		/** Resolves once generated; rejects with the API error (shown inline). */
		ongenerate: (period: TopPlaylistPeriod, date: string) => Promise<void>;
	} = $props();

	const PERIOD_LABELS: Record<TopPlaylistPeriod, string> = {
		week: 'Week',
		month: 'Month',
		year: 'Year'
	};

	// Mounted only while open (see +page.svelte), so every opening starts fresh.
	let period = $state<TopPlaylistPeriod>(untrack(() => status.periods[0]) ?? 'week');
	let date = $state('');
	let error = $state<string | null>(null);
	let busy = $state(false);

	// "Today" in the generator's configured zone, not the browser's.
	const max = $derived(calendarDateIn(status.time_zone));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (blocked) return;
		if (!date) {
			error = 'Pick a date inside the period.';
			return;
		}
		if (date > max) {
			error = 'That date is in the future.';
			return;
		}
		busy = true;
		error = null;
		try {
			await ongenerate(period, date);
			open = false;
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'period_not_completed'
					? 'That period is still running. Only completed periods can be generated.'
					: errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog
	bind:open
	title="Generate a Top 50 playlist"
	description="Creates or refreshes the playlist of any completed period."
>
	<form id="tpa-generate" class="flex flex-col gap-4" onsubmit={submit} novalidate>
		<div>
			<label class="label" for="tpa-period">Period</label>
			<select id="tpa-period" class="input" bind:value={period} disabled={busy}>
				{#each status.periods as p (p)}
					<option value={p}>{PERIOD_LABELS[p]}</option>
				{/each}
			</select>
		</div>
		<div>
			<label class="label" for="tpa-date">Any date inside the period</label>
			<input
				id="tpa-date"
				class="input"
				type="date"
				{max}
				bind:value={date}
				disabled={busy}
				aria-invalid={!!error}
				aria-describedby={error ? 'tpa-date-hint tpa-date-err' : 'tpa-date-hint'}
			/>
			<p id="tpa-date-hint" class="hint">
				Dates are in {status.time_zone}. Existing playlists are updated in place, never duplicated.
			</p>
			{#if error}<p id="tpa-date-err" class="field-error" role="alert">{error}</p>{/if}
		</div>
	</form>
	{#snippet footer()}
		<button type="button" class="btn btn-secondary" onclick={() => (open = false)} disabled={busy}>
			Cancel
		</button>
		<button type="submit" form="tpa-generate" class="btn btn-primary" disabled={busy || blocked}>
			{#if busy}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{/if}
			Generate
		</button>
	{/snippet}
</Dialog>
