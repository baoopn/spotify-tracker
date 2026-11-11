// URL guard checks for src/lib/links.ts, run by `pnpm check` (node strips the TS types).
import assert from 'node:assert/strict';
import { httpsHref, isSpotifyAuthorizeUrl } from '../src/lib/links.ts';

assert.equal(httpsHref('https://open.spotify.com/track/1'), 'https://open.spotify.com/track/1');
for (const bad of [
	'javascript:alert(1)',
	' javascript:alert(1)',
	'data:text/html,x',
	'http://x.test/',
	'//evil.test',
	'',
	null,
	undefined
]) {
	assert.equal(httpsHref(bad), null, `should reject ${JSON.stringify(bad)}`);
}

assert.ok(isSpotifyAuthorizeUrl('https://accounts.spotify.com/authorize?client_id=x'));
for (const bad of [
	'https://accounts.spotify.com.evil.test/authorize',
	'https://evil.test/https://accounts.spotify.com/',
	'http://accounts.spotify.com/authorize',
	'javascript:alert(1)',
	'',
	null,
	42
]) {
	assert.equal(isSpotifyAuthorizeUrl(bad), false, `should reject ${JSON.stringify(bad)}`);
}

console.log('links: ok');
