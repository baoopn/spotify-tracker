<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import {
		CircleAlert,
		CircleCheck,
		CircleDashed,
		CirclePause,
		TriangleAlert
	} from '@lucide/svelte';
	import Skeleton from '../Skeleton.svelte';
	import { relativeClock } from '#lib/clock.svelte.js';
	import {
		backupsChip,
		enrichmentChip,
		lyricsChip,
		servicesChip,
		spotifyChip,
		translationsChip,
		type HealthChip,
		type HealthLevel
	} from '#lib/health.js';
	import { queries } from '#lib/queries.js';
	import type { Overview } from '#lib/types.js';

	// One chip per Status-page section. The queries share their cache and
	// polling with the Status page (same keys).
	let { overview }: { overview: Overview | undefined } = $props();

	const backups = createQuery(() => queries.backups());
	const lyrics = createQuery(() => queries.lyricsQueue());
	const translations = createQuery(() => queries.translationQueue());

	const chips = $derived<(HealthChip | null)[]>([
		overview ? spotifyChip(overview.spotify, relativeClock.now) : null,
		overview ? servicesChip(overview.services) : null,
		backups.data || backups.isError ? backupsChip(backups.data, relativeClock.now) : null,
		lyrics.data || lyrics.isError ? lyricsChip(lyrics.data) : null,
		translations.data || translations.isError ? translationsChip(translations.data) : null,
		overview ? enrichmentChip(overview.enrichment) : null
	]);

	// Status colours (they mean state), always paired with an icon.
	const look: Record<HealthLevel, { dot: string; icon: typeof CircleCheck }> = {
		ok: { dot: 'bg-spot-mint text-on-spot', icon: CircleCheck },
		warn: { dot: 'bg-spot-butter text-on-spot', icon: TriangleAlert },
		error: { dot: 'bg-spot-rose text-on-spot', icon: CircleAlert },
		off: { dot: 'bg-spot-sky text-on-spot', icon: CirclePause },
		unknown: { dot: 'bg-paper-4 text-ink', icon: CircleDashed }
	};

	const statusHref = resolve('status');
</script>

<section aria-labelledby="health-h">
	<div class="mb-3 flex items-baseline justify-between gap-2">
		<h2 id="health-h" class="eyebrow">Health</h2>
		<a href={statusHref} class="text-sm font-medium text-primary hover:underline">All status</a>
	</div>
	<ul
		class="grid grid-cols-2 gap-2 sm:grid-cols-3 2xl:grid-cols-6"
		aria-busy={chips.includes(null)}
	>
		{#each chips as c, i (c?.id ?? i)}
			<li class="min-w-0">
				{#if c}
					{@const l = look[c.level]}
					<a
						href="{statusHref}#{c.id}"
						class="card flex h-full min-w-0 items-start gap-2.5 p-3 transition-colors duration-150 hover:border-ink hover:bg-paper-3"
						aria-label="{c.label}: {c.value}, {c.status}"
					>
						<span
							class={['mt-0.5 grid size-5 shrink-0 place-items-center rounded-full', l.dot]}
							aria-hidden="true"
						>
							<l.icon class="size-3.5" />
						</span>
						<span class="flex min-w-0 flex-col">
							<span class="eyebrow">{c.label}</span>
							<span class="text-sm font-medium break-words text-ink">{c.value}</span>
						</span>
					</a>
				{:else}
					<div class="card flex items-start gap-2.5 p-3" aria-hidden="true">
						<Skeleton class="size-5 rounded-full" />
						<div class="flex flex-1 flex-col gap-1.5">
							<Skeleton class="h-3 w-2/3" /><Skeleton class="h-4 w-full" />
						</div>
					</div>
				{/if}
			</li>
		{/each}
	</ul>
</section>
