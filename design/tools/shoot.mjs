// Builds design/preview/dist/luma-preview.html (tokens inlined) and screenshots it headlessly.
// Usage: node tools/shoot.mjs   → PNGs in design/preview/shots/
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { chromium } from 'playwright';

const root = new URL('../', import.meta.url);
const read = (p) => readFileSync(new URL(p, root), 'utf8');

const html = read('preview/index.html')
  .replace('/*__LUMA_TOKENS__*/', () => read('generated/luma-tokens.css'))
  .replace('/*__TOKENS_JSON__*/null', () => JSON.stringify(JSON.parse(read('tokens.json'))));
mkdirSync(new URL('preview/dist/', root), { recursive: true });
mkdirSync(new URL('preview/shots/', root), { recursive: true });
writeFileSync(new URL('preview/dist/luma-preview.html', root), html);

// Mirror the artifact skeleton so local renders match the published page.
const doc = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><style>body{margin:0}[hidden]{display:none!important}img{max-width:100%}</style></head><body>${html}</body></html>`;

const browser = await chromium.launch();
const problems = [];
for (const [scheme, width] of [['light', 1440], ['dark', 1440], ['light', 390]]) {
  const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: scheme, deviceScaleFactor: width < 500 ? 2 : 1, reducedMotion: 'reduce' });
  page.on('console', (m) => m.type() === 'error' && problems.push(`${scheme}@${width} console: ${m.text()}`));
  page.on('pageerror', (e) => problems.push(`${scheme}@${width} error: ${e.message}`));
  await page.setContent(doc, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  const fonts = await page.evaluate(() => ['Instrument Sans', 'Inter', 'Roboto Flex', 'Noto Sans', 'Open Sans'].filter((f) => !document.fonts.check(`16px "${f}"`)));
  if (fonts.length) problems.push(`${scheme}@${width} fonts not loaded: ${fonts.join(', ')}`);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  if (overflow > 0) problems.push(`${scheme}@${width} horizontal overflow ${overflow}px`);
  const tag = `${scheme}-${width}`;
  await page.screenshot({ path: new URL(`preview/shots/page-${tag}.png`, root).pathname, fullPage: true });
  if (width > 500) {
    for (const el of await page.$$('[data-shot]')) {
      const name = await el.getAttribute('data-shot');
      await el.screenshot({ path: new URL(`preview/shots/${name}-${scheme}.png`, root).pathname });
    }
  }
  await page.close();
}
await browser.close();
console.log(problems.length ? problems.join('\n') : 'no console errors, no overflow, all fonts loaded');
