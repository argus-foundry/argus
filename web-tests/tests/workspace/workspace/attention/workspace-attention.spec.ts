import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  await page.addInitScript(() => {
    window.badgeTexts = [];
    const fillText = CanvasRenderingContext2D.prototype.fillText;
    CanvasRenderingContext2D.prototype.fillText = function (text, ...args) {
      window.badgeTexts.push(text);
      return fillText.call(this, text, ...args);
    };
  });
  await page.route('**/attention-fixture', route => route.fulfill({
    contentType: 'text/html',
    body: `<!doctype html><title>Theia</title><link rel="icon" href="/theia-icon-192.png"><script type="module">
 import {WorkspaceAttention} from '/workspace/attention/workspace-attention.js';
 window.originalIcon=document.querySelector('link[rel="icon"]');
 window.attention=new WorkspaceAttention(()=>{});
 window.count=value=>attention.update(Array.from({length:value},(_,id)=>({id:String(id),needs_input:true})));
 </script>`
  }));
  await page.goto((process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750') + '/attention-fixture');
  await page.waitForFunction(() => window.attention);
  return page;
}
async function plain(page) {
  assert.equal(await page.title(), 'Theia');
  assert.equal(await page.locator('link[rel="icon"]').count(), 1);
  assert.equal(await page.locator('link[rel="icon"]').getAttribute('href'), '/theia-icon-192.png');
  assert.equal(await page.evaluate(() => document.querySelector('link[rel="icon"]') === window.originalIcon), true);
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
});
test.afterEach(async () => {
  const errors = [];
  for (const dispose of cleanup.splice(0)) {
    try {
      await dispose();
    } catch (error) {
      errors.push(error);
    }
  }
  if (errors.length) throw new AggregateError(errors, 'Fixture cleanup failed');
});
test('attention restores the existing icon and Theia title after 0, 5, 0', async () => {
  const page = await fixture();
  await plain(page);
  await page.evaluate(() => window.count(5));
  assert.equal(await page.title(), '(5) Theia');
  await page.waitForFunction(() => document.querySelector('link[rel="icon"]').href.startsWith('data:image/png'));
  assert.equal(await page.locator('link[rel="icon"]').count(), 1);
  const pixels = await page.evaluate(async () => {
    const load = src => new Promise((resolve, reject) => {
      const image = new Image();
      image.onload = () => resolve(image);
      image.onerror = reject;
      image.src = src;
    });
    const original = await load('/theia-icon-192.png'),
      badged = await load(document.querySelector('link[rel="icon"]').href);
    const read = image => {
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 192;
      const context = canvas.getContext('2d');
      context.drawImage(image, 0, 0, 192, 192);
      return [...context.getImageData(0, 0, 192, 192).data];
    };
    return {
      original: read(original),
      badged: read(badged)
    };
  });
  assert.deepEqual(pixels.badged.slice(0, 192 * 100 * 4), pixels.original.slice(0, 192 * 100 * 4), 'The Theia eye outside the badge must retain the original pixels');
  assert.notDeepEqual(pixels.badged, pixels.original, 'The count must be drawn over the original icon');
  assert.deepEqual(await page.evaluate(() => window.badgeTexts), ['5']);
  await page.evaluate(() => window.count(100));
  assert.equal(await page.title(), '(100) Theia');
  assert.deepEqual(await page.evaluate(() => window.badgeTexts), ['5', '99']);
  await page.evaluate(() => window.count(0));
  await plain(page);
});
test('canvas failure retains the original icon and can clear attention', async () => {
  const page = await fixture();
  await page.evaluate(() => HTMLCanvasElement.prototype.getContext = () => null);
  const failed = page.waitForEvent('console', message => message.text().includes('favicon badge unavailable'));
  await page.evaluate(() => window.count(4));
  await failed;
  assert.equal(await page.title(), '(4) Theia');
  assert.equal(await page.locator('link[rel="icon"]').getAttribute('href'), '/theia-icon-192.png');
  await page.evaluate(() => window.count(0));
  await plain(page);
});
test('failed icon loading retains the original href with the attention title', async () => {
  const page = await fixture();
  await page.route('**/theia-icon-192.png', route => route.abort());
  const failed = page.waitForEvent('console', message => message.text().includes('favicon image unavailable'));
  await page.evaluate(() => window.count(2));
  await failed;
  assert.equal(await page.title(), '(2) Theia');
  assert.equal(await page.locator('link[rel="icon"]').count(), 1);
  assert.equal(await page.locator('link[rel="icon"]').getAttribute('href'), '/theia-icon-192.png');
  await page.evaluate(() => window.count(0));
  await plain(page);
});
test('a delayed icon load cannot restore a cleared badge', async () => {
  const page = await fixture();
  let release;
  const held = new Promise(resolve => release = resolve);
  await page.route('**/theia-icon-192.png', async route => {
    await held;
    await route.continue();
  });
  const request = page.waitForRequest('**/theia-icon-192.png');
  await page.evaluate(() => window.count(3));
  await request;
  await page.evaluate(() => window.count(0));
  await plain(page);
  const loaded = page.waitForEvent('console', message => message.text().includes('favicon image loaded'));
  release();
  await loaded;
  await plain(page);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

test('real workspace badges every Theia favicon candidate and restores SVG and PNG fallbacks', async () => {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  await page.setViewportSize({width: 1440, height: 900});
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(() => document.querySelector('#connect-dialog').close());
  const icons = page.locator('link[rel="icon"]');
  const originals = await icons.evaluateAll(links => links.map(link => ({href: link.getAttribute('href'), type: link.type, sizes: link.sizes.value})));
  assert.deepEqual(originals.map(link => link.href), ['/theia-icon-32.png', '/theia-icon-192.png', '/theia-favicon.svg']);
  const fs = await import('node:fs/promises');
  const path = await import('node:path');
  const directory = path.resolve('../.context/theia-logo');
  await fs.mkdir(directory, {recursive: true});
  await page.locator('.workspace-header').screenshot({path: path.join(directory, 'header.png')});
  const captureIcon = async name => {
    const href = await icons.last().getAttribute('href');
    const preview = await browser.newPage({viewport: {width: 96, height: 48}, deviceScaleFactor: 1});
    try {
      await preview.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
      await preview.setContent('<body style="margin:0;background:#20242b;display:flex;align-items:center;justify-content:space-evenly;height:48px"><img width="16" height="16"><img width="32" height="32"></body>');
      await preview.locator('img').evaluateAll((images, src) => { for (const image of images) image.src = src; }, new URL(href, page.url()).href);
      await preview.locator('img').evaluateAll(images => Promise.all(images.map(image => image.decode())));
      await preview.screenshot({path: path.join(directory, name)});
    } finally { await preview.close(); }
  };
  await captureIcon('tab-favicon-normal.png');
  await page.evaluate(async () => {
    const {AttentionTab} = await import('/workspace/attention/attention-tab.js');
    window.logoAttention = new AttentionTab();
    window.logoAttention.update(5);
  });
  await page.waitForFunction(() => [...document.querySelectorAll('link[rel="icon"]')].every(link => link.href.startsWith('data:image/png')));
  assert.equal(new Set(await icons.evaluateAll(links => links.map(link => link.href))).size, 1);
  assert.deepEqual(await icons.evaluateAll(links => links.map(link => [link.type, link.sizes.value])), Array(3).fill(['image/png', '192x192']));
  await captureIcon('tab-favicon-badge-5.png');
  await page.evaluate(() => window.logoAttention.update(0));
  assert.deepEqual(await icons.evaluateAll(links => links.map(link => ({href: link.getAttribute('href'), type: link.type, sizes: link.sizes.value}))), originals);
  assert.equal(await page.title(), 'Theia');
});
