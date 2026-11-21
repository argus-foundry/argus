import {test,expect,Page} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {createHash} from 'node:crypto';
const shell=readFileSync(resolve(__dirname,'../../internal/api/static/decide.html'),'utf8');
async function setup(page:Page,override:any={}){
 const sends:any[]=[];let resolved=false;
 const item={id:11,kind:'decision',updated_at:'2026-11-20T00:00:00Z',subject:'Choose a path',body:'Hello **bold** and plain text.\n\n- First bullet\n- Nested detail https://example.com',structured:{options:[{id:'a',label:'Path A',summary:'Shared option text',upside:'Faster',downside:'More risk'}],recommended:'a',reason:'Worth the risk'},...override};
 await page.addInitScript(()=>{window.close=()=>{(globalThis as any).attemptedClose=true;};});
 await page.route('**/*',async route=>{const path=new URL(route.request().url()).pathname;
 if(path==='/decide')return route.fulfill({contentType:'text/html',body:shell});
 if(path.endsWith('decision-session'))return route.fulfill({json:{session:'test-session',initial_role_id:1}});
 if(path.endsWith('decision-navigation'))return route.fulfill({json:{roles:[{role_id:1,orchestrator:'Test',coordinator:'coord',decisions:resolved||item.kind==='fyi'?0:1,fyis:!resolved&&item.kind==='fyi'?1:0}]}});
 if(path.endsWith('decide-items'))return route.fulfill({json:{items:resolved?[]:[item]}});
 if(path.endsWith('/answers')){const a=route.request().postDataJSON().answers;sends.push(...a);resolved=a.some((v:any)=>v.action!=='comment');return route.fulfill({json:{results:a.map((v:any)=>({item_id:v.item_id,action_id:v.action_id,outcome:v.action==='comment'?'commented':'resolved'}))}});}
 return route.fulfill({status:404});});
 await page.goto('about:blank');await page.goto('http://localhost/decide#t=launch');await expect(page.locator('#decision-11')).toBeVisible();return sends;
}
async function select(page:Page,start:number,end:number){await page.locator('[data-field="body"]').evaluate((field,{start,end})=>{const walker=document.createTreeWalker(field,NodeFilter.SHOW_TEXT);const r=document.createRange();let n:Node|null,at=0,started=false;while(n=walker.nextNode()){const len=n.textContent!.length;if(!started&&start<=at+len){r.setStart(n,start-at);started=true;}if(end<=at+len){r.setEnd(n,end-at);break;}at+=len;}const s=getSelection()!;s.removeAllRanges();s.addRange(r);field.dispatchEvent(new MouseEvent('mouseup',{bubbles:true}));},{start,end});await expect(page.locator('#selection-toolbar')).toBeVisible();}
const panel=(page:Page)=>page.locator((page.viewportSize()!.width<851?'#sheet-content':'#annotation-panel'));
test('cross-node comment, overlapping delete, independent removal and phone sheet',async({page})=>{
 await setup(page);await select(page,2,18);await page.locator('#selection-toolbar').getByRole('button',{name:'Comment',exact:true}).click();
 await panel(page).getByRole('textbox',{name:'Comment',exact:true}).fill('Please clarify');await panel(page).getByRole('button',{name:'Save comment'}).click();
 if(page.viewportSize()!.width<851)await page.locator('#sheet-close').click();
 await select(page,6,23);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();
 await page.locator('#decision-11').getByRole('button',{name:/Annotations/}).click();
 await expect(panel(page).locator('.annotation-card')).toHaveCount(2);await expect(page.locator('input:checked')).toHaveCount(0);
 await panel(page).locator('.annotation-card').filter({hasText:'Please clarify'}).getByRole('button',{name:'Remove'}).click();
 await expect(panel(page).locator('.annotation-card')).toHaveCount(1);await expect(page.locator('.range-delete').first()).toBeAttached();
 if(page.viewportSize()!.width<851){await page.keyboard.press('Escape');await expect(page.locator('#decision-11 .annotation-trigger')).toBeFocused();}
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
test('annotation-only Submit keeps page and decision open, clears drafts and shows receipt',async({page})=>{
 const sends=await setup(page);await select(page,0,20);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();
 await page.getByRole('button',{name:'Submit',exact:true}).click();await expect(page.locator('#status')).toContainText('Actions sent');await expect(page.locator('.receipt')).toHaveCount(0);await expect(page.locator('#decision-11')).toBeVisible();await expect(page.locator('#tally')).toContainText('0 queued');
 expect(sends[0].action).toBe('comment');expect(sends[0].choice).toBe('');expect(sends[0].annotations[0].type).toBe('DELETION');expect(sends[0].annotations[0].selected_text_hash).toBe(createHash('sha256').update('Hello bold and plain').digest('hex'));expect(await page.evaluate(()=>(globalThis as any).attemptedClose)).toBeUndefined();
 await page.locator('input[value="a"]').check();await page.getByRole('button',{name:'Submit',exact:true}).click();await expect(page.locator('#decision-11')).toHaveCount(0);await expect(page.getByRole('tab',{name:'Decisions, 0 open'})).toBeVisible();
});
test('Submit & close shares annotated payload path and confirms all accepted actions',async({page})=>{
 const sends=await setup(page);await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.getByRole('button',{name:'Submit & close',exact:true}).click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(sends[0].annotations).toHaveLength(1);expect(await page.evaluate(()=>(globalThis as any).attemptedClose)).toBe(true);
});

test('cross-paragraph range, keyboard fallback, option isolation and native links',async({page})=>{
 const sends=await setup(page);await page.locator('[data-field="body"]').focus();await page.keyboard.press('Enter');await expect(page.locator('#selection-toolbar button').first()).toBeFocused();await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();
 await page.getByRole('button',{name:'Submit',exact:true}).click();await expect(page.locator('#status')).toContainText('Actions sent');await expect(page.locator('.receipt')).toHaveCount(0);
 expect(sends[0].annotations[0].selected_text_hash).toBe(createHash('sha256').update('Hello bold and plain text.\nFirst bullet\nNested detail https://example.com').digest('hex'));
 await expect(page.locator('#note-11')).toBeEnabled();
 await page.locator('[data-field="options.a.summary"]').evaluate(field=>{const r=document.createRange();r.selectNodeContents(field);getSelection()!.removeAllRanges();getSelection()!.addRange(r);field.dispatchEvent(new MouseEvent('mouseup',{bubbles:true}));});await expect(page.locator('#selection-toolbar')).toBeVisible();await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await expect(page.locator('input:checked')).toHaveCount(0);
 await page.getByRole('button',{name:'Submit',exact:true}).click();await expect.poll(()=>sends.length).toBe(2);expect(sends[1].annotations[0].start_path).toBe('options.a.summary');
 const link=page.locator('[data-field="body"] a');await page.route('https://example.com/',r=>r.fulfill({body:'Linked page'}));const popup=page.waitForEvent('popup');await link.click();await (await popup).close();
});
test('comment edit preserves ID and limits retain composer text; remove last annotation unqueues',async({page})=>{
 await setup(page);await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Comment',exact:true}).click();
 await panel(page).getByRole('textbox',{name:'Comment',exact:true}).fill('🚀'.repeat(1025));await panel(page).getByRole('button',{name:'Save comment'}).click();await expect(page.locator('#status')).toContainText('4 KiB');await expect(panel(page).getByRole('textbox')).toHaveValue('🚀'.repeat(1025));
 await panel(page).getByRole('textbox').fill('Original');await panel(page).getByRole('button',{name:'Save comment'}).click();const first=await page.evaluate(()=>JSON.parse(localStorage.getItem('board-responses')!)['1:11'].draft.annotations[0].annotation_id);
 await panel(page).getByRole('button',{name:'Edit',exact:true}).click();await panel(page).getByRole('textbox').fill('Edited');await panel(page).getByRole('textbox').press('Control+Enter');await expect(panel(page)).toContainText('Edited');expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('board-responses')!)['1:11'].draft.annotations[0].annotation_id)).toBe(first);
 await panel(page).getByRole('button',{name:'Remove',exact:true}).click();if(page.viewportSize()!.width<851)await page.locator('#sheet-close').click();await expect(page.locator('#tally')).toContainText('0 queued');await expect(page.locator('#submit')).toBeDisabled();await expect(page.locator('#submit-close')).toBeDisabled();
});
test('annotated uncertain payload is frozen across reload and either submit control retries exactly',async({page})=>{
 await setup(page);let calls:any[]=[];let fail=true;await page.route('**/board/answers',async route=>{const a=route.request().postDataJSON().answers;calls.push(a);if(fail)return route.abort();return route.fulfill({json:{results:a.map((v:any)=>({item_id:v.item_id,action_id:v.action_id,outcome:'commented'}))}});});
 await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#submit').click();await expect(page.locator('#main h1')).toHaveText('Queued responses');await expect(page.getByRole('button',{name:'Clear queued response',exact:true})).toBeDisabled();
 await page.reload();await expect(page.locator('#note-11')).toBeDisabled();await page.locator('#decision-11 .annotation-trigger').click();await expect(panel(page).getByRole('button',{name:'Remove'})).toBeDisabled();if(page.viewportSize()!.width<851)await page.locator('#sheet-close').click();fail=false;await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(calls).toHaveLength(2);expect(calls[1]).toEqual(calls[0]);
});
test('revision conflicts retain recoverable stale annotations and block resubmission',async({page})=>{
 await setup(page);await page.route('**/board/answers',route=>{const a=route.request().postDataJSON().answers[0];return route.fulfill({json:{results:[{item_id:a.item_id,action_id:a.action_id,outcome:'conflict',item:{id:11,kind:'decision',updated_at:'new-revision',subject:'Revised',body:'Changed text'}}]}});});
 await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#submit').click();await page.getByRole('button',{name:'Edit on board'}).click();await expect(page.locator('#decision-11')).toContainText('Stale draft');await expect(page.locator('#decision-11')).toContainText('Hello');await expect(page.locator('#submit')).toBeDisabled();await page.getByRole('button',{name:'Remove stale draft'}).click();await expect(page.locator('#decision-11')).not.toContainText('Hello');
});

test('dark theme annotations stay within viewport and retain modal focus',async({page},testInfo)=>{
 await setup(page);await page.locator('#theme').selectOption('dark');await select(page,0,18);await page.locator('#selection-toolbar').getByRole('button',{name:'Comment',exact:true}).click();await panel(page).getByRole('textbox',{name:'Comment',exact:true}).fill('Inspect the darker presentation.');await panel(page).getByRole('button',{name:'Save comment'}).click();
 if(page.viewportSize()!.width<851){await page.locator('#sheet-close').focus();await page.keyboard.press('Shift+Tab');await expect(panel(page).getByRole('button',{name:'General comment ↗'})).toBeFocused();}
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 expect(await page.locator('#selection-toolbar').evaluate(n=>getComputedStyle(n).position)).toBe('fixed');
 if(page.viewportSize()!.width>=851){const main=await page.locator('.board-column').boundingBox(),aside=await page.locator('#annotation-panel').boundingBox();expect(aside!.x).toBeGreaterThanOrEqual(main!.x+main!.width-1);}
 else{const bounds=await page.locator('#annotation-sheet').boundingBox();expect(bounds!.y+bounds!.height).toBeLessThanOrEqual(page.viewportSize()!.height+1);expect(await page.locator('#annotation-sheet').evaluate(n=>getComputedStyle(n).position)).toBe('fixed');}
 await page.screenshot({path:testInfo.outputPath('annotations-dark.png'),fullPage:true});
});

test('shared Go/browser vectors prove rendered fields, UTF-16 anchors and global context',async({page})=>{
 const vectors=JSON.parse(readFileSync(resolve(__dirname,'../../internal/db/testdata/decide-annotation-vectors.json'),'utf8'));
 for(const vector of vectors){
  const sends=await setup(page,{body:vector.body,structured:vector.structured||null});
  expect(await page.locator('#decision-11 [data-field]').evaluateAll(nodes=>nodes.map(n=>({path:(n as HTMLElement).dataset.field,text:n.textContent})))).toEqual(vector.fields);
  await page.locator('#decision-11').evaluate((card,a)=>{const fields=[...card.querySelectorAll('[data-field]')],find=(path:string,offset:number)=>{const f=fields.find(n=>(n as HTMLElement).dataset.field===path)!;const w=document.createTreeWalker(f,NodeFilter.SHOW_TEXT);let n:Node|null,at=0;while(n=w.nextNode()){if(offset<=at+n.textContent!.length)return {node:n,offset:offset-at};at+=n.textContent!.length;}throw Error('Bad test offset');};const s=find(a.start_path,a.start_offset),e=find(a.end_path,a.end_offset),range=document.createRange();range.setStart(s.node,s.offset);range.setEnd(e.node,e.offset);getSelection()!.removeAllRanges();getSelection()!.addRange(range);card.dispatchEvent(new MouseEvent('mouseup',{bubbles:true}));},vector.selection);
  await expect(page.locator('#selection-toolbar')).toBeVisible();await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#submit').click();await expect.poll(()=>sends.length).toBe(1);
  await expect(page.locator('#note-11')).toBeEnabled();const a=sends[0].annotations[0];expect(a).toMatchObject({...vector.selection,prefix:vector.prefix,suffix:vector.suffix,selected_text_hash:createHash('sha256').update(vector.selected).digest('hex')});
 }
});

test('identical ranges remain independent at the 20 annotation cap',async({page})=>{
 await setup(page);await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();
 await page.evaluate(()=>{const saved=JSON.parse(localStorage.getItem('board-responses')!),d=saved['1:11'].draft;d.annotations=Array.from({length:20},(_,n)=>({...d.annotations[0],annotation_id:'overlap-'+n}));localStorage.setItem('board-responses',JSON.stringify(saved));});await page.reload();await expect(page.locator('#decision-11 .annotation-trigger')).toContainText('20');
 await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await expect(page.locator('#status')).toContainText('Maximum 20');
 await page.locator('#decision-11 .annotation-trigger').click();await expect(panel(page).locator('.annotation-card')).toHaveCount(20);await panel(page).locator('.annotation-card').first().getByRole('button',{name:'Remove'}).click();await expect(panel(page).locator('.annotation-card')).toHaveCount(19);await expect(page.locator('mark[data-ann="overlap-19"]')).toBeAttached();await expect(page.locator('mark[data-ann="overlap-0"]')).toHaveCount(0);
});

test('FYI annotations convert dismiss visibly and plain decision comments retain open semantics',async({page})=>{
 const sends=await setup(page,{kind:'fyi',structured:null});await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#decision-11').getByRole('button',{name:'Dismiss',exact:true}).click();await expect(page.getByRole('button',{name:'Comment and dismiss',exact:true})).toHaveAttribute('aria-pressed','true');
 let action:any;await page.route('**/board/fyi-actions',route=>{action=route.request().postDataJSON().actions[0];return route.fulfill({json:{results:[{item_id:action.item_id,action_id:action.action_id,outcome:'resolved'}]}});});await page.locator('#submit-close').click();await expect(page.locator('h1')).toHaveText('Actions sent — you can close this tab');expect(action.annotations).toHaveLength(1);expect(action.action).toBe('comment_and_dismiss');
 await setup(page,{structured:null});await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#note-11').fill('General feedback, keep this open');const response=page.waitForResponse(r=>r.url().endsWith('/answers'));await page.locator('#submit').click();const payload=(await response).request().postDataJSON().answers[0];expect(payload.action).toBe('comment');expect(payload.choice).toBe('');await expect(page.locator('#decision-11')).toBeVisible();expect(sends).toHaveLength(0);
});

test('Submit keeps edits disabled until its accepted refresh completes',async({page})=>{
 await setup(page);let release!:()=>void;const gate=new Promise<void>(resolve=>release=resolve);await page.route('**/decision-navigation',async route=>{await gate;return route.fulfill({json:{roles:[{role_id:1,orchestrator:'Test',coordinator:'coord',decisions:1,fyis:0}]}});});
 await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();await page.locator('#submit').click();await expect(page.locator('#note-11')).toBeDisabled();await expect(page.locator('#submit-close')).toBeDisabled();release();await expect(page.locator('#note-11')).toBeEnabled();await expect(page.locator('#status')).toContainText('Actions sent');await expect(page.locator('.receipt')).toHaveCount(0);await expect(page.locator('#tally')).toContainText('0 queued');
});

test('long selections retain full ranges and hashes while previews stop at 512 UTF-8 bytes',async({page})=>{
 const body='😀'.repeat(300)+' end',sends=await setup(page,{body,structured:null});await page.locator('[data-field="body"]').focus();await page.keyboard.press('Enter');await page.locator('#selection-toolbar').getByRole('button',{name:'Delete',exact:true}).click();const quote=await page.evaluate(()=>JSON.parse(localStorage.getItem('board-responses')!)['1:11'].draft.annotations[0].quote);expect(Buffer.byteLength(quote)).toBe(512);expect(quote).toBe('😀'.repeat(128));await page.locator('#submit').click();await expect.poll(()=>sends.length).toBe(1);const a=sends[0].annotations[0];expect(a.start_offset).toBe(0);expect(a.end_offset).toBe(604);expect(a.selected_text_hash).toBe(createHash('sha256').update(body).digest('hex'));
});

test('an unsaved composer cannot attach old feedback to a refreshed revision',async({page})=>{
 await page.clock.install();await setup(page);await select(page,0,5);await page.locator('#selection-toolbar').getByRole('button',{name:'Comment',exact:true}).click();await panel(page).getByRole('textbox').fill('Keep this feedback on the original revision');
 await page.route('**/board/decide-items',route=>route.fulfill({json:{items:[{id:11,kind:'decision',updated_at:'new-revision',subject:'Edited item',body:'Hello **bold** and plain text.\n\n- First bullet\n- Nested detail https://example.com'}]}}));await page.clock.fastForward(20000);await expect(page.locator('#updates')).toBeVisible();
 if(page.viewportSize()!.width<851)await page.locator('#refresh').evaluate((b:HTMLButtonElement)=>b.click());else await page.locator('#refresh').click();
 await panel(page).getByRole('button',{name:'Save comment'}).click();if(page.viewportSize()!.width<851)await page.locator('#sheet-close').click();await expect(page.locator('#decision-11')).toContainText('Stale draft');await expect(page.locator('#decision-11')).toContainText('Keep this feedback on the original revision');await expect(page.locator('#tally')).toContainText('0 queued');await expect(page.locator('#submit')).toBeDisabled();
});

test('touch selection settles into the toolbar without switching tabs or stealing the range',async({page})=>{
 await setup(page);await page.locator('[data-field="body"]').evaluate(field=>{const text=field.firstChild!.firstChild!,range=document.createRange();range.setStart(text,0);range.setEnd(text,5);getSelection()!.removeAllRanges();getSelection()!.addRange(range);const start=new Event('touchstart',{bubbles:true});Object.defineProperty(start,'touches',{value:[{clientX:220,clientY:200}]});field.dispatchEvent(start);const end=new Event('touchend',{bubbles:true});Object.defineProperty(end,'changedTouches',{value:[{clientX:100,clientY:205}]});field.dispatchEvent(end);});await expect(page.locator('#selection-toolbar')).toBeVisible();await expect(page.getByRole('tab',{name:'Decisions, 1 open'})).toHaveAttribute('aria-selected','true');expect(await page.evaluate(()=>getSelection()!.toString())).toBe('Hello');const bounds=await page.locator('#selection-toolbar').boundingBox();expect(bounds!.x).toBeGreaterThanOrEqual(0);expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
});
