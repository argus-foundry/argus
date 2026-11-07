import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { refreshWorkspace } from '../_workspace-controls-fixture.ts';
import assert from 'node:assert/strict';
test('workspace responsive-live', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750',
    daemon = process.env.WORKSPACE_TEST_API || 'http://127.0.0.1:17744',
    headers = {
      Authorization: 'Bearer test-token'
    };
  const task = (await (await fetch(daemon + '/api/tasks?archived=all', {
    headers
  })).json()).tasks.find(task => task.name === 'echo-bash');
  assert.ok(task?.backend === 'bash-test' && task.worktree_path.includes('/argus-test-home-'), 'Refusing resize outside isolated harness');
  const page = await browser.newPage({
      viewport: {
        width: 1920,
        height: 1080
      }
    }),
    resizes = [],
    errors = [],
    runtime = {
      idle: true
    };
  const ownResize = r => new URL(r.url()).pathname === `/api/tasks/${task.id}/resize`;
  page.on('request', r => {
    if (ownResize(r)) resizes.push(r);
  });
  page.on('pageerror', e => errors.push(e.message));
  async function confirmedSize() {
    const response = await fetch(`${daemon}/api/tasks/${task.id}/size`, {
      headers
    });
    assert.equal(response.ok, true);
    const size = await response.json();
    assert.deepEqual(size, resizes.at(-1).postDataJSON(), 'Daemon size independently confirms requested cells');
    return size;
  }
  async function fitted(expected) {
    await page.waitForFunction(() => document.querySelector('#fit-session').dataset.fitNeeded === 'false');
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const size = await confirmedSize(),
      value = await page.evaluate(() => {
        const pane = document.querySelector('#terminal'),
          css = getComputedStyle(pane),
          screen = document.querySelector('.xterm-screen').getBoundingClientRect(),
          cache = document.querySelector('.cached-terminal'),
          viewport = cache.querySelector('.xterm-viewport'),
          bar = viewport.offsetWidth - viewport.clientWidth;
        return {
          glyphGutter: parseFloat(getComputedStyle(cache.querySelector('.xterm')).getPropertyValue('--terminal-glyph-gutter')),
          availableWidth: pane.clientWidth - parseFloat(css.paddingLeft) - parseFloat(css.paddingRight),
          availableHeight: pane.clientHeight - parseFloat(css.paddingTop) - parseFloat(css.paddingBottom),
          width: screen.width,
          height: screen.height,
          right: screen.right,
          cellEdge: viewport.getBoundingClientRect().left + viewport.clientWidth - Math.max(0, 16 - bar),
          scrollWidth: cache.scrollWidth,
          clientWidth: cache.clientWidth,
          scrollLeft: cache.scrollLeft,
          document: document.scrollingElement.scrollWidth,
          viewport: innerWidth
        };
      });
    assert.ok(value.width <= value.availableWidth + 1 && value.height <= value.availableHeight + 1, JSON.stringify(value));
    assert.ok(value.availableWidth - value.width < 21 + value.glyphGutter + value.width / size.cols && value.availableHeight - value.height < 20, 'Fill leaves only a cell plus scrollbar and glyph gutters: ' + JSON.stringify(value));
    assert.ok(value.right <= value.cellEdge - 3, 'Final cell clears the scrollbar: ' + JSON.stringify(value));
    assert.ok(value.scrollWidth <= value.clientWidth && value.scrollLeft === 0 && value.document <= value.viewport, JSON.stringify(value));
    assert.equal(resizes.length, expected);
    return value;
  }
  async function edgeProof(width) {
    const size = await confirmedSize(),
      stamp = String(Date.now()),
      proof = 'EDGE_' + stamp;
    await page.locator('#draft').fill(`printf 'EDGE_%s\\n%*sZ\\n' '${stamp}' ${size.cols - 1} ''`);
    await page.locator('#send').click();
    await page.waitForFunction(proof => document.querySelector('.xterm-accessibility-tree')?.textContent.includes(proof), proof);
    const output = await (await fetch(`${daemon}/api/tasks/${task.id}/output?bytes=1048576`, {
      headers
    })).text();
    assert.ok(output.split(/\r?\n/).includes(' '.repeat(size.cols - 1) + 'Z'), 'Real bash printed a glyph in the final cell');
    const glyphs = await page.evaluate(() => {
      const viewport = document.querySelector('.xterm-viewport'),
        cellEdge = viewport.getBoundingClientRect().left + viewport.clientWidth - Math.max(0, 16 - (viewport.offsetWidth - viewport.clientWidth));
      return [...document.querySelectorAll('.xterm-rows>div')].filter(row => row.textContent.trim() === 'Z').map(row => {
        const span = [...row.querySelectorAll('span')].findLast(span => span.textContent.endsWith('Z')),
          range = document.createRange();
        range.setStart(span.firstChild, span.firstChild.textContent.length - 1);
        range.setEnd(span.firstChild, span.firstChild.textContent.length);
        return {
          glyph: range.getBoundingClientRect().right,
          row: row.getBoundingClientRect().right,
          cellEdge
        };
      });
    });
    assert.ok(glyphs.length);
    for (const glyph of glyphs) {
      assert.ok(glyph.glyph <= glyph.row + .5, 'Final glyph must not be clipped inside a renderer row: ' + JSON.stringify(glyph));
      assert.ok(glyph.glyph <= glyph.cellEdge - 3, 'Final glyph has a scrollbar gutter: ' + JSON.stringify(glyph));
    }
    await page.locator('#terminal').screenshot({
      path: `.context/terminal-auto-edge-${width}.png`
    });
    const box = await page.locator('#terminal').boundingBox();
    await page.screenshot({
      path: `.context/terminal-auto-edge-detail-${width}.png`,
      clip: {
        x: Math.floor(box.x + box.width - 180),
        y: Math.floor(box.y),
        width: 180,
        height: Math.floor(box.height)
      }
    });
    console.log(`PASS real final-cell glyph at ${width}px with captured right gutter`);
  }
  {
    // Only runtime eligibility is controlled; size/output/resize all reach the owned bash task.
    const metadata = () => ({
      ...task,
      idle: runtime.idle,
      needs_input: false
    });
    await page.route('**/api/tasks?archived=all', route => route.fulfill({
      json: {
        tasks: [metadata()]
      }
    }));
    await page.route('**/api/tasks/' + task.id, route => route.fulfill({
      json: metadata()
    }));
    let response = page.waitForResponse(ownResize);
    await page.goto(frontend);
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await response;
    await fitted(1);
    console.log('PASS visible idle terminal automatically fits real isolated PTY geometry');
    let expected = 1;
    for (const width of [1280, 1728, 2544, 3440]) {
      response = page.waitForResponse(ownResize);
      await page.setViewportSize({
        width,
        height: 1080
      });
      await response;
      await fitted(++expected);
      await page.waitForTimeout(550);
      assert.equal(resizes.length, expected);
      console.log(`PASS settled automatic Fit at ${width}px confirms daemon cells and a visible last column`);
      await edgeProof(width);
    }
    runtime.idle = false;
    await refreshWorkspace(page);
    await page.setViewportSize({
      width: 1280,
      height: 1080
    });
    await page.waitForTimeout(650);
    assert.equal(resizes.length, expected, 'Known busy runtime must not resize automatically');
    assert.equal(await page.locator('#fit-session').getAttribute('data-fit-needed'), 'true');
    response = page.waitForResponse(ownResize);
    await page.locator('#fit-session').click();
    await response;
    await fitted(++expected);
    console.log('PASS busy sessions retain the mismatch hint and explicit Fit remains available');
    for (const width of [2560, 3440]) {
      await page.setViewportSize({
        width,
        height: 1440
      });
      for (const toggle of [null, '#toggle-tools', '#toggle-nav']) {
        if (toggle) await page.locator(toggle).click();
        await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
        await page.locator('.cached-terminal').evaluate(pane => {
          pane.scrollLeft = pane.scrollWidth;
        });
        response = page.waitForResponse(ownResize);
        await page.locator('#fit-session').click();
        await response;
        await fitted(++expected);
      }
      await page.screenshot({
        path: '.context/responsive-fit-' + width + '.png'
      });
      console.log(`PASS busy manual Fit at ${width}px after rail toggles fills cells without clipping or stale scroll`);
    }
    assert.deepEqual(errors, []);
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
