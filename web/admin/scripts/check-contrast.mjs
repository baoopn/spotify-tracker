// WCAG contrast check for src/lib/theme/tokens.css ("paper / ink / spot inks").
// Spots are tested as backgrounds under --on-spot text (both modes), never as text colours.
// Usage: node scripts/check-contrast.mjs   (exits 1 on any failure)
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const css = readFileSync(
	fileURLToPath(new URL('../src/lib/theme/tokens.css', import.meta.url)),
	'utf8'
).replace(/\/\*[\s\S]*?\*\//g, '');

/** @type {{ selector: string, vars: Record<string, string> }[]} */
const blocks = [];
for (const m of css.matchAll(/([^{}]+)\{([^}]*)\}/g)) {
	const vars = {};
	for (const d of m[2].matchAll(/--([\w-]+):\s*([^;]+);/g)) vars[d[1]] = d[2].trim();
	blocks.push({ selector: m[1].trim(), vars });
}
const merge = (pred) =>
	Object.assign({}, ...blocks.filter((b) => pred(b.selector)).map((b) => b.vars));

const brand = merge((s) => s === ':root');
const modes = {
	light: merge((s) => s.includes("[data-theme='light']")),
	dark: merge((s) => s === "[data-theme='dark']")
};
const palettes = [...new Set([...css.matchAll(/data-palette='([\w-]+)'/g)].map((m) => m[1]))];

function resolve(t) {
	const out = {};
	for (const k of Object.keys(t)) {
		let v = t[k];
		for (let n = 0; n < 6 && /^var\(--[\w-]+\)$/.test(v); n++) v = t[v.slice(6, -1)] ?? v;
		out[k] = v;
	}
	return out;
}

const lum = (hex) => {
	const c = [1, 3, 5]
		.map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
		.map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
	return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};
const ratio = (a, b) => {
	const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
	return (x + 0.05) / (y + 0.05);
};
const isHex = (v) => /^#[0-9a-f]{6}$/i.test(v ?? '');

let failures = 0;
let checks = 0;
function need(label, t, fg, bg, min) {
	const a = t[fg];
	const b = t[bg];
	if (!isHex(a) || !isHex(b)) {
		failures++;
		console.log(`FAIL ${label}: cannot resolve ${fg}=${a} / ${bg}=${b}`);
		return;
	}
	checks++;
	const r = ratio(a, b);
	if (r < min) {
		failures++;
		console.log(`FAIL ${label}: ${fg} ${a} on ${bg} ${b} = ${r.toFixed(2)} < ${min}`);
	}
}

const papers = ['paper', 'paper-2', 'paper-3', 'paper-4'];
const spots = Object.keys(modes.light).filter((k) => k.startsWith('spot-'));

for (const [mode, base] of Object.entries(modes)) {
	for (const p of palettes) {
		// palette block (spot leads + light paper-2 tint), then the dark paper-2 tint
		const lead = merge((s) => s.includes(`[data-palette='${p}']`) && !s.includes('dark'));
		const darkTint =
			mode === 'dark' ? merge((s) => s.includes(`[data-theme='dark'][data-palette='${p}']`)) : {};
		const t = resolve({ ...brand, ...base, ...lead, ...darkTint });
		const L = `${p}/${mode}`;
		for (const bg of papers) {
			need(L, t, 'ink', bg, 7);
			need(L, t, 'ink-soft', bg, 4.5);
			for (const s of ['ok', 'warn', 'danger', 'info']) need(L, t, s, bg, 4.5);
		}
		// spots are fills: --on-spot text on every spot (and on the leading spots)
		for (const s of [...spots, 'spot-now', 'spot-select', 'spot-chart'])
			need(L, t, 'on-spot', s, 4.5);
		need(L, t, 'paper', 'ink', 4.5); // primary button: paper text on ink fill
		need(L, t, 'brand-fg', 'brand', 4.5);
		need(L, t, 'ink', 'paper', 3); // focus ring + chart strokes (non-text 3:1)
	}
}

console.log(
	`${checks} contrast checks across ${palettes.length} palettes x 2 modes, ${failures} failures`
);
process.exit(failures ? 1 : 0);
