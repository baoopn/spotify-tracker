<script lang="ts" module>
	export type BrandAsset =
		| 'listening-wordmark'
		| 'listening-mark'
		/** Master baoopn wordmark: only for the link back to baoopn.com. */
		| 'baoopn-wordmark';
</script>

<script lang="ts">
	import { asset } from '$app/paths';

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

	// Brand-kit files, used as-is: primary (navy badge) in light mode, inverse in dark mode.
	const src = $derived(
		kind === 'baoopn-wordmark'
			? {
					light: asset('brand/baoopn-wordmark-primary.svg'),
					dark: asset('brand/baoopn-wordmark-inverse.svg')
				}
			: {
					light: asset(`brand/listening/${kind}-primary.svg`),
					dark: asset(`brand/listening/${kind}-inverse.svg`)
				}
	);
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
