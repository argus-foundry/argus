import { test, expect } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
let browser, frontend, snapshot;
const cleanup = [];
async function fixture(section, {
  integrated = false
} = {}) {
  const page = await browser.newPage({
    viewport: {
      width: integrated ? 1280 : 1000,
      height: 800
    }
  });
  cleanup.push(() => page.close());
  const state = {
    rows: [{
      id: 'job',
      name: 'Nightly',
      project: 'demo',
      backend: 'alpha',
      model: 'custom-model',
      schedule: '@daily',
      prompt: 'hello\nworld',
      enabled: true,
      next_run_at: '2099-01-01T00:00:00Z',
      last_error: 'Prior failure'
    }],
    writes: [],
    metricsReads: 0,
    logReads: [],
    metrics: snapshot(),
    fail: false
  };
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    json: {
      tasks: [{
        id: 'fixture',
        name: 'Fixture task',
        project: 'demo',
        status: 'complete',
        live: false
      }]
    }
  }));
  await page.route('**/api/hera', route => route.fulfill({
    json: {
      orchestrators: []
    }
  }));
  await page.route('**/api/projects/full', route => route.fulfill({
    json: {
      projects: [{
        name: 'demo'
      }]
    }
  }));
  await page.route('**/api/backends', route => route.fulfill({
    json: {
      backends: [{
        name: 'alpha',
        models: ['model-a', 'model-b']
      }]
    }
  }));
  await page.route('**/api/schedules', route => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON();
      state.writes.push({
        method: 'POST',
        path: '/api/schedules',
        body
      });
      if (state.fail) return route.fulfill({
        status: 500,
        json: {}
      });
      state.rows.push({
        id: 'new-job',
        ...body
      });
      return route.fulfill({
        status: 201,
        json: state.rows.at(-1)
      });
    }
    return route.fulfill({
      json: {
        schedules: state.omitSavedRow ? [] : state.rows
      }
    });
  });
  await page.route('**/api/schedules/**', async route => {
    const request = route.request(),
      path = new URL(request.url()).pathname,
      body = request.postData() ? request.postDataJSON() : null;
    state.writes.push({
      method: request.method(),
      path,
      body
    });
    if (state.holdSave && request.method() === 'PUT') await new Promise(resolve => {
      state.releaseSave = resolve;
      state.heldSave?.();
    });
    if (state.fail) return route.fulfill({
      status: 500,
      json: {}
    });
    if (path.endsWith('/run')) return route.fulfill({
      status: 202,
      json: {
        task_id: 'created-task'
      }
    });
    if (request.method() === 'DELETE') {
      state.rows = state.rows.filter(r => r.id !== path.split('/').at(-1));
      return route.fulfill({
        status: 204
      });
    }
    state.rows = state.rows.map(row => row.id === path.split('/').at(-1) ? {
      ...row,
      ...body
    } : row);
    return route.fulfill({
      json: state.rows.find(r => r.id === path.split('/').at(-1))
    }).catch(() => {});
  });
  await page.route('**/api/system-metrics', route => {
    state.metricsReads++;
    return state.failMetrics ? route.fulfill({
      status: 503,
      json: {}
    }) : route.fulfill({
      json: state.metrics
    });
  });
  await page.route('**/api/logs/*', async route => {
    const name = new URL(route.request().url()).pathname.split('/').at(-1);
    state.logReads.push(name);
    if (state.failLogs) return route.fulfill({
      status: 500,
      json: {}
    });
    if (state.emptyLogs) return route.fulfill({
      body: '',
      contentType: 'text/plain'
    });
    if (name === 'ux' && state.holdUX) await new Promise(resolve => {
      state.release = resolve;
      state.held?.();
    });
    await route.fulfill({
      body: name === 'ux' ? '<img src=x onerror="window.injected=true">\n' + 'line\n'.repeat(200) : 'daemon fixture\n' + 'Z'.repeat(2000),
      contentType: 'text/plain'
    }).catch(() => {});
  });
  const html = `<!doctype html><link rel="stylesheet" href="/workspace/styles.css"><main style="height:100dvh;padding:24px;min-width:0"><p id="status" role="status"></p><div id="mount"></div></main><script type="module">
 import {Api} from '/core/api.js';const api=new Api();api.setToken('fixture-token');window.changes=0;window.busy=false;
 const modules={schedules:['/settings/schedules/settings-schedules.js','ScheduleSettings'],system:['/settings/system/settings-system.js','SystemSettings'],logs:['/settings/system/settings-logs.js','LogSettings']};
 window.mount=async name=>{window.view?.destroy();document.querySelector('#mount').replaceChildren();document.querySelector('#status').textContent='';if(!name){window.view=null;return;}const [path,key]=modules[name];const module=await import(path);window.view=new module[key]({api,host:document.querySelector('#mount'),status:(text,error)=>{document.querySelector('#status').textContent=text;document.querySelector('#status').dataset.error=String(!!error);},changed:()=>window.changes++,busy:value=>window.busy=value,baseline:value=>value});};await window.mount('${section}');window.ready=true;
 </script>`;
  await page.route('**/operations-fixture', route => route.fulfill({
    contentType: 'text/html',
    body: html
  }));
  if (!integrated) await page.clock.install();
  if (integrated) {
    await page.goto(frontend);
    await page.locator('#token').fill('fixture-token');
    await page.locator('#connect-button').click();
    await page.locator('#disconnect:visible').waitFor();
    await page.getByRole('button', {
      name: 'Settings',
      exact: true
    }).click();
    await page.locator(`#settings-panel [data-section="${section}"]`).click();
  } else {
    await page.goto(frontend + '/operations-fixture');
    await page.waitForFunction(() => window.ready, null, {
      timeout: 5000
    });
  }
  return {
    page,
    state
  };
}
test.beforeAll(async ({
  browser: workerBrowser
}) => {
  browser = workerBrowser;
  await resetServer();
  frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  snapshot = () => ({
    cpu_avail: true,
    cpu_percent: 37,
    load_avail: true,
    load1: 1,
    load5: 2,
    load15: 3,
    mem_avail: true,
    mem_used: 4 * 1024 ** 3,
    mem_total: 8 * 1024 ** 3,
    mem_percent: 50,
    swap_avail: true,
    swap_total: 0,
    disk_avail: true,
    disk_used: 10 * 1024 ** 3,
    disk_total: 100 * 1024 ** 3,
    disk_free: 90 * 1024 ** 3,
    disk_percent: 10,
    disk_path: '/tmp/fixture',
    proc_avail: true,
    proc_rss: 1024 ** 3,
    uptime_avail: true,
    uptime_sec: 90061,
    sessions: {
      running: 2,
      idle: 1
    },
    sampled_at: '2026-11-06T12:00:00Z',
    host_sleep: {
      status: 'at_risk',
      message: 'The host may sleep.',
      fix: 'sudo pmset -c sleep 0'
    }
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
test('schedules create and edit preserve fields, failures retain edits and run uses the saved row', async () => {
  const {
    page,
    state
  } = await fixture('schedules');
  await page.getByRole('button', {
    name: 'Edit Nightly',
    exact: true
  }).click();
  await page.getByLabel('Model', {
    exact: true
  }).fill('custom-new');
  await page.getByLabel('Prompt', {
    exact: true
  }).fill('retained\nmultiline\n');
  state.fail = true;
  await page.getByRole('button', {
    name: 'Save schedule',
    exact: true
  }).click();
  await page.locator('#status').filter({
    hasText: 'not confirmed'
  }).waitFor();
  assert.equal(await page.getByLabel('Prompt', {
    exact: true
  }).inputValue(), 'retained\nmultiline\n');
  state.fail = false;
  await page.getByRole('button', {
    name: 'Save schedule',
    exact: true
  }).click();
  await page.locator('#status').filter({
    hasText: 'Saved'
  }).waitFor();
  assert.equal(state.rows[0].model, 'custom-new');
  assert.equal(state.rows[0].prompt, 'retained\nmultiline\n');
  assert.equal(state.writes.at(-1).body.schedule, undefined);
  await page.getByRole('button', {
    name: 'Run Nightly now',
    exact: true
  }).click();
  await page.locator('#status').filter({
    hasText: 'created-task'
  }).waitFor();
  assert.equal(state.writes.at(-1).path, '/api/schedules/job/run');
  await page.getByRole('button', {
    name: 'New schedule',
    exact: true
  }).click();
  await page.getByLabel('Name', {
    exact: true
  }).fill('New daily');
  await page.getByLabel('Prompt', {
    exact: true
  }).fill('new prompt');
  await page.getByRole('button', {
    name: 'Save schedule',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Edit New daily',
    exact: true
  }).waitFor();
  assert.equal(state.rows.at(-1).schedule, '@daily');
  assert.equal(state.rows.at(-1).enabled, true);
});
test('schedule deletion confirms consequences and cancellation writes nothing', async () => {
  const {
    page,
    state
  } = await fixture('schedules');
  await page.getByRole('button', {
    name: 'Delete Nightly',
    exact: true
  }).waitFor();
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Delete Nightly',
    exact: true
  }).click(), dialog => {
    assert.match(dialog.message(), /already created.*not affected/i);
    dialog.dismiss();
  });
  assert.equal(state.writes.length, 0);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Delete Nightly',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByText('No schedules yet.', {
    exact: true
  }).waitFor();
  assert.equal(state.writes[0].method, 'DELETE');
});
test('System shows available metrics and host sleep commands, polling only while visible', async () => {
  const {
    page,
    state
  } = await fixture('system');
  await page.getByText('37%', {
    exact: true
  }).waitFor();
  for (const text of ['4.0 GiB / 8.0 GiB (50%)', '2 running, 1 idle', 'The host may sleep.', 'sudo pmset -c sleep 0', 'caffeinate -is -w $(cat ~/.argus/daemon.pid)']) assert.ok((await page.locator('#mount').textContent()).includes(text), text);
  const refreshed = page.waitForResponse('**/api/system-metrics');
  await page.clock.fastForward(2100);
  await refreshed;
  assert.equal(state.metricsReads, 2);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => true
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.fastForward(10000);
  assert.equal(state.metricsReads, 2);
  const resumed = page.waitForResponse('**/api/system-metrics');
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false
    });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await resumed;
  assert.equal(state.metricsReads, 3);
  await page.evaluate(() => window.mount(null));
  await page.clock.fastForward(10000);
  assert.equal(state.metricsReads, 3);
  assert.equal(state.writes.length, 0);
});
test('Logs stay plain text, cancel stale selection reads and scroll only inside the panel', async () => {
  const {
    page,
    state
  } = await fixture('logs');
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '<img'
  }).waitFor();
  assert.equal(await page.locator('#mount img').count(), 0);
  assert.equal(await page.evaluate(() => window.injected), undefined);
  state.holdUX = true;
  const held = new Promise(resolve => {
    state.held = resolve;
  });
  await page.getByRole('button', {
    name: 'Refresh log',
    exact: true
  }).click();
  await held;
  await page.getByLabel('Log file', {
    exact: true
  }).selectOption('daemon');
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: 'daemon fixture'
  }).waitFor();
  state.release();
  await page.waitForTimeout(50);
  assert.ok((await page.getByLabel('Log contents', {
    exact: true
  }).textContent()).startsWith('daemon fixture'));
  await page.setViewportSize({
    width: 320,
    height: 700
  });
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true);
  assert.ok(await page.getByLabel('Log contents', {
    exact: true
  }).evaluate(el => el.scrollWidth > el.clientWidth));
  await page.evaluate(() => window.mount(null));
  assert.equal(state.writes.length, 0);
});
test('grouped Settings mounts operations sections and cancels metrics when navigating away', async () => {
  const {
    page,
    state
  } = await fixture('system', {
    integrated: true
  });
  await page.getByText('37%', {
    exact: true
  }).waitFor({
    timeout: 5000
  });
  const reads = state.metricsReads;
  await page.locator('#settings-panel [data-section="schedules"]').click();
  await page.getByRole('button', {
    name: 'Edit Nightly',
    exact: true
  }).waitFor();
  await page.waitForTimeout(2100);
  assert.equal(state.metricsReads, reads);
  await page.getByRole('button', {
    name: 'New schedule',
    exact: true
  }).click();
  await page.getByLabel('Cron schedule', {
    exact: true
  }).waitFor();
  await page.locator('#settings-panel [data-section="logs"]').click();
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '<img'
  }).waitFor();
  await page.setViewportSize({
    width: 320,
    height: 700
  });
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true);
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden, null, {
    timeout: 5000
  });
  assert.equal(state.writes.length, 0);
});
test('schedule save locks controls, prevents duplicate submission and cannot overwrite a new section', async () => {
  const {
    page,
    state
  } = await fixture('schedules');
  await page.getByRole('button', {
    name: 'Edit Nightly',
    exact: true
  }).click();
  state.holdSave = true;
  const held = new Promise(resolve => {
    state.heldSave = resolve;
  });
  await page.getByRole('button', {
    name: 'Save schedule',
    exact: true
  }).click();
  await held;
  assert.equal(await page.evaluate(() => window.busy), true);
  assert.equal(await page.getByRole('button', {
    name: 'Save schedule',
    exact: true
  }).isDisabled(), true);
  await page.locator('#mount form').evaluate(form => form.dispatchEvent(new Event('submit', {
    bubbles: true,
    cancelable: true
  })));
  assert.equal(state.writes.length, 1);
  await page.evaluate(() => window.mount('logs'));
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '<img'
  }).waitFor();
  state.releaseSave();
  await page.waitForTimeout(50);
  assert.equal(await page.evaluate(() => window.busy), false);
  assert.equal(await page.locator('#status').textContent(), '');
  assert.equal(await page.locator('#mount form').count(), 0);
});
test('System refresh failure is explicit and unavailable values do not become fake bars', async () => {
  const {
    page,
    state
  } = await fixture('system');
  await page.getByText('37%', {
    exact: true
  }).waitFor();
  state.failMetrics = true;
  await page.getByRole('button', {
    name: 'Refresh metrics',
    exact: true
  }).click();
  await page.locator('#status').filter({
    hasText: 'refresh failed'
  }).waitFor();
  assert.equal(await page.getByText('37%', {
    exact: true
  }).count(), 1);
  state.failMetrics = false;
  state.metrics = {
    ...state.metrics,
    cpu_percent: 0,
    mem_avail: false,
    disk_avail: false,
    host_sleep: {
      status: 'protected'
    }
  };
  await page.getByRole('button', {
    name: 'Refresh metrics',
    exact: true
  }).click();
  await page.getByText('0%', {
    exact: true
  }).waitFor();
  assert.equal(await page.locator('#mount progress').count(), 1);
  assert.equal(await page.getByText('Host may sleep', {
    exact: true
  }).isVisible(), false);
  assert.equal(state.writes.length, 0);
});
test('log failures remain retryable and empty logs have an explicit empty state', async () => {
  const {
    page,
    state
  } = await fixture('logs');
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '<img'
  }).waitFor();
  state.failLogs = true;
  await page.getByRole('button', {
    name: 'Refresh log',
    exact: true
  }).click();
  await page.locator('#status').filter({
    hasText: 'Log read failed'
  }).waitFor();
  assert.ok((await page.getByLabel('Log contents', {
    exact: true
  }).textContent()).includes('unavailable'));
  state.failLogs = false;
  state.emptyLogs = true;
  await page.getByRole('button', {
    name: 'Refresh log',
    exact: true
  }).click();
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '(empty)'
  }).waitFor();
  assert.equal(state.writes.length, 0);
});
test('destroying a pending log read aborts it and retains no text in storage or detached host', async () => {
  const {
    page
  } = await fixture('logs');
  await page.getByLabel('Log contents', {
    exact: true
  }).filter({
    hasText: '<img'
  }).waitFor();
  const result = await page.evaluate(async () => {
    const before = {
      local: {
        ...localStorage
      },
      session: {
        ...sessionStorage
      }
    };
    let release, signal;
    const gate = new Promise(resolve => release = resolve);
    window.view.api.request = async (path, options) => {
      signal = options.signal;
      await gate;
      return new Response('private late log sentinel');
    };
    const pane = window.view.pane,
      pending = window.view.load();
    await window.mount(null);
    const aborted = signal.aborted;
    release();
    await pending;
    return {
      aborted,
      text: pane.textContent,
      host: document.querySelector('#mount').textContent,
      before,
      after: {
        local: {
          ...localStorage
        },
        session: {
          ...sessionStorage
        }
      }
    };
  });
  assert.equal(result.aborted, true);
  assert.equal(result.text, 'Loading log…');
  assert.equal(result.host, '');
  assert.deepEqual(result.after, result.before);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

for(const name of ['New schedule','Edit Nightly'])test(`keyboard Cancel returns schedule focus to ${name}`,async()=>{
  const {page}=await fixture('schedules');
  const opener=page.getByRole('button',{name,exact:true});
  await opener.focus();await page.keyboard.press('Enter');
  await expect(page.getByLabel('Name',{exact:true})).toBeFocused();
  const cancel=page.getByRole('button',{name:'Cancel',exact:true});
  await cancel.focus();await page.keyboard.press('Enter');
  await expect(cancel).toHaveCount(0);await expect(opener).toBeFocused();
});

test('Cancel falls back to New schedule when its Edit opener disappears',async()=>{
  const {page}=await fixture('schedules');
  const opener=page.getByRole('button',{name:'Edit Nightly',exact:true});
  await opener.focus();await page.keyboard.press('Enter');
  await page.evaluate(()=>window.view.renderList());
  await page.getByRole('button',{name:'Cancel',exact:true}).focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button',{name:'New schedule',exact:true})).toBeFocused();
});

for(const mode of ['new','edit','missing','outside','moved'])test(`schedule Save restores ${mode} keyboard position`,async()=>{
  const {page,state}=await fixture('schedules');
  state.rows.unshift({...state.rows[0],id:'other',name:'Other'});
  await page.evaluate(()=>window.view.load());
  await page.getByRole('button',{name:mode==='new'?'New schedule':'Edit Nightly',exact:true}).click();
  await page.getByLabel('Name',{exact:true}).fill('Renamed');
  await page.getByLabel('Prompt',{exact:true}).fill('hello');
  if(mode==='missing')state.omitSavedRow=true;
  const save=page.getByRole('button',{name:'Save schedule',exact:true});
  if(mode==='outside') {
    await page.evaluate(()=>{const outside=document.createElement('button');outside.id='outside';outside.textContent='Outside';document.body.append(outside);outside.focus();document.querySelector('form').requestSubmit();});
  } else if(mode==='moved') {
    state.holdSave=true;const held=new Promise(resolve=>state.heldSave=resolve);
    await save.focus();await page.keyboard.press('Enter');await held;
    await page.evaluate(()=>{const outside=document.createElement('button');outside.id='outside';outside.textContent='Outside';document.body.append(outside);outside.focus();});
    state.releaseSave();
  } else {await save.focus();await page.keyboard.press('Enter');}
  await expect(save).toHaveCount(0);
  const target=['outside','moved'].includes(mode)?page.locator('#outside'):page.getByRole('button',{name:mode==='edit'?'Edit Renamed':'New schedule',exact:true});
  await expect(target).toBeFocused();await expect(target).toBeEnabled();
});
