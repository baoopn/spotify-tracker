// The one QueryClient for the admin SPA, tuned so data is always fresh:
// every mount, focus and reconnect refetches, and polling (per query, see
// queries.ts) pauses while the tab is hidden.

import { QueryClient } from '@tanstack/svelte-query';
import { ApiError } from './api';

const MAX_RETRIES = 2;

/** Client errors won't change on retry (auth, not found, validation); 408/429 might. */
function shouldRetry(failureCount: number, error: unknown): boolean {
	if (failureCount >= MAX_RETRIES) return false;
	if (error instanceof ApiError) {
		const s = error.status;
		if (s >= 400 && s < 500 && s !== 408 && s !== 429) return false;
	}
	return true;
}

export const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			staleTime: 0,
			refetchOnMount: 'always',
			refetchOnWindowFocus: 'always',
			refetchOnReconnect: 'always',
			refetchIntervalInBackground: false,
			retry: shouldRetry
		},
		mutations: {
			retry: false
		}
	}
});
