// Post-build: adapter-static empties build/ on every build, so restore the
// .gitkeep the Go embed relies on, and print the CSP hashes of the inline
// scripts (the Go server computes the same hashes at startup).
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const buildDir = fileURLToPath(new URL('../build/', import.meta.url));
writeFileSync(`${buildDir}.gitkeep`, '');

for (const file of ['index.html', '404.html']) {
	const path = `${buildDir}${file}`;
	if (!existsSync(path)) {
		console.error(`postbuild: missing build/${file}`);
		process.exit(1);
	}
	const html = readFileSync(path, 'utf8');
	const hashes = [];
	for (const m of html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)) {
		if (!m[1].trim()) continue;
		hashes.push(`'sha256-${createHash('sha256').update(m[1], 'utf8').digest('base64')}'`);
	}
	console.log(`csp: build/${file}: ${hashes.length} inline script(s) ${hashes.join(' ')}`);
}
