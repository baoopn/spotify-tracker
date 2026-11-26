<script lang="ts">
	import { CircleAlert, CircleCheck, TriangleAlert } from '@lucide/svelte';
	import RelTime from '../RelTime.svelte';
	import { titleCase } from '#lib/format.js';
	import type { ServiceHealth } from '#lib/types.js';

	let { services }: { services: ServiceHealth[] } = $props();

	const styles = {
		ok: { cls: 'bg-spot-mint text-on-spot', icon: CircleCheck, label: 'OK' },
		degraded: { cls: 'bg-spot-butter text-on-spot', icon: TriangleAlert, label: 'Degraded' },
		down: { cls: 'bg-spot-rose text-on-spot', icon: CircleAlert, label: 'Down' }
	} as const;
</script>

<section aria-labelledby="svc-title">
	<h2 id="svc-title" class="eyebrow mb-3">Services</h2>
	{#if services.length === 0}
		<p class="text-sm text-muted">No services reported.</p>
	{:else}
		<ul class="flex flex-col divide-y divide-border">
			{#each services as svc (svc.name)}
				{@const s = styles[svc.status] ?? styles.degraded}
				<li class="flex items-start justify-between gap-3 py-2.5 first:pt-0 last:pb-0">
					<div class="min-w-0">
						<p class="text-sm font-medium text-fg">{titleCase(svc.name)}</p>
						{#if svc.detail}
							<p class="truncate text-xs text-muted" title={svc.detail}>{svc.detail}</p>
						{/if}
						{#if svc.last_seen_at}
							<p class="text-xs text-muted">seen <RelTime value={svc.last_seen_at} /></p>
						{/if}
					</div>
					<span
						class={[
							'inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
							s.cls
						]}
					>
						<s.icon class="size-3.5" aria-hidden="true" />
						{s.label}
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</section>
