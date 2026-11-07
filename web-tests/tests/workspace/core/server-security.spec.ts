import { test } from '@playwright/test';
// Self-contained synthetic upstream. Run separately from the isolated daemon harness.

import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { readFile } from 'node:fs/promises';
import { createWorkspaceServer } from '../../../../web-workspace/dev-server.mjs';
let browser, origin, token, requests, script, html, svg, upstream, proxy;
const cleanup = [];
async function listen(server, port) {
  server.listen(port, '127.0.0.1');
  await once(server, 'listening');
}
async function close(server) {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
async function workspace() {
  const context = await browser.newContext({
    acceptDownloads: true
  });
  cleanup.push(() => context.close());
  const page = await context.newPage();
  await page.goto(origin);
  await page.evaluate(token => {
    localStorage.setItem('argus-workspace-token', token);
    document.querySelector('#connect-dialog')?.close();
  }, token);
  return {
    page,
    context
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  origin = 'http://127.0.0.1:17750';
  token = 'synthetic-remembered-master';
  requests = [];
  script = 'document.body.textContent="STOLEN="+localStorage.getItem("argus-workspace-token");fetch("/api/stolen?value="+encodeURIComponent(localStorage.getItem("argus-workspace-token")))';
  html = `<body><h1>Safe fixture heading</h1><script>${script}</script></body>`;
  svg = `<svg xmlns="http://www.w3.org/2000/svg"><text y="20">Safe SVG</text><script>document.documentElement.setAttribute("data-stolen",localStorage.getItem("argus-workspace-token"));fetch("/api/stolen")</script></svg>`;
  upstream = http.createServer((request, response) => {
    requests.push({
      url: request.url,
      headers: request.headers
    });
    if (request.url === '/frame') {
      response.writeHead(200, {
        'Content-Type': 'text/html'
      });
      return response.end(`<iframe src="${origin}/"></iframe>`);
    }
    if (request.url.includes('/artifacts/')) {
      const isSVG = request.url.includes('.svg');
      response.writeHead(200, {
        'Content-Type': isSVG ? 'image/svg+xml' : 'text/html',
        'Content-Security-Policy': "frame-ancestors 'self'",
        'X-Frame-Options': 'SAMEORIGIN'
      });
      return response.end(isSVG ? svg : html);
    }
    response.writeHead(200, {
      'Content-Type': 'application/json'
    });
    response.end(JSON.stringify({
      tasks: [],
      projects: [],
      orchestrators: [],
      freelance: []
    }));
  });
  proxy = createWorkspaceServer({
    api: 'http://127.0.0.1:17744'
  });
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
test.beforeAll(async () => {
  await listen(upstream, 17744);
  await listen(proxy, 17750);
});
for (const extension of ['html', 'svg']) test(`raw ${extension} task link cannot steal a remembered token`, async () => {
  const {
    page,
    context
  } = await workspace();
  requests.length = 0;
  await page.evaluate(async extension => {
    const {
      renderTaskLinks
    } = await import('/tasks/task-links.js');
    const host = document.createElement('div');
    host.id = 'attack-link';
    document.body.append(host);
    renderTaskLinks(host, [{
      label: 'Artifact fixture',
      url: `${location.origin}/api/tasks/task/artifacts/evil.${extension}?token=synthetic-attacker-device`
    }]);
  }, extension);
  const popupEvent = context.waitForEvent('page');
  await page.locator('#attack-link a').click();
  const popup = await popupEvent;
  await popup.waitForLoadState('load');
  const text = await popup.locator(':root').textContent();
  assert.doesNotMatch(text, /STOLEN=synthetic-remembered-master/, 'Artifact JavaScript stole the remembered token');
  assert.equal(requests.some(request => request.url.startsWith('/api/stolen')), false);
  const navigation = requests.find(request => request.url.includes('/artifacts/'));
  assert.ok(navigation);
  assert.equal(navigation.headers.authorization, undefined);
  assert.equal(navigation.headers['sec-fetch-dest'], 'document');
  const storage = await popup.evaluate(() => {
    try {
      return localStorage.getItem('argus-workspace-token');
    } catch (error) {
      return error.name;
    }
  });
  assert.equal(storage, 'SecurityError', 'Raw artifact must have an opaque sandbox origin');
});
test('same-site loopback frame cannot auto-connect with the remembered token', async () => {
  const {
    page
  } = await workspace();
  requests.length = 0;
  const messages = [];
  page.on('console', message => messages.push(message.text()));
  await page.goto('http://127.0.0.1:17744/frame');
  // A completed frame load is the browser's boundary, including a refused frame.
  const displayed = await page.locator('iframe').evaluate(frame => {
    try {
      return frame.contentDocument?.title || '';
    } catch {
      return '';
    }
  });
  const authenticated = requests.filter(request => request.headers.authorization === `Bearer ${token}`);
  assert.equal(authenticated.length, 0, 'Framed workspace used its remembered credential');
  assert.equal(displayed, '');
  assert.ok(messages.some(message => /frame-ancestors|X-Frame-Options/.test(message)));
  assert.equal(page.frames().some(frame => frame.url() === origin + '/'), false);
});
test('real Review fetch, sandboxed preview and blob download retain artifact content', async () => {
  const {
    page
  } = await workspace();
  requests.length = 0;
  await page.evaluate(async token => {
    const {
      Api
    } = await import('/core/api.js');
    const {
      TaskReview
    } = await import('/review/review.js');
    const api = new Api();
    api.setToken(token);
    const review = new TaskReview(api, () => {});
    const task = {
      id: 'task'
    };
    review.selectTask(task);
    await review.openArtifact(task, {
      filename: 'evil.html',
      type: 'html',
      name: 'HTML fixture'
    });
    document.querySelector('#review-panel').hidden = false;
    window.securityReview = review;
  }, token);
  const frame = page.locator('.artifact-preview iframe');
  assert.equal(await frame.getAttribute('sandbox'), '');
  const preview = await frame.contentFrame();
  await preview.getByText('Safe fixture heading').waitFor();
  assert.equal(await preview.locator('script').count(), 0);
  assert.equal(requests.some(request => request.url.startsWith('/api/stolen')), false);
  const downloadEvent = page.waitForEvent('download');
  await page.locator('.artifact-download').click();
  const download = await downloadEvent;
  assert.equal(download.suggestedFilename(), 'evil.html');
  assert.equal(await readFile(await download.path(), 'utf8'), html);
  const artifactRequests = requests.filter(request => request.url.includes('/artifacts/'));
  assert.equal(artifactRequests.length, 2);
  assert.ok(artifactRequests.every(request => request.headers.authorization === `Bearer ${token}`));
  await page.evaluate(() => window.securityReview.clear());
});
test.afterAll(async () => {
  await close(proxy);
  await close(upstream);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
