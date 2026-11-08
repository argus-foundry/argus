import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { actorPage } from '../_lifecycle-fixture.ts';
let browser;
const cleanup = [];
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
test('no-SSE fallback pauses hidden reads, wakes once, and stops on suspend', async () => {
  const page = await actorPage(browser, cleanup, `<form id="connect-form"></form><input id="token"><input id="remember-token" type="checkbox"><button id="connect-button"></button><button id="disconnect"></button><dialog id="connect-dialog"></dialog><p id="connect-error"></p><span id="connection-state"></span>`);
  await page.clock.install();
  await page.evaluate(async () => {
    const {
      WorkspaceConnection
    } = await import('/workspace/workspace-connection.js');
    window.reads = 0;
    window.hidden = false;
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => window.hidden
    });
    const api = {
      setToken() {},
      json: async path => path.startsWith('/api/tasks') ? {
        tasks: []
      } : path === '/api/projects/full' ? {
        projects: []
      } : {}
    };
    window.connection = new WorkspaceConnection(api, {
      connected() {},
      refresh() {
        window.reads++;
      }
    });
    await window.connection.connect('fixture');
  });
  await page.clock.runFor(8001);
  assert.equal(await page.evaluate(() => window.reads), 1, 'Foreground keeps the eight-second cadence');
  await page.evaluate(() => {
    window.hidden = true;
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.runFor(60001);
  assert.equal(await page.evaluate(() => window.reads), 1);
  await page.evaluate(() => {
    window.hidden = false;
    document.dispatchEvent(new Event('visibilitychange'));
  });
  assert.equal(await page.evaluate(() => window.reads), 2);
  await page.evaluate(() => {
    window.connection.suspend();
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.runFor(16001);
  assert.equal(await page.evaluate(() => window.reads), 2);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
