import {test,expect} from '@playwright/test';
import {actorPage} from '../_lifecycle-fixture.ts';
const cleanup=[];test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
test('unmeasured Claude response renders a single calm line and real mismatch remains an error',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<div id="usage"></div>');
 await page.evaluate(async()=>{const {SessionUsage}=await import('/composer/session-usage.js');window.response={task_id:'a',session_id:'',totals:null,turns:[]};window.usage=new SessionUsage({json:async()=>window.response},()=>{});window.usage.select({id:'a'});window.usage.event({task_id:'a',type:'task.context_changed',payload:{session_id:'claude-bridge'}});await window.usage.flight;document.querySelector('#usage').append(window.usage.render());});
 await expect(page.getByText('Session tokens are not measured for this agent yet.')).toBeVisible();await expect(page.getByRole('alert')).toBeHidden();await expect(page.getByRole('button',{name:'Refresh usage'})).toBeHidden();
 await page.evaluate(async()=>{window.response={...window.response,session_id:'wrong'};window.usage.expectedSession='expected';await window.usage.refresh();window.usage.render();});await expect(page.getByRole('alert')).toContainText('Usage session changed');await expect(page.getByRole('button',{name:'Refresh usage'})).toBeVisible();
});
for(const outside of [false,true])test(`unmeasured usage retry preserves keyboard ownership outside=${outside}`,async({browser})=>{
 const page=await actorPage(browser,cleanup,'<section id="popover" tabindex="0"><div id="usage"></div></section><button id="outside">Outside</button>');
 await page.evaluate(async()=>{
  const {SessionUsage}=await import('/composer/session-usage.js');let initial=true;
  window.usage=new SessionUsage({json:async()=>{if(initial){initial=false;throw new Error('temporary failure');}return new Promise(resolve=>window.finishUsage=()=>resolve({task_id:'a',session_id:'',totals:null,turns:[]}));}},()=>{const group=window.usage.render();if(!group.isConnected)document.querySelector('#usage').append(group);});
  window.usage.select({id:'a'});await window.usage.refresh();
 });
 const refresh=page.getByRole('button',{name:'Refresh usage'});await refresh.focus();await page.keyboard.press('Enter');
 await page.waitForFunction(()=>!!window.finishUsage);if(outside)await page.locator('#outside').focus();
 await page.evaluate(()=>window.finishUsage());await expect(refresh).toBeHidden();
 if(outside)await expect(page.locator('#outside')).toBeFocused();else await expect(page.getByRole('region',{name:'Session tokens'})).toBeFocused();
});
