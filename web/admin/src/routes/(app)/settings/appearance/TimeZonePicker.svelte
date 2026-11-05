<script lang="ts">
	import { onMount } from 'svelte';
	import { Check, LoaderCircle, RotateCcw } from '@lucide/svelte';
	import Badge from '#lib/components/Badge.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { secondClock } from '#lib/clock.svelte.js';
	import { intlOffsetMinutes, isValidTimeZone, offsetLabel, timeOfDay } from '#lib/time.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { ServerZone } from '#lib/types.js';

	const inputId = $props.id();

	// All zones this browser knows, plus zones whose rules changed recently.
	const zones = (() => {
		let list: string[];
		try {
			list = Intl.supportedValuesOf('timeZone');
		} catch {
			list = [];
		}
		return [...new Set([...list, 'America/Edmonton', 'UTC'])].sort();
	})();

	let draft = $state('');
	let error = $state<string | null>(null);
	let preview = $state.raw<ServerZone | null>(null);
	let previewError = $state<string | null>(null);
	let previewing = $state(false);
	let ctrl: AbortController | null = null;

	onMount(() => {
		draft = zone.override ?? '';
		void lookup(zone.iana);
		return () => ctrl?.abort();
	});

	/** Ask the server for its current offset of `tz` (shown next to the picker). */
	async function lookup(tz: string | null) {
		ctrl?.abort();
		preview = null;
		previewError = null;
		if (!tz) return;
		const c = new AbortController();
		ctrl = c;
		previewing = true;
		try {
			const res = await api.timezone(tz, { signal: c.signal });
			if (!c.signal.aborted) preview = res.data;
		} catch (err) {
			if (!c.signal.aborted) previewError = errorMessage(err);
		} finally {
			if (ctrl === c) previewing = false;
		}
	}

	function apply(e: SubmitEvent) {
		e.preventDefault();
		const tz = draft.trim();
		if (!tz || !isValidTimeZone(tz)) {
			error = `“${tz || '(empty)'}” is not a time zone this browser knows.`;
			return;
		}
		error = null;
		zone.setOverride(tz);
		void lookup(tz);
	}

	function reset() {
		draft = '';
		error = null;
		zone.setOverride(null);
		void lookup(zone.iana);
	}

	const browserOffset = $derived(
		preview ? intlOffsetMinutes(preview.tz, new Date(secondClock.now)) : null
	);
</script>

<div class="flex flex-col gap-5">
	<dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[10rem_1fr]">
		<dt class="eyebrow pt-0.5">In use</dt>
		<dd class="flex flex-wrap items-center gap-2 text-ink">
			<span class="font-mono">{zone.iana ?? 'Unknown (UTC)'}</span>
			{#if zone.override}<Badge tone="accent">Override</Badge>{:else}<Badge tone="plain"
					>Automatic</Badge
				>{/if}
			{#if zone.corrected}<Badge tone="warn">Corrected {zone.spec.label}</Badge>{/if}
		</dd>
		<dt class="eyebrow pt-0.5">Detected</dt>
		<dd class="font-mono text-ink-soft">{zone.detected ?? 'unknown (the browser did not say)'}</dd>
		<dt class="eyebrow pt-0.5">Local time</dt>
		<dd class="font-mono text-ink">{timeOfDay(new Date(secondClock.now))}</dd>
	</dl>

	<form class="flex flex-col gap-2" onsubmit={apply} novalidate>
		<label class="label" for={inputId}>Time zone</label>
		<div class="flex flex-wrap items-start gap-2">
			<input
				id={inputId}
				class="input max-w-sm flex-1"
				list="{inputId}-zones"
				placeholder="Search, e.g. America/Edmonton"
				autocomplete="off"
				spellcheck="false"
				bind:value={draft}
				aria-invalid={!!error}
				aria-describedby="{inputId}-hint"
			/>
			<datalist id="{inputId}-zones">
				{#each zones as z (z)}<option value={z}></option>{/each}
			</datalist>
			<button type="submit" class="btn btn-primary">
				<Check class="size-4" aria-hidden="true" /> Use this zone
			</button>
			<button type="button" class="btn btn-secondary" onclick={reset} disabled={!zone.override}>
				<RotateCcw class="size-4" aria-hidden="true" /> Reset to automatic
			</button>
		</div>
		<p id="{inputId}-hint" class="hint">
			Automatic uses the zone your browser reports. Pick one if it reports none or the wrong one.
		</p>
		{#if error}<p class="field-error" role="alert">{error}</p>{/if}
	</form>

	<div class="border-t border-rule pt-4 text-sm" aria-live="polite">
		<p class="eyebrow mb-1">Server's current offset</p>
		{#if previewing}
			<p class="flex items-center gap-2 text-ink-soft">
				<LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Checking…
			</p>
		{:else if preview}
			<p class="font-mono text-ink">
				{preview.tz}: UTC{preview.offset} ({preview.abbreviation ||
					offsetLabel(preview.offset_minutes)})
				<span class="text-ink-soft">· source {preview.source}</span>
			</p>
			{#if browserOffset != null && browserOffset !== preview.offset_minutes}
				<p class="mt-1 text-ink-soft">
					This browser thinks it is {offsetLabel(browserOffset)}; times use the server's {offsetLabel(
						preview.offset_minutes
					)}.
				</p>
			{/if}
		{:else if previewError}
			<p class="text-ink-soft">
				Server check unavailable ({previewError}); using the browser's own data.
			</p>
		{:else}
			<p class="text-ink-soft">No zone selected: times are shown in UTC.</p>
		{/if}
	</div>
</div>
