import { test } from '@playwright/test';
import { resetServer } from '../../../_helpers';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
let browser, tasks, roles, near, box;
const cleanup = [];
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  tasks = ['parent', 'child', 'sibling', 'leaf', 'grandchild'].map(id => ({
    id,
    name: id,
    project: 'fixture',
    status: 'in_progress',
    live: false,
    pinned: id === 'parent'
  }));
  roles = [{
    role_id: 1,
    kind: 'coordinator',
    task_id: 'parent',
    live: true,
    status: 'working'
  }, {
    role_id: 2,
    kind: 'worker',
    task_id: 'child',
    live: true,
    status: 'ready'
  }, {
    role_id: 3,
    kind: 'worker',
    task_id: 'sibling',
    live: true,
    status: 'working'
  }];
  near = (a, b, message) => assert.ok(Math.abs(a - b) <= 1, `${message}: ${a} / ${b}`);
  box = locator => locator.evaluate(element => {
    const b = element.getBoundingClientRect();
    return {
      x: b.x,
      y: b.y,
      width: b.width,
      height: b.height,
      right: b.right,
      center: b.y + b.height / 2
    };
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
for (const width of [1280, 390]) for (const light of [false, true]) test(`rail nesting geometry at ${width} ${light ? 'light' : 'dark'}`, async () => {
  const page = await browser.newPage({
    viewport: {
      width,
      height: 900
    },
    reducedMotion: 'reduce'
  });
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    let json = {};
    if (path === '/api/tasks') json = {
      tasks
    };else if (path === '/api/projects/full') json = {
      projects: [{
        name: 'fixture'
      }, {
        name: 'empty'
      }]
    };else if (path === '/api/hera') json = {
      orchestrators: [{
        id: 1,
        roles
      }, {
        id: 2,
        bridge_parent_orch_id: 1,
        bridge_parent_role_id: 2,
        roles: [{
          role_id: 4,
          kind: 'coordinator',
          task_id: 'child',
          live: true,
          status: 'working'
        }, {
          role_id: 5,
          kind: 'worker',
          task_id: 'grandchild',
          live: true,
          status: 'ready'
        }]
      }]
    };else if (path === '/api/backends') json = {
      backends: []
    };else if (path === '/api/accounts') json = [];else if (path.endsWith('/size')) json = {
      cols: 80,
      rows: 24
    };else if (path.endsWith('/output')) return route.fulfill({
      body: 'fixture\r\n'
    });else if (path.endsWith('/stream') || path === '/api/events') return route.fulfill({
      contentType: 'text/event-stream',
      body: ':fixture\n\n'
    });else json = tasks.find(task => task.id === path.split('/')[3]) || {};
    return route.fulfill({
      json
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL);
  if (light) await page.evaluate(() => {
    for (const [key, value] of Object.entries({
      '--background': '#f7f5f2',
      '--surface': '#ffffff',
      '--raised': '#eee9e2',
      '--text': '#201c18',
      '--secondary': '#655f58',
      '--border': '#c3bbb2'
    })) document.documentElement.style.setProperty(key, value);
  });
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.task-row').first().waitFor({
    state: 'attached'
  });
  if (width < 700) await page.locator('#toggle-nav').click();
  const project = page.locator('.project-group[data-project="fixture"]');
  const row = id => project.locator(`.task-row[data-task-id="${id}"]`);
  const icon = id => row(id).locator('.task-status-icon');
  const parent = await box(icon('parent')),
    child = await box(icon('child')),
    sibling = await box(icon('sibling')),
    leaf = await box(icon('leaf'));
  const name = await box(project.locator('.project-name')),
    caret = await box(project.locator('.project-caret'));
  assert.ok(parent.x > caret.x, 'root task icon must sit past project chevron');
  assert.ok(parent.x - caret.x <= 20, `root task icon offset ${parent.x - caret.x} stays within 20px of project chevron`);
  const avatar = await box(project.locator('.project-avatar'));
  near(parent.x, avatar.x, 'root task icon aligns with project avatar');
  near((await box(row('parent').locator('.task-name'))).x, name.x, 'task name aligns with project name');
  console.log(`Root offset ${parent.x - caret.x}px; avatar delta ${parent.x - avatar.x}px; descendant step ${child.x - parent.x}px`);
  const indent = await project.evaluate(element => parseFloat(getComputedStyle(element).getPropertyValue('--rail-indent')));
  assert.equal(indent, 12);
  assert.ok(child.x - parent.x >= 10 && child.x - parent.x <= 14, 'level step stays between 10 and 14px');
  near(child.x, parent.x + indent, 'one indent per level');
  near(child.x, sibling.x, 'siblings share an icon column');
  near((await box(icon('grandchild'))).x, child.x + indent, 'second nesting step stays one indent');
  near(parent.x, leaf.x, 'root leaf reserves disclosure slot');
  for (const id of ['parent', 'child', 'sibling', 'leaf', 'grandchild']) {
    const i = await box(icon(id)),
      r = await box(row(id));
    near(i.center, r.center, 'task icon vertically centered');
    assert.equal(i.width, 16);
    assert.equal(i.height, 16);
    near(r.right, (await box(project)).right, 'highlight reaches project right edge');
  }
  const disclosure = project.locator('.task-disclosure').first();
  const chevron = id => project.locator(`.task-disclosure[data-parent-task-id="${id}"] > .task-caret`);
  for (const id of ['parent', 'child']) {
    const glyph = await box(chevron(id)),
      r = await box(row(id)),
      i = await box(icon(id)),
      text = await box(row(id).locator('.task-name'));
    near(glyph.center, text.center, 'chevron centered on text line');
    assert.equal(glyph.width, 12);
    assert.equal(glyph.height, 12);
    assert.ok(i.x - glyph.right >= 3, `chevron leaves icon gap: ${i.x - glyph.right}`);
    near(i.x - glyph.right, 4, 'chevron has 4px icon gap');
    assert.ok(glyph.x >= r.x + 3 && glyph.right <= r.right && glyph.y >= r.y && glyph.y + glyph.height <= r.y + r.height, 'chevron inside background and beyond accent bar');
    assert.equal(await chevron(id).locator('path').getAttribute('d'), await project.locator('.project-chevron path').getAttribute('d'));
  }
  await mkdir('.context/chevron', {
    recursive: true
  });
  await page.locator('#navigation').screenshot({
    path: `.context/chevron/unselected-${width}-${light ? 'light' : 'dark'}.png`
  });
  const previousSelection = await page.locator('.task-row[aria-current=page]').evaluateAll(rows => rows[0]?.dataset.taskId || null);
  await chevron('child').click();
  assert.equal(await row('grandchild').isVisible(), false);
  assert.equal(await row('child').getAttribute('aria-current'), null, 'unselected disclosure does not select its row');
  if (previousSelection) assert.equal(await page.locator('.task-row[aria-current=page]').getAttribute('data-task-id'), previousSelection);
  await chevron('child').click();
  await row('parent').click();
  if (width < 700) await page.locator('#toggle-nav').click();
  assert.equal(await row('parent').getAttribute('aria-current'), 'page');
  await page.locator('#navigation').screenshot({
    path: `.context/chevron/selected-${width}-${light ? 'light' : 'dark'}.png`
  });
  const selectedBox = await box(row('parent')),
    selectedGlyph = await box(chevron('parent'));
  assert.ok(selectedGlyph.x >= selectedBox.x + 3, 'selected accent never intersects chevron');
  assert.equal(await disclosure.getAttribute('data-tooltip'), 'Collapse 2 tasks');
  await chevron('parent').click();
  assert.equal(await row('child').isVisible(), false);
  assert.equal(await disclosure.getAttribute('data-tooltip'), 'Expand 2 tasks');
  assert.equal(await row('parent').getAttribute('aria-current'), 'page');
  await row('leaf').click();
  if (width < 700) await page.locator('#toggle-nav').click();
  await disclosure.focus();
  await page.keyboard.press('ArrowRight');
  assert.equal(await row('child').isVisible(), true);
  await page.keyboard.press('ArrowLeft');
  assert.equal(await row('child').isVisible(), false);
  await page.keyboard.press('Enter');
  assert.equal(await row('child').isVisible(), true);
  await page.keyboard.press('Space');
  assert.equal(await row('child').isVisible(), false);
  await page.keyboard.press('ArrowRight');
  assert.equal(await row('leaf').getAttribute('aria-current'), 'page');
  assert.equal(await page.locator('#task-title').textContent(), 'leaf');
  assert.equal(await row('parent').getAttribute('aria-current'), null);
  const tags = await Promise.all(['parent', 'child', 'sibling', 'grandchild'].map(async id => {
    const b = await box(row(id).locator('.task-role-tag'));
    near(b.center, (await box(row(id))).center, 'pill vertically centered');
    return b;
  }));
  for (const tag of tags) near(tag.right, tags[0].right, 'status pills share right edge even with pinned task');
  await project.locator('.project-header').hover();
  const actions = await project.locator('.project-action').all();
  assert.equal(actions.length, 2);
  const actionBoxes = await Promise.all(actions.map(box));
  const header = await box(project.locator('.project-name'));
  for (let i = 0; i < actions.length; i++) {
    near(actionBoxes[i].center, header.center, 'project action centered on name');
    assert.equal(actionBoxes[i].width, 24);
    assert.equal(actionBoxes[i].height, 24);
    const glyph = await box(actions[i].locator('svg'));
    assert.equal(glyph.width, 16);
    assert.equal(glyph.height, 16);
    assert.equal(await actions[i].locator('svg').evaluate(icon => getComputedStyle(icon).strokeWidth), '1.6px');
    near(glyph.center, header.center, 'glyph centered');
  }
  near((await box(page.locator('.project-group[data-project="empty"] .project-name'))).x, name.x, 'empty project preserves gutter');
  await row('child').click();
  assert.equal(await row('child').getAttribute('aria-current'), 'page');
  if (width < 700) await page.locator('#toggle-nav').click();
  await mkdir('.context/rail-indent', {
    recursive: true
  });
  await project.locator('.project-header').hover();
  await page.locator('#navigation').screenshot({
    path: `.context/rail-indent/${width}-${light ? 'light' : 'dark'}.png`
  });
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
