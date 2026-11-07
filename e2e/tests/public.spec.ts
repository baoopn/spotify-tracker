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

	test('the progress bar glides every frame while playing', async ({ page }) => {
		// Deterministic playback: no stream (falls back to polling), fixed track.
		await page.route('**/api/v1/now-playing/stream', (route) => route.abort());
		// Each poll reports the position a real player would be at by then.
		const startedAt = Date.now() - 60_000;
		await page.route('**/api/v1/now-playing', (route) =>
			route.fulfill({
				json: {
					data: {
						is_playing: true,
						progress_ms: Date.now() - startedAt,
						fetched_at: new Date().toISOString(),
						track: { id: 'glide1', name: 'Glide test', duration_ms: 240_000, explicit: false, artists: [] }
					}
				}
			})
		);
		await page.goto('/');
		const bar = page.getByRole('progressbar', { name: 'Playback progress' });
		await expect(bar).toBeVisible();
		// Sample the dot rail for ~1 s of animation frames.
		const xs = await page.evaluate(
			() =>
				new Promise<number[]>((resolve) => {
					const rail = document.querySelector<HTMLElement>('[role="progressbar"] .rail');
					const out: number[] = [];
					const t0 = performance.now();
					const frame = () => {
						const m = /translateX\(([\d.]+)%\)/.exec(rail?.style.transform ?? '');
						if (m) out.push(Number(m[1]));
						if (performance.now() - t0 < 1000) requestAnimationFrame(frame);
						else resolve(out);
					};
					requestAnimationFrame(frame);
				})
		);
		const distinct = new Set(xs).size;
		// A 1 s stepping clock would give 1-2 positions; frames give dozens.
		expect(distinct).toBeGreaterThan(15);
		for (let i = 1; i < xs.length; i++) expect(xs[i]).toBeGreaterThanOrEqual(xs[i - 1]);
		// ~1 s of a 240 s track moves the dot ~0.42% of the width.
		expect(xs.at(-1)! - xs[0]).toBeGreaterThan(0.2);
		expect(xs.at(-1)! - xs[0]).toBeLessThan(1);
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

test('automated browsers are not counted', async ({ page }) => {
	const beacons: string[] = [];
	page.on('request', (r) => {
		if (r.url().endsWith('/api/v1/events')) beacons.push(r.method());
	});
	await page.goto('/');
	await expect(page.getByText('Updated live')).toBeVisible({ timeout: 20_000 });
	expect(beacons).toEqual([]);
});

test('the public page sends a page-view beacon', async ({ page }) => {
	// Pretend to be a regular browser so the beacon is sent; the request is
	// aborted so the test visit never lands in the analytics.
	await page.addInitScript(() => Object.defineProperty(navigator, 'webdriver', { get: () => false }));
	await page.route('**/api/v1/events', (route) => route.abort());
	const beacon = page.waitForRequest(
		(r) => r.url().endsWith('/api/v1/events') && r.method() === 'POST',
		{ timeout: 15_000 }
	);
	await page.goto('/');
	const body = JSON.parse((await beacon).postData() ?? '{}');
	expect(body).toMatchObject({ type: 'pageview', path: '/' });
	expect(typeof body.width).toBe('number');
});
