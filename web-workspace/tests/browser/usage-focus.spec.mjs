import {test,expect} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
async function fixture(page){
 await page.route('**/usage-focus',route=>route.fulfill({contentType:'text/html',body:`<form id="composer"><button id="context-toggle" type="button"><span class="context-ring"></span></button></form><button id="outside">Outside</button><script type="module">
 import {ComposerContext} from '/composer/composer-context.js';
 const usage={task_id:'a',session_id:'s',totals:{input_tokens:42},turns:[{turn_id:'one',input_tokens:42}],next_cursor:'older'};
 window.hold=false;window.usageRequests=0;const api={request:async(path)=>path.includes('/usage?')?(usageRequests++,hold?new Promise(resolve=>window.release=()=>resolve(Response.json(usage))):Response.json(usage)):Response.json({task_id:'a',session_id:'s',live:true,context:null}),json:async()=>({})};
 window.context=new ComposerContext(api);context.select({id:'a'});await context.sessionUsage.flight;window.ready=true;
 </script>`}));
 await page.goto('/usage-focus');await page.waitForFunction(()=>window.ready);await page.locator('#context-toggle').click();await page.waitForFunction(()=>window.usageRequests>=2&&!window.context.sessionUsage.flight);
}
for(const name of ['Refresh usage','Older turns'])for(const trigger of ['event','cadence'])test(`automatic ${trigger} refresh retains focused ${name}`,async({page})=>{
 await fixture(page);const control=page.getByRole('button',{name,exact:true});await control.focus();
 await page.evaluate(trigger=>{window.hold=true;if(trigger==='event')context.event({task_id:'a',type:'task.usage_changed',payload:{session_id:'s'}});else context.refresh();},trigger);
 await expect(control).toBeDisabled();await page.evaluate(async()=>{window.release();await context.sessionUsage.flight;});await expect(control).toBeFocused();
});
for(const departure of ['outside','closed','scope'])test(`automatic usage focus recovery respects ${departure} departure`,async({page})=>{
 await fixture(page);await page.getByRole('button',{name:'Refresh usage',exact:true}).focus();
 await page.evaluate(()=>{window.hold=true;context.event({task_id:'a',type:'task.usage_changed',payload:{session_id:'s'}});});await page.waitForFunction(()=>window.release);
 if(departure==='outside')await page.locator('#outside').focus();
 else if(departure==='closed')await page.keyboard.press('Escape');
 else await page.evaluate(()=>context.select(null));
 const expected=await page.evaluate(()=>document.activeElement.id);await page.evaluate(async()=>{window.release();await new Promise(resolve=>setTimeout(resolve,0));});expect(await page.evaluate(()=>document.activeElement.id)).toBe(expected);
});
