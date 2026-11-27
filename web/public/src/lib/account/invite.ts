// Clerk invitation links on /listen (pure; tested by scripts/check-listen.mjs).
// Clerk sends invitees to /listen?__clerk_status=sign_up&__clerk_ticket=...;
// "complete" or "sign_in" means the invite was already used, or the email
// already has an account.

/**
 * sign_up  a fresh invite: show Clerk's sign-up
 * used     the invite was used (or the account exists): show sign-in
 * none     no invite in the URL
 */
export type InviteMode = 'sign_up' | 'used' | 'none';

export function inviteMode(search: string): InviteMode {
	const status = new URLSearchParams(search).get('__clerk_status');
	if (status === 'sign_up') return 'sign_up';
	if (status === 'complete' || status === 'sign_in') return 'used';
	return 'none';
}

/** An invitation ticket is in the URL. */
export function hasInviteTicket(search: string): boolean {
	return !!new URLSearchParams(search).get('__clerk_ticket');
}

/** Any Clerk invite parameter (`__clerk_*`) is in the URL. */
export function hasClerkParams(search: string): boolean {
	return [...new URLSearchParams(search).keys()].some((k) => k.startsWith('__clerk_'));
}

/**
 * The same path without its `__clerk_*` parameters (so a reload never retries
 * a used ticket); other parameters and the hash stay.
 */
export function stripClerkParams(pathname: string, search: string, hash: string): string {
	const q = new URLSearchParams(search);
	for (const k of [...q.keys()]) if (k.startsWith('__clerk_')) q.delete(k);
	const rest = q.toString();
	return pathname + (rest ? `?${rest}` : '') + hash;
}
