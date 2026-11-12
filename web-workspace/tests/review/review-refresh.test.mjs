import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskReview} from '../../review/review.js';
import {focusDOM} from '../shared/focus-dom.mjs';
const settle=()=>new Promise(setImmediate);
const event=revision=>({type:'worktree.changed',task_id:'a',payload:{revision}});
async function fixture(t,{failure=false}={}){
 const dom=focusDOM(t),panel=dom.element('section');panel.id='review-panel';dom.body.append(panel);const lookup=document.getElementById;
 document.getElementById=id=>id==='review-panel'?panel:lookup(id);document.hidden=false;document.hasFocus=()=>true;document.addEventListener=()=>{};document.removeEventListener=()=>{};
 Object.getPrototypeOf(dom.body).getClientRects=function(){return this.isConnected?[{}]:[];};
 const previousStyle=globalThis.getComputedStyle;globalThis.getComputedStyle=()=>({visibility:'visible'});t.after(()=>{globalThis.getComputedStyle=previousStyle;});
 const calls=[],review=new TaskReview({json:(path,options)=>new Promise((resolve,reject)=>calls.push({path,...options,resolve,reject}))},()=>{});review.task={id:'a'};
 const first=review.openDiff(review.task,'file.txt');if(failure)calls[0].reject(new Error('offline'));else calls[0].resolve({Diff:'@@ -1 +1 @@\n-old\n+initial'});await first;t.after(()=>review.cancel());return {...dom,panel,review,calls};
}
for(const hidden of ['panel','document'])test(`hidden ${hidden} defers all diff events and refreshes once on show`,async t=>{
 const f=await fixture(t);if(hidden==='panel')f.panel.hidden=true;else document.hidden=true;
 for(let i=0;i<20;i++)f.review.event(event('r'+i));assert.equal(f.calls.length,1);
 f.panel.hidden=false;document.hidden=false;f.review.wake();assert.equal(f.calls.length,2);f.calls[1].resolve({Diff:'fresh'});await settle();
});
test('diff bursts share one flight, dedupe revisions and never apply a superseded response',async t=>{
 const f=await fixture(t);f.review.event(event('r2'));for(let i=0;i<20;i++)f.review.event(event('r2'));assert.equal(f.calls.length,2);assert.equal(f.calls[1].signal.aborted,false);
 f.review.event(event('r3'));f.calls[1].resolve({Diff:'obsolete'});await settle();assert.equal(f.calls.length,3);assert.equal(f.panel.querySelectorAll('.diff-code').some(node=>node.textContent==='obsolete'),false);
 f.calls[2].resolve({Diff:'fresh'});await settle();f.review.event(event('r3'));assert.equal(f.calls.length,3);
});
test('overlapping diff refreshes retain Split focus until the newest response settles',async t=>{
 const f=await fixture(t),split=f.panel.querySelectorAll('button').find(node=>node.textContent==='Split');split.focus();
 f.review.event(event('r2'));f.review.event(event('r3'));for(const call of f.calls.slice(1))call.resolve({Diff:'old'});await settle();f.calls.at(-1).resolve({Diff:'fresh'});await settle();
 assert.equal(document.activeElement.textContent,'Split');assert.notEqual(document.activeElement,document.body);
});
test('a diff response cannot apply after the selected task identity changes',async t=>{
 const f=await fixture(t);f.review.event(event('r2'));f.review.task={id:'b'};f.calls[1].resolve({Diff:'wrong task response'});await settle();
 assert.equal(f.panel.querySelectorAll('.diff-code').some(node=>node.textContent==='wrong task response'),false);
});

test('initial diff failure retains the accessible empty state without stealing outside focus',async t=>{
 const f=await fixture(t,{failure:true});const box=f.panel.querySelector('.empty-state');assert.ok(box);assert.equal(box.getAttribute('role'),'alert');assert.equal(document.activeElement,f.tab);
});
test('background diff failure preserves the toolbar and its focused control',async t=>{
 const f=await fixture(t),split=f.panel.querySelectorAll('button').find(node=>node.textContent==='Split');split.focus();f.review.event(event('r2'));f.calls[1].reject(new Error('offline'));await settle();assert.equal(document.activeElement,split);assert.ok(f.panel.querySelector('.tool-error'));
});
