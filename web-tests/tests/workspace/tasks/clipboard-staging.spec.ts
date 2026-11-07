import {test, expect} from '@playwright/test';
import {resetServer} from '../../_helpers';

async function fixture(page, {staging = 'First staged\nsecond line', failure = false} = {}) {
  const requests = [], messages = [];
  page.on('console', message => messages.push(message.text()));
  await page.addInitScript(() => {
    window.copied = []; window.sources = []; window.copyDenied = false;
    Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{
      if(window.copyDenied)throw new Error('Denied');window.copied.push(text);
    }}});
    window.EventSource = class extends EventTarget {
      constructor(url) {super();this.url=url;window.sources.push(this);queueMicrotask(()=>this.onopen?.({}));}
      close() {this.closed=true;}
    };
  });
  const tasks = ['a','b'].map(id=>({id,name:`Clipboard ${id}`,project:'clipboard',status:'in_progress',live:true,idle:false}));
  await page.route('**/api/**',route=>{
    const path=new URL(route.request().url()).pathname;
    requests.push({path,method:route.request().method()});
    if(path==='/api/tasks')return route.fulfill({json:{tasks}});
    if(path==='/api/projects/full')return route.fulfill({json:{projects:[]}});
    if(path==='/api/hera')return route.fulfill({json:{orchestrators:[]}});
    if(path.endsWith('/clipboard'))return route.fulfill(failure?{status:503,json:{}}:
      path.includes('/a/') && staging !== null?{json:{text:staging}}:{status:204});
    if(path.endsWith('/size'))return route.fulfill({json:{cols:80,rows:24}});
    if(path.endsWith('/output'))return route.fulfill({body:'VISIBLE OUTPUT\r\n',headers:{'X-Output-Total':'16'}});
    if(path.endsWith('/links'))return route.fulfill({json:{links:[]}});
    if(path.endsWith('/meta'))return route.fulfill({json:{entries:[]}});
    if(tasks.some(task=>path===`/api/tasks/${task.id}`))return route.fulfill({json:tasks.find(task=>path.endsWith('/'+task.id))});
    return route.fulfill({status:404,json:{}});
  });
  await page.goto(process.env.WORKSPACE_TEST_URL!);
  await page.locator('#token').fill('test-token');
  await page.locator('#connect-button').click();
  await page.waitForFunction(()=>document.querySelector('#terminal-state')?.textContent==='Live terminal');
  return {requests,messages,setStaging: value => { staging = value; }};
}
async function frame(page,value,id='a') {
  await page.evaluate(({value,id})=>{
    const source=window.sources.findLast(source=>source.url.includes(`/tasks/${id}/stream?`)&&!source.closed);
    source.dispatchEvent(new MessageEvent('clipboard',{data:typeof value==='string'?value:JSON.stringify(value)}));
  },{value,id});
}
test.beforeAll(async()=>{await resetServer();});

for(const width of [1280,390])test(`staged copy preserves text, live updates and focused clearing at ${width}px`,async({page})=>{
  await page.setViewportSize({width,height:900});
  const {requests,setStaging}=await fixture(page);
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});
  await expect(copy).toBeVisible();
  await expect(copy).toHaveAttribute('data-tooltip','First staged second line');
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await copy.hover();await expect(page.getByRole('tooltip')).toHaveText('First staged second line');
  await copy.focus();await page.keyboard.press('Enter');await page.keyboard.press('Space');
  await expect.poll(()=>page.evaluate(()=>window.copied)).toEqual(['First staged\nsecond line','First staged\nsecond line']);
  await expect(page.locator('#notice')).toHaveText('Copied staged text.');
  await expect(copy).toBeVisible();
  expect(requests.filter(request=>request.method==='DELETE')).toEqual([]);
  await frame(page,{text:'<script>literal</script>\n'+ 'z'.repeat(180)});
  await expect(copy).toHaveAttribute('data-tooltip',/^<script>literal<\/script> z+…$/);
  expect((await copy.getAttribute('data-tooltip'))!.length).toBeLessThanOrEqual(121);
  await frame(page,{text:''});await copy.click();
  await expect.poll(()=>page.evaluate(()=>window.copied.at(-1))).toBe('');
  setStaging(null);await copy.focus();await frame(page,{cleared:true});
  await expect(copy).toBeHidden();await expect(page.locator('#draft')).toBeFocused();
  await frame(page,'bad json');await frame(page,{text:17});
  await expect(page.locator('#terminal-state')).toHaveText('Live terminal');
  await frame(page,{text:'replacement'});await copy.focus();
  expect(await copy.evaluate(el=>getComputedStyle(el).outlineStyle)).not.toBe('none');
  await page.locator('#draft').focus();await frame(page,{cleared:true});
  await expect(page.locator('#draft')).toBeFocused();
  const bounds=await page.evaluate(()=>({scroll:document.documentElement.scrollWidth,width:innerWidth}));
  expect(bounds.scroll).toBeLessThanOrEqual(bounds.width);
});

test('visible output shortcut stays separate and copy denial uses shared feedback',async({page})=>{
  await fixture(page);
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});await expect(copy).toBeVisible();
  await page.locator('#terminal .xterm-helper-textarea').focus();await page.keyboard.press('Meta+Shift+C');
  await expect.poll(()=>page.evaluate(()=>window.copied.length)).toBe(1);
  expect(await page.evaluate(()=>window.copied[0])).toContain('VISIBLE OUTPUT');
  expect(await page.evaluate(()=>window.copied[0])).not.toContain('First staged');
  await expect(page.locator('#notice')).toHaveText('Copied visible output.');
  await page.evaluate(()=>{window.copyDenied=true;});await copy.click();
  await expect(page.locator('#notice')).toContainText('Copy failed');await expect(copy).toBeVisible();
});

test('selection isolates cached and split stream frames and reconnect rereads staging',async({page})=>{
  const {requests}=await fixture(page);
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});await expect(copy).toBeVisible();
  await page.locator('.task-row').filter({hasText:'Clipboard b'}).click();
  await expect(copy).toBeHidden();
  await page.evaluate(()=>window.sources.find(source=>source.url.includes('/tasks/a/stream?')).dispatchEvent(new MessageEvent('clipboard',{data:'{"text":"late a"}'})));
  await expect(copy).toBeHidden();
  await page.locator('#split-toggle').click();await page.locator('#secondary-task-picker').selectOption('a');
  await page.waitForFunction(()=>window.sources.filter(source=>source.url.includes('/tasks/a/stream?')&&!source.closed).length>0);
  await frame(page,{text:'secondary a'});await expect(copy).toBeHidden();
  await frame(page,{text:'current b'},'b');await expect(copy).toBeVisible();await copy.click();
  await expect.poll(()=>page.evaluate(()=>window.copied.at(-1))).toBe('current b');
  const reads=requests.filter(request=>request.path==='/api/tasks/b/clipboard').length;
  await page.evaluate(()=>window.sources.findLast(source=>source.url.includes('/tasks/b/stream?')&&!source.closed).onmessage({data:'%%%'}));
  await page.getByRole('button',{name:'Reconnect',exact:true}).click();
  await expect.poll(()=>requests.filter(request=>request.path==='/api/tasks/b/clipboard').length).toBeGreaterThan(reads);
  await expect(copy).toBeHidden();
  await page.locator('#disconnect').click();await expect(copy).toBeHidden();
});

test('read failure keeps terminal usable and live staging recovers without logging content',async({page})=>{
  const {messages}=await fixture(page,{failure:true});
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});await expect(copy).toBeHidden();
  await expect(page.locator('#terminal-state')).toHaveText('Live terminal');
  await frame(page,{text:'SECRET_PAYLOAD'});await expect(copy).toBeVisible();
  expect(messages.join('\n')).not.toContain('SECRET_PAYLOAD');
});

test('existing daemon staging reaches the workspace through real REST and SSE',async({page})=>{
  await resetServer();
  const headers={Authorization:'Bearer test-token','Content-Type':'application/json'};
  const api=process.env.WORKSPACE_TEST_API!;
  const tasks=(await (await fetch(api+'/api/tasks',{headers})).json()).tasks;
  const task=tasks.find(task=>task.name==='echo-bash');
  expect(task?.worktree_path).toContain('/argus-test-home-');
  const path=api+`/api/tasks/${task.id}/clipboard`;
  const stage=async text=>expect((await fetch(path,{method:'POST',headers,body:JSON.stringify({text})})).ok).toBe(true);
  await stage('Already staged');
  await page.addInitScript(()=>{
    window.copied=[];Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>window.copied.push(text)}});
  });
  await page.goto(process.env.WORKSPACE_TEST_URL!);await page.locator('#token').fill('test-token');await page.locator('#connect-button').click();
  await expect(page.locator('#terminal-state')).toHaveText('Live terminal');
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});
  await expect(copy).toHaveAttribute('data-tooltip','Already staged');
  await copy.click();await copy.click();
  await expect.poll(()=>page.evaluate(()=>window.copied)).toEqual(['Already staged','Already staged']);
  expect(await (await fetch(path,{headers})).json()).toEqual({text:'Already staged'});
  await stage('Live SSE replacement');await expect(copy).toHaveAttribute('data-tooltip','Live SSE replacement');
  await stage('');await expect(copy).toHaveAttribute('data-tooltip','Empty staged text');
  await copy.click();await expect.poll(()=>page.evaluate(()=>window.copied.at(-1))).toBe('');
  expect(await (await fetch(path,{headers})).json()).toEqual({text:''});
  await copy.focus();expect((await fetch(path,{method:'DELETE',headers})).ok).toBe(true);
  await expect(copy).toBeHidden();await expect(page.locator('#draft')).toBeFocused();
});

test('focused staging preview and accessible description track the live copy payload',async({page})=>{
  await fixture(page,{staging:'FIRST'});
  const copy=page.getByRole('button',{name:'Copy staged text',exact:true});
  await page.keyboard.press('Tab');await copy.focus();
  const bubble=page.getByRole('tooltip');
  await expect(bubble).toHaveText('FIRST');
  await frame(page,{text:'SECOND'});
  await expect(copy).toBeFocused();
  await expect(bubble).toHaveText('SECOND');
  await expect(copy).toHaveAccessibleDescription('SECOND');
  await page.keyboard.press('Enter');
  await expect.poll(()=>page.evaluate(()=>window.copied.at(-1))).toBe('SECOND');
});
