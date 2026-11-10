<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { auth } from '#lib/auth.svelte.js';

	/** Mounts Clerk's <UserProfile /> into the element; unmounts on cleanup. */
	function clerkUserProfile(node: HTMLDivElement) {
		const clerk = auth.clerk;
		if (!clerk) return;
		// Global colours come from clerkAppearance() in clerk.load; only fill the column here.
		clerk.mountUserProfile(node, {
			routing: 'hash',
			appearance: {
				elements: {
					rootBox: { width: '100%', maxWidth: 'none' },
					cardBox: { width: '100%', maxWidth: 'none' },
					// One Clerk user is shared by every baoopn.com app, so deleting it
					// here would remove it everywhere. Delete from the Clerk dashboard.
					profileSection__danger: { display: 'none' }
				}
			}
		});
		return () => clerk.unmountUserProfile(node);
	}
</script>

<PageHeader
	title="Account"
	eyebrow="Settings"
	description="Your sign-in details, managed by Clerk."
/>

<div {@attach clerkUserProfile} class="min-h-[32rem] w-full"></div>
