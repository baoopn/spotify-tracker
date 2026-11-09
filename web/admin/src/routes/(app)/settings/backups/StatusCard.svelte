<script lang="ts">
	import { CircleAlert, Info, LoaderCircle } from '@lucide/svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import OutLink from '#lib/components/OutLink.svelte';
	import RelTime from '#lib/components/RelTime.svelte';
	import { BACKUP_DOCS_URL, healthExplanation, healthMeta, shortKey } from '#lib/backups.js';
	import { bytes, dateTime, num } from '#lib/format.js';
	import type { BackupStatus } from '#lib/types.js';

	let { status }: { status: BackupStatus } = $props();

	const hm = $derived(healthMeta(status.health));
	const agent = $derived(status.agent);
	const ok = $derived(status.last_success);
	const running = $derived(status.runs.find((r) => r.status === 'running') ?? null);
	// Only worth showing while it is the latest outcome.
	const failure = $derived(
		status.last_failure &&
			(!ok || Date.parse(status.last_failure.started_at) > Date.parse(ok.started_at))
			? status.last_failure
			: null
	);
</script>

<section class="card p-6" aria-labelledby="bk-status-h">
	<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
		<h2 id="bk-status-h" class="font-semibold text-fg">Status</h2>
		<span
			class={[
				'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
				hm.cls
			]}
		>
			<hm.icon class="size-3.5" aria-hidden="true" />
			{hm.label}
		</span>
	</div>
	<p class="mb-4 text-sm text-ink-soft" role="status">{healthExplanation(status)}</p>

	{#if running}
		<p class="mb-4 flex items-center gap-2 rounded-xl bg-spot-sky px-3 py-2 text-sm text-on-spot">
			<LoaderCircle class="size-4 shrink-0 animate-spin" aria-hidden="true" />
			A backup is running (started <RelTime value={running.started_at} />).
		</p>
	{/if}

	{#if agent?.config_error}
		<p class="mb-4 flex gap-2 rounded-xl bg-spot-butter px-3 py-2 text-sm text-on-spot">
			<CircleAlert class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span class="font-mono break-words">{agent.config_error}</span>
		</p>
	{/if}

	<dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[12rem_1fr]">
		<dt class="text-muted">Last successful backup</dt>
		<dd class="text-fg">
			{#if ok}
				<RelTime value={ok.finished_at ?? ok.started_at} showAbsolute />
				<span class="block text-xs text-muted">
					{bytes(ok.size_bytes)} · {num(ok.tables)}
					{ok.tables === 1 ? 'table' : 'tables'}
				</span>
			{:else}
				<span class="text-muted">Never</span>
			{/if}
		</dd>

		{#if failure}
			<dt class="text-muted">Last failure</dt>
			<dd class="text-fg">
				<RelTime value={failure.started_at} />
				{#if failure.error}
					<span class="block font-mono text-xs break-words text-danger">{failure.error}</span>
				{/if}
			</dd>
		{/if}

		{#if agent}
			<dt class="text-muted">Next run</dt>
			<dd class="text-fg">
				{#if agent.paused}
					<span class="text-muted">Paused</span>
				{:else if agent.next_run_at}
					{dateTime(agent.next_run_at)}
				{:else}
					<span class="text-muted">–</span>
				{/if}
				<span class="block text-xs text-muted">Daily at {agent.backup_time} UTC</span>
			</dd>

			<dt class="text-muted">Service last seen</dt>
			<dd class="text-fg">
				<RelTime value={agent.last_seen_at} />
				{#if agent.version}<span class="block text-xs text-muted">version {agent.version}</span
					>{/if}
			</dd>

			<dt class="text-muted">Retention</dt>
			<dd class="text-fg">
				{agent.keep_days}
				{agent.keep_days === 1 ? 'day' : 'days'}, at least {agent.keep_min}
				{agent.keep_min === 1 ? 'backup' : 'backups'}
				<span class="block text-xs text-muted">
					Settings from {agent.settings_source === 'admin' ? 'this dashboard' : 'environment'}
				</span>
			</dd>

			<dt class="text-muted">Destination</dt>
			<dd>
				{#if agent.destination}
					<code class="rounded-lg bg-surface-2 px-2 py-1 font-mono text-xs break-all text-fg"
						>{agent.destination}</code
					>
				{:else}
					<span class="text-muted">Not set</span>
				{/if}
			</dd>

			<dt class="text-muted">Encryption recipients</dt>
			<dd>
				{#if agent.recipients.length}
					<ul class="flex flex-col gap-1.5">
						{#each agent.recipients as key (key)}
							<li class="flex flex-wrap items-center gap-2">
								<code
									class="rounded-lg bg-surface-2 px-2 py-1 font-mono text-xs text-fg"
									title={key}>{shortKey(key)}</code
								>
								<CopyButton value={key} label="Copy public key" />
							</li>
						{/each}
					</ul>
				{:else}
					<span class="text-muted">Not set</span>
				{/if}
			</dd>
		{/if}
	</dl>

	{#if !agent}
		<p class="mt-4 flex gap-2 rounded-xl bg-spot-sky p-3 text-sm text-on-spot">
			<Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
			<span>
				The backup service has not reported yet. Start the backup container and give it database
				access; see
				<OutLink href={BACKUP_DOCS_URL} class="font-medium underline">docs/backup.md</OutLink>.
			</span>
		</p>
	{/if}
</section>
