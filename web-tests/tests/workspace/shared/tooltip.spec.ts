import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
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
test('delegated tooltip lifecycle, safe content, single bubble and viewport edges', async () => {
  const page = await browser.newPage({
    viewport: {
      width: 390,
      height: 400
    }
  });
  cleanup.push(() => page.close());
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(async () => {
    document.querySelectorAll('dialog').forEach(dialog => dialog.close());
    document.body.replaceChildren();
    const {
      installTooltips
    } = await import('/shared/tooltip.js');
    installTooltips();
    installTooltips();
    for (const [id, left, top] of [['a', 0, 0], ['b', 360, 370]]) {
      const button = document.createElement('button');
      button.id = id;
      button.textContent = '+';
      button.style.cssText = `position:fixed;left:${left}px;top:${top}px;width:30px;height:30px`;
      button.dataset.tooltip = '<img src=x> example';
      button.dataset.tooltipShortcut = '⌥⇧A';
      button.setAttribute('aria-label', 'Example');
      button.setAttribute('aria-describedby', 'existing');
      document.body.append(button);
    }
  });
  const bubble = page.locator('[role="tooltip"]'),
    a = page.locator('#a'),
    b = page.locator('#b');
  await a.hover();
  await page.waitForTimeout(150);
  assert.equal(await bubble.isVisible(), false);
  await bubble.waitFor({
    state: 'visible'
  });
  assert.equal(await bubble.locator('img').count(), 0);
  assert.match(await bubble.textContent(), /⌥⇧A/);
  assert.match(await a.getAttribute('aria-describedby'), /existing workspace-tooltip/);
  const inside = async () => {
    const box = await bubble.boundingBox();
    assert.ok(box.x >= 0 && box.y >= 0 && box.x + box.width <= 390 && box.y + box.height <= 400);
  };
  await inside();
  await page.keyboard.press('Escape');
  assert.equal(await bubble.isVisible(), false);
  assert.equal(await a.getAttribute('aria-describedby'), 'existing');
  await page.mouse.move(150, 150);
  await b.hover();
  await bubble.waitFor({
    state: 'visible'
  });
  await inside();
  assert.equal(await bubble.count(), 1);
  await page.mouse.move(150, 150);
  await bubble.waitFor({
    state: 'hidden'
  });
  await page.keyboard.press('Tab');
  await a.focus();
  await bubble.waitFor({
    state: 'visible',
    timeout: 300
  });
  await a.evaluate(element => element.blur());
  await bubble.waitFor({
    state: 'hidden'
  });
  await a.hover();
  await page.mouse.move(150, 150);
  await page.waitForTimeout(450);
  assert.equal(await bubble.isVisible(), false);
  for (const event of ['scroll', 'click']) {
    await a.hover();
    await bubble.waitFor({
      state: 'visible'
    });
    await page.evaluate(event => document.dispatchEvent(new Event(event)), event);
    assert.equal(await bubble.isVisible(), false);
    await page.mouse.move(150, 150);
  }
});
test('modal top layer, theme tokens, reduced motion and disposal', async () => {
  const page = await browser.newPage({
    viewport: {
      width: 390,
      height: 500
    },
    reducedMotion: 'reduce'
  });
  cleanup.push(() => page.close());
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(async () => {
    const {
      installTooltips
    } = await import('/shared/tooltip.js');
    window.disposeTooltips = installTooltips();
    const button = document.createElement('button');
    button.id = 'modal-tooltip';
    button.textContent = '+';
    button.dataset.tooltip = 'Modal control';
    button.setAttribute('aria-label', 'Modal control');
    document.querySelectorAll('dialog').forEach(dialog => dialog.close());
    const modal = document.createElement('dialog');
    modal.append(button);
    document.body.append(modal);
    modal.showModal();
    document.documentElement.style.setProperty('--raised', 'rgb(245, 240, 230)');
    document.documentElement.style.setProperty('--text', 'rgb(30, 25, 20)');
  });
  await page.locator('#modal-tooltip').hover();
  const bubble = page.locator('[role=tooltip]');
  await bubble.waitFor({
    state: 'visible'
  });
  assert.equal(await bubble.evaluate(element => element.parentElement.matches('dialog[open]')), true);
  assert.deepEqual(await bubble.evaluate(element => {
    const style = getComputedStyle(element);
    return [style.backgroundColor, style.color, style.animationName];
  }), ['rgb(245, 240, 230)', 'rgb(30, 25, 20)', 'none']);
  const box = await bubble.boundingBox();
  assert.ok(box.x >= 0 && box.y >= 0 && box.x + box.width <= 390 && box.y + box.height <= 500);
  await page.evaluate(() => window.dispatchEvent(new Event('resize')));
  assert.equal(await bubble.isVisible(), false);
  await page.mouse.move(0, 0);
  await page.locator('#modal-tooltip').hover();
  await bubble.waitFor({
    state: 'visible'
  });
  await page.evaluate(() => window.disposeTooltips());
  assert.equal(await bubble.count(), 0);
  assert.equal(await page.locator('#modal-tooltip').getAttribute('aria-describedby'), null);
});
test('nested owners transition child to parent and retain the bubble within one owner', async () => {
  const page = await browser.newPage({
    viewport: {
      width: 600,
      height: 400
    }
  });
  cleanup.push(() => page.close());
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(() => {
    document.querySelectorAll('dialog').forEach(dialog => dialog.close());
    document.body.replaceChildren();
    const row = document.createElement('button');
    row.id = 'path-owner';
    row.dataset.tooltip = 'src/example.js';
    row.setAttribute('aria-label', 'Open src/example.js');
    row.style.cssText = 'position:fixed;left:100px;top:100px;padding:20px;display:flex;gap:30px';
    const counts = document.createElement('span');
    counts.id = 'count-owner';
    counts.dataset.tooltip = '3 additions';
    counts.setAttribute('aria-label', '3 additions');
    counts.textContent = '+3';
    const path = document.createElement('span');
    path.id = 'path-text';
    path.textContent = 'example.js';
    const suffix = document.createElement('span');
    suffix.id = 'path-suffix';
    suffix.textContent = 'file';
    row.append(counts, path, suffix);
    document.body.append(row);
  });
  const bubble = page.locator('[role=tooltip]');
  const expectLabel = async label => {
    await page.waitForFunction(label => {
      const bubble = document.querySelector('[role=tooltip]');
      return bubble && !bubble.hidden && bubble.textContent === label;
    }, label, {
      timeout: 2000
    });
  };
  await page.locator('#count-owner').hover();
  await expectLabel('3 additions');
  await page.locator('#path-text').hover();
  await expectLabel('src/example.js');
  assert.equal(await page.locator('#count-owner').getAttribute('aria-describedby'), null);
  assert.equal(await page.locator('#path-owner').getAttribute('aria-describedby'), 'workspace-tooltip');
  await page.locator('#path-suffix').hover();
  assert.equal(await bubble.isVisible(), true);
  assert.equal(await bubble.textContent(), 'src/example.js');
  await page.locator('#count-owner').hover();
  await expectLabel('3 additions');
  assert.equal(await page.locator('#path-owner').getAttribute('aria-describedby'), null);
  assert.equal(await bubble.count(), 1);
  await page.mouse.move(20, 20);
  await bubble.waitFor({
    state: 'hidden'
  });
});
test('redundant labels are suppressed, clipped task names are revealed and shortcuts stand alone', async () => {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(async () => {
    document.querySelectorAll('dialog').forEach(dialog => dialog.close());
    document.body.replaceChildren();
    const {
      compactTaskRow
    } = await import('/workspace/rail/task-row.js');
    const row = compactTaskRow({
      task: {
        id: 'fixture',
        name: 'A very long task name with useful detail'
      },
      select() {}
    });
    row.id = 'task';
    row.style.width = '120px';
    document.body.append(row);
    const picker = document.createElement('select');
    picker.id = 'picker';
    picker.style.width = '100px';
    const option = document.createElement('option');
    option.textContent = 'A long selected task name';
    picker.append(option);
    picker.dataset.tooltip = option.textContent;
    picker.dataset.tooltipOverflow = '';
    document.body.append(picker);
    for (const [id, text, label, shortcut] of [['system', '  System \n settings  ', 'System settings', ''], ['shortcut', 'Add task', 'Add task', '⌥⇧A'], ['icon', '+', 'Add task', '']]) {
      const button = document.createElement('button');
      button.id = id;
      button.textContent = text;
      button.dataset.tooltip = label;
      if (shortcut) button.dataset.tooltipShortcut = shortcut;
      document.body.append(button);
    }
  });
  const bubble = page.getByRole('tooltip');
  await page.locator('#system').hover();
  await page.waitForTimeout(500);
  assert.equal(await bubble.isVisible(), false);
  assert.deepEqual(await page.locator('#task').evaluate(row => [row.scrollWidth <= row.clientWidth, row.querySelector('.task-name').scrollWidth > row.querySelector('.task-name').clientWidth]), [true, true]);
  await page.locator('#task .task-name').hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), 'A very long task name with useful detail');
  await page.mouse.move(0, 0);
  await page.locator('#task').evaluate(row => row.style.width = '800px');
  await page.locator('#task .task-name').hover();
  await page.waitForTimeout(500);
  assert.equal(await bubble.isVisible(), false);
  await page.locator('#shortcut').hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), '⌥⇧A');
  await page.locator('#icon').hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), 'Add task');
  await page.locator('#picker').hover();
  await bubble.waitFor();
  assert.equal(await bubble.textContent(), 'A long selected task name');
  await page.mouse.move(0, 0);
  await page.locator('#picker').evaluate(element => element.style.width = '800px');
  await page.locator('#picker').hover();
  await page.waitForTimeout(500);
  assert.equal(await bubble.isVisible(), false);
});
test('fully visible ws-composer-polish rail name stays quiet beside its worker status pill', async () => {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.evaluate(async () => {
    document.querySelectorAll('dialog').forEach(dialog => dialog.close());
    document.body.replaceChildren();
    const {
      compactTaskRow
    } = await import('/workspace/rail/task-row.js');
    const row = compactTaskRow({
      task: {
        id: 'composer',
        name: 'ws-composer-polish'
      },
      role: {
        kind: 'worker',
        status: 'working',
        live: true
      },
      select() {}
    });
    row.id = 'exact-rail';
    row.style.width = '300px';
    document.body.append(row);
  });
  const row = page.locator('#exact-rail'),
    name = row.locator('.task-name'),
    bubble = page.getByRole('tooltip');
  assert.equal(await name.evaluate(element => element.scrollWidth <= element.clientWidth), true);
  assert.match(await row.textContent(), /stopped/);
  await name.hover();
  await page.waitForTimeout(500);
  assert.equal(await bubble.isVisible(), false);
  await page.mouse.move(0, 0);
  await row.evaluate(element => element.style.width = '210px');
  assert.equal(await name.evaluate(element => element.scrollWidth > element.clientWidth), true);
  await name.hover();
  await bubble.waitFor({
    timeout: 2000
  });
  assert.equal(await bubble.textContent(), 'ws-composer-polish');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
