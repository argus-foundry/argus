import assert from 'node:assert/strict';
export const frontend = process.env.WORKSPACE_TEST_URL;
export async function actorPage(browser, cleanup, html) {
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(async () => {
    await page.close();
    assert.deepEqual(errors, []);
  });
  await page.route(frontend + '/', route => route.fulfill({
    contentType: 'text/html',
    body: html
  }));
  await page.goto(frontend);
  return page;
}
