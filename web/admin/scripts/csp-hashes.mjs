// Post-build: compute CSP hashes of the inline <script> blocks in the SPA
// fallback (the theme bootstrap from app.html and SvelteKit's start script),
// so the Go server can send `script-src 'self' 'sha256-…'` instead of
// 'unsafe-inline'. Writes build/csp-hashes.json and prints the values.
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const buildDir = fileURLToPath(new URL('../build/', import.meta.url));
const html = readFileSync(`${buildDir}index.html`, 'utf8');

const hashes = [];
for (const m of html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)) {
	const body = m[1];
	if (!body.trim()) continue;
	hashes.push(`'sha256-${createHash('sha256').update(body, 'utf8').digest('base64')}'`);
}

writeFileSync(
	`${buildDir}csp-hashes.json`,
	JSON.stringify({ file: 'index.html', script_src: hashes }, null, '\t') + '\n'
);
console.log(`csp: ${hashes.length} inline script hash(es) -> build/csp-hashes.json`);
for (const h of hashes) console.log(`  ${h}`);
