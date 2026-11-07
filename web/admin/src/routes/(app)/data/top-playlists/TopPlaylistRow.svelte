<script lang="ts" module>
	import type { Tone } from '#lib/components/Badge.svelte';
	import type { AdminTopPlaylist, TopPlaylistStatus } from '#lib/types.js';

	export const STATUS_UI: Record<TopPlaylistStatus, { tone: Tone; label: string }> = {
		created: { tone: 'ok', label: 'Created' },
		pending: { tone: 'info', label: 'Pending' },
		empty: { tone: 'neutral', label: 'Empty' },
		waiting_auth: { tone: 'warn', label: 'Waiting for auth' },
		failed: { tone: 'danger', label: 'Failed' }
	};

	/** Only real Spotify playlist URLs are linked; anything else is omitted. */
	export function spotifyHref(url: string | null): string | null {
		return url?.startsWith('https://open.spotify.com/') ? url : null;
	}

	/** created / empty rows are regenerated; the rest are retried. */
	export function isRetry(row: AdminTopPlaylist): boolean {
		return row.status !== 'created' && row.status !== 'empty';
	}
</script>

<script lang="ts">
	import { ChevronDown, ExternalLink, LoaderCircle, RefreshCw, RotateCcw } from '@lucide/svelte';
	import Badge from '#lib/components/Badge.svelte';
	import OutLink from '#lib/components/OutLink.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import TopPlaylistTracks from './TopPlaylistTracks.svelte';
	import { dateTime, num } from '#lib/format.js';

	let {
		row,
		expanded,
		busy,
		toggling,
		disabled,
		regenerateBlocked = null,
		ontoggle,
		onregenerate,
		onsethidden
	}: {
		row: AdminTopPlaylist;
		expanded: boolean;
		/** A regenerate / retry of this row is in flight. */
		busy: boolean;
		/** A visibility change of this row is in flight. */
		toggling: boolean;
		/** Feature disabled server-side: every mutation would answer 503. */
		disabled: boolean;
		/** Why Regenerate / Retry is unavailable right now (shown as its tooltip), or null. */
		regenerateBlocked?: string | null;
		ontoggle: () => void;
		onregenerate: () => void;
		onsethidden: (hidden: boolean) => void;
	} = $props();

	const detailsId = $props.id();
	const ui = $derived(STATUS_UI[row.status]);
	const retry = $derived(isRetry(row));
	const zone = $derived({ timeZone: row.time_zone });
	const href = $derived(spotifyHref(row.spotify_url));
	// A failed regenerate of a published row keeps it published and only records the error.
	const staleError = $derived(row.status === 'created' && !!row.last_error);
</script>

<li class="flex flex-col gap-3 px-4 py-4 sm:px-5">
	<div class="grid grid-cols-1 items-center gap-x-6 gap-y-3 md:grid-cols-[minmax(0,1fr)_auto_auto]">
		<div class="min-w-0">
			<p class="eyebrow mb-1">{row.period}</p>
			<p class="truncate font-semibold text-fg" title={row.label}>{row.label}</p>
			<p class="truncate text-xs text-muted" title={row.name}>{row.name}</p>
			<div class="mt-2 flex flex-wrap items-center gap-1.5">
				<Badge tone={ui.tone}>{ui.label}</Badge>
				{#if staleError}<Badge tone="warn" dot={false} title={row.last_error ?? undefined}
						>Last update failed</Badge
					>{/if}
				{#if row.status === 'created' && row.hidden}<Badge tone="plain" dot={false}>Hidden</Badge
					>{/if}
			</div>
		</div>

		<div class="flex flex-wrap gap-x-6 gap-y-1 text-sm md:flex-col md:text-right">
			<span class="tabular-nums">
				<span class="font-medium text-fg">{num(row.track_count)}</span>
				<span class="text-muted">{row.track_count === 1 ? 'track' : 'tracks'} ·</span>
				<span class="font-medium text-fg">{num(row.total_plays)}</span>
				<span class="text-muted">{row.total_plays === 1 ? 'play' : 'plays'}</span>
			</span>
			<span class="text-muted">
				{#if row.published_at}Published <RelTime value={row.published_at} />{:else}Not published{/if}
			</span>
		</div>

		<div class="flex flex-wrap items-center gap-2 md:justify-end">
			{#if row.status === 'created'}
				<button
					type="button"
					role="switch"
					aria-checked={!row.hidden}
					class="inline-flex min-h-9 items-center gap-2 rounded-lg px-2 text-sm text-ink-soft hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-50"
					onclick={() => onsethidden(!row.hidden)}
					disabled={disabled || toggling}
				>
					<span
						class={[
							'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-ink transition-colors duration-150',
							row.hidden ? 'bg-paper-4' : 'bg-spot-mint'
						]}
						aria-hidden="true"
					>
						<span
							class={[
								'size-3.5 rounded-full bg-ink transition-transform duration-150',
								row.hidden ? 'translate-x-0.5' : 'translate-x-4.5'
							]}
						></span>
					</span>
					Shown on public page
				</button>
			{/if}
			{#if href}
				<OutLink {href} class="btn btn-ghost btn-sm" label="Open {row.label} in Spotify">
					<ExternalLink class="size-4" aria-hidden="true" /> Open in Spotify
				</OutLink>
			{/if}
			<button
				type="button"
				class="btn btn-secondary btn-sm"
				onclick={onregenerate}
				disabled={disabled || busy || !!regenerateBlocked}
				title={regenerateBlocked ?? undefined}
				aria-label="{retry ? 'Retry' : 'Regenerate'} {row.label}"
			>
				{#if busy}
					<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
				{:else if retry}
					<RotateCcw class="size-4" aria-hidden="true" />
				{:else}
					<RefreshCw class="size-4" aria-hidden="true" />
				{/if}
				{retry ? 'Retry' : 'Regenerate'}
			</button>
			<button
				type="button"
				class="btn btn-ghost btn-icon btn-sm"
				aria-expanded={expanded}
				aria-controls={detailsId}
				aria-label="{expanded ? 'Hide' : 'Show'} details for {row.label}"
				onclick={ontoggle}
			>
				<ChevronDown
					class={['size-4 transition-transform duration-150', expanded && 'rotate-180']}
					aria-hidden="true"
				/>
			</button>
		</div>
	</div>

	{#if expanded}
		<div id={detailsId} class="grid gap-5 rounded-xl bg-surface-2 p-4 lg:grid-cols-[20rem_1fr]">
			<dl class="grid content-start gap-x-4 gap-y-2 text-sm sm:grid-cols-[8rem_1fr]">
				<dt class="text-muted">Status</dt>
				<dd class="text-fg">{ui.label}</dd>
				<dt class="text-muted">Last error</dt>
				<dd class={row.last_error ? 'font-mono text-xs break-words text-danger' : 'text-fg'}>
					{row.last_error ?? 'None'}
				</dd>
				<dt class="text-muted">Attempts</dt>
				<dd class="text-fg tabular-nums">{num(row.attempts)}</dd>
				<dt class="text-muted">Next attempt</dt>
				<dd class="text-fg"><RelTime value={row.next_attempt_at} /></dd>
				<dt class="text-muted">Period</dt>
				<dd class="text-fg">
					{dateTime(row.period_start, zone)} → {dateTime(row.period_end, zone)}
				</dd>
				<dt class="text-muted">Time zone</dt>
				<dd class="font-mono text-xs text-fg">{row.time_zone}</dd>
				<dt class="text-muted">Spotify ID</dt>
				<dd class="font-mono text-xs break-all text-fg">{row.spotify_id ?? '–'}</dd>
				<dt class="text-muted">Updated</dt>
				<dd class="text-fg"><RelTime value={row.updated_at} /></dd>
				<dt class="text-muted">Visibility</dt>
				<dd class="text-fg">
					{row.hidden ? 'Hidden from the public page' : 'Shown on the public page once created'}
				</dd>
			</dl>
			<TopPlaylistTracks period={row.period} start={row.period_start} />
		</div>
	{/if}
</li>
