import {test,expect} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
test('500 retained turn rows survive busy and unchanged refresh without allocation',async({page})=>{
 await page.route('**/usage-retained',route=>route.fulfill({contentType:'text/html',body:`<form id="composer"><button id="context-toggle" type="button"><span class="context-ring"></span></button></form><script type="module">
 import {ComposerContext} from '/composer/composer-context.js';
 const usage={task_id:'a',session_id:'s',totals:{input_tokens:42},turns:Array.from({length:500},(_,i)=>({turn_id:String(i),input_tokens:i})),next_cursor:null};
 let hold=false;window.api={request:async(path)=>path.includes('/usage?')?(hold?new Promise(resolve=>window.release=()=>resolve(Response.json(usage))):Response.json(usage)):Response.json({task_id:'a',session_id:'s',live:true,context:null}),json:async()=>({})};
 window.context=new ComposerContext(api);context.select({id:'a'});await context.sessionUsage.flight;
 window.start=()=>{hold=true;window.allocations=0;const create=document.createElement.bind(document);document.createElement=(...args)=>{allocations++;return create(...args);};window.read=context.sessionUsage.refresh();};window.ready=true;
 </script>`}));
 await page.goto('/usage-retained');await page.waitForFunction(()=>window.ready);await page.locator('#context-toggle').click();
 const turn=page.locator('.session-usage details').first();await turn.locator('summary').click();await expect(turn).toHaveAttribute('open','');
 await page.evaluate(()=>{window.first=document.querySelector('.session-usage details');window.start();});
 expect(await page.evaluate(()=>window.allocations)).toBeLessThan(10);expect(await page.evaluate(()=>window.first===document.querySelector('.session-usage details'))).toBe(true);
 await expect(turn).toHaveAttribute('open','');await page.evaluate(async()=>{window.release();await window.read;});await expect(turn).toHaveAttribute('open','');expect(await page.evaluate(()=>window.allocations)).toBeLessThan(10);
});
