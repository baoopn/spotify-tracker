<script lang="ts">
	import BrandLogo from '../BrandLogo.svelte';
	import { LogOut, Menu, Monitor, Moon, Palette, Sun } from '@lucide/svelte';
	import { resolve } from '$app/paths';
	import Popover from './Popover.svelte';
	import ZoneNotice from '../ZoneNotice.svelte';
	import PalettePicker from '../PalettePicker.svelte';
	import ModePicker from '../ModePicker.svelte';
	import { auth } from '#lib/auth.svelte.js';
	import { theme } from '#lib/theme.svelte.js';

	let { sidebarOpen = $bindable(false) }: { sidebarOpen?: boolean } = $props();

	const initials = $derived(
		(auth.user?.name || auth.user?.email || '?')
			.split(/[\s@.]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((s) => s[0]?.toUpperCase())
			.join('')
	);

	let avatarFailed = $state(false);
	const ModeIcon = $derived(theme.mode === 'system' ? Monitor : theme.mode === 'dark' ? Moon : Sun);
</script>

<header
	class="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-rule bg-paper px-4 sm:px-6 lg:px-8"
>
	<button
		type="button"
		class="btn btn-ghost btn-icon lg:hidden"
		aria-label="Open navigation"
		aria-controls="app-sidebar"
		aria-expanded={sidebarOpen}
		onclick={() => (sidebarOpen = true)}
	>
		<Menu class="size-5" aria-hidden="true" />
	</button>
	<a href={resolve('')} class="rounded-lg lg:hidden">
		<BrandLogo kind="listening-mark" class="size-8" />
	</a>

	<div class="flex min-w-0 flex-1 justify-end"><ZoneNotice /></div>

	<Popover label="Appearance" triggerClass="btn btn-ghost btn-icon">
		{#snippet trigger()}
			<span class="relative">
				<Palette class="size-5" aria-hidden="true" />
				<ModeIcon
					class="absolute -right-1.5 -bottom-1.5 size-3 rounded-full bg-paper text-ink-soft"
					aria-hidden="true"
				/>
			</span>
		{/snippet}
		{#snippet children(close)}
			<p class="eyebrow mb-2">Spot ink</p>
			<PalettePicker size="sm" />
			<p class="eyebrow mt-4 mb-2">Paper</p>
			<ModePicker size="sm" />
			<a
				href={resolve('settings/appearance')}
				class="mt-4 block text-sm font-semibold text-ink underline decoration-rule-strong underline-offset-4 hover:decoration-ink"
				onclick={close}>More appearance settings</a
			>
		{/snippet}
	</Popover>

	<Popover label="Account menu" triggerClass="btn btn-ghost gap-2 px-1.5 sm:px-2">
		{#snippet trigger()}
			{#if auth.user?.image_url && !avatarFailed}
				<img
					src={auth.user.image_url}
					alt=""
					class="size-8 rounded-full border border-ink bg-paper-4 object-cover"
					referrerpolicy="no-referrer"
					onerror={() => (avatarFailed = true)}
				/>
			{:else}
				<span
					class="grid size-8 place-items-center rounded-full border border-ink bg-spot-select font-mono text-xs font-semibold text-on-spot"
					aria-hidden="true">{initials}</span
				>
			{/if}
			<span class="hidden max-w-40 truncate text-sm font-medium sm:inline">
				{auth.user?.name || auth.user?.email}
			</span>
		{/snippet}
		{#snippet children(close)}
			<div class="flex items-center gap-3 px-1 pb-3">
				<div class="min-w-0">
					<p class="truncate text-sm font-medium text-fg">{auth.user?.name || 'Admin'}</p>
					<p class="truncate text-xs text-muted">{auth.user?.email}</p>
				</div>
			</div>
			{#if auth.version}
				<p class="eyebrow flex items-center gap-2 border-t border-rule px-1 py-2">
					API <span class="font-mono text-ink">{auth.version}</span>
				</p>
			{/if}
			<button
				type="button"
				class="btn btn-ghost w-full justify-start"
				onclick={() => {
					close();
					void auth.signOut();
				}}
			>
				<LogOut class="size-4" aria-hidden="true" /> Sign out
			</button>
		{/snippet}
	</Popover>
</header>
