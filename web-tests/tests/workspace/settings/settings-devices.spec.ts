import { test, expect } from '@playwright/test';
import { resetServer } from '../../_helpers';
import { confirmClick } from '../_confirmation-fixture.ts';
import assert from 'node:assert/strict';
let browser;
const cleanup = [];
async function fixture(permission = 'granted', holdRead = false) {
  const page = await browser.newPage();
  cleanup.push(() => page.close());
  const calls = [],
    errors = [],
    state = {
      failTokens: false,
      failPush: false,
      tokens: [],
      subscriptions: []
    };
  page.on('pageerror', e => errors.push(e.message));
  cleanup.push(() => assert.deepEqual(errors, []));
  await page.addInitScript(({
    permission,
    holdRead
  }) => {
    window.pushEvents = [];
    let subscription = null;
    const manager = {
      getSubscription: async () => subscription,
      subscribe: async () => {
        window.pushEvents.push('subscribe');
        subscription = {
          toJSON: () => ({
            endpoint: 'https://push.example.invalid/fixture',
            keys: {
              auth: 'fixture',
              p256dh: 'fixture'
            }
          }),
          unsubscribe: async () => {
            window.pushEvents.push('unsubscribe');
            subscription = null;
            return true;
          }
        };
        return subscription;
      }
    };
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: {
        getRegistration: async () => {
          if (holdRead) await new Promise(resolve => {
            window.releasePushRead = resolve;
          });
          return undefined;
        },
        register: async () => {
          window.pushEvents.push('register');
          return {
            pushManager: manager
          };
        }
      }
    });
    Object.defineProperty(window, 'PushManager', {
      value: function () {}
    });
    Object.defineProperty(window, 'Notification', {
      value: {
        permission,
        requestPermission: async () => {
          window.pushEvents.push('permission');
          return permission;
        }
      }
    });
    if (permission === 'unsupported') Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: undefined
    });
  }, {
    permission,
    holdRead
  });
  await page.route('**/api/**', route => route.fulfill({
    status: 404,
    json: {}
  }));
  for (const [path, json] of [['tasks?archived=all', {
    tasks: []
  }], ['projects/full', {
    projects: []
  }], ['hera', {
    orchestrators: []
  }], ['backends', {
    backends: []
  }], ['settings', {
    defaults: {},
    api: {},
    kb: {}
  }], ['push/vapid-public-key', {
    public_key: 'AQID'
  }]]) await page.route('**/api/' + path, route => route.fulfill({
    json
  }));
  await page.route('**/api/tokens', route => {
    const method = route.request().method();
    if (method !== 'GET') calls.push({
      path: '/api/tokens',
      method,
      body: route.request().postDataJSON()
    });
    if (state.failTokens) return route.fulfill({
      status: 403,
      json: {}
    });
    if (method === 'POST') {
      state.tokens.push({
        id: 1,
        label: 'Phone',
        last4: 'ture'
      });
      return route.fulfill({
        json: {
          id: 1,
          label: 'Phone',
          token: 'minted-fixture'
        }
      });
    }
    return route.fulfill({
      json: {
        tokens: state.tokens
      }
    });
  });
  await page.route('**/api/tokens/*', route => {
    calls.push({
      path: new URL(route.request().url()).pathname,
      method: route.request().method()
    });
    state.tokens = [];
    return route.fulfill({
      json: {}
    });
  });
  await page.route('**/api/push/subscriptions', route => route.fulfill({
    json: {
      subscriptions: state.subscriptions
    }
  }));
  await page.route('**/api/push/subscribe', route => {
    calls.push({
      path: '/api/push/subscribe',
      body: route.request().postDataJSON()
    });
    if (state.failPush) return route.fulfill({
      status: 500,
      json: {}
    });
    state.subscriptions.push({
      id: 3,
      label: 'Fixture browser',
      endpoint_masked: 'https://push...fixture'
    });
    return route.fulfill({
      json: {
        id: 3
      }
    });
  });
  await page.route('**/api/push/subscribe/*', route => {
    calls.push({
      path: new URL(route.request().url()).pathname,
      method: route.request().method()
    });
    state.subscriptions = [];
    return route.fulfill({
      json: {}
    });
  });
  await page.route('**/api/push/test', route => {
    calls.push({
      path: '/api/push/test',
      method: route.request().method()
    });
    return route.fulfill({
      json: {
        sent: true
      }
    });
  });
  await page.goto(process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750');
  await page.locator('#token').fill('isolated-fixture-token');
  await page.locator('#connect-button').click();
  await page.locator('#disconnect:visible').waitFor();
  await page.locator('#workspace-settings').click();
  await page.locator('.settings-nav').getByRole('button', {
    name: 'Notifications & devices',
    exact: true
  }).click();
  return {
    page,
    state,
    calls
  };
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
test('device tokens are transient, master gated and revoke only after confirmation', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  await page.getByLabel('Token label', {
    exact: true
  }).fill('Phone');
  state.failTokens = true;
  await page.getByRole('button', {
    name: 'Create device token',
    exact: true
  }).click();
  await page.getByText('A master token is required to manage API tokens.', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByLabel('Token label', {
    exact: true
  }).inputValue(), 'Phone');
  state.failTokens = false;
  await page.getByRole('button', {
    name: 'Create device token',
    exact: true
  }).click();
  await page.getByLabel('New device token', {
    exact: true
  }).waitFor();
  assert.equal(await page.getByLabel('New device token', {
    exact: true
  }).inputValue(), 'minted-fixture');
  assert.equal(await page.getByRole('button', {
    name: 'Copy device token',
    exact: true
  }).getAttribute('data-tooltip'), null);
  assert.equal(await page.getByRole('button', {
    name: 'Copy device token',
    exact: true
  }).getAttribute('aria-label'), null);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Revoke Phone',
    exact: true
  }).click(), dialog => dialog.dismiss());
  assert.equal(calls.some(call => call.method === 'DELETE'), false);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Revoke Phone',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByRole('button', {
    name: 'Revoke Phone',
    exact: true
  }).waitFor({
    state: 'detached'
  });
  assert.ok(calls.some(call => call.path === '/api/tokens/1' && call.method === 'DELETE'));
  await page.locator('#settings-panel').getByRole('button', {
    name: '← Back',
    exact: true
  }).click();
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  assert.equal(await page.getByLabel('New device token', {
    exact: true
  }).count(), 0);
});
test('push setup is explicit, test is confirmed and browser/device cleanup are separate', async () => {
  const {
    page,
    calls
  } = await fixture();
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).waitFor();
  assert.deepEqual(await page.evaluate(() => window.pushEvents), []);
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Disable desktop alerts',
    exact: true
  }).waitFor();
  assert.equal(calls.filter(call => call.path === '/api/push/subscribe').length, 1);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Send test notification',
    exact: true
  }).click(), dialog => dialog.dismiss());
  assert.equal(calls.some(call => call.path === '/api/push/test'), false);
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Send test notification',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByText('Test notification requested for registered devices.', {
    exact: true
  }).waitFor();
  await page.getByRole('button', {
    name: 'Disable desktop alerts',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).waitFor();
  await confirmClick(page, () => page.getByRole('button', {
    name: 'Remove device Fixture browser',
    exact: true
  }).click(), dialog => dialog.accept());
  await page.getByRole('button', {
    name: 'Remove device Fixture browser',
    exact: true
  }).waitFor({
    state: 'detached'
  });
  assert.ok(calls.some(call => call.path === '/api/push/subscribe/3' && call.method === 'DELETE'));
});
test('share URLs require explicit credential disclosure and clear with the view', async () => {
  const {
    page,
    calls
  } = await fixture();
  await page.getByLabel('Original Argus URL', {
    exact: true
  }).fill('https://argus.example.invalid');
  assert.equal(await page.getByLabel('iOS share URL', {
    exact: true
  }).count(), 0);
  await page.getByRole('button', {
    name: 'Generate share URL',
    exact: true
  }).click();
  const url = new URL(await page.getByLabel('iOS share URL', {
    exact: true
  }).inputValue());
  assert.equal(url.searchParams.get('token'), 'isolated-fixture-token');
  assert.equal(url.pathname, '/share');
  assert.equal(await page.getByRole('button', {
    name: 'Copy share URL',
    exact: true
  }).getAttribute('data-tooltip'), null);
  assert.equal(await page.getByRole('button', {
    name: 'Copy share URL',
    exact: true
  }).getAttribute('aria-label'), null);
  await page.getByRole('button', {
    name: 'Hide share URL',
    exact: true
  }).click();
  assert.equal(await page.getByLabel('iOS share URL', {
    exact: true
  }).count(), 0);
  assert.deepEqual(calls, []);
});
test('denied push permission sends no subscription request', async () => {
  const {
    page,
    calls
  } = await fixture('denied');
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.getByText(/Notification permission was denied/).waitFor();
  assert.equal(calls.some(call => call.path === '/api/push/subscribe'), false);
});
test('push server failures keep retryable browser state without duplicate subscriptions', async () => {
  const {
    page,
    state,
    calls
  } = await fixture();
  state.failPush = true;
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.getByText(/Notification operation was not confirmed/).waitFor();
  state.failPush = false;
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Disable desktop alerts',
    exact: true
  }).waitFor();
  assert.equal((await page.evaluate(() => window.pushEvents)).filter(event => event === 'subscribe').length, 1);
  assert.equal(calls.filter(call => call.path === '/api/push/subscribe').length, 2);
});
test('a delayed initial browser read cannot replace successfully enabled push state', async () => {
  const {
    page
  } = await fixture('granted', true);
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.getByRole('button', {
    name: 'Disable desktop alerts',
    exact: true
  }).waitFor();
  await page.evaluate(async () => {
    window.releasePushRead();
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  });
  assert.equal(await page.getByRole('button', {
    name: 'Disable desktop alerts',
    exact: true
  }).count(), 1);
});
test('unsupported browser push leaves token and share controls available', async () => {
  const {
    page,
    calls
  } = await fixture('unsupported');
  await page.getByText(/Browser push is unavailable/).waitFor();
  assert.equal(await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).count(), 0);
  assert.equal(await page.getByLabel('Token label', {
    exact: true
  }).isVisible(), true);
  assert.equal(await page.getByLabel('Original Argus URL', {
    exact: true
  }).isVisible(), true);
  assert.deepEqual(calls, []);
});
test('closing Notifications cancels a late desktop permission result before subscription', async () => {
  const {
    page,
    calls
  } = await fixture('default');
  await page.evaluate(() => {
    window.permissionStarted = false;
    Notification.requestPermission = () => {
      window.permissionStarted = true;
      return new Promise(resolve => window.releasePermission = () => resolve('granted'));
    };
  });
  await page.getByRole('button', {
    name: 'Enable desktop alerts',
    exact: true
  }).click();
  await page.waitForFunction(() => window.permissionStarted);
  await page.goBack();
  await page.waitForFunction(() => document.querySelector('#settings-panel').hidden);
  await page.evaluate(() => window.releasePermission());
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  assert.equal(calls.some(call => call.path === '/api/push/subscribe'), false);
  assert.deepEqual(await page.evaluate(() => window.pushEvents), []);
});
for (const exit of ['section', 'close', 'disconnect']) test(`exposed share URL is erased on ${exit}`, async () => {
  const {
    page,
    calls
  } = await fixture();
  await page.getByLabel('Original Argus URL', {
    exact: true
  }).fill('https://argus.example.invalid');
  await page.getByRole('button', {
    name: 'Generate share URL',
    exact: true
  }).click();
  await page.getByLabel('iOS share URL', {
    exact: true
  }).evaluate(input => {
    window.exposedShare = input;
  });
  assert.match(await page.evaluate(() => window.exposedShare.value), /isolated-fixture-token/);
  if (exit === 'section') await page.locator('.settings-nav [data-section="general"]').click();else if (exit === 'close') await page.locator('#settings-panel').getByRole('button', {
    name: '← Back',
    exact: true
  }).click();else await page.locator('#disconnect').evaluate(button => button.click());
  await page.waitForFunction(() => !window.exposedShare.isConnected);
  assert.equal(await page.evaluate(() => window.exposedShare.value), '');
  assert.equal(await page.getByLabel('iOS share URL', {
    exact: true
  }).count(), 0);
  assert.deepEqual(calls, []);
});
test('dummy registered section receives callbacks and is destroyed exactly once per mount', async () => {
  const {
    page
  } = await fixture();
  const result = await page.evaluate(async () => {
    const {
      settingsSections
    } = await import('/settings/settings-sections.js');
    const {
      WorkspaceSettings
    } = await import('/settings/settings-workspace.js');
    const contexts = [],
      destroyed = [],
      events = [];
    settingsSections.push({
      id: 'dummy',
      label: 'Dummy',
      group: 'System',
      create: context => {
        contexts.push(context);
        const id = contexts.length;
        return {
          destroy() {
            destroyed.push(id);
          }
        };
      }
    });
    const api = {
      json: async () => ({
        projects: []
      })
    };
    const settings = new WorkspaceSettings(api, {
      opened() {
        events.push('opened');
      },
      closed() {
        events.push('closed');
      },
      changed() {
        events.push('changed');
      }
    });
    settings.setConnected(true);
    settings.open('dummy', {
      history: false
    });
    const first = contexts[0],
      baseline = first.baseline({
        kept: 1
      });
    first.status('fixture status', true);
    first.changed();
    first.busy(true);
    const live = {
      api: first.api === api,
      host: first.host === settings.host,
      status: settings.status.textContent,
      error: settings.status.classList.contains('error-text'),
      busy: settings.busy,
      baseline: first.baseline({
        kept: 2
      })
    };
    first.busy(false);
    settings.open('general', {
      history: false
    });
    first.status('stale');
    first.changed();
    first.busy(true);
    const stale = {
      status: settings.status.textContent,
      busy: settings.busy
    };
    settings.open('dummy', {
      history: false
    });
    settings.dismiss();
    settings.dismiss();
    settingsSections.pop();
    return {
      live,
      stale,
      destroyed,
      events,
      baseline
    };
  });
  assert.deepEqual(result.live, {
    api: true,
    host: true,
    status: 'fixture status',
    error: true,
    busy: true,
    baseline: {
      kept: 1
    }
  });
  assert.deepEqual(result.stale, {
    status: '',
    busy: false
  });
  assert.deepEqual(result.destroyed, [1, 2]);
  assert.deepEqual(result.events, ['opened', 'changed', 'closed']);
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});

for(const kind of ['share','token'])for(const mode of ['hide','outside'])test(`${kind} credential ${mode} keeps keyboard position`,async()=>{
  const {page}=await fixture();
  if(kind==='share') {
    await page.getByLabel('Original Argus URL',{exact:true}).fill('https://argus.example.invalid');
    await page.getByRole('button',{name:'Generate share URL',exact:true}).click();
  } else await page.getByRole('button',{name:'Create device token',exact:true}).click();
  const output=page.getByLabel(kind==='share'?'iOS share URL':'New device token',{exact:true});
  await output.evaluate(input=>window.exposedCredential=input);
  const hide=page.getByRole('button',{name:kind==='share'?'Hide share URL':'Hide device token',exact:true});
  if(mode==='hide'){await hide.focus();await page.keyboard.press('Enter');}
  else {
    await page.getByLabel('Original Argus URL',{exact:true}).focus();
    await hide.evaluate(button=>button.click());
  }
  await expect(output).toHaveCount(0);
  assert.equal(await page.evaluate(()=>window.exposedCredential.value),'');
  const target=mode==='outside'?page.getByLabel('Original Argus URL',{exact:true}):kind==='share'?page.getByRole('button',{name:'Generate share URL',exact:true}):page.getByLabel('Token label',{exact:true});
  await expect(target).toBeFocused();
  if(mode==='hide') {
    await page.keyboard.press(kind==='share'?'Shift+Tab':'Tab');
    await expect(kind==='share'?page.getByLabel('Original Argus URL',{exact:true}):page.getByRole('button',{name:'Create device token',exact:true})).toBeFocused();
  }
});
