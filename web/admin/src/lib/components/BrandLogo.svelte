<script lang="ts" module>
	export type BrandAsset = 'listening-wordmark' | 'listening-mark';
</script>

<script lang="ts">
	let {
		kind = 'listening-wordmark',
		alt = 'baoopn listening',
		class: className = ''
	}: {
		kind?: BrandAsset;
		alt?: string;
		/** Size classes, applied to both theme variants. */
		class?: string;
	} = $props();

	// Brand-kit files via /brand/<slot> (302 to the baoopn brand API's media URL,
	// or the static copy): primary (navy badge) in light mode, inverse in dark mode.
	const base = $derived(kind === 'listening-mark' ? 'mark' : 'wordmark');
	const src = $derived({
		light: `/brand/${base}_primary`,
		dark: `/brand/${base}_inverse`
	});
	const size = $derived(kind === 'listening-mark' ? { w: 200, h: 200 } : { w: 320, h: 132 });
</script>

<img
	src={src.light}
	{alt}
	width={size.w}
	height={size.h}
	class={['block dark:hidden', className]}
/>
<img src={src.dark} {alt} width={size.w} height={size.h} class={['hidden dark:block', className]} />
