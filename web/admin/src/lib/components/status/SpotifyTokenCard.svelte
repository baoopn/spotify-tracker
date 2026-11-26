<script lang="ts">
	import { CircleAlert, CircleCheck, KeyRound, LoaderCircle, TriangleAlert } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import { inDays, utcDateTime } from '#lib/format.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { startSpotifyAuthorize, tokenDaysLeft, tokenHealth } from '#lib/spotify.js';
	import type { SpotifyStatus } from '#lib/types.js';

	let { status }: { status: SpotifyStatus } = $props();

	const health = $derived(tokenHealth(status));
	const daysLeft = $derived(tokenDaysLeft(status, relativeClock.now));
	let busy = $state(false);

	const tone = $derived(
		health === 'ok'
			? {
					cls: 'bg-spot-mint text-on-spot',
					bar: 'bg-spot-mint',
					icon: CircleCheck,
					label: 'Healthy'
				}
			: health === 'warn'
				? {
						cls: 'bg-spot-butter text-on-spot',
						bar: 'bg-spot-butter',
						icon: TriangleAlert,
						label: 'Expiring soon'
					}
				: health === 'danger'
					? {
							cls: 'bg-spot-rose text-on-spot',
							bar: 'bg-spot-rose',
							icon: CircleAlert,
							label: status.connected ? 'Action needed' : 'Not connected'
						}
					: { cls: 'bg-surface-3 text-fg', bar: 'bg-paper-4', icon: KeyRound, label: 'Unknown' }
	);

	const lifeRatio = $derived(
		daysLeft != null && status.lifetime_days > 0
			? Math.max(0, Math.min(1, daysLeft / status.lifetime_days))
			: 0
	);

	async function reauthorize() {
		busy = true;
		await startSpotifyAuthorize();
		busy = false;
	}
</script>

<section class="flex flex-col gap-4 border-t border-ink pt-4" aria-labelledby="sp-title">
	<div class="flex items-center justify-between gap-2">
		<h2 id="sp-title" class="eyebrow">Spotify token</h2>
		<span
			class={[
				'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
				tone.cls
			]}
		>
			<tone.icon class="size-3.5" aria-hidden="true" />
			{tone.label}
		</span>
	</div>

	<div>
		{#if status.connected && daysLeft != null}
			<p class="text-3xl font-semibold tracking-tight text-fg">
				{daysLeft}
				<span class="text-base font-medium text-muted">{daysLeft === 1 ? 'day' : 'days'} left</span>
			</p>
			<div
				class="mt-2 h-2 overflow-hidden rounded-full bg-surface-3"
				role="meter"
				aria-label="Token lifetime remaining"
				aria-valuemin={0}
				aria-valuemax={status.lifetime_days}
				aria-valuenow={Math.max(0, daysLeft)}
			>
				<div class={['h-full rounded-full', tone.bar]} style:width="{lifeRatio * 100}%"></div>
			</div>
			<p class="mt-2 text-xs text-muted">
				Expires {utcDateTime(status.expires_at)} ({inDays(daysLeft)}) · source:
				{status.source}
			</p>
		{:else if status.connected}
			<p class="text-lg font-semibold text-fg">Connected</p>
			<p class="text-xs text-muted">Expiry unknown · source: {status.source}</p>
		{:else}
			<p class="text-lg font-semibold text-fg">Not connected</p>
			<p class="text-sm text-muted">Authorize Spotify so the poller can read playback.</p>
		{/if}
		{#if status.last_error}
			<p class="mt-2 rounded-lg bg-spot-rose px-2.5 py-1.5 text-xs text-on-spot">
				Last error: <span class="font-mono">{status.last_error}</span>
			</p>
		{/if}
	</div>

	<div class="mt-auto flex flex-wrap gap-2">
		<button
			type="button"
			class={['btn btn-sm', health === 'ok' ? 'btn-secondary' : 'btn-primary']}
			onclick={reauthorize}
			disabled={busy}
		>
			{#if busy}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<KeyRound
					class="size-4"
					aria-hidden="true"
				/>{/if}
			{status.connected ? 'Re-authorize' : 'Connect Spotify'}
		</button>
		<a href={resolve('settings/spotify')} class="btn btn-ghost btn-sm">Details</a>
	</div>
</section>
