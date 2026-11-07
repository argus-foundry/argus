import { test, expect } from '@playwright/test';

async function fixture(page, backendGate = null) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    const json = path === '/api/tasks' ? {tasks:[{id:'a',name:'Focus fixture',project:'demo',status:'complete'}]} :
      path === '/api/projects/full' ? {projects:[{name:'demo',path:'/tmp/demo'}]} :
      path.endsWith('/git/status') ? {BranchFiles:'M\tfile.js'} : {};
    return route.fulfill({json});
  });
  if (backendGate) await page.route('**/api/backends', async route => { await backendGate; await route.fulfill({json:{backends:[]}}); });
  await page.goto('/');
  await page.locator('#token').fill('fixture');
  await page.locator('#connect-button').click();
  await page.locator('.file-row').first().waitFor();
}

for (const width of [1280,700]) for (const section of ['add-repository','repository:demo']) for (const move of [false,true]) {
  test(`repository load retains focus at ${width} ${section} moved=${move}`, async ({page}) => {
    let release;
    const gate = new Promise<void>(resolve => { release=resolve; });
    // Hold the shared backend read before connection to cover its cached consumers.
    await fixture(page,gate);
    await page.locator('#workspace-settings').click();
    await page.setViewportSize({width,height:800});
    await page.locator('.settings-nav-entry[data-section="repository:demo"]').waitFor({state:'attached'});
    try {
      const owner = width===700 ? page.getByLabel('Settings section',{exact:true}) : page.locator(`.settings-nav-entry[data-section="${section}"]`);
      await owner.focus();
      if(width===700) await owner.selectOption(section); else await page.keyboard.press('Enter');
      await page.getByText('Loading projects…',{exact:true}).waitFor();
      const target=move?page.locator('.settings-back'):owner;
      if(move) await target.focus();
      release();
      await page.getByLabel('Repository path',{exact:true}).waitFor();
      await expect(target).toBeFocused();
    } finally { release(); }
  });
}

test('artifact refresh retains equivalent row, neighbour and tab without stealing outside focus', async ({page}) => {
  await fixture(page);
  let filenames=['a.txt','b.txt','c.txt'];
  await page.route('**/api/tasks/a/artifacts', route => route.fulfill({json:{artifacts:filenames.map(filename=>({filename,type:'text',size:1}))}}));
  await page.locator('#tool-artifacts').click();
  const row = name => page.locator('.artifact-row').filter({hasText:name});
  await row('b.txt').focus();
  async function refresh(next) {
    filenames=next;
    const response=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/tasks/a/artifacts');
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
    await response;
    await expect(page.locator('.artifact-row')).toHaveCount(next.length);
  }
  await refresh(['z.txt','a.txt','b.txt','c.txt']);
  await expect(row('b.txt')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#tab-review')).toHaveAttribute('aria-selected','true');
  await row('b.txt').focus();
  await refresh(['z.txt','a.txt','c.txt']);
  await expect(row('c.txt')).toBeFocused();
  await refresh([]);
  await expect(page.locator('#tool-artifacts')).toBeFocused();
  await page.locator('#tab-agent').focus();
  await refresh(['a.txt']);
  await expect(page.locator('#tab-agent')).toBeFocused();
});

for (const owner of ['agents','add-repository','search','back','selector','content']) {
  test(`successful save repository reload preserves ${owner} focus`, async ({page}) => {
    await fixture(page);
    await page.locator('#workspace-settings').click();
    await page.getByRole('button',{name:'Save General',exact:true}).waitFor();
    let release, started;
    const gate=new Promise<void>(resolve=>{release=resolve;});
    const requested=new Promise<void>(resolve=>{started=resolve;});
    await page.route('**/api/projects/full',async route=>{started();await gate;await route.fulfill({json:{projects:[{name:'demo',path:'/tmp/demo'},{name:'new',path:'/tmp/new'}]}});});
    try {
      await page.getByRole('button',{name:'Save General',exact:true}).click();
      await requested;
      await expect(page.locator('.settings-nav-entry[data-section="agents"]')).toBeEnabled();
      const targets={search:page.getByLabel('Search settings',{exact:true}),back:page.locator('.settings-back'),selector:page.getByLabel('Settings section',{exact:true}),content:page.getByLabel('Vault path',{exact:true})};
      if(owner==='selector')await page.setViewportSize({width:700,height:800});
      const target=targets[owner]||page.locator(`.settings-nav-entry[data-section="${owner}"]`);
      await target.focus();
      if(owner==='agents'||owner==='add-repository')await page.keyboard.press('Enter');
      release();
      await page.locator('.settings-nav-entry[data-section="repository:new"]').waitFor({state:'attached'});
      await expect(target).toBeFocused();
    } finally {release();}
  });
}
