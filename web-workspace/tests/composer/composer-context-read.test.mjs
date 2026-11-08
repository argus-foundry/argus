import test from 'node:test';
import assert from 'node:assert/strict';
import { readContext } from '../../composer/composer-context-read.js';
import { ComposerContext } from '../../composer/composer-context.js';

const measured = {task_id:'a',session_id:'s1',provider:'anthropic',live:true,measured_at:'2026-11-06T12:00:00Z',source:'bridge',context:{used_tokens:42,window_tokens:100,percent:42,categories:{messages:42}}};
for (const [name,response,expected] of [
  ['measurement',()=>Response.json(measured),measured],
  ['no measurement',()=>Response.json({...measured,context:null,live:false}),{...measured,context:null,live:false}],
  ['404 route',()=>new Response('',{status:404}),undefined],
  ['HTML fallback',()=>new Response('<!doctype html><html>old daemon</html>',{headers:{'content-type':'text/html'}}),undefined],
]) test(`context route handles ${name}`,async()=>{
  const controller = new AbortController();
  const api={request:async(path,options)=>{assert.equal(path,'/api/tasks/a/context');assert.equal(options.signal,controller.signal);return response();}};
  assert.deepEqual(await readContext(api,'a',controller.signal),expected);
});
for (const [name,response] of [
  ['server failure',()=>new Response('',{status:500})],
  ['malformed JSON',()=>Response.json({})],
  ['different task',()=>Response.json({...measured,task_id:'other'})],
]) test(`context route rejects ${name}`,async()=>{
  await assert.rejects(readContext({request:async()=>response()},'a',new AbortController().signal));
});
function context(t,request){
  const original=globalThis.document;globalThis.document={hidden:false};t.after(()=>globalThis.document=original);
  const view=Object.create(ComposerContext.prototype);
  Object.assign(view,{task:{id:'a',session_id:'s1'},data:{},render(){},api:{request,json:async()=>({})}});
  t.after(()=>view.cancelReads());return view;
}
test('superseded task and session context reads cannot publish',async t=>{
  let release;const view=context(t,()=>new Promise(resolve=>release=resolve));
  const pending=view.refresh(),signal=view.controller.signal;
  view.task={id:'b'};view.controller.abort();view.data={};release(Response.json(measured));await pending;
  assert.equal(signal.aborted,true);assert.deepEqual(view.data,{});
});
test('context event refreshes only selected task and invalidates same-task old session',async t=>{
  const view=context(t,async()=>Response.json({...measured,session_id:'s2'}));
  view.data={measurement:measured};view.controller=new AbortController();const old=view.controller;
  view.event({type:'task.context_changed',task_id:'other',payload:{session_id:'s2'}});assert.equal(view.controller,old);
  view.event({type:'task.context_changed',task_id:'a',payload:{session_id:'s2'}});
  assert.equal(old.signal.aborted,true);assert.equal(view.data.measurement,null);
  await view.contextRead;assert.equal(view.data.measurement.session_id,'s2');
});
test('failed supported read clears measurements rather than reviving bridge fallback',async t=>{
  const view=context(t,async()=>new Response('',{status:500}));
  view.data={measurement:measured};view.api.json=async()=>({live:true,context:{tokens:99,window:100}});
  await view.refresh();assert.equal(view.data.measurement,null);
});

test('same-task replacement rejects the old in-flight measurement even when abort is ignored',async t=>{
  let release;const view=context(t,()=>new Promise(resolve=>release=resolve));
  const old=view.refresh(),oldController=view.controller;
  view.api.request=async()=>Response.json({...measured,session_id:'s2',context:{...measured.context,categories:{memory:7}}});
  await view.invalidate('s2');
  release(Response.json(measured));await old;
  assert.equal(oldController.signal.aborted,true);
  assert.equal(view.data.measurement.session_id,'s2');
  assert.deepEqual(view.data.measurement.context.categories,{memory:7});
});
test('an old-session response after a new-session invalidation is unavailable',async t=>{
  const view=context(t,async()=>Response.json(measured));await view.invalidate('s2');
  assert.equal(view.data.measurement,null);
});
test('a settled context event does not pin later polling to an obsolete session',async t=>{
  const view=context(t,async()=>Response.json({...measured,session_id:'s2'}));
  await view.invalidate('s2');
  view.api.request=async()=>Response.json({...measured,session_id:'s3'});
  await view.refresh();assert.equal(view.data.measurement.session_id,'s3');
});
test('polling recovers when another session replaces an unsettled event measurement',async t=>{
  const view=context(t,async()=>Response.json(measured));
  await view.invalidate('s2');assert.equal(view.data.measurement,null);
  view.api.request=async()=>Response.json({...measured,session_id:'s3'});
  await view.refresh();assert.equal(view.data.measurement.session_id,'s3');
});

test('context publishes before slow account reads settle',async t=>{
  let release;const accounts=new Promise(resolve=>release=resolve);
  const view=context(t,async()=>Response.json({...measured,context:{...measured.context,categories:{messages:99}}}));
  view.data={measurement:measured};view.api.json=async path=>path==='/api/accounts'?accounts:{};
  const pending=view.refresh();
  try{await new Promise(setImmediate);assert.equal(view.data.measurement.context.categories.messages,99);}
  finally{release([]);await pending;}
});
test('same-session context bursts retain displayed categories and make bounded context-only reads',async t=>{
  t.mock.timers.enable({apis:['setTimeout']});
  let contexts=0,auxiliary=0;
  const view=context(t,async()=>{contexts++;return Response.json(measured);});
  view.data={measurement:measured,accounts:[{name:'work'}]};view.api.json=async()=>{auxiliary++;return {};};
  for(let i=0;i<10;i++){
    view.event({type:'task.context_changed',task_id:'a',payload:{session_id:'s1'}});
    assert.equal(view.data.measurement,measured);
    t.mock.timers.tick(20);await new Promise(setImmediate);
  }
  t.mock.timers.tick(100);await new Promise(setImmediate);
  assert.equal(contexts,1);assert.equal(auxiliary,0);
  assert.deepEqual(view.data.accounts,[{name:'work'}]);
});
test('overlapping open and poll refreshes share in-flight reads',async t=>{
  const releases=[];let contexts=0,auxiliary=0;
  const view=context(t,()=>{contexts++;return new Promise(resolve=>releases.push(resolve));});
  view.api.json=async()=>{auxiliary++;return {};};
  const pending=[view.refresh(),view.refresh(),view.refresh()];
  try{assert.equal(contexts,1);assert.equal(auxiliary,3);}
  finally{releases.forEach(release=>release(Response.json(measured)));await Promise.all(pending);}
});
test('supported context does not request legacy Hera metadata',async t=>{
  const paths=[],view=context(t,async()=>Response.json(measured));
  view.api.json=async path=>{paths.push(path);return {};};
  await view.refresh();assert.equal(paths.some(path=>path.includes('/meta')),false);
});

test('a context event during an in-flight read queues only one fresh follow-up',async t=>{
  t.mock.timers.enable({apis:['setTimeout']});
  const releases=[];let reads=0;
  const view=context(t,()=>{reads++;return new Promise(resolve=>releases.push(resolve));});view.data={measurement:measured};
  const first=view.refreshMeasurement();
  for(let i=0;i<10;i++)view.event({type:'task.context_changed',task_id:'a',payload:{session_id:'s1'}});
  t.mock.timers.tick(100);assert.equal(reads,1);
  releases.shift()(Response.json(measured));await first;assert.equal(reads,2);
  const fresh=view.contextRead;releases.shift()(Response.json({...measured,context:{...measured.context,categories:{messages:77}}}));
  await fresh;assert.equal(view.data.measurement.context.categories.messages,77);assert.equal(reads,2);
});
test('session replacement fences delayed auxiliary results independently',async t=>{
  let release;const accounts=new Promise(resolve=>release=resolve);
  const view=context(t,async()=>Response.json(measured));view.api.json=async path=>path==='/api/accounts'?accounts:{};
  const old=view.refresh(),controller=view.usageController;
  view.api.request=async()=>Response.json({...measured,session_id:'s2'});
  view.api.json=async path=>path==='/api/accounts'?[{name:'new'}]:{};
  await view.invalidate('s2');release([{name:'old'}]);await old;
  assert.equal(controller.signal.aborted,true);assert.deepEqual(view.data.accounts,[{name:'new'}]);
  assert.equal(view.data.measurement.session_id,'s2');
});

test('a read deadline clears a stuck shared measurement and allows retry',async t=>{
  let deadline=new AbortController();t.mock.method(AbortSignal,'timeout',()=>deadline.signal);
  const view=context(t,async(path,{signal})=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true})));
  view.data={measurement:measured};const first=view.refreshMeasurement(),second=view.refreshMeasurement();
  deadline.abort(new DOMException('Timed out','TimeoutError'));await Promise.all([first,second]);
  assert.equal(view.data.measurement,null);assert.equal(view.contextRead,null);
  deadline=new AbortController();view.api.request=async()=>Response.json(measured);
  await view.refreshMeasurement();assert.equal(view.data.measurement.session_id,'s1');
});

for(const type of ['session.started','session.exited'])test(`selected lifecycle ${type} clears, refetches and rejects the old read`,async t=>{
  let release,reads=0;
  const view=context(t,()=>{reads++;return new Promise(resolve=>release=resolve);});view.data={measurement:measured};
  const old=view.refreshMeasurement(),controller=view.controller;
  view.event({type,task_id:'other'});assert.equal(controller.signal.aborted,false);assert.equal(reads,1);
  view.api.request=async()=>{reads++;return Response.json({...measured,live:type!=='session.exited',context:{...measured.context,categories:{messages:77}}});};
  view.event({type,task_id:'a'});
  assert.equal(controller.signal.aborted,true);assert.equal(view.data.measurement,null);assert.equal(reads,2);
  await view.contextRead;release(Response.json(measured));await old;
  assert.equal(view.data.measurement.context.categories.messages,77);
  assert.equal(view.data.measurement.live,type!=='session.exited');
});

test('measurement and auxiliary reads use the documented ten-second deadline',async t=>{
  const durations=[],timeout=AbortSignal.timeout.bind(AbortSignal);
  t.mock.method(AbortSignal,'timeout',duration=>{durations.push(duration);return timeout(duration);});
  const view=context(t,async()=>Response.json(measured));
  await view.refresh();assert.deepEqual(durations,[10000,10000]);
});
