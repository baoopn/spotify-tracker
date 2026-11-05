<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		trigger,
		children,
		triggerClass = 'btn btn-ghost btn-sm'
	}: {
		/** Accessible name of the trigger button. */
		label: string;
		trigger: Snippet;
		children: Snippet<[close: () => void]>;
		triggerClass?: string;
	} = $props();

	const id = $props.id();
	let panel: HTMLDivElement | undefined = $state();
	let expanded = $state(false);

	function close() {
		panel?.hidePopover();
	}
</script>

<button
	type="button"
	class={triggerClass}
	popovertarget={id}
	aria-haspopup="dialog"
	aria-expanded={expanded}
	aria-label={label}
>
	{@render trigger()}
</button>

<div
	bind:this={panel}
	{id}
	popover="auto"
	role="dialog"
	aria-label={label}
	class="pop card"
	ontoggle={(e) => (expanded = (e as ToggleEvent).newState === 'open')}
>
	{@render children(close)}
</div>

<style>
	.pop {
		position: fixed;
		inset: 4.25rem 1rem auto auto;
		margin: 0;
		width: min(20rem, calc(100vw - 2rem));
		padding: 0.75rem;
		color: var(--text);
		box-shadow: var(--shadow-print);
	}
	.pop:popover-open {
		animation: pop-in 140ms ease-out;
	}
	@keyframes pop-in {
		from {
			opacity: 0;
			transform: translateY(-4px);
		}
	}
</style>
