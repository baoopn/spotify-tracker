<script lang="ts">
	import type { Clerk } from '@clerk/clerk-js';
	import { signInTarget } from '#lib/account/redirect.js';

	let { clerk }: { clerk: Clerk } = $props();

	/**
	 * Mounts Clerk's sign-in, or its sign-up for an invitation link
	 * (`__clerk_status=sign_up` with a `__clerk_ticket`); unmounts on cleanup.
	 */
	function mount(c: Clerk) {
		return (node: HTMLDivElement) => {
			const params = new URLSearchParams(window.location.search);
			// /listen, or a safe ?redirect_url= / ?return_to= path (never another site).
			const redirect = new URL(
				signInTarget(window.location.pathname, window.location.search),
				window.location.origin
			).href;
			const opts = {
				routing: 'hash' as const,
				forceRedirectUrl: redirect,
				fallbackRedirectUrl: redirect
			};
			if (params.get('__clerk_status') === 'sign_up') {
				c.mountSignUp(node, opts);
				return () => c.unmountSignUp(node);
			}
			c.mountSignIn(node, opts);
			return () => c.unmountSignIn(node);
		};
	}
</script>

<div {@attach mount(clerk)} class="min-h-[22rem]"></div>
