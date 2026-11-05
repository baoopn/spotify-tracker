import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, type ProxyOptions } from 'vite';

const API_TARGET = process.env.PUBLIC_API_TARGET ?? 'http://127.0.0.1:8080';

// Server-Sent Events must pass through the dev proxy unbuffered and
// uncompressed, or EventSource never sees the first event.
const apiProxy: Record<string, ProxyOptions> = {
	'/api': {
		target: API_TARGET,
		changeOrigin: false,
		ws: false,
		configure: (proxy) => {
			proxy.on('proxyReq', (proxyReq, req) => {
				if (req.headers.accept?.includes('text/event-stream')) {
					proxyReq.setHeader('accept-encoding', 'identity');
				}
			});
			proxy.on('proxyRes', (proxyRes, _req, res) => {
				if (!String(proxyRes.headers['content-type'] ?? '').startsWith('text/event-stream')) return;
				proxyRes.headers['cache-control'] = 'no-cache, no-transform';
				delete proxyRes.headers['content-encoding'];
				delete proxyRes.headers['content-length'];
				res.setHeader('x-accel-buffering', 'no');
				proxyRes.socket?.setNoDelay(true);
				// End the browser's stream when the upstream one dies, so EventSource
				// notices and reconnects instead of waiting on a silent socket.
				proxyRes.on('close', () => res.end());
				// 'proxyRes' fires before the proxy copies status and headers onto
				// res, so flush right after that synchronous step.
				queueMicrotask(() => {
					if (!res.headersSent) res.flushHeaders();
				});
			});
		}
	}
};

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// Prerendered static shell served by the Go API at the site root; all
			// data is fetched client-side. 404.html is served for unknown paths.
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: '404.html',
				precompress: false,
				strict: true
			}),
			paths: {
				base: ''
			},
			version: {
				// The site is embedded in the API binary; no need to poll for new builds.
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
		proxy: apiProxy
	},
	preview: {
		proxy: apiProxy
	},
	build: {
		rolldownOptions: {
			checks: { pluginTimings: false }
		}
	}
});
