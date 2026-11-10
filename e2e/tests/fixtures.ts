import { test as base, expect, type Page } from '@playwright/test';

export type Problems = { pageErrors: string[]; cspViolations: string[]; consoleErrors: string[] };

/** Collects uncaught exceptions, CSP violations and console errors per page. */
export const test = base.extend<{ problems: Problems }>({
	// Never call the real baoopn.com credit from tests; the credit tests route their own.
	page: async ({ page }, use) => {
		await page.route(/^https:\/\/baoopn\.com\/api\/v1\/credit(\?|$)/, (route) => route.abort());
		await use(page);
	},
	problems: async ({ page }, use, testInfo) => {
		const problems: Problems = { pageErrors: [], cspViolations: [], consoleErrors: [] };
		page.on('pageerror', (err) => problems.pageErrors.push(err.message));
		page.on('console', (msg) => {
			if (msg.type() === 'error') problems.consoleErrors.push(msg.text());
		});
		await page.exposeFunction('__e2eCsp', (v: string) => problems.cspViolations.push(v));
		await page.addInitScript(() => {
			document.addEventListener('securitypolicyviolation', (e) =>
				(window as unknown as { __e2eCsp: (v: string) => void }).__e2eCsp(
					`${e.violatedDirective} blocked ${e.blockedURI}`
				)
			);
		});
		await use(problems);
		if (problems.consoleErrors.length) {
			testInfo.annotations.push({ type: 'console errors', description: problems.consoleErrors.join('\n') });
		}
	}
});

export { expect };

/** Fails on uncaught exceptions and CSP violations (console noise is only reported). */
export function expectClean(problems: Problems) {
	expect(problems.pageErrors, 'uncaught exceptions').toEqual([]);
	expect(problems.cspViolations, 'CSP violations').toEqual([]);
}

export async function history(page: Page) {
	const res = await page.request.get('/api/v1/history?limit=5');
	return (await res.json()).data as { track: { id: string; name: string } }[];
}
