<script lang="ts">
	import BrandLogo from '../BrandLogo.svelte';
	import OutLink from '../OutLink.svelte';
	import ListeningIcon, { type ListeningIconName } from '../ListeningIcon.svelte';
	import {
		ChartLine,
		History,
		KeyRound,
		ListOrdered,
		Mail,
		MicVocal,
		Palette,
		ScrollText,
		X
	} from '@lucide/svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { MediaQuery } from 'svelte/reactivity';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	const desktop = new MediaQuery('min-width: 1024px');

	type NavItem = {
		href: ReturnType<typeof resolve>;
		label: string;
		exact: boolean;
		/** lucide icon, or a listening brand glyph (rendered as a currentColor mask). */
		icon?: typeof History;
		brand?: ListeningIconName;
	};

	const sections: { label: string | null; items: NavItem[] }[] = [
		{
			label: null,
			items: [{ href: resolve(''), label: 'Overview', brand: 'headphones', exact: true }]
		},
		{
			label: 'Data',
			items: [
				{ href: resolve('data/history'), label: 'History', icon: History, exact: false },
				{ href: resolve('data/tracks'), label: 'Tracks', brand: 'track', exact: false },
				{ href: resolve('data/artists'), label: 'Artists', icon: MicVocal, exact: false },
				{ href: resolve('data/playlists'), label: 'Playlists', brand: 'playlist', exact: false },
				{ href: resolve('data/top-playlists'), label: 'Top 50', icon: ListOrdered, exact: false }
			]
		},
		{
			label: 'Insights',
			items: [{ href: resolve('analytics'), label: 'Analytics', icon: ChartLine, exact: false }]
		},
		{
			label: 'Settings',
			items: [
				{ href: resolve('settings/spotify'), label: 'Spotify', icon: KeyRound, exact: false },
				{ href: resolve('settings/email'), label: 'Email alerts', icon: Mail, exact: false },
				{ href: resolve('settings/appearance'), label: 'Appearance', icon: Palette, exact: false },
				{ href: resolve('audit'), label: 'Audit log', icon: ScrollText, exact: false }
			]
		}
	];

	function isActive(href: string, exact: boolean): boolean {
		const path = page.url.pathname.replace(/\/$/, '') || '/';
		const target = href.replace(/\/$/, '') || '/';
		return exact ? path === target : path === target || path.startsWith(target + '/');
	}
</script>

{#if open}
	<button
		type="button"
		class="fixed inset-0 z-30 bg-ink/30 lg:hidden"
		aria-label="Close navigation"
		onclick={() => (open = false)}
	></button>
{/if}

<aside
	id="app-sidebar"
	class={[
		'fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-rule bg-paper transition-transform duration-200 lg:sticky lg:top-0 lg:h-dvh lg:translate-x-0',
		open ? 'translate-x-0 shadow-print' : '-translate-x-full'
	]}
	aria-label="Main navigation"
	inert={!open && !desktop.current}
>
	<div class="flex h-16 items-center justify-between gap-2 px-5">
		<a href={resolve('')} class="flex items-center gap-3 rounded-xl">
			<BrandLogo kind="listening-wordmark" class="h-auto w-32" />
			<span class="eyebrow">Admin</span>
		</a>
		<button
			type="button"
			class="btn btn-ghost btn-icon btn-sm lg:hidden"
			aria-label="Close navigation"
			onclick={() => (open = false)}
		>
			<X class="size-4" aria-hidden="true" />
		</button>
	</div>

	<nav class="flex-1 overflow-y-auto px-3 pb-6">
		{#each sections as section, si (si)}
			<div class="mt-4 first:mt-1">
				{#if section.label}
					<p class="eyebrow px-3 pb-1.5">
						{section.label}
					</p>
				{/if}
				<ul class="flex flex-col gap-0.5">
					{#each section.items as item (item.href)}
						{@const active = isActive(item.href, item.exact)}
						<li>
							<a
								href={item.href}
								aria-current={active ? 'page' : undefined}
								onclick={() => (open = false)}
								class={[
									'flex min-h-10 items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors duration-150',
									active
										? 'bg-spot-select text-on-spot'
										: 'text-ink-soft hover:bg-paper-3 hover:text-ink'
								]}
							>
								{#if item.brand}
									<ListeningIcon name={item.brand} mode="mask" class="size-4.5" />
								{:else if item.icon}
									<item.icon class="size-4.5 shrink-0" aria-hidden="true" />
								{/if}
								{item.label}
							</a>
						</li>
					{/each}
				</ul>
			</div>
		{/each}
	</nav>

	<!-- The master baoopn logo only links back to the main site. -->
	<div class="border-t border-rule px-5 py-3">
		<OutLink
			href="https://baoopn.com"
			class="inline-flex items-center gap-2 rounded-lg font-mono text-xs text-ink-soft hover:text-ink"
		>
			<BrandLogo kind="baoopn-wordmark" alt="baoopn" class="h-6 w-auto" />
			<span>baoopn.com</span>
		</OutLink>
	</div>
</aside>
