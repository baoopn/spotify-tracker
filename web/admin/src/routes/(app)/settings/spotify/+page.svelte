<script lang="ts">
	import { onMount } from 'svelte';
	import { Info, KeyRound, LoaderCircle, PlugZap, RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import { resolve } from '$app/paths';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import Skeleton from '#lib/components/Skeleton.svelte';
	import ErrorState from '#lib/components/ErrorState.svelte';
	import Badge from '#lib/components/Badge.svelte';
	import { api, errorMessage } from '#lib/api.js';
	import { artistNames, dateTime, inDays, utcDateTime } from '#lib/format.js';
	import { relativeClock } from '#lib/clock.svelte.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { qk, queries } from '#lib/queries.js';
	import { startSpotifyAuthorize, tokenDaysLeft, tokenHealth } from '#lib/spotify.js';
	import { toasts } from '#lib/toast.svelte.js';
	import { ManualRefetch } from '#lib/manual-refetch.svelte.js';

	const queryClient = useQueryClient();
	const status = createQuery(() => queries.spotify());
	// Dim/spin only for this (user-triggered) refetch, never for background polls.
	const manual = new ManualRefetch(() => status.refetch());
	let authorizing = $state(false);

	/** Token status shows up on this page and on the overview. */
	function invalidateSpotify() {
		return Promise.all([
			queryClient.invalidateQueries({ queryKey: qk.spotify }),
			queryClient.invalidateQueries({ queryKey: qk.overview })
		]);
	}

	// The test may refresh the access token server-side, so re-read the status after it.
	const test = createMutation(() => ({
		mutationFn: () => api.spotifyTest(),
		onSuccess: (res) => {
			if (res.ok) toasts.success('Spotify connection works');
			else toasts.error('Spotify test failed', res.error);
		},
		onError: (err) => toasts.error('Spotify test failed', errorMessage(err)),
		onSettled: () => invalidateSpotify()
	}));
	const testResult = $derived(test.data);

	onMount(() => {
		// Returning from the OAuth callback: /admin/settings/spotify?result=success|error&message=…
		const params = page.url.searchParams;
		const result = params.get('result');
		if (result) {
			if (result === 'success') {
				toasts.success('Spotify connected', 'The new refresh token is stored.');
				void invalidateSpotify();
			} else {
				toasts.error('Spotify authorization failed', params.get('message') ?? undefined);
			}
			replaceState(resolve('settings/spotify'), {});
		}
	});

	async function authorize() {
		authorizing = true;
		await startSpotifyAuthorize();
		authorizing = false;
	}

	const s = $derived(status.data);
	const health = $derived(tokenHealth(s));
	const daysLeft = $derived(tokenDaysLeft(s, relativeClock.now));
	const missingPrivatePlaylists = $derived(
		!!s?.connected && !s.scopes.includes('playlist-read-private')
	);
</script>

<PageHeader
	title="Spotify"
	eyebrow="Settings"
	description="The refresh token the poller uses to read playback."
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary"
			onclick={() => test.mutate()}
			disabled={test.isPending || !s?.connected}
		>
			{#if test.isPending}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<PlugZap class="size-4" aria-hidden="true" />{/if}
			Test connection
		</button>
		<button type="button" class="btn btn-primary" onclick={authorize} disabled={authorizing}>
			{#if authorizing}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<KeyRound class="size-4" aria-hidden="true" />{/if}
			{s?.connected ? 'Re-authorize Spotify' : 'Connect Spotify'}
		</button>
	{/snippet}
</PageHeader>

{#if status.isError && !s}
	<div class="card">
		<ErrorState message={errorMessage(status.error)} onretry={() => manual.run()} />
	</div>
{:else if !s}
	<div class="card flex flex-col gap-3 p-6">
		{#each { length: 6 }, i (i)}<Skeleton class="h-6" />{/each}
	</div>
{:else}
	<div class="grid gap-4 lg:grid-cols-3">
		<section class="card p-6 lg:col-span-2" aria-labelledby="tok-h">
			<div class="mb-4 flex items-center justify-between gap-2">
				<h2 id="tok-h" class="font-semibold text-fg">Token status</h2>
				<button
					type="button"
					class="btn btn-ghost btn-sm"
					onclick={() => manual.run()}
					disabled={manual.active}
				>
					<RefreshCw class={['size-4', manual.active && 'animate-spin']} aria-hidden="true" /> Refresh
				</button>
			</div>
			<dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[12rem_1fr]">
				<dt class="text-muted">Connected</dt>
				<dd>
					{#if s.connected}<Badge tone="ok">Yes</Badge>{:else}<Badge tone="danger">No</Badge>{/if}
				</dd>
				<dt class="text-muted">Account</dt>
				<dd class="text-fg">
					{s.account ? `${s.account.display_name} (${s.account.id})` : '–'}
				</dd>
				<dt class="text-muted">Token source</dt>
				<dd class="text-fg">
					{s.source === 'env' ? 'Environment variable' : 'Authorized in dashboard'}
				</dd>
				<dt class="text-muted">Authorized</dt>
				<dd class="text-fg">{dateTime(s.authorized_at)}</dd>
				<dt class="text-muted">Expires</dt>
				<dd class="text-fg">
					{utcDateTime(s.expires_at)}
					{#if daysLeft != null}
						<Badge tone={health === 'ok' ? 'ok' : health === 'warn' ? 'warn' : 'danger'}>
							{inDays(daysLeft)}
						</Badge>
					{/if}
					<span class="block text-xs text-muted"
						>Local: {dateTime(s.expires_at)} · lifetime {s.lifetime_days} days</span
					>
				</dd>
				<dt class="text-muted">Last refresh</dt>
				<dd class="text-fg">{dateTime(s.last_refresh_at)}</dd>
				<dt class="text-muted">Last error</dt>
				<dd class={s.last_error ? 'font-mono text-danger' : 'text-fg'}>{s.last_error ?? 'None'}</dd>
				<dt class="text-muted">Scopes</dt>
				<dd class="flex flex-wrap gap-1.5">
					{#each s.scopes as scope (scope)}<span class="chip font-mono">{scope}</span>{:else}<span
							class="text-muted">–</span
						>{/each}
					{#if missingPrivatePlaylists}
						<p
							class="mt-1 flex w-full items-center gap-2 rounded-xl bg-spot-butter px-3 py-2 text-sm text-on-spot"
							role="status"
						>
							<Info class="size-4 shrink-0" aria-hidden="true" />
							<span class="flex-1">Re-authorize to include private playlists.</span>
							<button
								type="button"
								class="btn btn-secondary btn-sm"
								onclick={authorize}
								disabled={authorizing}>Re-authorize</button
							>
						</p>
					{/if}
				</dd>
				<dt class="text-muted">Client ID</dt>
				<dd class="flex flex-wrap items-center gap-2">
					<code class="rounded-lg bg-surface-2 px-2 py-1 font-mono text-xs break-all text-fg"
						>{s.client_id || '–'}</code
					>
					{#if s.client_id}<CopyButton value={s.client_id} label="Copy client ID" />{/if}
				</dd>
			</dl>
		</section>

		<section class="card flex flex-col gap-3 p-6" aria-labelledby="redir-h">
			<h2 id="redir-h" class="font-semibold text-fg">Redirect URI</h2>
			<code class="rounded-xl bg-surface-2 px-3 py-2 font-mono text-xs break-all text-fg"
				>{s.redirect_uri}</code
			>
			<div><CopyButton value={s.redirect_uri} label="Copy redirect URI" /></div>
			<p class="flex gap-2 rounded-xl bg-spot-sky p-3 text-sm text-on-spot">
				<Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
				<span>
					This exact URI must be registered under <strong>Redirect URIs</strong> of your app in the
					Spotify Developer Dashboard, otherwise authorization fails with
					<code class="font-mono">INVALID_CLIENT</code>.
				</span>
			</p>
		</section>
	</div>

	{#if testResult}
		<section class="card mt-4 p-6" aria-labelledby="test-h" aria-live="polite">
			<h2 id="test-h" class="mb-2 font-semibold text-fg">Connection test</h2>
			{#if testResult.ok}
				<p class="text-sm text-fg">
					<Badge tone="ok">OK</Badge> Spotify answered with HTTP {testResult.status}.
				</p>
				{#if testResult.now_playing?.track}
					<p class="mt-2 text-sm text-muted">
						Currently {testResult.now_playing.is_playing ? 'playing' : 'paused'}:
						<span class="font-medium text-fg">{testResult.now_playing.track.name}</span>
						by {artistNames(testResult.now_playing.track.artists)}
					</p>
				{:else}
					<p class="mt-2 text-sm text-muted">Nothing is playing right now.</p>
				{/if}
			{:else}
				<p class="text-sm text-ink">
					<Badge tone="danger">Failed</Badge>
					<span class="font-mono">{testResult.error}</span>
				</p>
			{/if}
		</section>
	{/if}
{/if}
