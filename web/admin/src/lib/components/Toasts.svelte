<script lang="ts">
	import { CircleCheck, CircleAlert, Info, TriangleAlert, X } from '@lucide/svelte';
	import { fly } from 'svelte/transition';
	import { toasts, type ToastKind } from '#lib/toast.svelte.js';

	const styles: Record<ToastKind, { icon: typeof Info; cls: string }> = {
		success: { icon: CircleCheck, cls: 'bg-spot-mint text-on-spot' },
		error: { icon: CircleAlert, cls: 'bg-spot-rose text-on-spot' },
		info: { icon: Info, cls: 'bg-spot-sky text-on-spot' },
		warning: { icon: TriangleAlert, cls: 'bg-spot-butter text-on-spot' }
	};
</script>

<!--
	The live region is always mounted (so announcements work), but it is only a
	fixed bottom layer while toasts are showing: iOS 26 Safari tints its URL bar
	from fixed elements at the screen edge.
-->
<div
	class={toasts.items.length > 0
		? 'pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 pt-4 pr-[max(1rem,env(safe-area-inset-right,0px))] pb-[calc(1rem+env(safe-area-inset-bottom,0px))] pl-[max(1rem,env(safe-area-inset-left,0px))] sm:items-end'
		: 'sr-only'}
	aria-live="polite"
	aria-relevant="additions"
>
	{#each toasts.items as t (t.id)}
		{@const s = styles[t.kind]}
		<div
			role={t.kind === 'error' ? 'alert' : 'status'}
			class="card-print pointer-events-auto flex w-full max-w-sm items-start gap-3 p-3"
			transition:fly={{ y: 16, duration: 180 }}
		>
			<span class={['grid size-8 shrink-0 place-items-center rounded-full', s.cls]}>
				<s.icon class="size-4" aria-hidden="true" />
			</span>
			<div class="min-w-0 flex-1 pt-0.5">
				<p class="text-sm font-semibold text-ink">{t.title}</p>
				{#if t.message}<p class="mt-0.5 text-sm break-words text-ink-soft">{t.message}</p>{/if}
			</div>
			<button
				type="button"
				class="btn btn-ghost btn-sm btn-icon"
				aria-label="Dismiss notification"
				onclick={() => toasts.dismiss(t.id)}
			>
				<X class="size-4" aria-hidden="true" />
			</button>
		</div>
	{/each}
</div>
