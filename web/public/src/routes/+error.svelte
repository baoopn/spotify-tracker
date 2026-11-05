<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';

	const notFound = $derived(page.status === 404);
</script>

<svelte:head>
	<title>{notFound ? 'Page not found' : 'Error'} · What Bao is listening to</title>
</svelte:head>

<main id="main" class="flex-1 border-t border-ink py-16">
	<p class="chip label [--chip:var(--spot-rose)]">
		<span class="size-1.5 rounded-full bg-ink" aria-hidden="true"></span>
		{page.status}
	</p>
	<h1 class="mt-5 font-display text-4xl font-semibold tracking-[-0.02em] sm:text-5xl">
		{notFound ? 'Nothing playing here' : 'Something went wrong'}
	</h1>
	<p class="mt-3 text-ink-soft">
		{notFound ? "This page doesn't exist." : (page.error?.message ?? 'Please try again.')}
	</p>
	<a href={resolve('/')} class="btn btn-primary mt-8">See what's playing</a>
</main>
