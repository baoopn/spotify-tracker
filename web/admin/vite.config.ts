import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, type Plugin } from 'vite';

// @clerk/ui ships React "use client" directives; they are meaningless in this SPA
// and only produce bundler warnings, so drop them.
function stripUseClient(): Plugin {
	return {
		name: 'strip-use-client',
		enforce: 'pre',
		transform(code, id) {
			if (!id.includes('node_modules') || !/^['"]use client['"];?/.test(code)) return null;
			return { code: code.replace(/^['"]use client['"];?/, ''), map: null };
		}
	};
}

const API_TARGET = process.env.ADMIN_API_TARGET ?? 'http://127.0.0.1:8080';

export default defineConfig({
	plugins: [
		stripUseClient(),
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// Pure SPA: the Go API embeds `build/` and serves it at /admin with an
			// index.html fallback for every unknown path.
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html',
				precompress: false,
				strict: true
			}),
			paths: {
				base: '/admin'
			},
			version: {
				// The SPA is embedded in the API binary; no need to poll for new builds.
				pollInterval: 0
			}
		})
	],
	server: {
		port: 5173,
		strictPort: false,
		// Extra Host names for the dev server, e.g. the compose service name when
		// e2e tests run in another container (VITE_ALLOWED_HOSTS=web-admin).
		allowedHosts: process.env.VITE_ALLOWED_HOSTS?.split(',').filter(Boolean),
		// Bind mounts from Windows/macOS hosts don't deliver file events to a
		// Linux container: poll the sources instead (VITE_WATCH_POLL=<ms>).
		// Dependencies and build output are never watched.
		watch: process.env.VITE_WATCH_POLL
			? {
					usePolling: true,
					interval: Number(process.env.VITE_WATCH_POLL) || 1000,
					ignored: [
						'**/node_modules/**',
						'**/build/**',
						'**/.svelte-kit/output/**',
						'**/.pnpm-store/**'
					]
				}
			: undefined,
		proxy: {
			'/admin/api': { target: API_TARGET, changeOrigin: false },
			'/api': { target: API_TARGET, changeOrigin: false },
			// Brand assets (and their static fallbacks) are served by the Go API.
			'/brand': { target: API_TARGET, changeOrigin: false },
			'/logo': { target: API_TARGET, changeOrigin: false },
			'/icons': { target: API_TARGET, changeOrigin: false }
		}
	},
	preview: {
		proxy: {
			'/admin/api': { target: API_TARGET, changeOrigin: false },
			'/api': { target: API_TARGET, changeOrigin: false },
			// Brand assets (and their static fallbacks) are served by the Go API.
			'/brand': { target: API_TARGET, changeOrigin: false },
			'/logo': { target: API_TARGET, changeOrigin: false },
			'/icons': { target: API_TARGET, changeOrigin: false }
		}
	},
	build: {
		// Clerk's prebuilt UI (@clerk/ui + React, ~570 kB gzip) is one lazy chunk that
		// only loads after /admin/api/config; our own chunks stay far below this.
		chunkSizeWarningLimit: 1600,
		// Never inline SVGs as data: URIs; the server CSP only allows same-origin images.
		assetsInlineLimit: (file) => (file.endsWith('.svg') ? false : undefined),
		rolldownOptions: {
			checks: { pluginTimings: false }
		}
	}
});
