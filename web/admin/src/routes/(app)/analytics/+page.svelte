<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Segmented from '#lib/components/Segmented.svelte';
	import KpiRow from '#lib/components/KpiRow.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ChartCard from '#lib/components/charts/ChartCard.svelte';
	import TimeSeriesChart from '#lib/components/charts/TimeSeriesChart.svelte';
	import BarList from '#lib/components/charts/BarList.svelte';
	import ListeningInsights from './ListeningInsights.svelte';
	import { api } from '#lib/api.js';
	import { compact, ms, num, percent } from '#lib/format.js';
	import { Query } from '#lib/query.svelte.js';
	import { bucketLabel, bucketTitle } from '#lib/time.js';
	import { zone } from '#lib/zone.svelte.js';
	import type { Analytics, AnalyticsRange } from '#lib/types.js';

	let range = $state<AnalyticsRange>('24h');
	// IANA zone for server-side day bucketing (the server has current tz rules);
	// unknown -> no tz -> UTC days.
	const timeZone = $derived(zone.iana);
	const analytics = new Query<Analytics>();

	$effect(() => {
		const r = range;
		const tz = timeZone;
		void analytics.run((signal) => api.analytics(r, tz, { signal }));
		return () => analytics.abort();
	});

	const a = $derived(analytics.data);
	const x = $derived(a?.timeseries.map((p) => new Date(p.bucket)) ?? []);

	// Bucket instants come from the server; they are only formatted, never shifted.
	// Daily (30d) buckets are calendar days in the zone the server bucketed in.
	function xFormat(d: Date): string {
		if (a?.range === '24h') return bucketLabel(d, 'hour');
		return bucketLabel(d, 'day', { timeZone: a?.bucket === 'day' ? a.time_zone : undefined });
	}
	function xTooltipFormat(d: Date): string {
		return a?.bucket === 'day'
			? bucketTitle(d, 'day', { timeZone: a.time_zone })
			: bucketTitle(d, 'hour');
	}

	const bucketNote = $derived(
		a?.bucket === 'day'
			? a.time_zone && a.time_zone !== 'UTC'
				? `per calendar day · ${a.time_zone}`
				: 'per UTC day'
			: `per hour · shown in ${timeZone ?? 'UTC'}${zone.corrected ? ` (${zone.spec.label})` : ''}`
	);

	const ranges = [
		{ value: '24h', label: '24 hours' },
		{ value: '7d', label: '7 days' },
		{ value: '30d', label: '30 days' }
	] satisfies { value: AnalyticsRange; label: string }[];
</script>

<PageHeader
	title="Analytics"
	eyebrow="Insights"
	description={a
		? `${compact(a.totals.requests)} public API requests in the last ${range} · p95 ${ms(a.totals.p95_ms)}`
		: 'Who reads the public API, how fast it answers, and what gets played.'}
/>

<section aria-labelledby="traffic-h">
	<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
		<h2 id="traffic-h" class="text-2xl font-semibold text-ink">API traffic</h2>
		<Segmented options={ranges} bind:value={range} label="Traffic time range" />
	</div>

	{#if analytics.error && !a}
		<div class="card-print">
			<ErrorState message={analytics.error} onretry={() => analytics.refetch()} />
		</div>
	{:else}
		<KpiRow
			label="API traffic totals"
			loading={!a}
			items={[
				{ label: 'Requests', value: compact(a?.totals.requests) },
				{
					label: 'Error rate',
					value: percent(a?.totals.error_rate),
					sub: a ? `${num(a.totals.errors)} × 5xx` : undefined
				},
				{ label: 'p50', value: ms(a?.totals.p50_ms) },
				{ label: 'p95', value: ms(a?.totals.p95_ms) },
				{ label: 'p99', value: ms(a?.totals.p99_ms) },
				{ label: 'Clients', value: compact(a?.totals.unique_clients) }
			]}
		/>

		<div class="mt-8 grid gap-6 xl:grid-cols-2">
			{#if a}
				<ChartCard
					title="Requests and errors"
					subtitle="{bucketNote} · errors are 5xx"
					refreshing={analytics.loading}
				>
					{#snippet chart()}
						<TimeSeriesChart
							{x}
							{xFormat}
							{xTooltipFormat}
							label="Requests and errors per {a.bucket}"
							series={[
								{
									id: 'requests',
									label: 'Requests',
									color: 'var(--series-a)',
									fill: 'var(--series-a-fill)',
									values: a.timeseries.map((p) => p.requests)
								},
								{
									id: 'errors',
									label: 'Errors',
									kind: 'column',
									color: 'var(--ink)',
									fill: 'var(--series-b-fill)',
									values: a.timeseries.map((p) => p.errors)
								}
							]}
						/>
					{/snippet}
					{#snippet table()}
						<table class="data-table">
							<thead>
								<tr
									><th scope="col">Bucket</th><th scope="col">Requests</th><th scope="col"
										>Errors</th
									></tr
								>
							</thead>
							<tbody>
								{#each a.timeseries as p (p.bucket)}
									<tr>
										<td>{xTooltipFormat(new Date(p.bucket))}</td>
										<td>{num(p.requests)}</td>
										<td>{num(p.errors)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{/snippet}
				</ChartCard>

				<ChartCard title="p95 latency" subtitle={bucketNote} refreshing={analytics.loading}>
					{#snippet chart()}
						<TimeSeriesChart
							{x}
							{xFormat}
							{xTooltipFormat}
							format={ms}
							axisFormat={(n) => `${compact(n)} ms`}
							label="p95 latency per {a.bucket}"
							series={[
								{
									id: 'p95',
									label: 'p95 latency',
									color: 'var(--series-a)',
									fill: 'var(--series-a-fill)',
									values: a.timeseries.map((p) => p.p95_ms)
								}
							]}
						/>
					{/snippet}
					{#snippet table()}
						<table class="data-table">
							<thead><tr><th scope="col">Bucket</th><th scope="col">p95</th></tr></thead>
							<tbody>
								{#each a.timeseries as p (p.bucket)}
									<tr><td>{xTooltipFormat(new Date(p.bucket))}</td><td>{ms(p.p95_ms)}</td></tr>
								{/each}
							</tbody>
						</table>
					{/snippet}
				</ChartCard>
			{:else}
				<div class="card p-5"><Skeleton class="h-64" /></div>
				<div class="card p-5"><Skeleton class="h-64" /></div>
			{/if}
		</div>

		{#if a}
			<div class="card mt-4 overflow-hidden">
				<h3 class="pb-3 text-2xl font-semibold text-ink">Routes</h3>
				{#if a.routes.length === 0}
					<EmptyState title="No traffic in this range" />
				{:else}
					<div class={['overflow-x-auto transition-opacity', analytics.loading && 'opacity-60']}>
						<table class="data-table">
							<caption class="sr-only">Requests per route</caption>
							<thead>
								<tr>
									<th scope="col">Route</th>
									<th scope="col" class="text-right">Requests</th>
									<th scope="col" class="text-right">Errors</th>
									<th scope="col" class="text-right">p50</th>
									<th scope="col" class="text-right">p95</th>
									<th scope="col" class="text-right">Avg</th>
								</tr>
							</thead>
							<tbody>
								{#each a.routes as r (r.method + r.route)}
									<tr>
										<td class="font-mono text-xs">
											<span class="mr-2 rounded bg-surface-3 px-1.5 py-0.5 font-semibold"
												>{r.method}</span
											>{r.route}
										</td>
										<td class="text-right">{num(r.requests)}</td>
										<td class={['text-right', r.errors > 0 && 'font-medium text-danger']}
											>{num(r.errors)}</td
										>
										<td class="text-right">{ms(r.p50_ms)}</td>
										<td class="text-right">{ms(r.p95_ms)}</td>
										<td class="text-right">{ms(r.avg_ms)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</div>

			<div class="mt-8 grid gap-6 lg:grid-cols-2">
				<ChartCard title="Status codes" refreshing={analytics.loading}>
					{#snippet chart()}
						{#if a.status_codes.length}
							<BarList
								label="Responses per status code"
								showShare
								items={a.status_codes.map((s) => ({
									id: String(s.code),
									label: String(s.code),
									value: s.count
								}))}
							/>
						{:else}
							<p class="text-sm text-muted">No responses in this range.</p>
						{/if}
					{/snippet}
				</ChartCard>
				<ChartCard title="Top origins" refreshing={analytics.loading}>
					{#snippet chart()}
						{#if a.top_origins.length}
							<BarList
								label="Requests per origin"
								showShare
								items={a.top_origins.map((o) => ({
									id: o.origin,
									label: o.origin || '(no origin)',
									value: o.count
								}))}
							/>
						{:else}
							<p class="text-sm text-muted">No cross-origin requests in this range.</p>
						{/if}
					{/snippet}
				</ChartCard>
			</div>
		{/if}
	{/if}
</section>

<ListeningInsights />
