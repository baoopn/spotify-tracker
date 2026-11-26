<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import PlayerCard from '#lib/components/player/PlayerCard.svelte';
</script>

<PageHeader
	title="Player"
	eyebrow="Listen"
	description="Control Spotify: play, skip, switch devices and queue songs."
/>

<PlayerCard />
