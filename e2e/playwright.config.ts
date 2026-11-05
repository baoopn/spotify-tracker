import { defineConfig, devices } from '@playwright/test';

// Runs inside the Playwright container of the dev compose stack:
//   docker compose --profile e2e run --rm e2e
// URLs default to the compose service names; override them to run elsewhere.
const API = process.env.E2E_API_URL ?? 'http://127.0.0.1:8080';
const ADMIN_DEV = process.env.E2E_ADMIN_DEV_URL ?? 'http://127.0.0.1:5173';
const PUBLIC_DEV = process.env.E2E_PUBLIC_DEV_URL ?? 'http://127.0.0.1:5174';

export default defineConfig({
	testDir: './tests',
	globalSetup: './global-setup.ts',
	timeout: 45_000,
	expect: { timeout: 15_000 },
	fullyParallel: true,
	retries: process.env.CI ? 1 : 0,
	reporter: [['list'], ['html', { open: 'never', outputFolder: 'report' }]],
	outputDir: 'test-results',
	use: {
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		timezoneId: 'America/Denver',
		locale: 'en-US'
	},
	projects: [
		{ name: 'api', testMatch: /api\.spec\.ts/, use: { baseURL: API } },
		{
			name: 'embedded',
			testMatch: /(public|admin)\.spec\.ts/,
			use: { ...devices['Desktop Chrome'], baseURL: API }
		},
		{
			name: 'public-dev',
			testMatch: /public\.spec\.ts/,
			use: { ...devices['Desktop Chrome'], baseURL: PUBLIC_DEV }
		},
		{
			name: 'admin-dev',
			testMatch: /admin\.spec\.ts/,
			use: { ...devices['Desktop Chrome'], baseURL: ADMIN_DEV }
		},
		{
			name: 'mobile',
			testMatch: /public\.spec\.ts/,
			use: { ...devices['Pixel 7'], baseURL: API }
		}
	]
});
