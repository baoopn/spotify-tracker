import { expect, expectClean, test } from './fixtures';

// Signed-in flows need a Clerk test user; these cover what works without one.
test.describe('admin dashboard (signed out)', () => {
	test('redirects to the login screen and mounts Clerk sign-in', async ({ page, problems }) => {
		await page.goto('/admin/data/artists');
		await expect(page).toHaveURL(/\/admin\/login\?next=%2Fadmin%2Fdata%2Fartists/);
		await expect(page.getByRole('heading', { name: /back of the sleeve/i })).toBeVisible();
		// Clerk's hosted component (loaded from Clerk's frontend API).
		await expect(page.getByLabel(/email address/i)).toBeVisible({ timeout: 30_000 });
		expectClean(problems);
	});

	test('no admin data requests succeed without a session', async ({ page }) => {
		const statuses: number[] = [];
		page.on('response', (r) => {
			const u = new URL(r.url());
			if (u.pathname.startsWith('/admin/api/') && u.pathname !== '/admin/api/config') statuses.push(r.status());
		});
		await page.goto('/admin/');
		await expect(page).toHaveURL(/\/admin\/login/);
		for (const s of statuses) expect(s).toBe(401);
	});
});
