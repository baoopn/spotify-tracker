import { expect, expectClean, history, test } from './fixtures';

test.describe('public page', () => {
	test('renders now playing, recent tracks and connects live', async ({ page, problems }) => {
		await page.goto('/');
		await expect(page.locator('h1')).toBeVisible();
		await expect(page.locator('#now-heading')).toHaveCount(1);
		await expect(page.getByRole('heading', { name: 'Recently played', exact: true })).toBeVisible();
		// The SSE stream connects (no fallback to polling or reconnect loops).
		await expect(page.getByText('Updated live')).toBeVisible({ timeout: 20_000 });
		expectClean(problems);
	});

	test('recent list matches the API (up to 5, no skeletons left)', async ({ page, problems }) => {
		const plays = await history(page);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="recent-heading"]');
		await expect(section).toHaveAttribute('aria-busy', 'false');
		if (plays.length === 0) return;
		await expect(section.getByRole('listitem').first()).toBeVisible();
		expect(await section.getByRole('listitem').count()).toBeLessThanOrEqual(5);
		expectClean(problems);
	});

	test('song stats settle: radar, "no audio data", or analysing (never stuck loading)', async ({
		page,
		problems
	}) => {
		const plays = await history(page);
		test.skip(plays.length === 0, 'no listening history in this database');
		await page.goto('/');
		const stats = page.locator('section[aria-labelledby="stats-heading"]');
		await expect(stats.getByRole('heading', { name: 'Song stats' })).toBeVisible();
		// The detail request finishes: 'Loading…' is replaced by a real state.
		await expect(stats.getByText('Loading…')).toHaveCount(0);
		await expect(stats).toHaveAttribute('aria-busy', 'false');

		const shown = plays[0].track.id;
		const detail = await (await page.request.get(`/api/v1/tracks/${shown}`)).json();
		const status: string = detail.data.features?.status ?? 'pending';
		if (status === 'pending') {
			await expect(stats.getByText('Analysing this track…')).toBeVisible();
		} else if (status === 'enriched') {
			await expect(stats.locator('svg').first()).toBeVisible();
			await expect(stats.getByText('Analysing this track…')).toHaveCount(0);
		} else {
			await expect(stats.getByText('No audio data for this track')).toBeVisible();
		}
		expectClean(problems);
	});

	test('a pending track keeps polling its stats every few seconds', async ({ page }) => {
		const plays = await history(page);
		test.skip(plays.length === 0, 'no listening history in this database');
		const detail = await (await page.request.get(`/api/v1/tracks/${plays[0].track.id}`)).json();
		test.skip((detail.data.features?.status ?? 'pending') !== 'pending', 'shown track is analysed');
		const hits: number[] = [];
		page.on('request', (r) => {
			if (r.url().includes(`/api/v1/tracks/${plays[0].track.id}`)) hits.push(Date.now());
		});
		await page.goto('/');
		await expect.poll(() => hits.length, { timeout: 15_000 }).toBeGreaterThanOrEqual(3);
	});

	test('appearance switcher changes the theme', async ({ page, problems }) => {
		await page.goto('/');
		// Interact only once the app has hydrated (the live status comes from JS).
		await expect(page.getByText('Updated live')).toBeVisible({ timeout: 20_000 });
		await page.getByRole('button', { name: 'Appearance' }).click();
		const panel = page.locator('#theme-panel');
		await expect(panel).toBeVisible();
		await panel.locator('input[name="mode"][value="dark"]').check({ force: true });
		await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
		expectClean(problems);
	});
});
