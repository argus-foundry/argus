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
for (const held of ['raw', 'links']) test(`Info ignores held ${held} after task switch`, async () => {
  const page = await actorPage(browser, cleanup, '<div id="workspace"><div id="tools"><div class="tool-tabs"><button role="tab" id="tool-status" data-tool="status"></button></div><div id="tool-panel"></div></div></div><h1 id="task-title"></h1>');
  const result = await page.evaluate(async held => {
    const {
      TaskTools
    } = await import('/tasks/tools.js');
    const {
      TaskHeaderLinks
    } = await import('/tasks/task-links.js');
    let release;
    const pending = new Promise(resolve => {
      release = resolve;
    });
    const signals = [];
    const api = {
      json: async (path, {
        signal
      } = {}) => {
        if (path === `/api/tasks/a/${held}`) {
          signals.push(signal);
          return pending;
        }
        if (path.endsWith('/raw')) return {
          model: 'Current model',
          account: 'Current account'
        };
        if (path.endsWith('/links')) return {
          links: [{
            url: 'https://example.com/current',
            label: 'Current link',
            isPR: true
          }]
        };
        return {};
      }
    };
    const tools = new TaskTools(api, {}),
      header = new TaskHeaderLinks(api);
    const reads = [],
      drawStatus = tools.drawStatus.bind(tools);
    tools.drawStatus = () => {
      const read = drawStatus();
      reads.push(read);
      return read;
    };
    tools.selectTask({
      id: 'a',
      project: 'Old project'
    });
    const oldInfo = reads.at(-1),
      oldHeader = header.select({
        id: 'a'
      });
    // Let drawStatus reach its second read when links, rather than raw, is held.
    await Promise.resolve();
    await Promise.resolve();
    const oldLinks = held === 'links' ? document.querySelector('#tool-panel').lastElementChild : null;
    const oldHeaderLinks = header.host.lastElementChild;
    tools.selectTask({
      id: 'b',
      project: 'Current project'
    });
    await reads.at(-1);
    await header.select({
      id: 'b'
    });
    const before = document.querySelector('#tool-panel').innerHTML;
    release(held === 'raw' ? {
      model: 'STALE MODEL',
      account: 'STALE ACCOUNT'
    } : {
      links: [{
        url: 'https://example.com/stale',
        label: 'STALE LINK',
        isPR: true
      }]
    });
    await Promise.all([oldInfo, oldHeader]);
    return {
      before,
      after: document.querySelector('#tool-panel').innerHTML,
      header: header.host.innerHTML,
      detachedInfo: oldLinks?.textContent || '',
      detachedHeader: oldHeaderLinks?.innerHTML || '',
      signalCount: signals.length,
      aborted: signals.every(signal => signal.aborted)
    };
  }, held);
  assert.match(result.before, /Current project/);
  assert.match(result.before, /Current model/);
  assert.match(result.before, /Current link/);
  assert.equal(result.after, result.before);
  assert.doesNotMatch(result.detachedInfo, /STALE LINK/);
  assert.doesNotMatch(result.detachedHeader, /stale/);
  assert.doesNotMatch(result.header, /stale/);
  assert.match(result.header, /https:\/\/example.com\/current/);
  assert.equal(result.signalCount, held === 'links' ? 2 : 1);
  assert.equal(result.aborted, true);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

test('Info coalesces identity reads and leaves unchanged or hidden panels intact', async () => {
  const page = await actorPage(browser, cleanup, '<div id="workspace"><div id="tools"><div class="tool-tabs"><button role="tab" id="tool-status" data-tool="status"></button></div><div id="tool-panel"></div></div></div>');
  const result = await page.evaluate(async () => {
    const {TaskTools} = await import('/tasks/tools.js');
    const {patchEvent} = await import('/workspace/events/workspace-events-policy.js');
    let release, rawReads=0, replacements=0;
    const pending=new Promise(resolve=>{release=resolve;});
    const api={json:async path=>path.endsWith('/raw')?(rawReads++,pending):{links:[]}};
    const tools=new TaskTools(api,{}), task={id:'a',status:'complete',project:'demo'};
    tools.selectTask(task);
    const updates=[];
    for(let i=0;i<100;i++) {
      const {tasks}=patchEvent([task],{task_id:i%2?'a':'b',type:'task.status_changed',payload:{to:'complete'}});
      tools.updateTask(tasks[0]);
      updates.push(tools.drawStatus());
    }
    const concurrentReads=rawReads;
    release({model:'Chosen model',account:'chosen'});
    await Promise.all(updates);
    const panel=document.querySelector('#tool-panel'), original=panel.firstElementChild;
    const observer=new MutationObserver(records=>{replacements+=records.filter(record=>record.target===panel&&record.removedNodes.length).length;});
    observer.observe(panel,{childList:true});
    for(let i=0;i<100;i++)tools.updateTask({...task});
    await tools.drawStatus();
    await Promise.resolve();
    const unchanged=panel.firstElementChild===original;
    const unchangedReplacements=replacements;
    document.querySelector('#tools').hidden=true;
    tools.updateTask({...task,project:'Hidden update'});
    await tools.drawStatus();
    const hiddenText=panel.textContent;
    document.querySelector('#tools').hidden=false;
    await tools.drawStatus();
    const visibleText=panel.textContent;
    tools.clear();observer.disconnect();
    return {concurrentReads,rawReads,unchanged,unchangedReplacements,hiddenText,visibleText};
  });
  assert.equal(result.concurrentReads,1);
  assert.equal(result.rawReads,1);
  assert.equal(result.unchanged,true);
  assert.equal(result.unchangedReplacements,0);
  assert.doesNotMatch(result.hiddenText,/Hidden update/);
  assert.match(result.visibleText,/Hidden update/);
});
