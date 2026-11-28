// Checks the contrast of every text and background pair in every theme palette
// (WCAG AA: 4.5:1 for text, 3:1 for an icon or a graphic).
//
// The palettes come from web/src/app.css itself: the dark one is the
// `:root, .over-video` block, and each other one is a `:root[data-theme='…']`
// block, which only overrides tokens of the dark one. A token that is not a
// solid hex color (`rgba()`, `color-mix()`) is not checked.
//
// A pair is listed here because the screen really puts that text on that
// background: a new pair on screen goes into PAIRS.
//
// It also checks that the system bar color of each theme, written by hand in
// the script of web/index.html (it runs before the CSS exists), is the --bg of
// that palette.
//
// Usage: node web/scripts/contrast.mjs [path to app.css] [path to index.html].
// It prints one table per palette and exits with 1 if anything fails. The prek
// hook runs it whenever app.css or index.html changes.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const TEXT = 4.5;
const ICON = 3;

// [foreground token, background token, minimum, where it is on screen]
const PAIRS = [
  ['fg', 'bg', TEXT, 'text on the page'],
  ['fg', 'panel', TEXT, 'text on a card'],
  ['fg', 'panel-2', TEXT, 'text in a field or button'],
  ['fg', 'active-bg', TEXT, 'text on a toggled-on button'],
  ['dim', 'bg', TEXT, 'secondary text on the page'],
  ['dim', 'panel', TEXT, 'secondary text on a card'],
  ['dim', 'panel-2', TEXT, 'secondary text in a field'],
  ['accent', 'bg', TEXT, 'link on the page'],
  ['accent', 'panel', TEXT, 'link on a card'],
  ['accent', 'panel-2', TEXT, 'accent text in a field, hovered day count'],
  ['on-accent', 'accent-fill', TEXT, 'primary button, selected day'],
  ['ok', 'panel', TEXT, 'ok text on a card'],
  ['warn', 'panel', TEXT, 'warning text on a card'],
  ['bad', 'bg', TEXT, 'error text on the page'],
  ['bad', 'panel', TEXT, 'error text on a card'],
  ['on-warn', 'warn', TEXT, 'number on the bell, warning'],
  ['on-bad', 'bad', TEXT, 'number on the bell, error'],
  ['on-strong', 'accent', TEXT, 'number in the funnel (Diagnostics)'],
  ['on-strong', 'ok', TEXT, 'number in the funnel (Diagnostics)'],
  ['on-strong', 'dim', TEXT, 'number in the funnel (Diagnostics)'],
  ['on-strong', 'bad', TEXT, 'number in the funnel (Diagnostics)'],
  ['person', 'panel', ICON, 'person family icon on a card'],
  ['vehicle', 'panel', ICON, 'vehicle family icon on a card'],
  ['animal', 'panel', ICON, 'animal family icon on a card'],
];

// The tokens declared in each block of the file: { dark: {...}, light: {...} }.
// The dark block is the one whose selector list starts with `:root,`.
export function palettes(css) {
  const out = {};
  const block = /([^{}]+)\{([^{}]*)\}/g;
  for (const [, selector, body] of css.matchAll(block)) {
    const sel = selector.replace(/\/\*[\s\S]*?\*\//g, '').trim();
    let name;
    if (/^:root,\s*\.over-video$/.test(sel)) {
      name = 'dark';
    } else {
      const m = /^:root\[data-theme=['"]([\w-]+)['"]\]$/.exec(sel);
      if (!m) continue;
      name = m[1];
    }
    const tokens = (out[name] ??= {});
    for (const [, token, value] of body.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
      tokens[token] = value.trim();
    }
  }
  if (!out.dark) throw new Error('no `:root, .over-video` block with the dark palette');
  // Every other palette starts from the dark one.
  for (const name of Object.keys(out)) {
    if (name !== 'dark') out[name] = { ...out.dark, ...out[name] };
  }
  return out;
}

function hex(value) {
  let h = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(value)?.[1];
  if (!h) return null;
  if (h.length === 3) h = [...h].map((c) => c + c).join('');
  const n = parseInt(h, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

// Relative luminance, per WCAG 2.
function luminance([r, g, b]) {
  const lin = (c) => {
    c /= 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

export function ratio(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// The `BAR` map of the index.html script: { dark: '#0d1117', ... }.
export function bars(html) {
  const body = /const BAR = \{([^}]*)\}/.exec(html)?.[1];
  if (!body) throw new Error('no `const BAR = {...}` in index.html');
  const out = {};
  for (const [, name, value] of body.matchAll(/['"]?([\w-]+)['"]?\s*:\s*['"](#[0-9a-f]+)['"]/gi)) {
    out[name] = value;
  }
  return out;
}

function main() {
  const file = process.argv[2] ?? fileURLToPath(new URL('../src/app.css', import.meta.url));
  const html = process.argv[3] ?? fileURLToPath(new URL('../index.html', import.meta.url));
  const all = palettes(readFileSync(file, 'utf8'));
  let failed = 0;

  const bar = bars(readFileSync(html, 'utf8'));
  console.log('\nsystem bar (index.html) = --bg');
  for (const name of new Set([...Object.keys(all), ...Object.keys(bar)])) {
    const want = all[name]?.bg;
    const ok = want !== undefined && hex(bar[name] ?? '')?.join() === hex(want)?.join();
    if (!ok) failed++;
    console.log(
      `  ${ok ? 'ok  ' : 'FAIL'}  ${name.padEnd(10)} ${bar[name] ?? '-'} (--bg ${want ?? '-'})`,
    );
  }
  for (const [name, tokens] of Object.entries(all)) {
    console.log(`\n${name}`);
    for (const [fg, bg, min, where] of PAIRS) {
      const a = hex(tokens[fg] ?? '');
      const b = hex(tokens[bg] ?? '');
      if (!a || !b) {
        console.log(`  FAIL  --${fg} on --${bg}: not a hex color (${tokens[fg]}, ${tokens[bg]})`);
        failed++;
        continue;
      }
      const r = ratio(a, b);
      const ok = r >= min;
      if (!ok) failed++;
      const pair = `--${fg} on --${bg}`.padEnd(30);
      console.log(
        `  ${ok ? 'ok  ' : 'FAIL'}  ${pair} ${r.toFixed(2).padStart(5)}:1 (min ${min})  ${where}`,
      );
    }
  }
  if (failed) {
    console.error(`\n${failed} check(s) failed`);
    process.exit(1);
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
