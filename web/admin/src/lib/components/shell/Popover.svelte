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
		top: calc(4.25rem + env(safe-area-inset-top, 0px));
		right: max(1rem, env(safe-area-inset-right, 0px));
		bottom: auto;
		left: auto;
		margin: 0;
		width: min(
			20rem,
			calc(100vw - 2rem - env(safe-area-inset-left, 0px) - env(safe-area-inset-right, 0px))
		);
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
