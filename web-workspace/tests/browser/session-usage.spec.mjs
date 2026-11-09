import {test,expect} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
test('Codex ring replaces compaction values and usage pages stay in their session',async({page})=>{
 await page.route('**/usage-fixture',route=>route.fulfill({contentType:'text/html',body:`
 <form id="composer"><textarea id="draft">preserve draft</textarea><button type="button" id="context-toggle"><span class="context-ring"></span></button></form>
 <script type="module">
 import {ComposerContext} from '/composer/composer-context.js';
 window.paths=[];window.used=12000;
 const api={request:async path=>path.includes('/usage?')?Response.json(await api.json(path)):Response.json({task_id:'a',session_id:'s',provider:'openai',source:'codex_rollout',live:true,context:{used_tokens:window.used,window_tokens:200000,percent:window.used/2000,categories:null}}),json:async path=>{
 paths.push(path);if(path.includes('/usage?'))return {task_id:'a',session_id:'s',totals:{input_tokens:1000,cached_input_tokens:500,output_tokens:null},turns:[{turn_id:path.includes('before=')?'older':'newest',input_tokens:100,cached_input_tokens:50,output_tokens:0}],next_cursor:path.includes('before=')?null:'opaque'};
 if(path==='/api/backends')return {backends:[{name:'codex',kind:'codex'}]};return {};}};
 window.context=new ComposerContext(api);context.select({id:'a',backend:'codex'});window.ready=true;
 </script>`}));
 await page.goto('/usage-fixture');await page.waitForFunction(()=>window.ready);
 await expect(page.locator('#context-toggle')).toHaveAttribute('aria-label','Context 6% used. Open usage details');
 await page.locator('#context-toggle').click();await expect(page.getByRole('heading',{name:'Session tokens'})).toBeVisible();
 await expect(page.locator('.session-usage')).toContainText('Cached input (included in input)');await expect(page.locator('.session-usage')).toContainText('Unavailable');
 await page.getByRole('button',{name:'Older turns'}).click();await expect(page.getByText('Turn older',{exact:true})).toBeVisible();
 expect(await page.evaluate(()=>paths.find(p=>p.includes('before=')))).toContain('session_id=s');
 await page.evaluate(()=>{used=4000;context.event({task_id:'a',type:'task.context_changed',payload:{session_id:'s'}});});
 await expect(page.locator('#context-toggle')).toHaveAttribute('aria-label','Context 2% used. Open usage details');
 await expect(page.locator('#draft')).toHaveValue('preserve draft');
 await page.keyboard.press('Escape');await expect(page.locator('#composer-context')).toBeHidden();
});
