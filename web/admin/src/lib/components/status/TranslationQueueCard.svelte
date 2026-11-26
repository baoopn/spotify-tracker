<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import RelTime from '../RelTime.svelte';
	import Skeleton from '../Skeleton.svelte';
	import { compact, num, usd } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import { capRatio, estimateCost, hasCap } from '#lib/translation.js';

	// The Claude lyrics translation queue: songs are queued when they play,
	// within a daily and a monthly cap of Claude calls.
	const query = createQuery(() => queries.translationQueue());
	const s = $derived(query.data);
	const busy = $derived(!!s && s.enabled && (s.queued > 0 || s.in_progress > 0));
	const failed = $derived(s ? s.error + s.given_up + s.refused : 0);
	const cost = $derived(s ? estimateCost(s.model, s.tokens_month) : null);
	// Joined in JS: separators in markup lose their spaces around {#if} blocks.
	const SEP = ' · ';
	const results = $derived(
		s
			? [
					`${compact(s.done)} done`,
					s.skipped ? `${compact(s.skipped)} skipped` : '',
					`${compact(failed)} failed`
				]
					.filter(Boolean)
					.join(SEP)
			: ''
	);
	const failedDetail = $derived(
		s && failed
			? [
					s.error ? `${compact(s.error)} retrying` : '',
					s.given_up ? `${compact(s.given_up)} given up` : '',
					s.refused ? `${compact(s.refused)} refused` : ''
				]
					.filter(Boolean)
					.join(SEP)
			: ''
	);
	const waiting = $derived(
		s
			? [
					s.in_progress ? `${compact(s.in_progress)} translating now` : '',
					`${compact(s.queued)} queued`
				]
					.filter(Boolean)
					.join(SEP)
			: ''
	);
	const meters = $derived(
		s
			? [
					{ id: 'tq-today', label: 'Calls today (UTC)', used: s.calls_today, cap: s.daily_cap },
					{ id: 'tq-month', label: 'This month', used: s.calls_month, cap: s.monthly_cap }
				]
			: []
	);
</script>

<section class="flex flex-col gap-3 border-t border-ink pt-4" aria-labelledby="tq-title">
	<div class="flex items-center justify-between gap-2">
		<h2 id="tq-title" class="eyebrow">Translations</h2>
		{#if s && !s.enabled}
			<span class="chip">Off</span>
		{:else if busy && s}
			<span class="chip" role="status">{compact(s.queued + s.in_progress)} to translate</span>
		{/if}
	</div>

	{#if s}
		<div class="text-sm">
			<p class="text-fg">{results}</p>
			{#if failedDetail}
				<p class="text-xs text-muted">Failed: {failedDetail}</p>
			{/if}
			<p class="text-xs text-muted">{waiting}</p>
		</div>

		<div class="flex flex-col gap-2.5">
			{#each meters as m (m.id)}
				{@const capped = hasCap(m.cap)}
				{@const ratio = capRatio(m.used, m.cap)}
				{@const full = capped && m.used >= m.cap}
				<div>
					<div class="flex items-baseline justify-between gap-2 text-xs">
						<span id="{m.id}-label" class="text-muted">{m.label}</span>
						<span class="font-mono text-fg tabular-nums">
							{#if capped}{num(m.used)} / {num(m.cap)}{#if full}<span class="text-danger">
										· cap reached</span
									>{/if}{:else}{num(m.used)} · no cap{/if}
						</span>
					</div>
					{#if capped}
						<div
							class="mt-1 h-1.5 overflow-hidden rounded-full bg-surface-3"
							role="meter"
							aria-labelledby="{m.id}-label"
							aria-valuemin={0}
							aria-valuemax={m.cap}
							aria-valuenow={Math.min(m.used, m.cap)}
							aria-valuetext="{num(m.used)} of {num(m.cap)} calls{full ? ', cap reached' : ''}"
						>
							<div
								class={[
									'h-full rounded-full',
									full ? 'bg-danger' : ratio >= 0.8 ? 'bg-warn' : 'bg-ink'
								]}
								style:width="{ratio * 100}%"
							></div>
						</div>
					{/if}
				</div>
			{/each}
		</div>

		<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
			<dt class="text-muted">Tokens this month</dt>
			<dd class="text-right font-mono text-fg tabular-nums">
				{compact(s.tokens_month.input)} in · {compact(s.tokens_month.output)} out
			</dd>
			{#if cost != null}
				<dt class="text-muted">Est. cost this month (at most)</dt>
				<dd class="text-right font-mono text-fg tabular-nums" title={s.model}>{usd(cost)}</dd>
			{/if}
		</dl>
		{#if cost != null}
			<p class="-mt-2 text-xs text-muted">Cached input counted at full price.</p>
		{/if}

		{#if s.model || s.next_retry_at}
			<p class="text-xs text-muted">
				{#if s.model}Model <span class="font-mono">{s.model}</span
					>{/if}{#if s.next_retry_at}{s.model ? SEP : ''}Next retry <RelTime
						value={s.next_retry_at}
					/>{/if}
			</p>
		{/if}
		{#if !s.enabled}
			<p class="text-xs text-muted">The translator is off (TRANSLATE_ENABLED=false).</p>
		{/if}
	{:else if query.isError}
		<p class="text-sm text-muted">The translation queue is unavailable.</p>
	{:else}
		<Skeleton class="h-4 w-2/3" /><Skeleton class="h-3 w-1/2" /><Skeleton class="h-1.5 w-full" />
	{/if}
</section>
