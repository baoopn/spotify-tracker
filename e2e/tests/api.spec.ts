import { expect, test } from '@playwright/test';

type Hist = { data: { played_at: string; track: { id: string; name: string } }[] };

test('health check', async ({ request }) => {
	const res = await request.get('/api/healthz');
	expect(res.status()).toBe(200);
	expect(await res.json()).toMatchObject({ status: 'ok' });
});

test('now playing is never cached', async ({ request }) => {
	const res = await request.get('/api/v1/now-playing');
	expect(res.status()).toBe(200);
	expect(res.headers()['cache-control']).toContain('no-store');
	expect(res.headers()['content-type']).toContain('application/json');
});

test('history returns at most the requested number of plays, newest first', async ({ request }) => {
	const res = await request.get('/api/v1/history?limit=5');
	expect(res.status()).toBe(200);
	const body = (await res.json()) as Hist;
	expect(Array.isArray(body.data)).toBe(true);
	expect(body.data.length).toBeLessThanOrEqual(5);
	const times = body.data.map((h) => Date.parse(h.played_at));
	expect([...times].sort((a, b) => b - a)).toEqual(times);
});

test('cached stats are served from Redis but must be revalidated by clients', async ({ request }) => {
	const url = `/api/v1/stats/summary?range=30d&e2e=${Date.now()}`;
	const first = await request.get(url);
	expect(first.status()).toBe(200);
	expect(first.headers()['x-cache']).toBe('MISS');
	const second = await request.get(url);
	expect(second.headers()['x-cache']).toBe('HIT');
	for (const res of [first, second]) {
		const cc = res.headers()['cache-control'];
		expect(cc).toBe('no-cache');
		expect(cc).not.toContain('max-age');
	}
	expect(await second.json()).toEqual(await first.json());
});

test('track detail: pending tracks are no-store, analysed tracks no-cache', async ({ request }) => {
	const hist = (await (await request.get('/api/v1/history?limit=20')).json()) as Hist;
	test.skip(hist.data.length === 0, 'no listening history in this database');
	for (const { track } of hist.data.slice(0, 10)) {
		const res = await request.get(`/api/v1/tracks/${track.id}`);
		expect(res.status()).toBe(200);
		const { data } = await res.json();
		const status = data.features?.status ?? 'pending';
		const cc = res.headers()['cache-control'];
		if (status === 'pending') expect(cc, `${track.name} is pending`).toBe('no-store');
		else expect(cc, `${track.name} is ${status}`).toBe('no-cache');
	}
});

test('unknown track is a JSON 404', async ({ request }) => {
	const res = await request.get('/api/v1/tracks/doesNotExist0000000000');
	expect(res.status()).toBe(404);
	expect(await res.json()).toMatchObject({ error: { code: 'not_found' } });
});

test('invalid stats range is a 400', async ({ request }) => {
	const res = await request.get('/api/v1/stats/summary?range=forever');
	expect(res.status()).toBe(400);
});

test('time zone endpoint reports the current offset', async ({ request }) => {
	const res = await request.get('/api/v1/timezone?tz=UTC');
	expect(res.status()).toBe(200);
	expect(await res.json()).toMatchObject({ data: { tz: 'UTC', offset_minutes: 0 } });
});

test('admin API rejects unauthenticated requests with JSON 401', async ({ request }) => {
	const res = await request.get('/admin/api/overview');
	expect(res.status()).toBe(401);
	expect(res.headers()['content-type']).toContain('application/json');
});

test('admin top playlists API requires authentication', async ({ request }) => {
	const res = await request.get('/admin/api/top-playlists');
	expect(res.status()).toBe(401);
	expect(res.headers()['content-type']).toContain('application/json');
	expect(await res.json()).toMatchObject({ error: { code: 'unauthorized' } });
});

test('admin config exposes only the publishable key', async ({ request }) => {
	const res = await request.get('/admin/api/config');
	expect(res.status()).toBe(200);
	const text = await res.text();
	expect(text).not.toMatch(/sk_(test|live)_/);
});

test('pages carry security headers', async ({ request }) => {
	for (const path of ['/', '/admin/']) {
		const res = await request.get(path);
		expect(res.status(), path).toBe(200);
		const h = res.headers();
		expect(h['content-security-policy'], path).toBeTruthy();
		expect(h['x-frame-options'], path).toBe('DENY');
		expect(h['x-content-type-options'], path).toBe('nosniff');
	}
});

test('live stream sends the current snapshot immediately (even when idle)', async ({ page }) => {
	await page.goto('/api/healthz');
	const first = await page.evaluate(
		() =>
			new Promise<string>((resolve) => {
				const es = new EventSource('/api/v1/now-playing/stream');
				const timer = setTimeout(() => {
					es.close();
					resolve('timeout');
				}, 5_000);
				es.addEventListener('now-playing', (e) => {
					clearTimeout(timer);
					es.close();
					resolve(`event:${(e as MessageEvent<string>).data.slice(0, 1)}`);
				});
			})
	);
	// JSON object or null: either way it arrives right away.
	expect(first).toMatch(/^event:[{n]/);
});

test('page-view beacon accepts valid events and rejects malformed ones', async ({ request }) => {
	const ok = await request.post('/api/v1/events', {
		data: { type: 'pageview', path: '/', referrer: 'https://example.org/', width: 1280 }
	});
	expect(ok.status()).toBe(202);
	const engagement = await request.post('/api/v1/events', {
		data: { type: 'engagement', path: '/', width: 1280, visible_ms: 1500 }
	});
	expect(engagement.status()).toBe(202);
	for (const body of ['{', '{"type":"click"}']) {
		const bad = await request.post('/api/v1/events', {
			data: body,
			headers: { 'content-type': 'text/plain' }
		});
		expect(bad.status(), body).toBe(400);
	}
	const big = await request.post('/api/v1/events', {
		data: JSON.stringify({ type: 'pageview', path: '/' + 'x'.repeat(4000) }),
		headers: { 'content-type': 'text/plain' }
	});
	expect(big.status()).toBe(413);
});
