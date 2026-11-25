<script lang="ts">
	import { MediaQuery } from 'svelte/reactivity';
	import { scriptLang } from '#lib/scriptLang.js';
	import type { LyricLine } from '#lib/types.js';

	let {
		lines,
		keys,
		active,
		size = 'sm',
		translation = null,
		translationLang,
		class: className = ''
	}: {
		lines: readonly LyricLine[];
		keys: readonly string[];
		active: number;
		size?: 'sm' | 'lg';
		/** One translated line per line ("" = none), shown under each one. */
		translation?: readonly string[] | null;
		translationLang?: string;
		class?: string;
	} = $props();

	const reducedMotion = new MediaQuery('prefers-reduced-motion: reduce');
	let box = $state<HTMLDivElement>();
	let pausedUntil = 0;
	let placed = false; // the first scroll after mount jumps; later ones glide
	/** CJK lines carry their language, so the matching font face comes first. */
	const langs = $derived(lines.map((l) => scriptLang(l.text)));

	// A web font arriving late (CJK faces load on demand) changes line heights:
	// centre the active line again once it has swapped in.
	let fontsLoaded = $state(0);
	$effect(() => {
		const fonts = document.fonts;
		const bump = () => fontsLoaded++;
		fonts.addEventListener('loadingdone', bump);
		return () => fonts.removeEventListener('loadingdone', bump);
	});

	function userScrolled() {
		pausedUntil = Date.now() + 5000;
	}

	// Keep the active line centred inside the box only (never scroll the page),
	// also when the lines change around it. Before the first line (or after
	// a change of song), go back to the top.
	$effect(() => {
		const i = active;
		void keys;
		void translation; // lines grow taller when a translation shows
		void fontsLoaded;
		if (!box) {
			placed = false;
			return;
		}
		if (Date.now() < pausedUntil) return;
		const behavior = !placed || reducedMotion.current ? 'auto' : 'smooth';
		if (i < 0) {
			box.scrollTo({ top: 0, behavior });
			placed = true;
			return;
		}
		const el = box.querySelector<HTMLElement>(`[data-i="${i}"]`);
		if (!el) return;
		box.scrollTo({
			top: el.offsetTop - box.clientHeight / 2 + el.clientHeight / 2,
			behavior
		});
		placed = true;
	});
</script>

<!-- A scrollable box must be focusable so keyboard users can scroll it; the
     listeners only pause auto-scroll while the user scrolls by hand.
     No aria-live: a line that changes every few seconds is too noisy. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
	bind:this={box}
	role="region"
	aria-label="Lyrics, scrollable"
	tabindex="0"
	class={[
		'relative overflow-y-auto',
		size === 'lg'
			? 'overscroll-contain py-[20svh] font-lyrics text-2xl leading-snug font-medium tracking-tight sm:text-3xl'
			: 'py-2 font-lyrics text-base leading-relaxed',
		className
	]}
	onwheel={userScrolled}
	onpointerdown={userScrolled}
	ontouchmove={userScrolled}
	onkeydown={userScrolled}
>
	{#each lines as line, i (keys[i])}
		<p
			data-i={i}
			dir="auto"
			lang={langs[i]}
			aria-current={i === active ? 'true' : undefined}
			class={[
				'lyric-line break-words',
				size === 'lg' ? 'py-2' : 'py-0.5',
				i === active
					? [size === 'lg' ? 'font-bold' : 'font-semibold', 'text-ink']
					: ['text-ink-soft', size === 'lg' ? 'opacity-50' : 'opacity-70']
			]}
		>
			{#if line.text}{line.text}{:else}<span aria-hidden="true">♪</span>{/if}
			{#if translation?.[i]}
				<span
					class={[
						'block font-normal opacity-75',
						size === 'lg' ? 'text-lg sm:text-xl' : 'text-[0.85em]'
					]}
					lang={translationLang}
					dir="auto">{translation[i]}</span
				>
			{/if}
		</p>
	{/each}
</div>

<style>
	@media (prefers-reduced-motion: no-preference) {
		.lyric-line {
			transition:
				color 200ms ease,
				opacity 200ms ease;
		}
	}
</style>
