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
for (const kind of ['diff', 'artifact', 'manifest']) test(`held ${kind} read cannot replace the next task review`, async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  const result = await page.evaluate(async kind => {
    const {
      TaskReview
    } = await import('/review/review.js');
    let release, signal;
    const pending = new Promise(resolve => {
      release = resolve;
    });
    const api = {
      json: (path, options) => {
        signal = options.signal;
        return pending;
      },
      request: (path, options) => {
        signal = options.signal;
        return pending;
      }
    };
    const review = new TaskReview(api, () => {}),
      a = {
        id: 'a'
      },
      b = {
        id: 'b'
      };
    review.selectTask(a);
    const artifact = {
      filename: 'old.txt',
      type: 'text',
      size: 20
    };
    const old = kind === 'diff' ? review.openDiff(a, 'old.go') : kind === 'manifest' ? review.restoreArtifact(a, 'old.txt') : review.openArtifact(a, artifact);
    // Retain the old content too: artifact renders into a captured, detached host.
    const captured = document.querySelector('.artifact-preview');
    review.selectTask(b);
    const before = document.querySelector('#review-panel').innerHTML;
    release(kind === 'diff' ? {
      Diff: 'diff --git a/old b/old\n@@ -0,0 +1 @@\n+STALE DIFF'
    } : kind === 'manifest' ? {
      artifacts: [artifact]
    } : new Response('STALE ARTIFACT'));
    await old;
    return {
      before,
      after: document.querySelector('#review-panel').innerHTML,
      detached: captured?.textContent || '',
      aborted: signal.aborted,
      urls: review.urls.size
    };
  }, kind);
  assert.equal(result.after, result.before);
  assert.match(result.after, /See the work take shape/);
  assert.doesNotMatch(result.detached, /STALE ARTIFACT/);
  assert.equal(result.aborted, true);
  assert.equal(result.urls, 0);
});
test('removed artifact manifest and bytes offer recovery and retry can succeed', async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  const result = await page.evaluate(async () => {
    const {
      TaskReview
    } = await import('/review/review.js');
    let reads = 0;
    const review = new TaskReview({
        json: async () => ({
          artifacts: []
        }),
        request: async () => {
          if (++reads === 1) throw Object.assign(new Error('Artifact removed (404)'), {
            status: 404
          });
          return new Response('Recovered bytes');
        }
      }, () => {}),
      task = {
        id: 'a'
      };
    review.selectTask(task);
    await review.restoreArtifact(task, 'gone.txt');
    const missing = document.querySelector('#review-panel').textContent;
    await review.openArtifact(task, {
      filename: 'gone.txt',
      type: 'text',
      size: 10
    });
    const failed = document.querySelector('#review-panel').textContent;
    document.querySelector('.artifact-preview button')?.click();
    for (let i = 0; i < 10 && !document.querySelector('pre'); i++) await new Promise(resolve => setTimeout(resolve, 0));
    return {
      missing,
      failed,
      recovered: document.querySelector('pre')?.textContent,
      reads
    };
  });
  assert.match(result.missing, /Artifact unavailable.*no longer registered/);
  assert.doesNotMatch(result.missing, /Finding registered/);
  assert.match(result.failed, /Preview unavailable.*Artifact removed.*Retry preview/);
  assert.equal(result.recovered, 'Recovered bytes');
  assert.equal(result.reads, 2);
});
for (const total of [6000, 6001]) test(`review bounds ${total} parsed diff lines in unified and split layouts`, async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  const result = await page.evaluate(async total => {
    const {
      renderDiff
    } = await import('/review/review-diff.js');
    const container = document.querySelector('#review-panel');
    const selection = {
      path: 'large.txt',
      mode: 'unified',
      diff: ['@@ -0,0 +1,6000 @@', ...Array.from({
        length: total - 1
      }, (_, i) => `+line-${i + 1}`)].join('\n')
    };
    const draw = mode => {
      selection.mode = mode;
      renderDiff(container, selection, draw);
    };
    const capture = () => ({
      mode: container.querySelector('.diff-table').className,
      rows: container.querySelectorAll('tbody tr').length,
      last: container.querySelector('tbody tr:last-child').textContent,
      note: container.querySelector('.diff-overflow-note')?.textContent || '',
      overflowVisible: [...container.querySelectorAll('.diff-code')].some(cell => cell.textContent === '+line-6000')
    });
    draw('unified');
    const unified = capture();
    [...container.querySelectorAll('button')].find(button => button.textContent === 'Split').click();
    return [unified, capture()];
  }, total);
  for (const [index, view] of result.entries()) {
    assert.match(view.mode, index === 0 ? /unified/ : /split/);
    assert.equal(view.rows, 6000 + index);
    assert.match(view.last, /\+line-5999$/);
    assert.equal(view.overflowVisible, false);
    if (total > 6000) assert.match(view.note, /Showing the first 6,000 lines.*original UI or a local editor.*full diff/);else assert.equal(view.note, '');
  }
});
test('review remembers path and mode without retaining inactive diff bodies', async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  const result = await page.evaluate(async () => {
    const {
      TaskReview
    } = await import('/review/review.js');
    let reads = 0;
    const review = new TaskReview({
      json: async () => ({
        Diff: `@@ -0,0 +1 @@\n+body-${++reads}`
      })
    }, () => {});
    for (let i = 0; i < 20; i++) {
      const task = {
        id: String(i)
      };
      review.selectTask(task);
      await review.openDiff(task, 'file.txt');
      document.querySelectorAll('.review-toolbar button')[1].click();
    }
    const retained = [...review.selections.values()].filter(selection => selection.diff !== undefined).length;
    review.selectTask({
      id: '0'
    });
    for (let i = 0; i < 20 && reads < 21; i++) await new Promise(resolve => setTimeout(resolve, 0));
    await new Promise(resolve => setTimeout(resolve, 0));
    const restored = {
      text: document.querySelector('#review-panel').textContent,
      mode: review.selections.get('0').mode,
      reads
    };
    review.clear();
    return {
      retained,
      restored,
      selections: review.selections.size
    };
  });
  assert.equal(result.retained, 0, 'diff bodies remain reachable from selection metadata');
  assert.equal(result.restored.mode, 'split');
  assert.equal(result.restored.reads, 21);
  assert.match(result.restored.text, /body-21/);
  assert.equal(result.selections, 0);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

for (const mode of ['unified', 'split']) test(`${mode} diff exposes scoped column headers`, async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  await page.evaluate(async mode => {
    const { renderDiff } = await import('/review/review-diff.js');
    renderDiff(document.querySelector('#review-panel'), {
      path: 'file.txt', mode, diff: '@@ -1,2 +1,2 @@\n same\n-before\n+after'
    }, () => {});
  }, mode);
  const table = page.getByRole('table', { name: `${mode} diff of file.txt` });
  const expected = mode === 'split' ? ['Old line', 'Before', 'New line', 'After'] : ['Old line', 'New line', 'Content'];
  assert.deepEqual(await table.getByRole('columnheader').allTextContents(), expected);
  assert.deepEqual(await table.getByRole('columnheader').evaluateAll(headers => headers.map(header => header.getAttribute('scope'))), expected.map(() => 'col'));
  assert.equal(await table.getByRole('cell', { name: mode === 'split' ? '-before' : '+after', exact: true }).count(), 1);
});

test('large review scans only the preview budget and sentinel on each layout', async () => {
  const page = await actorPage(browser, cleanup, '<div id="review-panel"></div>');
  const scans = await page.evaluate(async () => {
    const { renderDiff } = await import('/review/review-diff.js');
    const diff = '@@ -0,0 +1,1000000 @@\n' + '+generated line\n'.repeat(1000000);
    const indexOf = String.prototype.indexOf;
    let scanned = 0;
    String.prototype.indexOf = function (...args) {
      if (this.valueOf() === diff) scanned++;
      return indexOf.apply(this, args);
    };
    try {
      return ['unified', 'split'].map(mode => {
        scanned = 0;
        renderDiff(document.querySelector('#review-panel'), { path: 'generated.txt', mode, diff }, () => {});
        return scanned;
      });
    } finally { String.prototype.indexOf = indexOf; }
  });
  assert.deepEqual(scans, [6001, 6001]);
});
