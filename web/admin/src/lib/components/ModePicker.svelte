<script lang="ts">
	import { Monitor, Moon, Sun } from '@lucide/svelte';
	import Segmented from './Segmented.svelte';
	import { theme, type Mode } from '#lib/theme.svelte.js';

	let { size = 'md' }: { size?: 'sm' | 'md' } = $props();

	const options: { value: Mode; label: string; icon: typeof Sun }[] = [
		{ value: 'light', label: 'Light', icon: Sun },
		{ value: 'dark', label: 'Dark', icon: Moon },
		{ value: 'system', label: 'System', icon: Monitor }
	];
</script>

<Segmented
	{options}
	{size}
	label="Colour mode"
	value={theme.mode}
	onchange={(m) => theme.setMode(m)}
/>
