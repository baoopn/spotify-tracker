<script lang="ts">
	import {
		CalendarPlus,
		KeyRound,
		ListOrdered,
		LoaderCircle,
		Play,
		ShieldCheck,
		TriangleAlert
	} from '@lucide/svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { resolve } from '$app/paths';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import Segmented from '#lib/components/Segmented.svelte';
	import Pagination from '#lib/components/Pagination.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import GenerateDialog from './GenerateDialog.svelte';
	import TopPlaylistRow, { ACTION_UI, rowAction } from './TopPlaylistRow.svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { ApiError, api, errorMessage } from '#lib/api.js';
	import { num } from '#lib/format.js';
	import { qk, queries } from '#lib/queries.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';
	import type { AdminTopPlaylist, TopPlaylistPeriod, TopPlaylistsList } from '#lib/types.js';

	const LIMIT = 25;

	type Filter = 'all' | TopPlaylistPeriod;
	const FILTERS: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'week', label: 'Week' },
		{ value: 'month', label: 'Month' },
		{ value: 'year', label: 'Year' }
	];

	let filter = $state<Filter>('all');
	let page = $state(0);
	let generateOpen = $state(false);
	let confirmOpen = $state(false);
	let confirmTarget = $state<AdminTopPlaylist | null>(null);
	const expanded = new SvelteSet<string>();

	const queryClient = useQueryClient();
	const list = createQuery(() =>
		queries.topPlaylists({ period: filter === 'all' ? '' : filter, page, limit: LIMIT })
	);
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => list.refetch());

	const status = $derived(list.data?.status);
	const enabled = $derived(status?.enabled ?? false);
	// Spotify must have granted playlist-modify-public before anything is generated.
	const unauthorized = $derived(!!status && status.enabled && !status.authorized);
	const AUTH_HINT = 'Re-authorize Spotify first';

	const keyOf = (r: Pick<AdminTopPlaylist, 'period' | 'period_start'>) =>
		`${r.period}|${r.period_start}`;

	/** Patch the row into every cached list page, then refetch the family. */
	function applyRow(updated: AdminTopPlaylist) {
		queryClient.setQueriesData<TopPlaylistsList>(
			{ queryKey: qk.topPlaylists.all, predicate: (q) => q.queryKey[1] !== 'detail' },
			(old) =>
				old && {
					...old,
					data: old.data.map((r) => (keyOf(r) === keyOf(updated) ? updated : r))
				}
		);
		return invalidate();
	}

	function invalidate() {
		return queryClient.invalidateQueries({ queryKey: qk.topPlaylists.all });
	}

	/** A finished generate / regenerate: say what the stored row ended up as. */
	function reportOutcome(r: AdminTopPlaylist, verb: string) {
		switch (r.status) {
			case 'created':
				toasts.success(
					`${r.label} ${verb}`,
					`${num(r.track_count)} tracks, ${num(r.total_plays)} plays.`
				);
				break;
			case 'empty':
				toasts.info(`${r.label}: no plays`, 'Nothing was played in this period.');
				break;
			default:
				toasts.warning(`${r.label}: ${r.status.replace('_', ' ')}`, r.last_error ?? undefined);
		}
	}

	function reportError(title: string, err: unknown) {
		if (err instanceof ApiError && err.code === 'no_plays') {
			// Regenerate of a published playlist whose period now ranks nothing: left unchanged.
			toasts.info('Playlist left unchanged', err.message);
		} else if (err instanceof ApiError && err.code === 'pruned') {
			toasts.info('Playlist was pruned', 'Only the newest playlists per kind are kept.');
		} else if (err instanceof ApiError && err.code === 'busy') {
			toasts.warning('Generator is busy', 'Another run is in progress. Try again in a moment.');
		} else {
			toasts.error(title, errorMessage(err));
		}
	}

	const run = createMutation(() => ({
		mutationFn: () => api.runTopPlaylists(),
		onSuccess: () => toasts.success('Generator ran', 'Due periods were processed.'),
		onError: (err) => reportError('Run failed', err),
		// Spotify failures are stored on the rows too, so refetch either way.
		onSettled: () => invalidate()
	}));

	const verify = createMutation(() => ({
		mutationFn: () => api.verifyTopPlaylists(),
		onSuccess: () =>
			toasts.success('Checked against Spotify', 'Missing playlists, if any, were restored.'),
		onError: (err) => reportError('Check failed', err),
		onSettled: () => invalidate()
	}));

	const generate = createMutation(() => ({
		mutationFn: (v: { period: TopPlaylistPeriod; date: string }) =>
			api.generateTopPlaylist(v.period, v.date),
		onSuccess: (r) => {
			reportOutcome(r, 'generated');
			return applyRow(r);
		},
		onError: () => invalidate()
	}));

	async function onGenerate(period: TopPlaylistPeriod, date: string) {
		// Errors propagate to the dialog, which shows them inline.
		await generate.mutateAsync({ period, date });
	}

	const regenerate = createMutation(() => ({
		mutationFn: (r: AdminTopPlaylist) => api.regenerateTopPlaylist(r.period, r.period_start),
		onMutate: (r) => ({ action: rowAction(r) }),
		onSuccess: (r, _prev, ctx) => {
			reportOutcome(r, ACTION_UI[ctx.action].done);
			return applyRow(r);
		},
		onError: (err, r, ctx) => {
			const label = ACTION_UI[ctx?.action ?? rowAction(r)].verb.toLowerCase();
			reportError(`Could not ${label} ${r.label}`, err);
			return invalidate();
		}
	}));
	const regeneratingKey = $derived(
		regenerate.isPending && regenerate.variables ? keyOf(regenerate.variables) : null
	);

	/** Run / check / generate / regenerate share the generator lock: one at a time. */
	const generatorBusy = $derived(
		run.isPending || verify.isPending || generate.isPending || regenerate.isPending
	);

	function askRegenerate(r: AdminTopPlaylist) {
		confirmTarget = r;
		confirmOpen = true;
	}

	async function confirmRegenerate() {
		if (!confirmTarget) return;
		try {
			await regenerate.mutateAsync(confirmTarget);
		} catch {
			// already reported by onError
		}
	}

	const setHidden = createMutation(() => ({
		mutationFn: (v: { row: AdminTopPlaylist; hidden: boolean }) =>
			api.setTopPlaylistHidden(v.row.period, v.row.period_start, v.hidden),
		onSuccess: (r) => {
			toasts.success(
				r.hidden ? 'Hidden from the public page' : 'Shown on the public page',
				r.label
			);
			return applyRow(r);
		},
		onError: (err) => toasts.error('Could not change visibility', errorMessage(err))
	}));
	const togglingKey = $derived(
		setHidden.isPending && setHidden.variables ? keyOf(setHidden.variables.row) : null
	);

	function toggleExpanded(key: string) {
		if (expanded.has(key)) expanded.delete(key);
		else expanded.add(key);
	}

	const confirmUi = $derived(confirmTarget ? ACTION_UI[rowAction(confirmTarget)] : null);

	/** "1 h", "90 min": an interval in whole minutes or hours. */
	function interval(seconds: number): string {
		const min = Math.max(1, Math.round(seconds / 60));
		return min % 60 === 0 ? `${num(min / 60)} h` : `${num(min)} min`;
	}
</script>

<PageHeader
	title="Top 50 playlists"
	eyebrow="Data"
	description="Auto-generated playlists of the 50 most-played tracks of each week, month and year."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary"
			onclick={() => (generateOpen = true)}
			disabled={!enabled ||
				!status?.periods.length ||
				unauthorized ||
				run.isPending ||
				verify.isPending}
			title={unauthorized ? AUTH_HINT : undefined}
		>
			<CalendarPlus class="size-4" aria-hidden="true" /> Generate…
		</button>
		<button
			type="button"
			class="btn btn-secondary"
			onclick={() => verify.mutate()}
			disabled={!enabled || generatorBusy || unauthorized}
			title={unauthorized ? AUTH_HINT : 'Check published playlists against Spotify now'}
		>
			{#if verify.isPending}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<ShieldCheck class="size-4" aria-hidden="true" />{/if}
			Check now
		</button>
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => run.mutate()}
			disabled={!enabled || generatorBusy || unauthorized}
			title={unauthorized ? AUTH_HINT : undefined}
		>
			{#if run.isPending}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Play
					class="size-4"
					aria-hidden="true"
				/>{/if}
			Run now
		</button>
	{/snippet}
</PageHeader>

{#if status}
	<section class="card mb-4 flex flex-col gap-3 p-4 sm:px-5" aria-label="Generator status">
		<div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
			{#if status.enabled}
				<Badge tone="ok">Enabled</Badge>
				<span class="text-fg">
					{status.periods.map((p) => p[0].toUpperCase() + p.slice(1)).join(' · ')}
				</span>
				<span class="text-muted">
					Zone <span class="font-mono text-xs text-fg">{status.time_zone}</span>
				</span>
				<span class="text-muted">
					Checks every {num(Math.round(status.interval_seconds / 60))} min
				</span>
				{#if status.live_periods?.length}
					<span class="inline-flex flex-wrap items-center gap-x-2 gap-y-1 text-muted">
						<Badge tone="info" dot={false}>Live</Badge>
						<span class="text-fg">
							{status.live_periods.map((p) => p[0].toUpperCase() + p.slice(1)).join(' · ')}
						</span>
						<span>refreshed every {interval(status.live_refresh_seconds)}</span>
					</span>
				{/if}
				<span class="text-muted">
					{status.keep > 0 ? `Keeps the newest ${num(status.keep)} per kind` : 'Keeps all'}
				</span>
				<span class="text-muted">
					{status.verify_seconds > 0
						? `Checked against Spotify every ${interval(status.verify_seconds)}`
						: 'Spotify check off'}
				</span>
				{#if status.owner && status.owner_source !== 'none'}
					<span class="text-muted">
						Named <span class="text-fg">“{status.owner}'s Top 50 · …”</span
						>{#if status.owner_source === 'spotify'}&nbsp;(from Spotify profile){/if}
					</span>
				{/if}
			{:else}
				<Badge tone="neutral">Disabled</Badge>
				<span class="text-muted">
					The generator is turned off in the server config; existing rows are read-only.
				</span>
			{/if}
		</div>
		{#if status.enabled && !status.authorized}
			<p
				class="flex flex-wrap items-center gap-2 rounded-xl bg-spot-butter px-3 py-2 text-sm text-on-spot"
				role="status"
			>
				<TriangleAlert class="size-4 shrink-0" aria-hidden="true" />
				<span class="flex-1">Re-authorize Spotify to grant playlist-modify-public.</span>
				<a href={resolve('settings/spotify')} class="btn btn-secondary btn-sm">
					<KeyRound class="size-4" aria-hidden="true" /> Spotify settings
				</a>
			</p>
		{/if}
	</section>
{/if}

<div class="mb-4 flex flex-wrap items-center gap-3">
	<Segmented
		options={FILTERS}
		bind:value={filter}
		label="Filter by period"
		onchange={() => {
			page = 0;
			expanded.clear();
		}}
	/>
</div>

<div class="card overflow-hidden">
	{#if list.isError && !list.data}
		<ErrorState message={errorMessage(list.error)} onretry={() => manual.run()} />
	{:else if !list.data}
		<div class="flex flex-col gap-4 p-4" aria-busy="true">
			{#each { length: 5 }, i (i)}
				<div class="flex items-center gap-3">
					<Skeleton class="h-10 flex-1" /><Skeleton class="h-4 w-24" /><Skeleton class="h-9 w-28" />
				</div>
			{/each}
		</div>
	{:else if list.data.data.length === 0}
		<EmptyState
			icon={ListOrdered}
			title="No Top 50 playlists yet"
			message={enabled
				? 'Playlists appear here once a week, month or year completes, or generate one for a past period.'
				: 'Enable the generator in the server config to create playlists.'}
		/>
	{:else}
		<ul
			class={[
				'divide-y divide-rule transition-opacity',
				(list.isPlaceholderData || manual.active) && 'opacity-60'
			]}
			aria-label="Top 50 playlists"
		>
			{#each list.data.data as row (keyOf(row))}
				{@const key = keyOf(row)}
				<TopPlaylistRow
					{row}
					expanded={expanded.has(key)}
					busy={regeneratingKey === key}
					toggling={togglingKey === key}
					disabled={!enabled}
					regenerateBlocked={unauthorized
						? AUTH_HINT
						: generatorBusy && regeneratingKey !== key
							? 'Another generator run is in progress'
							: null}
					ontoggle={() => toggleExpanded(key)}
					onregenerate={() => askRegenerate(row)}
					onsethidden={(hidden) => setHidden.mutate({ row, hidden })}
				/>
			{/each}
		</ul>
		<Pagination
			offset={page * LIMIT}
			limit={LIMIT}
			total={list.data.total}
			disabled={list.isPlaceholderData}
			onchange={(o) => {
				page = Math.floor(o / LIMIT);
				expanded.clear();
			}}
		/>
	{/if}
</div>

{#if status && generateOpen}
	<GenerateDialog
		bind:open={generateOpen}
		{status}
		blocked={run.isPending || verify.isPending || regenerate.isPending}
		ongenerate={onGenerate}
	/>
{/if}

<ConfirmDialog
	bind:open={confirmOpen}
	title={confirmTarget && confirmUi ? `${confirmUi.verb} ${confirmTarget.label}?` : ''}
	message={confirmUi?.confirm ?? ''}
	confirmLabel={confirmUi?.label ?? 'Regenerate'}
	onconfirm={confirmRegenerate}
/>
