<script lang="ts">
	// Listening sub-brand UI glyphs, served via /brand/<slot> (302 to the baoopn
	// brand API's media URL, or to the static/icons copy when it is unavailable).
	// Names here have no hyphens except now-playing, which has its own branch.
	// tone="auto" swaps navy (light theme) and cream (dark theme); "cream"/"navy"
	// pin one variant, e.g. cream inside the always-navy brand chip.
	type Name = 'now-playing' | 'paused' | 'track' | 'playlist' | 'headphones';
	type Tone = 'navy' | 'cream';

	let {
		name,
		tone = 'auto',
		class: klass = 'size-5'
	}: { name: Name; tone?: Tone | 'auto'; class?: string } = $props();

	const tones = $derived<{ tone: Tone; wrap: string }[]>(
		tone === 'auto'
			? [
					{ tone: 'navy', wrap: 'contents dark:hidden' },
					{ tone: 'cream', wrap: 'hidden dark:contents' }
				]
			: [{ tone, wrap: 'contents' }]
	);
</script>

{#each tones as t (t.tone)}
	<span class={t.wrap}>
		{#if name === 'now-playing'}
			<!-- The animated bars are SMIL inside the SVG; reduced motion gets the static copy. -->
			<picture>
				<source
					media="(prefers-reduced-motion: reduce)"
					srcset="/brand/icon_now_playing_static_{t.tone}"
				/>
				<img
					src="/brand/icon_now_playing_{t.tone}"
					alt=""
					width="200"
					height="200"
					class={['inline-block shrink-0', klass]}
				/>
			</picture>
		{:else}
			<img
				src="/brand/icon_{name}_{t.tone}"
				alt=""
				width="200"
				height="200"
				class={['inline-block shrink-0', klass]}
			/>
		{/if}
	</span>
{/each}
