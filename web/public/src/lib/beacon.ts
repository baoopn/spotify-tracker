// Privacy-friendly page analytics: one beacon per page view and, when the
// page is hidden, how long it was visible. No cookies, no storage, no IDs;
// the server only keeps coarse, aggregated counts.
const ENDPOINT = '/api/v1/events';

type PageEvent =
	| { type: 'pageview'; path: string; referrer: string; width: number }
	| { type: 'engagement'; path: string; width: number; visible_ms: number };

function send(event: PageEvent): void {
	const body = JSON.stringify(event);
	try {
		if (navigator.sendBeacon?.(ENDPOINT, body)) return;
	} catch {
		// fall through to fetch
	}
	void fetch(ENDPOINT, { method: 'POST', body, keepalive: true }).catch(() => {});
}

/** Starts page analytics; returns a cleanup function. */
export function startPageAnalytics(): () => void {
	// Automated browsers (tests, scrapers) announce themselves; don't count them.
	if (navigator.webdriver) return () => {};
	const path = location.pathname;
	const width = () => window.innerWidth;
	send({ type: 'pageview', path, referrer: document.referrer, width: width() });

	let visibleSince = document.visibilityState === 'visible' ? performance.now() : null;
	const report = () => {
		if (visibleSince == null) return;
		const ms = Math.round(performance.now() - visibleSince);
		visibleSince = null;
		if (ms > 0) send({ type: 'engagement', path, width: width(), visible_ms: ms });
	};
	const onVisibility = () => {
		if (document.visibilityState === 'hidden') report();
		else visibleSince ??= performance.now();
	};
	document.addEventListener('visibilitychange', onVisibility);
	window.addEventListener('pagehide', report);
	return () => {
		report();
		document.removeEventListener('visibilitychange', onVisibility);
		window.removeEventListener('pagehide', report);
	};
}
