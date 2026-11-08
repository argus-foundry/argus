import test from 'node:test';
import assert from 'node:assert/strict';
import { ComposerBridge } from '../../composer/composer-bridge.js';
import { ComposerSuggestion } from '../../composer/composer-suggestion.js';

const now = Date.parse('2026-11-08T15:00:00Z');
const bridge = (text = 'Continue with tests') => ({task_id:'a',live:true,session_id:'s',generation:'g',last_event_at:new Date(now).toISOString(),suggestion:{text,session_id:'s',generation:'g',recorded_at:new Date(now).toISOString()}});
function fixture(t, read = async () => bridge()) {
  const original = globalThis.document;
  globalThis.document = new EventTarget(); document.hidden = false;
  t.mock.timers.enable({apis:['setTimeout','Date'],now});
  const draft = {value:'',placeholder:'Message your agent…'}, calls = [];
  const hint = new ComposerSuggestion({json:(...args)=>{calls.push(args);return read(...args);}},draft);
  t.after(()=>{hint.select(null);globalThis.document=original;});
  return {hint,draft,calls};
}
const settle = async () => { for(let i=0;i<8;i++) await Promise.resolve(); };

test('live hint is placeholder text only, and never replaces typed or restored drafts',async t=>{
  const {hint,draft}=fixture(t,async()=>bridge('<b>Literal</b>\nNext'));
  hint.select({id:'a'});await settle();
  assert.equal(draft.placeholder,'<b>Literal</b>\nNext');assert.equal(draft.value,'');
  for(const value of ['typed draft','restored draft',' ']){
    draft.value=value;hint.render();assert.equal(draft.placeholder,'Message your agent…');assert.equal(draft.value,value);
  }
  draft.value='';hint.render();assert.equal(draft.placeholder,'<b>Literal</b>\nNext');
});

test('missing, unsupported, stale and mismatched authority all retain normal hint',async t=>{
  let data; const {hint,draft}=fixture(t,async()=>data);
  for(const value of [null,{...bridge(),suggestion:null},{...bridge(),live:false},{...bridge(),session_id:'other'},{...bridge(),generation:'other'},{...bridge(),task_id:'b'},{...bridge(),last_event_at:new Date(now-45000).toISOString()}]){
    data=value;hint.select({id:'a'});await settle();assert.equal(draft.placeholder,'Message your agent…');
  }
});

test('selection, newer refresh and disconnect fence even reads that ignore abort',async t=>{
  const held=[];const {hint,draft,calls}=fixture(t,()=>new Promise(resolve=>held.push(resolve)));
  hint.select({id:'a'});hint.select({id:'b'});
  assert.equal(calls[0][1].signal.aborted,true);
  held[0](bridge());held[1]({...bridge('B'),task_id:'b'});await settle();assert.equal(draft.placeholder,'B');
  const old=hint.refresh();hint.event({type:'task.suggestion_changed',task_id:'b',payload:{available:false}});
  held[2]({...bridge('old'),task_id:'b'});await old;hint.render();assert.equal(draft.placeholder,'Message your agent…');
  t.mock.timers.tick(100);hint.select(null);held[3]({...bridge('late'),task_id:'b'});await settle();assert.equal(draft.placeholder,'Message your agent…');
});

test('suggestion bursts debounce a GET, clear immediately and ignore other tasks',async t=>{
  let data=bridge();const {hint,draft,calls}=fixture(t,async()=>data);
  hint.select({id:'a'});await settle();
  hint.event({type:'task.suggestion_changed',task_id:'b'});assert.equal(draft.placeholder,'Continue with tests');
  data={...bridge(),suggestion:null};
  for(let i=0;i<5;i++)hint.event({type:'task.suggestion_changed',task_id:'a',payload:{text:'never trust event'}});
  assert.equal(draft.placeholder,'Message your agent…');t.mock.timers.tick(100);await settle();
  assert.equal(calls.length,2);assert.equal(draft.placeholder,'Message your agent…');
});

test('polls only visible empty hints, serializes reads and expires authority without events',async t=>{
  const {hint,draft,calls}=fixture(t);
  hint.select({id:'a'});await settle();t.mock.timers.tick(15000);await settle();assert.equal(calls.length,2);
  draft.value='draft';hint.render();t.mock.timers.tick(15000);await settle();assert.equal(calls.length,2);
  draft.value='';hint.render();document.hidden=true;document.dispatchEvent(new Event('visibilitychange'));
  t.mock.timers.tick(15000);await settle();assert.equal(calls.length,2);assert.equal(draft.placeholder,'Message your agent…');
  document.hidden=false;document.dispatchEvent(new Event('visibilitychange'));await settle();assert.equal(calls.length,3);
});

test('no hint means no poll; unavailable reads clear the hint',async t=>{
  let fail=false;const {hint,draft,calls}=fixture(t,async()=>{if(fail)throw Object.assign(new Error('missing'),{status:404});return bridge();});
  hint.select({id:'a'});await settle();fail=true;await hint.refresh();assert.equal(draft.placeholder,'Message your agent…');
  t.mock.timers.tick(60000);await settle();assert.equal(calls.length,2);
});

test('frequent composer renders cannot postpone the authority poll',async t=>{
 const {hint,calls}=fixture(t);hint.select({id:'a'});await settle();
 t.mock.timers.tick(10000);hint.render();t.mock.timers.tick(5000);await settle();assert.equal(calls.length,2);
});

test('a pending authority read is single-flight and a new turn cancels it',async t=>{
 let release,block=false;
 const {hint,draft,calls}=fixture(t,async()=>block?new Promise(resolve=>release=resolve):bridge());
 hint.select({id:'a'});await settle();block=true;
 const read=hint.refresh();hint.refresh();hint.refresh();assert.equal(calls.length,2);
 hint.newTurn();assert.equal(calls[1][1].signal.aborted,true);release(bridge());await read;
 assert.equal(draft.placeholder,'Message your agent…');t.mock.timers.tick(15000);await settle();assert.equal(calls.length,2);
});

test('resuming a suspended page clears expired text before the authority read settles',async t=>{
 let block=false,release;
 const {hint,draft}=fixture(t,async()=>block?new Promise(resolve=>release=resolve):bridge());
 hint.select({id:'a'});await settle();document.hidden=true;document.dispatchEvent(new Event('visibilitychange'));
 t.mock.timers.setTime(now+60000);block=true;document.hidden=false;document.dispatchEvent(new Event('visibilitychange'));
 assert.equal(draft.placeholder,'Message your agent…');release({...bridge(),suggestion:null});await settle();
});


test('an invalidation cannot join a context read started before the suggestion event',async t=>{
 const held=[];const {hint,draft,calls}=fixture(t,()=>new Promise(resolve=>held.push(resolve)));
 hint.api=new ComposerBridge(hint.api);hint.select({id:'a'});held[0](bridge());await settle();
 const context=hint.api.json('/api/tasks/a/bridge');
 hint.event({type:'task.suggestion_changed',task_id:'a'});assert.equal(calls[1][1].signal.aborted,true);
 t.mock.timers.tick(100);assert.equal(calls.length,3);
 held[1](bridge('obsolete context read'));await context;await settle();assert.equal(draft.placeholder,'Message your agent…');
 held[2](bridge('fresh suggestion'));await settle();assert.equal(draft.placeholder,'fresh suggestion');
});

test('covering the composer suspends hint work until its view returns',async t=>{
 const {hint,draft,calls}=fixture(t);hint.select({id:'a'});await settle();
 hint.setVisible(false);t.mock.timers.tick(15000);await settle();assert.equal(calls.length,1);
 hint.event({type:'task.suggestion_changed',task_id:'a'});t.mock.timers.tick(100);await settle();assert.equal(calls.length,1);
 hint.setVisible(true);await settle();assert.equal(calls.length,2);assert.equal(draft.placeholder,'Continue with tests');
});

test('browser hide aborts the shared bridge transport and resume cannot join its old read',async t=>{
 const held=[];const {hint,draft,calls}=fixture(t,()=>new Promise(resolve=>held.push(resolve)));
 hint.api=new ComposerBridge(hint.api);hint.select({id:'a'});
 const subscriber=hint.controller.signal;
 document.hidden=true;document.dispatchEvent(new Event('visibilitychange'));
 assert.equal(subscriber.aborted,true);assert.equal(calls[0][1].signal.aborted,true);
 document.hidden=false;document.dispatchEvent(new Event('visibilitychange'));assert.equal(calls.length,2);
 held[0](bridge('old hidden transport'));await settle();hint.render();assert.equal(draft.placeholder,'Message your agent…');
 held[1](bridge('fresh after resume'));await settle();assert.equal(draft.placeholder,'fresh after resume');
});
