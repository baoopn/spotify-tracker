import type { Page, Route } from '@playwright/test';
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

// My top 5: range-keyed fixtures for tracks and artists.
type TopRange = '7d' | '30d' | '365d';
const TOP5_TRACK_COUNTS: Record<TopRange, number> = { '7d': 5, '30d': 2, '365d': 0 };
const TOP5_ARTIST_COUNTS: Record<TopRange, number> = { '7d': 5, '30d': 1, '365d': 0 };

function top5Tracks(range: TopRange) {
	return Array.from({ length: TOP5_TRACK_COUNTS[range] }, (_, i) => ({
		plays: 10 - i * 2 - (i === 4 ? 1 : 0),
		last_played: '2026-11-07T03:04:06Z',
		track: {
			id: `top-${range}-t${i + 1}`,
			name: `Top ${range} song ${i + 1}`,
			duration_ms: 200_000,
			explicit: i === 0,
			external_url: `https://open.spotify.com/track/top${range}t${i + 1}`,
			artists: [{ id: `a${i + 1}`, name: `Singer ${i + 1}`, external_url: `https://open.spotify.com/artist/a${i + 1}` }]
		}
	}));
}

function top5Artists(range: TopRange) {
	return Array.from({ length: TOP5_ARTIST_COUNTS[range] }, (_, i) => ({
		plays: 20 - i * 3,
		last_played: '2026-11-07T03:04:06Z',
		artist: {
			id: `top-${range}-a${i + 1}`,
			name: `Top ${range} artist ${i + 1}`,
			external_url: `https://open.spotify.com/artist/top${range}a${i + 1}`
		}
	}));
}

async function routeTop5(page: Page) {
	const ranges = { tracks: [] as string[], artists: [] as string[] };
	const serve = (kind: 'tracks' | 'artists') => async (route: Route) => {
		const url = new URL(route.request().url());
		const range = (url.searchParams.get('range') ?? '7d') as TopRange;
		ranges[kind].push(range);
		const data = kind === 'tracks' ? top5Tracks(range) : top5Artists(range);
		await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data, range }) });
	};
	await page.route('**/api/v1/stats/top-tracks*', serve('tracks'));
	await page.route('**/api/v1/stats/top-artists*', serve('artists'));
	return ranges;
}

test.describe('my top 5', () => {
	test('lists top tracks and artists and switches ranges together', async ({ page, problems }) => {
		const ranges = await routeTop5(page);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top5-heading"]');
		await expect(section.getByRole('heading', { name: 'My top 5', level: 2 })).toBeVisible();
		await expect(section).toHaveAttribute('aria-busy', 'false');
		await expect(section.getByText('Last 7 days', { exact: true })).toBeVisible();

		const group = section.getByRole('group', { name: 'Top 5 range' });
		const d7 = group.getByRole('radio', { name: 'Last 7 days' });
		const d30 = group.getByRole('radio', { name: 'Last 30 days' });
		const d365 = group.getByRole('radio', { name: 'Last 365 days' });
		await expect(d7).toBeChecked();
		await expect(d30).not.toBeChecked();
		await expect(d365).not.toBeChecked();

		const tracks = section.getByRole('list', { name: 'Tracks' });
		const artists = section.getByRole('list', { name: 'Artists' });
		await expect(section.getByRole('heading', { name: 'Tracks', level: 3 })).toBeVisible();
		await expect(section.getByRole('heading', { name: 'Artists', level: 3 })).toBeVisible();
		await expect(tracks.getByRole('listitem')).toHaveCount(5);
		await expect(artists.getByRole('listitem')).toHaveCount(5);
		await expect(tracks.getByRole('listitem').first()).toContainText('01');
		await expect(tracks.getByRole('listitem').first()).toContainText('Top 7d song 1');
		await expect(tracks.getByRole('listitem').first()).toContainText('10 plays');
		await expect(tracks.getByRole('listitem').nth(4)).toContainText('05');
		await expect(tracks.getByRole('listitem').nth(4)).toContainText('1 play');
		await expect(tracks.getByRole('listitem').nth(4)).not.toContainText('1 plays');
		await expect(artists.getByRole('listitem').first()).toContainText('20 plays');

		const trackLink = tracks.getByRole('link', { name: 'Top 7d song 1' });
		await expect(trackLink).toHaveAttribute('href', 'https://open.spotify.com/track/top7dt1');
		await expect(trackLink).toHaveAttribute('target', '_blank');
		await expect(trackLink).toHaveAttribute('rel', 'noopener noreferrer');
		const artistLink = artists.getByRole('link', { name: 'Top 7d artist 1' });
		await expect(artistLink).toHaveAttribute('href', 'https://open.spotify.com/artist/top7da1');
		await expect(artistLink).toHaveAttribute('target', '_blank');
		await expect(artistLink).toHaveAttribute('rel', 'noopener noreferrer');

		// 30D: both lists follow the one control.
		const tracks30 = page.waitForRequest((r) => r.url().includes('/api/v1/stats/top-tracks') && r.url().includes('range=30d'));
		const artists30 = page.waitForRequest((r) => r.url().includes('/api/v1/stats/top-artists') && r.url().includes('range=30d'));
		await section.getByText('30D', { exact: true }).click();
		await Promise.all([tracks30, artists30]);
		await expect(d30).toBeChecked();
		await expect(section.getByText('Last 30 days', { exact: true })).toBeVisible();
		await expect(tracks.getByRole('listitem')).toHaveCount(2);
		await expect(artists.getByRole('listitem')).toHaveCount(1);
		await expect(tracks.getByRole('link', { name: 'Top 30d song 1' })).toBeVisible();
		await expect(artists.getByRole('link', { name: 'Top 30d artist 1' })).toBeVisible();

		// 365D: empty in both columns, the control stays usable.
		await section.getByText('365D', { exact: true }).click();
		await expect(d365).toBeChecked();
		await expect(section.getByText('Last 365 days', { exact: true })).toBeVisible();
		await expect(section.getByText('No plays in this range yet.')).toHaveCount(2);
		await expect(section.getByRole('list', { name: 'Tracks' })).toHaveCount(0);
		await expect(section.getByRole('list', { name: 'Artists' })).toHaveCount(0);
		expect(ranges.tracks).toContain('365d');
		expect(ranges.artists).toContain('365d');

		// Keyboard: arrow keys move back through the segmented control.
		await d365.focus();
		await page.keyboard.press('ArrowLeft');
		await expect(d30).toBeChecked();
		await expect(tracks.getByRole('listitem')).toHaveCount(2);
		expectClean(problems);
	});

	test('is absent when both first loads fail', async ({ page, problems }) => {
		await page.route('**/api/v1/stats/top-tracks*', (route) => route.fulfill({ status: 500, body: '' }));
		await page.route('**/api/v1/stats/top-artists*', (route) => route.fulfill({ status: 500, body: '' }));
		await page.goto('/');
		await expect(page.getByRole('heading', { name: 'Recently played', exact: true })).toBeVisible();
		// Retries (2) finish, then the optional section is gone.
		await expect(page.locator('section[aria-labelledby="top5-heading"]')).toHaveCount(0, { timeout: 20_000 });
		expectClean(problems);
	});
});
