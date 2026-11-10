import { expect, test } from '@playwright/test';

// Brand files come from the baoopn brand API (media.baoopn.com) when it is
// reachable, else from the static copies. Every assertion accepts both.

const files: [string, RegExp][] = [
	['/favicon.svg', /^image\//],
	['/site.webmanifest', /^application\/manifest\+json/],
	['/og-image.png', /^image\//],
	['/brand/icon_now_playing_navy', /^image\//]
];

for (const [path, type] of files) {
	test(`${path} ends 200 after redirects`, async ({ request }) => {
		const res = await request.get(path);
		expect(res.status()).toBe(200);
		expect(res.headers()['content-type']).toMatch(type);
	});
}

test('manifest icons load', async ({ request, baseURL }) => {
	const res = await request.get('/site.webmanifest');
	expect(res.status()).toBe(200);
	const manifest = (await res.json()) as { icons: { src: string; purpose?: string }[] };
	expect(manifest.icons).toHaveLength(3);
	expect(manifest.icons.map((i) => i.purpose ?? '')).toEqual(['', '', 'maskable']);
	for (const icon of manifest.icons) {
		const img = await request.get(new URL(icon.src, baseURL).toString());
		expect(img.status(), icon.src).toBe(200);
		expect(img.headers()['content-type'], icon.src).toMatch(/^image\//);
	}
});

test('/brand/{slot} redirects to media or the static copy', async ({ request }) => {
	const res = await request.get('/brand/icon_now_playing_navy', { maxRedirects: 0 });
	expect(res.status()).toBe(302);
	expect(res.headers()['cache-control']).toBe('public, max-age=300');
	expect(res.headers()['location']).toMatch(
		/^(https:\/\/media\.baoopn\.com\/brand\/listening\/|\/icons\/now-playing-navy\.svg$)/
	);
});

test('unknown brand slot is 404', async ({ request }) => {
	const res = await request.get('/brand/nope', { maxRedirects: 0 });
	expect(res.status()).toBe(404);
});
