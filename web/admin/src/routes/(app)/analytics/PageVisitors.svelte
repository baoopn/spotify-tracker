<script lang="ts">
	import KpiRow from '#lib/components/KpiRow.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ChartCard from '#lib/components/charts/ChartCard.svelte';
	import TimeSeriesChart from '#lib/components/charts/TimeSeriesChart.svelte';
	import BarList, { type BarItem } from '#lib/components/charts/BarList.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { ShieldCheck } from '@lucide/svelte';
	import { errorMessage } from '#lib/api.js';
	import { compact, num, timeSpan, titleCase } from '#lib/format.js';
	import { queries } from '#lib/queries.js';
	import { bucketLabel, bucketTitle } from '#lib/time.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { AnalyticsRange, DimCount } from '#lib/types.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	let { range }: { range: AnalyticsRange } = $props();

	// Same zone handling as API traffic: server-side day bucketing in the viewer's zone.
	const timeZone = $derived(zone.iana);
	// Keeps the previous range's data on screen (dimmed) while the new one loads.
	const pages = createQuery(() => queries.pageAnalytics(range, timeZone));
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => pages.refetch());

	const p = $derived(pages.data);
	const refreshing = $derived(pages.isPlaceholderData || manual.active);
	const x = $derived(p?.timeseries.map((b) => new Date(b.bucket)) ?? []);

	// Bucket instants come from the server; they are only formatted, never shifted.
	function xFormat(d: Date): string {
		if (p?.range === '24h') return bucketLabel(d, 'hour');
		return bucketLabel(d, 'day', { timeZone: p?.bucket === 'day' ? p.time_zone : undefined });
	}
	function xTooltipFormat(d: Date): string {
		return p?.bucket === 'day'
			? bucketTitle(d, 'day', { timeZone: p.time_zone })
			: bucketTitle(d, 'hour');
	}

	const bucketNote = $derived(
		p?.bucket === 'day'
			? p.time_zone && p.time_zone !== 'UTC'
				? `per calendar day · ${p.time_zone}`
				: 'per UTC day'
			: `per hour · shown in ${timeZone ?? 'UTC'}${zone.corrected ? ` (${zone.spec.label})` : ''}`
	);

	let regionNames: Intl.DisplayNames | null = null;
	try {
		regionNames = new Intl.DisplayNames(['en'], { type: 'region' });
	} catch {
		regionNames = null;
	}
	function countryName(code: string): string {
		if (!code) return 'Unknown';
		try {
			return regionNames?.of(code.toUpperCase()) ?? code;
		} catch {
			return code;
		}
	}

	const bars = (rows: DimCount[], label: (name: string) => string): BarItem[] =>
		rows.map((r) => ({ id: r.name || '∅', label: label(r.name), value: r.views }));

	const lists = $derived(
		p
			? [
					{
						title: 'Referrers',
						label: 'Views per referrer',
						empty: 'No referrers in this range.',
						items: bars(p.referrers, (n) => n || 'Direct')
					},
					{
						title: 'Countries',
						label: 'Views per country',
						empty: 'No countries in this range.',
						items: bars(p.countries, countryName)
					},
					{
						title: 'Devices',
						label: 'Views per device type',
						empty: 'No devices in this range.',
						items: bars(p.devices, (n) => (n ? titleCase(n) : 'Unknown'))
					},
					{
						title: 'Browsers',
						label: 'Views per browser',
						empty: 'No browsers in this range.',
						items: bars(p.browsers, (n) => n || 'Unknown')
					}
				]
			: []
	);
</script>

<section aria-labelledby="visitors-h" class="mt-10">
	<div class="mb-4">
		<h2 id="visitors-h" class="text-2xl font-semibold text-ink">Public page visitors</h2>
		<p class="mt-1 flex items-start gap-1.5 text-sm text-ink-soft">
			<ShieldCheck class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span>
				Cookieless and anonymous: no personal data is stored, visitors are counted with a salted
				hash that rotates daily, and bots are excluded.
			</span>
		</p>
	</div>

	{#if pages.isError && !p}
		<div class="card-print">
			<ErrorState message={errorMessage(pages.error)} onretry={() => manual.run()} />
		</div>
	{:else}
		<KpiRow
			label="Public page visitor totals"
			loading={!p}
			columns={4}
			items={[
				{ label: 'Views', value: compact(p?.totals.views) },
				{
					label: 'Visitors',
					value: compact(p?.totals.visitors),
					sub: 'unique, cookieless'
				},
				{ label: 'Avg. time on page', value: timeSpan(p?.totals.avg_visible_ms), sub: 'visible' },
				{
					label: 'Live now',
					value: compact(p?.totals.live_now),
					sub: 'last 5 minutes',
					live: (p?.totals.live_now ?? 0) > 0
				}
			]}
		/>

		{#if !p}
			<div class="mt-8 grid gap-6 xl:grid-cols-2">
				<div class="card p-5"><Skeleton class="h-64" /></div>
				<div class="card p-5"><Skeleton class="h-64" /></div>
			</div>
		{:else if p.totals.views === 0}
			<div class={['card mt-8 transition-opacity', refreshing && 'opacity-60']}>
				<EmptyState
					title="No visits recorded in this range yet"
					message="Views of the public page appear here shortly after they happen."
				/>
			</div>
		{:else}
			<div class="mt-8 grid gap-6 xl:grid-cols-2">
				<ChartCard title="Page views" subtitle={bucketNote} {refreshing}>
					{#snippet chart()}
						<TimeSeriesChart
							{x}
							{xFormat}
							{xTooltipFormat}
							label="Public page views per {p.bucket}"
							series={[
								{
									id: 'views',
									label: 'Views',
									color: 'var(--series-a)',
									fill: 'var(--series-a-fill)',
									values: p.timeseries.map((b) => b.views)
								}
							]}
						/>
					{/snippet}
					{#snippet table()}
						<table class="data-table">
							<thead><tr><th scope="col">Bucket</th><th scope="col">Views</th></tr></thead>
							<tbody>
								{#each p.timeseries as b (b.bucket)}
									<tr><td>{xTooltipFormat(new Date(b.bucket))}</td><td>{num(b.views)}</td></tr>
								{/each}
							</tbody>
						</table>
					{/snippet}
				</ChartCard>

				<ChartCard title="Pages" subtitle="By views" {refreshing}>
					{#snippet chart()}
						{#if p.paths.length}
							<BarList
								label="Views per page path"
								showShare
								format={num}
								items={bars(p.paths, (n) => n || '/')}
							/>
						{:else}
							<p class="text-sm text-muted">No page views in this range.</p>
						{/if}
					{/snippet}
				</ChartCard>
			</div>

			<div class="mt-8 grid gap-6 lg:grid-cols-2">
				{#each lists as l (l.title)}
					<ChartCard title={l.title} {refreshing}>
						{#snippet chart()}
							{#if l.items.length}
								<BarList label={l.label} showShare format={num} items={l.items} />
							{:else}
								<p class="text-sm text-muted">{l.empty}</p>
							{/if}
						{/snippet}
					</ChartCard>
				{/each}
			</div>
		{/if}
	{/if}
</section>
