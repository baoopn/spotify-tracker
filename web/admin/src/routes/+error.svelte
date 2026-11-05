<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { CircleAlert } from '@lucide/svelte';
</script>

<svelte:head>
	<title>{page.status} · Listening admin</title>
</svelte:head>

<main id="main" class="grid min-h-dvh place-items-center p-4">
	<div class="card-print w-full max-w-md p-8">
		<p
			class="eyebrow inline-flex items-center gap-2 rounded-full bg-spot-rose px-3 py-1 text-on-spot"
		>
			<CircleAlert class="size-4" aria-hidden="true" />
			{page.status}
		</p>
		<h1 class="mt-5 text-3xl font-semibold text-ink">
			{page.status === 404 ? 'Page not found' : 'Something went wrong'}
		</h1>
		<p class="mt-2 font-mono text-sm text-ink-soft">{page.error?.message ?? 'Unexpected error'}</p>
		<a href={resolve('')} class="btn btn-primary mt-6">Back to overview</a>
	</div>
</main>
