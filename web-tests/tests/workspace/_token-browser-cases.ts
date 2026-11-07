import assert from 'node:assert/strict';
const key = 'argus-workspace-token';
export async function rememberedToken(browser, frontend, record) {
  const page = await browser.newPage();
  const errors = [],
    logs = [],
    credentialURLs = [];
  page.on('console', message => logs.push(message.text()));
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.searchParams.has('token') && !/^\/api\/tasks\/[^/]+\/stream$/.test(url.pathname)) credentialURLs.push(url.pathname);
  });
  page.on('pageerror', error => errors.push(error.message));
  const saved = () => page.evaluate(key => localStorage.getItem(key), key);
  const login = async () => {
    await page.locator('#token').fill('test-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
  };
  try {
    await page.goto(frontend);
    assert.equal(await page.getByLabel('Remember on this device').isChecked(), true);
    await page.locator('#token').fill('wrong-test-token');
    await page.locator('#connect-button').click();
    await page.locator('#connect-error:visible').waitFor();
    assert.equal(await saved(), null);
    let release;
    const pending = new Promise(resolve => {
      release = resolve;
    });
    const hold = async route => {
      await pending;
      await route.continue();
    };
    await page.route('**/api/tasks?archived=all', hold);
    await page.locator('#token').fill('test-token');
    const requested = page.waitForRequest('**/api/tasks?archived=all');
    await page.locator('#connect-button').click();
    await requested;
    assert.equal(await saved(), null, 'Pending authentication must not persist a token');
    release();
    await page.locator('#disconnect:visible').waitFor();
    await page.unroute('**/api/tasks?archived=all', hold);
    assert.equal(await saved(), 'test-token');
    await page.locator('#draft').fill('memory-only draft');
    await page.reload();
    await page.locator('#disconnect:visible').waitFor();
    assert.equal(await page.locator('#draft').inputValue(), '');
    assert.doesNotMatch(await page.evaluate(() => JSON.stringify({
      ...localStorage,
      ...sessionStorage
    })), /memory-only draft/);
    assert.equal(await saved(), 'test-token');
    record('remembered token survives refresh while drafts remain memory-only');
    await page.locator('#disconnect').click();
    await page.reload();
    await page.locator('#connect-dialog:visible').waitFor();
    assert.equal(await saved(), null);
    assert.equal(await page.locator('#disconnect').isVisible(), false);
    record('disconnect removes saved credentials and refresh requires login');
    for (const status of [401, 403]) {
      await page.evaluate(({
        key
      }) => localStorage.setItem(key, 'rejected-token'), {
        key
      });
      const reject = route => route.fulfill({
        status,
        json: {}
      });
      await page.route('**/api/tasks?archived=all', reject);
      await page.reload();
      await page.locator('#connect-error:visible').waitFor();
      assert.match(await page.locator('#connect-error').textContent(), /Saved token was rejected/);
      assert.equal(await saved(), null);
      assert.equal(await page.locator('#token').inputValue(), '');
      await page.unroute('**/api/tasks?archived=all', reject);
    }
    record('401 and 403 reject saved tokens and return to login');
    for (const failure of ['server', 'network']) {
      await page.evaluate(key => localStorage.setItem(key, 'test-token'), key);
      const fail = route => failure === 'server' ? route.fulfill({
        status: 503,
        json: {}
      }) : route.abort('failed');
      await page.route('**/api/tasks?archived=all', fail);
      await page.reload();
      await page.locator('#connect-error:visible').waitFor();
      assert.match(await page.locator('#connect-error').textContent(), /Could not connect/);
      assert.equal(await saved(), 'test-token');
      await page.unroute('**/api/tasks?archived=all', fail);
      await page.locator('#connect-button').click();
      await page.locator('#disconnect:visible').waitFor();
      await page.locator('#disconnect').click();
    }
    record('server and network failures retain saved credentials for explicit retry');
    await page.getByLabel('Remember on this device').uncheck();
    await login();
    assert.equal(await saved(), null);
    await page.reload();
    await page.locator('#connect-dialog:visible').waitFor();
    assert.equal(await page.locator('#disconnect').isVisible(), false);
    record('unchecked Remember keeps credentials in memory only');
    await page.addInitScript(() => Object.defineProperty(window, 'localStorage', {
      get() {
        throw new DOMException('Storage blocked', 'SecurityError');
      }
    }));
    await page.reload();
    await login();
    await page.reload();
    await page.locator('#connect-dialog:visible').waitFor();
    assert.equal(await page.locator('#disconnect').isVisible(), false);
    assert.deepEqual(errors, []);
    assert.doesNotMatch(logs.join('\n'), /test-token|rejected-token|memory-only draft/);
    assert.deepEqual(credentialURLs, []);
    record('blocked browser storage falls back to memory-only connection');
  } finally {
    await page.close();
  }
}
