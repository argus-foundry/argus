import assert from 'node:assert/strict';
export async function renderingNegativeControl(browser, frontend, task) {
  const page = await browser.newPage({
    viewport: {
      width: 1440,
      height: 900
    }
  });
  try {
    await page.route('**/terminal/terminal-output.js', async route => {
      const response = await route.fetch();
      const source = await response.text();
      assert.ok(source.includes('write(bytes) {'), 'The output mutation must actually be applied');
      await route.fulfill({
        contentType: 'text/javascript',
        body: source.replace('write(bytes) {', 'write(bytes) { return Promise.resolve();')
      });
    });
    await page.goto(frontend + '?task=' + encodeURIComponent(task.id));
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
    const suffix = String(Date.now());
    const proof = 'NO_RENDER_' + suffix;
    await page.locator('#draft').fill("printf 'NO_RENDER_%s\\n' '" + suffix + "'");
    await page.locator('#send').click();
    await page.waitForFunction(async ({
      id,
      expected
    }) => {
      const response = await fetch(`/api/tasks/${id}/output?bytes=1048576`, {
        headers: {
          Authorization: 'Bearer test-token'
        }
      });
      return (await response.text()).split(/\r?\n/).includes(expected);
    }, {
      id: task.id,
      expected: proof
    });
    let rejected = false;
    try {
      await page.waitForFunction(expected => document.querySelector('.xterm-accessibility-tree')?.textContent.includes(expected), proof, {
        timeout: 750
      });
    } catch (error) {
      if (error.name !== 'TimeoutError') throw error;
      rejected = true;
    }
    assert.equal(rejected, true, 'The browser assertion must fail when terminal output rendering is disabled');
  } finally {
    await page.close();
  }
}
