// WCAG 2.x contrast checks for design/tokens.json. CLI exits 1 on any failing pair.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

function luminance(hex) {
  const n = parseInt(hex.slice(1), 16);
  const [r, g, b] = [n >> 16, (n >> 8) & 255, n & 255].map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function ratio(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

export function checkAll(tokens) {
  const out = [];
  for (const theme of Object.keys(tokens.color)) {
    const c = tokens.color[theme];
    for (const [fg, bg, min] of tokens.contrastPairs) {
      const r = ratio(c[fg], c[bg]);
      out.push({ theme, fg, bg, ratio: Math.round(r * 100) / 100, min, ok: r >= min });
    }
  }
  return out;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const tokens = JSON.parse(readFileSync(new URL('../tokens.json', import.meta.url)));
  const results = checkAll(tokens);
  for (const r of results) {
    console.log(`${r.ok ? 'ok  ' : 'FAIL'} ${r.theme.padEnd(5)} ${r.fg} on ${r.bg}: ${r.ratio} (min ${r.min})`);
  }
  process.exit(results.every((r) => r.ok) ? 0 : 1);
}
