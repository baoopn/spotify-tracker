import { chromium, type FullConfig } from '@playwright/test';

// Vite dev servers pre-bundle dependencies on the first browser visit and then
// force a full reload, which would abort a test's navigation. Visit each dev
// server until a load completes without that reload.
export default async function globalSetup(config: FullConfig) {
	const urls = config.projects
		.filter((p) => p.name.endsWith('-dev'))
		.map((p) => new URL(p.name === 'admin-dev' ? '/admin/' : '/', p.use.baseURL).toString());
	const browser = await chromium.launch();
	try {
		for (const url of urls) {
			const page = await browser.newPage();
			for (let i = 0; i < 6; i++) {
				try {
					await page.goto(url, { waitUntil: 'networkidle', timeout: 90_000 });
					await page.waitForTimeout(3_000);
					// Stable if a second load doesn't fail.
					await page.reload({ waitUntil: 'networkidle', timeout: 90_000 });
					break;
				} catch {
					// optimizer reload in progress; try again
				}
			}
			await page.close();
		}
	} finally {
		await browser.close();
	}
}
