// Footer credit parser checks, run by `pnpm check`.
// Uses Node's built-in TypeScript type stripping to import the real parser.
import assert from 'node:assert/strict';
import { FALLBACK, LOGO_HEIGHT, logoWidth, parseCredit } from '../src/lib/credit.ts';

const MEDIA = 'https://media.baoopn.com/brand';
/**
 * @param {string} name
 * @param {number} width
 * @param {number} height
 */
const image = (name, width, height) => ({
	url: `${MEDIA}/${name}.svg`,
	width,
	height,
	content_type: 'image/svg+xml'
});
/**
 * Parses a body that must be valid.
 * @param {unknown} v
 */
function parse(v) {
	const c = parseCredit(v);
	assert.ok(c, `should accept ${JSON.stringify(v)}`);
	return c;
}
/**
 * @param {Record<string, unknown>} [over]
 * @param {Record<string, unknown>} [logo]
 */
const body = (over = {}, logo = {}) => ({
	data: {
		text: 'by',
		prefix: 'by',
		suffix: 'baoopn',
		url: 'https://baoopn.com',
		alt: 'baoopn.com',
		logo: {
			kind: 'mark',
			style: 'color',
			light: image('mark-light', 200, 200),
			dark: image('mark-dark', 200, 200),
			...logo
		},
		updated_at: null,
		...over
	}
});

// Prefix and suffix pass through; empty or missing ones come back as ''.
let c = parse(body());
assert.ok(c);
assert.equal(c.prefix, 'by');
assert.equal(c.suffix, 'baoopn');
assert.equal(c.url, 'https://baoopn.com');
c = parse(body({ prefix: '  Made by ', suffix: '' }));
assert.equal(c.prefix, 'Made by');
assert.equal(c.suffix, '');
c = parse(body({ prefix: '   ', suffix: undefined }));
assert.equal(c.prefix, '');
assert.equal(c.suffix, '');

// A colour badge renders at 24x24 with rounded corners.
c = parse(body());
assert.equal(c.style, 'color');
assert.equal(c.rounded, true);
assert.deepEqual([c.logo.light.width, c.logo.light.height], [24, 24]);
assert.deepEqual([c.logo.dark.width, c.logo.dark.height], [24, 24]);

// A mono glyph of 160x78 keeps its shape (width 49) and is never rounded.
c = parse(
	body(
		{},
		{ style: 'mono', light: image('glyph-light', 160, 78), dark: image('glyph-dark', 160, 78) }
	)
);
assert.equal(c.style, 'mono');
assert.equal(c.rounded, false);
assert.equal(LOGO_HEIGHT, 24);
assert.deepEqual([c.logo.light.width, c.logo.light.height], [49, 24]);
assert.equal(logoWidth(160, 78), 49);
// Unknown style: treated as mono, so nothing is clipped.
assert.equal(parse(body({}, { style: 'neon' })).rounded, false);
// Odd sizes stay in a sane range.
assert.equal(logoWidth(1, 1000), 1);
assert.equal(logoWidth(10000, 1), 120);

// Bad media host, http URLs and broken sizes are rejected.
assert.equal(
	parseCredit(
		body({}, { light: { ...image('x', 200, 200), url: 'https://evil.example.com/x.svg' } })
	),
	null
);
assert.equal(
	parseCredit(
		body({}, { dark: { ...image('x', 200, 200), url: 'http://media.baoopn.com/x.svg' } })
	),
	null
);
assert.equal(
	parseCredit(
		body({}, { light: { ...image('x', 200, 200), url: 'https://media.baoopn.com.evil.com/x.svg' } })
	),
	null
);
assert.equal(parseCredit(body({ url: 'http://baoopn.com' })), null);
assert.equal(parseCredit(body({ url: 'javascript:alert(1)' })), null);
assert.equal(parseCredit(body({}, { light: image('x', 0, 200) })), null);
assert.equal(parseCredit(body({}, { dark: image('x', 200, -1) })), null);
assert.equal(parseCredit(body({}, { light: image('x', Number.NaN, 200) })), null);

// Invalid responses give null, so the footer keeps its fallback text.
for (const bad of [
	null,
	undefined,
	'x',
	[],
	{},
	{ data: null },
	body({}, { kind: 'wordmark' }),
	body({ logo: null })
]) {
	assert.equal(parseCredit(bad), null, `should reject ${JSON.stringify(bad)}`);
}
assert.deepEqual({ ...FALLBACK }, { prefix: 'by', suffix: 'baoopn' });

console.log('credit checks: ok');
