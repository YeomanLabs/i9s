// Turns docs/shots/*.html (from `go run ./cmd/shots`) into PNGs.
// Needs playwright-core and Microsoft Edge: npx -y playwright-core is fine.
import { readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.PLAYWRIGHT ?? 'playwright-core');
const dir = resolve('docs/shots');
const browser = await chromium.launch({ channel: 'msedge' });
const page = await browser.newPage({ deviceScaleFactor: 1.5 });
for (const f of readdirSync(dir).filter((x) => x.endsWith('.html'))) {
  await page.goto(pathToFileURL(join(dir, f)).href);
  const box = await page.locator('.term').boundingBox();
  await page.setViewportSize({ width: Math.ceil(box.width), height: Math.ceil(box.height) });
  await page.locator('.term').screenshot({ path: join(dir, f.replace('.html', '.png')) });
  console.log('wrote', f.replace('.html', '.png'));
}
await browser.close();
