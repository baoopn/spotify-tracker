<script lang="ts" module>
	export type Tone = 'neutral' | 'accent' | 'ok' | 'warn' | 'danger' | 'info' | 'plain';
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		tone = 'neutral',
		dot = true,
		children,
		title
	}: { tone?: Tone; dot?: boolean; children: Snippet; title?: string } = $props();

	// Flat spot-ink chips with on-spot text; neutral is a plain paper chip.
	const tones: Record<Tone, string> = {
		neutral: 'bg-paper-4 text-ink border-transparent',
		accent: 'bg-spot-select text-on-spot border-transparent',
		ok: 'bg-spot-mint text-on-spot border-transparent',
		warn: 'bg-spot-butter text-on-spot border-transparent',
		danger: 'bg-spot-rose text-on-spot border-transparent',
		info: 'bg-spot-sky text-on-spot border-transparent',
		plain: 'bg-transparent text-ink border-rule-strong'
	};
</script>

<span
	{title}
	class={[
		'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 font-mono text-[0.6875rem] font-medium tracking-[0.06em] whitespace-nowrap uppercase',
		tones[tone]
	]}
>
	{#if dot}<span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>{/if}
	{@render children()}
</span>
