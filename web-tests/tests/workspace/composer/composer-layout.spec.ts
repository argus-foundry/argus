import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [{
        id: 'a',
        name: 'Composer task',
        backend: 'codex',
        status: 'in_progress',
        live: true,
        project: 'p'
      }]
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: []
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: []
    }
  }));
  await page.route('**/api/tasks/a/raw', route => route.fulfill({
    json: {
      id: 'a',
      backend: 'codex',
      model: 'gpt-6.1-sol',
      account: 'work'
    }
  }));
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().click();
  await page.waitForFunction(() => document.querySelector('#composer-label').textContent.includes('gpt-6.1-sol'));
  return page;
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
test('composer has a quiet draft area, read-only identity and ordered bottom actions', async () => {
  const page = await fixture();
  assert.equal(await page.locator('#composer-label').textContent(), 'codex · gpt-6.1-sol');
  assert.equal(await page.locator('#draft').evaluate(field => field.placeholder), '');
  assert.equal(await page.locator('#composer-label button, #composer-label select').count(), 0);
  assert.equal(await page.locator('.attachment-status').isVisible(), false);
  assert.equal(await page.locator('#send').getAttribute('aria-label'), 'Send message');
  const boxes = await Promise.all(['#draft', '#composer-label', '#context-toggle', '#attach-files', '#send'].map(id => page.locator(id).boundingBox()));
  assert.ok(boxes[0].height >= 80);
  assert.ok(boxes[0].y < boxes[1].y);
  for (let i = 2; i < boxes.length; i++) assert.ok(boxes[i].x > boxes[i - 1].x);
  await page.screenshot({
    path: '.context/composer-layout.png'
  });
});
test('plus opens the native multiple-file picker and reuses attachment uploads', async () => {
  const page = await fixture();
  await page.evaluate(async () => {
    const {
      TaskComposer
    } = await import('/composer/composer.js');
    window.pickerUploads = [];
    window.pickerComposer = new TaskComposer(async () => {}, {
      json: async (path, options) => {
        if (!options?.body) return {};
        window.pickerUploads.push({
          path,
          files: options.body.getAll('files').map(file => file.name)
        });
        return {
          paths: ['./.context/picker.txt']
        };
      }
    });
    window.pickerComposer.select({
      id: 'picker',
      backend: 'codex'
    });
    window.pickerComposer.setInteractive(true);
  });
  const chooser = page.waitForEvent('filechooser');
  await page.locator('#attach-files').click();
  const picker = await chooser;
  assert.equal(picker.isMultiple(), true);
  await picker.setFiles({
    name: 'picker.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('chosen')
  });
  await page.waitForFunction(() => window.pickerUploads.length === 1, null, {
    timeout: 5000
  });
  await page.waitForFunction(() => window.pickerComposer.attachments.store.ready('picker').length === 1, null, {
    timeout: 5000
  });
  assert.deepEqual(await page.evaluate(() => window.pickerUploads), [{
    path: '/api/tasks/picker/upload',
    files: ['picker.txt']
  }]);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
