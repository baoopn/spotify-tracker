import type { Page } from '@playwright/test';
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

// Top 50 playlists: fixtured API responses (the dev database has none).
const WEEK_ID = '37i9dQZF1DXcBWIGoYBM5M';
const OLD_WEEK_ID = '37i9dQZF1DX0XUsuxWHRQd';
const MONTH_ID = '37i9dQZF1DWXRqgorJj26U';

function topPlaylist(
	period: 'week' | 'month' | 'year',
	id: string,
	start: string,
	end: string,
	label: string,
	newest: boolean
) {
	return {
		period,
		period_start: start,
		period_end: end,
		time_zone: 'UTC',
		label,
		name: `Top 50 · ${label}`,
		spotify_id: id,
		spotify_url: `https://open.spotify.com/playlist/${id}`,
		track_count: 50,
		total_plays: 312,
		published_at: end,
		tracks: newest
			? [
					{
						position: 1,
						plays: 14,
						track: { id: 'track1', name: 'Example song', duration_ms: 200_000, explicit: false, artists: [] }
					}
				]
			: []
	};
}

const TOP_POPULATED = {
	data: {
		time_zone: 'UTC',
		periods: {
			week: [
				topPlaylist('week', WEEK_ID, '2026-11-02T00:00:00Z', '2026-11-09T00:00:00Z', 'Week of Nov 2, 2026', true),
				topPlaylist('week', OLD_WEEK_ID, '2026-10-26T00:00:00Z', '2026-11-02T00:00:00Z', 'Week of Oct 26, 2026', false)
			],
			month: [
				topPlaylist('month', MONTH_ID, '2026-10-01T00:00:00Z', '2026-11-01T00:00:00Z', 'October 2026', true)
			],
			year: []
		}
	}
};

const TOP_EMPTY = { data: { time_zone: 'UTC', periods: { week: [], month: [], year: [] } } };

async function routeTop(page: Page, body: unknown) {
	await page.route('**/api/v1/playlists/top*', (route) =>
		route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
	);
	// Keep the Spotify embed offline: an empty document stands in for the player.
	await page.route('https://open.spotify.com/**', (route) =>
		route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>embed</title>' })
	);
}

test.describe('top 50 playlists', () => {
	test('lists the newest playlist per period with a Spotify embed', async ({ page, problems }) => {
		await routeTop(page, TOP_POPULATED);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top-heading"]');
		await expect(section.getByRole('heading', { name: 'Top 50 playlists' })).toBeVisible();
		await expect(section).toHaveAttribute('aria-busy', 'false');

		// Only kinds with entries are offered; the first available (week) is selected.
		const group = section.getByRole('group', { name: 'Playlist period' });
		const week = group.getByRole('radio', { name: 'Week' });
		const month = group.getByRole('radio', { name: 'Month' });
		await expect(week).toBeChecked();
		await expect(month).not.toBeChecked();
		await expect(group.getByRole('radio', { name: 'Year' })).toHaveCount(0);

		await expect(section.getByRole('heading', { name: 'Top 50 · Week of Nov 2, 2026' })).toBeVisible();
		// The range is shown in the response's zone (UTC), not the browser's (America/Denver).
		await expect(section.getByText(/Nov 2\s*[–-]\s*8, 2026/)).toBeVisible();
		await expect(section.getByText('50 tracks')).toBeVisible();
		await expect(section.getByText('312 plays')).toBeVisible();

		const open = section.getByRole('link', { name: /Open in Spotify/ });
		await expect(open).toHaveAttribute('href', `https://open.spotify.com/playlist/${WEEK_ID}`);
		await expect(open).toHaveAttribute('target', '_blank');
		await expect(open).toHaveAttribute('rel', 'noopener noreferrer');

		const iframe = section.locator('iframe');
		await expect(iframe).toHaveAttribute('src', new RegExp(String.raw`^https://open\.spotify\.com/embed/playlist/${WEEK_ID}\?utm_source=generator(&theme=0)?$`));
		await expect(iframe).toHaveAttribute('title', /Week of Nov 2, 2026/);
		await expect(iframe).toHaveAttribute('loading', 'lazy');

		// Older entries of the kind are plain links under "Earlier".
		const older = section.getByRole('link', { name: 'Week of Oct 26, 2026' });
		await expect(older).toHaveAttribute('href', `https://open.spotify.com/playlist/${OLD_WEEK_ID}`);
		await expect(older).toHaveAttribute('rel', 'noopener noreferrer');

		// Keyboard: arrow keys move through the segmented control.
		await week.focus();
		await page.keyboard.press('ArrowRight');
		await expect(month).toBeChecked();
		await expect(section.getByRole('heading', { name: 'Top 50 · October 2026' })).toBeVisible();
		await expect(section.getByText(/Oct 1\s*[–-]\s*31, 2026/)).toBeVisible();
		await expect(iframe).toHaveAttribute('src', new RegExp(String.raw`^https://open\.spotify\.com/embed/playlist/${MONTH_ID}\?utm_source=generator(&theme=0)?$`));
		await expect(section.getByRole('link', { name: 'Week of Oct 26, 2026' })).toHaveCount(0);
		expectClean(problems);
	});

	test('is absent when every period is empty', async ({ page, problems }) => {
		await routeTop(page, TOP_EMPTY);
		const answered = page.waitForResponse((r) => r.url().includes('/api/v1/playlists/top'));
		await page.goto('/');
		await answered;
		await expect(page.locator('section[aria-labelledby="top-heading"]')).toHaveCount(0);
		await expect(page.locator('iframe[src*="open.spotify.com"]')).toHaveCount(0);
		expectClean(problems);
	});
});
