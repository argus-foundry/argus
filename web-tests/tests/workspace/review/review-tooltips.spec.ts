import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
import assert from 'node:assert/strict';
import { auditIconTooltips } from '../shared/_icon-tooltip-audit.ts';
let browser;
const cleanup = [];
async function fixture() {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.goto(process.env.WORKSPACE_TEST_URL);
  await page.evaluate(() => document.querySelector('#connect-dialog').close());
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
test('Changes icon controls, full paths, statuses and exact counts expose shared tooltip attributes', async () => {
  const page = await fixture();
  const result = await page.evaluate(async () => {
    const {
      TaskChanges
    } = await import('/tasks/changes/task-changes.js');
    const abort = new AbortController(),
      panel = document.querySelector('#tool-panel');
    const changes = new TaskChanges({
      api: {
        json: async () => ({
          Diff: ''
        })
      },
      task: {
        id: 'fixture'
      },
      panel,
      signal: abort.signal,
      openDiff: () => {}
    });
    changes.files = ['A', 'M', 'D', '??', 'R'].map((status, i) => ({
      path: `long/directory/file-${i}.js`,
      status
    }));
    changes.totals = {
      added: 12,
      removed: 3
    };
    changes.draw();
    changes.counts.results.set('long/directory/file-0.js', {
      added: 8,
      removed: 2
    });
    changes.updateCounts();
    const attributes = selector => [...panel.querySelectorAll(selector)].map(node => ({
      tooltip: node.dataset.tooltip,
      label: node.getAttribute('aria-label'),
      title: node.getAttribute('title')
    }));
    const result = {
      icons: attributes('.changes-view-toggle,.changes-refresh,.file-more'),
      paths: attributes('.file-row'),
      statuses: attributes('.file-status'),
      counts: attributes('.file-counts'),
      total: attributes('.changes-totals')
    };
    panel.querySelector('.changes-view-toggle').click();
    result.tree = attributes('.changes-view-toggle');
    result.folders = attributes('.file-folder');
    abort.abort();
    return result;
  });
  assert.equal(result.icons.length, 6);
  assert.equal(result.paths.length, 5);
  assert.equal(result.statuses.length, 5);
  assert.equal(result.counts.length, 5);
  assert.ok(result.folders.length > 0);
  assert.equal(result.folders.at(-1).tooltip, 'long/directory');
  for (const item of [...result.icons, ...result.paths, ...result.statuses, ...result.counts, ...result.total, ...result.tree]) {
    assert.ok(item.tooltip);
    assert.ok(item.label);
    assert.equal(item.title, null);
  }
  assert.deepEqual(result.statuses.map(item => item.tooltip), ['Added', 'Modified', 'Deleted', 'Untracked', 'Renamed']);
  assert.equal(result.counts[0].tooltip, '8 lines added, 2 removed');
  assert.equal(result.total[0].tooltip, '12 lines added, 3 removed');
  assert.equal(result.paths[0].tooltip, 'long/directory/file-0.js');
  assert.equal(result.tree[0].tooltip, 'Show flat view');
});
test('task menu triggers and truncated review/artifact names carry accessible full values', async () => {
  const page = await fixture();
  const result = await page.evaluate(async () => {
    const {
        taskMenu
      } = await import('/workspace/rail/task-menu.js'),
      {
        renderDiff
      } = await import('/review/review-diff.js'),
      {
        TaskTools
      } = await import('/tasks/tools.js'),
      {
        TaskReview
      } = await import('/review/review.js');
    const task = {
        id: 'fixture',
        name: 'Full task name'
      },
      [trigger, menu] = taskMenu(task, {
        settings: () => {}
      });
    document.body.append(trigger, menu);
    renderDiff(document.querySelector('#review-panel'), {
      path: 'full/path/to/file.js',
      mode: 'unified',
      diff: ''
    }, () => {});
    const diff = document.querySelector('.review-title');
    const tools = new TaskTools({}, {});
    tools.task = task;
    tools.drawArtifacts([{
      name: 'Long artifact name',
      filename: 'full/artifact.txt',
      type: 'text',
      size: 1
    }]);
    const artifact = document.querySelector('.artifact-row strong');
    const review = new TaskReview({
      request: async () => new Response('text')
    }, () => {});
    review.task = task;
    await review.openArtifact(task, {
      name: 'Long artifact name',
      filename: 'full/artifact.txt',
      type: 'text',
      size: 1
    });
    const attrs = node => ({
      tooltip: node.dataset.tooltip,
      label: node.getAttribute('aria-label'),
      title: node.getAttribute('title')
    });
    return {
      trigger: attrs(trigger),
      diff: attrs(diff),
      artifact: attrs(artifact),
      preview: attrs(document.querySelector('.review-title')),
      labeled: [...document.querySelectorAll('.review-toolbar button')].map(node => node.dataset.tooltip)
    };
  });
  assert.deepEqual(result.trigger, {
    tooltip: 'Task actions for Full task name',
    label: 'Task actions for Full task name',
    title: null
  });
  assert.equal(result.diff.tooltip, 'full/path/to/file.js');
  assert.equal(result.artifact.tooltip, 'Long artifact name');
  assert.equal(result.preview.tooltip, 'Long artifact name');
  assert.deepEqual(result.labeled, [undefined]);
});
test('split picker and roster preserve full names while labeled dialogs avoid redundant tooltips', async () => {
  const page = await fixture();
  const result = await page.evaluate(async () => {
    const {
        SplitWorkspace
      } = await import('/workspace/split/split-workspace.js'),
      {
        confirmDialog
      } = await import('/shared/confirm-dialog.js'),
      {
        WorkspaceShortcuts
      } = await import('/workspace/shortcuts/shortcuts.js');
    const split = new SplitWorkspace(() => null),
      tasks = [{
        id: 'c',
        name: 'Coordinator',
        project: 'p'
      }, {
        id: 'w',
        name: 'Full worker name',
        project: 'p'
      }];
    split.enabled = true;
    split.select(tasks[0], tasks, {
      orchestrators: [{
        id: 1,
        name: 'Full coordinator name',
        roles: [{
          role_id: 1,
          kind: 'coordinator',
          task_id: 'c',
          live: true
        }, {
          role_id: 2,
          kind: 'worker',
          task_id: 'w',
          live: true,
          status: 'working'
        }]
      }]
    });
    const attrs = node => ({
      tooltip: node.dataset.tooltip,
      label: node.getAttribute('aria-label'),
      title: node.getAttribute('title')
    });
    const result = {
      picker: attrs(split.picker),
      closedRosterRows: split.rosterRows.querySelectorAll('.task-row').length
    };
    const confirmation = confirmDialog({
      message: 'Test'
    });
    result.confirm = [...confirmation.dialog.querySelectorAll('button')].map(node => node.dataset.tooltip);
    confirmation.dialog.querySelector('button').click();
    const shortcuts = new WorkspaceShortcuts({}, () => null);
    shortcuts.showHelp();
    result.close = [...shortcuts.dialog.querySelectorAll('button')].map(node => ({
      text: node.textContent,
      tooltip: node.dataset.tooltip
    }));
    return result;
  });
  assert.deepEqual(result.picker, {
    tooltip: 'p / Full worker name',
    label: 'Second task',
    title: null
  });
  assert.equal(result.closedRosterRows, 0);
  await page.getByRole('dialog', { name: 'Keyboard shortcuts' }).getByRole('button', { name: 'Close', exact: true }).click();
  await page.locator('#split-roster > summary').click();
  const worker = page.locator('#split-roster .task-row[data-task-id="w"]');
  await worker.waitFor();
  assert.equal(await worker.getAttribute('data-tooltip'), 'Full worker name');
  assert.equal(await worker.getAttribute('aria-label'), 'Full worker name, Session offline, Agent working');
  assert.equal(await worker.getAttribute('title'), null);
  assert.deepEqual(result.confirm, [undefined, undefined]);
  assert.deepEqual(result.close, [{
    text: 'Close',
    tooltip: undefined
  }]);
});
test('line count tooltips replace pending text and distinguish unavailable and binary results', async () => {
  const page = await fixture();
  const result = await page.evaluate(async () => {
    const {
      paintCounts
    } = await import('/tasks/changes/task-files.js');
    const host = document.createElement('span');
    document.body.append(host);
    return [undefined, {
      added: 0,
      removed: 0
    }, {
      kind: 'binary'
    }, {
      kind: 'unavailable'
    }, {
      kind: 'directory'
    }, {
      added: 4,
      removed: 9
    }].map(counts => {
      paintCounts(host, counts);
      return {
        text: host.textContent,
        tooltip: host.dataset.tooltip,
        label: host.getAttribute('aria-label'),
        title: host.getAttribute('title')
      };
    });
  });
  const labels = ['Loading line counts', '0 lines added, 0 removed', 'Binary diff', 'Line counts unavailable', 'Expand directory to inspect its files', '4 lines added, 9 removed'];
  assert.deepEqual(result.map(item => item.tooltip), labels);
  assert.deepEqual(result.map(item => item.label), labels);
  assert.ok(result.every(item => item.title === null));
  assert.deepEqual(result.map(item => item.text), ['', '+0 −0', 'bin', '', '', '+4 −9']);
});
test('delegated tooltips render exact review text after hover, keyboard focus and tree rerender', async () => {
  const page = await fixture();
  await page.evaluate(async () => {
    const {
      TaskChanges
    } = await import('/tasks/changes/task-changes.js');
    const panel = document.querySelector('#tool-panel'),
      signal = new AbortController().signal;
    const changes = new TaskChanges({
      api: {
        json: async () => ({
          Diff: '@@ -1 +1 @@\n-old\n+new'
        })
      },
      task: {
        id: 'fixture'
      },
      panel,
      signal,
      openDiff: () => {}
    });
    changes.files = [{
      path: 'full/directory/file.js',
      status: 'M'
    }];
    changes.totals = {
      added: 1,
      removed: 1
    };
    changes.counts.results.set('full/directory/file.js', {
      added: 1,
      removed: 1
    });
    changes.draw();
  });
  await auditIconTooltips(page, '#tools');
  const bubble = page.locator('#workspace-tooltip');
  async function hover(selector, label) {
    await page.locator(selector).hover();
    await page.waitForFunction(text => {
      const tip = document.querySelector('#workspace-tooltip');
      return tip && !tip.hidden && tip.textContent === text;
    }, label);
    assert.equal(await bubble.isVisible(), true);
  }
  await hover('.changes-view-toggle', 'Show tree view');
  assert.equal(await page.locator('.changes-view-toggle').getAttribute('aria-describedby'), 'workspace-tooltip');
  await hover('.file-status', 'Modified');
  await hover('.file-counts', '1 lines added, 1 removed');
  await page.locator('.file-path').hover();
  await page.waitForTimeout(500);
  assert.equal(await bubble.isVisible(), false);
  await page.locator('.file-prefix').evaluate(element => element.style.maxWidth = '20px');
  await page.mouse.move(0, 0);
  await hover('.file-path', 'full/directory/file.js');
  await bubble.evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished)));
  await page.screenshot({
    path: '.context/review-path-tooltip.png'
  });
  await page.locator('.changes-view-toggle').click();
  await hover('.file-status', 'Modified');
  await page.keyboard.press('Escape');
  assert.equal(await bubble.isVisible(), false);
  await page.keyboard.press('Tab');
  await page.locator('.changes-view-toggle').focus();
  await page.waitForFunction(() => {
    const tip = document.querySelector('#workspace-tooltip');
    return tip && !tip.hidden && tip.textContent === 'Show flat view';
  });
  assert.equal(await page.locator('.changes-view-toggle').evaluate(node => node.matches(':focus-visible')), true);
  await page.keyboard.press('Escape');
  assert.equal(await bubble.isVisible(), false);
  await page.locator('.file-row').focus();
  await page.keyboard.press('Tab');
  await page.waitForFunction(() => {
    const tip = document.querySelector('#workspace-tooltip');
    return tip && !tip.hidden && tip.textContent === 'File actions for full/directory/file.js';
  });
  await page.keyboard.press('Enter');
  assert.equal(await bubble.isVisible(), false);
  assert.equal(await page.getByRole('menu').isVisible(), true);
  await auditIconTooltips(page, 'body');
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
