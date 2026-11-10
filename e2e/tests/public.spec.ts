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
		const { startedAt, durationMs } = await routePlaying(page, 0.25);
		await page.goto('/');
		const bar = page.getByRole('progressbar', { name: 'Playback progress' });
		await expect(bar).toBeVisible();
		// Sample the dot's position (as a share of the bar's width) for ~1 s of frames,
		// and where the fixture's player is at the first sample.
		const { xs, expected } = await page.evaluate(
			({ startedAt, durationMs }) =>
				new Promise<{ xs: number[]; expected: number }>((resolve) => {
					const bar = document.querySelector<HTMLElement>('[role="progressbar"]');
					const dot = bar?.querySelector<HTMLElement>('.rounded-full');
					const out: number[] = [];
					let expected = NaN;
					const t0 = performance.now();
					const frame = () => {
						if (bar && dot) {
							const b = bar.getBoundingClientRect();
							const d = dot.getBoundingClientRect();
							if (out.length === 0) {
								expected = Math.min(100, ((Date.now() - startedAt) / durationMs) * 100);
							}
							out.push(((d.left + d.width / 2 - b.left) / b.width) * 100);
						}
						if (performance.now() - t0 < 1000) requestAnimationFrame(frame);
						else resolve({ xs: out, expected });
					};
					requestAnimationFrame(frame);
				}),
			{ startedAt, durationMs }
		);
		// The dot sits where the player is (the fill's end), however long the page took
		// to load. If 100cqw resolved against the viewport instead of the bar, it would be off.
		expect(Math.abs(xs[0] - expected)).toBeLessThanOrEqual(2);
		const distinct = new Set(xs).size;
		// A 1 s stepping clock would give 1-2 positions; frames give dozens.
		expect(distinct).toBeGreaterThan(15);
		for (let i = 1; i < xs.length; i++) expect(xs[i]).toBeGreaterThanOrEqual(xs[i - 1]);
		// ~1 s of a 240 s track moves the dot ~0.42% of the width.
		expect(xs.at(-1)! - xs[0]).toBeGreaterThan(0.2);
		expect(xs.at(-1)! - xs[0]).toBeLessThan(1);
	});

	for (const at of [0.05, 0.95]) {
		test(`the progress dot at ${at * 100}% does not widen the page`, async ({ page }) => {
			await routePlaying(page, at);
			await page.goto('/');
			await expect(page.getByRole('progressbar', { name: 'Playback progress' })).toBeVisible();
			await expectNoHorizontalOverflow(page);
		});
	}

	test('the live page has no horizontal scroll on phones', async ({ page }, testInfo) => {
		test.skip(testInfo.project.name !== 'mobile', 'phone layout only');
		await page.goto('/');
		await expect(page.locator('section[aria-labelledby="now-heading"]')).toHaveAttribute(
			'aria-busy',
			'false'
		);
		await expect(page.locator('section[aria-labelledby="recent-heading"]')).toHaveAttribute(
			'aria-busy',
			'false'
		);
		const stats = page.locator('section[aria-labelledby="stats-heading"]');
		if (await stats.count()) await expect(stats).toHaveAttribute('aria-busy', 'false');
		await page.evaluate(() => document.fonts.ready.then(() => undefined));
		await expectNoHorizontalOverflow(page);
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

	test('header and footer link to the contact page', async ({ page }, testInfo) => {
		await page.goto('/');
		const contact = 'https://baoopn.com/contact';
		const header = page.getByRole('banner').getByRole('link', { name: 'Contact', exact: true });
		const footer = page.getByRole('contentinfo').getByRole('link', { name: 'Contact', exact: true });
		await expect(header).toHaveAttribute('href', contact);
		await expect(header).not.toHaveAttribute('target', /.+/);
		await expect(footer).toHaveAttribute('href', contact);
		if (testInfo.project.name === 'mobile') {
			await page.setViewportSize({ width: 320, height: 640 });
			await expect(header).toBeVisible();
			await page.evaluate(() => document.fonts.ready.then(() => undefined));
			await expectNoHorizontalOverflow(page);
		}
	});
});

test.describe('footer credit', () => {
	const CREDIT_URL = /^https:\/\/baoopn\.com\/api\/v1\/credit(\?|$)/;
	const LIGHT_LOGO = 'https://media.baoopn.com/brand/e2e-light.svg';
	const DARK_LOGO = 'https://media.baoopn.com/brand/e2e-dark.svg';
	const credit = (kind: string) => ({
		data: {
			text: 'Made with care by',
			url: 'https://baoopn.com',
			alt: 'baoopn.com',
			logo: {
				kind,
				light: { url: LIGHT_LOGO, width: 200, height: 200, content_type: 'image/svg+xml' },
				dark: { url: DARK_LOGO, width: 200, height: 200, content_type: 'image/svg+xml' }
			},
			updated_at: null
		}
	});
	const SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200"><rect width="200" height="200"/></svg>';

	async function routeCredit(page: Page, kind: string) {
		await page.route(CREDIT_URL, (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				headers: { 'access-control-allow-origin': '*' },
				body: JSON.stringify(credit(kind))
			})
		);
		await page.route('https://media.baoopn.com/**', (route) =>
			route.fulfill({ status: 200, contentType: 'image/svg+xml', body: SVG })
		);
	}

	test('shows "by [monogram] baoopn" and follows the theme', async ({ page, problems }) => {
		await routeCredit(page, 'mark');
		const asked = page.waitForRequest(CREDIT_URL);
		await page.goto('/');
		expect(new URL((await asked).url()).searchParams.get('logo')).toBe('mark');
		await expect(page.getByText('Updated live')).toBeVisible({ timeout: 20_000 });
		const footer = page.getByRole('contentinfo');
		const link = footer.getByRole('link', { name: 'by baoopn', exact: true });
		await expect(link).toHaveAttribute('href', 'https://baoopn.com');
		await expect(link).toHaveAttribute('rel', 'noopener');
		await expect(footer.getByText('Made with care by')).toHaveCount(0);
		const img = link.locator('img');
		await expect(img).toHaveAttribute('alt', '');
		await expect(img).toHaveAttribute('width', '24');

		// Light first, then the inverse monogram once the theme switches.
		await page.getByRole('button', { name: 'Appearance' }).click();
		const panel = page.locator('#theme-panel');
		await panel.locator('input[name="mode"][value="light"]').check({ force: true });
		await expect(img).toHaveAttribute('src', LIGHT_LOGO);
		await panel.locator('input[name="mode"][value="dark"]').check({ force: true });
		await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
		await expect(img).toHaveAttribute('src', DARK_LOGO);
		expectClean(problems);
	});

	test('keeps plain "by baoopn" when the endpoint sends a wordmark', async ({ page, problems }) => {
		await routeCredit(page, 'wordmark');
		const done = page.waitForResponse(CREDIT_URL);
		await page.goto('/');
		await done;
		await page.waitForTimeout(500);
		const footer = page.getByRole('contentinfo');
		await expect(footer.getByRole('link', { name: 'by baoopn', exact: true })).toBeVisible();
		await expect(footer.locator('img')).toHaveCount(0);
		expectClean(problems);
	});

	test('keeps plain "by baoopn" when the credit request fails', async ({ page, problems }) => {
		await page.route(CREDIT_URL, (route) => route.abort());
		const failed = page.waitForEvent('requestfailed', (r) => CREDIT_URL.test(r.url()));
		await page.goto('/');
		await failed;
		// Give the failed fetch time to settle, then the fallback must still be there.
		await page.waitForTimeout(500);
		const footer = page.getByRole('contentinfo');
		const link = footer.getByRole('link', { name: 'by baoopn', exact: true });
		await expect(link).toHaveAttribute('href', 'https://baoopn.com');
		await expect(footer.locator('img')).toHaveCount(0);
		expectClean(problems);
	});
});

/**
 * Deterministic playback at `at` (0-1) of a 240 s track: no stream (falls back
 * to polling), and each poll reports the position a real player would be at.
 */
async function routePlaying(page: Page, at: number): Promise<{ startedAt: number; durationMs: number }> {
	const durationMs = 240_000;
	await page.route('**/api/v1/now-playing/stream', (route) => route.abort());
	const startedAt = Date.now() - at * durationMs;
	await page.route('**/api/v1/now-playing', (route) =>
		route.fulfill({
			json: {
				data: {
					is_playing: true,
					progress_ms: Date.now() - startedAt,
					fetched_at: new Date().toISOString(),
					track: { id: 'glide1', name: 'Glide test', duration_ms: durationMs, explicit: false, artists: [] }
				}
			}
		})
	);
	return { startedAt, durationMs };
}

/** The page can't scroll sideways and nothing sticks out past the viewport (1 px slack). */
async function expectNoHorizontalOverflow(page: Page) {
	const { scrollWidth, clientWidth, offenders } = await page.evaluate(() => {
		const root = document.documentElement;
		const vw = root.clientWidth;
		// Content inside a clipping ancestor can't widen the page; the ancestor is checked itself.
		const clipped = (el: Element) => {
			for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
				if (getComputedStyle(p).overflowX !== 'visible') return true;
			}
			return false;
		};
		const offenders = [...document.querySelectorAll<HTMLElement>('body *')]
			.filter((el) => el.getBoundingClientRect().right > vw + 1 && !clipped(el))
			.map(
				(el) =>
					`${el.tagName.toLowerCase()}.${[...el.classList].join('.')} right=${Math.round(el.getBoundingClientRect().right)}`
			);
		return { scrollWidth: root.scrollWidth, clientWidth: vw, offenders };
	});
	expect(offenders, 'elements past the right edge').toEqual([]);
	expect(scrollWidth).toBeLessThanOrEqual(clientWidth);
}

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
	newest: boolean,
	live: { refreshedAt: string } | null = null
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
		published_at: live ? start : end,
		live: live !== null,
		refreshed_at: live ? live.refreshedAt : null,
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

const LIVE_WEEK_ID = '37i9dQZF1DX4JAvHpjipBk';

const DAY = 86_400_000;
const utcDay = (ms: number, opts: Intl.DateTimeFormatOptions) =>
	new Intl.DateTimeFormat('en-US', { ...opts, timeZone: 'UTC' }).format(ms);

/**
 * The running UTC week (Monday start; live, refreshed a few minutes ago) above the
 * finished previous week. Dates follow the clock so the live period never ends mid-suite.
 */
function topLive() {
	const now = Date.now();
	const today = Math.floor(now / DAY) * DAY;
	const start = today - ((new Date(today).getUTCDay() + 6) % 7) * DAY;
	const prev = start - 7 * DAY;
	const iso = (ms: number) => new Date(ms).toISOString();
	const weekOf = (ms: number) => `Week of ${utcDay(ms, { month: 'short', day: 'numeric', year: 'numeric' })}`;
	const live = { id: LIVE_WEEK_ID, label: weekOf(start), start: utcDay(start, { month: 'short', day: 'numeric' }) };
	const done = {
		id: WEEK_ID,
		label: weekOf(prev),
		range: new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' }).formatRange(prev, start - 1)
	};
	const body = {
		data: {
			time_zone: 'UTC',
			periods: {
				week: [
					topPlaylist('week', live.id, iso(start), iso(start + 7 * DAY), live.label, true, {
						refreshedAt: iso(now - 5 * 60_000)
					}),
					topPlaylist('week', done.id, iso(prev), iso(start), done.label, false)
				],
				month: [] as ReturnType<typeof topPlaylist>[],
				year: [] as ReturnType<typeof topPlaylist>[]
			}
		}
	};
	return { body, live, done };
}

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

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

const embedRe = (id: string) =>
	new RegExp(String.raw`^https://open\.spotify\.com/embed/playlist/${id}\?utm_source=generator(&theme=0)?$`);

test.describe('top 50 playlists', () => {
	test('lists the kept playlists per period and previews the picked one', async ({ page, problems }) => {
		await routeTop(page, TOP_POPULATED);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top-heading"]');
		await expect(section.getByRole('heading', { name: 'Top 50 playlists' })).toBeVisible();
		await expect(section).toHaveAttribute('aria-busy', 'false');
		await expect(section.getByText(/Auto-generated/)).toHaveCount(0);
		await expect(section.getByRole('heading', { name: 'Earlier' })).toHaveCount(0);

		// Only kinds with entries are offered; the first available (week) is selected.
		const group = section.getByRole('group', { name: 'Playlist period' });
		const week = group.getByRole('radio', { name: 'Week' });
		const month = group.getByRole('radio', { name: 'Month' });
		await expect(week).toBeChecked();
		await expect(month).not.toBeChecked();
		await expect(group.getByRole('radio', { name: 'Year' })).toHaveCount(0);

		// Every kept week is a row; the newest is picked and plays in the embed.
		const list = section.getByRole('group', { name: 'Playlist', exact: true });
		const newest = list.getByRole('radio', { name: /^Week of Nov 2, 2026/ });
		const older = list.getByRole('radio', { name: /^Week of Oct 26, 2026/ });
		await expect(list.getByRole('radio')).toHaveCount(2);
		await expect(newest).toBeChecked();
		await expect(older).not.toBeChecked();
		// The range is shown in the response's zone (UTC), not the browser's (America/Denver).
		await expect(section.getByText(/Nov 2\s*[–-]\s*8, 2026/)).toBeVisible();
		await expect(section.getByText('50 tracks')).toHaveCount(2);

		const iframe = section.locator('iframe');
		await expect(iframe).toHaveAttribute('src', embedRe(WEEK_ID));
		await expect(iframe).toHaveAttribute('title', 'Spotify player: Top 50 · Week of Nov 2, 2026');
		await expect(iframe).toHaveAttribute('loading', 'lazy');
		// The embed carries its own Spotify link.
		await expect(section.getByRole('link', { name: /Open in Spotify/ })).toHaveCount(0);

		// Clicking an earlier row previews it.
		await list.getByText('Week of Oct 26, 2026', { exact: true }).click();
		await expect(older).toBeChecked();
		await expect(newest).not.toBeChecked();
		await expect(section.locator('iframe')).toHaveAttribute('src', embedRe(OLD_WEEK_ID));
		await expect(section.locator('iframe')).toHaveAttribute('title', /Week of Oct 26, 2026/);

		// Keyboard: arrow keys move through the segmented control; a new kind starts at its newest.
		await week.focus();
		await page.keyboard.press('ArrowRight');
		await expect(month).toBeChecked();
		await expect(list.getByRole('radio')).toHaveCount(1);
		await expect(list.getByRole('radio', { name: /^October 2026/ })).toBeChecked();
		await expect(section.getByText(/Oct 1\s*[–-]\s*31, 2026/)).toBeVisible();
		await expect(section.locator('iframe')).toHaveAttribute('src', embedRe(MONTH_ID));

		// Back to week: the newest is picked again.
		await page.keyboard.press('ArrowLeft');
		await expect(week).toBeChecked();
		await expect(newest).toBeChecked();
		await expect(section.locator('iframe')).toHaveAttribute('src', embedRe(WEEK_ID));
		expectClean(problems);
	});

	test('shows the running week as live above the finished week', async ({ page, problems }) => {
		const { body, live, done } = topLive();
		await routeTop(page, body);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top-heading"]');
		await expect(section).toHaveAttribute('aria-busy', 'false');

		// One kind only: no period control.
		await expect(section.getByRole('group', { name: 'Playlist period' })).toHaveCount(0);

		const list = section.getByRole('group', { name: 'Playlist', exact: true });
		const liveRow = list.getByRole('radio', { name: /^This week so far/ });
		const doneRow = list.getByRole('radio', { name: new RegExp(`^${escapeRe(done.label)}`) });
		await expect(liveRow).toBeChecked();
		await expect(section.getByText('This week so far', { exact: true })).toBeVisible();
		// A running period reads "Nov 9 – today", plus when it was last re-ranked.
		await expect(section.getByText(new RegExp(String.raw`^${escapeRe(live.start)}\s*[–-]\s*today$`))).toBeVisible();
		// RelTime's short style: "5 min. ago" (en-US).
		await expect(section.getByText(/^updated\s+\d+\s*min\S*\s+ago$/)).toBeVisible();
		await expect(section.locator('time[datetime]').filter({ hasText: /^\d+\s*min\S*\s+ago$/ })).toHaveCount(1);

		const iframe = section.locator('iframe');
		await expect(iframe).toHaveAttribute('src', embedRe(LIVE_WEEK_ID));
		await expect(iframe).toHaveAttribute('title', `Spotify player: Top 50 · ${live.label}`);

		// The finished week shows its label and full range, with no live marker.
		await expect(section.getByText(done.label, { exact: true })).toBeVisible();
		await expect(section.getByText(done.range, { exact: true })).toBeVisible();
		await expect(section.getByText('This week so far')).toHaveCount(1);
		await expect(section.getByText(/^updated/)).toHaveCount(1);

		await list.getByText(done.label, { exact: true }).click();
		await expect(doneRow).toBeChecked();
		await expect(liveRow).not.toBeChecked();
		await expect(section.locator('iframe')).toHaveAttribute('src', embedRe(done.id));

		await page.evaluate(() => document.fonts.ready.then(() => undefined));
		await expectNoHorizontalOverflow(page);
		expectClean(problems);
	});

	test('a live entry whose period has ended is shown as finished', async ({ page, problems }) => {
		// Stale flag: the newest week (Oct 26 – Nov 1, 2026) is long over but still says live.
		const body = {
			data: {
				time_zone: 'UTC',
				periods: {
					week: [
						topPlaylist('week', OLD_WEEK_ID, '2026-10-26T00:00:00Z', '2026-11-02T00:00:00Z', 'Week of Oct 26, 2026', true, {
							refreshedAt: '2026-11-01T23:00:00Z'
						})
					],
					month: [],
					year: []
				}
			}
		};
		await routeTop(page, body);
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top-heading"]');
		await expect(section).toHaveAttribute('aria-busy', 'false');
		await expect(section.getByRole('radio', { name: /^Week of Oct 26, 2026/ })).toBeChecked();
		await expect(section.getByText(/Oct 26\s*[–-]\s*Nov 1, 2026/)).toBeVisible();
		await expect(section.locator('iframe')).toHaveAttribute('src', new RegExp(String.raw`/embed/playlist/${OLD_WEEK_ID}\?`));
		await expect(section.getByText('This week so far')).toHaveCount(0);
		await expect(section.getByText(/today/)).toHaveCount(0);
		await expect(section.getByText(/^updated/)).toHaveCount(0);
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
		const rangeLine = section.getByTestId('top5-range');
		await expect(rangeLine).toHaveText('Last 7 days');

		const group = section.getByRole('group', { name: 'Top 5 range' });
		const d7 = group.getByRole('radio', { name: '7D Last 7 days', exact: true });
		const d30 = group.getByRole('radio', { name: '30D Last 30 days', exact: true });
		const d365 = group.getByRole('radio', { name: '365D Last 365 days', exact: true });
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
		await d30.check({ force: true });
		await Promise.all([tracks30, artists30]);
		await expect(d30).toBeChecked();
		await expect(rangeLine).toHaveText('Last 30 days');
		await expect(tracks.getByRole('listitem')).toHaveCount(2);
		await expect(artists.getByRole('listitem')).toHaveCount(1);
		await expect(tracks.getByRole('link', { name: 'Top 30d song 1' })).toBeVisible();
		await expect(artists.getByRole('link', { name: 'Top 30d artist 1' })).toBeVisible();

		// 365D: empty in both columns, the control stays usable.
		await d365.check({ force: true });
		await expect(d365).toBeChecked();
		await expect(rangeLine).toHaveText('Last 365 days');
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
		// Skeleton first, while the requests (and their retries) are in flight.
		const section = page.locator('section[aria-labelledby="top5-heading"]');
		await expect(section).toBeVisible();
		await expect(section).toHaveAttribute('aria-busy', 'true');
		// Retries (2) finish, then the optional section is gone.
		await expect(section).toHaveCount(0, { timeout: 20_000 });
		expectClean(problems);
	});

	test('keeps the section and control when a later range fails', async ({ page, problems }) => {
		const fail = (kind: 'tracks' | 'artists') => async (route: Route) => {
			const range = (new URL(route.request().url()).searchParams.get('range') ?? '7d') as TopRange;
			if (range === '365d') return route.fulfill({ status: 500, body: '' });
			const data = kind === 'tracks' ? top5Tracks(range) : top5Artists(range);
			await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data, range }) });
		};
		await page.route('**/api/v1/stats/top-tracks*', fail('tracks'));
		await page.route('**/api/v1/stats/top-artists*', fail('artists'));
		await page.goto('/');
		const section = page.locator('section[aria-labelledby="top5-heading"]');
		const group = section.getByRole('group', { name: 'Top 5 range' });
		const d7 = group.getByRole('radio', { name: '7D Last 7 days', exact: true });
		const d365 = group.getByRole('radio', { name: '365D Last 365 days', exact: true });
		const tracks = section.getByRole('list', { name: 'Tracks' });
		const artists = section.getByRole('list', { name: 'Artists' });
		await expect(tracks.getByRole('listitem')).toHaveCount(5);
		await expect(artists.getByRole('listitem')).toHaveCount(5);

		await d365.check({ force: true });
		// After the retries both columns report the failure; the section and radios stay.
		await expect(section.getByText("Couldn't load tracks.")).toBeVisible({ timeout: 20_000 });
		await expect(section.getByText("Couldn't load artists.")).toBeVisible({ timeout: 20_000 });
		await expect(section).toBeVisible();
		await expect(group.getByRole('radio')).toHaveCount(3);
		await expect(d365).toBeChecked();

		// Back to 7D: the cached lists show again.
		await d7.check({ force: true });
		await expect(tracks.getByRole('listitem')).toHaveCount(5);
		await expect(artists.getByRole('listitem')).toHaveCount(5);
		await expect(tracks.getByRole('link', { name: 'Top 7d song 1' })).toBeVisible();
		expectClean(problems);
	});
});
