import { test, expect } from '@playwright/test';

for (const [kind, dropped] of [['claude', true], ['', true], ['codex', false]]) {
  test(`terminal input stays responsive before backend classification (${JSON.stringify(kind)})`, async ({page}) => {
    let release, started;
    const gate = new Promise<void>(resolve => { release = resolve; });
    const requested = new Promise<void>(resolve => { started = resolve; });
    const writes = [];
    await page.route('**/api/backends', async route => {
      started();
      await gate;
      await route.fulfill({json:{backends:[{name:'alias',kind}]}});
    });
    await page.route('**/api/tasks/input-fixture/input', async route => {
      writes.push(route.request().postData());
      await route.fulfill({body:''});
    });
    await page.goto('/');
    await page.evaluate(async () => {
      const {Api} = await import('/core/api.js');
      const {TerminalInput} = await import('/terminal/terminal-input.js');
      const task = {id:'input-fixture',backend:'alias',status:'in_progress',live:true};
      window.focusInput = new TerminalInput(new Api(() => task),task,new AbortController().signal,() => true);
    });
    await requested;
    try {
      await page.evaluate(async () => {
        await window.focusInput.raw('\x1b[O');
        await window.focusInput.raw('typed');
        await window.focusInput.raw('\x03');
      });
      expect(writes).toEqual(['typed','\x03']);
    } finally { release(); }
    await expect.poll(() => page.evaluate(() => typeof window.focusInput.claudeFamily)).toBe('boolean');
    await page.evaluate(async () => {
      await window.focusInput.raw('\x1b[O');
      await window.focusInput.raw('\x1b[I');
    });
    expect(writes).toEqual(dropped ? ['typed','\x03','\x1b[I'] : ['typed','\x03','\x1b[O','\x1b[I']);
  });
}
